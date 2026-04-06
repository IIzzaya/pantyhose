# Pantyhose - AI Agent Development Guide

## Project Overview

Pantyhose is a forward SOCKS5 proxy server written in Go, using the `txthinking/socks5` library. It runs on a Windows machine and allows other LAN machines to route TCP/UDP traffic through it (e.g. via Proxifier).

## Architecture

- **Single binary** (`pantyhose.exe`) — all logic lives in `main.go`
- **Library**: `github.com/txthinking/socks5` provides the SOCKS5 protocol implementation
- **Protocol**: SOCKS5 with CONNECT (TCP) and UDP ASSOCIATE support
- **Auth**: Runtime switchable — no-auth (default) or username/password (RFC 1929)

## Key Design Decisions

1. All configuration via CLI flags, no config files
2. Outbound IP auto-detected via `net.Dial("udp", "8.8.8.8:53")`, overridable with `--ip`
3. `NewClassicServer` + `DefaultHandle` (nil handler) for standard proxy behavior
4. Firewall warning printed at startup (does not auto-modify system settings)
5. Graceful shutdown via OS signal capture + `Server.Shutdown()`

## Build & Test Commands

```bash
# Build
go build -o pantyhose.exe .

# Run tests (unit + integration)
go test -v -count=1 -timeout 30s ./...

# Run the server (no auth)
./pantyhose.exe --addr 0.0.0.0:1080

# Run the server (with auth)
./pantyhose.exe --addr 0.0.0.0:1080 --user admin --pass secret
```

## File Structure

| File | Purpose |
|------|---------|
| `main.go` | Entry point, CLI parsing, server lifecycle, IP detection, firewall warning |
| `main_test.go` | Unit tests (IP detection, error classification) + integration tests (TCP proxy, auth) |
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

- **Unit tests**: Pure logic (IP detection, error classification, flag parsing)
- **Integration tests**: Start real SOCKS5 server on `127.0.0.1` with random port, verify TCP proxy and auth
- **E2E tests**: Manual, using WSL `curl --socks5` (documented in README.md)
- Always run `go test ./...` before committing

## Git Workflow

**IMPORTANT**: Follow these rules for all code changes:

1. **Always commit changes**: Every feature addition, bug fix, or refactoring must be committed with a clear commit message
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
