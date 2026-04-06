# Pantyhose

A lightweight SOCKS5 forward proxy server for Windows. Run it on one machine, and route another machine's entire network traffic through it using tools like [Proxifier](https://www.proxifier.com/).

Built with [txthinking/socks5](https://github.com/txthinking/socks5). Supports TCP (CONNECT) and UDP (UDP ASSOCIATE) with optional username/password authentication.

## Quick Start

```bash
# No authentication
pantyhose.exe

# With authentication
pantyhose.exe --user admin --pass secret

# Custom address and port
pantyhose.exe --addr 0.0.0.0:9090 --user admin --pass secret
```

## Installation

### Build from Source

Requires [Go 1.21+](https://go.dev/dl/).

```bash
git clone <repo-url>
cd pantyhose
go build -o pantyhose.exe .
```

### Cross-compile for Linux (optional)

```bash
GOOS=linux GOARCH=amd64 go build -o pantyhose .
```

## Usage

```
pantyhose.exe [flags]

Flags:
  --addr string        Listen address (default "0.0.0.0:1080")
  --ip string          Outbound IP for UDP replies (auto-detected if empty)
  --user string        Username for SOCKS5 auth (no auth if empty)
  --pass string        Password for SOCKS5 auth (no auth if empty)
  --tcp-timeout int    TCP idle timeout in seconds (default 60)
  --udp-timeout int    UDP session timeout in seconds (default 60)
  --version            Print version and exit
```

### Authentication Modes

- **No auth**: Omit `--user` and `--pass` (or leave them empty)
- **Username/Password**: Provide both `--user` and `--pass`

## Client Setup (Proxifier)

On the machine you want to proxy:

1. Install [Proxifier](https://www.proxifier.com/)
2. Go to **Profile > Proxy Servers > Add**
3. Enter:
   - Address: `<pantyhose machine IP>` (e.g. `192.168.1.100`)
   - Port: `1080` (or your custom port)
   - Protocol: **SOCKS Version 5**
   - Authentication: enter username/password if configured
4. Set up **Proxification Rules** to route desired traffic through the proxy
5. Enable **"Resolve hostnames through proxy"** in Settings for full DNS proxying

## Firewall

The server listens on a TCP+UDP port. Windows Firewall may block inbound connections. Run these commands as Administrator:

```powershell
netsh advfirewall firewall add rule name="pantyhose-tcp" dir=in action=allow protocol=TCP localport=1080
netsh advfirewall firewall add rule name="pantyhose-udp" dir=in action=allow protocol=UDP localport=1080
```

Replace `1080` with your actual port if different.

## Testing

### Automated Tests

```bash
go test -v -count=1 -timeout 30s ./...
```

### Manual Verification via WSL

From a WSL terminal on the same machine (or another Linux machine on the LAN):

```bash
# Test TCP proxy (no auth)
curl --socks5 <host-ip>:1080 http://httpbin.org/ip

# Test TCP proxy (with auth)
curl --socks5 <host-ip>:1080 --proxy-user admin:secret http://httpbin.org/ip

# Test DNS resolution through proxy
curl --socks5-hostname <host-ip>:1080 http://httpbin.org/ip
```

Replace `<host-ip>` with the Windows machine's LAN IP (shown in startup logs as "Using outbound IP: x.x.x.x").

## License

MIT
