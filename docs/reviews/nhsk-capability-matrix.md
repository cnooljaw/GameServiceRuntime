# 宁海双扣 GameLogic 替换能力证据矩阵

> 状态：迁移范围已冻结并完成仓库内实现，2026-08-20
>
> 本文只记录参考证据和迁移范围，不是第二份公开契约。发生冲突时，以
> `docs/rfcs/` 为准。具体切片实现后的逐项一致性检查记录在
> `nhsk-reference-reconciliation.md`；Legacy 消息面见
> `nhsk-legacy-message-matrix.md`。

## 1. 目的

第一阶段只替换旧 GameLogic，旧 GameMaster、Agent 和客户端保持不变。
“标准四人牌局可以运行”只是最小可玩里程碑，不等于可以切换生产流量。

本矩阵用以下证据决定一项能力是否进入切换门槛：

- 旧系统存在可达调用点，并有业务入口实际触发；
- 目标部署的配置实际启用该能力；
- 现网录制消息或 golden bytes 证明该路径出现过；
- 用户明确要求新系统保留该能力。

测试、配置字段、协议常量、接口、TODO、不可达分支或完整但未接入的实现，只能
帮助理解行为，不能单独证明生产使用。确认原系统未使用时直接标记为“放弃”，首版
不写占位、adapter、provider 或预留接口。

## 2. 状态定义

| 状态 | 含义 |
|---|---|
| 必须兼容 | 切换旧 GameLogic 前必须有实现、测试和参考核对记录 |
| 配置门控兼容 | 已确认目标部署使用，但该部署允许通过配置关闭 |
| 有意偏差 | 已由 RFC 和决策批准，不照搬参考实现 |
| 放弃 | 已确认原系统未使用；首版不实现任何相关代码 |
| 待录包确认 | 源码有可达路径，但还需要真实消息字节冻结 wire 契约 |

## 3. Legacy 容器与消息面

| 能力 | 参考所有者 | 证据 | 迁移裁决 |
|---|---|---|---|
| GameLogic 主动连接 GM、双向 origin、单连接全双工 | `gamelogic`、`gamemaster`、`nbgame_core` | `NewBSProtocol(BS_CONNECTIONTYPE_GAME_LOGIC)` 的 `ConnectedEvent`；GM 按 origin 创建 `GameController`；GM controller 和 GL pusher 共用连接 | 必须兼容；双向 origin 与代表性连接整包 golden 已完成 |
| 创建 Battle | 旧 `GameMasterController`、Round manager | controller 注册 `BS_MSG_GM2GL_NEW_GAME`；旧 GM 真实发送；`GameInnerId` 建立 Round 索引 | 必须兼容 |
| NEW_GAME IsNewNacos | GM、旧 GL | wire 有字段，但 GL handler 未传入 RoundInitConfig；Round 默认 false，BindNacosFile 为空 | 放弃；D-076 只解码保 wire |
| START_NEW_GAME 回放上下文 | GM、旧 BaseGame replay | 只在已存在 Round 再次 CreateGame 时发送；GL 保存 Total/Used/RoomInfo，下一次 replayAttributeInitialize 读取 | 必须兼容；D-077 作为 pending RoundContext，不作为启动门禁 |
| GameID 玩法选择 | 旧 GL plugin loader、NHSK 配置 | NEW_GAME GameID 选择插件；目标配置固定 82/宁海双扣；回放与 RobotTran 复用 | 必须兼容；D-078 Host 边界校验，Battle 不保存常量 |
| 初始化、更新玩家、开始、更新局数 | 旧容器 | controller 注册 `INIT_GAME`、`UPDATE_PLAYER`、`START_NEW_GAME`、`UPDATE_GAME`，handler 投递 Round；`BaseGame` 有实际状态写入 | 必须兼容 |
| COMMAND：开始、继续、比赛停止 | Round manager、当前 GM | 当前 GM 有 START、CONTINUE、MATCH_STOP 发送点 | 必须兼容 |
| COMMAND：暂停 | Round manager、NHSK | controller 能解析 PAUSE，但当前 GM 没有发送点，GM PauseGame 是 TODO，NHSK 方法未被旧 Round 正确接通 | 放弃；按 D-045 不实现 |
| 玩家退出、换座、服饰、带入分、换桌请求 | 旧 `BaseGame` | 玩家退出和 Dress 有当前 GM 发送点；换座、TakePoints 没有当前 GM 构造或发送点；ChangeRound ACK 是空实现 | 玩家退出、Dress 保留；换座、TakePoints、ChangeRound 放弃，不写空 handler |
| 删除 Battle | 旧 controller、Round manager | controller 注册 `BS_MSG_GM2GL_DEL_GAME`；旧 GM `PushDelGame` 可达 | 必须兼容；隔离 Battle 的处理按 D-038/D-040 有意偏差 |
| 普通 GameMsg、Base relay、GC2GS relay | 旧 controller、`BaseGame.onMsgGameMsg` | `BS_MSG_GM2GL_GAME_MSG`、`BS_MSG_GAME_BASE_RELAY` 均注册；内层 `BS_MSG_GC2GS_RELAY` 有真实解析 | 必须兼容；外围 Header 只留在 adapter |
| `DEL_ONE_GAME` 电视赛 | 旧 `BaseGame.OnMsg` | 分支明确注释“电视赛，二期做”；controller 未注册该 MessageID | 放弃；不写任何相关代码 |
| `REPLAY_MOVE`、金额限制、预扣/退款等协议定义 | 通用 protocol | 当前 GL controller、NHSK 和当前 GM 没有对应可达生产分支 | 放弃；不写任何相关代码 |

## 4. NHSK 玩法与外围能力

| 能力 | 参考所有者 | 使用证据 | 测试证据 | 迁移裁决 |
|---|---|---|---|---|
| 四人发牌、出牌、跟牌、过牌、排名、双扣结算 | NHSK | `DoGameStart`、`DoOutCard`、`CalcSuccessResult`、`DoProcessResult` | `TestDoGameStartDealsAndBroadcastsGameInfo`、`TestDoOutCardRemovesCardsAndBroadcasts`、`TestXuRuoxuanSettlementCases` 等 | 必须兼容 |
| VerifyCode、回合状态和迟到动作拒绝 | NHSK | 出牌和 AI 回包都校验 VerifyCode/当前座位 | `TestDoOutCardRejectsStaleVerifyCode`、`TestAIMessageWithIgnoredVerifyCodeDoesNotPlay` | 必须兼容；新实现同时使用 TurnRevision |
| 首次/普通出牌、托管、机器人、AI Timer | NHSK | `DoAskOutCard` 会按玩家类型启动普通与专用 Timer | 多个首次、普通、Robot、AI timeout 测试 | 有意偏差 D-026：每个 TurnRevision 只有一个 `ActionDeadline` |
| 暂停玩法 | 旧 Round、旧 BaseGame、NHSK | 只有 controller 分支和 NHSK 直接测试，没有当前 GM 发送点或正确端到端接线 | `TestPauseRejectsOutCardAndContinueAllowsIt` 只覆盖 NHSK 内部入口 | 放弃；不实现暂停状态或 deadline 恢复 |
| 用户离线 | BaseGame、NHSK | `GAME_MSG -> USER_OFFLINE`；NHSK 写玩家 Offline；约局模式另启动离线期限 | 游戏状态路径可达 | 必须兼容 |
| 用户重连 | BaseGame、NHSK | `USER_RECONNECT` 清 Offline；仅 playing 时取消自动并恢复视图 | `TestUserReconnectCancelsAutoStateDuringRound` | 必须兼容，遵循 D-043 |
| 主动请求场景 | BaseGame、NHSK | `GAME_SCENE` 要求有效 game/subgame，不清 Offline，取消自动并恢复视图 | `TestUserGameSceneCancelsAutoStateDuringStartedGame`、场景补发测试 | 必须兼容，不能与重连合并语义 |
| 用户托管/状态变化 | NHSK | `OnMsgUserStateChange` 修改 Auto/Offline，活动座位开启 Auto 时立即代打 | `TestUserStateChangeTogglesAutoAndBroadcastsRobotState`、`TestUserStateChangeActiveSeatAutoForcesOutCard` | 必须兼容 |
| 托管结算认定 | NHSK | GameRule 前两项、每次合法动作计数、`isPlayerAutoInSubGame/applyAutoSettlementCorrection` | 自动计数和 `TestCalcSuccessResultAdjustsScoresForAutoPlayerInSubgame` | 必须兼容；D-065 保留旧乘法公式和单人托管负分修正，不解释为百分比 |
| 超时惩罚 | NHSK | 取决于 BaseRule index 52 和动态规则；仓库没有目标规则样本 | `TestParseNacosRuleLoadsDefaultPersonalAndPunishment`、`TestPunishShortensOutCardTimeAndAskNotifiesPlayer` | 放弃；测试不能证明生产使用 |
| 外部 AI HTTP | 旧 BaseGame、NHSK | 生产 `robot_level=2`、`ms_out_card_robot=1000`、`ms_ai_timeout=6000`，并配置 `mid-robottran-svc`；离线托管结果有最小延迟，真实机器人立即回投 | `TestDoAskAIOutCardSendsSceneToRobot`、`TestOnAIMsgParsesRobotranOutCardResponse` | 必须兼容；D-026/D-028/D-048 冻结单 Timer、请求身份和 RobotTran wire |
| 示例本地 AI | 新 GSR 组合根 | 参考实现没有本地 fallback；为了示例少依赖外部系统 | NHSK 已有合法出牌选择和回包复核测试可复用 | 有意新增 D-028；默认本地 provider |
| Robot relay | NHSK | `OnMsgRobotRelay` 有实现和直接测试，但生产代码没有调用点 | `TestRobotRelayBroadcastsWrappedPayload` | 放弃；外部 AI 路径不依赖该方法 |
| 旁观场景 | NHSK、GameAPI | 生产 `match_live_mode=0`；实际出牌路径中的 `SendGameSceneToWatchSrv` 被注释 | `TestDoOutCardPushesWatchSceneWhenEnabled` | 放弃；不实现 WatchService 或输出 adapter |
| 战绩记录 | NHSK、旧 BaseGame | 取决于 BaseRule index 5；仓库没有目标规则样本 | `TestDoProcessResultStoresGameRecordsWhenEnabled` | 放弃；不建立战绩模块 |
| 回放生成 | 旧 BaseGame、NHSK | 三份配置 `replay_enabled=1`，且旧 BaseGame 创建/保存条件已注释，实际始终生成；每小局同步保存后结算 | 回放 package、parser、runner 以及线上 C++ sample 测试完整 | 必须兼容；D-047 保留 XML/名称/目录，D-086 删除假开关并始终提交有界 writer |
| 回放 GameRule 与玩家文本投影 | 旧 BaseGame、gamecore replay | GameRule 固定输出业务局数/时长和 BaseRule 投影；Players 写昵称/CltID/初始分，非法 UTF-8 昵称按 GBK 转码；Go map 使节点顺序随机 | replay parser/converter 覆盖固定属性；RecordGameStart 明确为空 | D-079 只保留 ReplayRuleSnapshot，不恢复已放弃玩法；Players/Dress 改按座位稳定输出；隔离使用 x/text 转昵称 |
| 回放终局、Summary 与 CardDetail | NHSK、gamecore replay | RecordGameOver 只写根节点；四座结果、动作/托管/耗时、牌型、本局统计和炸弹明细均由真实结算路径生成；结束和动作耗时直接读系统时间 | 当前测试断言无 GameOver Move；runner 旧 fixture 仍含旧 Move 形状 | 必须兼容 D-080；改用 Battle Clock，保留当前根节点/统计树，放弃跨局战绩累计，不复制过时 fixture |
| 回放中间 Moves 与 XML writer | NHSK replay、gamecore XMLNode | Deal/CurrentPoint/OutCard/CatchPoint/TurnEnd 有真实调用；CurrentPoint 先于 OutCard；PickCard/Offline/Reconnect 无调用；属性排序与小写十六进制显式实现 | move shape 与 runner 测试覆盖 | D-081 精确保留调用线序和 Actor，删除无调用入口，byte golden 固定序列化 |
| 回放树冻结与 writer 边界 | gamecore Init/Save、旧 BaseGame GameOver | Init 决定基础节点顺序，Dress/Other 后追加，Save 编码前补 Count 并同步写盘 | 无并发冻结测试 | D-082 在 Battle 内冻结不可变 ReplayDocument，固定完整树序；序列化失败复用 D-047，runner 只做 I/O |
| 回放文本与 Prop 编码 | gamecore text helper、GL addMoveProp、protocol suffix | MatchName/UserName 单独做 UTF-8/GBK；RoomInfo/Dress/PropID 原样 AddAttr；Prop 无 Actor且目标逗号连接 | 零散 formatter/replay 测试 | D-083 按字段来源编码，标准 XML 转义；不透明原文不入日志 |
| 回放玩家/发牌冻结 | GL replayAttributeInitialize、GM 结算 UPDATE_PLAYER、NHSK Deal | START 前复制玩家字段；结算 ACK 前 GM 更新 Player.Score；Deal 使用最终手牌；GM Flag 与 GL PlayerFlag 错位 | 无跨阶段快照测试 | D-084 分离当前 InitScore、ACK TotalScore、下一局 InitScore；D-087 不迁移失效 Flag 中间状态，破产/封顶取结算 ACK；Deal 与私有输出复用同一牌序 |
| 综合结算响应完整性 | GL `onMsgGameResultComprehensive`、GM `BuildAndSendResultInfo` | 正常 GM 回送四名玩家且 TeamID=SeatID；旧 GL 对未知/缺失项跳过、重复 Team/交易覆盖后仍结算；PlayerData.Score 未读，Exp/ResultType 只进日志；失败响应触发 Dissolve 平局零分 | 无坏响应整包/字段 owner/失败 golden | D-088 成功响应完整校验后原子提交，坏内容保持等待；D-089 只消费身份、Flag 与交易矩阵；D-090 保持失败外观并单独诊断 |
| 综合结算双入口 | Legacy `BSAck|0x8650`、GSR Send/Call、Battle staged output | 旧 ACK 是异步完成；Cluster Command 可由 Send/Call 共用 handler；业务输出不属于 Reply | 无端到端双入口测试 | D-091 统一 CompleteSettlement；Legacy 用 Send，Cluster Call 只回应用结果；请求事实不绑 Session |
| 删除与回放竞态 | GM `CleanRound`、GL MATCH_STOP/DEL_GAME、Replay writer | 外部删除连续发送 MATCH_STOP/DEL_GAME，不等待强制结果或回放；旧 TurnEngine/ClearRound 有竞态 | 无屏障/writer 顺序测试 | D-092 用 Battle Mailbox 屏障确定化；已提交不撤回，已启动 I/O 可留孤立文件，Stop 完成后才复用编号 |
| ReplayName/ReplayUID/FuPanUID | GL createReplayName/GetUnicode、NHSK CalcSuccessResult | 文件名用 Product/Round/时间/Seat0；UID 用时间+Creator 并复制到客户端固定数组；0x8650 无该字段 | 名称与 ResultDetail 测试分散 | D-085 两个不可变标识分工；NHSK 私有前缀，不增加碰撞处理 |
| 玩家装扮回放元数据 | GM、旧 BaseGame | GM 每小局 START 前发送 DRESS；GL 只保存并由 `replayAttributeInitialize/AddDress` 读取，NHSK 不消费 | 线上回放包含 Dress 节点，转换测试保留四项 | 必须兼容；D-066 只保留有序不透明元数据，不进入玩法或通用模板 |
| 回放上传 | 旧 BaseGame HTTP adapter | 取决于 BaseRule index 24；只有 URL 和实现，没有目标规则样本 | 无容器端到端测试 | 放弃；只保留本地回放生成 |
| 偏置洗牌 | NHSK | 取决于 GameRule 第 3 项与动态规则；仓库没有目标规则样本 | `TestSetGameRuleLoadsRobotThresholdsAndBiasedFlags`、Nacos 解析测试 | 放弃；只实现普通随机和已确认的新手路径 |
| 新手发牌 | NHSK | `NEW_GAME.IsNewbie -> dealCardsFromRules -> RandCardListByNewPlayer` 可达；选择首个非机器人，helper 实际可能交换四家牌 | `TestRandCardListByNewPlayerImprovesTargetSingles` 等 | 必须兼容；D-063 保留最终调整结果和优先级，但不恢复 Nacos/通用偏洗牌 |
| 普通发牌散牌调整 | NHSK | 默认 `SingleCountToSwap=4`，普通 `dealCardsFromRules` 无条件调用 `SwapSingleCard` | `TestSwapSingleCardReducesExcessiveSingles`、GameRule 测试 | 必须兼容；D-064 保留顺序与最终牌序，不把阈值误作全局后置条件 |
| 自定义牌堆与指定庄家 | NHSK、GameAPI | 生产 `custom_deck.enabled=1` 且配置账号白名单；旧 `MakecardConfig` 先读 ProductID、空值回退 GameID，`SetGame/DoGameStart` 每小局相邻重复读取，parser 测试接受 `0x01..0x68` 任意字节 | parse、deal、enabled/disabled、allowed account 测试 | 必须兼容；D-046 合并为每小局一次外部装载，但保留任意 `uint8` 调试牌堆 grammar |
| 固定六张牌 TestMode | NHSK | pro、test、默认配置均为 0，无 GM/wire 入口；开启后每座仅 6 张固定牌 | 只有直接单元测试 | 放弃；D-072 由固定 seed 和测试注入 CustomDeckProvider 覆盖 |
| Config owner 与死字段 | NHSK Config、组合根 | MsDeal/MsContinueDelay/TableMultiplier 只加载不消费；MsShowCard/MsCommentate/TestMode 已关闭或放弃；URL、目录、自定义牌堆源属于外围 I/O | 配置加载测试不能证明玩法使用 | D-073 删除死字段并把 adapter 配置归还 provider/runner |
| INIT 字段 owner | protocol、旧 Round、BaseGame replay | 旧 Round 只消费 MatchKey 子集；CreateTime 未下传；玩法、客户端 GameInfo 和回放各读取不同字段 | INIT formatter、replayAttributeInitialize | D-074 完整解码后按单一身份、玩法/计分和回放独有元数据归一化 |
| 回放时间与 UniCode | 旧 BaseGame | PreStartGame 用 Unix 秒+CreatorID；createReplayName 连续三次读取 time.Now | 无边界测试 | D-075 单次 Clock 快照，保留格式并有意消除跨时刻不一致 |
| 道具 | GM、旧 BaseGame | GM/CMT 判定成功并广播；BaseGame 只把相同包写入 replay，未调用 NHSK 的空 `OnMsgUseProp` | 没有 NHSK 道具效果测试 | 必须兼容回放留痕；D-067 删除死 seam，不创造玩法或通用道具服务 |
| GAME_MSG 内层路由 | GM、旧 BaseGame、NHSK | 正常 0x7402 relay 承载三个玩法输入；离线/重连/场景/道具有专用分支；0x7200 直传在 NHSK 实际落入未知消息 | NHSK 三个 OnMsg 分支有测试 | 必须兼容 allowlist；D-068 放弃 0x7200/0x8655 假兼容和未知透传 |
| PLAYER_LIMIT / UpdatePlayerInfo | GM、旧 BaseGame、NHSK | PLAYER_LIMIT 被错误包入 GAME_MSG 且接收端无分支；UpdatePlayerInfo 仅有 NHSK 空方法、无容器调用 | 无端到端效果测试 | 放弃；D-069 保持 no-op，不猜测修通 |
| 通用小局开始 | 旧 BaseGame、GM | `BaseGame.startGame` 每局先广播无 body `0x7205 GAME_START`，再发送 `GAME_STARTED`，然后调用 NHSK StartGame | 无独立容器测试；调用链稳定可达 | 必须兼容；D-070 补齐此前只盘点 NHSK 消息造成的遗漏 |
| 通用小局结束 | 旧 BaseGame | `SendMsgGameEnd` 和 `0x7206` formatter 存在，但没有调用点 | 无 | 放弃；D-070 不从死 helper 猜测客户端契约 |
| GM/GL 生命周期结果 | 旧 BaseGame、GM | GAME_STARTED 固定成功并携 ReplayName；回放后先向 ready 玩家 relay ROUND_STAT，再以四个座位索引型 PlayerDatas 发送 GAME_OVER；GM 自己判断续局/RoundOver，NOTICE 只由 GL 强制 RoundOver 分支发送 | GM `OnStartGame/OnGameOver/checkResultAndRound` | 必须兼容；D-070/D-093 保持 owner 与线序，ROUND_STAT 保留空包但不恢复已放弃的战绩累计 |
| ReplayName 累计 | 旧 BaseGame、GM | `replayNames` 只有追加无读取；每次 GAME_OVER 使用当前名，GM 仅在真实大局结束时上报当次名；overType 参数未消费 | 无累计行为测试或消费者 | D-094 删除死列表；保留当前名和 Legacy Reason wire，不建立额外状态 |
| 客户端输出资格 | 旧 BaseGame、NHSK | GAME_START 与 NHSK wrapper 均 force=true，忽略 ClientReady 但过滤 Exited；ROUND_STAT 单独要求 ready | 无统一资格测试 | D-095 统一玩法资格并保留 ROUND_STAT 例外；START 对 Exited 四座做可修正拒绝 |
| 客户端输出 Legacy 封包 | GL、protocol、GM | 业务广播由 GL 逐玩家发送；wire 为 `0x8644 + 0x7400` 双层定向头，Cnt/Clt 未写，GM 按用户代理地址转发 | 无逐字段 golden | D-096 adapter 按座位稳定展开；CntID 丢弃、CltID 仅回放，不复制无生产者的 UserID=0 分支 |
| MATCH_STOP 强制收尾 | GM、旧 BaseGame、NHSK | GM Clean 先发 MATCH_STOP 再 DEL_GAME；Playing 时 ForceGameOver 清队列、跳过综合结算并本地出结果，Idle 时非约局 no-op | ForceGameOver 测试确认无 ResScore 请求 | 必须兼容业务流向；D-071 用显式阶段替代旧 TurnEngine/删除竞态 |
| 投票解散 | 旧 BaseGame | generic relay 可以承载 `GAME_VOTE_CANCEL`，BaseGame 有完整实现；但当前 GM 无专用调用点、无目标配置或录包 | 旧容器无自动测试 | 放弃；不实现投票状态机 |
| 约局 YueJu | 旧 BaseGame、BaseRule | 生产 `yueju_mode=0`；虽有离线超时、行动提示和 `StopGameWithIdle` 实现，但目标配置关闭 | NHSK 有 move timer start/stop 测试，旧容器无端到端测试 | 放弃；不实现约局专用状态机 |
| 骰子定座与换座回报 | 旧 BaseGame、旧 GM | 取决于 BaseRule 随机座位模式；仓库没有目标规则样本 | 旧容器无测试 | 放弃；不实现 GL 定座流程 |
| `TimerGameOver` | NHSK 枚举 | 只有枚举和 `StopAllTimers`，无启动点、无 handler | 无 | 放弃；按 D-025 不实现 |

## 5. 结算路径裁决

### 5.1 当前 NHSK 真实路径

```text
NHSK.DoProcessResult
  -> GameAPI.SendMsgAskResScore
  -> BaseGame.SendMsgAskResScore
  -> GameMasterService.PushGameResult
  -> BS_MSG_GL2GM_GAME_RESULT_COMPREHENSIVE
  -> GameMaster 计算
  -> ReqGM2GLGameResultComprehensive
  -> BaseGame.onMsgGameResultComprehensive
  -> NHSK.CalResScore
  -> GameOver / replay / records
```

这条链在 NHSK、旧 GameLogic 和当前 GameMaster 三个工程中都存在真实调用点；
当前 GM 的 `gameDispatcher` 也只注册综合结算请求。

裁决：综合结算请求/响应是第一阶段必须兼容的唯一 NHSK 结算 wire 路径。

### 5.2 通用容器遗留路径

旧 GameLogic 还注册以下输入：

- `BS_MSG_ACK_GM2GL_GAME_RESULT`
- `BS_MSG_ACK_GM2GL_GAME_RESULT_BY_GROUP`
- `BS_MSG_ACK_GM2GL_GAME_RESULT_BY_RATIO`

这些 handler 有完整计算代码，但当前 `BaseGame.SendMsgAskResScore` 只发综合结算，
当前 GM 也没有对应旧请求入口或发送点。它们属于通用容器遗留兼容面，不是当前
NHSK 的可达结算链。

裁决：放弃三条旧 ACK，不写 handler、codec 或预留接口。以后产生真实需求时，
再通过新 RFC 和纵向切片加入。

## 6. 已确认的有意差异

| 参考行为 | GSR 行为 | 依据 |
|---|---|---|
| 同一出牌机会同时存在普通 Timer 和 Robot/AI Timer | 一个 TurnRevision 只有一个 ActionDeadline | D-026 |
| 未接入的暂停代码 | 不迁移暂停状态和 Timeline 分支 | D-045 |
| 外部 AI 的 HTTP、sleep 和回投散落在 BaseGame | Battle 只发类型化请求；有界 runner 执行；最小延迟通过替换唯一 ActionDeadline 保留 | D-026、D-028、D-048 |
| 异常 Battle 可能被 DEL_GAME、GM 断线或同号 NewGame 清掉 | Quarantined 保留现场，凭精确 receipt 人工局部释放 | D-038、D-040 |
| Legacy envelope 进入业务逻辑 | envelope 仅在 codec/adapter，业务只看类型化 Command | D-039 |
| 普通玩法 Call 被误解为可自动重试 | 不建通用幂等表；超时后先查询 Snapshot | D-042 |

## 7. 尚缺的证据

以下缺口阻止“可以无损替换旧 GameLogic”的结论，但不阻止先实现标准四人纵向切片：

1. 保留的 GM2GL/GL2GM 主链路 golden bytes，包括双向 origin、普通 relay、旧 relay
   和综合结算。已取得与旧系统依赖一致的 `baison_middle/protocol v1.0.5`：
   `BSHeader` 按小端顺序固定为 `Magic@0 u32`、`Serial@4 u32`、
   `Origine@8 u16`、`Reserve@10 u16`、`Type@12 u32`、`Param@16 u32`、
   `Length@20 u32`，总长 24 字节；`BSSUFFIXIDX` 固定为
   `SuffixOffSet@0 u32 + SuffixSize@4 u32`，偏移相对整条消息起点。
   `nbgame_core` 还确认 transport 按 Header.Length 拆帧、要求单个出站 frame 长度
   完全匹配并限制为 8 KiB。字段级契约已不再阻塞；仍需把 origin、普通 relay、
   旧 relay 和综合结算的代表性整包字节固化为 golden fixture。
2. 自定义牌堆、回放和 RobotTran 的源码契约已由 D-046 至 D-048 冻结；仍需把代表性
   旧产物整理成测试 fixture，验证 codec 和文件输出没有字节级偏差。

这些问题优先通过源码、配置样本和录包回答。已放弃能力不再作为证据缺口，也不阻止
切换。

## 8. 本轮验证

- `GameServiceRuntime`：`git diff --check` 通过。
- `GameServiceRuntime`：`go test ./runtime -run 'TestRFC'` 通过。
- 参考底座：`baison_middle/protocol` 位于 tag `v1.0.5`，包本身可编译；执行
  `go test ./...` 时 224 项序列化测试有 3 项既有失败（GBK `IsGbk` 与两个 ACK
  `Param` round-trip），与本次 Header/Relay 读取无关，未把全量测试误写为通过。
- 参考 NHSK：取得底座后应通过临时 `go.work` 组合本地只读 module 补跑
  `go test ./game ./replay/...`；不得为测试修改参考源码或提交 replace。
