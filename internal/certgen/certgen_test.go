package certgen

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerate(t *testing.T) {
	dir := t.TempDir()
	files, err := Generate(dir, []string{"127.0.0.1", "10.0.0.1", "proxy.example.com"}, 365)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	expectedFiles := []string{"ca.crt", "ca.key", "server.crt", "server.key", "client.crt", "client.key"}
	for _, name := range expectedFiles {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected file %s: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("file %s is empty", name)
		}
	}

	caCertPEM, err := os.ReadFile(files.CACert)
	if err != nil {
		t.Fatal(err)
	}
	caBlock, _ := pem.Decode(caCertPEM)
	if caBlock == nil {
		t.Fatal("failed to decode CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if !caCert.IsCA {
		t.Error("CA cert should have IsCA=true")
	}

	caPool := x509.NewCertPool()
	caPool.AddCert(caCert)

	if _, err := tls.LoadX509KeyPair(files.ServerCert, files.ServerKey); err != nil {
		t.Fatalf("server cert/key pair invalid: %v", err)
	}

	if _, err := tls.LoadX509KeyPair(files.ClientCert, files.ClientKey); err != nil {
		t.Fatalf("client cert/key pair invalid: %v", err)
	}

	serverCertPEM, err := os.ReadFile(files.ServerCert)
	if err != nil {
		t.Fatal(err)
	}
	serverBlock, _ := pem.Decode(serverCertPEM)
	serverCert, err := x509.ParseCertificate(serverBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := serverCert.Verify(x509.VerifyOptions{Roots: caPool}); err != nil {
		t.Errorf("server cert not signed by CA: %v", err)
	}

	clientCertPEM, err := os.ReadFile(files.ClientCert)
	if err != nil {
		t.Fatal(err)
	}
	clientBlock, _ := pem.Decode(clientCertPEM)
	clientCert, err := x509.ParseCertificate(clientBlock.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := clientCert.Verify(x509.VerifyOptions{Roots: caPool}); err != nil {
		t.Errorf("client cert not signed by CA: %v", err)
	}

	found127 := false
	for _, ip := range serverCert.IPAddresses {
		if ip.String() == "127.0.0.1" {
			found127 = true
		}
	}
	if !found127 {
		t.Error("server cert missing 127.0.0.1 in IPAddresses")
	}

	foundDNS := false
	for _, name := range serverCert.DNSNames {
		if name == "proxy.example.com" {
			foundDNS = true
		}
	}
	if !foundDNS {
		t.Error("server cert missing proxy.example.com in DNSNames")
	}
}

func TestGenerateDefaultHosts(t *testing.T) {
	dir := t.TempDir()
	files, err := Generate(dir, nil, 30)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	serverCertPEM, err := os.ReadFile(files.ServerCert)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(serverCertPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(cert.IPAddresses) == 0 {
		t.Error("server cert should have at least 127.0.0.1 when no hosts specified")
	}
}
