# TCP Transport：网络归网络，业务归 Service

Transport 最容易膨胀成第二个 Runtime：连接池、重试、路由、序列化、业务协议全塞进去。GSR 把它压回一个窄接口。

## 接口

```go
type ClusterTransport interface {
    Start(NodeID, ClusterEvents) error
    Send(NodeID, WireEnvelope) error
    Close(context.Context) error
}
```

Transport 不调用 Service，不决定 CommandID，也不理解业务 payload。

## TCP 实现

`transport/tcp` 提供持久、全双工连接：

```go
transport := tcp.New(tcp.Config{
    ListenAddress:    "127.0.0.1:9001",
    DialTimeout:      3 * time.Second,
    HandshakeTimeout: 3 * time.Second,
    WriteTimeout:     3 * time.Second,
    MaxFrameSize:     16 << 20,
})
```

启动后 Transport：

- 监听入站连接；
- 按需拨号已配置 peer；
- 复用每个 peer 的连接；
- 处理双向同时拨号；
- 为每条连接设置单独写锁；
- 在 Close 时关闭 listener、连接并等待 goroutine。

Transport 是少数允许拥有 I/O goroutine 的 adapter，因为它有明确 owner、关闭入口和 `WaitGroup`。

## Handshake

连接先交换：

```text
magic = GSRH
protocol version
NodeID
```

它检查：

- 版本一致；
- peer NodeID 非空；
- peer 不是自己；
- 主动拨号时 peer 与目标一致。

这不是互联网级身份认证。当前安全前提是可信集群网络，但仍拒绝错误 NodeID。

## Frame

每帧使用 4 字节大端长度前缀。WireEnvelope 内部字段也有长度上限：

- NodeID；
- CallPath；
- error code/message；
- payload；
- 整个 frame。

这些限制防止错误或恶意帧造成无界分配。

## Codec 不属于 TCP

TCP 只编码 WireEnvelope 的结构。业务 payload 由 `ClusterCodec` 处理：

```go
type ClusterCodec interface {
    Encode(command CommandID, response bool, value any) ([]byte, error)
    Decode(command CommandID, response bool, payload []byte) (any, error)
}
```

Tooling Codec 采用组合方式：

```go
codec := control.NewCodec(
    discovery.NewCodec(
        servicegroup.NewCodec(gameCodec),
    ),
)
```

每层只认识自己的 Command 区间，其余交给 fallback。

## 断线不是重试协议

写失败时，Transport 移除连接，下次 Send 可以重新拨号。但它不自动重放已经失败的业务 Command。

自动重试会制造重复副作用。是否重试必须由具有 RequestID 语义的上层决定。

## 对照源码

- `transport/tcp/transport.go`
- `transport/tcp/wire.go`
- `transport/tcp/wire_test.go`
- `examples/cluster-runtime/`

## 本章小结

Transport 拥有连接和 I/O 生命周期；Runtime 拥有 Envelope 语义；Service 拥有业务状态。三者各守一层，网络故障才不会变成隐式业务重试。
