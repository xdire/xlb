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
	routes      []*route
	mutex       sync.Mutex
	strategy    leastConnection
	logger      Logger
	dialTimeout time.Duration
}

func NewForwarder(ctx context.Context, params ServicePool, logger Logger) *Forwarder {
	fwd := &Forwarder{
		routes: nil,
		mutex:  sync.Mutex{},
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
		fwd.routes = append(fwd.routes, &route{
			address:     rte.Path(),
			healthy:     atomic.Bool{},
			connections: 0,
			active:      atomic.Bool{},
		})
	}
	return fwd
}

func (f *Forwarder) Attach(ctx context.Context, in *tls.Conn) error {

	errTransport := make(chan error, 2)
	defer func(conn net.Conn) {
		err := conn.Close()
		if err != nil {
			f.logger.Debug(fmt.Sprintf("forwarder attached channel closed: %s", in.LocalAddr().String()))
		}
		close(errTransport)
	}(in)

	rte := f.strategy.Next()
	dest, err := net.DialTimeout("tcp", rte.address, f.dialTimeout)
	if err != nil {
		f.logger.Error(fmt.Sprintf("route unreachable %s", rte.address))
		return err
	}

	go func(w io.Writer, r io.Reader) {
		atomic.AddUint32(&rte.connections, 1)
		_, err := io.Copy(w, r)
		errTransport <- err
		atomic.AddUint32(&rte.connections, ^uint32(0))
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
		case err := <-errTransport:
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

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
	var rte *route
	for _, route := range lc.fwd.routes {
		if route.active.Load() && route.healthy.Load() {
			conn := atomic.LoadUint32(&route.connections)
			if conn < minVal {
				minVal = conn
				rte = route
			}
		}
	}
	return rte
}
