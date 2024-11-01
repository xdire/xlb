package xlb

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/xdire/xlb/tlsutil"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultRequestPerSecondRate = 1000
	defaultIPLRUCapacity        = 1000
	defaultIPLRUBlockThreshold  = 10
)

// ServicePool structure that describes the unit of services
// where traffic can be routed using mTLS verification
type ServicePool struct {
	// How each ServicePool identified, CN match
	SvcIdentity string
	// to listen for incoming traffic
	SvcPort int
	// Rate per unit of  time.Duration
	SvcRateQuotaTimes int
	// Unit of time for rate
	SvcRateQuotaDuration time.Duration
	// Where to route this pool
	SvcRoutes []ServicePoolRoute
	// String server certificate as it was read from file
	Certificate string
	// String server key as it was read from file
	CertKey string
	// String CA certificate as it was read from file
	CACert string
	// General timeout to call the upstream service
	SvcRouteTimeout time.Duration
	// How many times server need to be revalidated before get healthy again
	SvcHealthCheckValidations int
	// How often Health check scheduler should check up on the server (1000ms or greater for optimal performance)
	SvcHealthCheckRescheduleMs int
}

func (t ServicePool) GetCertificate() string { return t.Certificate }

func (t ServicePool) GetPrivateKey() string { return t.CertKey }

func (t ServicePool) GetCACertificate() string { return t.CACert }

func (t ServicePool) Identity() string { return t.SvcIdentity }

func (t ServicePool) Port() int { return t.SvcPort }

func (t ServicePool) RateQuota() (int, time.Duration) {
	return t.SvcRateQuotaTimes, t.SvcRateQuotaDuration
}

func (t ServicePool) Routes() []ServicePoolRoute { return t.SvcRoutes }

func (t ServicePool) UnauthorizedAttempts() int {
	//TODO implement me
	panic("implement me")
}

func (t ServicePool) HealthCheckValidations() int { return t.SvcHealthCheckValidations }

func (t ServicePool) HealthCheckRescheduleMs() int { return t.SvcHealthCheckRescheduleMs }

func (t ServicePool) RouteTimeout() time.Duration { return t.SvcRouteTimeout }

type ServicePoolRoute struct {
	ServicePath   string
	ServiceActive bool
}

func (t ServicePoolRoute) Path() string { return t.ServicePath }

func (t ServicePoolRoute) Active() bool { return t.ServiceActive }

type Options struct {
	// Provide the reference for the logger instance
	Logger *zerolog.Logger
	// Log level as trace,debug,info,error
	LogLevel string
	// Capacity of IP LRU cache to manage unauthorized requests limits
	IpBlockListCapacity int
}

// LoadBalancer provides capability to accept the traffic and route it
// to the pool of upstream servers
type LoadBalancer struct {
	id           string
	runCtx       context.Context
	killCtx      context.CancelFunc
	logger       zerolog.Logger
	poolMap      map[string]ServicePool
	forwarderMap map[string]*Forwarder
	mutex        sync.Mutex
	ipLRU        *LRUCache
}

// NewLoadBalancer creates new instance of the load balancer
// using array of pool configuration. For each pool it is
// assumed that it has unique port, otherwise Listen will
// fail with the error
// TODO: Add checkup in constructor for enforce unique port per pool configuration
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

	logger := newZeroLogForName("xlb", id.String(), opt.LogLevel)
	if opt.Logger != nil {
		logger = *opt.Logger
	}

	poolMap := make(map[string]ServicePool)
	for _, pool := range cfgPool {
		if len(pool.Identity()) == 0 {
			return nil, fmt.Errorf("pool missing identity")
		}
		poolMap[pool.Identity()] = pool
	}

	ipLRUCap := opt.IpBlockListCapacity
	if ipLRUCap == 0 {
		ipLRUCap = defaultIPLRUCapacity
	}

	derCtx, cancelFunc := context.WithCancel(ctx)
	return &LoadBalancer{
		id:           id.String(),
		runCtx:       derCtx,
		killCtx:      cancelFunc,
		logger:       logger,
		forwarderMap: map[string]*Forwarder{},
		poolMap:      poolMap,
		ipLRU:        NewLRUCache(defaultIPLRUCapacity),
	}, nil
}

// UpdatePool will update pool using pool.Identity() method, this will
// trigger hot-swap operation on running forwarder for pool and should
// replace targets behind the load balancer without the restart
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
	scheduleListeners := make([]schedule, len(mapping))
	i := 0
	// Build schedule list not to have thread failures at this stage
	for port, identity := range mapping {

		pki, err := tlsutil.FromPKI(identity.GetCertificate(), identity.GetPrivateKey())
		if err != nil {
			return fmt.Errorf("invalid service pool pki data")
		}

		caCert := identity.GetCACertificate()
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM([]byte(caCert)) {
			return fmt.Errorf("invalid service pool ca data")
		}

		config := &tls.Config{
			Certificates:     []tls.Certificate{pki.Certificate},
			ClientAuth:       tls.RequireAndVerifyClientCert,
			ClientCAs:        caCertPool,
			MinVersion:       tls.VersionTLS13,
			MaxVersion:       tls.VersionTLS13,
			CurvePreferences: []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		}

		scheduleListeners[i] = schedule{port, config, identity}
		i++
	}

	// Schedule listeners one by one and fail if any of them fail
	derCtx, derCancel := context.WithCancel(lb.runCtx)
	errChan := make(chan error, len(scheduleListeners))
	defer close(errChan)

	wg := sync.WaitGroup{}
	for _, params := range scheduleListeners {
		wg.Add(1)
		go func(ctx context.Context, cancelAll context.CancelFunc, errChan chan error, toSchedule schedule) {

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
				lb.logger.Info().Msgf("closing listener at port %d", toSchedule.port)
				if err != nil {
					lb.logger.Err(err).Msgf("error closing listener at port %d", toSchedule.port)
				}
			}(listen)

			lb.logger.Info().Msgf("listening at port %d", toSchedule.port)

			// Bind pool and forwarder
			currentPool := toSchedule.pool
			var forwarder *Forwarder

			// Calculate Rate Quota or take a default of defaultRequestPerSecondRate rps
			// TODO: Provide ability to hot reload rate quotas per pool
			times, perTimeUnit := toSchedule.pool.RateQuota()
			if times == 0 {
				times = defaultRequestPerSecondRate
			}
			rateLimiter := NewTokenBucket(uint32(times), perTimeUnit)

			// Maintain incoming connections
			for {

				conn, err := listen.Accept()
				if err != nil {
					if strings.Contains(err.Error(), "closed network") {
						// Graceful
						lb.logger.Debug().Msgf("listener closed, closed network for port: %d", toSchedule.port)
						errChan <- nil
					} else {
						// Other kind of error
						lb.logger.Err(err).Msgf("failed to accept connection, error")
						errChan <- fmt.Errorf("failed to listen on port, error: %w", err)
					}
					lb.mutex.Lock()
					delete(lb.forwarderMap, currentPool.Identity())
					lb.mutex.Unlock()
					return
				}

				// Check if IP address connecting is in our cache and if it violated anything
				if entry, ok := lb.ipLRU.Get(conn.RemoteAddr().String()); ok {
					if entry.Count > defaultIPLRUBlockThreshold {
						// If the entry exists and has not expired, block the connection
						if entry.ExpiresAt.After(time.Now()) {
							lb.logger.Trace().Msgf("rate quota exceeded for pool: %s", toSchedule.pool.Identity())
							// TODO Provide notification pipeline abstraction where certain events can be dumped for behavior adjustments
							// example: notify.Submit(IpBlockedNotification{identity,quota,time})
							conn.Close()
							continue
						} else {
							// Invalidate lazily
							lb.ipLRU.Invalidate(conn.RemoteAddr().String())
						}
					}
				}

				lb.logger.Debug().Msgf("accepting request for port %d", toSchedule.port)

				// Convert to TLS and proceed with the handshake
				tlsConn := conn.(*tls.Conn)
				err = tlsConn.Handshake()
				if err != nil {
					lb.logger.Err(err).Msg("cannot complete handshake")
					err = tlsConn.Close()
					if err != nil {
						lb.logger.Err(err).Msg("cannot close connection after failed handshake")
					}
					// Add address to IP LRU list and increment count of engagements
					lb.ipLRU.IncrementCount(tlsConn.RemoteAddr().String(), 5*time.Minute)
					continue
				}

				// As pool using the mTLS for the identity verification (at this moment port->creds)
				// it seems to be logical to apply the Rate quotas right after the verified credentials
				if !rateLimiter.WithinRateLimit() {
					lb.logger.Trace().Msgf("rate quota exceeded for pool: %s", toSchedule.pool.Identity())
					// TODO Provide notification pipeline abstraction where certain events can be dumped for behavior adjustments
					// example: notify.Submit(RateLimitNotification{identity,quota,time})
					err = tlsConn.Close()
					if err != nil {
						lb.logger.Err(err).Msg("cannot close connection on rate quota limit")
					}
					continue
				}

				// Verify certificate CN and find or create corresponding backend to dispatch
				certs := tlsConn.ConnectionState().PeerCertificates
				if len(certs) == 0 {
					lb.logger.Error().Msg("failed to extract certificate")
					err = tlsConn.Close()
					if err != nil {
						lb.logger.Err(err).Msg("cannot close connection after certificate failure")
					}
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
								if errors.Is(err, context.Canceled) {
									lb.logger.Err(err).Msg("conn closing gracefully on context")
									return
								}
								lb.logger.Err(err).Msg("cannot attach to backend")
							}
						}()
					} else {
						forwarder = NewForwarder(currentPool, lb.logger)
						lb.mutex.Lock()
						lb.forwarderMap[currentPool.Identity()] = forwarder
						lb.mutex.Unlock()
						go func() {
							err := forwarder.Attach(ctx, tlsConn)
							if err != nil {
								if errors.Is(err, context.Canceled) {
									lb.logger.Err(err).Msg("conn closing gracefully on context")
									return
								}
								lb.logger.Err(err).Msgf("cannot attach to backend")
							}
						}()
					}
				} else {

					// TODO Add here the rate limiting for incorrect matches, possibly placing them into the LRU cache with bad IP address match
					lb.logger.Warn().Msgf("certificate failed identity matching %s", identity)
					err = tlsConn.Close()
					if err != nil {
						lb.logger.Err(err).Msg("cannot close connection after identity mismatch")
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

// TokenBucket Not a thread safe rate limiting object, provides capability to
// count the rate of calls for Allowed() method with decision
// if call fits within certain limits or not
type TokenBucket struct {
	tokens   uint32
	bucket   uint32
	timeUnit time.Duration
	updated  time.Time
}

// NewTokenBucket create new token bucket with required params to operate
func NewTokenBucket(tokens uint32, timeUnit time.Duration) *TokenBucket {
	return &TokenBucket{bucket: tokens, tokens: tokens, timeUnit: timeUnit, updated: time.Now()}
}

// WithinRateLimit get the bool decision if this call within rate limit or not
func (t *TokenBucket) WithinRateLimit() bool {
	curTime := time.Now()
	passTime := curTime.Sub(t.updated)
	t.updated = curTime

	tu := t.timeUnit.Seconds()
	// Calculate as following:
	// r.tokens/tu -> 10/time-unit -> 10/1 sec -> 10 in 1 second
	// passed time -> 0.2sec * 10 in one sec = +2 units
	t.bucket = t.bucket + uint32(passTime.Seconds()*(float64(t.tokens)/tu))

	if t.bucket > t.tokens {
		t.bucket = t.tokens
	}

	if t.bucket < 1 {
		return false
	}
	t.bucket--
	return true
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

func newZeroLogForName(name, id, level string) zerolog.Logger {
	zLevel := zerolog.ErrorLevel
	if len(level) > 0 {
		newLevel, err := zerolog.ParseLevel(level)
		if err == nil {
			zLevel = newLevel
		}
	}
	return zerolog.New(os.Stdout).
		Level(zLevel).With().Timestamp().
		Caller().Str(name, id).Logger()
}
