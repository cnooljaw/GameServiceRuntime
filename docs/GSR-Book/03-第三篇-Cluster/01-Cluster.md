# Cluster Data Plane：让远程 Command 仍然像 Command

“把本地 Envelope JSON 序列化后发出去，Cluster 就完成了吧？”

老周摇头：“传出去容易。难的是来源、Reply、错误、调用环和节点断开以后还要保持同一套语义。”

## 创建 Cluster Runtime

```go
transport := tcp.New(tcp.Config{
    ListenAddress: "127.0.0.1:9001",
    Peers: map[gsr.NodeID]string{
        "node-b": "127.0.0.1:9002",
    },
})

runtime, err := gsr.NewClusterRuntime(
    gsr.Config{NodeID: "node-a", Workers: 4},
    transport,
    myCodec,
)
```

与本地 Runtime 相比，多了两个边界：

- `ClusterTransport`：移动字节安全的 `WireEnvelope`；
- `ClusterCodec`：编码和解码 Command/Reply payload。

## 本地和远程的分流

```text
Runtime.Send(target)
  ├── target.Node == local -> local Mailbox
  └── target.Node != local -> ClusterCodec -> Transport
```

远端接收后：

```text
Transport Receive
  -> 校验 peer、Source、Target、Session、CallPath
  -> Decode payload
  -> 目标本地 Mailbox
```

远程 Command 仍然要经过目标 Service 的 Command 白名单和 Mailbox。

## WireEnvelope

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
```

`Source.Node` 必须等于真实 peer，`Target.Node` 必须等于本地节点。Cluster 不信任错误的程序状态，不能接受对方伪造来源。

## 远程错误

Runtime 稳定错误使用固定 error code：

```text
service_not_found
mailbox_full
call_cycle
service_failed
...
```

调用方仍可使用 `errors.Is`。

业务自定义错误没有稳定 Core code 时，返回 `RemoteError{Code:"remote", Message:...}`，并限制 message 长度。

## 节点断开

Transport 报告某 peer unavailable 后，Runtime 会失败所有与该节点相关的 Pending Call：

```text
ErrRemoteUnavailable
```

已经 Send 的无等待 Command 不会凭空得到业务确认。需要确认的流程必须 Call 或设计业务 acknowledgement。

## 节点端点

`ServiceID(0)` 是 Core 节点端点。目前用于 `ResolveRemote`：

```go
ref, err := runtime.ResolveRemote(ctx, "node-b", ".config")
```

它不分发给普通 Service，也不应扩展成万能控制面。

## 对照源码

- `runtime/cluster.go`
- `runtime/core_endpoint.go`
- `examples/cluster-runtime/`
- `runtime/cluster_error_test.go`

## 本章小结

Cluster Data Plane 不负责发现、负载均衡或自动恢复。它只把已经明确目标的 Command 和 Reply 安全地跨节点传输。
