package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/txthinking/socks5"
	"pantyhose/internal/certgen"
	"pantyhose/internal/tunnel"
)

// TestTunnelE2E verifies the full chain:
//   HTTP echo <-- pantyhose-server (TLS) <-- yamux tunnel <-- pantyhose-client <-- SOCKS5 client
func TestTunnelE2E(t *testing.T) {
	httpAddr, cleanup := startHTTPEcho(t)
	defer cleanup()

	certs := generateTestCerts(t)

	// Start pantyhose-server in TLS mode
	serverPort := freePort(t)
	serverAddr := fmt.Sprintf("127.0.0.1:%d", serverPort)

	srv, err := socks5.NewClassicServer(serverAddr, "127.0.0.1", "", "", 10, 10)
	if err != nil {
		t.Fatal(err)
	}

	tunnelSrv, err := tunnel.NewServer(serverAddr, certs.ServerCert, certs.ServerKey, certs.CACert)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnelSrv.Close()

	go serveTLSMode(tunnelSrv, srv, nil, 10)

	// Start local tunnel client
	tunnelClient, err := tunnel.NewClient(tunnelSrv.Addr().String(), certs.ClientCert, certs.ClientKey, certs.CACert)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnelClient.Close()

	if err := tunnelClient.Connect(); err != nil {
		t.Fatalf("tunnel connect: %v", err)
	}

	// Start local SOCKS5 listener that forwards to the tunnel
	localLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer localLn.Close()

	go func() {
		for {
			conn, err := localLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				stream, err := tunnelClient.OpenStream()
				if err != nil {
					return
				}
				defer stream.Close()
				done := make(chan struct{}, 2)
				go func() { io.Copy(stream, c); done <- struct{}{} }()
				go func() { io.Copy(c, stream); done <- struct{}{} }()
				<-done
			}(conn)
		}
	}()

	localAddr := localLn.Addr().String()

	// Use socks5 client to connect through the tunnel
	socks5Client, err := socks5.NewClient(localAddr, "", "", 10, 10)
	if err != nil {
		t.Fatal(err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return socks5Client.Dial(network, addr)
			},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := httpClient.Get(fmt.Sprintf("http://%s/ping", httpAddr))
	if err != nil {
		t.Fatalf("GET through TLS tunnel failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong" {
		t.Errorf("expected 'pong', got %q", string(body))
	}
	t.Log("E2E test passed: HTTP request through TLS tunnel returned 'pong'")
}

// TestTunnelE2EWithSNI tests the full chain with SNI remap enabled.
func TestTunnelE2EWithSNI(t *testing.T) {
	cert := generateSelfSignedCert(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "pong-tunnel-tls")
	})
	tlsLn, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tlsLn.Close()

	httpSrv := &http.Server{Handler: mux}
	go httpSrv.Serve(tlsLn)
	defer httpSrv.Close()

	_, tlsPortStr, _ := net.SplitHostPort(tlsLn.Addr().String())

	certs := generateTestCerts(t)

	serverPort := freePort(t)
	serverAddr := fmt.Sprintf("127.0.0.1:%d", serverPort)

	srv, err := socks5.NewClassicServer(serverAddr, "127.0.0.1", "", "", 10, 10)
	if err != nil {
		t.Fatal(err)
	}

	tunnelSrv, err := tunnel.NewServer(serverAddr, certs.ServerCert, certs.ServerKey, certs.CACert)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnelSrv.Close()

	sniHandler := &SNIRemapHandler{TCPTimeout: 10, UDPTimeout: 10, IPv4Only: false}
	go serveTLSMode(tunnelSrv, srv, sniHandler, 10)

	tunnelClient, err := tunnel.NewClient(tunnelSrv.Addr().String(), certs.ClientCert, certs.ClientKey, certs.CACert)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnelClient.Close()

	if err := tunnelClient.Connect(); err != nil {
		t.Fatal(err)
	}

	localLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer localLn.Close()

	go func() {
		for {
			conn, err := localLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				stream, err := tunnelClient.OpenStream()
				if err != nil {
					return
				}
				defer stream.Close()
				done := make(chan struct{}, 2)
				go func() { io.Copy(stream, c); done <- struct{}{} }()
				go func() { io.Copy(c, stream); done <- struct{}{} }()
				<-done
			}(conn)
		}
	}()

	socks5Client, err := socks5.NewClient(localLn.Addr().String(), "", "", 10, 10)
	if err != nil {
		t.Fatal(err)
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			Dial: func(network, addr string) (net.Conn, error) {
				return socks5Client.Dial(network, addr)
			},
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}

	// Use the TLS port for HTTPS request through the tunnel
	resp, err := httpClient.Get(fmt.Sprintf("https://127.0.0.1:%s/ping", tlsPortStr))
	if err != nil {
		t.Fatalf("HTTPS through TLS tunnel failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong-tunnel-tls" {
		t.Errorf("expected 'pong-tunnel-tls', got %q", string(body))
	}
	t.Log("E2E SNI test passed: HTTPS through TLS tunnel returned 'pong-tunnel-tls'")
}

// TestTunnelE2ERawTCP tests raw TCP echo through the TLS tunnel.
func TestTunnelE2ERawTCP(t *testing.T) {
	echoAddr := startTCPEcho(t)

	certs := generateTestCerts(t)

	serverPort := freePort(t)
	serverAddr := fmt.Sprintf("127.0.0.1:%d", serverPort)

	srv, err := socks5.NewClassicServer(serverAddr, "127.0.0.1", "", "", 10, 10)
	if err != nil {
		t.Fatal(err)
	}

	tunnelSrv, err := tunnel.NewServer(serverAddr, certs.ServerCert, certs.ServerKey, certs.CACert)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnelSrv.Close()

	go serveTLSMode(tunnelSrv, srv, nil, 10)

	tunnelClient, err := tunnel.NewClient(tunnelSrv.Addr().String(), certs.ClientCert, certs.ClientKey, certs.CACert)
	if err != nil {
		t.Fatal(err)
	}
	defer tunnelClient.Close()

	if err := tunnelClient.Connect(); err != nil {
		t.Fatal(err)
	}

	localLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer localLn.Close()

	go func() {
		for {
			conn, err := localLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				stream, err := tunnelClient.OpenStream()
				if err != nil {
					return
				}
				defer stream.Close()
				done := make(chan struct{}, 2)
				go func() { io.Copy(stream, c); done <- struct{}{} }()
				go func() { io.Copy(c, stream); done <- struct{}{} }()
				<-done
			}(conn)
		}
	}()

	socks5Client, err := socks5.NewClient(localLn.Addr().String(), "", "", 10, 10)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := socks5Client.Dial("tcp", echoAddr)
	if err != nil {
		t.Fatalf("Dial through tunnel: %v", err)
	}
	defer conn.Close()

	msg := []byte("hello encrypted tunnel")
	if _, err := conn.Write(msg); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 256)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != string(msg) {
		t.Errorf("expected %q, got %q", msg, buf[:n])
	}
	t.Log("E2E raw TCP test passed: echo through TLS tunnel works")
}

func generateTestCerts(t *testing.T) *certgen.CertFiles {
	t.Helper()
	dir := t.TempDir()
	files, err := certgen.Generate(dir, []string{"127.0.0.1"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	return files
}
