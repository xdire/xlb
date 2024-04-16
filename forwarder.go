package xlb

import (
	"context"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"io"
	"math"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	closedNetworkConnection = "use of closed network connection"
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
	logger      zerolog.Logger
	dialTimeout time.Duration
}

// NewForwarder creates load balancer forwarder that can be used to
// establish proxy communication between client and destination, adding
// some features like: the least loaded server balancing, health check
// routing, dynamic route update
func NewForwarder(params ServicePool, logger zerolog.Logger) *Forwarder {
	fwd := &Forwarder{
		routes: &[]*route{},
		logger: logger,
	}
	fwd.strategy = leastConnection{fwd}
	dialTimeout := params.RouteTimeout()
	if dialTimeout == 0 {
		dialTimeout = time.Second * 30
	}
	fwd.dialTimeout = dialTimeout
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

// UpdateServicePool will update service pool merging the new pool routes
// configuration with existing routes
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
	f.logger.Info().Msgf("forwarder routes updated to: %+v from: %+v", *f.routes, pool.Routes())
}

// Attach will attach some incoming session to the pool of upstream traffic distribution
func (f *Forwarder) Attach(ctx context.Context, in io.ReadWriteCloser) error {

	errTransport := make(chan error, 2)
	defer in.Close()

	rte := f.strategy.Next()
	if rte == nil {
		return fmt.Errorf("no active routes available")
	}

	dest, err := net.DialTimeout("tcp", rte.address, f.dialTimeout)
	if err != nil {
		f.logger.Err(err).Msgf("route unreachable %s", rte.address)
		return err
	}
	defer dest.Close()

	// Connection increment here as we reached destination
	atomic.AddUint32(&rte.connections, 1)

	go func(w io.WriteCloser, r io.ReadCloser) {
		defer w.Close()
		defer r.Close()
		_, err := io.Copy(w, r)
		errTransport <- err
	}(dest, in)

	go func(w io.WriteCloser, r io.ReadCloser) {
		defer w.Close()
		defer r.Close()
		_, err := io.Copy(w, r)
		errTransport <- err
	}(in, dest)

	var errs []error
	for i := 0; i < 2; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err = <-errTransport:
			// If detected error, check that error has nature of a normal behavior in the system
			// and will not affect the further behavior
			if err != nil && !(errors.Is(err, io.EOF) || strings.Contains(err.Error(), closedNetworkConnection)) {
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

// Next will make selection of the next route using the algorithm of least
// utilization (from the standpoint of this system) of the host connectivity
func (lc leastConnection) Next() *route {
	minVal := uint32(math.MaxUint32)

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
