# RFC-0191：Cluster Transport

> 状态：草案  
> 范围：Cluster  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`、Skynet `cluster.lua`、`clustersender.lua`、`clusteragent.lua`

## 目的

本文定义节点之间如何传输 Envelope。

## 职责

ClusterTransport 只负责节点间消息传输。

它不理解：

- Battle。
- Player。
- Wallet。
- 业务规则。

它只理解：

- Source。
- Target。
- Session。
- Command。
- Payload bytes。

## Runtime 与 Transport 契约

```go
type WireEnvelope struct {
    Source       ServiceRef
    Target       ServiceRef
    Session      SessionID
    Command      CommandID
    Payload      []byte
    Response     bool
    CallPath     []ServiceRef
    ErrorCode    string
    ErrorMessage string
}

type ClusterEvents struct {
    Receive     func(peer NodeID, envelope WireEnvelope)
    Unavailable func(peer NodeID)
}

type ClusterTransport interface {
    Start(local NodeID, events ClusterEvents) error
    Send(target NodeID, envelope WireEnvelope) error
    Close(context.Context) error
}
```

`ClusterTransport` 负责连接、握手、WireEnvelope 编码、帧边界和断线通知。它不解码业务 payload，不调用 Service，也不管理 PendingCall。

Transport 可以并发调用 `Receive` 和 `Unavailable`。同一连接上的帧必须保持读取顺序；Runtime 自己负责 Mailbox 串行语义。

## Payload Codec

```go
type ClusterCodec interface {
    Encode(command CommandID, response bool, value any) ([]byte, error)
    Decode(command CommandID, response bool, payload []byte) (any, error)
}
```

`response=false` 表示 Command payload，`response=true` 表示 Reply payload。Codec 可以按 `CommandID` 选择 protobuf、FlatBuffers 或其它稳定编码。内部 Service handler 不强制使用 protobuf，也不接触 WireEnvelope。

## Transport 与 Protocol 的边界

GSR 不引入 Skynet 风格的 `PTYPE_LUA`、`PTYPE_SOCKET`、`PTYPE_HARBOR`。

Transport 可以选择不同链路和编码：

- protobuf over TCP。
- protobuf over WebSocket。
- 自定义二进制协议 over TCP。
- QUIC。

这些选择只影响节点之间如何传输 `WireEnvelope` 和 payload bytes，不影响 Service 如何处理消息。

业务分发永远看 `CommandID`：

```text
WireEnvelope
  ↓
Decode payload bytes
  ↓
Envelope
  ↓
Mailbox
  ↓
Command Dispatcher
```

第一版提供自定义二进制 WireEnvelope over TCP；业务 payload 编码由 `ClusterCodec` 决定。未来支持 protobuf WireEnvelope、WebSocket 或 QUIC 时，实现新的 `ClusterTransport` 即可，不新增业务可见的 `ProtocolID`。

## TCP 连接模型

TCP adapter 位于 `transport/tcp`，通过以下配置构造：

```go
type Config struct {
    ListenAddress    string
    Peers            map[NodeID]string
    DialTimeout      time.Duration
    HandshakeTimeout time.Duration
    WriteTimeout     time.Duration
    MaxFrameSize     uint32
}

transport := tcp.New(config)
```

`Peers` 只提供第一版同步建连所需的静态地址，不承担服务发现；动态地址更新接口由后续 Control Plane 设计，不进入当前 Core API。

状态：

```text
Connecting
Connected
Disconnected
Closed
```

第一版不在 Core 中实现自动重连状态机。`Send` 在没有当前连接时按静态 peer 地址同步建连；建连或写入失败返回 `ErrRemoteUnavailable`。重试、退避、动态地址变更属于后续 Tooling/Control Plane。

每个 peer 最多保留一条当前全双工连接。两端同时建连时，按 NodeID 排序确定首选发起方向，避免两端各保留不同连接。连接写入必须串行，读取由 Transport 自有 goroutine 持续解帧。

## 握手

连接建立后先握手：

```protobuf
message Handshake {
  string node_id = 1;
  uint32 version = 2;
}
```

流程：

```text
connect
  ↓
Handshake
  ↓
Verify version/node
  ↓
Connected
```

握手必须在业务帧之前完成。对端 NodeID 不能为空、不能等于本地 NodeID，协议版本必须相同；主动连接时，对端声明的 NodeID 还必须等于目标 NodeID。失败连接不得进入连接表。

TCP 帧使用 `uint32` 大端长度前缀。实现必须限制 NodeID 长度、CallPath 长度、payload 长度和总帧长度，不能按不可信长度无限分配内存。

第一版握手协议版本固定为 `1`，WireEnvelope 二进制格式版本固定为 `1`。默认最大帧为 16 MiB，NodeID 最长 255 bytes，CallPath 最多 64 项；这些限制在读取 payload 前检查。

## 断线处理

断线时：

1. 标记连接状态。
2. 失败相关 Pending Call。
3. 后续 Call 返回 `ErrRemoteUnavailable` 或进入重连策略。
4. 不把底层 `EOF` 暴露给业务。

只有从连接表移除当前连接的 goroutine 可以发送一次 `Unavailable(peer)`。被新连接替换的旧读循环不得把新连接误判为断线。

## Proxy

可以提供轻量 Proxy：

```go
proxy := runtime.Proxy(ref)
proxy.Call(ctx, CmdAddCoin, req)
```

Proxy 只是语法糖。

底层仍然调用：

```go
runtime.Call(ctx, ref, cmd, req)
```

## 测试

必须覆盖：

- 远程 Send。
- 远程 Call/Reply。
- 断线时 Call 失败。
- 超时后晚到 Reply 被丢弃。
- 本地和远程同一 Command 行为一致。
- 握手版本或节点身份不匹配时拒绝连接。
- 超限帧在分配 payload 前拒绝。
- 同时建连后每个 peer 只保留同一条逻辑连接。
