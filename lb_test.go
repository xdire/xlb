package xlb

import (
	"context"
	"github.com/xdire/xlb/httputil"
	"github.com/xdire/xlb/tlsutil"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TODO 1: For All Tests: find the areas of code duplication, separate those across multiple functions

// TODO 2: If tests launched with -race flag, it should be following optimizations to be added:
// TODO: watch for the race flag and increase shutoff timers accordingly to match any type of machine performance
// TODO: if tests running in bulk, the file generation/deletion might be optimized to run once per similar test batch

// TestRunningLoadBalancerBaseRouting
// Will test basic connectivity and balancing for the LoadBalancer
func TestRunningLoadBalancerBaseRouting(t *testing.T) {
	ctx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()

	t.Log("test prepare, create TLS files")
	// Prepare TLS data for the test
	err := tlsutil.CreateLocalTLSData("test")
	defer func() {
		t.Log("test unwind, delete TLS files")
		out, err := tlsutil.WipeLocalTLSData("./")
		if err != nil {
			t.Error("cannot clean pre-arranged test files")
		}
		t.Logf("files deleted: %+v", out)
	}()
	if err != nil {
		t.Fatal(err)
	}

	// Prepare responding servers for the test
	// TODO: Optimize tests in the way to obtain random ranges for ports to configure LB and test targets
	t.Log("test prepare, create responding servers")
	stopServer1, err := httputil.CreateTestServer(9081, "api", "Server 1 responded")
	if err != nil {
		t.Errorf("Failed to start test server 1: %v", err)
	}
	defer stopServer1()
	stopServer2, err := httputil.CreateTestServer(9082, "api", "Server 2 responded")
	if err != nil {
		t.Errorf("Failed to start test server 2: %v", err)
	}
	defer stopServer2()
	stopServer3, err := httputil.CreateTestServer(9083, "api", "Server 3 responded")
	if err != nil {
		t.Errorf("Failed to start test server 3: %v", err)
	}
	defer stopServer3()

	t.Log("test prepare, create loadbalancer instance")
	cert, err := os.ReadFile("server.crt")
	if err != nil {
		t.Fatal(err)
		return
	}
	key, err := os.ReadFile("server.key")
	if err != nil {
		t.Fatal(err)
		return
	}
	ca, err := os.ReadFile("ca.crt")
	if err != nil {
		t.Fatal(err)
		return
	}

	balancer, err := NewLoadBalancer(ctx, []ServicePool{{
		SvcIdentity:          "test",
		SvcPort:              9092,
		SvcRateQuotaTimes:    1000,
		SvcRateQuotaDuration: time.Second * 1,
		SvcRoutes: []ServicePoolRoute{
			ServicePoolRoute{
				ServicePath:   "localhost:9081",
				ServiceActive: true,
			},
			ServicePoolRoute{
				ServicePath:   "localhost:9082",
				ServiceActive: true,
			},
			ServicePoolRoute{
				ServicePath:   "localhost:9083",
				ServiceActive: true,
			},
		},
		Certificate: string(cert),
		CertKey:     string(key),
		CACert:      string(ca),
	}}, Options{})
	if err != nil {
		t.Fatal("cannot configure load balancer")
	}

	// Give 10 seconds for testing everything
	go func() {
		<-time.After(time.Second * 15)
		cancelAll()
	}()

	// Create senders
	responses := make(chan string, 100)
	go func() {
		// Give 5 seconds for everything to startup
		<-time.After(time.Second * 5)
		for i := 0; i < 100; i++ {
			// Spawn requests in batches
			if i%10 == 0 {
				<-time.After(time.Millisecond * 100)
			}
			go func() {
				// Test load balancer
				res, err := httputil.SendTestRequest("https://localhost:9092/api")
				if err != nil {
					t.Errorf("cannot reach remotes, error: %+v", err)
				}
				responses <- res
			}()
		}
	}()

	// Listen for all threads
	err = balancer.Listen()
	if err != nil {
		t.Errorf("listen returned error: %+v", err)
	}

	responded := [3]int{0, 0, 0}
	for res := range responses {
		if strings.Contains(res, "1") {
			responded[0]++
		} else if strings.Contains(res, "2") {
			responded[1]++
		} else if strings.Contains(res, "3") {
			responded[2]++
		}
		if len(responses) == 0 {
			break
		}
	}

	if responded[0] == 0 || responded[1] == 0 || responded[2] == 0 {
		t.Errorf("one of the servers was not selected by the strategy")
	}
	t.Logf("Servers responded 1<%d times> 2<%d times> 3<%d times>", responded[0], responded[1], responded[2])

	if responded[0]+responded[1]+responded[2] > 100 {

	}

	// Let everything unwind gracefully
	<-time.After(time.Second * 5)
}

// TestLoadBalancerHotReloadRouting
// Will test how servers can be added to balancer for forwarder to
// start to dispatch immediately for added capacity
func TestLoadBalancerHotReloadRouting(t *testing.T) {
	ctx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()

	t.Log("test prepare, create TLS files")
	// Prepare TLS data for the test
	err := tlsutil.CreateLocalTLSData("test")
	defer func() {
		t.Log("test unwind, delete TLS files")
		out, err := tlsutil.WipeLocalTLSData("./")
		if err != nil {
			t.Error("cannot clean pre-arranged test files")
		}
		t.Logf("files deleted: %+v", out)
	}()
	if err != nil {
		t.Fatal(err)
	}

	// Prepare responding servers for the test
	// TODO: Optimize tests in the way to obtain random ranges for ports to configure LB and test targets
	t.Log("test prepare, create responding servers")
	stopServer1, err := httputil.CreateTestServer(9084, "api", "Server 1 responded")
	if err != nil {
		t.Errorf("Failed to start test server 1: %v", err)
	}
	defer stopServer1()
	stopServer2, err := httputil.CreateTestServer(9085, "api", "Server 2 responded")
	if err != nil {
		t.Errorf("Failed to start test server 2: %v", err)
	}
	defer stopServer2()
	stopServer3, err := httputil.CreateTestServer(9086, "api", "Server 3 responded")
	if err != nil {
		t.Errorf("Failed to start test server 3: %v", err)
	}
	defer stopServer3()

	t.Log("test prepare, create loadbalancer instance")
	cert, err := os.ReadFile("server.crt")
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile("server.key")
	if err != nil {
		t.Fatal(err)
	}
	ca, err := os.ReadFile("ca.crt")
	if err != nil {
		t.Fatal(err)
		return
	}

	// Create pool with the single host and add rest of the hosts during the
	// requests coming concurrently
	servicePool := ServicePool{
		SvcIdentity:          "test",
		SvcPort:              9093,
		SvcRateQuotaTimes:    1000,
		SvcRateQuotaDuration: time.Second * 1,
		SvcRoutes: []ServicePoolRoute{
			{
				ServicePath:   "localhost:9084",
				ServiceActive: true,
			},
		},
		Certificate: string(cert),
		CertKey:     string(key),
		CACert:      string(ca),
	}

	balancer, err := NewLoadBalancer(ctx, []ServicePool{servicePool}, Options{LogLevel: "error"})
	if err != nil {
		t.Fatal("cannot configure load balancer")
	}

	// Give 10 seconds for testing everything
	go func() {
		<-time.After(time.Second * 15)
		cancelAll()
	}()

	// Create senders
	responses := make(chan string, 100)

	go func() {
		// Give 5 seconds for everything to start up
		<-time.After(time.Second * 5)
		nextPort := 9084
		for i := 0; i < 100; i++ {

			// Spawn requests in batches
			if i%10 == 0 {
				<-time.After(time.Millisecond * 100)
			}

			// each 20th request hot reload the routes, adding more servers to route to
			if i > 0 && i%20 == 0 && nextPort < 9086 {
				nextPort++
				servicePool.SvcRoutes = append(servicePool.SvcRoutes, ServicePoolRoute{
					ServicePath:   "localhost:" + strconv.Itoa(nextPort),
					ServiceActive: true,
				})
				// Do hot reload for the forwarder
				err = balancer.UpdatePool(servicePool)
				if err != nil {
					t.Errorf("cannot apply update pool for balancer, error %+v", err)
				}
			}

			go func() {
				// Test load balancer
				res, err := httputil.SendTestRequest("https://localhost:9093/api")
				if err != nil {
					t.Errorf("cannot reach remotes, error: %+v", err)
				}
				responses <- res
			}()
		}
	}()

	// Listen for all threads
	err = balancer.Listen()
	if err != nil {
		t.Fatalf("listen returned error: %+v", err)
	}

	responded := [3]int{0, 0, 0}
	for res := range responses {
		if strings.Contains(res, "1") {
			responded[0]++
		} else if strings.Contains(res, "2") {
			responded[1]++
		} else if strings.Contains(res, "3") {
			responded[2]++
		}
		if len(responses) == 0 {
			break
		}
	}

	if responded[0] == 0 || responded[1] == 0 || responded[2] == 0 {
		t.Errorf("one of the servers was not selected by the strategy")
	}
	t.Logf("Servers responded 1<%d times> 2<%d times> 3<%d times>", responded[0], responded[1], responded[2])

	// Let everything unwind gracefully
	<-time.After(time.Second * 5)
}

// TestLoadBalancerHealthCheckFlow
// Will test how servers will interact with the health check service in terms of
// - shutdown
// - restore
// - balancing after the restore event
func TestLoadBalancerHealthCheckFlow(t *testing.T) {
	ctx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()

	t.Log("test prepare, create TLS files")
	// Prepare TLS data for the test
	err := tlsutil.CreateLocalTLSData("test")
	defer func() {
		t.Log("test unwind, delete TLS files")
		out, err := tlsutil.WipeLocalTLSData("./")
		if err != nil {
			t.Error("cannot clean pre-arranged test files")
		}
		t.Logf("files deleted: %+v", out)
	}()
	if err != nil {
		t.Fatal(err)
	}

	// Prepare responding servers for the test
	// TODO: Optimize tests in the way to obtain random ranges for ports to configure LB and test targets
	t.Log("test prepare, create responding servers")
	stopServer1, err := httputil.CreateTestServer(9087, "api", "Server 1 responded")
	if err != nil {
		t.Errorf("Failed to start test server 1: %v", err)
	}
	defer stopServer1()
	stopServer2, err := httputil.CreateTestServer(9088, "api", "Server 2 responded")
	if err != nil {
		t.Errorf("Failed to start test server 2: %v", err)
	}
	defer stopServer2()
	stopServer3, err := httputil.CreateTestServer(9089, "api", "Server 3 responded")
	if err != nil {
		t.Errorf("Failed to start test server 3: %v", err)
	}
	defer stopServer3()
	stopServer4, err := httputil.CreateTestServer(9090, "api", "Server 4 responded")
	if err != nil {
		t.Errorf("Failed to start test server 3: %v", err)
	}
	defer stopServer4()

	// Order to switch servers offline
	windDownChan := make(chan func() error, 4)
	windDownChan <- stopServer1
	windDownChan <- stopServer2
	windDownChan <- stopServer3
	windDownChan <- stopServer4

	// Order to bring servers back online
	scaleUpChan := make(chan func() (func() error, error), 4)
	scaleUpChan <- func() (func() error, error) {
		return httputil.CreateTestServer(9087, "api", "Server 1 after scale")
	}
	scaleUpChan <- func() (func() error, error) {
		return httputil.CreateTestServer(9088, "api", "Server 2 after scale")
	}
	scaleUpChan <- func() (func() error, error) {
		return httputil.CreateTestServer(9089, "api", "Server 3 after scale")
	}
	scaleUpChan <- func() (func() error, error) {
		return httputil.CreateTestServer(9090, "api", "Server 4 after scale")
	}

	wipeCleanChan := make(chan func() error, 4)

	t.Log("test prepare, create loadbalancer instance")
	cert, err := os.ReadFile("server.crt")
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile("server.key")
	if err != nil {
		t.Fatal(err)
	}
	ca, err := os.ReadFile("ca.crt")
	if err != nil {
		t.Fatal(err)
		return
	}

	// Create pool with the single host and add rest of the hosts during the
	// requests coming concurrently
	servicePool := ServicePool{
		SvcIdentity:          "test",
		SvcPort:              9094,
		SvcRateQuotaTimes:    1000,
		SvcRateQuotaDuration: time.Second * 1,
		SvcRoutes: []ServicePoolRoute{
			ServicePoolRoute{
				ServicePath:   "localhost:9087",
				ServiceActive: true,
			},
			ServicePoolRoute{
				ServicePath:   "localhost:9088",
				ServiceActive: true,
			},
			ServicePoolRoute{
				ServicePath:   "localhost:9089",
				ServiceActive: true,
			},
			ServicePoolRoute{
				ServicePath:   "localhost:9090",
				ServiceActive: true,
			},
		},
		Certificate:                string(cert),
		CertKey:                    string(key),
		CACert:                     string(ca),
		SvcHealthCheckRescheduleMs: 1000,
		SvcRouteTimeout:            time.Second * 1,
	}

	balancer, err := NewLoadBalancer(ctx, []ServicePool{servicePool}, Options{LogLevel: "info"})
	if err != nil {
		t.Fatal("cannot configure load balancer")
	}

	// Give 15 seconds for testing everything (a little more time here as additional reloads mid-scrjpt)
	go func() {
		<-time.After(time.Second * 20)
		cancelAll()
	}()

	// Create senders
	responses := make(chan string, 300)

	// Start the test
	// Strategy:
	//   - start normal
	//   - after some dispatch kill servers
	//   - revive servers after a few moments altogether
	//   - observe health check work for state restoration
	//   - observe the requests before and after destroy/revive events
	go func() {
		// Give 5 seconds for everything to startup
		<-time.After(time.Second * 5)
		for i := 0; i < 300; i++ {

			// Add some delay on batches to give servers time to catch up for situation
			if i%10 == 0 {
				<-time.After(time.Millisecond * 200)
			}

			// Each 20th request kill
			if i%25 == 0 {
				if state, exists := balancer.forwarderMap["test"]; exists {
					var out []struct {
						name    string
						healthy bool
					}
					for _, rte := range *state.routes {
						out = append(out, struct {
							name    string
							healthy bool
						}{name: rte.address, healthy: rte.healthy.Load()})
					}
					t.Logf("routes state at i=%d: %+v", i, out)
				}
				<-time.After(time.Millisecond * 200)
			}

			// destroy servers after 50 requests dispatched
			if i == 50 {
				if len(windDownChan) > 0 {
				destroy:
					for {
						select {
						case canceler := <-windDownChan:
							canceler()
							t.Logf("upstream server shutdown")
							break
						default:
							// No action... no need to wind down anything, just skipping
							break destroy
						}
					}
				}
			}

			// on 100th and following iteration try to restore servers during the load
			if i > 100 && i%20 == 0 {
				if len(scaleUpChan) > 0 {
					select {
					case restoreSvc := <-scaleUpChan:
						canceler, err := restoreSvc()
						if err != nil {
							t.Errorf("server cannot come up")
						}
						t.Logf("server coming up")
						wipeCleanChan <- canceler
						break
					default:
						// No action... no need to wind down anything, just skipping
					}
				}
			}

			go func() {
				// Test load balancer
				res, err := httputil.SendTestRequest("https://localhost:9094/api")
				if err != nil {
					// t.Logf("cannot reach remotes, error: %+v", err)
				}
				responses <- res
			}()
		}
	}()

	// Listen for all threads
	err = balancer.Listen()
	if err != nil {
		t.Errorf("listen returned error: %+v", err)
	}

run:
	for {
		select {
		case closer := <-wipeCleanChan:
			closer()
		default:
			break run
		}
	}

	// Provide some stats
	responded := [4]int{0, 0, 0, 0}
	respondedAfterScaleUp := [4]int{0, 0, 0, 0}
	for res := range responses {
		if strings.Contains(res, "1") {
			responded[0]++
			if strings.Contains(res, "scale") {
				respondedAfterScaleUp[0]++
			}
		} else if strings.Contains(res, "2") {
			responded[1]++
			if strings.Contains(res, "scale") {
				respondedAfterScaleUp[1]++
			}
		} else if strings.Contains(res, "3") {
			responded[2]++
			if strings.Contains(res, "scale") {
				respondedAfterScaleUp[2]++
			}
		} else if strings.Contains(res, "4") {
			responded[3]++
			if strings.Contains(res, "scale") {
				respondedAfterScaleUp[3]++
			}
		}
		if len(responses) == 0 {
			break
		}
	}

	if responded[0] == 0 || responded[1] == 0 || responded[2] == 0 || responded[3] == 0 {
		t.Errorf("one of the servers was not selected by the strategy")
	}
	if respondedAfterScaleUp[0] == 0 || respondedAfterScaleUp[1] == 0 || respondedAfterScaleUp[2] == 0 || respondedAfterScaleUp[3] == 0 {
		t.Errorf("one of the servers was not reinstantiated by the health-check service")
	}
	t.Logf("Servers responded total   1<%d times> 2<%d times> 3<%d times> 4<%d times>", responded[0], responded[1], responded[2], responded[3])
	t.Logf("Servers after scale up    1<%d times> 2<%d times> 3<%d times> 4<%d times>", respondedAfterScaleUp[0], respondedAfterScaleUp[1], respondedAfterScaleUp[2], respondedAfterScaleUp[3])

	// Let everything unwind gracefully
	<-time.After(time.Second * 5)
}

// TestRunningLoadBalancerWithRateLimit
// Will test rate limiting capabilities of LoadBalancer quota manager within a timeframe
func TestRunningLoadBalancerWithRateLimit(t *testing.T) {
	ctx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()

	t.Log("test prepare, create TLS files")
	// Prepare TLS data for the test
	err := tlsutil.CreateLocalTLSData("test")
	defer func() {
		t.Log("test unwind, delete TLS files")
		out, err := tlsutil.WipeLocalTLSData("./")
		if err != nil {
			t.Error("cannot clean pre-arranged test files")
		}
		t.Logf("files deleted: %+v", out)
	}()
	if err != nil {
		t.Fatal(err)
	}

	// Prepare responding servers for the test
	// TODO: Optimize tests in the way to obtain random ranges for ports to configure LB and test targets
	t.Log("test prepare, create responding servers")
	stopServer1, err := httputil.CreateTestServer(9095, "api", "Server 1 responded")
	if err != nil {
		t.Errorf("Failed to start test server 1: %v", err)
	}
	defer stopServer1()
	stopServer2, err := httputil.CreateTestServer(9096, "api", "Server 2 responded")
	if err != nil {
		t.Errorf("Failed to start test server 2: %v", err)
	}
	defer stopServer2()
	stopServer3, err := httputil.CreateTestServer(9097, "api", "Server 3 responded")
	if err != nil {
		t.Errorf("Failed to start test server 3: %v", err)
	}
	defer stopServer3()

	t.Log("test prepare, create loadbalancer instance")
	cert, err := os.ReadFile("server.crt")
	if err != nil {
		t.Fatal(err)
		return
	}
	key, err := os.ReadFile("server.key")
	if err != nil {
		t.Fatal(err)
		return
	}
	ca, err := os.ReadFile("ca.crt")
	if err != nil {
		t.Fatal(err)
		return
	}

	const quotaPerSecond = 10

	balancer, err := NewLoadBalancer(ctx, []ServicePool{{
		SvcIdentity:          "test",
		SvcPort:              9098,
		SvcRateQuotaTimes:    quotaPerSecond,
		SvcRateQuotaDuration: time.Second * 1,
		SvcRoutes: []ServicePoolRoute{
			ServicePoolRoute{
				ServicePath:   "localhost:9095",
				ServiceActive: true,
			},
			ServicePoolRoute{
				ServicePath:   "localhost:9096",
				ServiceActive: true,
			},
			ServicePoolRoute{
				ServicePath:   "localhost:9097",
				ServiceActive: true,
			},
		},
		Certificate: string(cert),
		CertKey:     string(key),
		CACert:      string(ca),
	}}, Options{})
	if err != nil {
		t.Fatal("cannot configure load balancer")
	}

	// Give 10 seconds for testing everything
	go func() {
		<-time.After(time.Second * 15)
		cancelAll()
	}()

	// Create senders
	responses := make(chan string, 200)

	// Create tools to calculate tick counts
	// TODO: For proper calculation in the test we need to collect all responses by time and then take ranges as following
	//   for each object like: {response, time}
	//      from the start - collect range until it fits quota (like all items within 1s)
	//           collect amount of items in such range to verify closest number of requests approved by quota
	lastResponseCount := 0
	tickCounts := make([]int, 0)

	// Currently test strategy is to approximately calculate that there were +/- 10 requests per second
	go func() {

		// Give 5 seconds for everything to startup
		<-time.After(time.Second * 5)

		// Define ticker to collect the data
		ticker := time.NewTicker(time.Second * 1)
		defer ticker.Stop()

		for i := 0; i < 200; i++ {
			select {
			case <-ticker.C:
				// Calculate new successful responses minus previous responses and place in a bucket
				newCount := len(responses)
				tickCounts = append(tickCounts, newCount-lastResponseCount)
				lastResponseCount = len(responses)
			default:
				// Spawn requests in batches of 30 per second
				if i%30 == 0 {
					<-time.After(time.Millisecond * 1000)
				}
				go func() {
					// Test load balancer
					res, err := httputil.SendTestRequest("https://localhost:9098/api")
					if err != nil {
						// t.Errorf("cannot reach remotes, error: %+v", err)
						return
					}
					// Collect response from server
					responses <- res
				}()
			}
		}
	}()

	// Listen for all threads
	err = balancer.Listen()
	if err != nil {
		t.Errorf("listen returned error: %+v", err)
	}

	responded := [3]int{0, 0, 0}
	for res := range responses {
		if strings.Contains(res, "1") {
			responded[0]++
		} else if strings.Contains(res, "2") {
			responded[1]++
		} else if strings.Contains(res, "3") {
			responded[2]++
		}
		if len(responses) == 0 {
			break
		}
	}

	if responded[0] == 0 || responded[1] == 0 || responded[2] == 0 {
		t.Errorf("one of the servers was not selected by the strategy")
	}
	t.Logf("Servers responded 1<%d times> 2<%d times> 3<%d times>", responded[0], responded[1], responded[2])

	for i, responseBucket := range tickCounts {
		if responseBucket > quotaPerSecond {
			t.Errorf("in test tick: %d quota exceeded: %d actual was: %d", i, quotaPerSecond, responseBucket)
		}
	}
	t.Logf("rate quota per 1s tick of the test timeline was: %v", tickCounts)

	// Let everything unwind gracefully
	<-time.After(time.Second * 5)
}
