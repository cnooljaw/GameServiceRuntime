# RFC-0340：PlayerService 设计

> 状态：待实现
> 目标阶段：Phase 12
> 范围：Business Layer
> 依赖：[RFC-0210](RFC-0210-Tooling-Snapshot.md)、[RFC-0290](RFC-0290-Tooling-LoginService-Gateway.md)、[RFC-0300](RFC-0300-Business-Layering.md)、[RFC-0350](RFC-0350-Business-PlayerModule.md)
> 依据：连接可达性与玩家长期权威状态必须分离

## 目的

PlayerService 是单个玩家的长期业务状态 owner。它接收经 Gateway/ProtocolMapper 映射的身份上下文，维护在线可达性、当前 Room/Battle Ref 和模块状态；它不认证用户、不直接持有 socket，也不成为 Wallet 的副本。

## 目标

- 定义 Player 创建、上线/离线、当前 Battle 绑定、查询和重连快照请求。
- 用 `PlayerModule` 在同一个 Player Mailbox 内扩展玩家领域状态。
- 所有跨 Service 加入/离开/结算使用 RequestID 或由对端回传结果 Command，避免共享 user lock。

## 非目标

- 不提供 token 校验、proof、连接读写、跨节点会话存储、数据库 schema、自动重连策略或 Wallet ledger。
- 不把在线状态等同于登录认证成功，也不在 Player Handler 中同步 Call Battle 以拼接跨 Service 原子状态。

## 分层与依赖

```text
Gateway Adapter -> ProtocolMapper -> PlayerService owns PlayerState + modules
PlayerService --reconnect request--> BattleService
Battle snapshot result Command --> PlayerService / Gateway response mapper
```

Gateway 只把已验证的 `SessionIdentity{Player, Account}` 放入业务 payload；PlayerState 的唯一可写者是 PlayerService。Module 不独立注册为 Service，除非它拥有可独立扩缩/持久化的权威状态。

## 公开契约

包路径为 `game`：

```go
type SessionIdentity struct { Player PlayerID; Account AccountID }
type PlayerState struct {
    Player PlayerID
    Account AccountID
    Online bool
    Room   gsr.ServiceRef
    Battle gsr.ServiceRef
}
type PlayerSnapshot struct { State PlayerState; Modules map[string][]byte }
type PlayerConfig struct {
    Identity SessionIdentity
    Modules  []PlayerModule
}
func NewPlayerService(PlayerConfig) (*PlayerService, error)
```

保留 CommandID：

```text
0x03000401 SetPlayerOnline
0x03000402 SetPlayerOffline
0x03000403 SetPlayerRoom
0x03000404 SetPlayerBattle
0x03000405 GetPlayerSnapshot
0x03000406 ApplyPlayerReconnectSnapshot
0x03000407 BackupPlayer
```

上线/离线 payload 必须带匹配 PlayerID 的 `SessionIdentity` 和会话 Generation（由 application 定义的单调字符串/整数）；迟到的 Offline 必须同时匹配当前 Generation，不能把新连接标离线。SetRoom/SetBattle 使用 `{RequestID, Ref}`；零 Ref 表示清除绑定。模块 Command 由 Module.Commands 声明，不能与保留 ID 或其他模块冲突。

## 状态与生命周期

PlayerService Init 固定 Identity 与按名称排序的 Module。Online 仅更新当前 Generation 和可达性，并向模块广播显式 PlayerEvent；Offline 只在 Generation 匹配时更新。SetBattle/SetRoom 的同 RequestID 相同输入返回已有结果；不同输入冲突。Reconnect 请求不改 Battle 状态：Player 发送带 PlayerID、Epoch（若已知）和 RequestID 的查询 Command，收到结果后以 ApplyPlayerReconnectSnapshot 保存最后可见投影供 Gateway 获取。

BackupPlayer 只使 Module 生成独立 Snapshot bytes 或向外部 writer 发送请求；它不直接在 Handler 做无界数据库 I/O。Stop/Close 只清理 Service 生命周期，不自动标记玩家结算完成或删除 Wallet 记录。

## 错误与失败语义

身份不匹配、无效 generation、未知/关闭 Battle、重复模块名、模块命令冲突、RequestID 冲突及无效 Ref 为稳定错误。Battle 请求 Send 失败可被记录为 pending；Call/网络超时后等待 Apply 结果或显式查询，不能假定 Battle 未处理。模块 Handle 错误不部分提交 PlayerState：实现必须先验证并仅在模块成功后写可见状态，或将失败状态显式记录为业务终态。

## 并发与所有权

PlayerState、模块状态和最后重连投影只在 Player Mailbox 改写。模块只能经 PlayerContext 访问当前 Player；不得持有其他 Module/Service 指针、保存 Context、创建 goroutine或直接访问 Repository。所有 Snapshot、state 和 bytes 必须复制。

## 可观测性

Snapshot 包含 PlayerID、online generation、Room/Battle Ref 与每模块匿名 bytes；日志/Record 记录 RequestID、模块名、阶段和错误类别，不记录 ticket/proof/secret。持久化进度由外部 writer 的结果 Command 观察。

## 验收

- 新/旧连接 generation、重复上线/离线、Module command 冲突、绑定 RequestID、Battle 结果迟到和返回副本均覆盖测试。
- Gateway 不直接写 PlayerState；Player 不直接改 Battle/Wallet；Player/Module 无 goroutine 与无直接持久化 I/O。
