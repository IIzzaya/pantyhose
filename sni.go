package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/txthinking/socks5"
)

// SNIRemapHandler intercepts HTTPS connections, extracts the SNI hostname from
// the TLS ClientHello, re-resolves it via the local system DNS, and connects
// to the correct IP. This fixes DNS pollution on the client side.
type SNIRemapHandler struct {
	TCPTimeout int
	UDPTimeout int
	IPv4Only   bool
}

func (h *SNIRemapHandler) TCPHandle(s *socks5.Server, c *net.TCPConn, r *socks5.Request) error {
	if r.Cmd == socks5.CmdConnect {
		destPort := binary.BigEndian.Uint16(r.DstPort)
		if destPort == 443 {
			return h.handleTLSWithSNI(s, c, r, destPort)
		}
		return (&socks5.DefaultHandle{}).TCPHandle(s, c, r)
	}
	return (&socks5.DefaultHandle{}).TCPHandle(s, c, r)
}

func (h *SNIRemapHandler) UDPHandle(s *socks5.Server, addr *net.UDPAddr, d *socks5.Datagram) error {
	return (&socks5.DefaultHandle{}).UDPHandle(s, addr, d)
}

func (h *SNIRemapHandler) handleTLSWithSNI(s *socks5.Server, c *net.TCPConn, r *socks5.Request, destPort uint16) error {
	// Reply success early so the client starts sending TLS ClientHello.
	// Use a dummy BND.ADDR; the client doesn't use it for CONNECT.
	reply := socks5.NewReply(socks5.RepSuccess, socks5.ATYPIPv4, []byte{0, 0, 0, 0}, []byte{0, 0})
	if _, err := reply.WriteTo(c); err != nil {
		return err
	}

	// Read TLS ClientHello from the client.
	buf := make([]byte, 16384)
	if h.TCPTimeout != 0 {
		c.SetReadDeadline(time.Now().Add(time.Duration(h.TCPTimeout) * time.Second))
	}
	n, err := c.Read(buf)
	if err != nil {
		// Client closed before sending TLS handshake (preconnect, cancelled request, etc.)
		return nil
	}
	firstData := buf[:n]

	destAddr := r.Address()
	sni := extractSNI(firstData)

	if sni != "" {
		resolved := h.resolveHostname(sni, destPort)
		if resolved != "" && resolved != destAddr {
			log.Printf("SNI remap: %s -> %s (host: %s)", destAddr, resolved, sni)
			destAddr = resolved
		} else if resolved != "" {
			debugf("SNI passthrough: %s (host: %s)", destAddr, sni)
		} else {
			debugf("SNI resolve failed for %s, using original %s", sni, destAddr)
		}
	} else {
		debugf("No SNI in ClientHello for %s, using original", destAddr)
	}

	network := "tcp"
	if h.IPv4Only {
		if isIPv6Addr(destAddr) {
			return errIPv6Disabled
		}
		network = "tcp4"
	}

	debugf("Connecting to %s (%s)", destAddr, network)
	rc, err := net.DialTimeout(network, destAddr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", destAddr, err)
	}
	defer rc.Close()
	debugf("Connected to %s", destAddr)

	if _, err := rc.Write(firstData); err != nil {
		return fmt.Errorf("forward ClientHello: %w", err)
	}

	go func() {
		defer rc.Close()
		defer c.Close()
		relay(rc, c, h.TCPTimeout)
	}()
	relay(c, rc, h.TCPTimeout)
	return nil
}

func (h *SNIRemapHandler) resolveHostname(hostname string, port uint16) string {
	ips, err := net.LookupIP(hostname)
	if err != nil || len(ips) == 0 {
		return ""
	}
	portStr := strconv.Itoa(int(port))
	if h.IPv4Only {
		for _, ip := range ips {
			if ip.To4() != nil {
				return net.JoinHostPort(ip.String(), portStr)
			}
		}
		return ""
	}
	// Prefer IPv4
	for _, ip := range ips {
		if ip.To4() != nil {
			return net.JoinHostPort(ip.String(), portStr)
		}
	}
	return net.JoinHostPort(ips[0].String(), portStr)
}

func relay(dst io.Writer, src io.Reader, timeoutSec int) {
	buf := make([]byte, 32*1024)
	for {
		if timeoutSec != 0 {
			if conn, ok := src.(net.Conn); ok {
				conn.SetDeadline(time.Now().Add(time.Duration(timeoutSec) * time.Second))
			}
		}
		n, err := src.Read(buf)
		if n > 0 {
			if _, wErr := dst.Write(buf[:n]); wErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// extractSNI parses a TLS ClientHello and returns the SNI hostname.
// Returns empty string if parsing fails or no SNI is found.
func extractSNI(data []byte) string {
	// TLS Record Header: ContentType(1) + Version(2) + Length(2)
	if len(data) < 5 {
		return ""
	}
	if data[0] != 0x16 { // Not a Handshake record
		return ""
	}
	recordLen := int(binary.BigEndian.Uint16(data[3:5]))
	data = data[5:]
	if len(data) < recordLen {
		// Truncated, work with what we have
		if len(data) == 0 {
			return ""
		}
	}

	// Handshake Header: Type(1) + Length(3)
	if len(data) < 4 {
		return ""
	}
	if data[0] != 0x01 { // Not ClientHello
		return ""
	}
	data = data[4:] // skip type + length

	// ClientHello: Version(2) + Random(32)
	if len(data) < 34 {
		return ""
	}
	data = data[34:]

	// SessionID: Length(1) + data
	if len(data) < 1 {
		return ""
	}
	sidLen := int(data[0])
	data = data[1:]
	if len(data) < sidLen {
		return ""
	}
	data = data[sidLen:]

	// CipherSuites: Length(2) + data
	if len(data) < 2 {
		return ""
	}
	csLen := int(binary.BigEndian.Uint16(data[:2]))
	data = data[2:]
	if len(data) < csLen {
		return ""
	}
	data = data[csLen:]

	// CompressionMethods: Length(1) + data
	if len(data) < 1 {
		return ""
	}
	cmLen := int(data[0])
	data = data[1:]
	if len(data) < cmLen {
		return ""
	}
	data = data[cmLen:]

	// Extensions: TotalLength(2) + extensions
	if len(data) < 2 {
		return ""
	}
	extTotalLen := int(binary.BigEndian.Uint16(data[:2]))
	data = data[2:]
	if len(data) < extTotalLen {
		extTotalLen = len(data)
	}
	data = data[:extTotalLen]

	// Iterate extensions
	for len(data) >= 4 {
		extType := binary.BigEndian.Uint16(data[:2])
		extLen := int(binary.BigEndian.Uint16(data[2:4]))
		data = data[4:]
		if len(data) < extLen {
			return ""
		}
		if extType == 0x0000 { // server_name extension
			return parseSNIExtension(data[:extLen])
		}
		data = data[extLen:]
	}
	return ""
}

func parseSNIExtension(data []byte) string {
	// ServerNameList: ListLength(2) + entries
	if len(data) < 2 {
		return ""
	}
	listLen := int(binary.BigEndian.Uint16(data[:2]))
	data = data[2:]
	if len(data) < listLen {
		return ""
	}
	data = data[:listLen]

	for len(data) >= 3 {
		nameType := data[0]
		nameLen := int(binary.BigEndian.Uint16(data[1:3]))
		data = data[3:]
		if len(data) < nameLen {
			return ""
		}
		if nameType == 0x00 { // host_name
			return string(data[:nameLen])
		}
		data = data[nameLen:]
	}
	return ""
}
