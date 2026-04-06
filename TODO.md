# Pantyhose - Development Kanban

## Completed

- [x] Project scaffolding (Go module, dependencies)
- [x] Core SOCKS5 server (TCP + UDP, NewClassicServer + DefaultHandle)
- [x] CLI flags (--addr, --ip, --user, --pass, --tcp-timeout, --udp-timeout, --version)
- [x] Auth mode: no-auth and username/password, runtime switchable
- [x] Outbound IP auto-detection with --ip override
- [x] Firewall warning at startup
- [x] Graceful shutdown (signal handling + Server.Shutdown)
- [x] Unit tests (IP detection, error classification)
- [x] Integration tests (TCP proxy no-auth, TCP proxy with auth, auth rejection, raw TCP echo, server shutdown)
- [x] AGENTS.md (AI agent guidance + git workflow)
- [x] README.md (usage, build, client setup, WSL verification)
- [x] TODO.md (this file)

## In Progress

_(none)_

## Backlog

- [ ] TLS/encryption layer for secure proxy connections
- [ ] IP whitelist / access control
- [ ] Connection logging with client IP and destination
- [ ] Rate limiting
- [ ] Run as Windows service (background daemon)
- [ ] UDP proxy integration tests
- [ ] Metrics / statistics endpoint
- [ ] Configuration file support (JSON/YAML) as alternative to CLI flags
