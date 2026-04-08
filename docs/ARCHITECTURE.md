# 系统架构

## 总体架构

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              客户端机器                                         │
│                                                                                 │
│  ┌──────────┐    ┌─────────────┐    ┌──────────────────┐                        │
│  │ 应用程序  │───►│ ProxyBridge │───►│ pantyhose-client │══════╗                 │
│  │ (浏览器等)│    │ (流量拦截)   │    │ (本地 SOCKS5)    │      ║                 │
│  └──────────┘    └─────────────┘    └──────────────────┘      ║                 │
│                                                                ║                 │
└────────────────────────────────────────────────────────────────╬─────────────────┘
                                                                 ║
                                                    TLS 1.3 + yamux 加密隧道
                                                                 ║
┌────────────────────────────────────────────────────────────────╬─────────────────┐
│                              服务端机器                        ║                 │
│                                                                ║                 │
│  ┌──────────────────┐    ┌─────────────┐    ┌──────────┐      ║                 │
│  │ pantyhose-server │◄═══╝             │    │          │      ║                 │
│  │                  │                  │    │  目标     │      ║                 │
│  │  ┌────────────┐  │    ┌───────────┐ │    │  服务器   │                        │
│  │  │ TLS 解密   │──┼───►│ SOCKS5    │─┼───►│          │                        │
│  │  │ + yamux    │  │    │ + SNI     │ │    │ (Google  │                        │
│  │  │ 解多路复用  │  │    │   remap   │ │    │  YouTube │                        │
│  │  └────────────┘  │    └───────────┘ │    │  etc.)   │                        │
│  └──────────────────┘                  │    └──────────┘                        │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## 组件职责

### pantyhose-server

| 模块 | 文件 | 职责 |
|------|------|------|
| 入口 / CLI | `cmd/pantyhose-server/main.go` | 子命令分发（gencert / serve）、flag 解析、启动流程 |
| TLS 隧道监听 | `internal/tunnel/server.go` | TLS 1.3 mTLS 监听、yamux session 管理、stream 分发 |
| TLS 模式 SOCKS5 | `cmd/pantyhose-server/tls_serve.go` | 在 yamux stream 上处理 SOCKS5 协议（绕过库的 `*net.TCPConn` 限制） |
| SNI Remap | `cmd/pantyhose-server/sni.go` | TLS ClientHello 解析、SNI 提取、DNS 重解析 |
| 证书生成 | `internal/certgen/certgen.go` | 自签名 CA + 服务端/客户端证书生成 |
| 非加密模式 | `cmd/pantyhose-server/main.go` (`--insecure`) | 直接使用 `socks5.Server.ListenAndServe` |

### pantyhose-client

| 模块 | 文件 | 职责 |
|------|------|------|
| 入口 / CLI | `cmd/pantyhose-client/main.go` | flag 解析、本地 SOCKS5 监听、连接转发 |
| TLS 隧道连接 | `internal/tunnel/client.go` | TLS 1.3 mTLS 连接、yamux session、自动重连 |

## 目录结构

```
pantyhose/
├── cmd/
│   ├── pantyhose-server/
│   │   ├── main.go                    # 服务端入口（gencert + serve 子命令）
│   │   ├── sni.go                     # SNI remap handler
│   │   ├── tls_serve.go               # TLS 模式 SOCKS5 stream handler
│   │   ├── main_test.go               # 服务端单元测试 + 非加密模式集成测试
│   │   ├── sni_test.go                # SNI 解析器测试
│   │   └── tunnel_integration_test.go # TLS 隧道端到端集成测试
│   │
│   └── pantyhose-client/
│       └── main.go                    # 客户端入口
│
├── internal/
│   ├── certgen/
│   │   ├── certgen.go                 # 证书生成（CA + server + client）
│   │   └── certgen_test.go
│   │
│   └── tunnel/
│       ├── server.go                  # TLS+yamux 服务端（实现 net.Listener）
│       ├── client.go                  # TLS+yamux 客户端（自动重连）
│       └── tunnel_test.go             # 隧道单元测试
│
├── docs/
│   ├── DESIGN.md                      # 设计决策记录
│   ├── ARCHITECTURE.md                # 本文档
│   ├── PROTOCOL.md                    # 协议细节
│   └── CERTIFICATES.md               # 证书管理指南
│
├── Dockerfile                         # 多阶段构建（server / client / test）
├── docker-compose.test.yml            # Docker 测试编排
├── Makefile                           # 构建/测试快捷命令
├── go.mod / go.sum
├── AGENTS.md                          # AI agent 开发指南
├── README.md                          # 用户文档（中文）
├── README_EN.md                       # 用户文档（英文）
└── TODO.md                            # 开发看板
```

## 连接生命周期

### TLS 模式（默认）

```
1. 启动
   pantyhose-server serve    // 证书从 ./certs/ 自动读取
   └─→ tunnel.NewServer(addr, cert, key, ca)
       └─→ tls.Listen("tcp", addr, tlsConfig)    // mTLS 监听
       └─→ go s.acceptLoop()                      // 后台接受 TLS 连接

2. 客户端连接
   pantyhose-client --server host:1080    // client.pem 从 ./certs/ 自动读取
   └─→ tunnel.NewClientFromPEM(serverAddr, pemFile)
   └─→ client.Connect()
       └─→ tls.Dial("tcp", serverAddr, tlsConfig) // mTLS 握手
       └─→ yamux.Client(conn, config)              // 建立 yamux session

3. 服务端处理 session
   s.acceptLoop()
   └─→ conn := tlsListener.Accept()               // 接受 TLS 连接
   └─→ go s.handleSession(conn)
       └─→ yamux.Server(conn, config)              // 创建 yamux session
       └─→ for { stream := session.Accept() }      // 持续接受 streams
           └─→ s.streamCh <- stream                // 送入 channel

4. SOCKS5 请求处理
   serveTLSMode()
   └─→ stream := tunnelSrv.Accept()                // 从 channel 取 stream
   └─→ go handleStream(stream, srv, sniHandler)
       ├─→ srv.Negotiate(stream)                   // SOCKS5 方法协商
       ├─→ srv.GetRequest(stream)                  // 解析 SOCKS5 请求
       └─→ 路由决策:
           ├─→ SNI 端口 → handleStreamSNI()        // SNI remap 路径
           └─→ 其他端口 → handleStreamConnect()     // 直接 CONNECT 路径

5. 数据转发
   handleStreamConnect() / handleStreamSNI()
   └─→ net.DialTimeout(destAddr)                   // 连接目标
   └─→ relay(stream ↔ destination)                 // 双向转发
```

### 非加密模式（--insecure）

```
pantyhose-server serve --insecure
└─→ socks5.NewClassicServer(addr, ...)
└─→ server.ListenAndServe(handler)    // 标准 SOCKS5 服务
    └─→ 每个 TCP 连接 → handler.TCPHandle()
```

## 数据流

### TLS 模式下一个 HTTPS 请求的完整路径

```
浏览器
  │ HTTP 请求 → https://www.youtube.com
  ▼
ProxyBridge (内核层拦截)
  │ 发送 SOCKS5 CONNECT 185.45.5.35:443 (被污染的 IP)
  ▼
pantyhose-client (127.0.0.1:1080)
  │ 1. 接受本地 TCP 连接
  │ 2. 打开 yamux stream
  │ 3. 原样转发 SOCKS5 握手和数据
  ▼                         ═══ TLS 1.3 加密 ═══
pantyhose-server (:1080)
  │ 1. yamux stream → handleStream()
  │ 2. SOCKS5 Negotiate (方法协商: no-auth)
  │ 3. SOCKS5 GetRequest (CONNECT 185.45.5.35:443)
  │ 4. 目标端口 443 → SNI remap 路径
  │ 5. 发送 SOCKS5 成功回复
  │ 6. 读取 TLS ClientHello → SNI = "www.youtube.com"
  │ 7. DNS 重解析: youtube.com → 142.251.10.91
  │ 8. 连接 142.251.10.91:443
  │ 9. 转发 ClientHello + 双向 relay
  ▼
www.youtube.com (142.251.10.91:443)
```

## 关键技术细节

### yamux stream 与 socks5 库的适配

`txthinking/socks5` 库的 `Handler.TCPHandle` 方法签名要求 `*net.TCPConn`：

```go
type Handler interface {
    TCPHandle(*Server, *net.TCPConn, *Request) error
    UDPHandle(*Server, *net.UDPAddr, *Datagram) error
}
```

yamux stream 实现的是 `net.Conn` 接口，无法直接传入。解决方案：

- **非加密模式**：使用库的 `Server.ListenAndServe(handler)`，库内部接受 `*net.TCPConn` 并调用 handler
- **TLS 模式**：绕过库的 handler 接口，使用 `Server.Negotiate()` 和 `Server.GetRequest()` 进行 SOCKS5 协商，然后在 `tls_serve.go` 中自行处理 CONNECT 请求（直接操作 `net.Conn`）

### 自动重连机制

```
连接断开
  │
  ▼
OpenStream() 检测 session 已关闭
  │
  ▼
reconnectWithBackoff()
  │
  ├── 尝试重连 ────── 成功 → 返回新 session
  │
  └── 失败 → 等待 backoff 时间
      │
      ├── 1s → 2s → 4s → 8s → 16s → 32s → 60s (上限)
      │
      └── 重试...
```

yamux 自带 30 秒心跳（KeepAlive），能及时检测到连接断开。重连期间新来的 SOCKS5 请求会等待隧道恢复或超时返回错误。

### tunnel.Server 的 net.Listener 实现

`tunnel.Server` 实现了 `net.Listener` 接口，使用内部 channel 将 yamux streams 从多个 sessions 汇聚到单一 `Accept()` 调用：

```go
type Server struct {
    tlsListener net.Listener        // TLS 监听器
    streamCh    chan net.Conn        // 汇聚所有 session 的 streams
    sessions    []*yamux.Session     // 活跃的 yamux sessions
}

func (s *Server) Accept() (net.Conn, error) {
    return <-s.streamCh              // 从任意 session 获取下一个 stream
}
```

这使得上层代码可以像使用普通 TCP 监听器一样使用隧道服务。

## 依赖关系

```
pantyhose (go.mod)
├── github.com/txthinking/socks5     # SOCKS5 协议实现
│   ├── github.com/txthinking/runnergroup
│   └── github.com/patrickmn/go-cache
│
└── github.com/hashicorp/yamux       # TCP 多路复用
```

Go 标准库提供：
- `crypto/tls` — TLS 1.3 + mTLS
- `crypto/ecdsa` + `crypto/x509` — 证书生成
- `net` — 网络通信

## 构建产物

| 二进制 | 构建命令 | 平台 |
|--------|---------|------|
| `pantyhose-server` | `go build ./cmd/pantyhose-server` | Windows / macOS / Linux |
| `pantyhose-client` | `go build ./cmd/pantyhose-client` | Windows / macOS / Linux |

GitHub Actions 自动构建 8 个产物：
- pantyhose-server: windows, mac-os-intel, mac-os-apple-silicon, linux
- pantyhose-client: windows, mac-os-intel, mac-os-apple-silicon, linux

## 测试架构

```
go test ./...
│
├── cmd/pantyhose-server/
│   ├── main_test.go              # 8 个测试
│   │   ├── TestDetectOutboundIP        (单元)
│   │   ├── TestDetectOutboundIPFallback(单元)
│   │   ├── TestIsShutdownError         (单元)
│   │   ├── TestTCPProxy                (集成: 非加密模式)
│   │   ├── TestTCPRawConnect           (集成: TCP echo)
│   │   ├── TestIsIPv6Addr              (单元)
│   │   ├── TestIPv4OnlyDialers...      (单元)
│   │   └── TestServerShutdown          (集成)
│   │
│   ├── sni_test.go               # 4 个测试
│   │   ├── TestExtractSNI              (单元)
│   │   ├── TestExtractSNIInvalidData   (单元)
│   │   ├── TestSNIRemapHandlerTLS      (集成)
│   │   └── TestSNIRemapNonTLS          (集成)
│   │
│   └── tunnel_integration_test.go # 3 个测试
│       ├── TestTunnelE2E               (E2E: HTTP through TLS tunnel)
│       ├── TestTunnelE2EWithSNI        (E2E: HTTPS + SNI through tunnel)
│       └── TestTunnelE2ERawTCP         (E2E: TCP echo through tunnel)
│
├── internal/certgen/
│   └── certgen_test.go           # 2 个测试
│       ├── TestGenerate                (单元: 完整证书链验证)
│       └── TestGenerateDefaultHosts    (单元)
│
└── internal/tunnel/
    └── tunnel_test.go            # 6 个测试
        ├── TestTunnelServerClient      (集成: echo through tunnel)
        ├── TestTunnelMultipleStreams    (集成: 10 个并发 streams)
        ├── TestTunnelRejectWithoutCert (安全: 无证书拒绝)
        ├── TestTunnelRejectWrongCA     (安全: 错误 CA 拒绝)
        ├── TestNewServerInvalidCert    (边界: 无效证书)
        └── TestNewClientInvalidCert    (边界: 无效证书)

总计: 23 个测试
```
