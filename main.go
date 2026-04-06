package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/txthinking/socks5"
)

var (
	version = "0.4.0"
	verbose bool
)

func debugf(format string, args ...any) {
	if verbose {
		log.Printf("[DEBUG] "+format, args...)
	}
}

func main() {
	addr := flag.String("addr", "0.0.0.0:1080", "Listen address (host:port)")
	ip := flag.String("ip", "", "Outbound IP for UDP ASSOCIATE replies (auto-detected if empty)")
	user := flag.String("user", "", "Username for SOCKS5 auth (no auth if empty)")
	pass := flag.String("pass", "", "Password for SOCKS5 auth (no auth if empty)")
	tcpTimeout := flag.Int("tcp-timeout", 60, "TCP connection idle timeout in seconds")
	udpTimeout := flag.Int("udp-timeout", 60, "UDP session timeout in seconds")
	noIPv6 := flag.Bool("no-ipv6", false, "Reject IPv6 destinations and force IPv4-only outbound")
	sniRemap := flag.Bool("sni-remap", false, "Sniff TLS SNI and re-resolve hostnames via local DNS (fixes client-side DNS pollution)")
	sniPorts := flag.String("sni-ports", "443", "Comma-separated list of ports to apply SNI remap (default: 443)")
	verboseFlag := flag.Bool("verbose", false, "Enable verbose logging (SNI remap details, connection info)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	verbose = *verboseFlag

	if *showVersion {
		fmt.Printf("pantyhose %s\n", version)
		os.Exit(0)
	}

	if *noIPv6 {
		installIPv4OnlyDialers()
		log.Println("IPv6 disabled: all outbound connections forced to IPv4")
	}

	outboundIP := *ip
	if outboundIP == "" {
		detected, err := detectOutboundIP()
		if err != nil {
			log.Fatalf("Failed to detect outbound IP: %v. Please specify --ip manually.", err)
		}
		outboundIP = detected
	}
	log.Printf("Using outbound IP: %s", outboundIP)

	authMode := "none"
	if *user != "" && *pass != "" {
		authMode = "username/password"
	}
	log.Printf("Auth mode: %s", authMode)

	_, port, err := net.SplitHostPort(*addr)
	if err != nil {
		log.Fatalf("Invalid listen address %q: %v", *addr, err)
	}

	printFirewallWarning(port)

	server, err := socks5.NewClassicServer(*addr, outboundIP, *user, *pass, *tcpTimeout, *udpTimeout)
	if err != nil {
		log.Fatalf("Failed to create SOCKS5 server: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down...", sig)
		if err := server.Shutdown(); err != nil {
			log.Printf("Shutdown error: %v", err)
		}
	}()

	var handler socks5.Handler
	if *sniRemap {
		ports, err := parsePorts(*sniPorts)
		if err != nil {
			log.Fatalf("Invalid --sni-ports: %v", err)
		}
		handler = &SNIRemapHandler{
			TCPTimeout: *tcpTimeout,
			UDPTimeout: *udpTimeout,
			IPv4Only:   *noIPv6,
			Ports:      ports,
		}
		log.Printf("SNI remap enabled on ports: %s", *sniPorts)
	}

	log.Printf("SOCKS5 server listening on %s (TCP + UDP)", *addr)

	if err := server.ListenAndServe(handler); err != nil {
		if isShutdownError(err) {
			log.Println("Server stopped.")
		} else {
			log.Fatalf("Server error: %v", err)
		}
	}
}

func detectOutboundIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:53")
	if err != nil {
		return detectOutboundIPFallback()
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

func detectOutboundIPFallback() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ipAddr net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ipAddr = v.IP
			case *net.IPAddr:
				ipAddr = v.IP
			}
			if ipAddr == nil || ipAddr.IsLoopback() || ipAddr.To4() == nil {
				continue
			}
			return ipAddr.String(), nil
		}
	}
	return "", fmt.Errorf("no suitable network interface found")
}

func printFirewallWarning(port string) {
	log.Println("WARNING: Make sure the firewall allows inbound connections on your listen port.")
	log.Printf("  On Windows, run as Administrator:")
	log.Printf("    netsh advfirewall firewall add rule name=\"pantyhose-tcp\" dir=in action=allow protocol=TCP localport=%s", port)
	log.Printf("    netsh advfirewall firewall add rule name=\"pantyhose-udp\" dir=in action=allow protocol=UDP localport=%s", port)
}

func isShutdownError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "server closed")
}

func parsePorts(s string) (map[uint16]bool, error) {
	ports := make(map[uint16]bool)
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid port %q", p)
		}
		ports[uint16(n)] = true
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("no valid ports specified")
	}
	return ports, nil
}

var errIPv6Disabled = fmt.Errorf("IPv6 destination rejected (--no-ipv6 is enabled)")

func isIPv6Addr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() == nil
}

func installIPv4OnlyDialers() {
	socks5.DialTCP = func(network, laddr, raddr string) (net.Conn, error) {
		if isIPv6Addr(raddr) {
			return nil, errIPv6Disabled
		}
		var la *net.TCPAddr
		if laddr != "" {
			var err error
			la, err = net.ResolveTCPAddr("tcp4", laddr)
			if err != nil {
				return nil, err
			}
		}
		ra, err := net.ResolveTCPAddr("tcp4", raddr)
		if err != nil {
			return nil, err
		}
		return net.DialTCP("tcp4", la, ra)
	}

	socks5.DialUDP = func(network, laddr, raddr string) (net.Conn, error) {
		if isIPv6Addr(raddr) {
			return nil, errIPv6Disabled
		}
		var la *net.UDPAddr
		if laddr != "" {
			var err error
			la, err = net.ResolveUDPAddr("udp4", laddr)
			if err != nil {
				return nil, err
			}
		}
		ra, err := net.ResolveUDPAddr("udp4", raddr)
		if err != nil {
			return nil, err
		}
		return net.DialUDP("udp4", la, ra)
	}
}
