# RFC-0250：Cluster Control Plane

> 状态：草案  
> 范围：Runtime Tooling、Admin  
> 依据：`hanxi/skynet-admin` 对 Skynet cluster 的管理面补充

## 目的

本文定义 GSR 的集群控制面。

Cluster Data Plane 负责业务消息跨节点投递。Cluster Control Plane 负责节点管理、健康检查、观测查询和受控运维命令。

二者必须分离，避免把管理后台能力塞进 `ClusterTransport`。

## 从 skynet-admin 学什么

`skynet-admin` 对 Skynet cluster 的启发是：

- Cluster 需要管理面，不只是远程 `call/send`。
- 节点配置和节点运行状态要分开。
- 可以通过远端管理 Service 查询节点详情。
- 管理后台只是适配层，核心能力仍应落在 Runtime Service。
- 远程注入能力适合调试，但不适合直接进入生产 Runtime。

GSR 学习这些方向，不照搬任意代码注入。

## 分层

```text
Admin API / CLI / Console
  ↓
ClusterControlService
  ↓
DiscoveryService / MonitorService / NodeAgentService
  ↓
Command
  ↓
Service
```

HTTP、CLI、Web Console 都是适配层。它们不能直接访问 `ClusterTransport`、`Scheduler`、`Mailbox` 或 Service 指针。

## ClusterControlService

`ClusterControlService` 是控制面的编排者。

职责：

- 读取节点 Desired State。
- 查询节点 Observed State。
- 触发节点列表刷新。
- 调用远端 `NodeAgentService`。
- 汇总节点详情。
- 记录管理命令审计。

建议 Command：

```text
CmdListNodes
CmdGetNodeStatus
CmdGetNodeDetail
CmdReloadClusterConfig
CmdDrainNode
CmdSwitchServiceGroup
CmdRollbackServiceGroup
CmdEnableNode
CmdDisableNode
```

第一版可以只实现只读命令和 reload。`DrainNode`、`EnableNode`、`DisableNode` 可以后置。

## NodeAgentService

每个节点可以启动一个系统级 `NodeAgentService`。

职责：

- 响应 ping。
- 返回节点运行信息。
- 返回 Service 列表。
- 返回 Mailbox、PendingCall、慢 Command 等观测信息。

建议 Command：

```text
CmdPingNode
CmdGetNodeStats
CmdListServices
CmdGetServiceStats
CmdGetMailboxStats
CmdGetSlowCommands
CmdGetPendingCalls
```

`NodeAgentService` 默认只读。需要修改运行状态的命令必须单独列入高危白名单。

## Desired State 与 Observed State

控制面必须区分配置期望和运行观测。

Desired State：

```go
type NodeDesiredState struct {
    ID      NodeID
    Address string
    Role    string
    Enabled bool
}
```

Observed State：

```go
type NodeObservedState struct {
    ID        NodeID
    Status    NodeStatus
    LastSeen  time.Time
    Latency   time.Duration
    Version   string
    LastError string
}
```

典型状态：

```text
Configured but disconnected
Enabled and connected
Disabled by admin
Unknown node
Version mismatch
```

这能避免把“配置里有这个节点”和“这个节点当前可用”混成一个布尔值。

## 节点详情

节点详情至少包括：

- NodeID。
- 地址。
- 版本。
- 连接状态。
- 最近心跳。
- Service 数量。
- Mailbox 概览。
- Pending Call 数量。
- 慢 Command 统计。
- Runtime 内存指标。

Game Layer 可以追加 Battle 数量、Room 数量、在线玩家数等业务指标，但这些不进入 Core Runtime。

## 安全边界

生产 Runtime 不支持远程任意代码注入。

允许：

- 白名单管理 Command。
- 只读观测命令。
- 受权限控制的节点 reload。
- 受权限控制的 Drain 和服务组切换。
- 审计所有管理命令。

禁止：

- 远程执行任意 Go 代码。
- Admin API 直接拿 Service 指针。
- Web Handler 直接操作 `ClusterTransport`。
- 生产环境默认开放危险命令。

后续需要补充：

- 管理面认证。
- 管理面授权。
- 审计日志格式。
- 高危命令二次确认。

## 与 Data Plane 的关系

控制面可以复用 Runtime 消息模型：

```text
Command
  ↓
Envelope
  ↓
Local or Remote
  ↓
NodeAgentService
```

但控制面不能影响业务消息投递路径的简单性。

`ClusterTransport` 仍然只负责传输 `Envelope`，不理解节点详情、管理后台、Service 统计或业务指标。

## 第一版验收

第一版完成后必须能做到：

- 查询所有配置节点。
- 区分节点期望状态和观测状态。
- ping 单个节点。
- 查询单个节点详情。
- 查询节点 Service 列表。
- 查询 Mailbox 和 Pending Call 概览。
- 管理命令有审计记录。
- 无任意代码注入接口。

后续版本再支持：

- 触发节点 Drain。
- 切换 ServiceGroup 版本。
- 回滚 ServiceGroup 版本。

## 规则

1. Control Plane 不得新增第二套 RPC。
2. Control Plane 必须通过系统 Service 和 Command 实现。
3. Admin API 只能调用控制面 Service。
4. `NodeAgentService` 默认只读。
5. 高危命令必须显式白名单、权限校验和审计。
6. Data Plane 和 Control Plane 的错误指标必须分开统计。
7. Control Plane 只能编排 Drain 和切换，不能绕过生命周期直接关闭 Service。
