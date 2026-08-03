# 宁海双扣 Legacy GM↔GameLogic 消息矩阵

> 状态：第二轮源码矩阵，2026-07-29
>
> 本文记录旧 GameMaster、旧 GameLogic、protocol 和 NHSK 当前可达消息面，不是
> 第二份公开契约。精确字段、字节序、长度和 suffix offset 最终由
> `RFC-0410` 与 golden bytes 冻结。

## 1. 分类规则

- `保留`：当前 GM 有发送点且 GL 有接收点，或当前 NHSK 有输出点且 GM 有接收点。
- `放弃`：已确认只是遗留、弃用或未接入能力；不写 codec、handler 或预留接口。
- Legacy 输入是异步消息，进入 GSR 后使用 `Send` 语义。旧协议存在 ACK 时，由对应
  生命周期或业务结果异步输出，不把 adapter 改造成同步 RPC。
- Cluster 调用者可以对同一类型化玩法 Command 使用 `Send` 或 `Call`；两者进入同一
  Battle handler。`Call` Reply 不是客户端输出，也不改变 Legacy wire。

## 2. 外层 Header

| 路径 | 外层形状 | 说明 |
|---|---|---|
| GM → GL 普通消息 | `TGM2GLHeader + body + suffix` | `TGM2GLHeader` 内含 `BSHeader`、`GameInnerId`、`UserId` |
| GL → GM 普通消息 | `TGM2GLHeader + body + suffix` | 复用同一 Header 结构，方向由 MessageID 区分 |
| `NEW_GAME` | `BSHeader + fixed body` | 尚未建立 Battle，不能要求 `GameInnerId` 已进入 GLHeader |
| `DEL_GAME` | `BSHeader + GameInnerID` | protocol 明确定义为无 GLHeader 的窄生命周期包 |
| 普通客户端透传 | GM 的 `0x8605` 外壳包裹完整内层 BS packet | adapter 校验后转成类型化 Command |
| 旧客户端透传 | `BS_MSG_GAME_BASE_RELAY` / `BS_MSG_GC2GS_RELAY` | 原始嵌套只留在 Legacy adapter |

已知 GM→GL 基址为 `0x8600`，GL→GM 基址为 `0x8640`。带 `BSAck` 的
MessageID 在 golden test 中保留协议库表达式，不自行猜测或重定义 ACK 位。

### 2.1 `BSHeader` 与 origin

`baison_middle/protocol v1.0.5` 使用小端编码，Header 固定为：

| Offset | 字段 | 类型 |
|---:|---|---|
| 0 | Magic | `uint32` |
| 4 | Serial | `uint32` |
| 8 | Origine | `uint16` |
| 10 | Reserve | `uint16` |
| 12 | Type | `uint32` |
| 16 | Param | `uint32` |
| 20 | Length | `uint32` |

GameLogic origin 的 24 字节 golden hex 为
`00000000000000006b000000000600000000000018000000`；GameMaster origin 只把
`Origine` 改为 100，对应
`000000000000000064000000000600000000000018000000`。两者的 Type 都是
`0x600`。

### 2.2 普通 relay 的嵌套边界

`0x8605` 和 `0x8644` 的固定外层均为 34 字节：

| 外层 offset | 内容 | 大小 |
|---:|---|---:|
| 0 | 外层 `BSHeader` | 24 |
| 24 | `HeaderLen=34` | 2 |
| 26 | `GameInnerId` | 4 |
| 30 | 外层 `UserId` | 4 |
| 34 | 内层完整 BS packet | `56 + N` |

内层普通 relay 固定区为 56 字节：

| 内层 offset | 内容 | 大小 |
|---:|---|---:|
| 0 | 内层 `BSHeader`，Type=`0x7402` | 24 |
| 24 | `TGameHeader`：六个 `uint32` | 24 |
| 48 | `BSSUFFIXIDX{Offset:56, Size:N}` | 8 |
| 56 | NHSK suffix | `N` |

因此外层 `Length=90+N`，内层 `Length=56+N`，内层 `SuffixOffSet=56`。
Suffix offset 相对内层 relay 起点，不包含前面的 34 字节 GLHeader。旧输出
`0x8655` 使用相同尺寸，只把内层 Type 换为 `0x7200`。参考 formatter 先产生
`Length=0` 的外壳，再由最终 connection `PackDetect` 按完整发送缓冲区补写外层
Length；新 codec 直接一次性写出最终正确值，不复制这个中间态。

### 2.3 失败边界

- Header.Length 小于 24、超过 8192 或无法恢复帧边界：关闭当前连接代际。
- 边界完整但 MessageID 未注册：告警并丢弃当前帧，连接继续。
- 已知消息的固定区、内层 Header 或 suffix 非法：告警、计量并丢弃当前帧，不进入
  Battle、不回复，连接继续。
- Suffix 必须满足 `offset >= 当前消息固定区长度` 且
  `offset + size <= 当前消息 Length`。

### 2.4 身份归一化

旧链路重复携带身份，但新业务只保留一份：

| Legacy 字段 | adapter 语义 | 进入 Battle |
|---|---|---|
| 外层 `GameInnerId` | BattleID、Host 路由键 | `BattleID` |
| 外层 `UserId` | 玩家动作身份 | `UserID` |
| 内层 `TGameHeader.UserID` | 玩家帧必须与外层一致 | 不保留 |
| 内层 `MatchID`、`Reserved1(ProductID)` | Battle 初始化后必须与本局一致 | 不保留 |
| Magic、Reserve、Serial、Param | 仅按逐消息 wire 契约解析或编码 | 不保留 |

身份不一致按完整非法帧丢弃。`UserId=0` 只允许参考代码明确存在的控制或广播路径；
玩家动作不能使用零身份。该检查只做一次，Battle handler 不再重复维护或比较旧
Agent、GM、GL 的三层路由字段。

## 3. GM → GL

| MessageID | 参考类型 | 当前发送证据 | GSR Command | Legacy 结果 | 裁决 |
|---|---|---|---|---|---|
| `0x86C1` `NEW_GAME` | `ReqGM2GLNewGame` | `Round.RoundStart -> PushCreateNewGame` | Host 先校验 GameID=82，再 CreateLegacyBattle；IsNewNacos 只解码丢弃 | `NEW_GAME` ACK，`Res=1/0` | 保留；D-076、D-078 |
| `0x8600` `INIT_GAME` | `ReqGM2GLBodyInitGame` | `Round.Send2GLInitGame` | `InitializeBattle` | 无同步 Reply | 保留 |
| `0x8601` `UPDATE_PLAYER` | `ReqGM2GLBodyUpdatePlayer` | `Game.UpdateGLPlayers` | `UpdatePlayers` | 无同步 Reply | 保留 |
| `0x8602` `COMMAND` | `ReqGM2GLCommand` | GM 发送 START、CONTINUE、MATCH_STOP；没有 PAUSE 发送点 | `ControlBattle` | GameStarted、GameOver 或 RoundOver 等异步输出 | 保留三个实际命令；PAUSE 放弃 |
| `0x8603` `CHANGE_SEAT` | `ReqGM2GLBodyChangeSeat` | GL controller 注册，但当前 GM 没有构造或发送点 | 无 | 无 | 放弃 |
| `0x8604` `UPDATE_GAME` | `ReqBSGM2GLBodyUpdateGame` | `Game.PreStartGame -> PushGameSence` | `UpdateGameProgress` | 无同步 Reply | 保留 |
| `0x8605` `GAME_MSG` | `ReqGM2GLGameMsg` | `Game.SendMsgToGL`；承载客户端玩法、离线、重连、场景和道具 | 按内层 MessageID 转具体 Battle Command | 玩法单播/广播异步输出 | 保留 |
| `0x8606` `PLAYER_EXIT_GAME` | `ReqBSGM2GLPlayerExitGame` | `Game.PlayerExitGame` | `ExitPlayer` | 无同步 Reply | 保留 |
| `0x8609` `CHANGE_ROUND` | `ReqGM2GLChangeRound` | 当前 GM 没有调用点；旧 GL ACK 方法也是空实现 | 无 | 无 | 放弃 |
| `0x860C` `TAKE_POINTS` | `ReqGM2GLTakePoints` | 当前 GM 没有调用点 | 无 | 无 | 放弃 |
| `0x860D` `START_NEW_GAME` | `ReqBSGM2GLStartNewGame` | 已在 playing Round 再次收到创建游戏时发送 | `UpdateRoundContext`，只更新时间与 RoomInfo | 无同步 Reply | 保留；不是小局启动门禁 |
| `0x8610` `DRESS` | `ReqGM2GLDress` | `Game.PreStartGame -> lockDressInfo`，位于 START 前 | `UpdatePlayerDress` | 无同步 Reply | 保留；只影响回放元数据 |
| `0x86C2` `DEL_GAME` | `ReqGM2GLDelGame` | `Round.CleanRound -> PushDelGame` | `StopLegacyBattle` | 无同步 Reply | 保留；Quarantined 按 D-038 |
| `BSAck \| 0x8650` 综合结算响应 | `ReqGM2GLGameResultComprehensive` | 当前 GM 处理 `0x8650` 后 `PushGameResult` | `CompleteSettlement` | GameOver/RoundOver | 保留 |
| 三套旧结算 ACK | 三个旧 ACK 类型 | 当前 GM 没有对应请求入口或发送点 | 无 | 无 | 放弃 |
| `DEL_ONE_GAME`、金额限制、预扣/退款、ReplayMove | 通用遗留类型 | 当前 controller/GM/NHSK 无完整可达链 | 无 | 无 | 放弃 |

当前 GM 的 `SendPlayerLimit` 虽有 kick 调用点，却把 PLAYER_LIMIT 控制包再次封入
`0x8605`；旧 GL 的 GAME_MSG switch 不接收，controller 也未注册，故仍按 D-069
归入“无完整可达链”，新实现不修通。

## 4. GL → GM

| MessageID | 参考类型 | 当前输出证据 | GSR Output | 裁决 |
|---|---|---|---|---|
| `BS_MSG_GM2GL + 0xC0 + BSAck` `NEW_GAME` ACK | `ReqGL2GMNewGame` | 创建成功/失败反馈，GM 以此继续 Round 初始化 | `LegacyCreateBattleResult` | 保留 |
| `0x8641` `GAME_OVER` | `ReqBSGL2GMGameOver` | `BaseGame.GameOverProcess` | `GameOverOutput` | 保留 |
| `0x8644` `GAME_MSG` | `ReqGL2GMRelayMessage`，内层 `0x7400` | NHSK `SendMsgToAll/SendMsgToUser` 及 BaseGame `SendMsgPlayerRoundStat` 经 GM relay 客户端；广播逐玩家展开 | `ClientGameOutput{Targets, Payload}` | 保留；双层定向 UserID，CntTID/CltTID=0；含 `0x7246 ROUND_STAT` |
| `0x864E` `NOTICE_ROUND_OVER` | `ReqBSGL2GMNoticeRoundOver` | `BaseGame.GameOverProcess` 仅在 force-round-over 时发送；正常结束由 GM 从 GAME_OVER 自行判断 | `RoundOverOutput` | 只保留 MATCH_STOP 强制路径；D-070 |
| `0x8650` `GAME_RESULT_COMPREHENSIVE` | `ReqGL2GMGameResultComprehensive` | NHSK `DoProcessResult -> SendMsgAskResScore` | `SettlementRequestOutput` | 保留 |
| `0x8654` `GAME_STARTED` | `ReqBSGL2GMStarted` | `BaseGame.startGame` 总会发送并携回放名 | `GameStartedOutput` | 保留 |
| `0x8655` `GAME_MSG_OLD` | `ReqGL2GMGameMsgOld` | NHSK 没有任何 `SendOldMsg*` 调用点 | 无 | 放弃；D-068 |
| `0x8647` `PLAYER_OUT` | `ReqGL2GMGamePlayerOut` | BaseGame 有 helper，NHSK 没有调用点 | 无 | 放弃 |
| `0x8651` `GAME_CHANGE_POS` | `ReqGL2GMGameChangePos` | 只在 GL 骰子定座路径调用；无目标 BaseRule 或录包证据 | 无 | 放弃 |
| `0x8652` `WATCH_MSG` | 遗留 watch message | protocol 明确标注弃用；NHSK 实际推送调用被注释 | 无 | 放弃 |
| 其余 GL2GM 结算、分组、缓存、触发命令 | 通用遗留类型 | 当前 NHSK 无调用点 | 无 | 放弃 |

## 5. `0x8605 GAME_MSG` 内层 NHSK 消息

用户已确认 `BS_MSG_GAME = 0x7600`，NHSK 保留原 MessageID：

| MessageID | 方向 | 类型化 Command / Output | Legacy 语义 |
|---|---|---|---|
| `0x7701` `NHSK_OUT_CARD` | Client → Battle | `PlayCards` | Legacy 映射为 Send；Cluster 可 Send/Call 同一 handler |
| `0x7702` `NHSK_CARD_ACTION` | Client → Battle | `PreviewCardSelection` | 同上；宽松预览，不代替 OUT_CARD 校验 |
| `0x7601` `GAME_INFO` | Battle → Client | `GameInfoOutput` | 单播/广播目标按参考路径 |
| `0x7602` `DEAL` | Battle → Client | `DealOutput` | 每位玩家只收到自己的手牌 |
| `0x7603` `ASK_OUT_CARD` | Battle → Client | `AskOutCardOutput` | 携带当前 VerifyCode/期限 |
| `0x7604` `OUT_CARD_INFO` | Battle → Client | `OutCardInfoOutput` | 广播 |
| `0x7605` `TURN_END` | Battle → Client | `TurnEndOutput` | 广播 |
| `0x7606` `SHOW_CARDS` | Battle → Client | `ShowCardsOutput` | 目标按参考实现 |
| `0x7607` `GAME_RESULT` | Battle → Client | `GameResultOutput` | 综合结算完成后输出 |
| `0x7608` `GAME_SCENE` | Battle → Client | `GameSceneOutput` | 重连和主动场景请求共享构造，不共享副作用 |
| `0x7609` `OUT_CARD_RESULT` | Battle → Client | `OutCardResultOutput` | 当前动作结果 |
| `0x7610` `COMMENTATE_TIME` | Battle → Client | `CommentateTimeOutput` | 生产 `match_live_mode=0`，当前放弃 |
| `0x7611` `CARD_ACTION_WATCH` | Battle → Client | `CardSelectionPreviewOutput` | 当前操作玩家发送 CARD_ACTION 后按原 payload 全桌广播 |
| `0x7612` AI scene | Battle → 外部 AI adapter | `AIRequest` | 生产 RobotTran 路径保留 |

参考代码还用同一个 `0x7612` 定义 Robot relay 输出，但该方法只有直接测试、没有
生产调用点，因此 Robot relay 语义放弃，不在新输出类型中复用这个 ID。

通用游戏内层 allowlist 包括离线、重连、场景请求、用户状态变化、道具和
`GC2GS_RELAY`。后者只允许表中两个 NHSK 输入与用户状态变化。它们不在业务层保留
原始 Header；Legacy codec 解包后转换为各自类型化 Command。旧
`protocol.BS_MSG_GAME_BASE (0x7200)` 直传在当前 NHSK 实际落入未知消息，按
D-068 放弃。

`0x8644 GAME_MSG` 的客户端输出由 Legacy adapter 按 SeatID 顺序展开为每目标
一个 `0x8644 GLHeader + 0x7400 GameHeader + payload`；外层和内层 UserID 相同，
GameInnerID、MatchID、ProductID 有值，CntTID、CltTID、Reserved2 为零。当前 GL
不使用 GM 支持的 UserID=0 广播；`0x7402` 只属于反方向输入 relay。
客户端输出还包括旧 BaseGame 在每小局真实发送的通用
`0x7205 GAME_START`。它没有 body，必须位于 `0x8654 GAME_STARTED` 之前；
后者固定 `Res=true` 并携带已经确定的 ReplayName，随后才产生 NHSK
`GameInfo -> Deal -> AskOutCard`。`0x7206 GAME_END` 只有 formatter 而无调用点，
不进入输出 allowlist。小局回放收敛后，先向每名未退出且 ClientReady 的玩家定向
relay 相同的 `0x7246 ROUND_STAT`，再发送普通 `0x8641 GAME_OVER`。首版已放弃
BaseRule index 5 战绩模块，ROUND_STAT 因此保持 PlayerCount=0，不用回放 Summary
统计补值。GAME_OVER 的 `IsGameOver` 保持为 0，PlayerCount=4，PlayerDatas
按 SeatID 0..3 编码 Score、Exp、Auto；旧 GM 按数组下标关联 SeatList，再结合
自己的小局和大局计数判断续局或 RoundOver。`MATCH_STOP` 在 Playing 或
AwaitingSettlement 时废止原流程，以 Success(0) 本地结算并跳过 `0x8650`；
独立完成时线序为 ROUND_STAT→GAME_OVER→NOTICE_ROUND_OVER。若旧 GM 紧接着发送
DEL_GAME，停止屏障不等待尚未完成的回放，也不补发被 fence 的这些输出。

## 6. 下一步 golden 范围

只为“保留”项写 golden：

1. 双向 origin。
2. `NEW_GAME` 请求和 ACK。
3. `INIT_GAME` 多 suffix offset。
4. `UPDATE_PLAYER` 数组 suffix。
5. `0x8605` 普通 GameMsg 与内层 `0x7402` relay。
6. `0x8644` 客户端输出。
7. `0x8650` 综合结算请求与 `BSAck | 0x8650` 响应。
8. `0x7205 GAME_START -> GAME_STARTED -> NHSK` 的开始线序。
9. 普通 `ROUND_STAT -> GAME_OVER`、独立完成的
   `MATCH_STOP -> ROUND_STAT -> GAME_OVER -> NOTICE_ROUND_OVER`，以及
   `MATCH_STOP -> DEL_GAME` fence 未完成回放的路径。
10. `DEL_GAME`。

放弃项不写 golden，也不进入 codec switch。
