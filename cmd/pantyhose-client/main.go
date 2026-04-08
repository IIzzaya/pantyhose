package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"pantyhose/internal/tunnel"
)

var version = "dev"

func main() {
	enableANSIColors()

	server := flag.String("server", "", "Remote pantyhose-server address (host:port)")
	listen := flag.String("listen", "127.0.0.1:1080", "Local SOCKS5 listen address")
	certFile := flag.String("cert", "certs/client.crt", "Client TLS certificate file")
	keyFile := flag.String("key", "certs/client.key", "Client TLS private key file")
	caFile := flag.String("ca", "certs/ca.crt", "CA certificate file for server verification")
	showVersion := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("pantyhose-client %s\n", version)
		os.Exit(0)
	}

	if *server == "" {
		fmt.Fprintln(os.Stderr, "Usage: pantyhose-client --server <host:port>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Required:")
		fmt.Fprintln(os.Stderr, "  --server   Remote pantyhose-server address (host:port)")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Optional:")
		fmt.Fprintln(os.Stderr, "  --listen   Local SOCKS5 listen address (default: 127.0.0.1:1080)")
		fmt.Fprintln(os.Stderr, "  --cert     Client certificate file (default: certs/client.crt)")
		fmt.Fprintln(os.Stderr, "  --key      Client private key file (default: certs/client.key)")
		fmt.Fprintln(os.Stderr, "  --ca       CA certificate file (default: certs/ca.crt)")
		os.Exit(1)
	}

	var missing []string
	for _, f := range []string{*certFile, *keyFile, *caFile} {
		if _, err := os.Stat(f); err != nil {
			missing = append(missing, f)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintln(os.Stderr, "Certificate files not found:")
		for _, m := range missing {
			fmt.Fprintf(os.Stderr, "  - %s\n", m)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Copy ca.crt, client.crt, client.key from the server machine to ./certs/")
		os.Exit(1)
	}

	client, err := tunnel.NewClient(*server, *certFile, *keyFile, *caFile)
	if err != nil {
		log.Fatalf("Failed to create tunnel client: %v", err)
	}

	log.Printf("Connecting to %s...", *server)
	if err := client.Connect(); err != nil {
		log.Fatalf("Failed to connect to server: %v", err)
	}
	log.Printf("Connected to %s", *server)

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", *listen, err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("Received signal %v, shutting down...", sig)
		ln.Close()
		client.Close()
	}()

	log.Printf("SOCKS5 listening on %s -> tunnel -> %s", *listen, *server)
	green := "\033[1;32m"
	reset := "\033[0m"
	fmt.Fprintf(os.Stderr, "%sClient started. Press Ctrl+C to stop.%s\n", green, reset)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handleLocalConn(conn, client)
	}
}

func handleLocalConn(local net.Conn, client *tunnel.Client) {
	defer local.Close()

	stream, err := client.OpenStream()
	if err != nil {
		log.Printf("Failed to open tunnel stream: %v", err)
		return
	}
	defer stream.Close()

	// SOCKS5 greeting from local client → forward to remote server via stream
	// The remote server handles full SOCKS5 protocol on the stream.
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(stream, local)
		closeWrite(stream)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(local, stream)
		closeWrite(local)
		done <- struct{}{}
	}()
	<-done
}

// closeWrite sends a FIN if the connection supports it.
func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok {
		cw.CloseWrite()
	}
}

