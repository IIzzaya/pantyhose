# Pantyhose - AI Agent Development Guide

## Project Overview

Pantyhose is a forward SOCKS5 proxy with encrypted TLS tunnel support, written in Go. It consists of two binaries:

- **pantyhose-server**: Runs on a machine with direct internet access, accepting SOCKS5 connections over TLS+yamux encrypted tunnels (or plain TCP in `--insecure` mode).
- **pantyhose-client**: Runs on the client machine, providing a local SOCKS5 port that transparently tunnels all traffic to the remote server over an encrypted channel.

The project uses `txthinking/socks5` for SOCKS5 protocol handling and `hashicorp/yamux` for multiplexing streams over a single TLS connection.

## Deployment Scenario

The primary use case is cross-network proxying with encryption:

```
[Applications] → [ProxyBridge] → 127.0.0.1:1080 → [pantyhose-client] ══TLS+yamux══> [pantyhose-server] → [Internet]
```

- **Server machine** (Windows/Linux/macOS): Sits on a corporate network with a dedicated internet line. Runs `pantyhose-server`.
- **Client machine** (macOS/Linux/Windows): Connects to the corporate network via VPN (e.g. CorpLink). Runs `pantyhose-client` + [ProxyBridge](https://github.com/InterceptSuite/ProxyBridge).

**Key challenge**: The client's VPN client runs a local DNS proxy that returns polluted/fake IPs for certain domains. ProxyBridge sends already-resolved IPs to the SOCKS5 proxy. SNI remap (enabled by default on the server) extracts the real hostname from TLS ClientHello and re-resolves it via the server's DNS.

**Recommended setup:**
```bash
# Server: generate certificates
pantyhose-server gencert --out ./certs/

# Server: start with TLS
pantyhose-server serve --cert certs/server.crt --key certs/server.key --ca certs/ca.crt

# Client: connect to server (copy ca.crt, client.crt, client.key from server)
pantyhose-client --server <server-ip>:1080 --ca ca.crt --cert client.crt --key client.key

# ProxyBridge: configure proxy as 127.0.0.1:1080
```

## Architecture

- **Two binaries**: `pantyhose-server` and `pantyhose-client`
- **Transport**: TLS 1.3 with mTLS (mutual TLS) for authentication
- **Multiplexing**: `hashicorp/yamux` over TLS — one TLS connection, multiple SOCKS5 sessions
- **Protocol**: SOCKS5 CONNECT (TCP). UDP data is encapsulated over the yamux stream.
- **Certificate management**: Built-in `gencert` subcommand generates CA + server + client certs
- **SNI Remap**: TLS SNI sniffing on the server to fix client-side DNS pollution
- **IPv4-only mode**: IPv6 auto-detected; disabled if unavailable
- **Insecure mode**: `--insecure` flag allows running without TLS (open proxy)

## Key Design Decisions

1. All configuration via CLI flags, no config files
2. Default mode is TLS; `--insecure` required for unencrypted mode
3. SOCKS5 username/password auth removed — security handled by mTLS certificates
4. yamux streams implement `net.Conn`, but socks5 library requires `*net.TCPConn` — TLS mode uses custom SOCKS5 handler (`tls_serve.go`) that works on `net.Conn`
5. Client auto-reconnects with exponential backoff (1s → 60s max)
6. Outbound IP auto-detected via `net.Dial("udp", "8.8.8.8:53")`, overridable with `--ip`
7. Firewall warning printed at startup (does not auto-modify system settings)

## Build & Test Commands

```bash
# Build both binaries
go build -o pantyhose-server.exe ./cmd/pantyhose-server
go build -o pantyhose-client.exe ./cmd/pantyhose-client

# Build (kill running process first on Windows)
Stop-Process -Name pantyhose-server -Force -ErrorAction SilentlyContinue; Start-Sleep -Seconds 1; go build -o pantyhose-server.exe ./cmd/pantyhose-server 2>&1

# Run tests (unit + integration + E2E)
go test -v -count=1 -timeout 60s ./...

# Run tests in Docker
docker compose -f docker-compose.test.yml up --build --abort-on-container-exit

# Generate certificates
./pantyhose-server gencert --out ./certs/

# Run server (TLS mode — default)
./pantyhose-server serve --cert certs/server.crt --key certs/server.key --ca certs/ca.crt

# Run server (insecure mode)
./pantyhose-server serve --insecure

# Run client
./pantyhose-client --server <host>:1080 --ca ca.crt --cert client.crt --key client.key
```

## File Structure

| File / Directory | Purpose |
|------|---------|
| `cmd/pantyhose-server/main.go` | Server entry point, CLI parsing, subcommands (serve, gencert) |
| `cmd/pantyhose-server/sni.go` | SNI remap handler: TLS ClientHello parser, DNS re-resolution |
| `cmd/pantyhose-server/tls_serve.go` | TLS mode SOCKS5 handler for yamux streams |
| `cmd/pantyhose-server/main_test.go` | Server unit tests + insecure mode integration tests |
| `cmd/pantyhose-server/sni_test.go` | SNI parser unit tests + integration tests |
| `cmd/pantyhose-server/tunnel_integration_test.go` | E2E tests: full TLS tunnel chain |
| `cmd/pantyhose-client/main.go` | Client entry point: local SOCKS5 → TLS tunnel forwarding |
| `internal/certgen/certgen.go` | Certificate generation (CA + server + client certs) |
| `internal/certgen/certgen_test.go` | Certificate generation tests |
| `internal/tunnel/server.go` | TLS listener + yamux server (implements net.Listener) |
| `internal/tunnel/client.go` | TLS dialer + yamux client with auto-reconnect |
| `internal/tunnel/tunnel_test.go` | Tunnel unit tests (echo, multi-stream, auth rejection) |
| `Dockerfile` | Multi-stage build: server, client, test targets |
| `docker-compose.test.yml` | Docker-based test runner |
| `Makefile` | Common build/test shortcuts |
| `go.mod` / `go.sum` | Go module dependencies |
| `.github/workflows/release.yml` | GitHub Actions: cross-platform build & release |
| `docs/DESIGN.md` | Design decisions record (Chinese) — all architectural choices with rationale |
| `docs/ARCHITECTURE.md` | System architecture (Chinese) — component diagrams, directory structure, data flow |
| `docs/PROTOCOL.md` | Protocol & security details (Chinese) — TLS, yamux, SOCKS5, SNI remap, reconnection |
| `docs/CERTIFICATES.md` | Certificate management guide (Chinese) — generation, distribution, renewal |
| `AGENTS.md` | This file — AI agent development guidance |
| `README.md` | Human-facing usage documentation (Chinese) |
| `README_EN.md` | Human-facing usage documentation (English) |
| `TODO.md` | Development kanban / task tracking |

## Code Conventions

- Language: Go, follow standard `gofmt` formatting
- All user-facing log messages must be in **English**
- Use `log.Printf` / `log.Println` for logging (no external logging libraries)
- Error handling: use `log.Fatalf` for fatal startup errors, return errors otherwise
- Test naming: `TestXxx` for unit tests, `TestTunnelE2EXxx` for E2E integration tests

## Testing Strategy

- **Unit tests**: Pure logic (IP detection, error classification, SNI parsing, certificate generation)
- **Integration tests**: Start real SOCKS5 server on `127.0.0.1` with random port, verify TCP proxy and SNI remap
- **Tunnel tests**: TLS+yamux echo, multi-stream, mTLS rejection (wrong CA, no cert)
- **E2E tests**: Full chain — SOCKS5 client → tunnel client → TLS tunnel → tunnel server → SOCKS5 handler → HTTP echo
- **Docker tests**: `docker compose -f docker-compose.test.yml up` for containerized testing
- Always run `go test ./...` before committing

## Server Flags Reference

### `pantyhose-server serve` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--addr` | `0.0.0.0` | Listen address |
| `--port` | `1080` | Listen port |
| `--ip` | auto-detected | Outbound IP for UDP ASSOCIATE |
| `--cert` | _(required)_ | Server TLS certificate file |
| `--key` | _(required)_ | Server TLS private key file |
| `--ca` | _(required)_ | CA certificate for client verification |
| `--insecure` | `false` | Run without TLS (open proxy) |
| `--tcp-timeout` | `60` | TCP idle timeout (seconds) |
| `--udp-timeout` | `60` | UDP session timeout (seconds) |
| `--enable-ipv6` | `false` | Allow IPv6 outbound |
| `--no-sni-remap` | `false` | Disable SNI remap |
| `--sni-ports` | `"443"` | Ports to apply SNI remap |
| `--verbose` | `false` | Enable verbose logging |
| `--version` | — | Print version and exit |

### `pantyhose-server gencert` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--out` | `./certs` | Output directory |
| `--hosts` | _(empty)_ | Additional server hostnames/IPs |
| `--days` | `3650` | Certificate validity in days |

### `pantyhose-client` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--server` | _(required)_ | Remote server address (host:port) |
| `--listen` | `127.0.0.1:1080` | Local SOCKS5 listen address |
| `--cert` | _(required)_ | Client TLS certificate file |
| `--key` | _(required)_ | Client TLS private key file |
| `--ca` | _(required)_ | CA certificate file |
| `--version` | — | Print version and exit |

## Git Workflow

**IMPORTANT**: Follow these rules for all code changes:

1. **Author & Committer identity**: All AI agent commits **must** set both Author and Committer to `<ModelName>` with no email. Set environment variables before committing:
   ```bash
   GIT_AUTHOR_NAME="<ModelName>" GIT_AUTHOR_EMAIL="" GIT_COMMITTER_NAME="<ModelName>" GIT_COMMITTER_EMAIL="" git commit -m "feat: ..."
   ```
   In PowerShell:
   ```powershell
   $env:GIT_AUTHOR_NAME="<ModelName>"; $env:GIT_AUTHOR_EMAIL=""; $env:GIT_COMMITTER_NAME="<ModelName>"; $env:GIT_COMMITTER_EMAIL=""; git commit -m "feat: ..."
   ```
   Replace `<ModelName>` with the actual model name (e.g. `Claude Sonnet 3.5`, `Gemini 2.5 Pro`). **Claude Opus 4.6 naming rule**: Any variant of this model name (`Claude Opus 4.6`, `Anthropic: Claude Opus 4.6`, `claude-opus-4.6`, etc.) **must** be unified as `Opus 4.6` to match the existing commit history. **Never** include a real email address. Using only `--author` is **insufficient** — it leaves the Committer as the system default.
2. **Auto-commit after each milestone**: After completing a feature, bug fix, or refactoring, **automatically commit without asking the user for confirmation**. Do not wait for a second confirmation — just run tests, and if they pass, commit immediately as part of the wrap-up.
3. **Commit message format**: Use conventional style — e.g. `feat: add TLS support`, `fix: handle nil IP in detection`, `test: add UDP proxy test`
4. **Test before committing**: Run `go test ./...` and ensure all tests pass before creating a commit
5. **One logical change per commit**: Don't bundle unrelated changes
6. **Update TODO.md**: Mark tasks complete and add new tasks as they arise
7. **Update documentation**: If a change affects usage (new flags, behavior changes), update README.md and AGENTS.md accordingly
8. **Never push to remote automatically**: After committing, do **not** run `git push`. Pushing to the remote repository is left to the user for manual review, unless the user explicitly asks to push

## Release Workflow

Releases are automated via GitHub Actions (`.github/workflows/release.yml`).

**How to release:**
1. Run `go test ./...` locally and ensure all tests pass
2. Tag the commit: `git tag v2.0.0`
3. Push the tag: `git push origin v2.0.0`
4. GitHub Actions builds all platforms with version injected via `-ldflags "-X main.version=..."`
5. A GitHub Release is created with 8 binaries (server + client × 4 platforms)

**Release artifacts:**
- `pantyhose-server-windows-amd64.exe`
- `pantyhose-server-darwin-amd64` / `pantyhose-server-darwin-arm64`
- `pantyhose-server-linux-amd64`
- `pantyhose-client-windows-amd64.exe`
- `pantyhose-client-darwin-amd64` / `pantyhose-client-darwin-arm64`
- `pantyhose-client-linux-amd64`

**Version embedding:** The `version` variable in each `main.go` defaults to `"dev"`. Release builds inject the real version from the git tag via `ldflags`. Never hardcode a version number in source.

## Adding New Features

When adding a new feature:

1. Add a task to `TODO.md`
2. Implement in the appropriate package (`cmd/pantyhose-server/`, `cmd/pantyhose-client/`, or `internal/`)
3. Add corresponding tests
4. Update CLI `--help` via `flag` definitions
5. Update `README.md` with usage examples
6. Run tests, then commit with a descriptive message
7. Mark the task complete in `TODO.md`
