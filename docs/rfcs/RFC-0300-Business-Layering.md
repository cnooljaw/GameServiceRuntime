# RFC-0300：Business Layer 分层

> 状态：已接受
> 接受日期：2026-07-24
> 实现日期：2026-07-24
> 目标阶段：Phase 12
> 范围：Business Layer
> 依赖：[RFC-0100](RFC-0100-Core-Service.md)、[RFC-0130](RFC-0130-Core-Send-Call-Reply.md)、[RFC-0210](RFC-0210-Tooling-Snapshot.md)、[RFC-0280](RFC-0280-Tooling-Command-Record-Replay.md)、[RFC-0290](RFC-0290-Tooling-LoginService-Gateway.md)
> 依据：领域状态只经所属 Service 的 Command 串行修改

## 目的

本文定义 GSR 的第一套可复用 Business Layer：它提供 Battle、Room、Player、Timeline、Broadcast、Wallet 的小型组合模板和稳定术语，但不把任何游戏规则、数据库或客户端协议放入 Core 或 Tooling。

## 目标

- 在 `game` 包提供领域 ID、RequestID、创建接口、错误模型和只依赖 Core 的窄运行时能力。
- 使业务状态变更只能经 Service Command；跨 Service 流程以 RequestID、结果 Command 和明确终态推进。
- 让组合根是唯一能 `CreateService`、配置持久化/外部 I/O adapter、选择游戏规则和停止短生命周期实例的位置。
- 各模板的具体公开 API 以 RFC-0310 至 RFC-0370 为准。

## 非目标

- 不提供 ORM、数据库连接池、登录认证、Socket/HTTP 协议、通用 ECS、场景同步、自动扩缩容或 Controller。
- 不要求所有业务使用 Battle/Room/PlayerModule；低频或单 owner 状态可以是一个独立业务 Service。
- 不让 `runtime` 或 `tooling` import `game`，不以全局 Service 表、Service 指针、用户锁或桌子锁共享业务状态。

## 分层与依赖

```text
application / examples composition root
  -> game (Battle, Timeline, Room, Player, Wallet templates)
  -> tooling (Gateway, Snapshot, Record, Directory)
  -> runtime (Service, ServiceRef, Command, Send, Call, After)
```

业务 ProtocolMapper 位于 application/game 外层：它把已认证连接输入转换为 `CommandID + payload`，再调用 Runtime Send/Call。Gateway 只持有连接绑定，不持有 Player、Battle 或 Wallet 的权威状态；业务 Service 不接收 ticket、proof 或 secret 明文。

## 公开契约

第一版所有通用业务类型位于 `game`：

```go
type PlayerID string
type AccountID string
type BattleID string
type RoomID string
type RequestID string
type BattleEpoch uint64
type TimelineID uint64
type TimelineRevision uint64

type ServiceCreator interface {
    CreateService(gsr.ServiceSpec) (gsr.ServiceRef, error)
}
type CommandRuntime interface {
    Send(gsr.ServiceRef, gsr.CommandID, any) error
    Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

var (
    ErrInvalidID          error
    ErrInvalidRequestID   error
    ErrInvalidParticipant error
    ErrRequestConflict    error
    ErrStateConflict      error
    ErrUnavailable        error
)
```

ID 必须为去除首尾空白后非空、有效 UTF-8、最大 128 bytes 的稳定字符串；RequestID 由入口/发起方生成，最大 128 bytes，同一 owner 内重复使用时必须逐字段匹配原始规范化请求。所有公开返回的 slice、map、bytes 和 state 均为独立副本，零 `ServiceRef` 仅表示“未绑定”，不得作为可投递目标。

业务 CommandID 按包分段，不与 Tooling 复用：

```text
0x030001xx Battle
0x030002xx Timeline（仅 BattleService 内部投递）
0x030003xx Room
0x030004xx Player
0x030005xx Wallet
```

应用可使用自己的 CommandID，但必须避开这些区间；每个 Service 的 Commands 返回完整且无重复的集合。

## 状态与生命周期

每一份可变业务状态只有一个 owner：Battle 拥有单局状态，Room 拥有房间与 Battle 索引，Player 拥有单个玩家长期状态，Wallet 拥有账本请求与结果。创建由组合根或显式 Factory Service 完成，Factory 的结果作为新 Command 返回；Service Handler 不保存完整 Runtime，也不直接创建另一个 Service。

短生命周期 Battle 的 Stop 由外层生命周期 owner 在业务终态、所有外部结果已收敛之后调用；Battle Handler 只记录终态和发送通知。持久化 adapter、Archive runner、Ledger writer 等外部任务必须由组合根拥有、关闭时等待真实返回，并把业务可见结果以 Command 交还 owner。

## 错误与失败语义

入口参数错误在投递前返回 `ErrInvalid*`；Command handler 中的业务拒绝通过 Call Reply 或业务结果 Command 表示，不能把可重试外部超时误写成“未执行”。跨 Service 请求的接收方按 RequestID 保持 pending/committed/rejected 等稳定结果；超时方必须查询或等待后续结果 Command，不得以 Call 超时推断远端未修改状态。

## 并发与所有权

Service 的 Mailbox 是它的唯一业务写入口。一个 Handler 可以 Send/Call 其他 Ref，但不得在本地状态写到一半后依赖同步 Call 完成跨 Service 原子事务；应先冻结请求，再由结果 Command 推进。业务 Service 和 Module 不得启动 goroutine、保留 ServiceContext 或直接访问另一个 Service 的内部对象。

## 可观测性

业务模板暴露只读 Get/Snapshot Command 或使用 Snapshot Tooling；运行时 Metrics 始终来自 `Runtime.Inspect().Metrics`。日志和 Record 只记录稳定 ID、RequestID、阶段和错误类别，任何账号密钥、票据、个人敏感字段均由 ProtocolMapper/Record Redactor 在进入记录前脱敏。

## 验收

- 编译依赖证明 `runtime`、`tooling` 不 import `game`，而 `game` 只经公开 Core 接口使用 Runtime。
- 每个模板的重复 RequestID、Call 超时、延迟结果、Stop 竞争和返回副本测试都遵循本文规则。
- 示例能从 Gateway/Mapper 到 Player/Room/Battle/Wallet 跑完一个 Command 流程，且无 Service 直接创建 goroutine。
