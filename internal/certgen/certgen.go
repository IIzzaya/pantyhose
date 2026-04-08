package certgen

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

type CertFiles struct {
	CACert     string
	CAKey      string
	ServerCert string
	ServerKey  string
	ClientCert string
	ClientKey  string
}

// Generate creates a self-signed CA, server certificate, and client certificate
// in the specified output directory. The CA signs both the server and client certs.
// Server cert includes 127.0.0.1 and any additional IPs/hostnames provided.
func Generate(outDir string, serverHosts []string, validDays int) (*CertFiles, error) {
	if err := os.MkdirAll(outDir, 0700); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}

	caKey, caCert, caCertDER, err := generateCA(validDays)
	if err != nil {
		return nil, fmt.Errorf("generate CA: %w", err)
	}

	serverKey, serverCertDER, err := generateSignedCert(caKey, caCert, "pantyhose-server", serverHosts, validDays)
	if err != nil {
		return nil, fmt.Errorf("generate server cert: %w", err)
	}

	clientKey, clientCertDER, err := generateSignedCert(caKey, caCert, "pantyhose-client", nil, validDays)
	if err != nil {
		return nil, fmt.Errorf("generate client cert: %w", err)
	}

	files := &CertFiles{
		CACert:     filepath.Join(outDir, "ca.crt"),
		CAKey:      filepath.Join(outDir, "ca.key"),
		ServerCert: filepath.Join(outDir, "server.crt"),
		ServerKey:  filepath.Join(outDir, "server.key"),
		ClientCert: filepath.Join(outDir, "client.crt"),
		ClientKey:  filepath.Join(outDir, "client.key"),
	}

	if err := writePEM(files.CACert, "CERTIFICATE", caCertDER); err != nil {
		return nil, err
	}
	caKeyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return nil, fmt.Errorf("marshal CA key: %w", err)
	}
	if err := writePEM(files.CAKey, "EC PRIVATE KEY", caKeyDER); err != nil {
		return nil, err
	}

	if err := writePEM(files.ServerCert, "CERTIFICATE", serverCertDER); err != nil {
		return nil, err
	}
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return nil, fmt.Errorf("marshal server key: %w", err)
	}
	if err := writePEM(files.ServerKey, "EC PRIVATE KEY", serverKeyDER); err != nil {
		return nil, err
	}

	if err := writePEM(files.ClientCert, "CERTIFICATE", clientCertDER); err != nil {
		return nil, err
	}
	clientKeyDER, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		return nil, fmt.Errorf("marshal client key: %w", err)
	}
	if err := writePEM(files.ClientKey, "EC PRIVATE KEY", clientKeyDER); err != nil {
		return nil, err
	}

	return files, nil
}

func generateCA(validDays int) (*ecdsa.PrivateKey, *x509.Certificate, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Pantyhose CA", Organization: []string{"Pantyhose"}},
		NotBefore:    time.Now().Add(-5 * time.Minute),
		NotAfter:     time.Now().Add(time.Duration(validDays) * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		IsCA:         true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, err
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, nil, err
	}

	return key, cert, certDER, nil
}

func generateSignedCert(caKey *ecdsa.PrivateKey, caCert *x509.Certificate, cn string, hosts []string, validDays int) (*ecdsa.PrivateKey, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn, Organization: []string{"Pantyhose"}},
		NotBefore:    time.Now().Add(-5 * time.Minute),
		NotAfter:     time.Now().Add(time.Duration(validDays) * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}

	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	if len(tmpl.IPAddresses) == 0 && len(tmpl.DNSNames) == 0 {
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}

	return key, certDER, nil
}

func writePEM(path, blockType string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: data})
}
