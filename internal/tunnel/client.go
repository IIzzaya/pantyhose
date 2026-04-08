package tunnel

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
)

// Client maintains a TLS+yamux session to a remote tunnel server and opens
// new streams on demand.
type Client struct {
	serverAddr string
	tlsCfg     *tls.Config
	session    *yamux.Session
	mu         sync.Mutex
	closed     chan struct{}
	closeOnce  sync.Once
}

// NewClient creates a tunnel client that connects to the given server address
// using mTLS.
func NewClient(serverAddr, certFile, keyFile, caFile string) (*Client, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load client cert: %w", err)
	}

	caCertPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		return nil, fmt.Errorf("invalid CA certificate")
	}

	host, _, err := net.SplitHostPort(serverAddr)
	if err != nil {
		host = serverAddr
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		ServerName:   host,
		MinVersion:   tls.VersionTLS13,
	}

	c := &Client{
		serverAddr: serverAddr,
		tlsCfg:     tlsCfg,
		closed:     make(chan struct{}),
	}

	return c, nil
}

// Connect establishes the initial TLS+yamux session. Call this once before
// opening streams.
func (c *Client) Connect() error {
	return c.reconnect()
}

func (c *Client) reconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.session != nil && !c.session.IsClosed() {
		return nil
	}

	conn, err := tls.Dial("tcp", c.serverAddr, c.tlsCfg)
	if err != nil {
		return fmt.Errorf("TLS dial %s: %w", c.serverAddr, err)
	}

	cfg := yamux.DefaultConfig()
	cfg.LogOutput = log.Writer()

	session, err := yamux.Client(conn, cfg)
	if err != nil {
		conn.Close()
		return fmt.Errorf("yamux client: %w", err)
	}

	c.session = session
	return nil
}

// OpenStream opens a new multiplexed stream over the TLS tunnel.
// If the session is closed, it triggers a reconnect with exponential backoff.
func (c *Client) OpenStream() (net.Conn, error) {
	select {
	case <-c.closed:
		return nil, net.ErrClosed
	default:
	}

	c.mu.Lock()
	sess := c.session
	c.mu.Unlock()

	if sess != nil && !sess.IsClosed() {
		stream, err := sess.Open()
		if err == nil {
			return stream, nil
		}
	}

	if err := c.reconnectWithBackoff(); err != nil {
		return nil, err
	}

	c.mu.Lock()
	sess = c.session
	c.mu.Unlock()

	return sess.Open()
}

func (c *Client) reconnectWithBackoff() error {
	backoff := time.Second
	maxBackoff := 60 * time.Second

	for {
		select {
		case <-c.closed:
			return net.ErrClosed
		default:
		}

		log.Printf("Reconnecting to %s...", c.serverAddr)
		c.mu.Lock()
		if c.session != nil {
			c.session.Close()
			c.session = nil
		}
		c.mu.Unlock()

		err := c.reconnect()
		if err == nil {
			log.Printf("Reconnected to %s", c.serverAddr)
			return nil
		}

		log.Printf("Reconnect failed: %v (retrying in %v)", err, backoff)
		select {
		case <-time.After(backoff):
		case <-c.closed:
			return net.ErrClosed
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// Close shuts down the tunnel client and its session.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.mu.Lock()
		if c.session != nil {
			c.session.Close()
		}
		c.mu.Unlock()
	})
	return nil
}
