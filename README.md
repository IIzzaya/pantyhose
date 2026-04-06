# Pantyhose

A lightweight SOCKS5 forward proxy server written in Go. Run it on a machine with network access (e.g. a corporate dedicated line), and route another machine's entire network traffic through it using [ProxyBridge](https://github.com/InterceptSuite/ProxyBridge), [Proxifier](https://www.proxifier.com/), or similar tools.

Built with [txthinking/socks5](https://github.com/txthinking/socks5). Supports TCP (CONNECT) and UDP (UDP ASSOCIATE) with optional username/password authentication. TLS SNI-based DNS remapping is enabled by default; IPv6 is auto-detected.

## Use Case

``` txt
┌──────────────────────┐                            ┌─────────────────────────┐
│  macOS / Linux / Win │                            │  Windows (corporate)    │
│                      │          VPN / LAN         │                         │
│  ProxyBridge/        │   ──────────────────────►  │  pantyhose.exe          │
│  Proxifier           │                            │  (defaults: SNI on,IPv6 auto)│
│                      │       SOCKS5 (TCP+UDP)     │         │               │
│  All traffic proxied │   ──────────────────────►  │         ▼               │
│                      │                            │  Corporate dedicated    │
│                      │                            │  line (internet access) │
└──────────────────────┘                            └─────────────────────────┘
```

**Typical scenario**: Your corporate Windows machine has unrestricted internet via a dedicated line. Your personal macOS/Linux machine connects to the corporate network via VPN but has restricted/polluted DNS. Pantyhose bridges the gap — your personal machine routes all traffic through the corporate machine's network.

## Quick Start

```bash
# Default (SNI remap on, IPv6 auto-detected, port 1080)
pantyhose.exe

# With authentication
pantyhose.exe --user admin --pass secret

# Custom port
pantyhose.exe --port 8899

# Force enable IPv6 outbound
pantyhose.exe --enable-ipv6

# Disable SNI remap (plain proxy mode)
pantyhose.exe --no-sni-remap
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
  --addr string        Listen address (default "0.0.0.0")
  --port int           Listen port (default 1080)
  --ip string          Outbound IP for UDP replies (auto-detected if empty)
  --user string        Username for SOCKS5 auth (no auth if empty)
  --pass string        Password for SOCKS5 auth (no auth if empty)
  --tcp-timeout int    TCP idle timeout in seconds (default 60)
  --udp-timeout int    UDP session timeout in seconds (default 60)
  --enable-ipv6        Allow IPv6 outbound (default: auto-disabled if unavailable)
  --no-sni-remap       Disable TLS SNI hostname re-resolution (enabled by default)
  --sni-ports string   Comma-separated ports for SNI remap (default "443")
  --verbose            Enable verbose logging (SNI details, connection lifecycle)
  --version            Print version and exit
```

### Authentication Modes

- **No auth**: Omit `--user` and `--pass` (or leave them empty)
- **Username/Password**: Provide both `--user` and `--pass`

## Flags Explained

### IPv6 Handling (auto / `--enable-ipv6`)

**Default behavior**: At startup, pantyhose probes IPv6 connectivity (TCP to `[2001:4860:4860::8888]:53`). If IPv6 is unavailable, all outbound connections are automatically forced to IPv4 — no flags needed. If IPv6 is available, it is still disabled by default to avoid DNS-polluted IPv6 addresses causing timeouts.

**`--enable-ipv6`**: Forces IPv6 support on, even if the auto-detection would disable it. Use this only if you know the proxy machine has working IPv6 connectivity and you want to allow IPv6 destinations.

**When IPv6 causes problems**: The client machine's DNS may resolve domains to IPv6 addresses, but the proxy machine has no IPv6 route. Without IPv4-only mode, these connections hang for ~30 seconds before timing out. The auto-detection handles this transparently.

### SNI Remap (default on / `--no-sni-remap`)

SNI remap is **enabled by default**. It solves DNS pollution from VPN clients like CorpLink.

**Problem**: The client machine's DNS is polluted (e.g. by a VPN client that returns fake IPs for certain domains). Tools like [ProxyBridge](https://github.com/InterceptSuite/ProxyBridge) intercept traffic at the kernel level and send already-resolved IP addresses — not domain names — to the SOCKS5 proxy. The proxy connects to the fake IP and fails.

**Solution**: Pantyhose intercepts HTTPS connections (port 443 by default) and reads the TLS ClientHello to extract the SNI (Server Name Indication) hostname. It then re-resolves that hostname using the proxy machine's local DNS (which returns correct IPs) and connects to the correct destination.

**How it works**:
```
1. Client DNS (polluted):  youtube.com → 185.45.5.35 (fake IP)
2. ProxyBridge sends:      CONNECT 185.45.5.35:443
3. Pantyhose reads TLS ClientHello → SNI = "www.youtube.com"
4. Pantyhose re-resolves:  youtube.com → 142.251.10.91 (correct IP via corporate DNS)
5. Pantyhose connects to:  142.251.10.91:443 ✓
```

**`--no-sni-remap`**: Disables SNI remap entirely. Use this if you don't have DNS pollution issues and want a plain SOCKS5 proxy.

**Limitation**: Only works for TLS traffic since it relies on TLS SNI. Non-TLS traffic is handled gracefully (falls back to direct connection).

By default only port 443 is intercepted. Use `--sni-ports` to add custom ports:

```bash
# Also sniff SNI on ports 8443 and 4443
pantyhose.exe --sni-ports 443,8443,4443
```

### Default Configuration

Just running `pantyhose.exe` with no flags gives you the **recommended configuration**:
- SNI remap enabled (fixes DNS pollution)
- IPv6 auto-detected (disabled if unavailable)
- Listening on `0.0.0.0:1080`
- No authentication

## Client Setup

### ProxyBridge (Recommended)

[ProxyBridge](https://github.com/InterceptSuite/ProxyBridge) is a cross-platform Proxifier alternative that intercepts all TCP/UDP traffic at the kernel level.

1. Install ProxyBridge on the client machine (macOS/Windows/Linux)

2. **Proxy Settings**: Go to **Proxy > Proxy Settings...** in the menu bar. Set **Proxy IP/Domain** to the LAN IP of the machine running pantyhose (e.g. `10.154.38.77`), and set **Proxy Port** to the pantyhose listen port (default `1080`).

![ProxyBridge Proxy Settings](proxy-bridge-pic-1.png)

3. **Proxy Rules**: Go to **Proxy > Proxy Rules...** to configure routing rules. VPN-related processes and addresses (e.g. `corplink-service`, `Lark Helper`, `Feishu`, `169.254.169.254`, `172.19.10.252`) should be set to **BOTH** protocol and **DIRECT** action to bypass the proxy. All other traffic should go through the proxy.

![ProxyBridge Proxy Rules](proxy-bridge-pic-2.png)

**Tip**: Disabling IPv6 on the client machine can further reduce DNS pollution issues:

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
The proxy machine has no IPv6 connectivity. This should be auto-detected at startup. If not, the default behavior already disables IPv6. Check startup logs for "IPv6 not available" or "IPv6 available but disabled".

### DNS pollution (wrong IPs, sites fail that work on proxy machine)
```
dial tcp4 185.45.5.35:443: connectex: A connection attempt failed...
```
The client's DNS returns fake IPs. SNI remap is enabled by default and should handle this. Verify by comparing DNS results:
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
- If those sites use HTTPS: ensure SNI remap is active (enabled by default, check logs for "SNI remap enabled")
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
