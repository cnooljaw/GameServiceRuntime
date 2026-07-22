# RFC-0350：PlayerModule 与玩家业务组合

> 状态：草案
> 目标阶段：Phase 12
> 范围：Business Layer
> 依赖：[RFC-0210](RFC-0210-Tooling-Snapshot.md)、[RFC-0340](RFC-0340-Business-PlayerService.md)
> 依据：`quix` 的 PlayerAgent 与模块生命周期设计

## 目的

本文定义 GSR 在业务层如何组织 PlayerService 内部的业务模块。

PlayerModule 是 Business Layer 概念，不进入 Core Runtime。

## 核心结论

可以学习 `quix` 的玩家业务组织方式：

```text
一个 PlayerService 拥有一个玩家的权威状态。
PlayerService 组合多个 PlayerModule。
PlayerModule 处理具体业务域。
```

但 Core Runtime 不知道 Player、Module、Online、Offline、Backup 等业务词汇。

## 分层位置

```text
Business Layer
  PlayerService
    ├── UserModule
    ├── WalletModule
    ├── RoomModule
    ├── ChatModule
    └── CrontabModule

Runtime Tooling
  Snapshot / Record / Monitor / Control Plane

Core Runtime
  Service / Command / Mailbox / Scheduler
```

## PlayerService

`PlayerService` 是玩家状态 owner。

职责：

- 加载玩家状态。
- 接收玩家相关 Command。
- 组合 PlayerModule。
- 处理上线、离线、重连、踢下线。
- 定期生成备份 Command 或调用 Repository。
- 对外只暴露 Command。

不负责：

- 直接维护 TCP/WebSocket 连接。
- 直接读写底层数据库连接。
- 直接绕过 Runtime 调用其他 Service 指针。

## PlayerModule

接口草案：

```go
type PlayerModule interface {
    Name() string
    OnLoad(ctx PlayerContext) error
    OnActivate(ctx PlayerContext) error
    OnOnline(ctx PlayerContext) error
    OnOffline(ctx PlayerContext) error
    OnBackup(ctx PlayerContext) error
    OnTimeEvent(ctx PlayerContext, event TimeEvent) error
}
```

所有回调都由 `PlayerService` 编排。`OnActivate`、`OnOnline`、`OnOffline`、`OnBackup` 和 `OnTimeEvent` 只能在对应 Command 的 Handler 内执行；它们不是绕过 Mailbox 的外部生命周期入口。`OnLoad` 只构造尚未注册的初始模块状态，不执行无界 IO。

模块之间共享的上下文必须通过 `PlayerContext` 暴露，不能互相持有具体模块指针。

## 协议映射

客户端协议不直接调用模块方法。

推荐流程：

```text
Client Packet
  ↓
Gateway Adapter
  ↓
ProtocolMapper
  ↓
CommandID + Payload
  ↓
PlayerService
  ↓
PlayerModule Handler
```

这保留 GSR 的统一 Command 模型，避免把 `service.client.xxx` 这类动态函数表带进 Go 实现。

## 数据保存

玩家状态保存属于业务层和持久化适配层。

推荐区分：

```text
PlayerState:
  有脏标记，定期保存。

Ledger / Log:
  append-only，直接写入记录表或事件流。
```

Wallet 相关数据必须遵守强一致和幂等规则，不能只靠 PlayerModule 的定期备份。

## 与 Runtime Tooling 的关系

PlayerService 可以使用：

- Snapshot：保存当前状态。
- Record：记录玩家 Command。
- Monitor：暴露模块耗时和状态。
- Control Plane：触发踢人、下线、保存等白名单管理命令。

这些都是外部工具能力，不改变 Core Runtime。

## 规则

1. PlayerModule 只属于 Business Layer。
2. Core Runtime 不内建玩家生命周期。
3. Gateway Adapter 只负责连接和转发，不拥有玩家状态。
4. 客户端包必须先映射成 Command。
5. PlayerService 是玩家状态唯一 owner。
6. 模块间交互必须通过 PlayerContext、Command 或明确的业务接口。
7. Wallet 一致性不能依赖普通 PlayerModule 定期保存。
