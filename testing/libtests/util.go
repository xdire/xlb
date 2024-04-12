package libtests

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/xdire/xlb"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type TestServicePoolConfig struct {
	SvcIdentity          string
	SvcPort              int
	SvcRateQuotaTimes    int
	SvcRateQuotaDuration time.Duration
	SvcRoutes            []xlb.Route
	Certificate          string
	CertKey              string
}

func (t TestServicePoolConfig) GetCertificate() string {
	return t.Certificate
}

func (t TestServicePoolConfig) GetPrivateKey() string {
	return t.CertKey
}

func (t TestServicePoolConfig) Identity() string {
	return t.SvcIdentity
}

func (t TestServicePoolConfig) Port() int {
	return t.SvcPort
}

func (t TestServicePoolConfig) RateQuota() (int, time.Duration) {
	return t.SvcRateQuotaTimes, t.SvcRateQuotaDuration
}

func (t TestServicePoolConfig) Routes() []xlb.Route {
	return t.SvcRoutes
}

func (t TestServicePoolConfig) TLSData() xlb.TLSData {
	return t
}

func (t TestServicePoolConfig) UnauthorizedAttempts() int {
	//TODO implement me
	panic("implement me")
}

func (t TestServicePoolConfig) HealthCheckValidations() int {
	//TODO implement me
	panic("implement me")
}

func (t TestServicePoolConfig) RouteTimeout() time.Duration {
	return time.Second * 30
}

type TestServicePoolRoute struct {
	ServicePath   string
	ServiceActive bool
}

func (t TestServicePoolRoute) Path() string {
	return t.ServicePath
}

func (t TestServicePoolRoute) Active() bool {
	return t.ServiceActive
}

func createLocalTLSData() error {

	caKey := exec.Command("openssl", "genrsa", "-out", "ca.key", "2048")
	err := caKey.Run()
	if err != nil {
		return err
	}
	caCert := exec.Command("openssl", "req", "-x509", "-new", "-nodes", "-key", "ca.key", "-sha256", "-days", "1", "-out", "ca.crt", `-subj`, `/C=US/ST=California/L=Alameda/O=XLB/CN=TESTCA`)
	err = caCert.Start()
	if err != nil {
		return fmt.Errorf("cannot generate caCert, error: %w", err)
	}
	err = caCert.Wait()
	if err != nil {
		return err
	}

	// Server TLS
	srvKey := exec.Command("openssl", "genrsa", "-out", "server.key", "2048")
	err = srvKey.Start()
	if err != nil {
		return err
	}
	err = srvKey.Wait()
	if err != nil {
		return err
	}

	srvCSR := exec.Command("openssl", "req", "-new", "-key", "server.key", "-out", "server.csr", "-subj", "/C=US/ST=California/L=Alameda/O=XLB", "-addext", "subjectAltName=DNS:localhost")
	err = srvCSR.Start()
	if err != nil {
		return err
	}
	err = srvCSR.Wait()
	if err != nil {
		return err
	}

	if err := os.WriteFile("server_san.txt", []byte("subjectAltName=DNS:localhost"), 0666); err != nil {
		return err
	}

	srvCert := exec.Command("openssl", "x509", "-req", "-in", "server.csr", "-CA", "ca.crt", "-CAkey", "ca.key", "-CAcreateserial", "-out", "server.crt", "-days", "1", "-sha256", "-extfile", "server_san.txt")
	err = srvCert.Start()
	if err != nil {
		return err
	}
	err = srvCert.Wait()
	if err != nil {
		return err
	}

	// Client TLS
	clientKey := exec.Command("openssl", "genrsa", "-out", "client.key", "2048")
	err = clientKey.Start()
	if err != nil {
		return err
	}
	err = clientKey.Wait()
	if err != nil {
		return err
	}

	clientCSR := exec.Command("openssl", "req", "-new", "-key", "client.key", "-out", "client.csr", "-subj", "/C=US/ST=California/L=San Francisco/O=Client Company/CN=test", "-addext", "subjectAltName=DNS:localhost")
	err = clientCSR.Start()
	if err != nil {
		return err
	}
	err = clientCSR.Wait()
	if err != nil {
		return err
	}

	clientCert := exec.Command("openssl", "x509", "-req", "-in", "client.csr", "-CA", "ca.crt", "-CAkey", "ca.key", "-CAcreateserial", "-out", "client.crt", "-days", "1", "-sha256")
	err = clientCert.Start()
	if err != nil {
		return err
	}
	err = clientCert.Wait()
	if err != nil {
		return err
	}
	return nil
}

func wipeLocalTLSData(dirPath string) error {
	ext := []string{".key", ".srl", ".crt", ".csr"}

	return filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			for _, e := range ext {
				if strings.HasSuffix(info.Name(), e) {
					err := os.Remove(path)
					if err != nil {
						fmt.Printf("Error deleting file %s: %v\n", path, err)
					} else {
						fmt.Printf("Deleted file: %s\n", path)
					}
					break
				}
			}
		}

		return nil
	})
}

func createTestServer(port int, path, response string) (func() error, error) {
	srv := &http.Server{Addr: ":" + strconv.Itoa(port)}

	http.HandleFunc("/"+path, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, response)
	})

	ctx, cancel := context.WithCancel(context.Background())
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

func sendRequest(url string) (string, error) {
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
	//caCert, err3 := x509.ParseCertificate(caData)
	//if err3 != nil {
	//	return "", fmt.Errorf("cannot decode ca certificate for sending the request, error: %w", err)
	//}

	// Configure TLS client
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caData)
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            pool,
		InsecureSkipVerify: false,
	}

	// Create an HTTP client with the custom TLS configuration
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	// Send HTTPS request
	// url := "https://localhost:9090"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Println("Error creating HTTP request:", err)
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending HTTPS request:", err)
		return "", err
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Error reading response body:", err)
		return "", err
	}

	return string(body), nil
}
