# Pantyhose - Development Kanban

## Completed

- [x] Project scaffolding (Go module, dependencies)
- [x] Core SOCKS5 server (TCP + UDP, NewClassicServer + DefaultHandle)
- [x] CLI flags (--addr, --ip, --tcp-timeout, --udp-timeout, --version)
- [x] Outbound IP auto-detection with --ip override
- [x] Firewall warning at startup
- [x] Graceful shutdown (signal handling + Server.Shutdown)
- [x] Unit tests (IP detection, error classification)
- [x] Integration tests (TCP proxy, raw TCP echo, server shutdown)
- [x] AGENTS.md (AI agent guidance + git workflow)
- [x] README.md (usage, build, client setup, WSL verification)
- [x] TODO.md (this file)
- [x] --no-ipv6 flag: reject IPv6 destinations, force IPv4-only outbound
- [x] IPv6 address detection and IPv4-only dialer tests
- [x] --sni-remap flag: TLS SNI sniffing + DNS re-resolution to fix client-side DNS pollution
- [x] SNI parser unit tests + SNI remap integration tests (TLS + non-TLS passthrough)
- [x] Suppress noisy "failed to read ClientHello: EOF" logs for aborted connections
- [x] Restructure project into cmd/ layout (pantyhose-server / pantyhose-client)
- [x] Remove SOCKS5 username/password authentication (replaced by mTLS)
- [x] Certificate generation: internal/certgen + `pantyhose-server gencert` subcommand
- [x] TLS+yamux tunnel server (internal/tunnel, mTLS, multiplexed streams)
- [x] TLS+yamux tunnel client (auto-reconnect with exponential backoff)
- [x] pantyhose-client: local SOCKS5 proxy forwarding through TLS tunnel
- [x] --insecure flag: default TLS mode, opt-in unencrypted mode
- [x] End-to-end integration tests (HTTP, HTTPS/SNI, raw TCP through TLS tunnel)
- [x] Dockerfile (multi-stage: server, client, test targets)
- [x] docker-compose.test.yml + Makefile for Docker-based testing
- [x] GitHub Actions: cross-platform builds (Windows, macOS amd64/arm64, Linux)

## In Progress

_(none)_

## Backlog

- [ ] IP whitelist / access control
- [ ] Connection logging with client IP and destination
- [ ] Rate limiting
- [ ] Run as Windows service (background daemon)
- [ ] UDP proxy integration tests
- [ ] Metrics / statistics endpoint
- [ ] Configuration file support (JSON/YAML) as alternative to CLI flags
- [ ] HTTP CONNECT proxy support (in addition to SOCKS5)
