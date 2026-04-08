package tunnel

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"os"
	"sync"

	"github.com/hashicorp/yamux"
)

// Server accepts TLS+yamux connections and exposes each multiplexed stream
// as a net.Conn via a standard net.Listener interface.
type Server struct {
	tlsListener net.Listener
	streamCh    chan net.Conn
	sessions    []*yamux.Session
	mu          sync.Mutex
	closed      chan struct{}
	closeOnce   sync.Once
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
		tlsListener: ln,
		streamCh:    make(chan net.Conn, 256),
		closed:      make(chan struct{}),
	}

	go s.acceptLoop()
	return s, nil
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
	cfg.LogOutput = log.Writer()

	session, err := yamux.Server(conn, cfg)
	if err != nil {
		log.Printf("yamux session error: %v", err)
		conn.Close()
		return
	}

	s.mu.Lock()
	s.sessions = append(s.sessions, session)
	s.mu.Unlock()

	log.Printf("New tunnel session from %s", conn.RemoteAddr())

	for {
		stream, err := session.Accept()
		if err != nil {
			select {
			case <-s.closed:
			default:
				if !session.IsClosed() {
					log.Printf("Tunnel session from %s closed: %v", conn.RemoteAddr(), err)
				}
			}
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
		for _, sess := range s.sessions {
			sess.Close()
		}
		s.mu.Unlock()
		s.tlsListener.Close()
	})
	return nil
}
