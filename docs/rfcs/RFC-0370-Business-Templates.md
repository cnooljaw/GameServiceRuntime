# RFC-0370：业务模板与状态归属

> 状态：待实现
> 目标阶段：Phase 12
> 范围：Business Layer
> 依赖：[RFC-0300](RFC-0300-Business-Layering.md)、[RFC-0310](RFC-0310-Business-Battle.md)、[RFC-0330](RFC-0330-Business-Room.md)、[RFC-0340](RFC-0340-Business-PlayerService.md)、[RFC-0360](RFC-0360-Business-WalletService.md)
> 依据：模板只固化 owner、Command 与失败收敛，不把某一游戏变成框架

## 目的

本文给出选择和组合 GSR 业务模板的规范。它不是第二套 API：具体类型、Command 与错误语义以 RFC-0310 至 RFC-0360 为准；本文确保在增加 Match、Task 或具体玩法时仍能判断状态该归谁。

## 目标

- 固化模板目录、依赖方向、何时应拆为独立 Service 和何时应留在 Module。
- 给出跨 Service RequestID、超时、补偿、重连、Snapshot 与 Record 的统一裁决。
- 为新增模板提供一份能直接转为 RFC/测试的验收清单。

## 非目标

- 不实现 MatchService、TaskService、排行榜、聊天、Guild 或任何具体玩法；这些能力出现时必须新增 RFC。
- 不用“模板”绕过领域建模，不以通用 callback/全局注册表取代公开 Command。

## 分层与依赖

```text
PlayerService (long-lived player state)
  -> RoomService (entry/index) -> BattleService (one activity)
  -> WalletService (ledger result)
  -> external Repository / Ledger / Archive adapters
```

箭头只能表示 Command、Result Command 或只读查询，不能表示持有 Service 对象指针、共享 map 或跨 Service 锁。Gateway/ProtocolMapper 是入口 adapter，不在图中拥有领域状态；Core 仅提供 Service/Command/Timer。

## 公开契约

模板的稳定命名和 owner 如下：

| 模板 | 权威状态 | 典型 Command | 何时独立 | 不负责 |
|---|---|---|---|---|
| PlayerService | 单玩家长期状态、连接可达性、模块状态 | online/offline、绑定、模块命令 | 玩家数据可独立恢复/路由 | 认证、socket、账本 |
| RoomService | 成员与 Battle 索引 | join/leave/start/result | 房间入口与单局生命周期不同 | 回合、座位、timer |
| BattleService | 一局参与者、规则、Timeline、结算编排 | start/game input/finish/result | 一局存在强一致状态与短生命周期 | Player/Wallet 权威写入 |
| WalletService | RequestID、账本请求、余额结果 | commit/get/recover | 强一致资金或资产 | 游戏规则、连接 |
| PlayerModule | 与单玩家共址的私有模块状态 | 已声明 Player Command/Event | 无独立扩缩/持久化边界 | 其他玩家/全局状态 |

新增 `MatchService` 或 `TaskService` 时必须声明新 CommandID 区段、State、RequestID 语义和返回副本规则，不得复用 Battle 的内部 Timeline/Wallet 私有 Command。

## 状态与生命周期

选择规则：需要独立 Service 的条件是它拥有独立的可变权威状态、可独立寻址/恢复、或者会被多个 owner 以 Command 协作；否则优先作为当前 owner 的 Module/Logic。一个业务实体在一个时刻只能有一个写 owner；投影/缓存不是 owner。

短生命周期 Battle 只在 Finished/Failed 且外部 Settlement 结果已收敛后由组合根 Stop。长期 Player/Room 的持久化由外部 writer 与结果 Command 编排；Wallet 永远以 LedgerStore 为权威。Record 是输入诊断，Snapshot 是恢复/投影，二者都不是 Wallet ledger。

## 错误与失败语义

所有会跨 Mailbox/进程重试的变更都带 RequestID。接收者必须存储或能查询 pending/terminal 结果；发起者在 Send/Call timeout 后通过 Result Command、Get 或人工补偿收敛。补偿是新、可审计的 RequestID，不允许把旧 Command 重放为“撤销”。一个流程的部分成功必须在 owner state 中显式显示，不能在 log 中猜测。

## 并发与所有权

```text
错误：Player lock -> Room lock -> Call Battle -> 继续修改 Player
正确：Player Command -> freeze RequestID -> Send Room/Battle
      -> result Command -> Player Command -> state transition
```

业务代码不得直接创建 goroutine、保存 ServiceContext、共享可变领域对象或把 Call 当分布式锁。外部 I/O worker 仅由组合根拥有，并满足有界提交、取消/Close、真实返回等待和结果 Command 回写。

## 可观测性

每个流程必须能由 `(owner stable ID, RequestID, phase, error category)` 关联。重连使用 BattleSnapshot 的 Epoch/Timeline Revision；复盘使用 Record Bundle；资金审计使用 LedgerStore。Metric 快照仍只通过 Runtime Inspect 获取，业务视图以只读 Command/Snapshot 暴露。

## 验收

新增模板前必须在 RFC 中回答并在测试中证明：

1. 唯一 owner 与可变状态是什么；哪些 Command 能写它。
2. 创建、停止、持久化和外部 I/O 分别由谁拥有。
3. 跨 Service RequestID、超时、结果查询和补偿如何收敛。
4. 错误/迟到/重复/关闭时的不可变事实是什么。
5. Snapshot、Record、审计和敏感字段脱敏分别在哪里完成。
