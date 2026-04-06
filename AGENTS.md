# Pantyhose - AI Agent Development Guide

## Project Overview

Pantyhose is a forward SOCKS5 proxy server written in Go, using the `txthinking/socks5` library. It runs on a Windows machine with corporate network access (dedicated line) and allows other machines to route TCP/UDP traffic through it.

## Deployment Scenario

The primary use case is cross-network proxying:

- **Proxy machine** (Windows): Sits on a corporate network with a dedicated internet line (can access YouTube, Google, etc. directly). Runs `pantyhose.exe`.
- **Client machine** (macOS/Linux/Windows): Connects to the corporate network via VPN (e.g. CorpLink). Uses [ProxyBridge](https://github.com/InterceptSuite/ProxyBridge) or Proxifier to route all traffic through pantyhose.

**Key challenge**: The client's VPN client (CorpLink) runs a local DNS proxy on `127.0.0.1:53` that returns polluted/fake IPs for certain domains (e.g. YouTube → fake IP). ProxyBridge intercepts traffic at the kernel level and sends already-resolved IPs to the SOCKS5 proxy, not domain names. The `--sni-remap` flag solves this by extracting the real hostname from TLS ClientHello and re-resolving it via the proxy machine's corporate DNS.

**Recommended startup command**:
```bash
pantyhose.exe --no-ipv6 --sni-remap
```

## Architecture

- **Single binary** (`pantyhose.exe`)
- **Library**: `github.com/txthinking/socks5` provides the SOCKS5 protocol implementation
- **Protocol**: SOCKS5 with CONNECT (TCP) and UDP ASSOCIATE support
- **Auth**: Runtime switchable — no-auth (default) or username/password (RFC 1929)
- **SNI Remap**: Optional TLS SNI sniffing to fix client-side DNS pollution — extracts hostname from ClientHello and re-resolves via local DNS
- **IPv4-only mode**: Optional `--no-ipv6` to reject IPv6 destinations and force IPv4 outbound

## Key Design Decisions

1. All configuration via CLI flags, no config files
2. Outbound IP auto-detected via `net.Dial("udp", "8.8.8.8:53")`, overridable with `--ip`
3. `NewClassicServer` + `DefaultHandle` (nil handler) for standard proxy behavior; `SNIRemapHandler` when `--sni-remap` is enabled
4. Firewall warning printed at startup (does not auto-modify system settings)
5. Graceful shutdown via OS signal capture + `Server.Shutdown()`
6. IPv4-only dialers override `socks5.DialTCP` / `socks5.DialUDP` package-level variables

## Build & Test Commands

```bash
# Build
go build -o pantyhose.exe .

# Run tests (unit + integration)
go test -v -count=1 -timeout 60s ./...

# Run the server (recommended for cross-network proxying)
./pantyhose.exe --no-ipv6 --sni-remap

# Run the server (with auth)
./pantyhose.exe --no-ipv6 --sni-remap --user admin --pass secret

# Run the server (basic, no special handling)
./pantyhose.exe --addr 0.0.0.0:1080
```

## File Structure

| File | Purpose |
|------|---------|
| `main.go` | Entry point, CLI parsing, server lifecycle, IP detection, firewall warning, IPv4-only dialers |
| `sni.go` | SNI remap handler: TLS ClientHello parser, DNS re-resolution, custom SOCKS5 handler |
| `main_test.go` | Unit tests + integration tests for core proxy functionality |
| `sni_test.go` | Unit tests (SNI parser) + integration tests (SNI remap handler with TLS) |
| `go.mod` / `go.sum` | Go module dependencies |
| `AGENTS.md` | This file — AI agent development guidance |
| `README.md` | Human-facing usage documentation |
| `TODO.md` | Development kanban / task tracking |

## Code Conventions

- Language: Go, follow standard `gofmt` formatting
- All user-facing log messages must be in **English**
- Use `log.Printf` / `log.Println` for logging (no external logging libraries)
- Error handling: use `log.Fatalf` for fatal startup errors, return errors otherwise
- Test naming: `TestXxx` for unit tests, `TestTCPProxyXxx` / `TestServerXxx` for integration

## Testing Strategy

- **Unit tests**: Pure logic (IP detection, error classification, SNI parsing, IPv6 detection)
- **Integration tests**: Start real SOCKS5 server on `127.0.0.1` with random port, verify TCP proxy, auth, SNI remap with TLS, and non-TLS passthrough
- **E2E tests**: Manual, using WSL `curl --socks5` or from another machine via ProxyBridge (documented in README.md)
- Always run `go test ./...` before committing

## Flags Reference

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `0.0.0.0:1080` | Listen address |
| `--ip` | auto-detected | Outbound IP for UDP ASSOCIATE replies |
| `--user` | _(empty)_ | SOCKS5 username (no auth if empty) |
| `--pass` | _(empty)_ | SOCKS5 password (no auth if empty) |
| `--tcp-timeout` | `60` | TCP idle timeout (seconds) |
| `--udp-timeout` | `60` | UDP session timeout (seconds) |
| `--no-ipv6` | `false` | Reject IPv6 destinations, force `tcp4`/`udp4` outbound. Overrides `socks5.DialTCP` and `socks5.DialUDP` package-level vars. |
| `--sni-remap` | `false` | Enable `SNIRemapHandler`: for port 443 connections, read TLS ClientHello, extract SNI hostname, re-resolve via local DNS, connect to correct IP. Non-443 traffic delegated to `DefaultHandle`. |
| `--version` | — | Print version and exit |

## Git Workflow

**IMPORTANT**: Follow these rules for all code changes:

1. **Auto-commit after each milestone**: After completing a feature, bug fix, or refactoring, **automatically commit without asking the user for confirmation**. Do not wait for a second confirmation — just run tests, and if they pass, commit immediately as part of the wrap-up.
2. **Commit message format**: Use conventional style — e.g. `feat: add TLS support`, `fix: handle nil IP in detection`, `test: add UDP proxy test`
3. **Test before committing**: Run `go test ./...` and ensure all tests pass before creating a commit
4. **One logical change per commit**: Don't bundle unrelated changes
5. **Update TODO.md**: Mark tasks complete and add new tasks as they arise
6. **Update documentation**: If a change affects usage (new flags, behavior changes), update README.md and AGENTS.md accordingly

## Adding New Features

When adding a new feature:

1. Add a task to `TODO.md`
2. Implement the feature in `main.go` (or create a new file if complexity warrants it)
3. Add corresponding tests in `main_test.go` (or a new `*_test.go` file)
4. Update CLI `--help` via `flag` definitions
5. Update `README.md` with usage examples
6. Run tests, then commit with a descriptive message
7. Mark the task complete in `TODO.md`
