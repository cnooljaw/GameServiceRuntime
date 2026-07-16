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
