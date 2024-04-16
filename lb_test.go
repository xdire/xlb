package xlb

import (
	"context"
	httputil "github.com/xdire/xlb/httputil"
	"github.com/xdire/xlb/tlsutil"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

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

	balancer, err := NewLoadBalancer(ctx, []ServicePool{ServicePoolConfig{
		SvcIdentity:          "test",
		SvcPort:              9089,
		SvcRateQuotaTimes:    10,
		SvcRateQuotaDuration: time.Second * 1,
		SvcRoutes: []Route{
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
	}}, Options{})
	if err != nil {
		t.Fatal("cannot configure load balancer")
	}

	// Give 5 seconds for testing everything
	go func() {
		<-time.After(time.Second * 5)
		cancelAll()
	}()

	// Create senders
	responses := make(chan string, 100)
	go func() {
		<-time.After(time.Second * 1)
		for i := 0; i < 100; i++ {
			// Spawn requests in batches
			if i%10 == 0 {
				time.Sleep(time.Millisecond * 100)
			}
			go func() {
				// Test load balancer
				res, err := httputil.SendTestRequest("https://localhost:9089/api")
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

}

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
	}
	key, err := os.ReadFile("server.key")
	if err != nil {
		t.Fatal(err)
	}

	// Create pool with the single host and add rest of the hosts during the
	// requests coming concurrently
	servicePool := ServicePoolConfig{
		SvcIdentity:          "test",
		SvcPort:              9089,
		SvcRateQuotaTimes:    10,
		SvcRateQuotaDuration: time.Second * 1,
		SvcRoutes: []Route{
			ServicePoolRoute{
				ServicePath:   "localhost:9081",
				ServiceActive: true,
			},
		},
		Certificate: string(cert),
		CertKey:     string(key),
	}

	balancer, err := NewLoadBalancer(ctx, []ServicePool{servicePool}, Options{LogLevel: "error"})
	if err != nil {
		t.Fatal("cannot configure load balancer")
	}

	// Give 5 seconds for testing everything
	go func() {
		<-time.After(time.Second * 5)
		cancelAll()
	}()

	// Create senders
	responses := make(chan string, 100)

	go func() {
		<-time.After(time.Second * 1)
		nextPort := 9081
		for i := 0; i < 100; i++ {

			// Spawn requests in batches
			if i%10 == 0 {
				<-time.After(time.Millisecond * 100)
			}

			// each 20th request hot reload the routes, adding more servers to route to
			if i > 0 && i%20 == 0 && nextPort < 9083 {
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
				res, err := httputil.SendTestRequest("https://localhost:9089/api")
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

}

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

	// Order to switch servers offline
	windDownChan := make(chan func() error, 3)
	windDownChan <- stopServer1
	windDownChan <- stopServer2
	windDownChan <- stopServer3

	// Order to bring servers back online
	scaleUpChan := make(chan func() (func() error, error), 3)
	scaleUpChan <- func() (func() error, error) {
		return httputil.CreateTestServer(9081, "api", "Server 1 after scale")
	}
	scaleUpChan <- func() (func() error, error) {
		return httputil.CreateTestServer(9082, "api", "Server 2 after scale")
	}
	scaleUpChan <- func() (func() error, error) {
		return httputil.CreateTestServer(9083, "api", "Server 3 after scale")
	}

	wipeCleanChan := make(chan func() error, 3)

	t.Log("test prepare, create loadbalancer instance")
	cert, err := os.ReadFile("server.crt")
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile("server.key")
	if err != nil {
		t.Fatal(err)
	}

	// Create pool with the single host and add rest of the hosts during the
	// requests coming concurrently
	servicePool := ServicePoolConfig{
		SvcIdentity:          "test",
		SvcPort:              9089,
		SvcRateQuotaTimes:    10,
		SvcRateQuotaDuration: time.Second * 1,
		SvcRoutes: []Route{
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
		Certificate:                string(cert),
		CertKey:                    string(key),
		SvcHealthCheckRescheduleMs: 1000,
		SvcRouteTimeout:            time.Second * 1,
	}

	balancer, err := NewLoadBalancer(ctx, []ServicePool{servicePool}, Options{LogLevel: "info"})
	if err != nil {
		t.Fatal("cannot configure load balancer")
	}

	// Give 5 seconds for testing everything (a little more time here as additional reloads mid-scrjpt)
	go func() {
		<-time.After(time.Second * 7)
		cancelAll()
	}()

	// Create senders
	responses := make(chan string, 200)

	go func() {
		<-time.After(time.Second * 1)
		for i := 0; i < 200; i++ {

			// Each 10th request kill a server until nothing to kill
			if i%20 == 0 {
				<-time.After(time.Millisecond * 200)
				select {
				case canceler := <-windDownChan:
					canceler()
					t.Logf("upstream server shutdown")
					break
				default:
					// No action... no need to wind down anything, just skipping
				}
			}

			// each 30th request try to restore some servers
			if i > 0 && i%40 == 0 {
				select {
				case restoreSvc := <-scaleUpChan:
					canceler, err := restoreSvc()
					if err != nil {
						t.Errorf("server cannot come up")
					}
					t.Logf("server coming up")
					wipeCleanChan <- canceler
					<-time.After(time.Millisecond * 200)
					break
				default:
					// No action... no need to wind down anything, just skipping
				}
			}

			go func() {
				// Test load balancer
				res, err := httputil.SendTestRequest("https://localhost:9089/api")
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
	responded := [3]int{0, 0, 0}
	respondedAfterScaleUp := [3]int{0, 0, 0}
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
		}
		if len(responses) == 0 {
			break
		}
	}

	if responded[0] == 0 || responded[1] == 0 || responded[2] == 0 {
		t.Errorf("one of the servers was not selected by the strategy")
	}
	if respondedAfterScaleUp[0] == 0 || respondedAfterScaleUp[1] == 0 || respondedAfterScaleUp[2] == 0 {
		t.Errorf("one of the servers was not reinstantiated by the health-check service")
	}
	t.Logf("Servers responded total   1<%d times> 2<%d times> 3<%d times>", responded[0], responded[1], responded[2])
	t.Logf("Servers after scale up    1<%d times> 2<%d times> 3<%d times>", respondedAfterScaleUp[0], respondedAfterScaleUp[1], respondedAfterScaleUp[2])
}
