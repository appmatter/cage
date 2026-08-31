package network

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CADir is .cage/.cache/ca under the working tree.
func CADir(projectRoot string) string {
	if projectRoot == "" {
		projectRoot = "."
	}
	return filepath.Join(projectRoot, ".cage", ".cache", "ca")
}

// MITM is a working-tree CA that mints leaf certs for TLS break/re-encrypt.
type MITM struct {
	caCert *x509.Certificate
	caKey  *rsa.PrivateKey
	pem    []byte // public CA PEM for guest install

	mu    sync.Mutex
	cache map[string]tls.Certificate
}

// LoadOrCreateCA loads .cage/.cache/ca or generates a new CA once.
func LoadOrCreateCA(projectRoot string) (*MITM, error) {
	dir := CADir(projectRoot)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	certPath := filepath.Join(dir, "ca.pem")
	keyPath := filepath.Join(dir, "ca.key")
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return loadCA(certPath, keyPath)
		}
	}
	return createCA(certPath, keyPath)
}

func loadCA(certPath, keyPath string) (*MITM, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, fmt.Errorf("mitm: invalid ca.pem")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	kblock, _ := pem.Decode(keyPEM)
	if kblock == nil {
		return nil, fmt.Errorf("mitm: invalid ca.key")
	}
	key, err := x509.ParsePKCS1PrivateKey(kblock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("mitm: parse ca.key: %w", err)
	}
	return &MITM{caCert: cert, caKey: key, pem: certPEM, cache: map[string]tls.Certificate{}}, nil
}

func createCA(certPath, keyPath string) (*MITM, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{Organization: []string{"Cage MITM"}, CommonName: "Cage MITM CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return nil, err
	}
	return &MITM{caCert: cert, caKey: key, pem: certPEM, cache: map[string]tls.Certificate{}}, nil
}

// CAPEM returns the public CA certificate PEM.
func (m *MITM) CAPEM() []byte {
	if m == nil {
		return nil
	}
	return m.pem
}

// LeafForHost returns a TLS certificate for host (DNS or IP), signed by the CA.
func (m *MITM) LeafForHost(host string) (tls.Certificate, error) {
	if m == nil {
		return tls.Certificate{}, fmt.Errorf("mitm: nil")
	}
	host = stripHostPort(host)
	if host == "" {
		return tls.Certificate{}, fmt.Errorf("mitm: empty host")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.cache[host]; ok {
		return c, nil
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, m.caCert, &key.PublicKey, m.caKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf := tls.Certificate{
		Certificate: [][]byte{der, m.caCert.Raw},
		PrivateKey:  key,
	}
	m.cache[host] = leaf
	return leaf, nil
}

func stripHostPort(host string) string {
	h, _, err := net.SplitHostPort(host)
	if err == nil {
		return h
	}
	return host
}
