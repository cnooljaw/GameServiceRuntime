# RFC-0190：Cluster Data Plane

> 状态：草案  
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

## Control Plane

控制面负责 Runtime 运维能力：

```text
Admin API / CLI
  ↓
ClusterControlService
  ↓
DiscoveryService / MonitorService / NodeAgentService
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

`ServiceID(0)` 保留为节点内 Runtime caller。进程外部直接调用 `Runtime.Call` 时，远程 Envelope 的 Source 使用 `{Node: localNode, ID: 0}`；Service 发起调用时仍使用该 Service 自己的 `ServiceRef`。

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

源 Runtime 只有在 responder、caller 和 Session 都与 PendingCall 一致时才接受 Reply。

## 入站边界

Transport 握手得到的 peer NodeID 是连接身份。Runtime 接收入站 WireEnvelope 时必须验证：

1. `Source.Node` 等于握手 peer。
2. `Target.Node` 等于本地 NodeID。
3. Command 帧允许 `Session=0`，Reply 帧必须 `Session>0`。
4. Command 的 `CallPath` 末项必须是 Target；Send 不携带 `CallPath`。

校验或 payload 解码失败时，Call 返回稳定的 Runtime 错误；Send 记录指标并丢弃。不能把未校验的入站数据直接放进 Mailbox。

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
9. 入站 Source.Node 必须绑定握手身份，Reply 必须校验 caller、responder 和 Session。
10. Cluster 启动失败必须通过可失败构造函数返回，不能让半启动 Runtime 对业务可见。
