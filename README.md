# Pantyhose

A lightweight SOCKS5 forward proxy server written in Go. Run it on a machine with network access (e.g. a corporate dedicated line), and route another machine's entire network traffic through it using [ProxyBridge](https://github.com/InterceptSuite/ProxyBridge), [Proxifier](https://www.proxifier.com/), or similar tools.

Built with [txthinking/socks5](https://github.com/txthinking/socks5). Supports TCP (CONNECT) and UDP (UDP ASSOCIATE) with optional username/password authentication, IPv6 filtering, and TLS SNI-based DNS remapping.

## Use Case

```
┌──────────────────────┐         VPN / LAN         ┌─────────────────────────┐
│  macOS / Linux / Win │  ──────────────────────►   │  Windows (corporate)    │
│                      │                            │                         │
│  ProxyBridge/        │   SOCKS5 (TCP+UDP)         │  pantyhose.exe          │
│  Proxifier           │   ──────────────────────►  │  --no-ipv6 --sni-remap  │
│                      │                            │         │               │
│  All traffic proxied │                            │         ▼               │
│                      │                            │  Corporate dedicated    │
│                      │                            │  line (internet access) │
└──────────────────────┘                            └─────────────────────────┘
```

**Typical scenario**: Your corporate Windows machine has unrestricted internet via a dedicated line. Your personal macOS/Linux machine connects to the corporate network via VPN but has restricted/polluted DNS. Pantyhose bridges the gap — your personal machine routes all traffic through the corporate machine's network.

## Quick Start

```bash
# Basic usage (no auth, default port 1080)
pantyhose.exe

# Recommended for cross-network proxying with DNS pollution
pantyhose.exe --no-ipv6 --sni-remap

# With authentication
pantyhose.exe --no-ipv6 --sni-remap --user admin --pass secret

# Custom address and port
pantyhose.exe --addr 0.0.0.0:9090 --no-ipv6 --sni-remap
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
  --no-ipv6            Reject IPv6 destinations, force IPv4-only outbound
  --sni-remap          Sniff TLS SNI and re-resolve hostnames via local DNS
  --version            Print version and exit
```

### Authentication Modes

- **No auth**: Omit `--user` and `--pass` (or leave them empty)
- **Username/Password**: Provide both `--user` and `--pass`

## Flags Explained

### `--no-ipv6`

**Problem**: The client machine's DNS may resolve domains to IPv6 addresses, but the proxy machine has no IPv6 connectivity. Without this flag, connections to IPv6 destinations will hang for ~30 seconds before timing out.

**Solution**: When enabled, pantyhose immediately rejects any IPv6 destination and forces all outbound connections to use IPv4 (`tcp4`/`udp4`).

**When to use**: When the proxy machine lacks IPv6 internet connectivity (common in corporate networks). Check by running `ping -6 google.com` on the proxy machine — if it fails, use this flag.

### `--sni-remap`

**Problem**: The client machine's DNS is polluted (e.g. by a VPN client like CorpLink that returns fake IPs for certain domains). Since tools like [ProxyBridge](https://github.com/InterceptSuite/ProxyBridge) intercept traffic at the kernel level, they send already-resolved IP addresses — not domain names — to the SOCKS5 proxy. The proxy connects to the fake IP and fails.

**Solution**: When enabled, pantyhose intercepts HTTPS connections (port 443) and reads the TLS ClientHello to extract the SNI (Server Name Indication) hostname. It then re-resolves that hostname using the proxy machine's local DNS (which returns correct IPs) and connects to the correct destination.

**How it works**:
```
1. Client DNS (polluted):  youtube.com → 185.45.5.35 (fake IP)
2. ProxyBridge sends:      CONNECT 185.45.5.35:443
3. Pantyhose reads TLS ClientHello → SNI = "www.youtube.com"
4. Pantyhose re-resolves:  youtube.com → 142.251.10.91 (correct IP via corporate DNS)
5. Pantyhose connects to:  142.251.10.91:443 ✓
```

**When to use**: When the client machine uses a VPN client (e.g. CorpLink) that runs a local DNS proxy returning polluted/fake IPs for certain domains, and you cannot change the client's DNS settings.

**Limitation**: Only works for HTTPS traffic (port 443) since it relies on TLS SNI. Non-TLS traffic on port 443 is handled gracefully (falls back to direct connection). HTTP and other non-443 traffic uses the default handler without SNI sniffing.

### Combining Flags

For cross-network proxying with a VPN client that pollutes DNS:

```bash
pantyhose.exe --no-ipv6 --sni-remap
```

This is the **recommended configuration** for the typical use case where:
- The proxy machine is on a corporate network with IPv4-only internet
- The client machine connects via VPN with a DNS-polluting VPN client
- You want to route all client traffic through the corporate network

## Client Setup

### ProxyBridge (Recommended)

[ProxyBridge](https://github.com/InterceptSuite/ProxyBridge) is a cross-platform Proxifier alternative that intercepts all TCP/UDP traffic at the kernel level.

1. Install ProxyBridge on the client machine (macOS/Windows/Linux)
2. Configure proxy:
   - Type: **SOCKS5**
   - Address: `<pantyhose machine IP>` (e.g. `10.154.38.77`)
   - Port: `1080` (or your custom port)
3. Set default rule to **Proxy** to route all traffic
4. Exclude the VPN client process from proxying to avoid loops

**Important**: When using ProxyBridge with `--sni-remap`, also disable IPv6 on the client machine if possible. This prevents the client from resolving domains to IPv6 addresses that the proxy can't reach:

```powershell
# Windows client
reg add "HKLM\SYSTEM\CurrentControlSet\Services\Tcpip6\Parameters" /v DisabledComponents /t REG_DWORD /d 0xFF /f

# macOS client
sudo networksetup -setv6off Wi-Fi
```

### Proxifier

1. Install [Proxifier](https://www.proxifier.com/)
2. Go to **Profile > Proxy Servers > Add**
3. Enter:
   - Address: `<pantyhose machine IP>`
   - Port: `1080`
   - Protocol: **SOCKS Version 5**
   - Authentication: enter username/password if configured
4. Set up **Proxification Rules** to route desired traffic
5. Enable **"Resolve hostnames through proxy"** for full DNS proxying

## Firewall

The server listens on a TCP+UDP port. Windows Firewall may block inbound connections. Run these commands **as Administrator** on the proxy machine:

```powershell
# TCP (required for all connections)
netsh advfirewall firewall add rule name="pantyhose-tcp" dir=in action=allow protocol=TCP localport=1080

# UDP (required for UDP ASSOCIATE / QUIC / DNS proxying)
netsh advfirewall firewall add rule name="pantyhose-udp" dir=in action=allow protocol=UDP localport=1080
```

Replace `1080` with your actual port if different. **Both rules are required** — missing the UDP rule will cause QUIC/HTTP3 traffic to fail with "The peer closed the flow" errors on the client.

## Troubleshooting

### Client can't connect at all
- Verify the proxy machine's IP is reachable from the client: `ping <proxy-ip>`
- Check that both TCP and UDP firewall rules are in place (see Firewall section)
- Ensure pantyhose is running and listening: check for `SOCKS5 server listening on ...` in logs

### IPv6 connection timeouts
```
dial tcp [2404:6800:4012:6::200e]:443: connectex: A connection attempt failed...
```
The proxy machine has no IPv6 connectivity. Add `--no-ipv6` flag.

### DNS pollution (wrong IPs, sites fail that work on proxy machine)
```
dial tcp4 185.45.5.35:443: connectex: A connection attempt failed...
```
The client's DNS returns fake IPs. Add `--sni-remap` flag. Verify by comparing DNS results:
```bash
# On client machine
nslookup www.youtube.com
# On proxy machine
nslookup www.youtube.com
```
If they return different IPs, DNS pollution is the cause.

### "The peer closed the flow" (ProxyBridge)
UDP firewall rule is missing. Add the UDP rule (see Firewall section).

### Some sites work on proxy machine but not through proxy
- If those sites use HTTPS: ensure `--sni-remap` is enabled
- If those sites use HTTP only: the client's DNS returns a fake IP and there's no SNI to extract. Consider adding the correct IP to the client's `/etc/hosts`

## Testing

### Automated Tests

```bash
go test -v -count=1 -timeout 60s ./...
```

### Manual Verification via WSL or Another Machine

```bash
# Test TCP proxy (no auth)
curl --socks5 <host-ip>:1080 http://httpbin.org/ip

# Test TCP proxy (with auth)
curl --socks5 <host-ip>:1080 --proxy-user admin:secret http://httpbin.org/ip

# Test DNS resolution through proxy
curl --socks5-hostname <host-ip>:1080 http://httpbin.org/ip

# Test HTTPS through proxy
curl --socks5 <host-ip>:1080 https://www.google.com -o /dev/null -w "%{http_code}\n"
```

Replace `<host-ip>` with the proxy machine's LAN IP (shown in startup logs as "Using outbound IP: x.x.x.x").

## License

MIT
