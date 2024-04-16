package tlsutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func CreateLocalTLSData(identityName string) error {

	if len(identityName) == 0 {
		identityName = "test"
	}

	caKey := exec.Command("openssl", "genrsa", "-out", "ca.key", "2048")
	err := caKey.Run()
	if err != nil {
		return err
	}
	caCert := exec.Command("openssl", "req", "-x509", "-new", "-nodes", "-key", "ca.key", "-sha256", "-days", "1", "-out", "ca.crt", `-subj`, `/C=US/ST=California/L=Alameda/O=XLB`)
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

	srvCSR := exec.Command("openssl", "req", "-new", "-key", "server.key", "-out", "server.csr", "-subj", "/C=US/ST=California/L=Alameda/O=XLB")
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

	clientCSR := exec.Command("openssl", "req", "-new", "-key", "client.key", "-out", "client.csr", "-subj", "/C=US/ST=California/L=San Francisco/O=Client Company/CN="+identityName, "-addext", "subjectAltName=DNS:localhost")
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

func WipeLocalTLSData(dirPath string) ([]string, error) {
	ext := []string{".key", ".srl", ".crt", ".csr", ".txt"}
	out := make([]string, 0)
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
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
						out = append(out, fmt.Sprintf("%s", path))
					}
					break
				}
			}
		}

		return nil
	})

	if err != nil {
		return out, fmt.Errorf("deletion failed, error: %w", err)
	}
	return out, nil
}
