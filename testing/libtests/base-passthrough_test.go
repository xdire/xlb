package libtests

import (
	"context"
	"fmt"
	"github.com/xdire/xlb"
	"os"
	"testing"
	"time"
)

func TestRunningLoadBalancerAsLib(t *testing.T) {
	// Prepare TLS data for the test
	err := createLocalTLSData()
	defer func() {
		err := wipeLocalTLSData("./")
		if err != nil {
			t.Error("cannot clean pre-arranged test files")
		}
	}()
	if err != nil {
		t.Fatal(err)
	}
	// Prepare responding servers for the test
	stopServer1, err := createTestServer(9081, "api1", "Server 1 responded")
	if err != nil {
		t.Errorf("Failed to start test server 1: %v", err)
	}
	defer stopServer1()
	stopServer2, err := createTestServer(9082, "api2", "Server 2 responded")
	if err != nil {
		t.Errorf("Failed to start test server 1: %v", err)
	}
	defer stopServer2()
	// Prepare dispatching service for the test

	cert, err := os.ReadFile("server.crt")
	if err != nil {
		t.Error(err)
		t.Fail()
		return
	}
	key, err := os.ReadFile("server.key")
	if err != nil {
		t.Error(err)
		t.Fail()
		return
	}

	ctx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()
	balancer, err := xlb.NewLoadBalancer(ctx, []xlb.ServicePool{TestServicePoolConfig{
		SvcIdentity:          "test",
		SvcPort:              9089,
		SvcRateQuotaTimes:    10,
		SvcRateQuotaDuration: time.Second * 1,
		SvcRoutes: []xlb.Route{
			TestServicePoolRoute{
				ServicePath:   "localhost:9081/api1",
				ServiceActive: true,
			},
			TestServicePoolRoute{
				ServicePath:   "localhost:9082/api2",
				ServiceActive: true,
			},
		},
		Certificate: string(cert),
		CertKey:     string(key),
	}}, xlb.Options{})
	if err != nil {
		return
	}

	// Give 5 seconds for testing everything
	go func() {
		<-time.After(time.Second * 5)
		cancelAll()
	}()

	// Give 5 seconds for testing everything
	go func() {
		<-time.After(time.Second * 1)
		res, err := sendRequest("https://localhost:9089")
		if err != nil {
			t.Errorf("cannot reach remotes")
		}
		fmt.Printf("result: %s", res)
	}()

	// Listen for all threads
	err = balancer.Listen()
	if err != nil {
		return
	}

}
