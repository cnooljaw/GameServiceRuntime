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
| 权威状态变化 | 无；codec 返回独立 payload 和完整重复身份；mapper 先验证 BattleID 与双层 UserID，后续 bridge 再把 MatchID/ProductID 与已初始化 Battle 身份核对 |
| Timer/Timeline | 无 |
| 输出目标与顺序 | 出站只写 BattleID、双层相同 UserID、MatchID、ProductID 和 payload；CntTID、CltTID、Reserved2、Magic、Reserve、Serial、Param 保持零；最终 Length 一次写对 |
| 生命周期结果 | 边界完整的 relay codec 错误由后续 classifier 局部丢帧，不关闭连接代际 |
| 结论 | Header 尺寸、字段顺序、方向 MessageID、suffix 相对起点和出站零值字段与参考实现 **已一致**；不接受 formatter 的 Length=0 中间态是 RFC 已批准的 **有意偏差** |
| RFC/决策 | RFC-0410、D-049、D-096 |
| 备注 | 本层不解析 payload 内层 NHSK MessageID，也不做身份归一化或 Command 映射；这些职责由后续 mapper 承担，因此坏 relay 不会在 codec 层产生 Battle Command、Reply 或 GameOutput。 |

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

### 4.10 每连接代际 GameOutputService

| 字段 | 内容 |
|---|---|
| 切片 | 协议无关的 `GameOutputBatch`、稳定连接失败类别和单代际输出 Service |
| GSR 文件/测试 | `examples/nhsk/outputs.go`、`output_service.go`、`output_service_test.go` |
| 参考入口 | `gamelogic/internal/game/game_api.go` 的 `SendMsgToAll/SendMsgToUser`、`app/services/msgservice/gamemasterservice.go` 的 `PushMessageToUser` |
| 参考测试/配置/录包 | 旧 BaseGame 逐玩家调用消息服务，最终共用同一 GM connection；没有跨连接代际缓存或独立每桌 socket |
| Legacy MessageID | 无；Service 不认识 `0x8644`、`0x7400` 或 NHSK MessageID，后续 sink/codec 才编码 |
| 输入与校验 | Batch 必须有非零 BattleID/MatchID/ProductID、完整 Battle Ref、匹配当前 Service 的非零 ConnectionGeneration 和至少一个非空类型化 GameOutput；旧代际或坏 Batch 不进入 sink |
| 权威状态变化 | Service 只拥有固定 ConnectionGeneration 与 sink/reporter capability；不拥有 Battle、socket、玩法状态或输出历史 |
| Timer/Timeline | 无 |
| 输出目标与顺序 | 一个连接代际的所有 Batch 经过同一 Mailbox，按接受顺序调用非阻塞 sink；Batch 内部 Outputs 保持原顺序 |
| 生命周期结果 | `ServiceSpec` 固定使用 `DiscardMailbox`，停止时不排空旧代际队列；Service 不关闭 sink。sink 拒绝报告 `output_sink_rejected`，Battle 入箱前拒绝保留独立的 `output_send_rejected` |
| 结论 | 逐玩家输出最终汇入同一 GM connection 的线序目的与参考实现 **已一致**；协议无关批次、代际 fence、单 Mailbox 和失败类别是 GSR 边界下的 **有意偏差** |
| RFC/决策 | RFC-0410、D-033、D-034、D-035 |
| 备注 | 本切片不实现 Legacy 编码、有界 writer FIFO、连接关闭或 Battle 提交；它只建立这些后续 owner 共同使用的最小 seam。 |

### 4.11 类型化 GAME_START Legacy egress

| 字段 | 内容 |
|---|---|
| 切片 | `GameStartPayload` 经逐目标展开编码为 `0x8644 + 0x7400 + 0x7205` |
| GSR 文件/测试 | `examples/nhsk/outputs.go`、`legacy_egress.go`、`legacy_egress_test.go`；`internal/legacywire/game_start.go`、`game_start_test.go` |
| 参考入口 | `gamelogic/internal/game/game_send_message.go:SendMsgGameStart`、`game.go:startGame`，`protocol/game/game.go:ReqGS2GCGameStart`、`tcpprotocol/game.go:BS_GAME_START` |
| 参考测试/配置/录包 | NHSK 未实现自定义 `GetStartMsg`，因此参考路径固定使用只有 24 字节 BSHeader 的 `0x7205`；StartGame 线序为 GAME_START、GAME_STARTED、玩法 StartGame |
| Legacy MessageID | 客户端 payload `0x7205`；relay 外层 `0x8644`、内层 `0x7400` |
| 输入与校验 | Batch 冻结非零 MatchID/ProductID；Targets 必须是可表示为非零 `uint32` 的十进制 PlayerID，保持输入顺序且不得重复；Kind 与 `GameStartPayload` 必须匹配 |
| 权威状态变化 | 无；MatchID/ProductID 是 Battle 已冻结身份的输出快照，adapter 不回查 Battle、不维护路由表 |
| Timer/Timeline | 无 |
| 输出目标与顺序 | payload 只编码一次，再按 Targets 顺序生成独立 frame；每帧内外 UserID 相同，CntTID/CltTID/Reserved2 为零，外层 Length=114、内层 Length=80、suffix offset=56/size=24 |
| 生命周期结果 | 编码任一输出或目标失败时整批不返回部分 frame，由后续 sink 将该提交视为失败并关闭匹配连接代际 |
| 结论 | GAME_START 的 MessageID、空 body、StartGame 前置线序和逐玩家 relay 与参考实现 **已一致**；类型化 payload 与 Batch 路由快照是 GSR adapter 边界下的 **有意偏差** |
| RFC/决策 | RFC-0410、D-033、D-095、D-096 |
| 备注 | 本切片只实现首个真实 ClientGameOutput，不添加无调用点的 `0x7206 GAME_END`，也不预建通用 MessageID→payload registry。 |

### 4.12 类型化 GAME_STARTED Legacy egress

| 字段 | 内容 |
|---|---|
| 切片 | `GameStartedOutput` 编码为直接发给旧 GameMaster 的 `0x8654` 控制帧 |
| GSR 文件/测试 | `examples/nhsk/outputs.go`、`legacy_egress.go`、`legacy_egress_test.go`；`internal/legacywire/game_started.go`、`game_started_test.go` |
| 参考入口 | `gamelogic/internal/game/game.go:startGame`、`app/services/msgservice/gamemasterservice.go:PushGameStarted`，`protocol/gamelogic/gl2gm.go:ReqBSGL2GMStarted` |
| 参考测试/配置/录包 | 实际 startGame 调用固定 `Res=true`；协议结构为 34 字节 TGM2GLHeader、1 字节 Res、80 字节 ReplayName |
| Legacy MessageID | `0x8654 BS_MSG_GL2GM_GAME_STARTED` |
| 输入与校验 | ReplayName 在领域边界必须非空；wire 保留参考 `copy([80]byte, name)` 的零填充和按字节截断语义 |
| 权威状态变化 | 无；ReplayName 由 Battle 的 StartSubgame 冻结，本 adapter 只消费不可变输出事实 |
| Timer/Timeline | 无 |
| 输出目标与顺序 | frame 直接发 GM，不走客户端 relay；HeaderLen=34、GameInnerID=BattleID、UserID=0、Res=1、总长=115；Batch 中位于 GAME_START 后即可保持原业务线序 |
| 生命周期结果 | 空 ReplayName 或同批其他非法输出使整批不返回部分 frame；连接失败仍由所属连接代际 owner 收敛 |
| 结论 | MessageID、字段布局、成功值、ReplayName copy 语义及 GAME_START 后发送的线序与参考实现 **已一致**；删除无实际调用的 Res=false 领域分支是“无调用点不实现”规则的 **有意偏差** |
| RFC/决策 | RFC-0410、D-033、D-070、D-085、D-094 |
| 备注 | 本切片不生成 ReplayName、不启动小局，也不提前实现 GAME_OVER、ROUND_STAT 或回放 writer。 |

### 4.13 类型化 NHSK GAME_INFO Legacy egress

| 字段 | 内容 |
|---|---|
| 切片 | `GameInfoPayload` 编码为客户端 `0x7601`，再按目标展开为 `0x8644 + 0x7400` relay |
| GSR 文件/测试 | `examples/nhsk/outputs.go`、`legacy_egress.go`、`legacy_egress_test.go`；`internal/legacywire/game_info.go`、`game_info_test.go` |
| 参考入口 | `nhsk/game/flow_core.go:DoGameStart`、`game/messages.go:SendMsgGameInfo`、`protocol/gs2gc.go:GameInfoNotify.FormatToTcp`、`protocol/tcpprotocol/gs2gc.go:GameInfoNotify` |
| 参考测试/配置/录包 | `nhsk/game/game_flow_test.go:TestDoGameStartDealsAndBroadcastsGameInfo` 验证首个玩法广播及 ServiceFee；生产结构固定为 50 字节 |
| Legacy MessageID | 客户端 payload `0x7601`；relay 外层 `0x8644`、内层 `0x7400` |
| 输入与校验 | Kind 必须匹配 `GameInfoPayload`；字段为 OutCardSeconds、ServiceFee、SeatID 0..3 的四个 Scores、GameNum，不接受任意旧协议值或额外配置 |
| 权威状态变化 | 无；payload 是 StartSubgame 已提交状态的不可变快照，编码不读取玩家或配置 owner |
| Timer/Timeline | 无 |
| 输出目标与顺序 | payload 长 50；完整单目标 frame 长 140，suffix offset=56/size=50；Targets 仍按 Batch 已冻结顺序逐用户展开 |
| 生命周期结果 | 类型不匹配或同批其他非法输出使整批返回零 frame；没有部分输出或状态回滚 |
| 结论 | MessageID、字段顺序、整数宽度、四座分数顺序和无 suffix 结构与参考实现 **已一致**；领域字段使用可读名称且不暴露旧 `GameInfoNotify` 是 GSR adapter 边界的 **有意偏差** |
| RFC/决策 | RFC-0410、D-033、D-059、D-070、D-095、D-096 |
| 备注 | 本切片只编码已形成的 GameInfo 快照；不实现 StartSubgame、玩家状态 owner、Deal 或 AskOutCard。 |

### 4.14 类型化 NHSK DEAL Legacy egress

| 字段 | 内容 |
|---|---|
| 切片 | 每个 `DealPayload` 编码一名玩家的私有 `0x7602`，再生成单目标 `0x8644 + 0x7400` relay |
| GSR 文件/测试 | `examples/nhsk/outputs.go`、`legacy_egress.go`、`legacy_egress_test.go`；`internal/legacywire/deal.go`、`deal_test.go` |
| 参考入口 | `nhsk/game/flow_core.go:DoDeal`、`game/messages.go:SendMsgDeal`、`protocol/gs2gc.go:DealNotify.FormatToTcp`、`protocol/tcpprotocol/gs2gc.go:DealNotify` |
| 参考测试/配置/录包 | `nhsk/game/game_flow_test.go:TestSendMsgDealOnlyIncludesReceiverHandCards` 明确验证逐用户一包、仅本座牌区有值、其他三座全零且没有广播 |
| Legacy MessageID | 客户端 payload `0x7602`；relay 外层 `0x8644`、内层 `0x7400` |
| 输入与校验 | Players 必须是 SeatID 0..3 的四个非零互异玩家；SeatID 位于 0..3；Cards 固定 26 字节；Targets 必须仅含 Players[SeatID]；Kind 与 `DealPayload` 必须匹配 |
| 权威状态变化 | 无；DealPayload 持有玩法已经提交的该座最终不可变手牌，adapter 不读取或裁剪可变 Battle 手牌 |
| Timer/Timeline | 无 |
| 输出目标与顺序 | payload 长 144，含四个 UserID 和四个 26 字节牌区；只填写接收座位牌区。完整 frame 长 234，suffix offset=56/size=144 |
| 生命周期结果 | 身份、座位、目标或类型任一不一致时整批返回零 frame；不会向错误玩家泄漏手牌，也不修改玩法状态 |
| 结论 | MessageID、字段布局、逐用户发送和仅接收座位可见手牌与参考代码及其隐私测试 **已一致**；领域值只携带单座手牌并在 adapter 重建零值牌区，是 GSR 深模块边界下的 **有意偏差** |
| RFC/决策 | RFC-0410、D-033、D-059、D-084、D-095、D-096 |
| 备注 | 本切片不实现洗牌、发牌算法、自定义牌堆或 Replay Deal；后续 Battle 切片必须证明它们复用同一份最终牌序。 |

### 4.15 类型化 NHSK ASK_OUT_CARD Legacy egress

| 字段 | 内容 |
|---|---|
| 切片 | `AskOutCardPayload` 编码为 `0x7603`，再按正常广播或场景恢复的 Targets 展开 relay |
| GSR 文件/测试 | `examples/nhsk/outputs.go`、`legacy_egress.go`、`legacy_egress_test.go`；`internal/legacywire/ask_out_card.go`、`ask_out_card_test.go` |
| 参考入口 | `nhsk/game/flow_core.go:DoAskOutCard`、`game/messages.go:SendMsgAskOutCard/SendMsgGameScene`、`protocol/gs2gc.go:AskOutCardNotify.FormatToTcp` |
| 参考测试/配置/录包 | `nhsk/protocol/protocol_test.go:TestAskOutCardNotifyHeader`；`game/game_flow_test.go:TestSendMsgGameSceneResendsActiveAskOutCard` 明确验证 UserID、VertifyCode 和 `SecRemain=9000` |
| Legacy MessageID | 客户端 payload `0x7603`；relay 外层 `0x8644`、内层 `0x7400` |
| 输入与校验 | ActivePlayer 必须可表示为非零 `uint32`，VerifyCode 非零，Kind 与 `AskOutCardPayload` 匹配；Targets 独立按通用规则校验，不要求包含 ActivePlayer |
| 权威状态变化 | 无；payload 是当前出牌机会的不可变展示快照，不创建、取消、替换或延长 ActionDeadline |
| Timer/Timeline | `ActionMilliseconds` 映射旧字段 SecRemain，但单位为毫秒且取玩家当前允许出牌时长；它不是动态 Timeline 剩余时间，编码无 Timer 副作用 |
| 输出目标与顺序 | payload 长 36，完整单目标 frame 长 126，suffix offset=56/size=36；正常流程面向过滤 Exited 后的全桌，场景恢复仅对当前行动者定向重发 |
| 生命周期结果 | 类型、行动者或 VerifyCode 非法时整批返回零 frame；合法 observer Targets 即使不含行动者也照常编码 |
| 结论 | MessageID、字段顺序、VerifyCode、毫秒值、正常广播及行动者场景恢复与参考实现 **已一致**；领域字段改名为 ActionMilliseconds、且不让 wire 展示值拥有 Timeline，是 GSR 边界的 **有意偏差** |
| RFC/决策 | RFC-0410、D-026、D-033、D-059、D-095、D-096 |
| 备注 | 本切片不生成 VerifyCode、不创建 TurnRevision/TimelineRevision，也不实现 ActionDeadline；后续 Battle 切片负责保证 `3,5,7...` 和唯一期限。 |

### 4.16 类型化 NHSK OUT_CARD_INFO Legacy egress

| 字段 | 内容 |
|---|---|
| 切片 | `OutCardInfoPayload` 编码为广播事实 `0x7604`，再按 Targets 展开逐用户 relay |
| GSR 文件/测试 | `examples/nhsk/outputs.go`、`legacy_egress.go`、`legacy_egress_test.go`；`internal/legacywire/out_card_info.go`、`out_card_info_test.go` |
| 参考入口 | `nhsk/game/flow_core.go:DoOutCard`、`game/messages.go:SendMsgOutCardInfo`、`protocol/gs2gc.go:OutCardInfoNotify.FormatToTcp` |
| 参考测试/配置/录包 | `nhsk/game/game_flow_test.go:TestDoOutCardRemovesCardsAndBroadcasts` 验证合法动作后 UserID、CardCount=2 和广播；协议 formatter 的 `copyCardsToFixed` 保留过牌 count=0 |
| Legacy MessageID | 客户端 payload `0x7604`；relay 外层 `0x8644`、内层 `0x7400` |
| 输入与校验 | Player 必须可表示为非零 `uint32`；领域 CardCount 位于 0..8，0 表示过牌；Kind 与 `OutCardInfoPayload` 必须匹配；Player 不要求出现在 Targets 中 |
| 权威状态变化 | 无；payload 只描述已经由 Battle 提交的动作事实，codec 不移除手牌、不更新上一手或回合状态 |
| Timer/Timeline | 无；成功动作后的旧期限失效和下一 Ask 由 Battle 负责，本输出不操作 Timeline |
| 输出目标与顺序 | payload 固定 55 字节：Player、26 字节 CardData、CardCount；只复制前 CardCount 张，其余为零。完整单目标 frame 长 145，suffix offset=56/size=55 |
| 生命周期结果 | 类型、Player 或领域牌数非法时整批返回零 frame；CardCount=0 正常产生过牌广播；成功输出不是 GSR Reply 或 error=0 ACK |
| 结论 | MessageID、字段布局、过牌表达、广播目标以及合法动作后才发送的语义与参考实现 **已一致**；领域只暴露 NHSK 最大 8 张而由 adapter 扩展旧 26 字节容量，是 GSR 边界的 **有意偏差** |
| RFC/决策 | RFC-0410、D-033、D-059、D-095、D-096 |
| 备注 | 本切片不实现出牌校验、手牌 mutation、ShowCards、TurnEnd 或下一 Ask；这些后续输出仍必须保持 D-059 线序。 |

### 4.17 类型化 NHSK TURN_END Legacy egress

| 字段 | 内容 |
|---|---|
| 切片 | `TurnEndPayload` 编码为本墩结束广播 `0x7605`，再按 Targets 展开逐用户 relay |
| GSR 文件/测试 | `examples/nhsk/outputs.go`、`legacy_egress.go`、`legacy_egress_test.go`；`internal/legacywire/turn_end.go`、`turn_end_test.go` |
| 参考入口 | `nhsk/game/helpers.go:endCurrentRound`、`game/messages.go:SendMsgTurnEnd`、`protocol/gs2gc.go:TurnEndNotify.FormatToTcp` |
| 参考测试/配置/录包 | `nhsk/game/game_flow_test.go:TestTurnEndAfterPlayerFinishedUsesPartnerWind/TestTurnEndAfterSkippedFinishedSeatLetsLastOutSeatLead` 验证墩结束后广播并选择下一行动者；wire 字段由直接 formatter 路径确认 |
| Legacy MessageID | 客户端 payload `0x7605`；relay 外层 `0x8644`、内层 `0x7400` |
| 输入与校验 | Winner 必须可表示为非零 `uint32`；CapturedPoints 允许为 0；Kind 与 `TurnEndPayload` 必须匹配；Winner 不要求出现在 Targets 中 |
| 权威状态变化 | 无；CapturedPoints 是 `awardCurrentScoreCardsToPreOutSeat` 本次返回的本墩抓分，不是玩家累计 Point；adapter 不保存第二份积分状态 |
| Timer/Timeline | 无；墩重置、下一行动者和下一 ActionDeadline 均由 Battle 在提交输出前后按业务顺序处理 |
| 输出目标与顺序 | payload 固定 32 字节：Winner、CapturedPoints；完整单目标 frame 长 122，suffix offset=56/size=32；只在最后 OutCardInfo 后、下一 Ask 前广播 |
| 生命周期结果 | 类型或 Winner 非法时整批返回零 frame；0 分墩正常输出；它不是同步 Reply，也不驱动回合推进 |
| 结论 | MessageID、字段布局、本墩赢家/抓分语义、广播目标和线序与参考实现 **已一致**；用 Winner/CapturedPoints 消除旧 UserID/Point 歧义是类型化领域边界的 **有意偏差** |
| RFC/决策 | RFC-0410、D-033、D-059、D-095、D-096 |
| 备注 | 本切片不实现抓分计算、累计 Point、墩重置、下一行动者或下一 Ask。 |

### 4.18 类型化 NHSK SHOW_CARDS Legacy egress

| 字段 | 内容 |
|---|---|
| 切片 | `ShowCardsPayload` 编码为手牌展示 `0x7606`，再按接收者视角冻结的 Targets 展开 relay |
| GSR 文件/测试 | `examples/nhsk/outputs.go`、`legacy_egress.go`、`legacy_egress_test.go`；`internal/legacywire/show_cards.go`、`show_cards_test.go` |
| 参考入口 | `nhsk/game/messages.go:SendMsgShowCard`、`game/flow_core.go:DoOutCard/DoGameOver`、`protocol/gs2gc.go:ShowCardsNotify.FormatToTcp` |
| 参考测试/配置/录包 | `nhsk/game/game_flow_test.go:TestDoOutCardSendsShowCardsToFinishedPlayerWhenPartnerHasCards`、终局 ShowCards 广播断言；`nhsk/protocol/protocol_test.go:TestShowCardsNotifyKeepsCountsForHiddenCards` |
| Legacy MessageID | 客户端 payload `0x7606`；relay 外层 `0x8644`、内层 `0x7400` |
| 输入与校验 | Players 必须是 SeatID 顺序的四个非零、互异玩家；HandCounts 为 0..26；每座 Cards 在 HandCount 后必须全零；Kind 与 `ShowCardsPayload` 必须匹配 |
| 权威状态变化 | 无；HandCounts 是 Battle 已冻结的真实剩余张数，Cards 只表达该接收者可见牌面；adapter 不持有或裁剪权威手牌 |
| Timer/Timeline | 无；终局展示等待仍由后续 Battle 阶段和 Timer Command 驱动，不由编码器启动 |
| 输出目标与顺序 | 玩家先出完且对家仍有牌时只定向本人并只展示对家；终局面向过滤 Exited 后的全桌并展示剩余牌；payload 固定 148 字节，完整单目标 frame 长 238，suffix offset=56/size=148 |
| 生命周期结果 | 类型、玩家、张数或尾部牌区非法时整批返回零 frame；隐藏牌仍保留真实 HandCount，牌区保持零 |
| 结论 | MessageID、四座布局、隐藏牌张数、定向/全桌两条路径与参考实现 **已一致**；将接收者视角在 Battle 内冻结、adapter 不再根据 Target 临时决定可见牌是 GSR 输出边界的 **有意偏差** |
| RFC/决策 | RFC-0410、D-033、D-059、D-095、D-096 |
| 备注 | 本切片不实现出完判定、终局判定、名次、展示 Timer 或结算推进。 |

### 4.19 类型化 NHSK GAME_RESULT Legacy egress

| 字段 | 内容 |
|---|---|
| 切片 | `GameResultPayload` 编码为综合结算已应用后的客户端结果 `0x7607`，再按 Targets 展开 relay |
| GSR 文件/测试 | `examples/nhsk/outputs.go`、`legacy_egress.go`、`legacy_egress_test.go`；`internal/legacywire/game_result.go`、`game_result_test.go` |
| 参考入口 | `nhsk/game/flow_core.go:CalcSuccessResult/applyResultScores`、`game/interface.go:finishProcessResult`、`game/messages.go:SendMsgGameOver`、`protocol/gs2gc.go:GameResultNotify.FormatToTcp` |
| 参考测试/配置/录包 | `nhsk/protocol/protocol_test.go:TestGameResultNotifyFormatToTcpWritesSuffixDetail`、`nhsk/game/game_flow_test.go:TestDoProcessResultRecordsReplaySummaryAndOther/TestCalcSuccessResultUsesPartnerGroups`；当前环境因参考 `go.mod` 指向无访问权限的私有模块而不能运行，已直接复核测试源码与 formatter |
| Legacy MessageID | 客户端 payload `0x7607`；relay 外层 `0x8644`、内层 `0x7400` |
| 输入与校验 | Players 必须是 SeatID 顺序的四个非零、互异玩家；Reason 为 0..4；Outcome 为 Win/Loss/Peace；Rank 为 1..4；Result 为 Single/Double/Peace；WinningTeam 为 0/1，Peace 时固定 0；Kind 与 `GameResultPayload` 必须匹配 |
| 权威状态变化 | 无；Scores 是综合结算交易矩阵已应用后的最终本局分，其他字段来自 Battle 已提交的同一结果快照；adapter 不计算输赢或回查玩家 |
| Timer/Timeline | 无；不启动展示、结算或回放 Timer，不推进 Battle 阶段 |
| 输出目标与顺序 | 面向过滤 Exited 后的全桌；正常终局线序为 ShowCards→结算请求/完成→GameResult→ReplayDocument；payload 固定 154 字节，主消息 suffix offset=32/size=122，完整单目标 frame 长 244，relay suffix offset=56/size=154 |
| 生命周期结果 | 类型、玩家或结果枚举非法时整批返回零 frame；ReplayUID 直接按 64 字节 copy 语义零填充或截断，不增加校验 |
| 结论 | MessageID、ResultDetail 字段顺序、两层 suffix、最终分、ReplayUID 和客户端结果先于回放的线序与参考实现 **已一致**；领域命名替代旧 `ResultDetail` 与字节 bool 是类型化输出边界的 **有意偏差** |
| RFC/决策 | RFC-0410、D-047、D-059、D-085、D-088～D-091、D-095、D-096 |
| 备注 | 本切片不实现输赢算法、综合结算请求/ACK、玩家分数 mutation、回放构建或 GAME_OVER。 |

### 4.20 类型化 NHSK GAME_SCENE Legacy egress

| 字段 | 内容 |
|---|---|
| 切片 | 请求者视角的 `GameScenePayload` 编码为断线重连/场景恢复定向消息 `0x7608` |
| GSR 文件/测试 | `examples/nhsk/outputs.go`、`legacy_egress.go`、`legacy_egress_test.go`；`internal/legacywire/game_scene.go`、`game_scene_test.go` |
| 参考入口 | `nhsk/game/messages.go:SendMsgGameScene/buildGameScenePacket/buildScenePlayer/outCardRemainSec/shouldRevealHand`、`protocol/gs2gc.go:GameSceneNotify.FormatToTcp` |
| 参考测试/配置/录包 | `nhsk/game/game_flow_test.go:TestSendMsgGameSceneBuildsHiddenSceneSuffixes/TestSendMsgGameSceneResendsActiveAskOutCard`、`nhsk/protocol/protocol_test.go:TestGameSceneNotifyFormatToTcpWritesSceneAndPlayers`；参考测试当前仍受无权限私有模块依赖阻塞，已直接复核源码 |
| Legacy MessageID | 客户端 payload `0x7608`；relay 外层 `0x8644`、内层 `0x7400` |
| 输入与校验 | 单一 Target 且属于四座玩家；State 为 Playing(3)/ShowingResult(4)；座位为 -1 或 0..3；墩分牌 0..24、完成人数 0..4；玩家非零互异，手牌 0..26，LastPlayCount 为 -1..8，Rank 为 0..4；固定牌区 count 后必须全零 |
| 权威状态变化 | 无；Automated/Offline 只投影为旧 State bit 1/2；隐藏手牌保留真实 HandCount；adapter 不回查 Battle 或按 Target 临时裁剪牌面 |
| Timer/Timeline | 无；RemainingSeconds 是 Battle 从当前单一 ActionDeadline 冻结的向下取整秒数，0 表示没有期限；编码不创建、替换或延长 Timeline |
| 输出目标与顺序 | GAME_SCENE 只定向请求者；完整恢复由 Battle 按 GameInfo→GameScene→可选 AskOutCard 排列，亮牌阶段随后产生的全桌 ShowCards 仍是独立输出 |
| 生命周期结果 | payload 固定 282 字节：主消息 Scene suffix offset=44/size=42，PlayerCount=4，Players suffix offset=86/size=196；完整单目标 frame 长 372，relay suffix offset=56/size=282；非法输入整批返回零 frame |
| 结论 | MessageID、双 suffix、四座固定布局、请求者/对家可见性、状态 bit、剩余秒和 LastPlayCount 三态与参考实现 **已一致**；以单一 ActionDeadline 替代参考多 Timer 取最大值、由 Battle 预先冻结请求者视角是既有 Timeline/adapter 裁决下的 **有意偏差** |
| RFC/决策 | RFC-0410、D-043、D-059、D-060、D-095、D-096 |
| 备注 | 本切片不实现 ReconnectPlayer、RequestGameScene、RestorePlayerView、ClientReady mutation、托管取消、Cluster Snapshot、AskOutCard 或亮牌广播。 |

### 4.21 类型化 NHSK OUT_CARD_RESULT Legacy egress

| 字段 | 内容 |
|---|---|
| 切片 | `OutCardRejectionPayload` 编码为真人非法出牌的定向失败结果 `0x7609` |
| GSR 文件/测试 | `examples/nhsk/outputs.go`、`legacy_egress.go`、`legacy_egress_test.go`；`internal/legacywire/out_card_result.go`、`out_card_result_test.go` |
| 参考入口 | `nhsk/game/handlers.go:OnMsgOutCard`、`game/flow_core.go:DoOutCard`、`game/messages.go:SendMsgOutCardResult`、`protocol/gs2gc.go:OutCardResultNotify.FormatToTcp` |
| 参考测试/配置/录包 | `nhsk/game/game_flow_test.go:TestDoOutCardRejectsStaleVerifyCode`；其他错误分支由 `DoOutCard` 直接调用点确认；参考测试当前仍受无权限私有模块依赖阻塞，已直接复核源码 |
| Legacy MessageID | 客户端 payload `0x7609`；relay 外层 `0x8644`、内层 `0x7400` |
| 输入与校验 | 单一 Target；Reason 只接受 CardCount(1)、Seat(2)、VerifyCode(3)、CardType(4)、Paused(5)；Kind 与 `OutCardRejectionPayload` 必须匹配；NoError(0) 和未知值拒绝 |
| 权威状态变化 | 无；输出只描述 Battle 已判定的稳定拒绝，不修改手牌、当前行动者、VerifyCode、托管或玩家状态 |
| Timer/Timeline | 无；参考所有可达失败都在 `StopTimer` 前返回，新实现不取消、替换或延长 ActionDeadline，也不重发 AskOutCard |
| 输出目标与顺序 | 只定向被拒绝的真人请求玩家；AI、机器人、托管和 Timeline 自动候选失败静默；成功动作没有 ACK |
| 生命周期结果 | payload 固定 28 字节，完整单目标 frame 长 118，relay suffix offset=56/size=28；非法 reason、目标数或 payload 类型使整批返回零 frame |
| 结论 | MessageID、错误码、真人定向、自动候选静默、成功无 ACK 和期限不变与参考实现 **已一致**；删除不可达 NoError 领域变体、使用 Rejection 命名是类型化边界的 **有意偏差** |
| RFC/决策 | RFC-0410、D-026、D-059、D-060、D-095、D-096 |
| 备注 | 本切片不实现出牌校验、拒绝决策、手牌 mutation、期限管理或成功动作输出。 |

### 4.22 类型化 NHSK CARD_ACTION_WATCH Legacy egress

| 字段 | 内容 |
|---|---|
| 切片 | `CardSelectionPreviewPayload` 编码为当前操作玩家的宽松选牌预览广播 `0x7611` |
| GSR 文件/测试 | `examples/nhsk/outputs.go`、`legacy_egress.go`、`legacy_egress_test.go`；`internal/legacywire/card_action_watch.go`、`card_action_watch_test.go` |
| 参考入口 | `nhsk/game/handlers.go:OnMsgCardAction`、`protocol/gc2gs.go:CardActionRequest.FormatFromTcp`、`protocol/gs2gc.go:CardActionWatchNotify.FormatToTcp` |
| 参考测试/配置/录包 | 参考仓库没有 CARD_ACTION 专项测试；由 handler 原样复制 `req.Cards`、固定 wire struct 与 D-062 已确认行为复核；参考测试环境仍受无权限私有模块依赖阻塞 |
| Legacy MessageID | 客户端 payload `0x7611`；relay 外层 `0x8644`、内层 `0x7400`；输入请求保持独立 `0x7702` |
| 输入与校验 | Player 必须是非零 uint32；CardCount 为 0..26；固定 Cards 在 CardCount 后必须为零；Kind 与 `CardSelectionPreviewPayload` 必须匹配；不校验手牌归属、重复、牌型或压牌 |
| 权威状态变化 | 无；这是客户端选择过程的瞬时事实，不修改手牌、当前墩、玩家状态或任何第二份预览状态 |
| Timer/Timeline | 无；不取消、替换或延长 ActionDeadline，不修改 TurnRevision |
| 输出目标与顺序 | Battle 只在 Playing/WaitingForAction 且请求者是当前操作人时产生；正常面向过滤 Exited 后的全桌广播；允许空选择、重复牌、非手牌和不能压牌的列表 |
| 生命周期结果 | payload 固定 55 字节，完整单目标 frame 长 145，relay suffix offset=56/size=55；非法玩家、容量、尾部或 payload 类型使整批返回零 frame |
| 结论 | MessageID、UserID、26 字节上限、全桌广播和宽松预览语义与参考实现 **已一致**；删除未接入 BaseRule 开关、用非权威 Preview 领域命名是 D-062 下的 **有意偏差** |
| RFC/决策 | RFC-0410、D-059、D-062、D-068、D-095、D-096 |
| 备注 | 本切片不实现 `PreviewCardSelection` Command、阶段/当前玩家门禁或真正 `PlayCards` 校验。 |

### 4.23 NHSK CARD_ACTION Legacy payload 解码

| 字段 | 内容 |
|---|---|
| 切片 | 将客户端 `0x7702 CARD_ACTION` 固定 payload 解码为独立 `CardActionRequest` 值 |
| GSR 文件/测试 | `examples/nhsk/internal/legacywire/card_action_request.go`、`card_action_request_test.go` |
| 参考入口 | `nhsk/protocol/tcpprotocol/gc2gs.go:CardActionRequest`、`nhsk/protocol/gc2gs.go:CardActionRequest.FormatFromTcp`、`nhsk/game/handlers.go:OnMsgCardAction` |
| 参考测试/配置/录包 | 参考仓库没有 CARD_ACTION 专项测试；golden 按参考 struct 小端布局与 formatter 复核；参考测试环境仍受无权限私有模块依赖阻塞 |
| Legacy MessageID | 输入 payload `0x7702`；本切片不解码外层 `0x8605 + 0x7402` relay |
| 输入与校验 | 固定 51 字节；Header Type/Length 必须为 `0x7702`/51；CardCount 为 0..26；CardCount 后固定牌区必须为零；空选择有效；Header 其他字段不进入归一化结果 |
| 权威状态变化 | 无；codec 返回复制后的 Cards/Count，不保存客户端缓冲区，不触达 Battle |
| Timer/Timeline | 无 |
| 输出目标与顺序 | 无；成功只提供给后续 bridge 映射，失败静默丢弃且不产生 `0x7609` |
| 生命周期结果 | 解码不持有 caller storage；短包、长包、错误 MessageID、错误 Length、超量 count 和非零尾部均稳定失败 |
| 结论 | MessageID、51 字节布局、26 字节容量、空选择和不做玩法校验与参考实现 **已一致**；显式拒绝非零未使用牌区是严格 wire 边界下的 **有意收紧**，不改变合法客户端行为 |
| RFC/决策 | RFC-0410、D-062、D-068 |
| 备注 | 本切片不实现 relay 身份一致性校验、Battle 阶段/当前玩家门禁或 `0x7611` 广播决策。 |

### 4.24 NHSK OUT_CARD Legacy payload 解码

| 字段 | 内容 |
|---|---|
| 切片 | 将客户端 `0x7701 OUT_CARD` 固定 payload 解码为独立 `OutCardRequest` 值 |
| GSR 文件/测试 | `examples/nhsk/internal/legacywire/out_card_request.go`、`out_card_request_test.go` |
| 参考入口 | `nhsk/protocol/tcpprotocol/gc2gs.go:OutCardRequest`、`nhsk/protocol/gc2gs.go:OutCardRequest.FormatFromTcp`、`nhsk/game/handlers.go:OnMsgOutCard`、`nhsk/game/flow_core.go:DoOutCard` |
| 参考测试/配置/录包 | `nhsk/game/game_flow_test.go:TestDoOutCardRejectsStaleVerifyCode` 证明 VerifyCode 属于业务拒绝；9..26 张到 `DoOutCard` 的 CardCount 分支由源码复核；golden 按参考 struct 小端布局核对；参考测试环境仍受无权限私有模块依赖阻塞 |
| Legacy MessageID | 输入 payload `0x7701`；本切片不解码外层 `0x8605 + 0x7402` relay |
| 输入与校验 | 固定 55 字节；Header Type/Length 必须为 `0x7701`/55；CardCount 为 0..26；CardCount 后固定牌区必须为零；保留末尾 uint32 VerifyCode；空选择和零 VerifyCode 可解码 |
| 权威状态变化 | 无；codec 只返回复制后的 Cards/Count/VerifyCode，不触达 Battle |
| Timer/Timeline | 无；VerifyCode 尚未和当前行动机会比较 |
| 输出目标与顺序 | 无；9..26 张继续进入后续 PlayCards，以保留真人 `0x7609 CardCount(1)` 业务拒绝；结构坏包静默丢弃 |
| 生命周期结果 | 解码不持有 caller storage；短包、长包、错误 MessageID、错误 Length、超过 26 的 count 和非零尾部均稳定失败 |
| 结论 | MessageID、55 字节布局、26 字节 wire 容量、过牌和 VerifyCode 原样传递与参考实现 **已一致**；显式拒绝非零未使用牌区是严格 wire 边界下的 **有意收紧**，不改变合法客户端行为 |
| RFC/决策 | RFC-0410、D-026、D-062、D-068 |
| 备注 | 本切片不实现 relay 身份一致性校验、当前玩家/VerifyCode/牌数/手牌/牌型校验或出牌状态变化。 |

### 4.25 NHSK Legacy gameplay Command 映射

| 字段 | 内容 |
|---|---|
| 切片 | 将已校验的 `0x7701 OUT_CARD`、`0x7702 CARD_ACTION` payload 显式映射为公开 GSR gameplay Command；后续相邻切片追加通用 `0x720A USER_STATE_CHANGE` |
| GSR 文件/测试 | `examples/nhsk/commands.go`、`commands_external_test.go`、`legacy_mapper.go`、`legacy_mapper_test.go`；`internal/legacywire/client_gameplay.go`、`client_gameplay_test.go` |
| 参考入口 | `nhsk/game/interface.go:OnMsg`、`nhsk/game/handlers.go:OnMsgOutCard/OnMsgCardAction` |
| 参考测试/配置/录包 | 两条已冻结 payload golden 复用真实 MessageID 与字段布局；参考仓库没有 CARD_ACTION 专项测试，OUT_CARD 业务拒绝继续由后续 handler 测试负责 |
| Legacy MessageID | 显式 `0x7701 -> PlayCardsCommand(0x04100301)`；`0x7702 -> PreviewCardSelectionCommand(0x04100302)`；不做数值复用或算术转换；`0x720A` 映射见 4.27 |
| 输入与校验 | 外层归一化 UserID 必须非零并转为十进制 `game.PlayerID`；Cards 深拷贝为领域 `[]byte`，丢弃 Header 与 CardCount；OUT_CARD 保留 VerifyCode；未知 MessageID、坏 frame 或坏 body 拒绝 |
| 权威状态变化 | 无；mapper 只产生 `gsr.Command`，不发送、不触达 Mailbox、不保存 Battle 状态 |
| Timer/Timeline | 无；VerifyCode、行动机会和 Deadline 尚未判定 |
| 输出目标与顺序 | 无；Legacy bridge 后续只需把返回 Command Send 到已解析 BattleRef；Cluster 调用者直接构造同一公开 request |
| 生命周期结果 | 返回 payload 不持有 Legacy frame storage；空 CARD_ACTION 与 9..26 张 OUT_CARD 均保持可映射，玩法拒绝留给 Battle；`package nhsk` 可由 Cluster 调用方直接导入 |
| 结论 | 两条 MessageID 分发、请求 UserID、Cards 与 VerifyCode 的业务流向和参考 `OnMsg` **已一致**；使用独立 CommandID、领域 slice 和类型化 request 是 D-097 下的 **有意边界调整** |
| RFC/决策 | RFC-0410、D-042、D-062、D-068、D-097 |
| 备注 | 本切片不实现 `0x8605 + 0x7402` relay 身份核对、BattleRef 路由、Runtime Send/Call、Battle handler 或客户端输出。 |

### 4.26 NHSK Legacy gameplay relay 归一化

| 字段 | 内容 |
|---|---|
| 切片 | 将完整 `0x8605 + 0x7402 + NHSK payload` 归一化为 Battle 路由身份和类型化 gameplay Command |
| GSR 文件/测试 | `examples/nhsk/legacy_relay_mapper.go`、`legacy_relay_mapper_test.go`；复用 `internal/legacywire/codec.go` 与 `legacy_mapper.go` |
| 参考入口 | `protocol/gamelogic/gm2gl.go:ReqGM2GLGameMsg.FormatFromTcp`、`gamelogic/app/handler/game.go:ReqGameMsg.Execute`、`gamelogic/internal/game/game.go:onMsgGameMsg`、`nhsk/game/interface.go:OnMsg` |
| 参考测试/配置/录包 | 145 字节 OUT_CARD 与 141 字节 CARD_ACTION 完整 relay golden 按参考 `GLHeader + BSHeader + GameHeader + BSSUFFIXIDX + payload` 逐字段复算；参考仓库没有完整 relay 专项测试 |
| Legacy MessageID | 外层 `0x8605`、内层 `0x7402`；payload `0x7701/0x7702` 分别进入既有显式 Command 映射；`0x720A` 由 4.27 追加到同一入口 |
| 输入与校验 | BattleID、外层 UserID 必须非零；内层 GameHeader.UserID 必须与外层一致；保留 MatchID/ProductID 供 bridge 与已初始化 Battle 身份核对；ConnectType/PlatformID/Reserved 不进入 Command |
| 权威状态变化 | 无；返回私有 `legacyInboundGameplayCommand{BattleID, MatchID, ProductID, Command}`，不查询 Host、不发送 Mailbox |
| Timer/Timeline | 无 |
| 输出目标与顺序 | 无；成功结果供后续 bridge 按 BattleID/ConnectionGeneration 路由，坏 relay、身份冲突、未知或坏 payload 均无 Command 输出 |
| 生命周期结果 | OUT_CARD/CARD_ACTION Command payload 均不持有 frame storage；错误只属于当前完整 frame，连接是否继续由 connection owner 按 D-049 决定 |
| 结论 | 外层 GameInnerID/UserID、内层 UserID 与 payload 的业务流向和参考实现 **已一致**；在进入 Battle 前显式拒绝双层身份冲突是已冻结 adapter 安全边界 |
| RFC/决策 | RFC-0410、D-039、D-049、D-068、D-097 |
| 备注 | 本切片不实现 INIT 身份缓存、MatchID/ProductID 最终比对、BattleRef 解析、ConnectionGeneration fence、Runtime Send/Call 或 Battle handler。 |

### 4.27 USER_STATE_CHANGE 托管 Command 映射

| 字段 | 内容 |
|---|---|
| 切片 | 将通用 `0x720A USER_STATE_CHANGE` payload 及完整 gameplay relay 归一化为 `SetPlayerAutoStateCommand` |
| GSR 文件/测试 | `examples/nhsk/commands.go`、`legacy_mapper.go`、`legacy_mapper_test.go`、`legacy_relay_mapper_test.go`；`internal/legacywire/header.go`、`user_state_change.go`、`user_state_change_test.go`、`client_gameplay.go` 及对应测试 |
| 参考入口 | `protocol/game/tcpprotocol/game.go:BS_USER_STATE_CHANGE`、`protocol/game/game.go:ReqGC2GSGameUserStateChange`、`nhsk/game/interface.go:OnMsg`、`nhsk/game/second_batch.go:OnMsgUserStateChange` |
| 参考测试/配置/录包 | `nhsk/game/game_flow_test.go:TestOnMsgUserStateChangePacketTogglesAuto/TestUserStateChangeTogglesAutoAndBroadcastsRobotState/TestUserStateChangeRejectsOutsidePlayState/TestUserStateChangeActiveSeatAutoForcesOutCard`；32 字节 payload 与 122 字节完整 relay golden |
| Legacy MessageID | `0x720A -> SetPlayerAutoStateCommand(0x04100303)`；仍位于允许的 `0x7402` gameplay relay 内，不接受裸 `0x7200` wrapper |
| 输入与校验 | payload 固定 32 字节 Header+UserId+State；Header Type/Length 必须为 `0x720A`/32；payload UserId 必须非零并与 relay 外层和 GameHeader UserID 一致；State bit 0 映射 Enabled，其他 bit 忽略 |
| 权威状态变化 | mapper 无状态变化，只产生 `SetPlayerAutoStateRequest{Player, Enabled}`；托管状态、广播和当前玩家自动动作由 Battle Mailbox Handler 负责 |
| Timer/Timeline | mapper 不取消或替换期限；后续 Battle 按 D-060 决定当前操作人的 100ms 边界和唯一 ActionDeadline |
| 输出目标与顺序 | mapper 无输出；Battle 成功后才产生一次全桌状态通知和一次请求者定向确认，非法阶段或身份静默拒绝 |
| 生命周期结果 | 任意 uint32 State 可解码，只有 bit 0 进入领域；坏结构、三层 UserID 冲突或未知消息不产生 Command；完整 relay 不持有 caller storage |
| 结论 | `0x720A` 布局、payload UserId、bit 0 托管语义及进入 NHSK `OnMsgUserStateChange` 的流向与参考实现 **已一致**；显式三层身份一致性是既有 adapter 安全边界 |
| RFC/决策 | RFC-0410、D-026、D-042、D-060、D-068、D-097 |
| 备注 | 本切片不实现 Battle 阶段/座位校验、托管 mutation、RobotState 输出、主动自动出牌、BattleRef 路由或 Runtime Send/Call。 |

### 4.28 NHSK Battle Mailbox 与 Host/Factory 生命周期最小切片

| 字段 | 内容 |
|---|---|
| 切片 | 建立可由 Cluster 创建、解析、初始化并直接调用的 NHSK Battle；Host 只持有 `BattleID -> ServiceRef`，Factory Service 执行创建/停止，Legacy relay 与 Cluster Call 共用 Battle Command |
| GSR 文件/测试 | `examples/nhsk/battle.go`、`battle_test.go`、`host.go`、`host_test.go`、`commands.go`、`legacy_relay_mapper.go`、`legacy_relay_mapper_test.go` |
| 参考入口 | `gamelogic/app/handler/game.go` 的 GameLogic 生命周期转发；`gamelogic/internal/game/game.go:onMsgGameMsg` 的按 GameInnerID 路由；`nhsk/game/interface.go:OnMsg` 的初始化/玩家/玩法入口；GSR `game/battle.go` 的 Service、Mailbox、Snapshot 和 Timer 边界 |
| 输入与校验 | Battle 只接受类型化 `InitializeBattle/UpdatePlayers/PrepareSubgame/StartSubgame/PlayCards/Preview/SetPlayerAutoState`；初始化冻结 BattleID/ProductID/MatchID；Start 前要求四个非零玩家与 0..3 座位；OUT_CARD 在 Battle 内校验行动玩家、VerifyCode、牌数、手牌、同点和基本压制关系；Preview 保持参考的宽松牌面校验；Host 只接受 0 外的 BattleID 且由 Factory 返回完整 ServiceRef |
| 权威状态变化 | Battle Mailbox 独占阶段、玩家、手牌、当前座位、VerifyCode、Auto/Offline、最近出牌和业务 Revision；Host 独占活动 Ref 索引；Factory 不拥有牌局状态；Legacy relay 只解析、ResolveHost、再向 Battle 发送/调用，不保存旧 ServiceRef |
| Timer/Timeline | 本切片保留私有 timer Command 入口和 Revision 检查 seam；完整唯一 ActionDeadline、托管自动动作和 AI provider 仍未进入本切片 |
| 输出目标与顺序 | Battle 产生类型化 `GameOutputBatch`，不直接编码 `0x8644`；已配置 OutputService 时按当前玩家目标发送 GAME_START、OUT_CARD_INFO、CARD_ACTION_WATCH、OUT_CARD_RESULT/SCENE；无 OutputService 的 Cluster 测试只验证 Command 结果与 Snapshot |
| 生命周期结果 | Host BeginCreate 返回 Creating Operation，Factory 完成后 Host 才发布 Active Ref；ResolveBattle 只返回 Active；Delete 通过有界 stop runner 停止精确 Ref，真实停止后移除索引并允许编号复用；Runtime Stop 由 Factory runner 执行，避免 Host Handler 在单 worker 上同步停 Battle 造成调用环 |
| 结论 | Service/Command/Mailbox/Host/Factory 边界与“Legacy、Cluster 同一 Command”目标已建立；基础四座发牌与出牌可运行。与最终 RFC 的异步 runner、Quarantine/诊断、完整牌型/结算/回放、Origin/TCP owner 仍是 **发现遗漏/待实现**，本切片不宣称可无损替换旧 GameLogic |
| RFC/决策 | RFC-0310、RFC-0410、RFC-0500、D-039、D-062、D-097 |
| 备注 | Battle 的示例发牌采用确定性四座分片，当前只覆盖最小 Command 流；生产规则、Legacy NEW_GAME/INIT/UPDATE_PLAYER 全量 codec、连接代际和旧 GM 控制消息必须在后续切片补齐并逐项回查参考代码。 |

### 4.29 Legacy connection owner、进程组合根与最小期限/结算

| 字段 | 内容 |
|---|---|
| 切片 | 增加单条主动 GM TCP connection owner、双向 origin、bounded output queue、指数退避重连、独立 `cmd/gamelogic` 组合根，并把 Battle 的行动期限与结算入口接入 Mailbox |
| GSR 文件/测试 | `examples/nhsk/legacy_connection.go`、`legacy_connection_test.go`、`process.go`、`process_test.go`、`cmd/gamelogic/main.go`；`internal/legacywire/packet.go`、`handshake.go`、`connection_config.go`；`battle.go`、`battle_test.go` |
| 参考入口 | `gamelogic/app/handler/game.go` 的主动连接/控制器入口；`gamelogic/internal/game/game.go` 的消息分发；`protocol/gamelogic/gm2gl.go` 与 `gl2gm.go` 的 origin/控制消息常量；`nhsk/game/flow_core.go` 的出牌期限与结算阶段 |
| 输入与校验 | connection owner 每个物理连接分配新 ConnectionGeneration，先写 origin=107、校验 GM origin=100，再读取受限 frame；输出入有界队列，队列满拒绝；Battle 每个 turn 只保存一个 turnRevision/期限，并对旧 Timer Command 做 fencing；最后一名玩家出完后只进入 AwaitingSettlement，CompleteSettlement 才进入 Finished |
| 权威状态变化 | TCP socket、reader/writer、重连状态和输出队列由 LegacyGMConnection 拥有；Battle 仍只在 Mailbox 修改手牌、期限、TurnRevision 和结算；进程组合根拥有 Runtime、Host、Factory、Connection 的关闭顺序 |
| Timer/Timeline | Timer 只投递私有 `nhskBattleTimerCommand`；托管当前行动人可用一个较短期限自动出最小单张；迟到 Timer 只忽略，不执行回调；尚未实现 AI 最小延迟替换和完整参考 timeout 分支 |
| 输出目标与顺序 | writer 将类型化 `GameOutputBatch` 转成既有 Legacy egress frame；handshake 未 Ready 时不接受输出；socket writer 失败关闭当前代际，不关闭新代际 |
| 生命周期结果 | `cmd/gamelogic` 可从 JSON 配置启动，收到 SIGINT/SIGTERM 后先停连接，再关闭 Runtime；连接重连采用配置的 bounded exponential backoff/jitter；控制面 frame 的完整 NEW_GAME/INIT/UPDATE_PLAYER/DEL_GAME/结算 ACK 解码和 ACK 发送仍未完成 |
| 结论 | origin、连接代际、输出 owner、进程装配和最小期限/结算边界已建立；完整旧 GM 控制面、Quarantine、完整牌型与回放仍是 **发现遗漏/待实现** |
| RFC/决策 | RFC-0180、RFC-0320、RFC-0410、RFC-0500、D-004、D-025、D-026、D-091、D-092 |
| 备注 | connection owner 的 I/O goroutine 只读写 socket/队列，不直接修改 Battle；它通过 `OnFrame` seam 把控制面留给后续 adapter，不在本切片伪造旧结构体。 |

### 4.30 Legacy GM 控制面 codec、路由与代际输出绑定

| 字段 | 内容 |
|---|---|
| 切片 | 解码旧 GM→GL 的 `NEW_GAME/INIT_GAME/UPDATE_PLAYER/COMMAND/UPDATE_GAME/START_NEW_GAME/DRESS/PLAYER_EXIT/DEL_GAME/0x80008650`，归一化到 Host/Battle Command，并让组合根按连接代际创建 OutputService、响应 NEW_GAME ACK、断线停止该代际 Battle |
| GSR 文件/测试 | `examples/nhsk/internal/legacywire/control.go`、`control_test.go`、`control_egress.go`、`control_egress_test.go`；`examples/nhsk/legacy_control_mapper.go`、`legacy_control_mapper_test.go`；`legacy_connection.go`、`legacy_connection_test.go`、`host.go`、`process.go` |
| 参考入口 | `protocol/gamelogic/gm2gl.go` 的 GM2GL structs/MessageID；`protocol/gamelogic/tcpprotocol/gm2gl.go` 的固定布局；`gamelogic/app/handler/game.go` 的按 GameInnerID 转发；`gamelogic/internal/game/game.go` 的 BaseGame 分发；`gamemaster/app/service/bussinessservice/gamelogic_service.go` 的控制消息发送；`gamelogic/app/services/msgservice/gamemasterservice.go` 的 NEW_GAME ACK |
| Legacy MessageID | `0x86c1 -> BeginCreateBattle` 并在 Host 完成后编码 `0x800086c0` ACK；`0x8600/01/02/04/06/0x860d/0x8610/0x86c2` 分别进入 Initialize/UpdatePlayers/Start或ForceFinish/Prepare/Exit/RoundContext/Dress/Delete；`0x80008650` 只解码并进入最小 CompleteSettlement；已确认但未迁移的旧控制号标为 `ControlUnsupported`，不使连接无故重连 |
| 输入与校验 | 所有 frame 先核对 BSHeader.Length；GLHeader 固定 34 字节；suffix 使用绝对 offset/size 且必须精确到 frame 尾；UPDATE_PLAYER 数量受 8 KiB 上限约束；BattleID 转 `game.BattleID(uint32)`，0 无效；玩家 ID 转十进制 `game.PlayerID`；旧 Round Command 仅保留 START(0)/MATCH_STOP(4)，PAUSE/CONTINUE/STOP/EXCEPTION 作为未迁移能力拒绝 |
| 权威状态变化 | NEW_GAME 由 Host/Factory 创建并发布 BattleRef；Factory 记录 ConnectionGeneration、OutputRef 和 BattleRef，代际断线通过有界 runner 停止该代际 Battle；Battle 仍只在 Mailbox 修改业务状态；PlayerFlag/ScoreChangeReason/ScoreChange 等字段仅解码，不建立无证据状态 |
| Timer/Timeline | 控制 codec 不创建 Timer；Battle 继续使用单一 `TurnRevision` deadline，断线收敛由 Factory lifecycle Command 执行 |
| 输出目标与顺序 | NEW_GAME ACK、最小 GAME_STARTED/GAME_OVER 进入同一连接 owner 的有界 raw/typed frame 队列；玩法输出继续 `GameOutputBatch -> GameOutputService -> Submit`，新代际不会消费旧代际批次；NOTICE、ROUND_STAT 和完整综合结算响应 wire 仍未实现 |
| 生命周期结果 | NEW_GAME ACK 等待 Host Operation 完成后才发送成功；业务拒绝发送失败 ACK 但保持 TCP 代际；GM 断线停止该代际普通 Battle，Quarantined/诊断模型尚未实现；连接 owner 继续无限退避重连 |
| 结论 | 旧 GM 控制面的布局、顺序、MessageID 与参考代码 **已一致**；使用 GSR Host/Battle Command、显式十进制 BattleID 和代际 OutputService 是 RFC-0410 下的 **有意边界调整**；完整牌规、综合结算字段消费、NOTICE/ROUND_STAT/Quarantine 仍是 **发现遗漏/待实现** |
| RFC/决策 | RFC-0410、RFC-0180、RFC-0310、D-039、D-062、D-091、D-092 |
| 备注 | 参考目录未修改；本切片只写 GSR 新代码和 `.codegraph` 外的测试/文档，不把旧 Go struct 作为运行时依赖。 |

### 4.31 Legacy GM 回合结束通知出站

| 字段 | 内容 |
|---|---|
| 切片 | 增加旧 GL→GM `NOTICE_ROUND_OVER (0x864e)` 的固定 codec、类型化 `NoticeRoundOverOutput` 和强制结束线序；强制结束时先发最小 `GAME_OVER (0x8641)`，再发 `NOTICE_ROUND_OVER` |
| GSR 文件/测试 | `examples/nhsk/internal/legacywire/round_over.go`、`round_over_test.go`；`examples/nhsk/outputs.go`、`legacy_egress.go`、`round_over_output_test.go`；`battle.go`、`battle_test.go` |
| 参考入口 | `protocol/gamelogic/gl2gm.go` 的 `BS_MSG_GL2GM_NOTICE_ROUND_OVER`；`protocol/gamelogic/tcpprotocol/gl2gm.go` 的 `BS_GL2GM_NOTICE_ROUND_OVER` 固定布局；`gamelogic/internal/game/game.go` 的 `GameOverProcess`、`SendMsgGL2GMRoundOver` 和 `isForceRoundOver` |
| Legacy MessageID/布局 | `0x864e`；`TGM2GLHeader (34 bytes) + YueJuEndReason int32 + YueJuEndPlayerId uint32`，总长度 42 字节；BattleID 仍使用现有十进制业务编号映射 |
| 输出与顺序 | `ForceFinishSubgameCommand` 在 Battle Mailbox 内提交 `GameOverOutput` 后提交 `NoticeRoundOverOutput`；同一输出 owner 按队列顺序编码为 `0x8641 -> 0x864e`。正常 `CompleteSettlement` 只提交 `GAME_OVER`，不伪造 NOTICE |
| 已一致 | MessageID、固定字段顺序、header 长度、force-round-over 条件和 `GAME_OVER` 在前/NOTICE 在后的参考时序已用源码与单元测试复核 |
| 有意偏差 | 当前 `GAME_OVER` 仍是空 PlayerData 的最小响应，`ROUND_STAT`、综合结算 ResultDetail 和真实 `endReason/endPlayer` 领域来源尚未迁移；强制路径按已冻结契约使用 `Success(0)`/`IsGameOver=0`，不凭空建立结束原因状态 |
| 发现遗漏 | 参考 `GameOverProcess` 在 GAME_OVER 前还发送 `ROUND_STAT`；该消息及其玩家数据投影需在结算 adapter 具备权威来源后单独实现，不能在本切片填充伪造数据 |
| 结论 | `NOTICE_ROUND_OVER` wire 与强制结束出站线序已完成；ROUND_STAT、完整玩家结算数据和真实结束上下文仍保持待实现，不改变当前最小 Battle 终态语义 |
| RFC/决策 | RFC-0410、D-091、D-092 |
| 备注 | 参考目录未修改；新 codec 不 import 旧协议 struct，玩家结束 ID 为空时编码为 0。 |

### 4.32 Legacy Client ROUND_STAT 空投影 egress

| 字段 | 内容 |
|---|---|
| 切片 | 增加客户端 `ROUND_STAT (0x7246)` 的空统计固定 codec、类型化 `OutputRoundStat/RoundStatPayload` 和 Legacy relay egress；暂不伪造结算时序 |
| GSR 文件/测试 | `examples/nhsk/internal/legacywire/round_stat.go`、`round_stat_test.go`；`examples/nhsk/outputs.go`、`legacy_egress.go`、`round_stat_output_test.go` |
| 参考入口 | `protocol/game/game.go:ReqGS2GCRoundStat.FormatToTcp`；`protocol/game/tcpprotocol/game.go:BS_MSG_GAME_ROUND_STAT`；`gamelogic/internal/game/game_send_message.go:SendMsgPlayerRoundStat`；`gamelogic/internal/game/game.go:GameOverProcess` |
| Legacy MessageID/布局 | payload `0x7246`；`BSHeader (24) + PlayerCount uint32 + BSSUFFIXIDX (8)`，固定长度 36 字节；首版 `PlayerCount=0`、suffix offset=36、size=0 |
| 输出目标与顺序 | `ClientGameOutput{Kind: OutputRoundStat}` 复用既有按目标逐用户 `0x8644 + 0x7400` relay；目标列表由调用方冻结，adapter 不生成 UserID=0 广播 |
| 已一致 | MessageID、固定长度、空统计字段、suffix index 和逐用户 relay 结构已用参考源码与单元测试复核 |
| 有意偏差 | 参考 `SendMsgPlayerRoundStat` 只向 `!Exited && ClientReady` 玩家投递；当前 Battle 尚未建立独立 ClientReady 权威来源，因此本切片只提供 codec/adapter，不在 `CompleteSettlement` 或 `MATCH_STOP` 中主动发包 |
| 发现遗漏 | 参考 `GameOverProcess` 先 ROUND_STAT 后 GAME_OVER；正式接入必须与 GameResult、回放收敛及 ClientReady 资格一起完成，不能只在当前最小结算入口插入一个广播 |
| 结论 | ROUND_STAT 的协议边界已就绪；结算时序、目标资格和完整统计来源继续保持待实现，避免建立无证据的业务状态 |
| RFC/决策 | RFC-0410、D-059、D-093、D-095 |
| 备注 | 参考目录未修改；不恢复 BaseRule index 5 的跨小局统计模块，也不把 Replay Summary 伪造成 ROUND_STAT。 |

### 4.33 Legacy USER_RECONNECT/GAME_SCENE 与恢复视图

| 字段 | 内容 |
|---|---|
| 切片 | 增加客户端 `0x7208 USER_RECONNECT`、`0x720D GAME_SCENE` 的固定布局 codec/mapper；Battle 建立 ClientReady 目标资格，并提供 Reconnect/Scene 的最小恢复线序 `GameInfo -> GameScene -> 当前 AskOutCard` |
| GSR 文件/测试 | `examples/nhsk/internal/legacywire/player_view.go`、`player_view_test.go`；`header.go`、`client_gameplay.go`、`legacy_mapper.go` 及测试；`battle.go`、`battle_test.go` |
| 参考入口 | `protocol/game/game.go`；`protocol/game/tcpprotocol/game.go`；`gamelogic/internal/game/game.go:OnMsgUserReconnect/OnMsgUserGameScene`；`nhsk/game/interface.go`；`nhsk/game/messages.go:SendMsgGameScene/buildScenePlayer` |
| MessageID/布局 | `0x7208/0x720D`；请求均为 `BSHeader (24) + TGameHeader (24)`，固定 48 字节；嵌入 UserID 必须与 relay 外层 UserID 一致，MatchID/ProductID 由 adapter 解码但不重复写入 Battle 状态 |
| 状态/副作用 | Reconnect 清除 Offline；仅 Playing 时退出托管并恢复；Scene 要求有效 game/subgame，不清除 Offline，退出托管并恢复；成功恢复均标记 ClientReady |
| 输出 | 目标玩家依次收到 GameInfo、GameScene；Playing 且目标为当前行动人时再收到 AskOutCard。Scene 只暴露请求者自己的手牌，其他玩家手牌隐藏 |
| 已一致 | MessageID、固定长度/类型校验、三层身份一致性、Reconnect 与 Scene 的 Offline/托管差异、恢复输出顺序和 `!Exited && ClientReady` 目标模型 |
| 有意偏差 | 当前最小 Scene 尚未实现完整 ShowCards/GameRecords/commentary 和“请求者无牌时显示伙伴手牌”的特殊规则；新加入玩家初始 ClientReady=true 以保持旧 GameLogic 的可投递行为，资格表仍只在 Battle 内维护 |
| 发现遗漏 | 参考 `SendMsgGameScene` 在部分非 Playing 阶段也可能发送 GameInfo，并包含亮牌/回放记录；完整恢复需要等结算与回放状态权威来源后接入，不能用当前最小 Snapshot 伪造 |
| 结论 | 两个旧请求现在进入统一类型化 Command，且不泄露其他玩家手牌；完整恢复 golden 和亮牌分支留给后续切片 |
| RFC/决策 | RFC-0410、D-043、D-095、D-096 |
| 备注 | 参考目录未修改；本切片只写入 GSR 和复核文档。 |

### 4.34 NHSK 对家出完牌与 SHOW_CARDS

| 字段 | 内容 |
|---|---|
| 切片 | 修正单个玩家出完牌的生命周期：继续当前小局并向该玩家展示固定对家手牌；只有同一对家组两座都出完才进入 AwaitingSettlement，并向全桌展示剩余手牌；下一行动继续输出 AskOutCard |
| GSR 文件/测试 | `examples/nhsk/battle.go`、`battle_test.go`；复用 `outputs.go` 的 `ShowCardsPayload` 和既有 Legacy `0x7606` egress |
| 参考入口 | `nhsk/game/flow_core.go:DoOutCard`；`nhsk/game/messages.go:SendMsgShowCard/buildScenePlayer/shouldRevealHand`；`nhsk/game/game_flow_test.go:TestDoOutCardSendsShowCardsToFinishedPlayerWhenPartnerHasCards`；`gamelogic/internal/game/game_send_message.go` 的客户端输出资格过滤 |
| 业务规则 | 两副牌固定对家为 Seat 0/2、1/3；单座出完只记录 Rank、增加 FinishedPlayerCount、跳过该座继续行动；对家也出完后进入 AwaitingSettlement |
| 输出目标与顺序 | 单座出完：`OUT_CARD_INFO -> SHOW_CARDS(target=出完玩家) -> ASK_OUT_CARD`；对家组结束：`OUT_CARD_INFO -> SHOW_CARDS(target=所有未退出玩家)`；SHOW_CARDS 保留所有座位 HandCount，单座路径只填对家 Cards，终局路径填所有剩余 Cards |
| 场景恢复 | 请求者手牌为空时，`GameScene` 额外显示其固定对家手牌；其他对手手牌仍隐藏。FinishedPlayerCount 和最小 Rank 从 Battle Mailbox 状态读取 |
| 已一致 | 单座出完不提前结算、固定对家显示、对家组结束门禁、SHOW_CARDS 目标/隐藏信息和下一 Ask 线序已用旧源码与 GSR 测试复核 |
| 有意偏差 | 当前 GSR 仍未迁移真实墩分、LastPlayedCards、CapturedPoints、完整牌型/名次算法和 GameRecords；Rank 只记录出完顺序，不作为综合结算结果来源 |
| 发现遗漏 | 参考终局还会进入亮牌等待、GameResult、回放和 ROUND_STAT/GAME_OVER；本切片只修正出完牌边界和客户端可见手牌，不伪造后续结算事实 |
| 结论 | Battle 不再把单个玩家出完误判为小局结束；对家组结束才允许 CompleteSettlement，且 Legacy/Cluster 共用同一 Mailbox 状态门禁 |
| RFC/决策 | RFC-0410、D-026、D-043、D-095 |
| 备注 | 参考目录未修改；Legacy `0x7606` codec/egress 复用既有实现。 |

### 4.35 Legacy 0x8650 综合结算后缀与矩阵门禁

| 字段 | 内容 |
|---|---|
| 切片 | 类型化解码旧 GM→GL `0x80008650` 的 ResultDetail/PlayerData 两段 suffix，并在 CompleteSettlement 中建立整包分数矩阵校验 |
| GSR 文件/测试 | `examples/nhsk/internal/legacywire/control.go`、`control_test.go`；`examples/nhsk/commands.go`；`examples/nhsk/legacy_control_mapper.go`、`legacy_control_mapper_test.go`；`examples/nhsk/battle.go`、`battle_test.go` |
| 参考入口 | `protocol/gamelogic/gm2gl.go:ReqGM2GLGameResultComprehensive`；`protocol/gamelogic/common/playerresult.go:TComGain/TComPlayerResult`；`gamecore/constants/player.go:BS_GAME_RESULT_USER_FLAG_SEAL/BREAK`；`gamelogic/internal/game/game.go:onMsgGameResultComprehensive/buildGameScore` |
| Legacy 布局 | 固定 67 字节（GLHeader 34 + `IsSuccess/ResultType/ResultCount/PlayerCount/TeamCount` 20 + 两个 BSSUFFIXIDX 16）；ResultDetail 每条 12 字节 `PayTeamId/GainTeamId/Score`，PlayerData 每条 20 字节 `PlayerId/Flag/Score/Exp/TeamId`；两个 offset/size 必须连续并精确到 frame 尾 |
| 输入与校验 | 记录数与 suffix 字节数精确匹配；Battle 成功路径要求四名冻结玩家、TeamCount=4、PlayerID 可映射、TeamID=SeatID、正分、有效 TeamID、Pay≠Gain 且有向交易键不重复；坏包整包拒绝并保留 AwaitingSettlement，不部分写分数 |
| 权威状态变化 | 交易矩阵计算每座分数（付款方扣分、收款方加分）后，与 `Flag & 0x100/0x200` 对应的 IsSeal/IsBreak 一次性应用；`PlayerData.Score/Exp` 和 `ResultType` 不进入 Battle 权威分数；`IsSuccess=false` 忽略详情、清零四座分数与标志并按 GameOverReasonDissolve 收敛；早期 Cluster `Scores[4]int32` 兼容回退暂保留 |
| 已一致 | 旧 12/20 字节记录布局、计数/边界语义、有效交易矩阵的分数方向与四座 TeamID/SeatID 关系，已用源码和失败/成功测试复核；Legacy 与 Cluster 进入同一 CompleteSettlement Mailbox |
| 有意偏差 | Legacy UPDATE_PLAYER 的 PlayerFlag 仍按 D-087 丢弃，不与结算 ACK 的 Flag 混用；PlayerData.Score/Exp 的展示、ResultType 领域含义、客户端 GameResult、回放、ROUND_STAT/GAME_OVER 完整终局时序仍未实现；不在本切片伪造这些输出 |
| 发现遗漏 | 参考旧实现仍允许跳过无法映射的记录并把 ResultDetail 交给玩法逻辑；生产替换前需完成真实 Flag/失败原因、完整 NHSK 分数/名次算法与重复/迟到/MATCH_STOP 竞争测试 |
| 结论 | `0x8650` 不再是只解码成功标志的空入口；adapter 已完成最小协议归一化，Battle 已具备分数与 IsSeal/IsBreak 的全量矩阵原子门禁及失败 Dissolve 收敛，但完整结算输出仍保持后续切片 |
| RFC/决策 | RFC-0410、D-091、D-092 |
| 备注 | 参考目录只读未修改；本切片只写入 GSR、测试和文档。 |

### 4.36 NHSK 牌型识别、比较与真实双副牌堆

| 字段 | 内容 |
|---|---|
| 切片 | 用参考 NHSK `Logic.GetCardType/CompareCardType/RemoveCards` 替换旧 Battle 中仅按低位相等和数量比较的简化牌规，并把示例发牌改为两副完整无王牌堆 |
| GSR 文件/测试 | `examples/nhsk/card_rules.go`、`card_rules_test.go`、`battle.go`；`battle_test.go` 的既有出牌生命周期回归 |
| 参考入口 | `nhsk/logic/logic.go:GetCardType`、`CompareCardType`、`RemoveCards`；`nhsk/logic/logic_test.go`；`nhsk/macros/macros.go` 的 `FullCount=104`、`CardCount=26`、`MinBombNum=4` 和 1..13 牌值 |
| 参考测试/配置/录包 | 参考测试覆盖单牌、对子、三张、三带二、炸弹、A/2 逻辑值、炸弹长度和不同非炸弹牌型不可互压；旧 `FullCardData` 是四种花色、1..13 牌值的两份副本 |
| Legacy MessageID | 本切片不新增 MessageID；`OUT_CARD (0x7701)` 与 `CARD_ACTION (0x7702)` 仍通过既有 relay/Command 入口进入 Battle |
| 输入与校验 | 牌值低四位必须为 1..13；识别单牌、同值对子、同值三张、三带二和 4 张以上同值炸弹；出牌仍要求来自当前玩家手牌并保留 8 张上限，双副允许相同物理牌字节按出现次数消费 |
| 权威状态变化 | `NHSKBattleService` Mailbox 仍独占手牌、最近牌型和回合状态；`classifyCards/compareCardSets` 只返回私有牌型值，不建立额外持久状态；`deal` 为四座按 26 张切分 104 张合法牌 |
| Timer/Timeline | 无新增 Timer/Timeline；现有 `TurnRevision` 和期限边界不变 |
| 输出目标与顺序 | 无新增输出；成功出牌仍按既有 `OUT_CARD_INFO -> SHOW_CARDS/ASK_OUT_CARD` 线序，非法牌型仍返回既有 `card_type` 拒绝 |
| 生命周期结果 | 不改变对家出完牌、AwaitingSettlement 或 CompleteSettlement 门禁；牌堆仍为确定性示例数据，未声称具备生产随机洗牌 |
| 已一致 | 牌型集合、A/2 逻辑值、不同非炸弹牌型不可互压、炸弹优先、同型炸弹长度优先、双副重复物理牌消费，以及 104 张/每座 26 张的牌堆规模均与参考源码和测试一致 |
| 有意偏差 | 当前牌堆按固定顺序切分，不实现参考中的随机、新手换牌、散牌调整、自定义牌堆和计分/单扣双扣；牌型仅作为 Battle 校验结果，不写入回放或完整 GameResult |
| 发现遗漏 | 参考还包含牌堆策略、抓分、名次和完整结算输出；这些需要 CARD-022、CARD-024、CARD-031 后续切片提供权威来源，不能在本切片伪造 |
| 结论 | 牌型识别/比较和合法双副牌堆 **已一致**；随机、特殊发牌、抓分、单双扣、完整结算和回放仍为 **发现遗漏/待实现** |
| RFC/决策 | RFC-0410、D-026、D-044、D-060 |
| 备注 | 参考目录只读未修改；本切片只写入 GSR、测试和文档。 |

### 4.37 NHSK 当前墩抓分与 TurnEnd 线序

| 字段 | 内容 |
|---|---|
| 切片 | 迁移参考 `DoOutCard/advanceTurnAfterOut/endCurrentRound` 的当前墩状态：累计出牌中的 5、10、K，三家过牌结束一墩，向赢家归属分值并发送 `TurnEnd` |
| GSR 文件/测试 | `examples/nhsk/battle.go`、`battle_test.go`、`card_rules.go`、`card_rules_test.go`；`outputs.go` 与既有 `legacy_egress.go`/TurnEnd codec |
| 参考入口 | `nhsk/game/flow_core.go:DoOutCard` 的 `PassCount/PreOutCardSeat/ScoreCards` 更新与 `endCurrentRound`；`nhsk/game/helpers.go:awardCurrentScoreCardsToPreOutSeat`；`nhsk/logic/logic.go:GetScoreCard`；`nhsk/game/messages.go:SendMsgTurnEnd` |
| 参考测试/配置/录包 | `nhsk/logic/logic_test.go:TestGetScoreCard/TestXuRuoxuanEightCardBombAndScoreCards`；`nhsk/game/game_flow_test.go` 的出牌/场景分牌事实；既有 Legacy `0x7605` TurnEnd golden |
| Legacy MessageID | `0x7605 NHSK_TURN_END`；本切片不新增 MessageID，仍通过既有 `OutputTurnEnd` egress |
| 输入与校验 | 合法非过牌的牌面按低位 5=5 分、10/K=10 分累积；过牌保留当前领先牌和分牌；三家实际过牌，加上已出完/退出座位的跳过等价条件达到一墩结束；首出不能过和牌型/压牌校验不变 |
| 权威状态变化 | Battle Mailbox 独占 `preOutSeat`、`passCount`、`scoreCards`、四座 `capturedPoints`、本墩 `lastPlayedCards/lastPlayCounts`；结束时一次性计算当前分牌、累加赢家累计抓分，然后清空本墩状态；不修改综合结算 Score |
| Timer/Timeline | TurnEnd 不创建额外 Timer；结束后复用现有唯一回合期限入口并提交下一 Ask，旧期限继续由 TurnRevision fencing |
| 输出目标与顺序 | 当前墩结束严格为 `OUT_CARD_INFO -> TURN_END -> ASK_OUT_CARD`；TurnEnd Targets 使用当前未退出玩家，Winner 可不在 Targets；GameScene 同步暴露当前墩分牌、PreviousPlayerSeat、LastPlayedCards、CapturedPoints |
| 生命周期结果 | 普通出牌不提前结束小局；对家组均出完仍进入 AwaitingSettlement，最终 GameResult/0x8650/回放未在本切片接入；赢家已出完时下一墩由固定对家优先领出 |
| 已一致 | 5/10/K 分牌值、PassCount/PreOutCardSeat 语义、赢家归属、出完赢家由对家领出、TurnEnd 位置和场景临时状态与参考源码/测试一致 |
| 有意偏差 | 当前只实现每墩抓分与客户端 TurnEnd，不实现参考的 Replay CurrentPoint/CatchPoint、名次/单双扣、综合结算分数、AI/托管统计；累计抓分只保存在 Battle/Scene，不写入最终 GameResult |
| 发现遗漏 | 参考还在终局前后将累计抓分投影到 GameResult、回放和 GAME_OVER；这些需要完整结算/回放权威来源后再接入，不能在当前切片伪造 |
| 结论 | 当前墩抓分、清墩和 `TurnEnd` 线序 **已一致**；最终计分、名次、单双扣、GameResult 和回放仍为 **发现遗漏/待实现** |
| RFC/决策 | RFC-0410、D-058、D-059、D-081 |
| 备注 | 参考目录只读未修改；本切片只写入 GSR、测试和文档。 |

### 4.38 NHSK Battle 独立随机源、庄家轮转与普通洗牌发牌

| 字段 | 内容 |
|---|---|
| 切片 | 为普通 NHSK 发牌接入 Battle 独占 PRNG：创建时从 `crypto/rand` 取得种子；每小局抽取一次庄家、洗牌一次标准 104 张牌组，并从庄家座位开始环形分发 |
| GSR 文件/测试 | `examples/nhsk/battle.go`、`examples/nhsk/battle_test.go`；`docs/TODO.md` CARD-024/CARD-031；`examples/nhsk/README.md` |
| 参考入口 | `nhsk/game/interface.go:ResetForNextGame`；`nhsk/game/flow_core.go:DoGameStart/DoDeal/dealCardsFromRules`；`nhsk/logic/logic.go:RandCardList/FullCardData`；`nhsk/macros/macros.go:FullCount/CardCount/MaxPlayerNum` |
| Legacy MessageID | 本切片不新增 MessageID；原有 `GAME_START (0x7205)`、`NHSK_DEAL (0x7602)` 的输出边界保持不变，发牌事实仍由同一 Battle 产生 |
| 输入与校验 | `StartSubgame` 仅在四座完整且非 Exited 时抽取 `0..3` 庄家；随机源返回越界值时稳定拒绝且不进入 Playing。标准牌组固定为两副四花色 `1..13`，总计 104 张，每座 26 张；不接受全局随机或时间降级 |
| 权威状态变化 | `NHSKBattleService` 持有 `NHSKRandomSource`；`NewBattleService` 在未注入时用 `crypto/rand` 生成私有 `math/rand.Rand`，随机读取失败直接返回创建错误。`StartSubgame` 只调用一次 `Intn(4)`，`deal` 只调用一次 `Shuffle`，以 `(BankerSeat+offset)%4` 分配四份手牌；activeSeat 与庄家一致 |
| Timer/Timeline | 无新增 Timer/Timeline；Clock 注入和时间判断仍按 CARD-024 后续切片处理 |
| 输出目标与顺序 | 不改变既有 GameStart、私有 Deal、AskOutCard 输出顺序；本切片只改变 Deal 的牌序与首出座位，不把随机 seed 写入旧回放 XML 或客户端 payload |
| 生命周期结果 | 随机源失败时 Battle 不创建；单局随机源由该 Battle 独占，下一小局重新抽取庄家并重新洗牌；不复制参考 `StartGame`/`DoGameStart` 连续两次 Reset 对随机源的额外消耗 |
| 已一致 | 参考的标准 104 张牌、多副牌允许重复物理牌、每座 26 张、庄家一次抽取、从庄家环形发牌和普通洗牌路径已由源码/配置复核并锁定为测试；同一固定 seed 的庄家、完整四手牌和牌面多重集合可复现 |
| 有意偏差 | 参考 `nhsk/logic/logic.go` 使用包级全局 `math/rand`，新实现改为 Battle 独占随机源，理由见 D-051/CARD-024；参考连续 Reset 造成二次随机庄家，新实现只在本小局初始化一次，理由见 D-058。新实现当前只接入普通洗牌，不提前搬入全局随机、Nacos 偏置或外部数据源 |
| 发现遗漏 | 参考普通路径还会执行 `SwapSingleCard` 散牌调整；`IsNewbie` 路径会执行 `RandCardListByNewPlayer`；自定义牌堆可覆盖牌序。它们分别留在 CARD-037、CARD-036、CARD-022，Clock 注入留在 CARD-024，不能在本切片伪造 |
| 结论 | Battle 独立随机、crypto/rand 创建失败、标准洗牌、单次庄家和环形发牌 **已一致/按 RFC 有意偏差实现**；散牌、新手、自定义牌堆与 Clock **发现遗漏/待后续切片** |
| RFC/决策 | RFC-0410、RFC-0500、D-051、D-058；TODO CARD-022、CARD-024、CARD-036、CARD-037 |
| 备注 | 参考目录只读未修改；`.codegraph/` 仅作分析元数据，未写入业务源码。 |

### 4.39 NHSK Battle-owned Clock 与期限读取

| 字段 | 内容 |
|---|---|
| 切片 | 将 Battle 的期限起点、剩余时间和到期边界从 Runtime 服务时钟/直接系统时间收拢到可注入的 `NHSKClock` |
| GSR 文件/测试 | `examples/nhsk/battle.go`、`examples/nhsk/battle_test.go`；`docs/TODO.md` CARD-024；`examples/nhsk/README.md` |
| 参考入口 | `nhsk/game/flow_core.go:DoAskOutCard` 的 `LastActionAt=time.Now()`、`StartTimer`；`nhsk/game/timers.go:OnTimer/onTimerOutCard`；`nhsk/game/interface.go` 的 `MsFirstOutCard/MsOutCard/MsOutCardRobot` 默认值和 `StopAllTimers` |
| Legacy MessageID | 本切片不新增 MessageID；既有 `ASK_OUT_CARD (0x7603)` 的 `SecRemain` 仍由同一 Battle 输出，单位和线序不变 |
| 输入与校验 | `NHSKBattleConfig.Clock` 可注入实现 `Now() time.Time`；nil 时只由私有 `systemNHSKClock` 使用系统时间。`StartSubgame`、新回合和托管期限用该 Clock 计算 deadline；deadline 前剩余毫秒向下取整，达到或超过 deadline 返回 0 |
| 权威状态变化 | Battle 仍在 Mailbox 内写入 `deadlineAt`；Clock 只提供只读时间事实，不持有 Battle 状态、不创建 Timer、不绕过 `ServiceContext.After`。Timer 仍由 Runtime 投递 `nhskBattleTimerCommand`，业务 Handler 决定是否自动出牌 |
| Timer/Timeline | 没有新增 Timer；现有唯一期限和 `turnRevision` fencing 不变。注入 Clock 不改变 Timer 的真实调度，只使 deadline 计算和场景剩余时间可复现 |
| 输出目标与顺序 | 不改变 GameStart、AskOutCard、Reconnect/Scene 恢复或托管输出；只替换期限计算的时间来源，Legacy adapter 不编码 Clock 或测试时间 |
| 生命周期结果 | Battle 初始化获得完整 Clock 依赖；生产默认 Clock 保持现有墙上时间行为，测试可推进 fake Clock 验证起点、500ms 边界和到期零值。回放开始/结束时间、诊断导出和其他未来时间事实仍未接入 |
| 已一致 | 旧实现的出牌期限由 `MsFirstOutCard/MsOutCard` 启动 Timer、期限事件进入统一 Timer Handler；新实现仍由 Battle 设置唯一 deadline、通过 Runtime Timer 投递 Command，并保持既有输出/VerifyCode fencing。Clock 所有读取集中到 Battle owner，满足 D-051 的可复现时间 seam |
| 有意偏差 | 参考直接调用 `time.Now()` 保存 `LastActionAt`，并由独立 timer manager 查询剩余；新实现不复制该全局/多 Timer 结构，改用 Battle-owned Clock + Runtime `After`，理由见 D-051 与 RFC-0410 Timeline 契约。生产 Clock 的默认 provider 仍返回系统时间，不增加新的外部时间服务 |
| 发现遗漏 | 参考的首次出牌/普通出牌/机器人/AI 专用期限、Timer 响应矩阵和回放操作耗时仍未完整接入；本切片只使当前期限依赖可注入、可测试，不宣称完成 CARD-033 或回放时间字段 |
| 结论 | Battle-owned Clock、期限起点、剩余毫秒和到期边界 **已按 RFC 实现**；完整配置化期限、专用 Timer、回放时间事实 **发现遗漏/待后续切片** |
| RFC/决策 | RFC-0410、RFC-0320、RFC-0500、D-025、D-051、D-075、D-080；TODO CARD-024、CARD-033、CARD-048、CARD-053 |
| 备注 | 参考目录只读未修改；`.codegraph/` 已同步且状态为 up to date。 |

### 4.40 NHSK INIT_GAME 规则 suffix 归一化与期限配置

| 字段 | 内容 |
|---|---|
| 切片 | 补齐旧 `INIT_GAME` 连续 `BaseRule/GameRule/MatchName/RoundUniCode` suffix 解码，并把可达 NHSK 规则投影为 Battle 初始化时冻结的 `NHSKConfig` |
| GSR 文件/测试 | `examples/nhsk/internal/legacywire/control.go`、`control_test.go`、`examples/nhsk/rules.go`、`rules_test.go`、`commands.go`、`legacy_control_mapper.go`、`battle.go`；Legacy mapper 与 Battle 规则/期限回归 |
| 参考入口 | `protocol/gamelogic/gm2gl.go:ReqGM2GLBodyInitGame.FormatFromTcp`；`protocol/gamelogic/tcpprotocol/gm2gl.go:BS_GM2GL_INIT_GAME`；`nhsk/game/interface.go:NewGame/SetGameRule`；`gamelogic/internal/game/game_config.go:applyBaseRuleValue` |
| 参考测试/配置/录包 | `nhsk/game/game_flow_test.go:TestSetGameRuleLoadsRobotThresholdsAndBiasedFlags`；旧默认 `MsFirstOutCard/MsOutCard=10000`、`MsOutCardRobot/MsAITimeout=0`、`SingleCountToSwap=4`；BaseRule index 1/6/22 |
| Legacy MessageID | `0x8600 INIT_GAME`；本切片不新增消息，原有 `0x7603 ASK_OUT_CARD` 继续使用同一输出边界 |
| 输入与校验 | 固定体为 144 字节；BaseRule/GameRule/MatchName/RoundUniCode suffix 必须连续、无尾部字节；历史未填三项 suffix 的本地 fixture 视为空值。坏边界拒绝整个 INIT。规则值按逗号和首个 `;` 归一化，坏值/缺失保留默认，多余字段忽略 |
| 权威状态变化 | `InitializeBattle` 只在首次成功时冻结 `NHSKConfig`；Battle 后续期限和 GameInfo 读取该副本。Raw 规则不进入 Battle 状态，也不建立 GM-owned 配置字段 |
| Timer/Timeline | 没有新增 Timer；已有 Runtime `After` 使用配置化首出/普通出牌期限，`NHSKClock` 继续提供 deadline 起点 |
| 输出目标与顺序 | `ASK_OUT_CARD` 与 `GameInfo.OutCardSeconds` 继续原线序；期限默认从示例硬编码 15 秒改为参考默认 10 秒。Legacy INIT 当前提供旧规则字段，期限覆盖也可由 Cluster 直接传入 `NHSKConfig` |
| 生命周期结果 | 直接 Cluster 初始化未提供 Rules 时使用 `DefaultNHSKConfig`；Legacy INIT 提供已归一化配置；重复相同 INIT no-op，冲突 INIT 仍按既有身份冲突拒绝 |
| 已一致 | suffix 固定布局、GameRule 前两项/第四项解析、BaseRule index 1/6/22、旧默认值及 Battle 内冻结后再消费均与参考证据一致 |
| 有意偏差 | 不把 `BiasedShuffling`、展示延迟、复盘开关、AI 地址和其他 GM-owned 索引带入 NHSKConfig；不保留原始规则全文，仅保留类型化配置，遵循 CARD-046/047 |
| 发现遗漏 | `MinRobotOutCardCount/Ratio` 与 `SingleCountToSwap` 已归一化但尚未改变发牌/托管算法；首次/机器人/AI 多期限矩阵、回放规则快照和诊断摘要仍待 CARD-033/036/037/052 |
| 结论 | `INIT_GAME` 规则 suffix 解码、默认归一化和可达期限消费 **已实现**；未消费规则与完整散牌/托管语义 **发现遗漏/后续切片** |
| RFC/决策 | RFC-0410；TODO CARD-025、CARD-033、CARD-034、CARD-036、CARD-037、CARD-046、CARD-047 |
| 备注 | 参考目录只读未修改；本切片只写入 GSR、测试和文档。 |

### 4.41 NHSK 普通发牌 SingleCountToSwap 散牌调整

| 字段 | 内容 |
|---|---|
| 切片 | 将旧 GameRule 第四项 `SingleCountToSwap` 真正接入普通随机发牌；默认值为 4，非正值关闭。 |
| GSR 文件/测试 | `examples/nhsk/battle.go`、`examples/nhsk/battle_test.go`、`examples/nhsk/single_count_swap_test.go`；旧算法交换顺序、关闭开关、固定 seed 四手牌 golden、总牌多重集合和每座 26 张回归。 |
| 参考入口 | `nhsk/game/flow_core.go:dealCardsFromRules` 的普通路径；`nhsk/logic/logic.go:SwapSingleCard`、`GetAllSingleCards`、`valueCount`、`findCardIndex`。 |
| 参考测试/配置/录包 | `nhsk/logic/logic_test.go:TestSwapSingleCardReducesExcessiveSingles`；`nhsk/game/interface.go` 默认 `SingleCountToSwap=4`，`SetGameRule` 第四项可覆盖。 |
| Legacy MessageID | 不新增 MessageID；仍由 `0x7602 NHSK_DEAL` 输出同一批最终手牌。 |
| 输入与校验 | `NHSKConfig.SingleCountToSwap` 已在 INIT/Cluster 边界归一化；普通 104 张洗牌牌组按 4 座×26 张处理。阈值 `<=0` 或牌组边界不足时不交换；交换只按牌面值，保留双副物理牌字节和总量。 |
| 权威状态变化 | Battle 在 Mailbox 内洗牌后、庄家环形切片前对扁平牌组执行旧座位顺序算法；不新增全局随机、Nacos、外部 I/O 或 Runtime 状态。最终手牌仍只保存在 Battle，并供客户端 Deal/Scene 共用。 |
| Timer/Timeline | 无新增 Timer/Timeline。 |
| 输出目标与顺序 | 不改变 `GAME_START -> GAME_STARTED -> GameInfo -> Deal -> AskOutCard` 线序、目标或私有可见性；仅替换普通 Deal 的 104 张最终牌序。旧回放不新增 `SingleCountSwap` 字段。 |
| 生命周期结果 | 每小局普通洗牌后执行一次调整；新手、自定义牌堆路径仍按既定优先级绕过本算法，尚未接入。固定 seed=1 锁定四座完整结果。 |
| 已一致 | 普通路径默认阈值、按座位依次处理、寻找本座已有牌面值并与其他座位候选交换、后续座位可能再次影响前座、四座数量和 104 张双副牌多重集合均与参考一致。 |
| 有意偏差 | 参考 helper 依赖旧 `Logic` 的包级全局随机，但该 helper 实际没有随机调用；新实现使用 Battle 内部牌面值 helper，保持顺序且不引入全局状态，符合 D-051。普通发牌已接入，但不提前实现新手/自定义分支。 |
| 发现遗漏 | 新手 `RandCardListByNewPlayer`、自定义牌堆覆盖和完整回放/结算仍待 CARD-036/CARD-022 等切片；它们不能由本切片推断为已完成。 |
| 结论 | 普通 `SwapSingleCard` 散牌调整 **已一致/按既有边界实现**；新手、自定义牌堆和最终结算 **发现遗漏/后续切片**。 |
| RFC/决策 | RFC-0410、RFC-0500、D-051、D-064、D-079；TODO CARD-025、CARD-031、CARD-036、CARD-037。 |
| 备注 | 已回查 `nhsk`、`gamelogic`、`gamemaster`、`gamecore`、`protocol`、`baison_middle/protocol`、`nbgame_core`；参考目录只读未修改，未写入业务源码。 |

### 4.42 NHSK NEW_GAME.IsNewbie 新手发牌调整

| 字段 | 内容 |
|---|---|
| 切片 | 将旧 `NEW_GAME.IsNewbie` 从 Host 创建请求传入 Battle，并在无自定义牌堆的普通牌组上复刻旧新手换牌路径。 |
| GSR 文件/测试 | `examples/nhsk/commands.go`、`battle.go`、`host.go`、`battle_test.go`、`single_count_swap_test.go`；覆盖算法 fallback、首个非自动玩家和全自动玩家跳过。 |
| 参考入口 | `nhsk/game/flow_core.go:dealCardsFromRules`；`nhsk/logic/logic.go:RandCardListByNewPlayer`；`nhsk/game/game_base_api.go:isNewbie/isRobot`；`gamelogic/app/handler/game.go:ReqNewGame`。 |
| 参考测试/配置/录包 | `nhsk/logic/logic_test.go:TestRandCardListByNewPlayerImprovesTargetSingles`；旧 `TPLAYERINFO.IsAI` 与 GameAPI `IsRobot` 作为自动玩家来源；GM `NEW_GAME` 的 `IsNewbie` 进入 RoundInitConfig。 |
| Legacy MessageID | 不新增 MessageID；保留 `NEW_GAME (0x86c1)` 的 `IsNewbie` 位，最终仍由 `0x7602 NHSK_DEAL` 输出最终手牌。 |
| 输入与校验 | `CreateBattleRequest.IsNewbie` 是每个 Battle 的不可变创建事实；`UPDATE_PLAYER.IsAI` 归一化为 `BattlePlayer.Automated`。新手路径按 `SeatID 0..3` 找首个非自动且已入座玩家；四座全自动时不调整。自定义牌堆尚未实现，因此本切片不新增 provider 或外部配置。 |
| 权威状态变化 | Factory 创建 `NHSKBattleService` 时冻结 `IsNewbie`；Battle 洗牌后在 Mailbox 内执行一次旧三张阈值调整，若目标座位的单牌数相对开局变多，再按四张阈值重试。算法可能交换四家牌，最终手牌仍由 Battle 唯一持有。 |
| Timer/Timeline | 无新增 Timer/Timeline。 |
| 输出目标与顺序 | 不改变 `GAME_START -> GAME_STARTED -> GameInfo -> Deal -> AskOutCard` 线序、目标或可见性；只改变普通牌组的最终牌序。新手标记不进入客户端、回放 XML 或通用 Runtime。 |
| 生命周期结果 | `IsNewbie=true` 的每小局都在普通洗牌后执行该路径；`IsNewbie=false` 继续执行 `SingleCountToSwap` 普通散牌调整；全自动新手局保持纯洗牌。固定 seed 测试锁定按首个非自动座位选择的四手结果。 |
| 已一致 | `NEW_GAME.IsNewbie` 可达入口、`IsAI`/自动玩家判定、首个非机器人选择、三张后四张重试、全局四座交换副作用和全自动跳过均与参考一致。 |
| 有意偏差 | 参考新手路径经 `RandCardListByBiased` 与 Nacos/偏置配置耦合；新实现按 D-063 去掉无生产证据的动态偏置，只保留可达的新手换牌结果。自定义牌堆优先级待 provider 切片实现。 |
| 发现遗漏 | 自定义牌堆装载/白名单/庄家覆盖、完整回放与最终结算仍待后续切片；本切片不把普通 `SingleCountToSwap` 同时应用到新手路径。 |
| 结论 | 新手标记传递、首个非自动选择、旧三张/四张调整和全自动旁路 **已一致/按既有决策实现**；自定义牌堆优先级 **发现遗漏/后续切片**。 |
| RFC/决策 | RFC-0410、RFC-0500、D-051、D-063、D-064、D-072；TODO CARD-022、CARD-025、CARD-031、CARD-036。 |
| 备注 | 已回查 `nhsk`、`gamelogic`、`gamemaster`、`gamecore`、`protocol`、`baison_middle/protocol`、`nbgame_core`；参考目录只读未修改，未写入业务源码。 |

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
