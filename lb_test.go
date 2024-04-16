package xlb

import (
	"context"
	testing2 "github.com/xdire/xlb/httputil"
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
	stopServer1, err := testing2.CreateTestServer(9081, "api", "Server 1 responded")
	if err != nil {
		t.Errorf("Failed to start test server 1: %v", err)
	}
	defer stopServer1()
	stopServer2, err := testing2.CreateTestServer(9082, "api", "Server 2 responded")
	if err != nil {
		t.Errorf("Failed to start test server 2: %v", err)
	}
	defer stopServer2()
	stopServer3, err := testing2.CreateTestServer(9083, "api", "Server 3 responded")
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
				res, err := testing2.SendTestRequest("https://localhost:9089/api")
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
	stopServer1, err := testing2.CreateTestServer(9081, "api", "Server 1 responded")
	if err != nil {
		t.Errorf("Failed to start test server 1: %v", err)
	}
	defer stopServer1()
	stopServer2, err := testing2.CreateTestServer(9082, "api", "Server 2 responded")
	if err != nil {
		t.Errorf("Failed to start test server 2: %v", err)
	}
	defer stopServer2()
	stopServer3, err := testing2.CreateTestServer(9083, "api", "Server 3 responded")
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
				res, err := testing2.SendTestRequest("https://localhost:9089/api")
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
