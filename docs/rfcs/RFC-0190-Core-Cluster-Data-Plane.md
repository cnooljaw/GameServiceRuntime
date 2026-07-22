# RFC-0190：Cluster Data Plane

> 状态：已接受
> 范围：Core Runtime、Cluster
> 依据：`docs/learn/005-Skynet设计思想与优雅实现.md`、`docs/learn/006-Go-Service-Runtime概要设计与约定.md`

## 目的

本文定义 GSR 的 Cluster 语义。

## 核心结论

Cluster 不是 RPC。

Cluster 是 Runtime Message Pipeline 的远程延伸。

Cluster 必须区分两条路径：

```text
Cluster Data Plane:
  业务 Envelope 跨节点投递。

Cluster Control Plane:
  节点管理、健康检查、观测查询、受控运维命令。
```

两者共享 Service、Command、Envelope 模型，但职责不同。

```text
runtime.Call(ctx, ref, cmd, req)
  ↓
Router
  ↓
Local or Remote
  ↓
Mailbox or ClusterTransport
```

业务代码不关心目标 Service 在本地还是远程。

## Data Plane

数据面只负责业务消息投递：

```text
Send / Call
  ↓
Envelope
  ↓
Router
  ↓
Local Mailbox or ClusterTransport
```

数据面不提供管理后台，不查询节点详情，不执行运维命令。

## 默认 Cluster 模式

GSR Cluster 默认采用与 Skynet `cluster` 一致的静态拓扑模型：

```text
部署配置：
  NodeID -> Transport 地址

节点内名字：
  ServiceName -> ServiceRef
```

调用方先从部署配置取得目标 `NodeID` 和地址，再用 `Runtime.ResolveRemote(node, name)` 解析目标节点的本地名字。基础 Cluster、普通业务 Service 和 Cluster Data Plane 都不依赖 Discovery。

Discovery 是可选 Runtime Tooling。只有调用方不应知道服务所在节点，或者系统需要动态迁移、节点目录和控制面时才启用。Discovery 不参与已知节点之间的普通 Send、Call 或名字解析。

## Control Plane

控制面负责 Runtime 运维能力：

```text
Admin API / CLI
  ↓
ClusterControlService
  ↓
DiscoveryService / NodeAgentService / Monitor adapter
  ↓
Command
```

控制面仍然通过 Command 访问系统服务，不新增第二套 RPC。

## 非目标

Cluster 不做：

- gRPC Stub。
- OpenAPI。
- HTTP 网关。
- 传统微服务注册中心。
- 按服务名随机负载均衡临时 Battle。
- 远程任意代码注入。
- 生产环境默认开启高危管理命令。

## 统一地址

远程调用仍然使用 `ServiceRef`：

```go
type ServiceRef struct {
    Node NodeID
    ID   ServiceID
}
```

Router 判断：

```go
if ref.Node == localNode {
    localDelivery(envelope)
} else {
    clusterTransport.Send(envelope)
}
```

`ServiceID(0)` 保留为节点内 Runtime caller 和节点级 Core endpoint。进程外部直接调用 `Runtime.Send` 或 `Runtime.Call` 时，Envelope 的 Source 使用 `{Node: localNode, ID: 0}`；Service 发起调用时仍使用该 Service 自己的 `ServiceRef`。

## 节点级名字查询

部署配置先提供目标 `NodeID` 和 Transport 地址，再通过稳定本地名字取得动态 ServiceRef：

```go
ref, err := runtime.ResolveRemote(ctx, node, ".config")
```

这对应 Skynet `cluster.query(node, name)` 的启动职责。实现规则：

1. 请求和响应继续使用 `WireEnvelope`、Session、PendingCall 和 Call 错误语义。
2. Target 和 responder 使用 `{Node, ID: 0}`。
3. 只允许一个 Core 私有 Resolve Command，且必须是 `Session > 0` 的 Call。
4. Core 自己编码名字请求和 ServiceID 响应，不交给业务 `ClusterCodec`。
5. 目标节点只查询自己的 `LocalRegistry`；返回的 `ServiceRef.Node` 必须等于目标节点。
6. 其它发往 ServiceID 0 的 Command 或 Send 均视为 `ErrInvalidClusterEnvelope`。

该能力只解决已知节点上的启动名字，不实现全局 Discovery、负载均衡或动态 peer 更新。

当调用方已知 `.config` 位于哪个节点时，应直接使用本节能力，不应为了取得它而先访问 Discovery。`.discovery` 只是启用可选 Discovery Tooling 时的启动名字。

## 构造边界

本地 Runtime 保持简单构造：

```go
runtime := NewRuntime(config)
```

Cluster 需要启动 Transport，启动可能因为监听地址、协议版本或资源问题失败，因此使用独立构造函数：

```go
runtime, err := NewClusterRuntime(config, transport, codec)
```

`NewClusterRuntime` 启动成功后，Transport 和 Codec 由 Runtime 持有到 `Runtime.Close`。构造失败必须关闭已经取得的 Transport 资源和 Scheduler。

## Send 流程

```text
runtime.Send
  ↓
Envelope(Session=0)
  ↓
Router
  ↓
ClusterTransport
  ↓
Remote Runtime
  ↓
Remote Mailbox
  ↓
Service.Handle
```

远程 Send 沿用 Skynet `cluster.send` 的异步 push 语义，不增加投递 ACK。调用方只能同步得到编码、建连和本地写入错误；目标 Service 不存在、Command 未注册或 Mailbox 满由远端 Runtime 记录 `cluster_delivery_errors_total`。需要业务结果时必须使用 Call。

## Call 流程

```text
runtime.Call
  ↓
Create Session
  ↓
Save PendingCall
  ↓
ClusterTransport
  ↓
Remote Mailbox
  ↓
Service.Handle
  ↓
Reply
  ↓
Source PendingCall
```

远程 Call 必须传输完整 `CallPath`。目标 Service 发起下一次同步 Call 时，仍按本地规则检查目标是否已在路径中；调用环不能因为跨节点而失效。

Reply 使用反向地址：

```text
Request: Source=caller, Target=responder
Reply:   Source=responder, Target=caller
```

源 Runtime 只有在 responder、caller、Command 和 Session 都与 PendingCall 一致时才接受 Reply。

## 入站边界

Transport 握手得到的 peer NodeID 是连接身份。Runtime 接收入站 WireEnvelope 时必须验证：

1. `Source.Node` 等于握手 peer。
2. `Target.Node` 等于本地 NodeID。
3. Command 帧允许 `Session=0`，Reply 帧必须 `Session>0`。
4. Command 的 `CallPath` 末项必须是 Target；Send 不携带 `CallPath`。

校验或 payload 解码失败时，Call 返回稳定的 Runtime 错误；Send 记录指标并丢弃。不能把未校验的入站数据直接放进 Mailbox。

已知 Runtime 错误跨节点返回后必须保留 `errors.Is` 语义。特别是多跳 Call 中间节点返回 `ErrRemoteUnavailable`、`ErrCallCycle`、`ErrServiceClosed` 等稳定错误时，源节点不能把它们降级为普通 `RemoteError`。

Reply payload 编码失败时，responder 必须发送一个不含业务 Payload 的 `ErrPayloadEncode` 错误响应，同时让本地 `CommandContext.Reply` 返回该错误。不能因为 Reply 已被标记为发送过，就让 caller 只能等待 context 超时。

## 断线与关闭

Transport 只在当前有效连接断开时通知节点不可用。Runtime 收到通知后立即失败所有以该节点为远端的 PendingCall；后续 Send/Call 返回 `ErrRemoteUnavailable`，或由调用方 context 超时。

`Runtime.Close` 先阻止新的消息，再关闭 Transport，随后收敛本地 Service、PendingCall、Timer 和 Scheduler。关闭 Transport 不得启动第二套 Service 清理流程。

## Node 与 ServiceName

Cluster 定位由三部分组成：

```text
Node + Service Address + Command
```

长期服务可以通过名字解析到 `ServiceRef`。

临时 `BattleService` 不注册名字，只传递 `ServiceRef`。

## 规则

1. 本地和远程必须共用 `Envelope`。
2. 业务 API 不区分 local 和 remote。
3. Cluster 错误必须转换成 Runtime 错误。
4. 远程节点断线时，Pending Call 必须失败或超时。
5. 不引入第二套 RPC API。
6. 管理面能力必须放在系统 Service 中，不能塞进 `ClusterTransport`。
7. 运维命令必须走白名单 Command，并记录审计信息。
8. 跨节点必须保留 `CallPath` 和本地相同的调用环检测语义。
9. 入站 Source.Node 必须绑定握手声明的节点身份，Reply 必须校验 caller、responder、Command 和 Session。
10. Cluster 启动失败必须通过可失败构造函数返回，不能让半启动 Runtime 对业务可见。
11. Cluster 启动失败后的 Transport 和 Runtime 清理错误必须与启动错误一起返回，不能静默丢弃。
12. 节点级名字查询必须绕过业务 Codec，但仍遵守 Envelope 校验和 PendingCall responder 校验。
