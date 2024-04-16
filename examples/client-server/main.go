package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/xdire/xlb"
	testUtil "github.com/xdire/xlb/httputil"
	"github.com/xdire/xlb/tlsutil"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"
)

func main() {

	ctx, cancelCtx := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	defer cancelCtx()

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
	pRoutes := make([]xlb.ServicePoolRoute, 0)
	for _, rte := range routes {
		if len(rte) == 0 {
			continue
		}
		pRoutes = append(pRoutes, xlb.ServicePoolRoute{
			ServicePath:   rte,
			ServiceActive: true,
		})
	}

	// Generate TLS data in folder
	if *optionGenTLS {
		err := tlsutil.CreateLocalTLSData(*identity)
		if err != nil {
			log.Fatalf("cannot generate TLS data, error: %+v", err)
		}
	}

	// Clean TLS data in folder
	if *optionWipeTLS {
		removed, err := tlsutil.WipeLocalTLSData("./")
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
			log.Fatalf("no server certificate")
			return
		}

		key, err := os.ReadFile(*serverKey)
		if err != nil {
			log.Fatalf("no server key")
			return
		}

		balancer, err := xlb.NewLoadBalancer(ctx, []xlb.ServicePool{{
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
			defer wg.Done()
			// Test load balancer
			res, err := testUtil.SendTestRequest("https://localhost:" + strconv.Itoa(targetPort) + "/api")
			if err != nil {
				log.Printf(fmt.Errorf("cannot reach remotes, error: %w", err).Error())
			} else {
				log.Println(res)
			}
		}()
	}
	wg.Wait()
}

func launchTestServers(ctx context.Context, routes []xlb.ServicePoolRoute) error {
	// Define context stop logic
	stopContextFn := make([]func() error, len(routes))
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
		stopServerFn, err := testUtil.CreateTestServer(port, "api", fmt.Sprintf("Server %d responded", i))
		if err != nil {
			log.Fatalf("Failed to start test server %d: %+v", i, err)
		}
		stopContextFn[i] = stopServerFn
	}
	<-ctx.Done()
	return nil
}
