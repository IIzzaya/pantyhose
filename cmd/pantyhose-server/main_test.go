package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/txthinking/socks5"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func startTestServer(t *testing.T, port int, username, password string) *socks5.Server {
	t.Helper()
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	server, err := socks5.NewClassicServer(addr, "127.0.0.1", username, password, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = server.ListenAndServe(nil)
	}()
	time.Sleep(200 * time.Millisecond)
	return server
}

func startHTTPEcho(t *testing.T) (string, func()) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "pong")
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(listener) }()
	return listener.Addr().String(), func() { srv.Close() }
}

func startTCPEcho(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()
	return listener.Addr().String()
}

// --- Unit tests ---

func TestDetectOutboundIP(t *testing.T) {
	ip, err := detectOutboundIP()
	if err != nil {
		t.Fatalf("detectOutboundIP failed: %v", err)
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		t.Fatalf("detectOutboundIP returned invalid IP: %s", ip)
	}
	if parsed.IsLoopback() {
		t.Errorf("detectOutboundIP returned loopback: %s", ip)
	}
	t.Logf("Detected outbound IP: %s", ip)
}

func TestDetectOutboundIPFallback(t *testing.T) {
	ip, err := detectOutboundIPFallback()
	if err != nil {
		t.Fatalf("detectOutboundIPFallback failed: %v", err)
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		t.Fatalf("detectOutboundIPFallback returned invalid IP: %s", ip)
	}
	t.Logf("Fallback detected IP: %s", ip)
}

func TestIsShutdownError(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"use of closed network connection", true},
		{"server closed", true},
		{"connection refused", false},
		{"timeout", false},
	}
	for _, tt := range tests {
		got := isShutdownError(fmt.Errorf("%s", tt.msg))
		if got != tt.want {
			t.Errorf("isShutdownError(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

// --- Integration tests ---

func TestTCPProxyNoAuth(t *testing.T) {
	httpAddr, cleanup := startHTTPEcho(t)
	defer cleanup()

	port := freePort(t)
	server := startTestServer(t, port, "", "")
	defer server.Shutdown()

	client, err := socks5.NewClient(fmt.Sprintf("127.0.0.1:%d", port), "", "", 10, 10)
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
		t.Fatalf("GET through proxy failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong" {
		t.Errorf("expected body 'pong', got %q", string(body))
	}
}

func TestTCPProxyWithAuth(t *testing.T) {
	httpAddr, cleanup := startHTTPEcho(t)
	defer cleanup()

	port := freePort(t)
	server := startTestServer(t, port, "admin", "secret")
	defer server.Shutdown()

	client, err := socks5.NewClient(fmt.Sprintf("127.0.0.1:%d", port), "admin", "secret", 10, 10)
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
		t.Fatalf("GET through proxy failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pong" {
		t.Errorf("expected body 'pong', got %q", string(body))
	}
}

func TestTCPProxyAuthRejected(t *testing.T) {
	port := freePort(t)
	server := startTestServer(t, port, "admin", "secret")
	defer server.Shutdown()

	client, err := socks5.NewClient(fmt.Sprintf("127.0.0.1:%d", port), "admin", "wrong", 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Dial("tcp", "127.0.0.1:80")
	if err == nil {
		t.Fatal("expected auth rejection, got nil error")
	}
	t.Logf("Auth correctly rejected: %v", err)
}

func TestTCPRawConnect(t *testing.T) {
	echoAddr := startTCPEcho(t)

	port := freePort(t)
	server := startTestServer(t, port, "", "")
	defer server.Shutdown()

	client, err := socks5.NewClient(fmt.Sprintf("127.0.0.1:%d", port), "", "", 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := client.Dial("tcp", echoAddr)
	if err != nil {
		t.Fatalf("Dial through proxy failed: %v", err)
	}
	defer conn.Close()

	msg := []byte("hello pantyhose")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	buf := make([]byte, 256)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Errorf("expected echo %q, got %q", msg, buf[:n])
	}
}

func TestIsIPv6Addr(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:80", false},
		{"10.0.0.1:443", false},
		{"[::1]:80", true},
		{"[2404:6800:4012:6::200e]:443", true},
		{"[2001::1]:443", true},
		{"example.com:80", false},
	}
	for _, tt := range tests {
		got := isIPv6Addr(tt.addr)
		if got != tt.want {
			t.Errorf("isIPv6Addr(%q) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}

func TestIPv4OnlyDialersRejectIPv6(t *testing.T) {
	installIPv4OnlyDialers()
	defer func() {
		// Restore default dialers after test
		socks5.DialTCP = defaultDialTCP()
		socks5.DialUDP = defaultDialUDP()
	}()

	_, err := socks5.DialTCP("tcp", "", "[2001::1]:443")
	if err == nil {
		t.Fatal("expected IPv6 rejection, got nil error")
	}
	if err != errIPv6Disabled {
		t.Errorf("expected errIPv6Disabled, got: %v", err)
	}

	_, err = socks5.DialUDP("udp", "", "[2001::1]:53")
	if err == nil {
		t.Fatal("expected IPv6 rejection, got nil error")
	}
	if err != errIPv6Disabled {
		t.Errorf("expected errIPv6Disabled, got: %v", err)
	}
}

func defaultDialTCP() func(string, string, string) (net.Conn, error) {
	return func(network, laddr, raddr string) (net.Conn, error) {
		var la *net.TCPAddr
		if laddr != "" {
			var err error
			la, err = net.ResolveTCPAddr(network, laddr)
			if err != nil {
				return nil, err
			}
		}
		ra, err := net.ResolveTCPAddr(network, raddr)
		if err != nil {
			return nil, err
		}
		return net.DialTCP(network, la, ra)
	}
}

func defaultDialUDP() func(string, string, string) (net.Conn, error) {
	return func(network, laddr, raddr string) (net.Conn, error) {
		var la *net.UDPAddr
		if laddr != "" {
			var err error
			la, err = net.ResolveUDPAddr(network, laddr)
			if err != nil {
				return nil, err
			}
		}
		ra, err := net.ResolveUDPAddr(network, raddr)
		if err != nil {
			return nil, err
		}
		return net.DialUDP(network, la, ra)
	}
}

func TestServerShutdown(t *testing.T) {
	port := freePort(t)
	server := startTestServer(t, port, "", "")

	err := server.Shutdown()
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	_, err = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
	if err == nil {
		t.Error("expected connection refused after shutdown")
	}
}
