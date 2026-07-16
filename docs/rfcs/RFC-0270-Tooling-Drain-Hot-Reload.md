# RFC-0270：Drain、热更新与访问者追踪

> 状态：草案  
> 范围：Runtime Tooling、Cluster Control Plane  
> 依据：`skynet_fly` 的热更新、访问者追踪和旧服务退出流程

## 目的

本文定义 GSR 的平滑下线和热更新切换。

这里的热更新不是 Go 进程内代码热补丁，而是：

```text
启动新实例
  ↓
切换流量
  ↓
Drain 旧实例
  ↓
关闭旧实例
```

## Core Runtime 边界

Core Runtime 只需要支持已有生命周期状态和 `Stop`。

Drain、访问者追踪、服务组切换属于扩展层。

禁止为热更新污染 Core Runtime 的最小接口。

## 状态模型

建议扩展层使用：

```text
Loading
Running
Draining
DrainReady
Closing
Closed
StartFailed
```

这些状态可以映射到 Core Runtime 的 `ServiceStatus`，但不要求 Core Runtime 原生暴露所有细节。

## 切换流程

```text
Create new Service instances
  ↓
Health check new instances
  ↓
Publish new ServiceSet Version
  ↓
Clients switch refs
  ↓
Mark old instances Draining
  ↓
Reject new external traffic
  ↓
Wait visitors release
  ↓
Stop old instances
```

如果新实例启动失败：

```text
Cancel switch
  ↓
Keep old ServiceSet Version
  ↓
Stop failed new instances
```

## Visitor Tracking

访问者追踪用于判断旧实例是否仍被使用。

最小模型：

```go
type VisitorTracker interface {
    AddVisitor(target ServiceRef, visitor ServiceRef, weak bool)
    RemoveVisitor(target ServiceRef, visitor ServiceRef)
    ActiveVisitors(target ServiceRef) []VisitorRef
}
```

访问者可以来自：

- 长期持有的 ServiceGroup client。
- 订阅关系。
- 正在执行的 Call。
- Game Layer 会话绑定。

## Weak Visitor

弱访问者不阻止旧实例退出。

典型场景：

- 双向依赖的系统服务。
- 只读缓存订阅。
- 可丢弃的观测连接。

Weak Visitor 必须显式声明，不能默认推断。

## 与 ServiceGroup 的关系

热更新切换通过 ServiceSet 版本完成：

```text
v1 -> [old refs]
v2 -> [new refs]
```

客户端 watch 到新版本后，根据自身策略切换。

旧版本进入 Drain。

## 与 Control Plane 的关系

控制面只负责编排：

```text
CmdStartNewVersion
CmdSwitchServiceGroup
CmdDrainService
CmdRollbackServiceGroup
```

控制面不能绕过生命周期直接杀 Service。

## 不做什么

第一版不做：

- 任意代码热替换。
- 动态替换已加载函数。
- 强依赖 Redis、Consul 或外部注册中心。
- 自动迁移所有内存状态。

状态迁移必须由具体 Service 显式实现 Snapshot/Restore 或业务迁移 Command。

## 验收

必须能验证：

- 新实例启动失败时旧实例继续服务。
- ServiceSet 版本切换后新请求进入新实例。
- 旧实例 Drain 后不再接收新外部流量。
- 有强访问者时旧实例暂不关闭。
- 只有弱访问者时旧实例可以关闭。
- 超时后 Drain 返回明确错误或进入人工处理。

## 规则

1. Drain 不进入 Core Runtime 最小接口。
2. 切换通过 ServiceSet 版本表达。
3. 旧实例退出前必须可观测。
4. 访问者追踪必须能防止旧实例过早关闭。
5. Weak Visitor 必须显式声明。
6. 热更新不等于代码热补丁。
