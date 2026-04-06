package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/txthinking/socks5"
)

func generateSelfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func buildClientHello(t *testing.T, sni string) []byte {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	done := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 16384)
		n, _ := serverConn.Read(buf)
		done <- buf[:n]
		serverConn.Close()
	}()

	tlsConn := tls.Client(clientConn, &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: true,
	})
	go func() {
		tlsConn.Handshake()
		tlsConn.Close()
	}()

	data := <-done
	clientConn.Close()
	return data
}

// --- SNI parser unit tests ---

func TestExtractSNI(t *testing.T) {
	tests := []struct {
		sni  string
		want string
	}{
		{"www.youtube.com", "www.youtube.com"},
		{"google.com", "google.com"},
		{"example.org", "example.org"},
		{"very-long-subdomain.cdn.example.co.uk", "very-long-subdomain.cdn.example.co.uk"},
	}
	for _, tt := range tests {
		hello := buildClientHello(t, tt.sni)
		got := extractSNI(hello)
		if got != tt.want {
			t.Errorf("extractSNI(ClientHello with SNI %q) = %q, want %q", tt.sni, got, tt.want)
		}
	}
}

func TestExtractSNIInvalidData(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"truncated header", []byte{0x16, 0x03}},
		{"HTTP request", []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")},
		{"wrong content type", []byte{0x17, 0x03, 0x01, 0x00, 0x05, 0x01, 0x00, 0x00, 0x01, 0x00}},
		{"too short", []byte{0x16, 0x03, 0x01, 0x00, 0x01, 0x01}},
	}
	for _, tt := range cases {
		sni := extractSNI(tt.data)
		if sni != "" {
			t.Errorf("extractSNI(%s) = %q, want empty", tt.name, sni)
		}
	}
}

// --- SNI remap integration tests ---

func TestSNIRemapHandlerTLS(t *testing.T) {
	cert := generateSelfSignedCert(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "pong-tls")
	})
	tlsListener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tlsListener.Close()
	srv := &http.Server{Handler: mux}
	go srv.Serve(tlsListener)
	defer srv.Close()

	_, tlsPortStr, _ := net.SplitHostPort(tlsListener.Addr().String())

	proxyPort := freePort(t)
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxyPort)
	server, err := socks5.NewClassicServer(proxyAddr, "127.0.0.1", "", "", 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	handler := &SNIRemapHandler{TCPTimeout: 10, UDPTimeout: 10, IPv4Only: false}
	go server.ListenAndServe(handler)
	defer server.Shutdown()
	time.Sleep(200 * time.Millisecond)

	client, err := socks5.NewClient(proxyAddr, "", "", 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return client.Dial(network, addr)
			},
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}
	resp, err := httpClient.Get(fmt.Sprintf("https://127.0.0.1:%s/ping", tlsPortStr))
	if err != nil {
		t.Fatalf("GET through SNI remap proxy failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong-tls" {
		t.Errorf("expected 'pong-tls', got %q", string(body))
	}
}

func TestSNIRemapNonTLS(t *testing.T) {
	httpAddr, cleanup := startHTTPEcho(t)
	defer cleanup()

	proxyPort := freePort(t)
	proxyAddr := fmt.Sprintf("127.0.0.1:%d", proxyPort)
	server, err := socks5.NewClassicServer(proxyAddr, "127.0.0.1", "", "", 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	handler := &SNIRemapHandler{TCPTimeout: 10, UDPTimeout: 10, IPv4Only: false}
	go server.ListenAndServe(handler)
	defer server.Shutdown()
	time.Sleep(200 * time.Millisecond)

	client, err := socks5.NewClient(proxyAddr, "", "", 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	httpClient := &http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return client.Dial(network, addr)
			},
		},
		Timeout: 5 * time.Second,
	}
	resp, err := httpClient.Get(fmt.Sprintf("http://%s/ping", httpAddr))
	if err != nil {
		t.Fatalf("GET non-TLS through SNI remap proxy failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong" {
		t.Errorf("expected 'pong', got %q", string(body))
	}
}
