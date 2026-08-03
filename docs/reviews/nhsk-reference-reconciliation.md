# 宁海双扣参考实现核对记录

> 本文是实现核对证据，不是公开契约，也不定义第二套需求。
>
> 公开行为以 `docs/rfcs/` 为准；迁移范围见 `nhsk-capability-matrix.md`，
> Legacy 消息面见 `nhsk-legacy-message-matrix.md`。每个 NHSK 功能切片完成后，
> 必须在这里追加核对结果。

## 1. 结论定义

| 结论 | 含义 |
|---|---|
| 已一致 | 新实现与参考的有效输入、状态变化、Timer、输出目标、顺序及生命周期一致 |
| 有意偏差 | RFC 已明确批准差异，并记录不照搬参考实现的原因 |
| 发现遗漏 | 新实现缺少迁移范围内的参考行为；当前切片不能完成 |
| 放弃 | 已确认原系统未使用；新实现不应出现相关代码 |
| 待实现 | 已进入迁移范围，但对应纵向切片尚未开始 |

“源码存在”不等于“需要迁移”。放弃项不要求做兼容实现，也不允许用空 handler、
空 adapter 或预留接口伪装完成。

## 2. 每个切片的核对步骤

1. 用 CodeGraph 定位参考入口、调用方和输出路径。
2. 阅读对应源码、测试、目标部署配置或录包证据。
3. 列出 MessageID、输入字段、权威状态变化、Timer、输出目标和生命周期结果。
4. 逐项标记 `已一致`、`有意偏差`、`发现遗漏` 或 `放弃`。
5. `有意偏差` 必须链接 RFC 和决策编号。
6. `发现遗漏` 必须先修订 RFC、失败测试和计划，再补实现。
7. 只为保留能力建立 golden；放弃能力不得进入 codec switch。

参考目录的业务源码、配置和资源保持只读；`.codegraph/` 分析元数据不属于业务修改。

## 3. 切换范围基线

以下是实现前的第一轮核对。`待实现` 不代表已有代码。

| 功能切片 | 参考入口/证据 | 核对项 | 当前结论 | RFC/决策 | 备注 |
|---|---|---|---|---|---|
| NHSK 公开 Command 与 owner | 既有 Legacy 消息矩阵、GL controller、GM Round/Game 调用链、NHSK 输入输出入口 | Host/Battle/玩法/结算分段；显式 MessageID 映射；Send/Call/Reply；类型化输出边界 | 待实现（契约已核对） | RFC-0410、D-097 | 没有发现迁移范围内的新入口；RoundContext/Dress/Offline/Prop 保持 Send-only，Host 异步结果不冒充 Battle 业务结果，废弃消息不分配 CommandID |
| GM 主连接、origin、身份与坏帧 | `NewBSProtocol`、GM `GameController`、双方 `ConnectedEvent`、`BSProtocol.UnpackDetect`、controller callbacks、GM `PushGameMsg` | 单连接全双工、双向 origin、嵌套精确长度、8 KiB 上限、外/内身份、未知 ID、坏 body/suffix | 待整包 golden | D-036、D-037、D-049、D-050 | 24 字节 Header 已冻结；冗余身份仅在 adapter 核对，不进入 Battle；完整坏帧局部丢弃 |
| Battle 创建与删除 | GM `RoundStart/CleanRound`、GL controller | `0x86C1`、ACK、`0x86C2`、GameInnerID | 待实现 | D-038、RFC-0410 | 隔离删除为有意偏差 |
| NEW_GAME IsNewNacos | protocol formatter、GL `ReqNewGame/NewRound/BindNacosFile` | wire 位存在但未进入 Round，绑定为空 | 放弃 | D-076 | codec golden 保留；领域与 Cluster 不出现 |
| START_NEW_GAME 回放上下文 | GM `PushStartNewGame`、GL `onMsgStartNewGame/replayAttributeInitialize` | Total/Used/RoomInfo 只供下一小局回放 | 待实现 | D-056、D-077 | pending context 在 StartSubgame 冻结；不计算 Used |
| GameID 玩法选择 | NEW_GAME、旧 GL loader、NHSK settings/replay/RobotTran | 82 选择宁海双扣；不是每桌变化身份 | 待实现 | D-078 | GameDescriptor 单一来源；未来其他玩法另 Host/Factory |
| 初始化与玩家更新 | GM `Send2GLInitGame`、`UpdateGLPlayers` | 多 suffix、座位、规则、玩家数据 | 待实现 | RFC-0500 | 只覆盖当前发送字段 |
| 玩法配置归一化 | NHSK `SetGameRule` | 前四项宽松解析、实际可达字段冻结 | 待实现 | D-044、D-052 | 偏置洗牌已放弃，不搬运历史空配置 |
| 随机与时间 | NHSK 包级 `math/rand`、直接时间读取 | Battle 独立 seed/PRNG、注入 Clock | 有意偏差 | D-051 | 消除跨 Battle 随机干扰，保留可复现事实 |
| 标准四人牌局 | NHSK `DoGameStart/DoOutCard/CalcSuccessResult` | 发牌、出牌、过牌、排名、得分 | 待实现 | D-044 | 第一条玩法纵向切片 |
| 核心牌规 | NHSK `logic.Logic`、`advanceTurnAfterOut/CalcSuccessResult` 及测试 | 104 张无王牌、五种牌型、墩轮转、固定对家、单双扣阈值 | 待实现 | D-058 | 双 Reset 标记为有意偏差，其余逐案例 golden |
| 操作与输出线序 | NHSK `DoAskOutCard/DoOutCard/messages.go/finishProcessResult` | VerifyCode、错误目标、隐藏信息、开局/出牌/终局输出顺序 | 待实现 | D-059 | 客户端 GameResult 在回放前，GM GameOver 在回放后 |
| 托管与超时 | NHSK `getAutoOutCards/changeRobotState/forceAutoOutCardForActiveSeat` | 最小领出、跟牌过、首次超时托管、100ms 主动切换边界 | 待实现 | D-060 | 显式关闭 TimeoutAutoMove 允许无限等待 |
| 托管结算认定 | NHSK `shouldCountAutoMove/isPlayerAutoInSubGame/applyAutoSettlementCorrection` | 按 Response 统计真人代打，旧 count/ratio 公式改变失败方负分 | 待实现 | D-065 | 保留反直觉乘法公式；统一 GM、客户端和回放标志 |
| 玩家装扮回放元数据 | GM `lockDressInfo`、GL `onMsgDress/replayAttributeInitialize` | START 前以 DRESS 覆盖已有玩家值，玩法不读取，回放冻结 | 待实现 | D-066 | 不透明字符串、无 Reply/Revision/客户端输出，不抽通用模块 |
| BaseRule | GL `GameRule.applyBaseRuleValue`、NHSK GameAPI 调用点 | 独立 INIT 字段优先，玩法配置仅保留 index 1/6/22 | 有意偏差 | D-061、D-079 | 其余历史索引不恢复业务；11/15/38/49 只作回放投影 |
| 回放 GameRule、Players 与文本编码 | GL `replayAttributeInitialize`、gamecore `SetGameRule/AddPlayerInfo/AddDress` | 固定 GameNum/GameTime 分支和属性；昵称 UTF-8/GBK；玩家与 Dress 经 map 迭代 | 待实现/有意偏差 | D-079 | schema/value 兼容，节点顺序改为 SeatID 稳定；不新增空 RecordGameStart 的参数 |
| 回放终局与统计 | NHSK `finishProcessResult/replayGameOverChairs/recordReplaySummaryAndOther`、gamecore `SetGameOver` | 根 GameOver、四座结果、Summary、炸弹 CardDetail；结束/动作耗时直接读系统时间 | 待实现/有意偏差 | D-080 | 注入 Clock；无 GameOver Move；RoundStat 只保留当前小局，不恢复战绩模块 |
| 回放中间 Moves/writer | NHSK `RecordDealDetail/RecordOutCard/RecordCurrentPoint/RecordCatchPoint/RecordTurnEnd`、gamecore XMLNode | 真实调用字段、反直觉线序、Actor、排序属性和牌值格式 | 待实现 | D-081 | 删除无调用 helper；writer 输出唯一当前形状，reader 可宽容旧形状 |
| 回放树冻结 | gamecore `Init/Save`、GL `GameOver` | 基础节点先建、Dress/Other 后追加，Save 前补 Count；旧实现同步编码写盘 | 有意偏差 | D-047、D-082 | Battle 内纯内存冻结深拷贝文档；writer 不接触可变状态 |
| 回放文本/Prop | gamecore `replayGbkToUtf8/SetMatchInfo`、GL `addMoveProp` | 字段级转码、可选 RoomInfo、无 Actor Prop 与重复 TargetID | 待实现 | D-067、D-083 | 不做全局转码或 JSON 解析；XML encoder 统一转义 |
| 回放玩家与 Deal 快照 | GL `replayAttributeInitialize`、GM `OnGLGameResult`、NHSK `DoDeal` | 开始前玩家字段、ACK 前分数更新、最终牌序 | 待实现 | D-057、D-084 | 每小局冻结；当前/结算/下一局分数语义不共用延迟读取 |
| ReplayName/ReplayUID | GL `createReplayName/GetUnicode`、NHSK `CalcSuccessResult` | 两套格式和消费者；FuPanUID 仅在客户端 ResultDetail | 待实现 | D-075、D-085 | 同一开始快照生成，彼此不推导；不误加进 0x8650 |
| 选牌预览 | NHSK `OnMsgCardAction`、`CARD_ACTION_WATCH` | 当前玩家宽松预览广播，OUT_CARD 单独权威校验 | 已一致 | D-062 | 只修复结算阶段残留座位误广播 |
| 新手发牌 | NHSK `dealCardsFromRules/RandCardListByNewPlayer` | 自定义牌优先；普通牌序后调整首个非机器人，旧 helper 可能交换四家牌 | 待实现 | D-063 | 固定 seed 锁定四家最终牌序；不恢复 Nacos 或通用偏洗牌 |
| 普通发牌散牌调整 | NHSK `dealCardsFromRules/SwapSingleCard` | 默认阈值 4，仅普通随机路径按座位依次换牌 | 待实现 | D-064 | 保留最终牌序；不承诺四家最终都低于阈值，不下沉通用模板 |
| 单一行动期限 | NHSK `DoAskOutCard` 及 Timer 测试 | 普通/Robot/AI deadline、VerifyCode | 待实现 | D-026 | 不复制双 Timer |
| 暂停 | GM 无 PAUSE 发送点；NHSK 只有直接测试 | 不出现暂停 Command、状态或 Timeline | 放弃 | D-045 | D-027 已被取代 |
| 外部 AI | 生产 `robot_level=2`、RobotTran 地址、1000ms move 与 6000ms timeout | HTTP/JSON/base64/二进制 golden、请求身份、最小延迟、迟到结果、候选复核 | 待实现 | D-026、D-028、D-048 | 默认本地 provider；隐藏信息不入日志 |
| Robot relay | NHSK 方法和直接测试，无生产调用点 | 新输出类型中不出现 Robot relay | 放弃 | D-044 | 与外部 AI 请求不是同一能力 |
| 重连与场景 | GM `OnPlayerReConnect/OnPlayerGameSence`、NHSK 两个入口测试 | Offline 副作用差异、恢复输出 | 待实现 | D-043 | 不能合并成同一写语义 |
| 道具 | GM `SendDropProp -> SendMsgToGL`、BaseGame `addMoveProp` | 只记录已成功广播的事实；目标顺序和重复值可观察 | 待实现 | D-044、D-067 | 不调用 NHSK 空 seam，不创造道具规则或通用服务 |
| GAME_MSG 内层路由 | GL `onMsgGameMsg`、NHSK `OnMsg` | 正常 relay 三个玩法 ID 与四类外围事实；未知 raw bytes 不属于领域 API | 待实现 | D-068 | 只保留 allowlist；0x7200/0x8655、投票、骰子放弃 |
| PLAYER_LIMIT / UpdatePlayerInfo | GM `SendPlayerLimit`、NHSK 空方法 | 前者错误嵌套后无人接收，后者无人调用 | 放弃 | D-069 | 不补齐旧 bug，不写占位 |
| 通用 GAME_START / GAME_END | GL `BaseGame.startGame/SendMsgGameStart/SendMsgGameEnd` | 每局调用 `0x7205 GAME_START`；`0x7206 GAME_END` 无调用点 | 前者待实现，后者放弃 | D-070 | GAME_START 是此前发现遗漏；GAME_END 不写死兼容 |
| ROUND_STAT / GAME_STARTED / GAME_OVER / NOTICE | GL `SendMsgPlayerRoundStat/startGame/GameOverProcess`、GM `OnStartGame/OnGameOver/checkResultAndRound` | GAME_STARTED 位于 NHSK StartGame 前；回放后向 ready 玩家 relay ROUND_STAT，再按四座数组发送 GAME_OVER；正常 GAME_OVER 由 GM 判断续局，NOTICE 仅强制 RoundOver | 待实现/有意偏差 | D-070、D-093 | 保留空 ROUND_STAT 线序但不恢复 BaseRule 5 战绩；GAME_OVER 数组索引即座位，不把 GM 的 Round 判断复制进 Battle |
| ReplayName 生命周期 | GL `StoreReplayName/GameOver`、GM `ProcessGameOver/SendMsGameResult` | 当前名直接进入同局 GAME_OVER；旧列表只写不读；GM 仅在真实大局结束时上报当次名称 | 待实现/有意删除死状态 | D-075、D-094 | 只保留当前小局名称，不累计、不拼接、不替未来 GM 预造索引 |
| 客户端投递资格 | GL `SendMsgGameStart/SendMsgToAll/SendMsgToUser/SendMsgPlayerRoundStat`、NHSK wrapper | GAME_START 与所有 NHSK 输出 force=true，只跳过 Exited；ROUND_STAT 额外检查 ClientReady | 待实现/有意收紧 | D-095 | 领域不暴露 force；START 额外拒绝 Exited 四座，正常输出资格保持参考 |
| GL→GM 客户端输出封装 | GL `PushMessageToUser`、protocol `ReqGL2GMRelayMessage`、GM `SendGLRelayMsg` | 广播逐用户产生 `0x8644 + 0x7400`；普通路径不写 Cnt/Clt，GM 按 UserID/proxyAddr 路由 | 待实现/有意确定化 | D-096 | Targets 按座位冻结；不使用 UserID=0，不让 Battle 持有连接编号 |
| MATCH_STOP 强制结算 | GM `Game.Clean`、GL `onCommandStop/StopGame`、NHSK `ForceGameOver` | Playing（含等待 0x8650）会清旧流程、跳过综合结算并本地 GameOver；非 Playing no-op | 待实现 | D-055、D-071 | DEL_GAME 不等待未完成回放；不承诺竞态输出必达 |
| 综合结算 | NHSK `SendMsgAskResScore`、GM `PushGameResult` | `0x8650` 单飞请求、ACK 响应、显式等待与回放收敛阶段；旧 GL 对坏项跳过或覆盖后继续；Score/Exp/ResultType 有冗余未消费字段；失败响应按 Dissolve 平局零分收尾 | 待实现/有意偏差 | D-044、D-053、D-088～D-091 | 成功响应按四座/Team/交易不变量整包校验；只消费身份、Flag 与交易矩阵；失败外观保持旧行为并单独诊断；Legacy/Cluster 共用 CompleteSettlement，无重试、超时或新增关联号 |
| 下一小局推进 | GM `CheckNextGame/StartTimerWaitGameOver/PreStartGame`、GL `onMsgUpdateGame/onMsgStartNewGame`、NHSK `ContinueGame` | `UPDATE_GAME -> PrepareSubgame`，`COMMAND START -> StartSubgame`；可选 START_NEW_GAME 只更新上下文；结算后的 CONTINUE no-op | 待实现 | D-054、D-056 | D-056 修正前置消息；GM 保留协调权 |
| 创建与初始化 | GM `RoundStart/ReqGl2GmNewGame/DoGameReady`、GL `ReqNewGame/InitGame` | ACK 后 AwaitingInit；一次性 INIT；可重复玩家 upsert；四座位开局门禁 | 待实现 | D-056 | 顺序为 NEW_GAME/ACK 后 INIT、玩家、UPDATE_GAME、START |
| 玩家更新与退出 | GM `UpdateGLPlayers/PushPlayerExitGame`、GL `onMsgUpdatePlayerInfo/onMsgPlayerExitGame` | 原子 upsert、业务/投递资料分层、Exited 可恢复；当前 GM 只写 Flag，旧 GL 只读恒零 PlayerFlag | 待实现/有意偏差 | D-057、D-087 | INIT 后全阶段可达；局中冻结座位；五个未消费或错位 wire 字段不进入领域，破产/封顶只取结算 ACK |
| 正常删除 | GM `CleanRound`、GL `ReqDelGameRound/ClearRound` | `MATCH_STOP` 业务收尾，`DEL_GAME` 触发 Mailbox 屏障和 Runtime Stop；旧 GM 不等待强制回放/生命周期输出 | 待实现/有意确定化 | D-055、D-071、D-092 | 保留同连接先入顺序；屏障不等 writer，允许孤立文件；隔离条目不删除 |
| 三套旧结算 ACK | GL 有 handler；当前 GM 无发送点 | 新代码中不存在 handler/codec | 放弃 | D-044 | 以后有需求重新新增 |
| 回放生成 | 配置 `replay_enabled=1`，但旧 BaseGame 开关判断被注释；GameOver 始终同步保存 | XML、规则/发牌/动作/结果、名称、目录、简单写入失败收敛 | 待实现 | D-044、D-047、D-086 | 删除假开关；外部有界 writer，无原子发布、fsync、重试或上传 |
| 回放上传 | 取决于 BaseRule index 24，仓库无目标规则样本 | 当前不出现上传 adapter | 放弃 | D-044 | 有真实规则数据再新增 |
| 自定义牌堆 | 生产 `custom_deck.enabled=1` 和白名单；旧 `GetCustomDeck/MakecardConfig` 每小局相邻重复读取，测试接受任意 `uint8` | ProductID 优先、GameID 回退、白名单、每小局一次快照、宽松调试 grammar、庄家和发牌 | 待实现 | D-044、D-046 | 示例本地文件，生产 Redis adapter；失败回退普通洗牌 |
| 固定六张牌 TestMode | NHSK `DoDeal/applyTestModeHands` | 所有目标配置关闭、无外部入口且不能形成完整牌局 | 放弃 | D-072 | 测试使用固定 seed 或注入 CustomDeckProvider，不留生产后门 |
| Config owner 与死字段 | NHSK `Config/loadConfig` 及全字段读取点 | 三个字段只加载未读取；外围 I/O 配置与玩法字段混装 | 有意偏差 | D-073 | NHSKConfig 只留真实玩法消费值，provider/runner 各自拥有 adapter 配置 |
| INIT 字段 owner | protocol `ReqGM2GLBodyInitGame`、GL `Round.InitGame`、BaseGame `replayAttributeInitialize` | 完整 wire、旧 Round 实际读取子集、客户端 Fee 和回放基础属性 | 待实现 | D-074 | CreateTime/未消费 MatchKey 只解码；回放复用单一身份与计分快照 |
| 回放时间与 UniCode | GL `PreStartGame/createReplayName/replayAttributeInitialize` | Unix 秒+CreatorID、文件名和目录时间 | 有意偏差 | D-075 | 单次注入 Clock 快照；格式不变，UTC+8，消除多次 time.Now 边界 |
| 偏置洗牌 | 取决于 GameRule 第 3 项和动态规则；无目标样本 | 当前不出现偏置实现 | 放弃 | D-044 | 测试存在不等于生产使用 |
| 惩罚 | 取决于 BaseRule index 52 和动态规则；无目标样本 | 当前不出现惩罚状态 | 放弃 | D-044 | 同上 |
| 战绩记录 | 取决于 BaseRule index 5；无目标样本 | 当前不出现战绩模块 | 放弃 | D-044 | 同上 |
| 投票解散 | generic relay 可承载，BaseGame 有实现；无专用调用、配置或录包 | 当前不出现投票状态机 | 放弃 | D-044 | 源码可达不等于生产使用 |
| 约局 | 生产 `yueju_mode=0` | 不出现约局离线专用状态机 | 放弃 | D-044 | 投票也因无使用证据单独放弃 |
| 旁观/赛事直播 | 生产 `match_live_mode=0`；watcher 调用被注释 | 不出现 WatchService/adapter | 放弃 | D-044 | `WATCH_MSG` 也已弃用 |
| GL 骰子定座 | 取决于 BaseRule 随机座位模式；无目标样本 | 当前不出现 GL 定座流程 | 放弃 | D-044 | GM 自身随机座位不等于 GL 路径 |
| `CHANGE_SEAT`/`TAKE_POINTS`/`CHANGE_ROUND` 输入 | GL controller 注册；当前 GM 无发送点 | 新 codec switch 不包含这些 ID | 放弃 | D-044 | ChangeRound 参考 ACK 还是空实现 |
| `PLAYER_OUT` 输出 | BaseGame 有 helper；NHSK 无调用点 | 新输出枚举不包含该能力 | 放弃 | D-044 | 当前 GM 注册不证明 NHSK 使用 |
| `DEL_ONE_GAME`、金额限制、预扣/退款 | protocol 遗留定义，无当前完整调用链 | 新代码中完全不存在 | 放弃 | D-044 | 不写占位 |

## 4. 已完成切片核对

### 4.1 GameLogic 应用配置

| 字段 | 内容 |
|---|---|
| 切片 | GameLogic 进程配置解析与校验 |
| GSR 文件/测试 | `examples/nhsk/config.go`、`examples/nhsk/config_test.go`、`examples/nhsk/config.example.json` |
| 参考入口 | `gamelogic/app/services/connectionservice/service.go`、`connecitons.go`，`nbgame_core/transport/newnet/tcp/connection/connection.go`、`protocols/bspacket.go`、`tcp/client.go` |
| 参考测试/配置/录包 | `gamelogic/config.yaml`、`config-test.yaml`、`config-pro.yaml` 中的 `connections.gameMaster` |
| Legacy MessageID | 本切片不编解码消息；确认 `NewBSProtocol(BS_CONNECTIONTYPE_GAME_LOGIC)` 在 client 创建时自动发送 origin 首包 |
| 输入与校验 | JSON 配置加显式环境覆盖；拒绝未知字段、非法地址、容量、超时、退避和启用后缺失的外围工具字段；错误不打印 secret 值 |
| 权威状态变化 | 无；只返回进程组合根私有的不可变配置值，不创建资源 |
| Timer/Timeline | 无业务 Timeline；只配置未来连接 owner 使用的拨号、origin、退避和稳定重置时长 |
| 输出目标与顺序 | 无网络输出 |
| 生命周期结果 | 配置失败时节点尚未创建；MySQL、Redis、微信默认关闭且不成为 GameLogic 启动依赖 |
| 结论 | GL 主动连接 GM、单地址配置和自动 origin **已一致**；持续指数退避、可配置超时及严格配置校验为 **有意偏差** |
| RFC/决策 | RFC-0410、D-037 |
| 备注 | 参考连接固定 3 秒 Dial，并以 timer/send 两套次数计数限制重连；新实现不复制该机制。参考配置的 reload 目前只发现初始化调用，没有把动态热更新提升为首版契约。Task 3 才创建连接并验证线序。 |

### 4.2 BattleID 数值表示

| 字段 | 内容 |
|---|---|
| 切片 | 通用 `game.BattleID` 从字符串迁移为 `uint32` |
| GSR 文件/测试 | `game/types.go`、`game/validation.go`、`game/room.go`、`game/battle_id_test.go` 及既有 Battle/Room/WhackMole 回归测试 |
| 参考入口 | `protocol/gamelogic/common/header.go`、`protocol/gamelogic/tcpprotocol/gm2gl.go`、`gamelogic/internal/roundmanager/round_manager.go` |
| 参考测试/配置/录包 | `TGM2GLHeader.GameInnerId uint32`；旧 GL 使用 `map[uint]*Round` 按同号查找、删除和复用 |
| Legacy MessageID | 本切片不编解码消息；所有 GLHeader 中的 GameInnerId 仍由 Task 3 codec 逐帧核对 |
| 输入与校验 | `BattleID(0)` 无效；非零 `uint32` 可作为 Battle/Room 数值索引，第一阶段由 Legacy mapper 直接接收 GameInnerId |
| 权威状态变化 | Battle 保存不可变数值 ID；Room 以同一数值键索引 Ref，不维护字符串映射 |
| Timer/Timeline | Timeline 继续只由当前 BattleRef 与 TimelineRevision fencing，不复制 BattleID 到内部 timer fire |
| 输出目标与顺序 | Snapshot 原样返回数值 BattleID；无网络输出 |
| 生命周期结果 | Battle 完全结束并从 Host/Room 索引移除后，协调者可以复用编号；当前 Core 不承担编号分配 |
| 结论 | wire 的 `uint32` 表示和旧 GL 数值索引 **已一致**；0 无效及活动编号冲突规则由 RFC 明确收紧 |
| RFC/决策 | RFC-0310、RFC-0410、D-022 |
| 备注 | WhackMole 只是通用 Battle API 消费者，改用小范围数值编号；未增加十进制字符串或 `legacy/` 前缀兼容层。 |

### 4.3 GameLogic 结构化日志

| 字段 | 内容 |
|---|---|
| 切片 | GameLogic 组合根 JSON 日志与脱敏 |
| GSR 文件/测试 | `examples/nhsk/logging.go`、`examples/nhsk/logging_test.go` |
| 参考入口 | `gamelogic/app/handler/game.go`、`internal/roundmanager/round_manager.go`、`internal/roundmanager/round.go`、`internal/game/game.go`，以及 `config.yaml` 的 `log` 配置 |
| 参考测试/配置/录包 | 旧 GL 使用 debug/info/error 级别，并在主要入口记录 GameInnerId、MessageID、handler 和结果；没有结构化日志测试 |
| Legacy MessageID | 日志可用 `command_id` 记录映射后的稳定命令编号；本切片不解释或编解码 Legacy payload |
| 输入与校验 | 日志级别仅允许 debug/info/warn/error；固定 JSON；token、secret、proof、微信 code 及原始 error/err/cause 属性按 key 脱敏 |
| 权威状态变化 | 无；logger 不是状态 owner |
| Timer/Timeline | 无；未来真实 Timeline 日志再加入其已定义的窄 Revision，不预建通用 Revision 字段 |
| 输出目标与顺序 | 进程 logger 固定带 node_id/process_role；Battle logger 追加数值 BattleID、完整 ServiceRef 和 ConnectionGeneration；错误使用稳定 error_category |
| 生命周期结果 | logger 由组合根构造并注入；不修改 Core Runtime 的 logger API |
| 结论 | 旧日志用于定位 handler、GameInnerId 和 MessageID 的目的 **已一致**；JSON、稳定字段和强制脱敏为 RFC 要求的 **有意偏差** |
| RFC/决策 | RFC-0410 |
| 备注 | 不复制参考实现的 `%+v` 全 payload/配置、`%p` Round 地址或错误 cause 输出。随着 Command Record、Subgame、TurnRevision 和 TimelineRevision 类型在后续切片出现，再把真实字段加入对应日志点。 |

### 4.4 GameLogic 节点 readiness 与关闭

| 字段 | 内容 |
|---|---|
| 切片 | GameLogic 组合根 readiness、逆序关闭与卡住 owner 诊断 |
| GSR 文件/测试 | `examples/nhsk/node.go`、`examples/nhsk/node_test.go` |
| 参考入口 | `gamelogic/app/server/serverstart.go`、`serverstop.go`，`nbgame_core/app.go` |
| 参考测试/配置/录包 | 旧 GL `BeforeStart` 依次初始化 handler、RoundManager、Game factory、连接和消息服务；`BeforeStop/AfterStop` 为空；nbgame_core 默认 stop timeout 为 10 秒 |
| Legacy MessageID | 无 |
| 输入与校验 | shutdown timeout 必须为正；组合根依赖和 root ServiceRef 必须有效；关闭 parent context 继续向每个 owner 传播 |
| 权威状态变化 | 节点只拥有 closing/closed、当前关闭 owner 和失败 owner 诊断；GM link 与 Quarantined 数量继续由各自 owner 提供只读健康快照 |
| Timer/Timeline | 无业务 Timeline |
| 输出目标与顺序 | shutdown 先使 readiness 为 NotReady，再按 connection、factory、root Services 逆创建顺序、Runtime 关闭；单步失败不跳过后续 owner |
| 生命周期结果 | 重复 Close 不重复调用 owner；卡住期间 `shutdownStatus` 精确显示尚未返回的 owner；全部调用收敛后 Closed 为 true，并保留失败 owner 名单 |
| 结论 | 启动依赖方向与旧 GL **已一致**；显式逆序关闭、Degraded readiness 和卡住 owner 诊断为 **有意偏差** |
| RFC/决策 | RFC-0410、D-037 |
| 备注 | 不复制旧 App 的固定 sleep、空 stop hook 或隐式 goroutine 收敛；也不创建通用 lifecycle group。Task 3 的真实连接和后续 factory 直接填入当前私有 owner 字段。 |

### 4.5 Legacy BSHeader、origin 与 NHSK MessageID

| 字段 | 内容 |
|---|---|
| 切片 | Legacy wire 最小 Header、origin 和保留 NHSK MessageID |
| GSR 文件/测试 | `examples/nhsk/internal/legacywire/header.go`、`header_test.go` |
| 参考入口 | `baison_middle/protocol/protocol.go`、`header.go`、`nbgame_core/transport/newnet/protocols/bspacket.go`、`nhsk/protocol/common.go` |
| 参考测试/配置/录包 | `nhsk/protocol/protocol_test.go`；参考 `NewBSProtocol` 构造及 `ConnectedEvent` 自动发送 origin |
| Legacy MessageID | `0x600`、`0x7400`、`0x7402`、`0x8605`、`0x8644`；`BS_MSG_GAME=0x7600`；客户端保留 `0x7601..0x7609`、`0x7611`、`0x7701`、`0x7702` |
| 输入与校验 | BSHeader 少于 24 字节拒绝；字段按固定小端 offset 解码 |
| 权威状态变化 | 无；wire 值不进入 Core 或 Battle 状态 |
| Timer/Timeline | 无 |
| 输出目标与顺序 | GameLogic origin 为 107、GameMaster origin 为 100；两者 Type=`0x600`、Length=24，其余字段为零，精确匹配 golden bytes |
| 生命周期结果 | 无 socket；Task 3 后续连接状态机在每个物理连接建立后使用同一 origin encoder |
| 结论 | Header offset、字节序、origin 和保留 MessageID **已一致**；无发现遗漏 |
| RFC/决策 | RFC-0410、D-036、D-044 |
| 备注 | 本切片不定义 `0x7610` 解说或 Robot relay 输出。外部 AI 的 `0x7612` 由后续 AI provider wire 单独拥有，不进入客户端 Legacy codec。 |

### 4.6 Legacy TCP frame 边界

| 字段 | 内容 |
|---|---|
| 切片 | 单条 TCP 字节流的有界 frame reader |
| GSR 文件/测试 | `examples/nhsk/internal/legacywire/packet.go`、`packet_test.go` |
| 参考入口 | `nbgame_core/transport/newnet/protocols/bspacket.go`、`tcp/client.go` |
| 参考测试/配置/录包 | 参考 `BSProtocol.UnpackDetect/PackDetect` 的 24 字节下界和 8 KiB 上界；RFC-0410 已裁决线上零 Length 不接受 |
| Legacy MessageID | frame reader 只读取 Header.Type，不判断已知或未知 ID |
| 输入与校验 | 精确读 24 字节 Header，再按最终 Length 读剩余 body；Length 必须为 `24..8192`；部分 Header/Body 是不可恢复截断 |
| 权威状态变化 | 无 |
| Timer/Timeline | 无 |
| 输出目标与顺序 | 每次只返回一个独立存储的完整 frame；连续 frame 不吞并，完整消费后返回 EOF |
| 生命周期结果 | 非法长度或截断返回稳定 framing 错误，由后续连接 owner 关闭当前 ConnectionGeneration |
| 结论 | Header 下界、frame 上限和连续读取 **已一致**；拒绝线上 `Length=0` 是 RFC 已批准的 **有意偏差** |
| RFC/决策 | RFC-0410、D-049 |
| 备注 | 未知 MessageID 与已知消息坏 body 具有完整 frame 边界，下一层负责局部丢弃并继续；本层不告警、不投递 Command。 |

### 4.7 Legacy 普通 relay 编解码

| 字段 | 内容 |
|---|---|
| 切片 | `0x8605 + 0x7402` 入站与 `0x8644 + 0x7400` 出站的最小 relay codec |
| GSR 文件/测试 | `examples/nhsk/internal/legacywire/codec.go`、`codec_test.go` |
| 参考入口 | `protocol/gamelogic/common/header.go`、`gm2gl.go`、`gl2gm.go`，`baison_middle/protocol/structs_add/common/gameheader.go`、`agent/game.go` |
| 参考测试/配置/录包 | 参考 formatter 的 `TGM2GLHeader=34`、`TGameHeader=24`、`BSSUFFIXIDX=8` 及普通 relay 组装顺序；golden hex 逐字段人工复算并与 formatter 写法核对 |
| Legacy MessageID | 入站外层 `0x8605`、内层 `0x7402`；出站外层 `0x8644`、内层 `0x7400` |
| 输入与校验 | 外层 HeaderLen 必须为 34；外层 Length 等于整帧；内层 Length 等于外层余量；suffix offset 必须为 56 且 offset+size 精确落在内层末尾；零 Length、错误方向、缺失固定区、间隙与尾随字节均拒绝 |
| 权威状态变化 | 无；codec 返回独立 payload 和完整重复身份，BattleID/UserID/MatchID/ProductID 一致性由后续 bridge 在进入 Command 前裁决 |
| Timer/Timeline | 无 |
| 输出目标与顺序 | 出站只写 BattleID、双层相同 UserID、MatchID、ProductID 和 payload；CntTID、CltTID、Reserved2、Magic、Reserve、Serial、Param 保持零；最终 Length 一次写对 |
| 生命周期结果 | 边界完整的 relay codec 错误由后续 classifier 局部丢帧，不关闭连接代际 |
| 结论 | Header 尺寸、字段顺序、方向 MessageID、suffix 相对起点和出站零值字段与参考实现 **已一致**；不接受 formatter 的 Length=0 中间态是 RFC 已批准的 **有意偏差** |
| RFC/决策 | RFC-0410、D-049、D-096 |
| 备注 | 本切片不解析 payload 内层 NHSK MessageID，也不做身份归一化或 Command 映射；因此坏 relay 不会在本层产生 Battle Command、Reply 或 GameOutput。 |

### 4.8 Legacy 双向 origin 握手

| 字段 | 内容 |
|---|---|
| 切片 | 单次物理连接的 GameLogic→GameMaster origin 握手原语 |
| GSR 文件/测试 | `examples/nhsk/internal/legacywire/handshake.go`、`handshake_test.go` |
| 参考入口 | `gamelogic/app/server/serverstart.go`、`gamemaster/app/server/tcpserver.go`、`nbgame_core/transport/newnet/protocols/bspacket.go`、`tcp/client.go` |
| 参考测试/配置/录包 | `BSProtocol.ConnectedEvent` 在连接建立后发送预生成 origin；GM 接受端按 origin=107 选择 GameController，并由自身协议回发 origin=100 |
| Legacy MessageID | 双方首包 Type=`0x600` |
| 输入与校验 | origin timeout 必须为正；先完整写出本方 24 字节 origin，再读取一帧；对端必须 Type=`0x600`、Origin=100、Length=24；Magic、Serial、Reserve、Param 不作为认证字段 |
| 权威状态变化 | 无；成功只证明连接来源握手完成，尚不发布 Ready 或创建 Battle |
| Timer/Timeline | 使用 socket deadline 限制握手，不创建业务 Timer |
| 输出目标与顺序 | 即使底层短写也循环写完本方 origin；成功后清除 deadline，后续业务 frame 才能由连接状态机读取 |
| 生命周期结果 | 写失败、无写进展、截断、错误 origin/type/body 或 deadline 操作失败均由未来连接 owner 关闭本代际；本原语不自行重连 |
| 结论 | 双向 origin 的发送方、值和“本方先写、对端后读”线序与参考实现 **已一致**；显式 timeout 与成功后清除 deadline 是 RFC 的确定化实现 |
| RFC/决策 | RFC-0410、D-036 |
| 备注 | 本切片不创建 ConnectionGeneration、OutputService 或重连循环；这些状态只有完整连接 owner 才能拥有。 |

### 4.9 Legacy 连接配置与退避策略

| 字段 | 内容 |
|---|---|
| 切片 | GameMaster 连接默认值、校验和有上限的指数退避计算 |
| GSR 文件/测试 | `examples/nhsk/internal/legacywire/connection_config.go`、`connection_config_test.go`；`examples/nhsk/config.go` |
| 参考入口 | `nbgame_core/transport/newnet/tcp/connection/connection.go` 的 `Send`、`Check`、`Connect`、`dial` |
| 参考测试/配置/录包 | 旧连接使用固定 3 秒 Dial，并分别以发送重试 5 次、定时检查 20 次限制重连；`Send` 会隐式触发 `Connect` |
| Legacy MessageID | 无 |
| 输入与校验 | Dial/origin/initial/max/stable 时长必须为正；initial 不得超过 max；multiplier 必须有限且大于 1；jitter 必须有限且位于 `(0,1)`；随机源必须存在 |
| 权威状态变化 | 私有策略只保存下一档基础等待；不拥有 socket、ConnectionGeneration、readiness 或 Battle |
| Timer/Timeline | 不创建 Runtime Timer 或 goroutine；未来连接 owner 使用可取消等待执行策略结果 |
| 输出目标与顺序 | 默认基础序列为 `1s,2s,4s,8s,16s,30s,30s`，每项再落入 ±20% 区间；连续 Ready 满 60 秒后重置到 1 秒 |
| 生命周期结果 | 本切片只计算等待，不 Dial、不 sleep、不重连；关闭时立即取消等待由下一连接 owner 切片负责 |
| 结论 | 保留“断线后重新连接”的业务目的；无限有界退避、稳定重置和 `Send` 不触发连接是 RFC 已批准的 **有意偏差** |
| RFC/决策 | RFC-0410、D-037 |
| 备注 | 默认值只由 `DefaultConnectionConfig` 定义，应用 JSON 默认配置从该值映射，不维护第二份常量。 |

## 5. 切片追加模板

复制下表并填写，不修改以前已经完成切片的证据：

| 字段 | 内容 |
|---|---|
| 切片 | |
| GSR 文件/测试 | |
| 参考入口 | |
| 参考测试/配置/录包 | |
| Legacy MessageID | |
| 输入与校验 | |
| 权威状态变化 | |
| Timer/Timeline | |
| 输出目标与顺序 | |
| 生命周期结果 | |
| 结论 | `已一致` / `有意偏差` / `发现遗漏` / `放弃` |
| RFC/决策 | |
| 备注 | |
