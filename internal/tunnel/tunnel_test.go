package tunnel

import (
	"fmt"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"pantyhose/internal/certgen"
)

func setupCerts(t *testing.T) *certgen.CertFiles {
	t.Helper()
	dir := t.TempDir()
	files, err := certgen.Generate(dir, []string{"127.0.0.1"}, 1)
	if err != nil {
		t.Fatalf("certgen.Generate failed: %v", err)
	}
	return files
}

func TestTunnelServerClient(t *testing.T) {
	certs := setupCerts(t)

	srv, err := NewServer("127.0.0.1:0", certs.ServerCert, certs.ServerKey, certs.CACert)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	srvAddr := srv.Addr().String()

	go func() {
		for {
			conn, err := srv.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	client, err := NewClient(srvAddr, certs.ClientCert, certs.ClientKey, certs.CACert)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	stream, err := client.OpenStream()
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer stream.Close()

	msg := []byte("hello tunnel")
	if _, err := stream.Write(msg); err != nil {
		t.Fatalf("Write: %v", err)
	}

	buf := make([]byte, 256)
	stream.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := stream.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != string(msg) {
		t.Errorf("expected %q, got %q", msg, buf[:n])
	}
}

func TestTunnelMultipleStreams(t *testing.T) {
	certs := setupCerts(t)

	srv, err := NewServer("127.0.0.1:0", certs.ServerCert, certs.ServerKey, certs.CACert)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	go func() {
		for {
			conn, err := srv.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	client, err := NewClient(srv.Addr().String(), certs.ClientCert, certs.ClientKey, certs.CACert)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	if err := client.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	const numStreams = 10
	for i := 0; i < numStreams; i++ {
		stream, err := client.OpenStream()
		if err != nil {
			t.Fatalf("OpenStream %d: %v", i, err)
		}

		msg := []byte(fmt.Sprintf("stream-%d", i))
		if _, err := stream.Write(msg); err != nil {
			stream.Close()
			t.Fatalf("Write %d: %v", i, err)
		}

		buf := make([]byte, 256)
		stream.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, err := stream.Read(buf)
		if err != nil {
			stream.Close()
			t.Fatalf("Read %d: %v", i, err)
		}
		if string(buf[:n]) != string(msg) {
			t.Errorf("stream %d: expected %q, got %q", i, msg, buf[:n])
		}
		stream.Close()
	}
}

func TestTunnelRejectWithoutClientCert(t *testing.T) {
	certs := setupCerts(t)

	srv, err := NewServer("127.0.0.1:0", certs.ServerCert, certs.ServerKey, certs.CACert)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	conn, err := net.DialTimeout("tcp", srv.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("TCP dial: %v", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	if err == nil {
		t.Error("expected connection to be rejected without client cert")
	}
}

func TestTunnelRejectWrongCA(t *testing.T) {
	certs := setupCerts(t)

	srv, err := NewServer("127.0.0.1:0", certs.ServerCert, certs.ServerKey, certs.CACert)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	otherDir := t.TempDir()
	otherCerts, err := certgen.Generate(otherDir, []string{"127.0.0.1"}, 1)
	if err != nil {
		t.Fatal(err)
	}

	client, err := NewClient(srv.Addr().String(), otherCerts.ClientCert, otherCerts.ClientKey, otherCerts.CACert)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer client.Close()

	err = client.Connect()
	if err == nil {
		stream, openErr := client.OpenStream()
		if openErr == nil {
			stream.Write([]byte("test"))
			buf := make([]byte, 1)
			stream.SetReadDeadline(time.Now().Add(time.Second))
			_, readErr := stream.Read(buf)
			stream.Close()
			if readErr == nil {
				t.Error("expected connection to be rejected with wrong CA")
			}
		}
	}
}

func TestNewServerInvalidCert(t *testing.T) {
	dir := t.TempDir()
	_, err := NewServer("127.0.0.1:0",
		filepath.Join(dir, "nonexistent.crt"),
		filepath.Join(dir, "nonexistent.key"),
		filepath.Join(dir, "nonexistent-ca.crt"))
	if err == nil {
		t.Error("expected error with nonexistent cert files")
	}
}

func TestNewClientInvalidCert(t *testing.T) {
	dir := t.TempDir()
	_, err := NewClient("127.0.0.1:9999",
		filepath.Join(dir, "nonexistent.crt"),
		filepath.Join(dir, "nonexistent.key"),
		filepath.Join(dir, "nonexistent-ca.crt"))
	if err == nil {
		t.Error("expected error with nonexistent cert files")
	}
}
