package tunnel

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"

	"github.com/hashicorp/yamux"
)

// Server accepts TLS+yamux connections and exposes each multiplexed stream
// as a net.Conn via a standard net.Listener interface.
type Server struct {
	tlsListener   net.Listener
	streamCh      chan net.Conn
	sessions      map[*yamux.Session]string // session → remote addr
	mu            sync.Mutex
	closed        chan struct{}
	closeOnce     sync.Once
	caFingerprint string
	logOutput     io.Writer
}

// NewServer creates a TLS listener with mTLS (mutual TLS) on the given address
// and starts accepting connections. Each TLS connection becomes a yamux session;
// each yamux stream is delivered through the net.Listener interface.
func NewServer(addr, certFile, keyFile, caFile string) (*Server, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load server cert: %w", err)
	}

	caCertPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		return nil, fmt.Errorf("invalid CA certificate")
	}

	var caFP string
	if block, _ := pem.Decode(caCertPEM); block != nil {
		h := sha256.Sum256(block.Bytes)
		caFP = fmt.Sprintf("%x", h[:4])
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}

	ln, err := tls.Listen("tcp", addr, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	s := &Server{
		tlsListener:   ln,
		streamCh:      make(chan net.Conn, 256),
		sessions:      make(map[*yamux.Session]string),
		closed:        make(chan struct{}),
		caFingerprint: caFP,
		logOutput:     io.Discard,
	}

	go s.acceptLoop()
	return s, nil
}

// SetLogOutput sets the writer for yamux internal logs. By default, yamux
// logs are discarded. Call this immediately after NewServer returns.
func (s *Server) SetLogOutput(w io.Writer) {
	s.mu.Lock()
	s.logOutput = w
	s.mu.Unlock()
}

// CAFingerprint returns the short SHA256 fingerprint of the CA certificate.
func (s *Server) CAFingerprint() string {
	return s.caFingerprint
}

// ActiveSessions returns the number of currently connected tunnel sessions.
func (s *Server) ActiveSessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions)
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.tlsListener.Accept()
		if err != nil {
			select {
			case <-s.closed:
				return
			default:
			}
			log.Printf("TLS accept error: %v", err)
			continue
		}
		go s.handleSession(conn)
	}
}

func (s *Server) handleSession(conn net.Conn) {
	cfg := yamux.DefaultConfig()
	s.mu.Lock()
	cfg.LogOutput = s.logOutput
	s.mu.Unlock()

	session, err := yamux.Server(conn, cfg)
	if err != nil {
		log.Printf("yamux session error: %v", err)
		conn.Close()
		return
	}

	addr := conn.RemoteAddr().String()

	s.mu.Lock()
	s.sessions[session] = addr
	active := len(s.sessions)
	s.mu.Unlock()

	log.Printf("[%s] Tunnel session connected (active: %d)", addr, active)

	defer func() {
		s.mu.Lock()
		delete(s.sessions, session)
		active := len(s.sessions)
		s.mu.Unlock()

		select {
		case <-s.closed:
		default:
			log.Printf("[%s] Tunnel session disconnected (active: %d)", addr, active)
		}
	}()

	for {
		stream, err := session.Accept()
		if err != nil {
			return
		}
		select {
		case s.streamCh <- stream:
		case <-s.closed:
			stream.Close()
			return
		}
	}
}

// Accept returns the next yamux stream as a net.Conn.
// Implements the net.Listener interface.
func (s *Server) Accept() (net.Conn, error) {
	select {
	case conn := <-s.streamCh:
		return conn, nil
	case <-s.closed:
		return nil, net.ErrClosed
	}
}

// Addr returns the listener's network address.
func (s *Server) Addr() net.Addr {
	return s.tlsListener.Addr()
}

// Close shuts down the tunnel server, closing all sessions and the TLS listener.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		close(s.closed)
		s.mu.Lock()
		for sess := range s.sessions {
			sess.Close()
		}
		s.sessions = make(map[*yamux.Session]string)
		s.mu.Unlock()
		s.tlsListener.Close()
	})
	return nil
}
