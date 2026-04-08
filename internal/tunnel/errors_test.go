package tunnel

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"testing"
)

func TestClassifyConnectError_ConnectionRefused(t *testing.T) {
	err := fmt.Errorf("TLS dial 10.0.0.5:1080: %w",
		&net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("connection refused")})

	info := ClassifyConnectError(err)
	if info.Category != "Connection refused" {
		t.Errorf("expected 'Connection refused', got %q", info.Category)
	}
	if info.Suggestion == "" {
		t.Error("suggestion should not be empty")
	}
}

func TestClassifyConnectError_DNSFailure(t *testing.T) {
	err := fmt.Errorf("TLS dial bad-host:1080: %w",
		&net.DNSError{Err: "no such host", Name: "bad-host"})

	info := ClassifyConnectError(err)
	if info.Category != "DNS resolution failed" {
		t.Errorf("expected 'DNS resolution failed', got %q", info.Category)
	}
}

func TestClassifyConnectError_UnknownAuthority(t *testing.T) {
	err := fmt.Errorf("TLS dial 10.0.0.5:1080: %w",
		&x509.UnknownAuthorityError{})

	info := ClassifyConnectError(err)
	if info.Category != "Certificate authority mismatch" {
		t.Errorf("expected 'Certificate authority mismatch', got %q", info.Category)
	}
}

func TestClassifyConnectError_CertificateInvalid(t *testing.T) {
	err := fmt.Errorf("TLS dial 10.0.0.5:1080: %w",
		&x509.CertificateInvalidError{Reason: x509.Expired})

	info := ClassifyConnectError(err)
	if info.Category != "Invalid certificate" {
		t.Errorf("expected 'Invalid certificate', got %q", info.Category)
	}
}

func TestClassifyConnectError_TLSRecordHeader(t *testing.T) {
	err := fmt.Errorf("TLS dial 10.0.0.5:1080: %w",
		tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"})

	info := ClassifyConnectError(err)
	if info.Category != "TLS handshake failed" {
		t.Errorf("expected 'TLS handshake failed', got %q", info.Category)
	}
}

func TestClassifyConnectError_Timeout(t *testing.T) {
	err := fmt.Errorf("TLS dial 10.0.0.5:1080: %w",
		&net.OpError{Op: "dial", Net: "tcp", Err: &timeoutErr{}})

	info := ClassifyConnectError(err)
	if info.Category != "Connection timed out" {
		t.Errorf("expected 'Connection timed out', got %q", info.Category)
	}
}

func TestClassifyConnectError_NetworkUnreachable(t *testing.T) {
	err := fmt.Errorf("TLS dial 10.0.0.5:1080: network is unreachable")

	info := ClassifyConnectError(err)
	if info.Category != "Network unreachable" {
		t.Errorf("expected 'Network unreachable', got %q", info.Category)
	}
}

func TestClassifyConnectError_GenericError(t *testing.T) {
	err := fmt.Errorf("something unexpected happened")

	info := ClassifyConnectError(err)
	if info.Category != "Connection failed" {
		t.Errorf("expected 'Connection failed', got %q", info.Category)
	}
	if info.Suggestion == "" {
		t.Error("suggestion should not be empty for generic errors")
	}
}

func TestClassifyConnectError_Nil(t *testing.T) {
	info := ClassifyConnectError(nil)
	if info.Category != "" {
		t.Errorf("expected empty category for nil error, got %q", info.Category)
	}
}

func TestClassifyConnectError_HostnameMismatch(t *testing.T) {
	cert := &x509.Certificate{DNSNames: []string{"other.example.com"}}
	err := fmt.Errorf("TLS dial 10.0.0.5:1080: %w",
		x509.HostnameError{Certificate: cert, Host: "10.0.0.5"})

	info := ClassifyConnectError(err)
	if info.Category != "Certificate hostname mismatch" {
		t.Errorf("expected 'Certificate hostname mismatch', got %q", info.Category)
	}
}

func TestClassifyConnectError_ConnectionReset(t *testing.T) {
	err := fmt.Errorf("TLS dial 10.0.0.5:1080: connection reset by peer")

	info := ClassifyConnectError(err)
	if info.Category != "Connection reset by server" {
		t.Errorf("expected 'Connection reset by server', got %q", info.Category)
	}
}

// Real connection attempt to a port with no listener.
func TestClassifyConnectError_RealConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	_, dialErr := net.Dial("tcp", addr)
	if dialErr == nil {
		t.Skip("expected connection refused but dial succeeded")
	}

	wrappedErr := fmt.Errorf("TLS dial %s: %w", addr, dialErr)
	info := ClassifyConnectError(wrappedErr)
	if info.Category != "Connection refused" {
		t.Errorf("expected 'Connection refused', got %q (err: %v)", info.Category, dialErr)
	}
}

type timeoutErr struct{}

func (e *timeoutErr) Error() string   { return "i/o timeout" }
func (e *timeoutErr) Timeout() bool   { return true }
func (e *timeoutErr) Temporary() bool { return true }

