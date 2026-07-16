# RFC-0370：业务模板与状态归属

> 状态：草案  
> 范围：Business Layer  
> 依据：本地稳定运行的棋牌游戏框架、Skynet 服务拆分经验、`quix` 的玩家模块组织

## 目的

本文定义 GSR 如何沉淀业务模板，同时不把棋牌游戏的结构写进 Core Runtime。

## 核心结论

GSR 提供业务模板，不提供业务框架。模板解决一类已反复出现的业务问题；它们可以组合，但不共享可变权威状态，也不要求所有业务套用同一套生命周期。

```text
Core Runtime
  Service / Command / Mailbox / Scheduler
    ↑
Business Templates
  Player / Battle / Room / Match / Task / Wallet
    ↑
Game or product implementation
  Mahjong / card game / match game / social feature / operations feature
```

## 模板目录

| 模板 | 状态 owner | 适用问题 | 不负责 |
|---|---|---|---|
| PlayerService | 单个玩家的长期状态 | 上下线、玩家数据、玩家模块、个人入口 | 登录密钥、连接 IO、对局规则 |
| BattleService | 一组参与者的一次游戏活动 | 座位、准备、重连、回合、动作、计时器、结算编排 | 长期房间索引、钱包权威状态 |
| RoomService | 房间入口和 Battle 索引 | 创建、加入、分配和 Battle 生命周期索引 | Battle 内部强一致状态 |
| MatchService | 匹配队列和候选者 | 排队、匹配、创建 Room/Battle 请求 | 对局规则、玩家长期状态 |
| TaskService | 一类任务聚合状态 | 进度、领取、刷新和周期结算 | 通用玩家生命周期 |
| WalletService | 资金与流水 | 幂等扣增、账本和持久化 | Battle 规则、连接状态 |

模板中的 `Service` 名称不是强制代码名。只有职责和状态归属稳定时，才创建对应 Service；低频或纯查询能力可以作为现有业务 Service 的模块。

## 多人游戏模板

棋牌只是 Battle 模板的一种实现：

```text
RoomService
  owns: room entry / battle index / allocation policy

BattleService
  owns: participants / seats / ready / reconnect / round / actions / timers
```

对局开始后，相关强一致状态必须聚合到同一个 `BattleService`。`Table` 可以是 `BattleState` 中的棋牌游戏字段，但不是默认 Service。

基于旧框架中 Room 与 Attack 服务的经验，GSR 第一版不拆分“桌子状态”和“本局状态”。只有长期场馆、跨局桌位、独立迁移或明显容量边界出现时，才以测得的压力拆分。

## 无锁协作规则

Service 的 Mailbox 是该 Service 的串行写入口。跨 Service 协作采用消息和结果，不采用共享锁。

```text
错误：user lock -> table lock -> Call other service -> 修改本地状态

正确：Command -> owner Mailbox -> 生成外部请求(RequestID)
      -> 外部结果 Command -> owner Mailbox -> 状态推进
```

规则：

1. 一份可变权威状态只有一个状态 owner。
2. 不持有另一个 Service 的对象指针，不访问其内部 map 或状态。
3. 不增加跨 Service 的嵌套锁；锁只可用于 Runtime 内部无业务语义的数据结构。
4. 外部请求可重试时，接收方必须按 `RequestID` 幂等处理。
5. `Call` 只用于读取或能够明确处理超时和重试的短事务；不要在状态变更中把它当分布式锁。
6. 超时后的业务状态必须由后续 Command、查询或补偿处理，不能假定远端一定未执行。

## 重连与录像

稳定游戏服务需要把重连与复盘作为模板能力，而不是附加脚本：

- Gateway 负责连接断开和已认证会话绑定；`BattleService` 决定断线后的业务规则。
- Battle Snapshot 返回当前可见状态、`BattleEpoch` 和 `TimelineRev`。
- 录像优先记录进入 Battle 的 Command、Timer 触发、随机种子和结算结果；业务事件可作为面向回放展示的投影。
- Record/Replay 是工具能力，不替代资金流水、持久化或审计日志。

## 不从旧框架继承的做法

1. 不让 Agent 同时承担认证、密钥协商、协议编解码、玩家状态和房间业务。
2. 不以全局服务表、动态继承或跨 Service 直接调用维护业务关系。
3. 不用用户锁、桌子锁等锁顺序解决状态归属不清的问题。
4. 不把具体麻将或棋牌命名提升为 Runtime 的公开概念。

## 验收

新增业务模板时，至少回答：

1. 它拥有哪一份权威状态？
2. 哪些 Command 可以修改它？
3. 它与其他模板交互时的 `RequestID` 和超时语义是什么？
4. 是否真的需要独立 Service，还是一个现有 Service 的业务模块即可？
5. 重连、持久化和回放分别由谁负责？
