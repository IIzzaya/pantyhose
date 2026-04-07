# Pantyhose - AI Agent Development Guide

## Project Overview

Pantyhose is a forward SOCKS5 proxy server written in Go, using the `txthinking/socks5` library. It runs on a Windows machine with corporate network access (dedicated line) and allows other machines to route TCP/UDP traffic through it.

## Deployment Scenario

The primary use case is cross-network proxying:

- **Proxy machine** (Windows): Sits on a corporate network with a dedicated internet line (can access YouTube, Google, etc. directly). Runs `pantyhose.exe`.
- **Client machine** (macOS/Linux/Windows): Connects to the corporate network via VPN (e.g. CorpLink). Uses [ProxyBridge](https://github.com/InterceptSuite/ProxyBridge) or Proxifier to route all traffic through pantyhose.

**Key challenge**: The client's VPN client (CorpLink) runs a local DNS proxy on `127.0.0.1:53` that returns polluted/fake IPs for certain domains (e.g. YouTube → fake IP). ProxyBridge intercepts traffic at the kernel level and sends already-resolved IPs to the SOCKS5 proxy, not domain names. SNI remap (enabled by default) solves this by extracting the real hostname from TLS ClientHello and re-resolving it via the proxy machine's corporate DNS.

**Recommended startup command** (defaults are sufficient for most cases):
```bash
pantyhose.exe
```

## Architecture

- **Single binary** (`pantyhose.exe`)
- **Library**: `github.com/txthinking/socks5` provides the SOCKS5 protocol implementation
- **Protocol**: SOCKS5 with CONNECT (TCP) and UDP ASSOCIATE support
- **Auth**: Runtime switchable — no-auth (default) or username/password (RFC 1929)
- **SNI Remap**: TLS SNI sniffing enabled by default to fix client-side DNS pollution — extracts hostname from ClientHello and re-resolves via local DNS. Disable with `--no-sni-remap`
- **IPv4-only mode**: IPv6 auto-detected at startup; if unavailable, IPv4-only mode is enabled automatically. Use `--enable-ipv6` to force IPv6 support

## Key Design Decisions

1. All configuration via CLI flags, no config files
2. Outbound IP auto-detected via `net.Dial("udp", "8.8.8.8:53")`, overridable with `--ip`
3. `NewClassicServer` + `SNIRemapHandler` by default; `DefaultHandle` when `--no-sni-remap` is specified
4. Firewall warning printed at startup (does not auto-modify system settings)
5. Graceful shutdown via OS signal capture + `Server.Shutdown()`
6. IPv4-only dialers override `socks5.DialTCP` / `socks5.DialUDP` package-level variables

## Build & Test Commands

```bash
# Build
go build -o pantyhose.exe .

# Build (kill running process first — required on Windows if pantyhose.exe is in use)
Stop-Process -Name pantyhose -Force -ErrorAction SilentlyContinue; Start-Sleep -Seconds 1; go build -o pantyhose.exe . 2>&1

# Run tests (unit + integration)
go test -v -count=1 -timeout 60s ./...

# Run the server (default: SNI remap on, IPv6 auto-detected)
./pantyhose.exe

# Run the server (with auth)
./pantyhose.exe --user admin --pass secret

# Run the server on a custom port
./pantyhose.exe --port 8899

# Run the server (basic, custom addr)
./pantyhose.exe --addr 0.0.0.0 --port 9090
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
| `--addr` | `0.0.0.0` | Listen address (IP or host:port) |
| `--port` | `1080` | Listen port (combined with `--addr`) |
| `--ip` | auto-detected | Outbound IP for UDP ASSOCIATE replies |
| `--user` | _(empty)_ | SOCKS5 username (no auth if empty) |
| `--pass` | _(empty)_ | SOCKS5 password (no auth if empty) |
| `--tcp-timeout` | `60` | TCP idle timeout (seconds) |
| `--udp-timeout` | `60` | UDP session timeout (seconds) |
| `--enable-ipv6` | `false` | Allow IPv6 outbound. By default IPv6 is auto-detected and disabled if unavailable. Overrides `socks5.DialTCP` and `socks5.DialUDP` package-level vars when IPv4-only. |
| `--no-sni-remap` | `false` | Disable `SNIRemapHandler`. SNI remap is enabled by default: for TLS connections on configured ports, read TLS ClientHello, extract SNI hostname, re-resolve via local DNS, connect to correct IP. Other traffic delegated to `DefaultHandle`. |
| `--sni-ports` | `"443"` | Comma-separated list of ports to apply SNI remap. Only used when SNI remap is active (not disabled by `--no-sni-remap`). Parsed by `parsePorts()` into `map[uint16]bool` and stored in `SNIRemapHandler.Ports`. |
| `--verbose` | `false` | Enable verbose logging: SNI passthrough details, DNS resolution results, connection lifecycle. Without this, only actual IP remaps and errors are logged. |
| `--version` | — | Print version and exit |

## Git Workflow

**IMPORTANT**: Follow these rules for all code changes:

1. **Author identity**: All AI agent commits **must** use the model name as the author with no email. Use `git commit --author="<model_name> <>"` for every commit. For example: `git commit --author="Opus 4.6 <>"`, `git commit --author="Claude 3.5 Sonnet <>"`. **Never** include a real email address in AI agent commits.
2. **Auto-commit after each milestone**: After completing a feature, bug fix, or refactoring, **automatically commit without asking the user for confirmation**. Do not wait for a second confirmation — just run tests, and if they pass, commit immediately as part of the wrap-up.
3. **Commit message format**: Use conventional style — e.g. `feat: add TLS support`, `fix: handle nil IP in detection`, `test: add UDP proxy test`
4. **Test before committing**: Run `go test ./...` and ensure all tests pass before creating a commit
5. **One logical change per commit**: Don't bundle unrelated changes
6. **Update TODO.md**: Mark tasks complete and add new tasks as they arise
7. **Update documentation**: If a change affects usage (new flags, behavior changes), update README.md and AGENTS.md accordingly

## Adding New Features

When adding a new feature:

1. Add a task to `TODO.md`
2. Implement the feature in `main.go` (or create a new file if complexity warrants it)
3. Add corresponding tests in `main_test.go` (or a new `*_test.go` file)
4. Update CLI `--help` via `flag` definitions
5. Update `README.md` with usage examples
6. Run tests, then commit with a descriptive message
7. Mark the task complete in `TODO.md`
