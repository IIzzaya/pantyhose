package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/txthinking/socks5"
)

var (
	version = "0.4.0"
	verbose bool
)

type logFilter struct {
	out   io.Writer
	drops []string
}

func (f *logFilter) Write(p []byte) (n int, err error) {
	line := string(p)
	for _, drop := range f.drops {
		if strings.Contains(line, drop) {
			return len(p), nil
		}
	}
	return f.out.Write(p)
}

func debugf(format string, args ...any) {
	if verbose {
		log.Printf("[DEBUG] "+format, args...)
	}
}

func enableANSIColors() {
	if runtime.GOOS != "windows" {
		return
	}
	const enableVirtualTerminalProcessing = 0x0004
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	handle := syscall.Handle(os.Stderr.Fd())
	var mode uint32
	r, _, _ := getConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	if r != 0 {
		setConsoleMode.Call(uintptr(handle), uintptr(mode|enableVirtualTerminalProcessing))
	}
}

func main() {
	enableANSIColors()
	addr := flag.String("addr", "0.0.0.0", "Listen address (IP or host:port; use --port to set port separately)")
	port := flag.Int("port", 1080, "Listen port (combined with --addr)")
	ip := flag.String("ip", "", "Outbound IP for UDP ASSOCIATE replies (auto-detected if empty)")
	user := flag.String("user", "", "Username for SOCKS5 auth (no auth if empty)")
	pass := flag.String("pass", "", "Password for SOCKS5 auth (no auth if empty)")
	tcpTimeout := flag.Int("tcp-timeout", 60, "TCP connection idle timeout in seconds")
	udpTimeout := flag.Int("udp-timeout", 60, "UDP session timeout in seconds")
	enableIPv6 := flag.Bool("enable-ipv6", false, "Allow IPv6 outbound connections (default: IPv6 auto-disabled if unavailable)")
	noSNIRemap := flag.Bool("no-sni-remap", false, "Disable TLS SNI hostname re-resolution (SNI remap is enabled by default)")
	sniPorts := flag.String("sni-ports", "443", "Comma-separated list of ports to apply SNI remap (default: 443)")
	verboseFlag := flag.Bool("verbose", false, "Enable verbose logging (SNI remap details, connection info)")
	fwClean := flag.Bool("fw-clean", false, "Print commands to remove firewall rules for the listen port and exit (does not start server)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	helpCN := flag.Bool("help-cn", false, "Print usage in Chinese and exit")
	flag.Parse()

	verbose = *verboseFlag

	if *helpCN {
		printHelpCN()
		os.Exit(0)
	}

	if *showVersion {
		fmt.Printf("pantyhose %s\n", version)
		os.Exit(0)
	}

	if *port < 1 || *port > 65535 {
		log.Fatalf("Invalid port %d: must be 1-65535", *port)
	}

	// Build listen address: if --addr already contains a port, use it as-is;
	// otherwise combine --addr and --port.
	listenAddr := *addr
	if _, _, err := net.SplitHostPort(listenAddr); err != nil {
		listenAddr = net.JoinHostPort(listenAddr, strconv.Itoa(*port))
	}
	portStr := strconv.Itoa(*port)
	if _, p, err := net.SplitHostPort(listenAddr); err == nil {
		portStr = p
	}

	if *fwClean {
		fmt.Println("Run the following commands as Administrator to remove firewall rules:")
		fmt.Printf("  netsh advfirewall firewall delete rule name=\"pantyhose-tcp\" protocol=TCP localport=%s\n", portStr)
		fmt.Printf("  netsh advfirewall firewall delete rule name=\"pantyhose-udp\" protocol=UDP localport=%s\n", portStr)
		os.Exit(0)
	}

	ipv4Only := false
	drops := []string{"169.254.169.254"}
	if *enableIPv6 {
		log.Println("IPv6 enabled: outbound connections may use IPv6")
	} else if probeIPv6() {
		installIPv4OnlyDialers()
		ipv4Only = true
		drops = append(drops, errIPv6Disabled.Error())
		log.Println("IPv6 available but disabled by default (use --enable-ipv6 to allow)")
	} else {
		installIPv4OnlyDialers()
		ipv4Only = true
		drops = append(drops, errIPv6Disabled.Error())
		log.Println("IPv6 not available: all outbound connections forced to IPv4")
	}
	log.SetOutput(&logFilter{out: os.Stderr, drops: drops})

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

	checkFirewall(portStr)

	server, err := socks5.NewClassicServer(listenAddr, outboundIP, *user, *pass, *tcpTimeout, *udpTimeout)
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

	var inner socks5.Handler
	if !*noSNIRemap {
		ports, err := parsePorts(*sniPorts)
		if err != nil {
			log.Fatalf("Invalid --sni-ports: %v", err)
		}
		inner = &SNIRemapHandler{
			TCPTimeout: *tcpTimeout,
			UDPTimeout: *udpTimeout,
			IPv4Only:   ipv4Only,
			Ports:      ports,
		}
		log.Printf("SNI remap enabled on ports: %s", *sniPorts)
	} else {
		inner = &socks5.DefaultHandle{}
		log.Println("SNI remap disabled")
	}
	handler := inner

	log.Printf("SOCKS5 server listening on %s (TCP + UDP)", listenAddr)
	green := "\033[1;32m"
	reset := "\033[0m"
	fmt.Fprintf(os.Stderr, "%sServer started. Press Ctrl+C to stop.%s\n", green, reset)

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

func checkFirewall(port string) {
	if runtime.GOOS != "windows" {
		log.Println("NOTE: Ensure your firewall allows inbound TCP+UDP on port " + port)
		return
	}

	tcpOk, udpOk := checkFirewallRules(port)

	if tcpOk && udpOk {
		log.Printf("Firewall: TCP and UDP port %s are open for inbound connections.", port)
		return
	}

	red := "\033[1;31m"
	reset := "\033[0m"
	fmt.Fprintf(os.Stderr, "%s[ERROR] Firewall may block inbound connections. Please run the following commands in an Administrator terminal:%s\n", red, reset)
	if !tcpOk {
		fmt.Fprintf(os.Stderr, "%s  netsh advfirewall firewall add rule name=\"pantyhose-tcp\" dir=in action=allow protocol=TCP localport=%s%s\n", red, port, reset)
	}
	if !udpOk {
		fmt.Fprintf(os.Stderr, "%s  netsh advfirewall firewall add rule name=\"pantyhose-udp\" dir=in action=allow protocol=UDP localport=%s%s\n", red, port, reset)
	}
	os.Exit(1)
}

func checkFirewallRules(port string) (tcpOk, udpOk bool) {
	out, err := exec.Command("netsh", "advfirewall", "firewall", "show", "rule", "name=all", "dir=in").CombinedOutput()
	if err != nil {
		debugf("Firewall check failed: %v", err)
		return false, false
	}

	// Rule blocks in netsh output are separated by a line of dashes.
	// Port numbers and protocol names (TCP/UDP/Any) are always ASCII
	// regardless of Windows locale, so string matching works reliably.
	sep := strings.Repeat("-", 70)
	blocks := strings.Split(string(out), sep)

	for _, block := range blocks {
		if !strings.Contains(block, port) {
			continue
		}
		if strings.Contains(block, "TCP") {
			tcpOk = true
		}
		if strings.Contains(block, "UDP") {
			udpOk = true
		}
		if tcpOk && udpOk {
			return
		}
	}
	return
}

func printHelpCN() {
	fmt.Printf(`pantyhose %s - SOCKS5 forward proxy server

用法: pantyhose [参数]

参数:
  --addr string        监听地址，可以是 IP 或 host:port 格式 (默认 "0.0.0.0")
  --port int           监听端口，与 --addr 组合使用 (默认 1080)
  --ip string          UDP ASSOCIATE 回复使用的出站 IP（留空则自动检测）
  --user string        SOCKS5 认证用户名（留空则不启用认证）
  --pass string        SOCKS5 认证密码（留空则不启用认证）
  --tcp-timeout int    TCP 连接空闲超时，单位秒 (默认 60)
  --udp-timeout int    UDP 会话超时，单位秒 (默认 60)
  --enable-ipv6        允许 IPv6 出站连接（默认自动检测，不可用时禁用）
  --no-sni-remap       禁用 TLS SNI 域名重解析（默认启用 SNI remap）
  --sni-ports string   应用 SNI remap 的端口列表，逗号分隔 (默认 "443")
  --verbose            启用详细日志（SNI remap 细节、连接生命周期等）
  --fw-clean           输出删除防火墙规则的命令后退出（不启动服务）
  --help-cn            显示本中文帮助信息后退出
  --version            显示版本号后退出

示例:
  pantyhose.exe                                    # 默认配置启动（SNI remap 开启，IPv6 自动检测）
  pantyhose.exe --port 8899                        # 监听 8899 端口
  pantyhose.exe --enable-ipv6                      # 允许 IPv6 出站连接
  pantyhose.exe --no-sni-remap                     # 禁用 SNI 域名重映射
  pantyhose.exe --user admin --pass secret         # 启用用户名密码认证
  pantyhose.exe --fw-clean --port 8899             # 输出清理 8899 端口防火墙规则的命令
`, version)
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

var errIPv6Disabled = fmt.Errorf("IPv6 rejected")

func isIPv6Addr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.To4() == nil
}

func probeIPv6() bool {
	conn, err := net.DialTimeout("tcp6", "[2001:4860:4860::8888]:53", 3*time.Second)
	if err != nil {
		debugf("IPv6 probe failed: %v", err)
		return false
	}
	conn.Close()
	return true
}

func installIPv4OnlyDialers() {
	socks5.DialTCP = func(network, laddr, raddr string) (net.Conn, error) {
		if isIPv6Addr(raddr) {
			debugf("IPv6 destination rejected: %s", raddr)
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
			debugf("IPv6 destination rejected: %s", raddr)
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
