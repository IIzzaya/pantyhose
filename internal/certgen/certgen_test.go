package certgen

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
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

	expectedFiles := []string{"ca.crt", "ca.key", "server.crt", "server.key", "client.pem"}
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

	// Verify client.pem contains 3 PEM blocks: CA cert, client cert, client key
	clientPEM, err := os.ReadFile(files.ClientPEM)
	if err != nil {
		t.Fatal(err)
	}

	var blocks []*pem.Block
	rest := clientPEM
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		blocks = append(blocks, block)
	}
	if len(blocks) != 3 {
		t.Fatalf("client.pem should contain 3 PEM blocks, got %d", len(blocks))
	}
	if blocks[0].Type != "CERTIFICATE" {
		t.Errorf("block 0 should be CERTIFICATE, got %s", blocks[0].Type)
	}
	if blocks[1].Type != "CERTIFICATE" {
		t.Errorf("block 1 should be CERTIFICATE, got %s", blocks[1].Type)
	}
	if blocks[2].Type != "EC PRIVATE KEY" {
		t.Errorf("block 2 should be EC PRIVATE KEY, got %s", blocks[2].Type)
	}

	// First cert in client.pem should be the CA cert
	pemCACert, err := x509.ParseCertificate(blocks[0].Bytes)
	if err != nil {
		t.Fatalf("parse CA cert from client.pem: %v", err)
	}
	if !pemCACert.IsCA {
		t.Error("first cert in client.pem should be CA")
	}

	// Second cert should be the client cert, signed by CA
	clientCert, err := x509.ParseCertificate(blocks[1].Bytes)
	if err != nil {
		t.Fatalf("parse client cert from client.pem: %v", err)
	}
	if _, err := clientCert.Verify(x509.VerifyOptions{Roots: caPool}); err != nil {
		t.Errorf("client cert not signed by CA: %v", err)
	}

	// Verify server cert
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

func TestGenerateCAFingerprint(t *testing.T) {
	dir := t.TempDir()
	files, err := Generate(dir, []string{"127.0.0.1"}, 30)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if files.CAFingerprint == "" {
		t.Fatal("CAFingerprint should not be empty")
	}

	if len(files.CAFingerprint) != 8 {
		t.Errorf("CAFingerprint should be 8 hex chars, got %d: %q", len(files.CAFingerprint), files.CAFingerprint)
	}

	// Regenerating with different keys should produce a different fingerprint.
	dir2 := t.TempDir()
	files2, err := Generate(dir2, []string{"127.0.0.1"}, 30)
	if err != nil {
		t.Fatalf("Generate (second) failed: %v", err)
	}

	if files.CAFingerprint == files2.CAFingerprint {
		t.Error("different CA keys should produce different fingerprints")
	}
}

func TestGenerateConsistentFingerprint(t *testing.T) {
	dir := t.TempDir()
	files, err := Generate(dir, []string{"127.0.0.1"}, 30)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	caCertPEM, err := os.ReadFile(files.CACert)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(caCertPEM)
	if block == nil {
		t.Fatal("failed to decode CA cert PEM")
	}

	h := sha256.Sum256(block.Bytes)
	expected := fmt.Sprintf("%x", h[:4])

	if files.CAFingerprint != expected {
		t.Errorf("fingerprint mismatch: got %q, want %q", files.CAFingerprint, expected)
	}
}
