# RFC-0191：Cluster Transport

> 状态：草案  
> 范围：Cluster  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

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

## WireEnvelope

```protobuf
message WireEnvelope {
  string source_node = 1;
  uint64 source_service = 2;
  string target_node = 3;
  uint64 target_service = 4;
  uint64 session = 5;
  uint32 command = 6;
  bytes payload = 7;
  bool response = 8;
}
```

protobuf 只用于跨节点边界。

内部 Service handler 不强制使用 protobuf。

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

目前服务间是 tcp，如果未来需要支持多种 payload 编码，应在 Transport 或 Codec Registry 中解决，而不是新增业务可见的 `ProtocolID`。

## 连接模型

```go
type ClusterManager struct {
    connections map[NodeID]*Connection
}

type Connection struct {
    Node    NodeID
    Conn    net.Conn
    Encoder Encoder
    Decoder Decoder
    State   ConnectionState
}
```

状态：

```text
Connecting
Connected
Disconnected
Reconnecting
Closed
```

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

## 断线处理

断线时：

1. 标记连接状态。
2. 失败相关 Pending Call。
3. 后续 Call 返回 `ErrRemoteUnavailable` 或进入重连策略。
4. 不把底层 `EOF` 暴露给业务。

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
