package testing

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

func CreateTestServer(ctx context.Context, port int, path, response string) (func() error, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/"+path, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		fmt.Fprint(w, response)
	})

	srv := &http.Server{
		Addr:    ":" + strconv.Itoa(port),
		Handler: mux,
	}

	ctx, cancel := context.WithCancel(ctx)
	var err error

	go func() {
		err = srv.ListenAndServe()
		if err != nil && !errors.Is(http.ErrServerClosed, err) {
			fmt.Printf("Error starting test server on port %d: %v\n", port, err)
		}
	}()

	return func() error {
		cancel()
		return srv.Shutdown(ctx)
	}, err
}

func SendRequest(url string) (string, error) {
	// Load certificate and key from a folder
	certFile := "client.crt"
	keyFile := "client.key"
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		fmt.Println("Error loading certificate and key:", err)
		return "", err
	}

	caData, err2 := os.ReadFile("ca.crt")
	if err2 != nil {
		return "", fmt.Errorf("cannot read ca certificate for sending the request, error: %w", err)
	}

	// Configure TLS client
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caData)
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
	}

	// Create an HTTP client with the custom TLS configuration
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
			// Ask to close the connections immediately
			DisableKeepAlives: true,
			// IdleConnTimeout:   time.Second * 1,
		},
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating HTTP request:", err)
		return "", err
	}

	// client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending HTTPS request:", err)
		return "", err
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Println("cannot close response body, error", err)
		}
	}(resp.Body)

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response body:", err)
		return "", err
	}
	return string(body), nil
}