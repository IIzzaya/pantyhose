package main

import (
	"encoding/binary"
	"log"
	"net"
	"time"

	"github.com/txthinking/socks5"
	"pantyhose/internal/tunnel"
)

// serveTLSMode runs the SOCKS5 server over TLS+yamux. Each yamux stream is
// handled as an independent SOCKS5 session. The socks5.Server is used only
// for protocol negotiation (Negotiate/GetRequest); actual connection handling
// bypasses the library's TCPHandle since yamux streams are net.Conn, not
// *net.TCPConn.
func serveTLSMode(tunnelSrv *tunnel.Server, srv *socks5.Server, sniHandler *SNIRemapHandler, tcpTimeout int) {
	for {
		stream, err := tunnelSrv.Accept()
		if err != nil {
			return
		}
		go handleStream(stream, srv, sniHandler, tcpTimeout)
	}
}

func handleStream(conn net.Conn, srv *socks5.Server, sniHandler *SNIRemapHandler, tcpTimeout int) {
	defer conn.Close()

	if err := srv.Negotiate(conn); err != nil {
		debugf("SOCKS5 negotiate error: %v", err)
		return
	}

	req, err := srv.GetRequest(conn)
	if err != nil {
		debugf("SOCKS5 request error: %v", err)
		return
	}

	if req.Cmd != socks5.CmdConnect {
		reply := socks5.NewReply(socks5.RepCommandNotSupported, socks5.ATYPIPv4, []byte{0, 0, 0, 0}, []byte{0, 0})
		reply.WriteTo(conn)
		return
	}

	destPort := binary.BigEndian.Uint16(req.DstPort)
	if sniHandler != nil && sniHandler.Ports[destPort] {
		handleStreamSNI(conn, req, sniHandler, destPort)
		return
	}

	handleStreamConnect(conn, req, tcpTimeout)
}

// handleStreamConnect handles a SOCKS5 CONNECT request on a yamux stream.
func handleStreamConnect(conn net.Conn, req *socks5.Request, tcpTimeout int) {
	destAddr := req.Address()

	network := "tcp"
	if isIPv6Addr(destAddr) {
		reply := socks5.NewReply(socks5.RepAddressNotSupported, socks5.ATYPIPv4, []byte{0, 0, 0, 0}, []byte{0, 0})
		reply.WriteTo(conn)
		return
	}

	rc, err := net.DialTimeout(network, destAddr, 10*time.Second)
	if err != nil {
		reply := socks5.NewReply(socks5.RepHostUnreachable, socks5.ATYPIPv4, []byte{0, 0, 0, 0}, []byte{0, 0})
		reply.WriteTo(conn)
		return
	}
	defer rc.Close()

	bindAddr := rc.LocalAddr().(*net.TCPAddr)
	reply := socks5.NewReply(socks5.RepSuccess, socks5.ATYPIPv4, bindAddr.IP.To4(), []byte{byte(bindAddr.Port >> 8), byte(bindAddr.Port)})
	if _, err := reply.WriteTo(conn); err != nil {
		return
	}

	go func() {
		defer rc.Close()
		defer conn.Close()
		relay(rc, conn, tcpTimeout)
	}()
	relay(conn, rc, tcpTimeout)
}

// handleStreamSNI handles a SOCKS5 CONNECT with SNI remap on a yamux stream.
func handleStreamSNI(conn net.Conn, req *socks5.Request, h *SNIRemapHandler, destPort uint16) {
	reply := socks5.NewReply(socks5.RepSuccess, socks5.ATYPIPv4, []byte{0, 0, 0, 0}, []byte{0, 0})
	if _, err := reply.WriteTo(conn); err != nil {
		return
	}

	buf := make([]byte, 16384)
	if h.TCPTimeout != 0 {
		conn.SetReadDeadline(time.Now().Add(time.Duration(h.TCPTimeout) * time.Second))
	}
	n, err := conn.Read(buf)
	if err != nil {
		return
	}
	firstData := buf[:n]

	destAddr := req.Address()
	sni := extractSNI(firstData)

	if sni != "" {
		resolved := h.resolveHostname(sni, destPort)
		if resolved != "" && resolved != destAddr {
			debugf("SNI remap: %s -> %s (host: %s)", destAddr, resolved, sni)
			destAddr = resolved
		} else if resolved != "" {
			debugf("SNI passthrough: %s (host: %s)", destAddr, sni)
		} else {
			debugf("SNI resolve failed for %s, using original %s", sni, destAddr)
		}
	}

	network := "tcp"
	if h.IPv4Only {
		if isIPv6Addr(destAddr) {
			return
		}
		network = "tcp4"
	}

	rc, err := net.DialTimeout(network, destAddr, 10*time.Second)
	if err != nil {
		log.Printf("TLS mode dial %s: %v", destAddr, err)
		return
	}
	defer rc.Close()

	if _, err := rc.Write(firstData); err != nil {
		log.Printf("TLS mode forward ClientHello: %v", err)
		return
	}

	go func() {
		defer rc.Close()
		defer conn.Close()
		relay(rc, conn, h.TCPTimeout)
	}()
	relay(conn, rc, h.TCPTimeout)
}

