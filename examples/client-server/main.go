package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/xdire/xlb"
	testUtil "github.com/xdire/xlb/testing"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {

	ctx, cancelAll := context.WithCancel(context.Background())
	defer cancelAll()
	signals(cancelAll)

	// Flags for pre-generate TLS data and identity
	identity := flag.String("id", "test", "Identity name to match for certificates")
	optionGenTLS := flag.Bool("gentls", false, "Generate TLS data for all parts of the chain to run")
	optionWipeTLS := flag.Bool("wipetls", false, "Wipe TLS data in the folder")

	// Flags for launching the load balancer service
	optionLoadBalancer := flag.Bool("start-lb", false, "Launch the load balancer service")
	port := flag.Int("port", 9089, "Port for the load balancer service")
	rateQuota := flag.Int("rate-quota", 10, "Rate quota for the load balancer")
	rateDuration := flag.Duration("rate-duration", time.Second, "Rate duration for the load balancer")
	serverCert := flag.String("server-cert", "server.crt", "Server certificate file for the load balancer")
	serverKey := flag.String("server-key", "server.key", "Server key file for the load balancer")
	routesStr := flag.String("routes", "", "Routes for the load balancer in the format 'localhost:9081,localhost:9082,localhost:9083'")

	// Flag for launching the SDK client
	optionClient := flag.Bool("send-client", false, "Launch the client to reach servers behind the load balancer")
	optionClientThreads := flag.Int("threads", 3, "How many parallel clients should be there trying to reach the servers behind the LB")

	// Flag for launching test servers
	optionTestServers := flag.Bool("start-test-servers", false, "Launch test servers to be hidden behind the load balancer, must be in conjunction with --routes xxx:port,xxx:port")

	flag.Parse()

	// Prepare routes from flags
	routes := strings.Split(*routesStr, ",")
	pRoutes := make([]xlb.Route, 0)
	for _, rte := range routes {
		if len(rte) == 0 {
			continue
		}
		pRoutes = append(pRoutes, testUtil.TestServicePoolRoute{
			ServicePath:   rte,
			ServiceActive: true,
		})
	}

	// Generate TLS data in folder
	if *optionGenTLS {
		err := testUtil.CreateLocalTLSData(*identity)
		if err != nil {
			log.Fatalf("cannot generate TLS data, error: %+v", err)
		}
	}

	// Clean TLS data in folder
	if *optionWipeTLS {
		removed, err := testUtil.WipeLocalTLSData("./")
		if err != nil {
			log.Fatalf("cannot generate TLS data, error: %+v", err)
		}
		log.Printf("files deleted: %+v", removed)
	}

	// Launch load balancer using provided options
	if *optionLoadBalancer {

		if len(pRoutes) == 0 {
			log.Fatalf("no routes provided")
			return
		}

		cert, err := os.ReadFile(*serverCert)
		if err != nil {
			log.Fatalf("server certificate not found")
			return
		}

		key, err := os.ReadFile(*serverKey)
		if err != nil {
			log.Fatalf("server key not found")
			return
		}

		balancer, err := xlb.NewLoadBalancer(ctx, []xlb.ServicePool{testUtil.TestServicePoolConfig{
			SvcIdentity:          "test",
			SvcPort:              *port,
			SvcRateQuotaTimes:    *rateQuota,
			SvcRateQuotaDuration: *rateDuration,
			SvcRoutes:            pRoutes,
			Certificate:          string(cert),
			CertKey:              string(key),
		}}, xlb.Options{LogLevel: "debug"})
		if err != nil {
			return
		}

		// Listen for all threads
		err = balancer.Listen()
		if err != nil {
			log.Fatalf("listen returned error: %+v", err)
			return
		}
	}

	// Launch the client to send the message
	if *optionClient {
		launchClient(*port, *optionClientThreads)
	}

	// Launch the test servers
	if *optionTestServers {

		if len(pRoutes) == 0 {
			log.Fatalf("no routes provided")
			return
		}

		err := launchTestServers(ctx, pRoutes)
		if err != nil {
			fmt.Printf("Error launching test servers: %v\n", err)
		}
	}
}

func launchClient(targetPort int, threads int) {
	// Test load balancer
	wg := sync.WaitGroup{}
	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			// Test load balancer
			res, err := testUtil.SendRequest("https://localhost:" + strconv.Itoa(targetPort) + "/api")
			if err != nil {
				log.Printf(fmt.Errorf("cannot reach remotes, error: %w", err).Error())
			} else {
				log.Println(res)
			}
			wg.Done()
		}()
	}
	wg.Wait()
}

func launchTestServers(ctx context.Context, routes []xlb.Route) error {
	// Define context stop logic
	stopContextFn := make([]func() error, 0)
	defer func() {
		for _, fn := range stopContextFn {
			err := fn()
			if err != nil {
				log.Println("cannot close context for test server")
				continue
			}
			log.Println("closing server context")
		}
	}()

	for i, rte := range routes {
		hostUri := strings.Split(rte.Path(), ":")
		if len(hostUri) < 2 {
			return fmt.Errorf("cannot parse port from %s", hostUri)
		}
		port, err := strconv.Atoi(hostUri[1])
		if err != nil {
			return fmt.Errorf("cannot detect port in %s", err)
		}
		log.Println("starting test server at port", port)
		stopServerFn, err := testUtil.CreateTestServer(ctx, port, "api", fmt.Sprintf("Server %d responded", i))
		if err != nil {
			log.Fatalf("Failed to start test server %d: %+v", i, err)
		}
		stopContextFn = append(stopContextFn, stopServerFn)
	}
	<-ctx.Done()
	return nil
}

func signals(cancelFunc context.CancelFunc) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGABRT)
	go func() {
		sig := <-sigCh
		log.Printf("\nexited with signal %v", sig)
		cancelFunc()
	}()
}
