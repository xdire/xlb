package xlb

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/google/uuid"
	"github.com/xdire/xlb/tlsutil"
	"net"
	"strings"
	"sync"
)

type Options struct {
	Logger   Logger
	LogLevel string
}

type LoadBalancer struct {
	id           string
	runCtx       context.Context
	killCtx      context.CancelFunc
	logger       Logger
	poolMap      map[string]ServicePool
	forwarderMap map[string]*Forwarder
	mutex        sync.Mutex
}

func NewLoadBalancer(ctx context.Context, cfgPool []ServicePool, opt Options) (*LoadBalancer, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("gen id failed")
	}
	if ctx == nil {
		return nil, fmt.Errorf("missing parameter ctx")
	}
	if cfgPool == nil || len(cfgPool) == 0 {
		return nil, fmt.Errorf("missing parameter cfgPool")
	}

	logger := opt.Logger
	if logger == nil {
		logger = newZeroLogForName("xlb", id.String(), opt.LogLevel)
	}

	poolMap := make(map[string]ServicePool)
	for _, pool := range cfgPool {
		if len(pool.Identity()) == 0 {
			return nil, fmt.Errorf("pool missing identity")
		}
		poolMap[pool.Identity()] = pool
	}

	derCtx, cancelFunc := context.WithCancel(ctx)
	return &LoadBalancer{
		id:           id.String(),
		runCtx:       derCtx,
		killCtx:      cancelFunc,
		logger:       logger,
		forwarderMap: map[string]*Forwarder{},
		poolMap:      poolMap,
	}, nil
}

func (lb *LoadBalancer) UpdatePool(pool ServicePool) error {
	if len(pool.Identity()) == 0 {
		return fmt.Errorf("pool missing identity")
	}
	lb.mutex.Lock()
	defer lb.mutex.Unlock()
	lb.poolMap[pool.Identity()] = pool
	if fwd, exists := lb.forwarderMap[pool.Identity()]; exists {
		fwd.UpdateServicePool(pool)
	}
	return nil
}

// Listen will try to launch balancer on all the required ports, strategy is all or nothing
func (lb *LoadBalancer) Listen() error {

	// Do this step to ensure that we will fail on misconfiguration if more than
	// one service pool mapping presented to this load balance for mTLS
	// TODO: TLS Allows manual verification for the handshake by that we can launch multiple pools on the same port
	// TODO: for the first version we will limit 1 pool per port for simplicity
	mapping, err := collectListenTargets(lb.poolMap)
	if err != nil {
		return err
	}

	type schedule struct {
		port int
		tls  *tls.Config
		pool ServicePool
	}
	scheduleListeners := make([]schedule, 0)

	// Build schedule list not to have thread failures at this stage
	for port, identity := range mapping {

		pki, err := tlsutil.FromPKI(identity.TLSData().GetCertificate(), identity.TLSData().GetPrivateKey())
		if err != nil {
			return fmt.Errorf("invalid service pool pki data")
		}

		config := &tls.Config{
			Certificates: []tls.Certificate{pki.Certificate},
			ClientAuth:   tls.RequestClientCert,
			RootCAs:      pki.CertPool,
			CipherSuites: []uint16{
				tls.TLS_AES_128_GCM_SHA256,       // 2022 TLS v1.3 compliant
				tls.TLS_CHACHA20_POLY1305_SHA256, // 2022 TLS v1.3 compliant
				tls.TLS_AES_256_GCM_SHA384,       // 2022 TLS v1.3 compliant
			},
			MinVersion:       tls.VersionTLS13,
			MaxVersion:       tls.VersionTLS13,
			CurvePreferences: []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		}

		scheduleListeners = append(scheduleListeners, schedule{port, config, identity})
	}

	// Schedule listeners one by one and fail if any of them fail
	derCtx, derCancel := context.WithCancel(lb.runCtx)
	errChan := make(chan error, len(scheduleListeners))
	defer close(errChan)

	wg := sync.WaitGroup{}
	for _, params := range scheduleListeners {
		go func(ctx context.Context, cancelAll context.CancelFunc, errChan chan error, toSchedule schedule) {

			wg.Add(1)
			// Don't forget to close all contexts
			defer derCancel()
			// Try to listen
			listen, err := tls.Listen("tcp", fmt.Sprintf("localhost:%d", toSchedule.port), toSchedule.tls)
			if err != nil {
				errChan <- fmt.Errorf("failed to listen on port")
				wg.Done()
				return
			}

			// Spawn the coroutine to watch for the context break
			go func(l net.Listener) {
				// Here all required procedures were established for the listener thread
				wg.Done()
				// Await for context break
				<-ctx.Done()
				err := listen.Close()
				lb.logger.Info(fmt.Sprintf("closing listener at port %d", toSchedule.port))
				if err != nil {
					lb.logger.Error(fmt.Sprintf("error closing listener at port %d", toSchedule.port))
				}
			}(listen)

			lb.logger.Info(fmt.Sprintf("listening at port %d", toSchedule.port))

			// Bind pool and forwarder
			currentPool := toSchedule.pool
			var forwarder *Forwarder

			// Maintain incoming connections
			for {

				conn, err := listen.Accept()
				if err != nil {
					if strings.Contains(err.Error(), "closed network") {
						// Graceful
						lb.logger.Debug(fmt.Sprintf("listener closed, closed network for port: %d", toSchedule.port))
						errChan <- nil
					} else {
						// Other kind of error
						lb.logger.Error(fmt.Errorf("failed to accept connection, error: %w", err).Error())
						errChan <- fmt.Errorf("failed to listen on port, error: %w", err)
					}
					delete(lb.forwarderMap, currentPool.Identity())
					return
				}

				lb.logger.Debug(fmt.Sprintf("accepting request for port %d", toSchedule.port))

				// Convert to TLS and proceed with the handshake
				tlsConn := conn.(*tls.Conn)
				err = tlsConn.Handshake()
				if err != nil {
					lb.logger.Error(fmt.Errorf("cannot complete handshake, %w", err).Error())
					continue
				}

				// Verify certificate CN and find or create corresponding backend to dispatch
				certs := tlsConn.ConnectionState().PeerCertificates
				if len(certs) == 0 {
					lb.logger.Error("failed to extract certificate")
					continue
				}

				// TODO Complete common name verification with certificate chain if provided as chain
				curCrt := certs[0]
				identity := curCrt.Subject.CommonName

				// Forward the connections
				if identity == currentPool.Identity() {

					if forwarder != nil {
						go func() {
							err := forwarder.Attach(ctx, tlsConn)
							if err != nil {
								if err.Error() == "context canceled" {
									lb.logger.Error("conn closing gracefully on context")
									return
								}
								lb.logger.Error(fmt.Errorf("cannot attach to backend, error: %w", err).Error())
							}
						}()
					} else {
						forwarder = NewForwarder(ctx, currentPool, lb.logger)
						lb.mutex.Lock()
						lb.forwarderMap[currentPool.Identity()] = forwarder
						lb.mutex.Unlock()
						go func() {
							err := forwarder.Attach(ctx, tlsConn)
							if err != nil {
								if err.Error() == "context canceled" {
									lb.logger.Error("conn closing gracefully on context")
									return
								}
								lb.logger.Error(fmt.Errorf("cannot attach to backend, error: %w", err).Error())
							}
						}()
					}
				}
			}

		}(derCtx, derCancel, errChan, params)
	}
	// Wait here until all the listeners will spawn and monitor if any failed, and if failed — fail the whole task
	wg.Wait()
	err = <-errChan
	if err != nil {
		derCancel()
		return fmt.Errorf("failed to listen for one of the ports, all listeners will shutdown, error: %w", err)
	}
	return nil
}

// Collect all the targets in correlation to the ports they're running at
func collectListenTargets(fromData map[string]ServicePool) (map[int]ServicePool, error) {
	portMap := make(map[int]ServicePool)
	for _, pool := range fromData {
		if _, found := portMap[pool.Port()]; !found {
			portMap[pool.Port()] = pool
			continue
		}
		return nil, fmt.Errorf("more than one service pool per port")
	}
	return portMap, nil
}
