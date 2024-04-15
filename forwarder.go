package xlb

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type route struct {
	address     string
	healthy     atomic.Bool
	connections uint32
	active      atomic.Bool
}

type Forwarder struct {
	routes      *[]*route
	mutex       sync.RWMutex
	updateLock  bool
	strategy    leastConnection
	logger      Logger
	dialTimeout time.Duration
	health      *HealthCheckScheduler
}

func NewForwarder(params ServicePool, logger Logger) *Forwarder {
	fwd := &Forwarder{
		routes: &[]*route{},
		logger: logger,
		health: NewHealthCheckScheduler(HealthSchedulerOptions{
			MaxItems:        len(params.Routes()) * 2,
			Logger:          logger,
			ReleaseChecks:   params.HealthCheckValidations(),
			CheckIntervalMs: 5000,
		}),
	}
	// Add strategy linking the forwarder
	fwd.strategy = leastConnection{fwd}
	// Default or provided timeout
	dialTimeout := params.RouteTimeout()
	if dialTimeout == 0 {
		dialTimeout = time.Second * 30
	}
	fwd.dialTimeout = dialTimeout
	// Assign routes
	for _, rte := range params.Routes() {
		if !rte.Active() {
			continue
		}
		*fwd.routes = append(*fwd.routes, func() *route {
			r := &route{
				address:     rte.Path(),
				healthy:     atomic.Bool{},
				connections: 0,
				active:      atomic.Bool{},
			}
			r.active.Store(true)
			r.healthy.Store(true)
			return r
		}())
	}
	return fwd
}

// UpdateServicePool
// Will update current service pool with the set of new routes or other information
// and do hotswap for the active routes in dispatch
func (f *Forwarder) UpdateServicePool(pool ServicePool) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	// Create the match map
	currentPoolMap := map[string]*route{}
	for _, rte := range *f.routes {
		currentPoolMap[rte.address] = rte
	}

	newRoutePool := make([]*route, 0)
	for _, poolRoute := range pool.Routes() {
		// If route exists then change parameters and inherit current connection stage
		if fwdRoute, exists := currentPoolMap[poolRoute.Path()]; exists {
			fwdRoute.active.Store(poolRoute.Active())
			newRoutePool = append(newRoutePool, fwdRoute)
			delete(currentPoolMap, poolRoute.Path())
			continue
		}
		// Create new otherwise
		newRoutePool = append(newRoutePool, func() *route {
			r := &route{
				address:     poolRoute.Path(),
				healthy:     atomic.Bool{},
				connections: 0,
				active:      atomic.Bool{},
			}
			r.active.Store(poolRoute.Active())
			r.healthy.Store(true)
			return r
		}())
	}
	// For the routes not in the new update, mark inactive for the rest of the resources free them if holding the pointer
	for _, rte := range currentPoolMap {
		rte.active.Store(false)
	}
	f.routes = &newRoutePool
	f.logger.Info(fmt.Sprintf("forwarder routes updated to: %+v from: %+v", *f.routes, pool.Routes()))
}

// Attach
// Attaches provided in channel to the one of the destinations behind the load balancer
func (f *Forwarder) Attach(ctx context.Context, in *tls.Conn) error {

	errTransport := make(chan error, 2)
	defer func(conn net.Conn) {
		err := conn.Close()
		if err != nil {
			f.logger.Debug(fmt.Sprintf("forwarder conn closed: %s", in.LocalAddr().String()))
		}
	}(in)

	var rte *route
	var dest net.Conn
	var err error

	// Find next available route for satisfy connection request or fail finding nothing
	for {
		rte = f.strategy.Next()
		// If no routes found, meaning all unhealthy or non-active then  provide error
		if rte == nil {
			return fmt.Errorf("no active routes available")
		}

		dest, err = net.DialTimeout("tcp", rte.address, f.dialTimeout)
		if err != nil {
			f.logger.Error(fmt.Sprintf("route unreachable %s", rte.address))
			f.health.AddUnhealthy(ctx, rte, 10*time.Second)
			continue
		}
		// Exit loop on first valid route
		break
	}

	// Connection increment here as we reached destination
	atomic.AddUint32(&rte.connections, 1)

	go func(w io.Writer, r io.Reader) {
		_, err := io.Copy(w, r)
		errTransport <- err
	}(dest, in)

	go func(w io.Writer, r io.Reader) {
		_, err := io.Copy(w, r)
		errTransport <- err
	}(in, dest)

	var errs []error
	for i := 0; i < 2; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err = <-errTransport:
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	// Connection decrement here as all pipes are closed by now
	atomic.AddUint32(&rte.connections, ^uint32(0))
	close(errTransport)
	if len(errs) > 0 {
		return fmt.Errorf("forwarder attach closed with errors: %+v", errs)
	}
	return nil
}

type leastConnection struct {
	fwd *Forwarder
}

func (lc leastConnection) Next() *route {
	minVal := uint32(32000)

	// Lock and unlock just to get access to the latest routes slice
	// this delivers support for hot-reload of the routes by pointer refresh
	// leastConnection might work for one cycle with outdated records
	lc.fwd.mutex.RLock()
	lc.fwd.mutex.RUnlock()

	var rte *route
	for _, route := range *lc.fwd.routes {
		if route.active.Load() && route.healthy.Load() {
			conn := atomic.LoadUint32(&route.connections)
			if conn < minVal {
				minVal = conn
				rte = route
			}
		}
	}
	// @ManualTesting
	// to observe the route to be dispatched in logs, uncomment following line
	// fmt.Println(fmt.Sprintf("[FORWARDER][STRATEGY][TEST] route selected: %s %d", rte.address, rte.connections))
	return rte
}
