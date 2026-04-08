# Pantyhose

English | [中文](README.md)

A SOCKS5 forward proxy with encrypted TLS tunnel support, written in Go. It consists of two components:

- **pantyhose-server**: Runs on a machine with internet access (e.g. a corporate Windows machine with a dedicated line), accepting encrypted SOCKS5 connections
- **pantyhose-client**: Runs on the client machine, providing a local SOCKS5 port that tunnels all traffic to the server over a TLS encrypted channel

Built with [txthinking/socks5](https://github.com/txthinking/socks5) for SOCKS5 protocol and [hashicorp/yamux](https://github.com/hashicorp/yamux) for multiplexing. Supports mTLS mutual certificate authentication, TLS SNI remap (fixes DNS pollution), and automatic reconnection.

## Use Case

``` txt
┌──────────────────────┐                                  ┌─────────────────────────┐
│  macOS / Linux / Win │                                  │  Windows (corporate)    │
│                      │          VPN / LAN               │                         │
│  ProxyBridge/        │   ─────────────────────────►     │  pantyhose-server       │
│  Proxifier           │                                  │  (TLS + mTLS auth)      │
│       ↓              │   TLS 1.3 + yamux encrypted      │         │               │
│  pantyhose-client    │   ═══════════════════════════►   │         ▼               │
│  (local SOCKS5)      │                                  │  Dedicated line         │
│                      │                                  │  (internet access)      │
└──────────────────────┘                                  └─────────────────────────┘
```

**Typical scenario**: Your corporate Windows machine has unrestricted internet via a dedicated line (YouTube, Google, etc.). Your personal macOS/Linux machine connects to the corporate network via VPN but has polluted DNS. Pantyhose bridges the gap with an encrypted tunnel, protecting traffic in transit.

## Quick Start

### 1. Server: Generate Certificates

```bash
pantyhose-server gencert --out ./certs/
# Optional: specify server IP/hostname
pantyhose-server gencert --out ./certs/ --hosts "10.0.0.5,proxy.local"
```

Generated files:
- `certs/ca.crt` — CA certificate
- `certs/ca.key` — CA private key (keep secure!)
- `certs/server.crt` / `certs/server.key` — Server certificate/key
- `certs/client.pem` — Client bundle (CA cert + client cert + client key)

### 2. Copy Client Certificate

Copy `client.pem` to the client machine's `certs/` directory.

### 3. Server: Start

```bash
# TLS encrypted mode (default, recommended — certs auto-loaded from ./certs/)
pantyhose-server serve

# Custom port
pantyhose-server serve --port 8899

# Custom certificate paths
pantyhose-server serve --cert /path/to/server.crt --key /path/to/server.key --ca /path/to/ca.crt

# Insecure mode (trusted networks only)
pantyhose-server serve --insecure
```

### 4. Client: Connect

> **macOS users**: Binaries downloaded from GitHub Releases need execute permission:
> ```bash
> chmod +x pantyhose-client-mac-os-apple-silicon   # Apple Silicon (M1/M2/M3/M4)
> chmod +x pantyhose-client-mac-os-intel             # Intel Mac
> ```

```bash
# Connect to server (default listen 127.0.0.1:1080, certs auto-loaded from certs/client.pem)
pantyhose-client --server 10.0.0.5:1080

# Custom local listen address
pantyhose-client --server 10.0.0.5:1080 --listen 127.0.0.1:9090

# Custom PEM file path
pantyhose-client --server 10.0.0.5:1080 --pem /path/to/client.pem
```

### 5. Configure ProxyBridge

Set the proxy address to `127.0.0.1:1080` (pantyhose-client's local port) in ProxyBridge.

> <sub>**Note**: Windows screen timeout does not affect operation, but **sleep/hibernation** will suspend the process. Set "Sleep" to **Never** in **Settings → Power**.</sub>

## Security Architecture

```
pantyhose-client ──[TLS 1.3 + mTLS]──► pantyhose-server
       │                                       │
   Client cert verification              Server cert verification
   (client.pem)                        (server.crt + server.key)
       │                                       │
       └──── Mutual auth, signed by same CA ───┘
```

- **TLS 1.3**: All traffic encrypted end-to-end
- **mTLS**: Server and client mutually verify certificates — no valid cert, no connection
- **yamux multiplexing**: One TLS connection carries multiple SOCKS5 sessions, reducing handshake overhead
- **No password auth**: Security entirely guaranteed by certificates, no SOCKS5 username/password

## Client Configuration

### ProxyBridge (Recommended)

[ProxyBridge](https://github.com/InterceptSuite/ProxyBridge) is a cross-platform Proxifier alternative.

1. Install ProxyBridge
2. **Proxy Settings**: Set proxy address to `127.0.0.1`, port `1080` (pantyhose-client local port)
3. **Proxy Rules**: Set VPN-related processes to DIRECT to bypass the proxy

### Proxifier

1. **Proxy Servers > Add**: Address `127.0.0.1`, port `1080`, protocol SOCKS5
2. Configure routing rules

## Firewall (Server)

```powershell
# TCP (required)
netsh advfirewall firewall add rule name="pantyhose-tcp" dir=in action=allow protocol=TCP localport=1080

# UDP (required for UDP ASSOCIATE)
netsh advfirewall firewall add rule name="pantyhose-udp" dir=in action=allow protocol=UDP localport=1080
```

Cleanup:
```bash
pantyhose-server serve --fw-clean
```

## Development

### Build from Source

Requires [Go 1.21+](https://go.dev/dl/).

```bash
git clone <repo-url>
cd pantyhose

# Build server and client
go build -o pantyhose-server.exe ./cmd/pantyhose-server
go build -o pantyhose-client.exe ./cmd/pantyhose-client

# Run tests
go test -v -count=1 -timeout 60s ./...

# Docker tests
docker compose -f docker-compose.test.yml up --build
```

### Cross-compile

```bash
# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o pantyhose-client-mac-os-apple-silicon ./cmd/pantyhose-client

# macOS (Intel)
GOOS=darwin GOARCH=amd64 go build -o pantyhose-client-mac-os-intel ./cmd/pantyhose-client

# Linux
GOOS=linux GOARCH=amd64 go build -o pantyhose-server-linux ./cmd/pantyhose-server
```

## Server Flags

### `pantyhose-server serve`

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `0.0.0.0` | Listen address |
| `--port` | `1080` | Listen port |
| `--ip` | auto-detected | Outbound IP for UDP ASSOCIATE |
| `--cert` | `certs/server.crt` | Server TLS certificate |
| `--key` | `certs/server.key` | Server TLS private key |
| `--ca` | `certs/ca.crt` | CA certificate (client verification) |
| `--insecure` | `false` | Insecure mode (no TLS) |
| `--tcp-timeout` | `60` | TCP idle timeout (seconds) |
| `--udp-timeout` | `60` | UDP session timeout (seconds) |
| `--enable-ipv6` | `false` | Allow IPv6 outbound |
| `--no-sni-remap` | `false` | Disable SNI remap |
| `--sni-ports` | `"443"` | SNI remap ports |
| `--verbose` | `false` | Verbose logging |
| `--fw-clean` | `false` | Print firewall cleanup commands and exit |
| `--help-cn` | `false` | Show help in Chinese |

### `pantyhose-server gencert`

| Flag | Default | Description |
|------|---------|-------------|
| `--out` | `./certs` | Certificate output directory |
| `--hosts` | _(empty)_ | Additional server IPs/hostnames |
| `--days` | `3650` | Certificate validity (days) |

## Client Flags

### `pantyhose-client`

| Flag | Default | Description |
|------|---------|-------------|
| `--server` | _(required)_ | Remote server address (host:port) |
| `--listen` | `127.0.0.1:1080` | Local SOCKS5 listen address |
| `--pem` | `certs/client.pem` | Client PEM file (CA cert + client cert + client key) |

## Key Features

### TLS Encrypted Tunnel

Enabled by default. Client and server establish an encrypted tunnel via TLS 1.3 + mTLS. All SOCKS5 traffic is transmitted inside the tunnel. Uses yamux multiplexing — one TLS connection carries multiple SOCKS5 sessions.

Auto-reconnect on disconnection (exponential backoff: 1s → 2s → 4s → ... → 60s max).

### SNI Remap (enabled by default)

Fixes VPN DNS pollution. Intercepts HTTPS connections, extracts the SNI hostname from TLS ClientHello, and re-resolves it via the server's DNS.

### IPv6 Handling (disabled by default)

IPv6 outbound is disabled by default to avoid connection timeouts on corporate networks without IPv6 routes. Enable with `--enable-ipv6`.

## Troubleshooting

### Client can't connect to server

- Verify server IP is reachable: `ping <server-ip>`
- Check firewall rules
- Verify certificate file is correct (client.pem)
- Check client logs for TLS errors

### DNS Pollution (SNI remap related)

```bash
# Compare DNS results between client and server
nslookup www.youtube.com  # on client
nslookup www.youtube.com  # on server
```

If results differ, DNS pollution exists. SNI remap handles this by default.

### Certificate Expired

Re-run `pantyhose-server gencert` to generate new certificates, then replace the client's certificate file.

## License

MIT
