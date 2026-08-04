# 宁海双扣（NHSK）GSR 示例

这个目录是宁海双扣在 Game Service Runtime（GSR）中的第一阶段示例。它的目标是逐步替换旧系统的 GameLogic：保留旧 GameMaster/Agent/客户端使用的 TCP 二进制流，同时让新的 Cluster 调用者能够直接解析 BattleRef，并对同一个 Battle 使用 `Send`/`Call`。

本目录只保存 GSR 侧的实现。`/Users/lijiawang/Documents/cocos/laya/nhsk`、`gamelogic`、`gamemaster`、`gamecore`、`protocol`、`baison_middle/protocol` 和 `nbgame_core` 是只读知识来源；实现没有编辑这些目录。每个完成的切片都记录在仓库的 [`docs/reviews/nhsk-reference-reconciliation.md`](../../docs/reviews/nhsk-reference-reconciliation.md) 中。

## 当前状态

目前已完成的是一个可测试的最小纵向切片：

- Legacy `0x7701 OUT_CARD`、`0x7702 CARD_ACTION`、`0x720A USER_STATE_CHANGE` 的固定字节解码、relay 身份核对和显式 MessageID→CommandID 映射。
- NHSK `BattleService` 的初始化、四座玩家更新、准备/开始小局、确定性发牌、基础出牌/过牌、预览选牌、托管开关、离线/重连状态和 Snapshot。
- `NHSKHostService` 的 BattleID 索引、异步创建操作、BattleRef 解析和精确停止；`BattleFactoryService` 负责 Runtime 创建/停止。
- Legacy relay 和 Cluster 调用都归一化为同一套类型化 Command，并进入同一个 Battle Mailbox。
- 类型化 `GameOutputBatch` 到 `GameOutputService` 的交付边界，Legacy encoder 仍在该边界之外。

这不是“已经可以无损替换生产旧 GameLogic”的声明。完整旧协议控制消息、origin/TCP connection owner、NEW_GAME/INIT/UPDATE_GAME 全量接入、完整双扣牌型/抓分/单扣双扣结算、回放、AI、Quarantine 取证和独立可部署进程仍属于后续切片。RFC 明确要求在达到这些验收条件前，不把示例描述为生产替换品。

## 代码结构

```text
examples/nhsk/
├── commands.go                 # Host/Battle/玩法 CommandID 与公开 request/result
├── battle.go                   # NHSKBattleService：单桌 Mailbox、阶段、手牌、动作
├── host.go                     # NHSKHostService + BattleFactoryService
├── outputs.go                  # GameOutput、ClientGameOutput、GameOutputBatch、payload
├── output_service.go           # 当代连接的输出 Service 与 sink 边界
├── legacy_mapper.go            # 单个客户端 payload → GSR Command
├── legacy_relay_mapper.go      # 0x8605+0x7402 relay → Host Resolve → Battle Send/Call
├── legacy_egress.go             # 类型化输出 → Legacy frame 的组合边界
├── internal/legacywire/
│   ├── header.go               # BSHeader、GameHeader、固定长度和 MessageID
│   ├── codec.go                # 0x8605/0x7402 relay 与 suffix
│   ├── client_gameplay.go      # 客户端玩法 MessageID 分类
│   ├── out_card_request.go     # 0x7701 55 字节输入
│   ├── card_action_request.go  # 0x7702 51 字节输入
│   ├── user_state_change.go    # 0x720A 32 字节输入
│   └── ...                     # 已确认输出/控制消息的固定 codec
├── config.go                   # gamelogic 配置与环境变量解析
├── logging.go                  # 结构化日志字段与脱敏边界
├── node.go                     # 节点 readiness 与关闭顺序模型
└── *_test.go                   # golden、边界、Mailbox、Host 和路由测试
```

`examples/nhsk` 是可导入的 `package nhsk`，不是不可复用的 `package main`。这样 Cluster 调用方可以直接引用 `PlayCardsCommand`、`PlayCardsRequest` 等公开 API；真正的进程组合根应放在独立 `cmd/` 目录，不把业务包改成 `main`。

## 旧系统调用模式

旧系统有多层 envelope，同一个玩家动作在不同节点被反复包裹：

```text
客户端 -> Agent
  BSHeader(Type=0x7402) + Suffix

Agent -> GameMaster
  BSHeader(Type=0x7402) + GameHeader + Suffix

GameMaster -> GameLogic
  GLHeader(Type=0x8605)
  + BSHeader(Type=0x7402)
  + GameHeader
  + Suffix
```

旧 GameLogic 通过外层 `GameInnerID` 找到牌局，通过外层 `UserID` 找到玩家，再由 `OnMsg` 按内层 MessageID 分发到 NHSK。内层 `TGameHeader.UserID` 是重复身份，应该与外层 UserID 一致；初始化完成后，MatchID/ProductID 也需要和本局已冻结身份一致。校验通过后，业务只需要 BattleID、玩家和 payload，不应把三层 header 继续带进牌局状态。

旧 GameLogic 的客户端输出反向包裹：

```text
GameLogic -> GameMaster
  GLHeader(Type=0x8644)
  + BSHeader(Type=0x7400)
  + GameHeader(UserID=目标玩家)
  + NHSK payload
```

广播不是 UserID=0 的一个包，而是按座位/目标玩家展开成多个目标包。GameMaster 收到后再把 `BSHeader + GameHeader + Suffix` 发回 Agent。

### 旧 MessageID 与处理方式

| 旧 MessageID | 旧含义 | 当前归一化目标 |
|---:|---|---|
| `0x7701` | `OUT_CARD` | `PlayCardsCommand` |
| `0x7702` | `CARD_ACTION` 选牌预览 | `PreviewCardSelectionCommand` |
| `0x720A` | `USER_STATE_CHANGE` 托管开关 | `SetPlayerAutoStateCommand` |
| `0x7601..0x7609`、`0x7611` | NHSK 客户端输出 | `GameOutput` 后由 Legacy egress 编码 |
| `0x8605` | GM→GL relay envelope | 不是业务 Command，只做边界解码 |
| `0x8644` | GL→GM 输出 envelope | 不是业务 Command，只做边界编码 |

旧 TCP 入口没有同步 GSR Reply。它的业务语义是：完整 frame 解码成功后，把 Command `Send` 到 Battle；坏 frame 或身份冲突只丢当前 frame，并由连接 owner 负责日志、计量和必要的连接处理。

## 新的调用模式

新的调用模式将“路由”和“业务”拆开。Host 只拥有 BattleID 到当前 BattleRef 的权威索引，不代理每一条牌局消息。

### 1. 创建和解析 Battle

```go
operation, err := runtime.Call(ctx, hostRef,
    nhsk.BeginCreateBattleCommand,
    nhsk.CreateBattleRequest{BattleID: 12345},
)
// operation 先是 creating；完成后：
resolved, err := runtime.Call(ctx, hostRef,
    nhsk.ResolveBattleCommand,
    nhsk.ResolveBattleRequest{BattleID: 12345},
)
battleRef := resolved.(nhsk.ResolveBattleResult).Ref
```

Host 把创建请求交给 `BattleFactoryService`。Factory 在组合根的 `ServiceCreator` 边界创建 Battle Service，返回完整 `ServiceRef`，Host 确认结果后才把 Battle 放入 Active 索引。调用者不得猜 ServiceID，不得自己拼 `ServiceRef`，也不得缓存跨进程/重启的旧 Ref。

### 2. 初始化和推进生命周期

```go
_, _ = runtime.Call(ctx, battleRef,
    nhsk.InitializeBattleCommand,
    nhsk.InitializeBattleRequest{
        Identity: nhsk.BattleIdentity{
            BattleID: 12345, ProductID: 82, MatchID: 88, RoundID: 1,
        },
    },
)
_, _ = runtime.Call(ctx, battleRef,
    nhsk.UpdatePlayersCommand,
    nhsk.UpdatePlayersRequest{Players: fourPlayers},
)
_, _ = runtime.Call(ctx, battleRef,
    nhsk.PrepareSubgameCommand,
    nhsk.PrepareSubgameRequest{GameNum: 1, SubgameNum: 1},
)
_, _ = runtime.Call(ctx, battleRef, nhsk.StartSubgameCommand, struct{}{})
```

当前最小 Battle 阶段为：

```text
AwaitingInit -> Preparing -> Playing -> Finished
```

所有阶段、玩家、座位、手牌、当前行动人、VerifyCode 和 Revision 都只在 Battle Mailbox Handler 中改变。Battle 不持有另一个 Service 指针，不直接创建 goroutine，不在 Handler 中 Stop 自己。

### 3. Cluster 的 Send/Call

需要结果时使用 `Call`：

```go
value, err := runtime.Call(ctx, battleRef,
    nhsk.PlayCardsCommand,
    nhsk.PlayCardsRequest{
        Player: "1001", Cards: []byte{3}, VerifyCode: 1,
    },
)
result := value.(nhsk.ActionResult)
```

不需要同步结果时使用 `Send`：

```go
err := runtime.Send(battleRef,
    nhsk.SetPlayerAutoStateCommand,
    nhsk.SetPlayerAutoStateRequest{Player: "1001", Enabled: true},
)
```

`Call` 的 Reply 只表达 Command 是否应用以及稳定拒绝原因，不携带客户端广播。Call 超时只表示调用方没有及时拿到 Reply，不取消已经进入 Mailbox 的 Command；当前示例不额外引入通用幂等缓存，调用方超时后应先查询 Snapshot。

### 4. Legacy 与 Cluster 共用一条业务路径

Legacy relay 入口的核心代码路径是：

```text
0x8605 + 0x7402 relay
  -> DecodeInboundGameRelay
  -> 校验 BattleID / 外层 UserID / 内层 UserID
  -> 0x7701/0x7702/0x720A 显式 MessageID 映射
  -> Host ResolveBattle(BattleID)
  -> BattleRef + Command
  -> runtime.Send（旧 TCP 语义）
```

代码调用：

```go
err := nhsk.RouteLegacyGameplaySend(ctx, runtime, hostRef, relayFrame)
```

测试或受信任的内部适配器如果需要同步结果，可以调用同一条归一化路径的 `Call` 版本：

```go
value, err := nhsk.RouteLegacyGameplayCall(ctx, runtime, hostRef, relayFrame)
```

这两个入口不会各自实现牌局逻辑：它们最后都进入 `NHSKBattleService.Handle`，因此旧 TCP 与新 Cluster 的状态门禁、VerifyCode、手牌校验和输出顺序是一套业务实现。

## 输出边界

Battle 只产生类型化值，例如：

```go
ClientGameOutput{
    Targets: []game.PlayerID{"1001", "1002", "1003", "1004"},
    Kind:    nhsk.OutputOutCardInfo,
    Payload: nhsk.OutCardInfoPayload{...},
}
```

它们被组成 `GameOutputBatch` 后发送到当代 `GameOutputService`。Battle 不构造 `GLHeader`、`BSHeader` 或 `GameHeader`；Legacy egress 才负责把每个 Target 展开为：

```text
0x8644 GLHeader + 0x7400 GameHeader(UserID=Target) + payload
```

Cluster/Agent 适配器则只消费 `UserID` 和类型化 payload，用自己的 SessionRegistry 路由，不依赖 Legacy envelope。

## 状态权威边界

当前示例不依赖 MySQL 或 Redis 才能启动：

| 数据 | 当前权威位置 | MySQL/Redis 计划 |
|---|---|---|
| 活动 Battle、阶段、手牌、座位、行动、托管 | Battle Service 内存 | 首版不外置；进程崩溃不承诺恢复 |
| BattleID→BattleRef | NHSKHostService 内存 | 重启后重新创建/解析，不恢复旧 Ref |
| 输出当代与 sink | GameOutputService/连接 owner | 不写业务数据库 |
| 配置、MySQL DSN、Redis 地址 | `config.go` | 只提供工具模块、连接/健康检查/关闭；不定义业务 schema/key |
| 登录账号、共享 token、微信凭据 | 后续 Auth/Login 进程 | 不进入 Battle 日志或 Snapshot |

`GameInnerID` 在第一阶段直接作为 `game.BattleID` 使用；它是短生命周期业务编号，0 无效，默认编号上限 10000，可配置。Battle Service 的运行身份仍是完整 `ServiceRef`。编号只有在精确 Service Stop 返回后才允许复用。

## 旧目录与新目录的关系

- 旧目录是行为知识和 wire golden 的来源，不被新实现 import 成业务运行时依赖。
- `internal/legacywire` 只复制协议边界所需的最小固定布局，不复制旧容器、旧 `ServiceRef` 或旧全局状态。
- `game` 提供通用 `Service`、Mailbox、Timer、Battle 模板；`examples/nhsk` 只放 NHSK 的身份、规则、输出和 adapter。
- 后续第二阶段替换 GameMaster 时，新的 GM 直接调用 `.nhsk-game-host`，保存 Host 返回的 BattleRef，再对 Battle Ref `Send`/`Call`；不再生成 `0x8605` relay。
- 后续第三阶段替换 Agent 时，新的 Agent 使用同一批 `ClientGameOutput.Targets` 和同一套玩法 Command，不要求 Battle 知道客户端连接。

## 当前未实现的明确边界

以下能力不能从当前最小切片推断为已完成：

1. 单条主动 TCP GM 连接、双向 origin、指数退避重连、ConnectionGeneration fencing。
2. Legacy NEW_GAME/INIT/UPDATE_PLAYER/UPDATE_GAME/MATCH_STOP/DEL_GAME/结算 ACK 的全量控制 codec 和 ACK 时序。
3. 完整 104 张牌的随机/新手/散牌调整、所有牌型、跟牌压制、抓分、单扣/双扣和结算。
4. 唯一 ActionDeadline、托管自动出牌、外部 AI、回放 writer 和 GAME_OVER 完整线序。
5. Quarantined Battle、诊断导出 receipt、人工释放和节点 Degraded 的完整实现。
6. Gateway、Login、Auth、Agent、微信 provider、`account + shared token` 开发认证进程，以及 MySQL/Redis 真实连接集成测试。
7. 独立生产进程的部署文件、健康端口和旧 GM 联调录包。

这些不是隐藏欠账，而是下一阶段必须继续实现并在参考核对表中逐项关闭的范围。当前阶段的业务价值是先验证：`BattleID -> BattleRef -> Command -> Mailbox -> typed output` 这条新边界能够和旧 relay 共存。

## 测试与验收

在仓库根目录执行：

```bash
GOCACHE=/tmp/gsr-gocache go test ./...
GOCACHE=/tmp/gsr-gocache go vet ./...
GOCACHE=/tmp/gsr-gocache go test -race ./...
git diff --check
```

NHSK 相关测试重点覆盖：

- Legacy fixed-length golden、短包/长包、尾部非零、未知 MessageID。
- 外层/内层 UserID 冲突、零 BattleID、payload 深拷贝。
- Battle INIT/玩家/准备/开始/出牌/预览/托管和 Snapshot。
- Host 异步创建、Resolve、精确停止和编号释放。
- Legacy relay Send/Call 解析到 Host，再直达同一个 BattleRef。

完成一个新功能切片后，先核对只读参考目录，再更新 `docs/reviews/nhsk-reference-reconciliation.md`；如果出现未裁决偏差，应先写 RFC/决策再继续代码。

