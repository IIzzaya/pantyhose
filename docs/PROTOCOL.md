# 协议与安全细节

## 协议栈

```
应用层数据 (HTTP/HTTPS/任意 TCP)
         │
    SOCKS5 协议封装
         │
    yamux 多路复用帧
         │
    TLS 1.3 加密
         │
    TCP 传输
```

每一层的职责：
- **TCP**：可靠的字节流传输
- **TLS 1.3**：加密 + 双向身份验证（mTLS）
- **yamux**：单 TCP 连接上的多路复用，每个 SOCKS5 会话一个 stream
- **SOCKS5**：代理协议（CONNECT 命令）
- **应用层**：实际的用户流量

---

## TLS 配置

### 版本

强制 TLS 1.3（`MinVersion: tls.VersionTLS13`）。TLS 1.2 及以下版本不被接受。

### 认证模式：mTLS（双向 TLS）

**服务端验证客户端**：
```go
tls.Config{
    ClientAuth: tls.RequireAndVerifyClientCert,
    ClientCAs:  caPool,  // 用 CA 证书验证客户端证书
}
```
只有持有 CA 签发的合法客户端证书和私钥（打包在 `client.pem` 中）的客户端才能连接。

**客户端验证服务端**：
```go
tls.Config{
    RootCAs:    caPool,       // 用 CA 证书验证服务端证书
    ServerName: host,         // 验证服务端证书的 CN/SAN
    Certificates: []tls.Certificate{clientCert}, // 提供客户端证书
}
```

### 证书体系

```
Pantyhose CA (ca.crt / ca.key)
├── pantyhose-server (server.crt / server.key)
│   ├── CN: pantyhose-server
│   ├── SAN: 127.0.0.1 + 用户指定的 hosts
│   └── ExtKeyUsage: ServerAuth, ClientAuth
│
└── pantyhose-client (client.pem: CA cert + client cert + client key)
    ├── CN: pantyhose-client
    ├── SAN: 127.0.0.1
    └── ExtKeyUsage: ServerAuth, ClientAuth
```

**算法细节**：
- 密钥类型：ECDSA P-256（256 位椭圆曲线）
- 签名算法：由 Go 标准库自动选择（通常 ECDSA-SHA256）
- 序列号：128 位随机数

---

## yamux 多路复用

### 配置

使用 `yamux.DefaultConfig()`：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| AcceptBacklog | 256 | 等待 Accept 的最大 stream 数 |
| EnableKeepAlive | true | 启用心跳 |
| KeepAliveInterval | 30s | 心跳间隔 |
| ConnectionWriteTimeout | 10s | 写超时 |
| MaxStreamWindowSize | 256 KB | stream 流控窗口 |

### 帧格式

yamux 使用 12 字节头部：

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Version    |     Type      |            Flags              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                          Stream ID                            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                          Length                                |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                          Payload...                            |
```

### 连接模型

```
单个 TLS TCP 连接
├── yamux session (客户端和服务端各持有一半)
│   ├── stream 1 → SOCKS5 session (www.youtube.com:443)
│   ├── stream 2 → SOCKS5 session (www.google.com:443)
│   ├── stream 3 → SOCKS5 session (api.github.com:443)
│   ├── ...
│   └── stream N → SOCKS5 session (...)
│
└── 心跳 (每 30 秒)
```

---

## SOCKS5 协议处理

### 认证方式

固定为 `NO AUTHENTICATION REQUIRED`（0x00）。安全由 mTLS 层保证。

### TLS 模式处理流程

由于 `txthinking/socks5` 库的 `Handler.TCPHandle` 需要 `*net.TCPConn`，但 yamux stream 只实现了 `net.Conn`，因此 TLS 模式下使用自定义处理逻辑：

```go
// 使用库的方法进行协议解析
srv.Negotiate(stream)       // 方法协商
req := srv.GetRequest(stream) // 读取 SOCKS5 请求

// 自定义处理（绕过 Handler 接口）
switch req.Cmd {
case CmdConnect:
    if sniHandler.Ports[destPort] {
        handleStreamSNI(stream, req, ...)     // SNI remap 路径
    } else {
        handleStreamConnect(stream, req, ...) // 直接连接路径
    }
default:
    // 回复 CommandNotSupported
}
```

### 非加密模式处理流程

直接使用库的标准接口：

```go
server.ListenAndServe(handler) // handler 实现 socks5.Handler 接口
```

### 支持的 SOCKS5 命令

| 命令 | TLS 模式 | 非加密模式 | 说明 |
|------|----------|-----------|------|
| CONNECT (0x01) | ✅ | ✅ | TCP 代理 |
| BIND (0x02) | ❌ | 由库处理 | — |
| UDP ASSOCIATE (0x03) | ❌ | ✅ | UDP 代理（仅非加密模式） |

> 注：TLS 模式下 UDP ASSOCIATE 尚未实现，UDP 流量通过 TCP 隧道中的 yamux stream 传输。

---

## SNI Remap

### 问题背景

客户端的 VPN 软件（如 CorpLink）在 `127.0.0.1:53` 运行本地 DNS 代理，对部分域名返回污染/虚假 IP。ProxyBridge 在内核层拦截流量时已经完成了 DNS 解析，发送给 SOCKS5 代理的是已解析的（可能被污染的）IP 地址。

### 解决原理

```
1. SOCKS5 CONNECT 185.45.5.35:443      ← 被污染的 IP
2. 先回复成功，让客户端发送 TLS ClientHello
3. 解析 ClientHello → SNI = "www.youtube.com"
4. 服务端 DNS 重解析: youtube.com → 142.251.10.91   ← 正确 IP
5. 连接 142.251.10.91:443
6. 转发 ClientHello 数据 + 后续双向 relay
```

### SNI 解析器实现

手动解析 TLS ClientHello（不依赖外部库）：

```
TLS Record Header (5 bytes)
└── ContentType = 0x16 (Handshake)
└── RecordLength

    Handshake Header (4 bytes)
    └── HandshakeType = 0x01 (ClientHello)
    └── Length

        ClientHello Body
        ├── Version (2 bytes)
        ├── Random (32 bytes)
        ├── SessionID (1+N bytes)
        ├── CipherSuites (2+N bytes)
        ├── CompressionMethods (1+N bytes)
        └── Extensions (2+N bytes)
            ├── Extension[0]
            ├── ...
            └── Extension[K]: server_name (type=0x0000)
                └── ServerNameList
                    └── HostName (type=0x00) → "www.youtube.com"
```

### SNI Remap 端口配置

默认仅对目标端口 443 生效。可通过 `--sni-ports` 自定义：

```bash
pantyhose-server serve --sni-ports 443,8443,9443
```

---

## 自动重连

### 触发条件

1. yamux session 关闭（心跳超时、网络断开、服务端重启）
2. `OpenStream()` 调用失败

### 退避策略

```
初始等待:   1 秒
倍增因子:   2x
最大等待:   60 秒
退避序列:   1s → 2s → 4s → 8s → 16s → 32s → 60s → 60s → ...
```

### 流程

```go
func (c *Client) OpenStream() (net.Conn, error) {
    // 1. 尝试使用现有 session
    if session.Open() 成功 {
        return stream
    }

    // 2. session 失效，触发重连
    c.reconnectWithBackoff()

    // 3. 重连成功后打开新 stream
    return session.Open()
}
```

重连期间所有新的 `OpenStream()` 调用会被 mutex 阻塞，等待重连完成或返回错误。

---

## 安全模型

### 威胁与对策

| 威胁 | 对策 |
|------|------|
| 流量窃听 | TLS 1.3 加密所有数据 |
| 中间人攻击 | mTLS 双向证书验证 |
| 未授权使用 | 客户端必须持有 CA 签发的证书 |
| 证书伪造 | CA 私钥仅存于服务端 |
| DNS 污染 | SNI remap 在服务端重解析 |
| 重放攻击 | TLS 1.3 内置防重放 |
| 暴力破解 | 无密码可猜，只有证书认证 |

### 不防护的场景

- 服务端机器本身被入侵（攻击者可直接读取 CA 私钥）
- 客户端证书文件泄露（需吊销并重新签发）
- 服务端到目标的最后一段网络（如果目标不用 HTTPS）

### 非加密模式风险

当使用 `--insecure` 时：
- 无加密：所有 SOCKS5 流量明文传输
- 无认证：任何能访问端口的客户端都能使用代理
- 适用场景：仅在可信网络环境下调试、测试

---

## 线程模型

### 服务端 goroutine 结构

```
main goroutine
├── signal handler (SIGINT/SIGTERM)
└── tunnel.Server
    ├── acceptLoop (goroutine)
    │   └── 每个 TLS 连接 → handleSession (goroutine)
    │       └── 循环 Accept yamux streams → 送入 streamCh
    │
    └── serveTLSMode (main goroutine)
        └── 从 streamCh 取 stream → handleStream (goroutine)
            ├── SOCKS5 negotiate + request
            └── relay goroutines (2 per connection)
```

### 客户端 goroutine 结构

```
main goroutine
├── signal handler (SIGINT/SIGTERM)
└── Accept loop (main goroutine)
    └── 每个本地连接 → handleLocalConn (goroutine)
        ├── OpenStream (可能触发 reconnect)
        └── io.Copy goroutines (2 per connection)
```
