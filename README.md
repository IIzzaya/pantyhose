# Pantyhose

[English](README_EN.md) | 中文

一个带加密 TLS 隧道的 SOCKS5 forward proxy，使用 Go 编写。由两个组件组成：

- **pantyhose-server**：运行在有网络出口的机器上（如公司专线 Windows 电脑），接受加密的 SOCKS5 连接
- **pantyhose-client**：运行在客户端机器上，提供本地 SOCKS5 端口，所有流量通过 TLS 加密隧道转发到服务端

基于 [txthinking/socks5](https://github.com/txthinking/socks5) 构建 SOCKS5 协议，使用 [hashicorp/yamux](https://github.com/hashicorp/yamux) 实现多路复用。支持 mTLS 双向证书认证、TLS SNI remap（修复 DNS 污染）、自动重连。

## 使用场景

``` txt
┌──────────────────────┐                                  ┌─────────────────────────┐
│  macOS / Linux / Win │                                  │  Windows (公司内网)     │
│                      │          VPN / LAN               │                         │
│  ProxyBridge/        │   ─────────────────────────►     │  pantyhose-server       │
│  Proxifier           │                                  │  (TLS + mTLS 认证)      │
│       ↓              │   TLS 1.3 + yamux 加密隧道       │         │               │
│  pantyhose-client    │   ═══════════════════════════►   │         ▼               │
│  (本地 SOCKS5)       │                                  │  公司专线               │
│                      │                                  │  (可访问外网)            │
└──────────────────────┘                                  └─────────────────────────┘
```

**典型场景**：公司 Windows 电脑通过专线可以直接访问外网（YouTube、Google 等）。个人 macOS/Linux 电脑通过 VPN 连接公司内网，但 DNS 被污染。Pantyhose 通过加密隧道将两者打通，同时保护传输安全。

## 快速开始

### 1. 服务端：生成证书

```bash
pantyhose-server gencert --out ./certs/
# 可选：指定服务端 IP/域名
pantyhose-server gencert --out ./certs/ --hosts "10.0.0.5,proxy.local"
```

生成的文件：
- `certs/ca.crt` — CA 证书
- `certs/ca.key` — CA 私钥（保管好！）
- `certs/server.crt` / `certs/server.key` — 服务端证书/私钥
- `certs/client.crt` / `certs/client.key` — 客户端证书/私钥

### 2. 拷贝客户端证书

将以下 3 个文件拷贝到客户端机器：
- `ca.crt`
- `client.crt`
- `client.key`

### 3. 服务端：启动

```bash
# TLS 加密模式（默认，推荐）
pantyhose-server serve --cert certs/server.crt --key certs/server.key --ca certs/ca.crt

# 自定义端口
pantyhose-server serve --port 8899 --cert certs/server.crt --key certs/server.key --ca certs/ca.crt

# 非加密模式（仅限可信网络）
pantyhose-server serve --insecure
```

### 4. 客户端：连接

```bash
# 连接到服务端（默认监听 127.0.0.1:1080）
pantyhose-client --server 10.0.0.5:1080 --ca ca.crt --cert client.crt --key client.key

# 自定义本地监听地址
pantyhose-client --server 10.0.0.5:1080 --listen 127.0.0.1:9090 --ca ca.crt --cert client.crt --key client.key
```

### 5. 配置 ProxyBridge

在 ProxyBridge 中将代理地址设置为 `127.0.0.1:1080`（pantyhose-client 的本地端口）。

> <sub>**注意**：Windows 息屏不影响运行，但系统**睡眠/休眠**会挂起进程。在 **设置 → 电源** 中将"睡眠"设为**从不**。</sub>

## 安全架构

```
pantyhose-client ──[TLS 1.3 + mTLS]──► pantyhose-server
       │                                       │
   客户端证书验证                           服务端证书验证
   (client.crt + client.key)            (server.crt + server.key)
       │                                       │
       └──── 双向认证，由同一 CA 签发 ─────────┘
```

- **TLS 1.3**：所有流量端到端加密
- **mTLS**：服务端和客户端互相验证证书，未持有合法证书无法连接
- **yamux 多路复用**：一条 TLS 连接承载多个 SOCKS5 会话，减少握手开销
- **无密码认证**：安全性完全由证书保证，不再使用 SOCKS5 用户名/密码

## 客户端配置

### ProxyBridge（推荐）

[ProxyBridge](https://github.com/InterceptSuite/ProxyBridge) 是跨平台的 Proxifier 替代品。

1. 安装 ProxyBridge
2. **Proxy Settings**：代理地址设为 `127.0.0.1`，端口 `1080`（pantyhose-client 本地端口）
3. **Proxy Rules**：VPN 相关进程设为 DIRECT 绕过代理

### Proxifier

1. **Proxy Servers > Add**：地址 `127.0.0.1`，端口 `1080`，协议 SOCKS5
2. 配置路由规则

## 防火墙（服务端）

```powershell
# TCP（必需）
netsh advfirewall firewall add rule name="pantyhose-tcp" dir=in action=allow protocol=TCP localport=1080

# UDP（UDP ASSOCIATE 必需）
netsh advfirewall firewall add rule name="pantyhose-udp" dir=in action=allow protocol=UDP localport=1080
```

清理：
```bash
pantyhose-server serve --fw-clean
```

## 开发

### 从源码编译

需要 [Go 1.21+](https://go.dev/dl/)。

```bash
git clone <repo-url>
cd pantyhose

# 编译服务端和客户端
go build -o pantyhose-server.exe ./cmd/pantyhose-server
go build -o pantyhose-client.exe ./cmd/pantyhose-client

# 运行测试
go test -v -count=1 -timeout 60s ./...

# Docker 测试
docker compose -f docker-compose.test.yml up --build
```

### 交叉编译

```bash
# macOS (Apple Silicon)
GOOS=darwin GOARCH=arm64 go build -o pantyhose-client-darwin-arm64 ./cmd/pantyhose-client

# Linux
GOOS=linux GOARCH=amd64 go build -o pantyhose-server-linux-amd64 ./cmd/pantyhose-server
```

## 服务端参数

### `pantyhose-server serve`

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--cert` | _(必需)_ | 服务端 TLS 证书 |
| `--key` | _(必需)_ | 服务端 TLS 私钥 |
| `--ca` | _(必需)_ | CA 证书（验证客户端） |
| `--insecure` | `false` | 非加密模式（无 TLS） |
| `--addr` | `0.0.0.0` | 监听地址 |
| `--port` | `1080` | 监听端口 |
| `--ip` | 自动检测 | UDP ASSOCIATE 出站 IP |
| `--tcp-timeout` | `60` | TCP 空闲超时（秒） |
| `--udp-timeout` | `60` | UDP 会话超时（秒） |
| `--enable-ipv6` | `false` | 允许 IPv6 出站 |
| `--no-sni-remap` | `false` | 禁用 SNI remap |
| `--sni-ports` | `"443"` | SNI remap 端口 |
| `--verbose` | `false` | 详细日志 |

### `pantyhose-server gencert`

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--out` | `./certs` | 证书输出目录 |
| `--hosts` | _(空)_ | 服务端额外 IP/域名 |
| `--days` | `3650` | 证书有效期（天） |

## 客户端参数

### `pantyhose-client`

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--server` | _(必需)_ | 远程服务端地址 |
| `--cert` | _(必需)_ | 客户端 TLS 证书 |
| `--key` | _(必需)_ | 客户端 TLS 私钥 |
| `--ca` | _(必需)_ | CA 证书（验证服务端） |
| `--listen` | `127.0.0.1:1080` | 本地 SOCKS5 监听地址 |

## 核心功能

### TLS 加密隧道

默认启用。客户端和服务端通过 TLS 1.3 + mTLS 建立加密隧道，所有 SOCKS5 流量在隧道内传输。使用 yamux 多路复用，一条 TLS 连接承载多个 SOCKS5 会话。

客户端断线后自动重连（指数退避：1s → 2s → 4s → ... → 60s）。

### SNI Remap（默认启用）

解决 VPN DNS 污染问题。拦截 HTTPS 连接，从 TLS ClientHello 提取 SNI hostname，通过服务端 DNS 重新解析。

### IPv6 处理（默认禁用）

默认禁用 IPv6 出站，避免公司网络无 IPv6 路由导致的连接超时。使用 `--enable-ipv6` 启用。

## 故障排查

### 客户端无法连接服务端

- 确认服务端 IP 可达：`ping <server-ip>`
- 检查防火墙规则
- 确认证书文件正确（ca.crt、client.crt、client.key）
- 检查客户端日志中的 TLS 错误信息

### DNS 污染（SNI remap 相关）

```bash
# 对比客户端和服务端的 DNS 解析结果
nslookup www.youtube.com  # 客户端
nslookup www.youtube.com  # 服务端
```

如果结果不同，说明存在 DNS 污染，SNI remap 默认会处理。

### 证书过期

重新运行 `pantyhose-server gencert` 生成新证书，替换客户端的证书文件。

## License

MIT
