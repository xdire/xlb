package tlsutil

import (
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
)

type TLSBundle struct {
	Certificate tls.Certificate
	PublicKey   crypto.PublicKey
	PrivateKey  crypto.PrivateKey
	CertPool    *x509.CertPool
}

func FromPKI(certificate string, privateKey string) (*TLSBundle, error) {
	trimC := strings.Trim(certificate, " ")
	trimK := strings.Trim(privateKey, " ")
	cert, err := tls.X509KeyPair([]byte(trimC), []byte(trimK))
	if err != nil {
		// Try possible flattening in PKI credential
		normalizedCert := stringToPEMFormat(trimC)
		normalizedKey := stringToPEMFormat(trimK)
		cert, err = tls.X509KeyPair([]byte(normalizedCert), []byte(normalizedKey))
		if err != nil {
			return nil, fmt.Errorf("cannot load certificate, error %w", err)
		}
		return FromPKICert(&cert)
	}
	return FromPKICert(&cert)
}

func FromPKICert(cert *tls.Certificate) (*TLSBundle, error) {
	var err error
	// Create Leaf form of certificate
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("cannot load ceritifcate, error: %w", err)
	}
	config := &TLSBundle{
		Certificate: *cert,
		PublicKey:   cert.Leaf.PublicKey,
		CertPool:    x509.NewCertPool(),
		PrivateKey:  cert.PrivateKey,
	}
	config.CertPool.AddCert(cert.Leaf)
	return config, nil
}

// Re-format PEM string which might lost format during the data conversions
func stringToPEMFormat(pem string) string {
	spaced := strings.Split(pem, " ")
	nline := strings.Join(spaced, "\n")
	res := strings.ReplaceAll(nline, "-----BEGIN\nCERTIFICATE-----", "-----BEGIN CERTIFICATE-----")
	res = strings.ReplaceAll(res, "-----END\nCERTIFICATE-----", "-----END CERTIFICATE-----")
	res = strings.ReplaceAll(res, "-----BEGIN\nRSA\nPRIVATE\nKEY-----", "-----BEGIN RSA PRIVATE KEY-----")
	res = strings.ReplaceAll(res, "-----END\nRSA\nPRIVATE\nKEY-----", "-----END RSA PRIVATE KEY-----")
	return res
}
