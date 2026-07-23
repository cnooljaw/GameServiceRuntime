# RFC-0350：PlayerModule 与玩家业务组合

> 状态：已接受
> 接受日期：2026-07-24
> 实现日期：2026-07-24
> 目标阶段：Phase 12
> 范围：Business Layer
> 依赖：[RFC-0210](RFC-0210-Tooling-Snapshot.md)、[RFC-0300](RFC-0300-Business-Layering.md)、[RFC-0340](RFC-0340-Business-PlayerService.md)
> 依据：模块扩展必须仍由 Player 的一个 Mailbox 串行编排

## 目的

本文定义 PlayerService 内部模块的稳定扩展点。PlayerModule 适用于背包、任务、个人房间入口等与单个玩家长期状态共址的小领域；它不是 Core 生命周期 hook，也不能绕过 Command 模型。

## 目标

- 用可声明 Command 集、显式 PlayerEvent 和 Snapshot 的接口替代隐式 OnOnline/OnBackup 回调表。
- 由 PlayerService 在自己的 Handler 内串行分派模块事件与命令，保证模块无法互相直接改状态。
- 使模块可测、可排序、可导出 snapshot，不要求模块各自成为 Service。

## 非目标

- 不定义动态模块热插拔、反射扫描、跨模块对象访问、数据库直接写入、后台 crontab goroutine 或具体钱包语义。
- 不要求 WalletModule：强一致账本必须使用 RFC-0360 的 WalletService。

## 分层与依赖

```text
ProtocolMapper -> PlayerService Command
  -> PlayerService validates identity/state
  -> one matching PlayerModule.Handle or all modules HandleEvent
  -> PlayerService commits visible PlayerState / replies
```

模块属于它的 PlayerService 实例，不能被另一个 Player 共享；模块之间的协作通过 PlayerService 的明确 Command/Event 或外部 Service RequestID 进行，不能互相保存具体模块指针。

## 公开契约

包路径为 `game`：

```go
type PlayerEventKind string
const (
    PlayerActivated PlayerEventKind = "activated"
    PlayerOnline    PlayerEventKind = "online"
    PlayerOffline   PlayerEventKind = "offline"
    PlayerBackup    PlayerEventKind = "backup"
)
type PlayerEvent struct {
    Kind       PlayerEventKind
    Generation string
    At         time.Time
}
type PlayerContext interface {
    gsr.CommandContext
    PlayerID() PlayerID
    AccountID() AccountID
    Now() time.Time
    Send(gsr.ServiceRef, gsr.CommandID, any) error
}
type PlayerModule interface {
    Name() string
    Commands() []gsr.CommandID
    Handle(PlayerContext, gsr.Command) error
    HandleEvent(PlayerContext, PlayerEvent) error
    Snapshot(PlayerContext) ([]byte, error)
}
```

Name 必须是去空白非空、最大 64 bytes 的稳定 ASCII 标识；一个 PlayerConfig 内不能重复。Commands 必须严格递增、非零且不与 Player 保留/其他模块 Command 重叠；没有外部 Command 的模块返回 nil。`Handle` 只收到自己声明的 Command；Event 按模块名词典序逐个分派，任一错误使当前 Player Command 返回错误且不继续分派后续模块。

## 状态与生命周期

PlayerService Init 之后以 `PlayerActivated` 调用所有 module；Online、Offline、Backup 分别由其保留 Command 产生 Event，所有事件发生在相应 Handler 内。模块的私有状态随 PlayerService 创建/关闭；模块 Snapshot 收集在 Get/Backup Command 中，按 Name 复制 bytes。模块不得自行调用 Start/Stop 或保留旧 Event/Context。

模块若需要异步外部工作，必须让 PlayerService 先冻结 RequestID 并 Send 到另一 Service/外部 runner，收到结果 Command 后再调用 Module.Handle；模块不能从 HandleEvent 中起 goroutine。

## 错误与失败语义

配置阶段拒绝空/重复名称、命令重叠、nil 模块或无效 Snapshot。Handler 阶段拒绝身份不匹配、未声明 Command、非法 Event 和无法序列化的 Snapshot。模块错误不会自动回滚模块已做的私有修改，因此模块必须先验证输入，或只在成功点写状态；跨模块原子更新不被承诺，应提升到 PlayerService 统一 Command。

## 并发与所有权

PlayerContext 仅在 Player Handler 有效；Module 不得缓存它、其 Self/Source、也不得把它传递给另一个模块。模块状态只在 Player Mailbox 读写；所有 bytes 和集合返回副本。没有模块可以访问 Runtime 的 Create/Stop 或 PlayerService 内部 map。

## 可观测性

Player Snapshot 使用模块名到匿名 Snapshot bytes 的映射；每次 Event/Command 记录模块名、PlayerID、RequestID（若有）和错误类别。模块不得将敏感业务数据绕过 Record Redactor 写入日志。

## 验收

- 模块排序、名称/Command 冲突、事件顺序、模块错误、Snapshot 副本和异步结果 Command 流程均有测试。
- AST/并发测试证明 Module 不创建 goroutine、不保存 PlayerContext，并且 Wallet 不能作为普通定期 Backup 模块实现。
