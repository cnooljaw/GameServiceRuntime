# RFC-0290：客户端登录与 Gateway 入口

> 状态：已接受
> 目标阶段：Phase 7F
> 接受日期：2026-07-22
> 范围：Runtime Tooling、客户端入口
> 依赖：[RFC-0100](RFC-0100-Core-Service.md)、[RFC-0120](RFC-0120-Core-Command.md)、[RFC-0130](RFC-0130-Core-Send-Call-Reply.md)、[RFC-0140](RFC-0140-Core-Session-PendingCall.md)
> 依据：Skynet `examples/login/client.lua`、`examples/login/logind.lua`、`examples/login/gated.lua`、`lualib/snax/loginserver.lua`

## 目的

本文定义 GSR 的最小客户端入口。它让客户端在登录端完成认证和会话材料建立，再在 Gateway 端证明自己持有同一份密钥，最后把已认证业务包映射为 GSR `Command`。

本文冻结 Phase 7F 的边界、代际、proof 线格式和失败交接。它不定义账号体系、TLS 终止、具体密钥协商算法、业务协议或玩家状态。

```text
Login Adapter
  -> SessionRegistry
  -> LoginService
  -> LoginTicket
  -> Gateway Adapter
  -> ProtocolMapper
  -> Send / Call
  -> Business Service
```

结论：

- Login Adapter 和 Gateway Adapter 是连接 IO owner；它们可以使用受管 goroutine，但不是 GSR Service。
- LoginService 是只经 Mailbox 写入状态的 Runtime Tooling Service；它不监听 socket、不做阻塞认证、不保存明文密钥。
- SessionRegistry 是受控内存中的会话材料与 proof/连接绑定 owner；它向 Adapter 提供原子操作，不是 Core Runtime，也不是业务 Service。
- ProtocolMapper 属于 Business Layer。它只看到已认证 `SessionIdentity` 和客户端包。
- Core Runtime 不知道 token、socket、TLS、secret、proof、ticket、uid、subid 或客户端协议号。

## 与 Skynet 的取舍

Skynet `examples/login` 把登录密钥交换、账号认证、Gateway proof 校验和已认证业务处理分开。GSR 保留该职责边界，但不复制 `PTYPE_*`、`msgserver` 或 Agent 进程模型。

GSR 进一步明确：连接级握手是可替换的 Adapter seam；登录状态、重复登录策略和票据只在 LoginService 的 Mailbox 内变化；业务包只在 Gateway 认证成功后才进入 `ProtocolMapper`。

## 分层与 owner

```text
Business Layer
  AuthProvider              认证业务规则或平台 SDK
  ProtocolMapper            已认证包 -> target + Command
  PlayerService 等          权威业务状态

Runtime Tooling
  Login Adapter             登录连接、握手、认证调用和响应写回
  SessionRegistry           secret、ticket、proof 序号、连接绑定
  LoginService              登录策略和票据签发状态
  Gateway Adapter           Gateway 连接、proof、绑定、收发包

Core Runtime
  Service / ServiceRef / Command / Send / Call
```

`AuthProvider` 可能阻塞，必须在 Login Adapter 的受管连接工作流或独立 AuthService 中执行；LoginService Handler 不得直接调用它。`ProtocolMapper` 不做登录握手，不保存连接，不接收明文密钥。

## 核心对象

公开类型至少具有下列语义；具体 Go 字段以本 RFC 为准。

```go
type SecretRef string

type AuthIdentity struct {
    AccountID string
    PlayerID  string
    Server    string
}

type LoginTicket struct {
    UID        string
    SubID      string
    Server     string
    SecretRef  SecretRef
    Generation uint64
    ExpiresAt  time.Time
}

type SessionIdentity struct {
    UID        string
    SubID      string
    PlayerID   string
    Server     string
    Generation uint64
}
```

`SecretRef` 只是不可预测的受控引用，不能由 `UID`、`SubID`、递增整数或明文密钥推导。明文 `secret` 仅在握手实现、Login Adapter 的短暂局部变量、SessionRegistry 和 Gateway proof 计算中出现。它不能进入普通业务 `Command`、日志、错误、Snapshot 或 Record/Replay。

`Generation` 是 LoginService 为同一逻辑会话签发的单调非零代际。重复登录、显式撤销和新票据覆盖都会使旧代际无效；旧 Gateway 连接不能凭旧 ticket 或旧 proof 重新获得绑定。它与 Core `SessionID` 完全无关。

## 会话状态与原子操作

SessionRegistry 必须把下列状态作为一个受锁保护的记录维护：ticket、受控 secret、最近成功 proof 序号和当前连接绑定。它至少提供以下原子操作：

```text
StoreSecret(identity, secret, expiresAt) -> SecretRef
Replace(ticket, identity, previous) -> replaced ConnectionID
VerifyAndBind(proof, connectionID) -> SessionBinding
Unbind(connectionID, generation) -> void
Revoke(uid, server, generation) -> void
```

`Replace` 只接受先前由 `StoreSecret` 返回的 `SecretRef`，将 ticket 与 identity 绑定，并在同一临界区撤销仍匹配的旧 ticket。`VerifyAndBind` 返回 `SessionBinding`（已认证 `SessionIdentity` 与被替换连接 ID），并必须在同一临界区完成：查找当前 ticket、检查未过期和未撤销、检查 `UID`/`SubID`/`Server`/`Generation`、校验 HMAC、要求 `Sequence` 严格大于已接受序号、记录新序号，并建立或替换该代际的连接绑定。它不得分成“先验证、后绑定”的两个可竞争调用。

同一 ticket 的新连接绑定会替换旧绑定；Gateway 通过由 Registry 返回的被替换 connection ID 主动关闭旧连接。`Unbind` 必须同时匹配 connection ID 与 Generation，迟到断线不能删除新绑定。票据过期、撤销或被新代际覆盖时必须删除 secret、proof 序号和绑定。Registry 清理也必须受 TTL 和最大活动会话数限制；容量满时拒绝新登录，不能驱逐仍有效的任意会话。

## LoginService

LoginService 只接受已经完成握手和认证的 `IssueLoginTicket` Command。它在自己的 Mailbox 中处理：

1. 验证 identity、`SecretRef`、过期时间和策略输入。
2. 根据 `SingleSession`、`AllowMultiple` 或业务自定义策略决定是否允许登录。
3. 为允许的逻辑会话分配新的非零 Generation、不可预测 `UID` 和 `SubID`。
4. 将旧 Generation 标记为撤销，并通过 SessionRegistry 原子写入新 ticket。
5. Reply 一个不含明文 secret 的 `TicketIssue`；其中包含 `LoginTicket` 和被 SingleSession 撤销的旧 Gateway `ConnectionID`。

LoginService 对 Registry 写入失败返回稳定错误且不 Reply 成功 ticket。Login Adapter 仅在收到成功 Reply 后才向客户端写入 ticket；连接中断、认证失败、Registry 满、Call 超时或 LoginService 关闭都不得产生半签发 ticket。若 LoginService 成功 Reply 但 Adapter 写回客户端失败，ticket 保留到过期；客户端可用该 ticket 进入 Gateway，调用方不得猜测它未签发。

第一版提供 `SingleSession` 策略。它按 `AccountID + Server` 维持当前 Generation；新的成功登录撤销旧 ticket，并在 `TicketIssue` 中返回旧绑定的 `ConnectionID`，由必填的 Gateway `ConnectionCloser` 关闭旧连接。多端并存和顶号通知是后续策略扩展，不改变 proof 格式。

## 登录握手 seam

登录协议和账号体系随产品变化，不能写进 Tooling 公共状态机。Login Adapter 依赖窄 `Handshake` seam：

```go
type Handshake interface {
    Accept(context.Context, net.Conn) (VerifiedLogin, error)
}

type VerifiedLogin struct {
    Identity AuthIdentity
    Secret   []byte
    ExpiresAt time.Time
}
```

`Handshake.Accept` 拥有 challenge、密钥协商、HMAC、token 解密和 `AuthProvider` 调用。它返回时必须已经认证 identity，并提供不少于 32 字节、密码学随机或经安全协商得到的 secret。生产实现必须运行在 TLS 或提供等价的认证密钥交换；本 RFC 的测试握手仅用于验证分层，不能作为公网安全协议。

Login Adapter 在成功返回后复制 secret 至 Registry，并尽力清零自己的局部副本。它不得把 `VerifiedLogin` 或 secret 作为 LoginService Command payload。

首版 TCP Login Adapter 成功后写入下列单行响应；其中 `uid`、`subid`、`server` 为无填充 base64url，`generation` 为非零十进制 `uint64`，`expires_unix_ms` 为过期时间的十进制 Unix 毫秒：

```text
TICKET <uid> <subid> <server> <generation> <expires_unix_ms>\n
```

该响应不包含 `SecretRef` 或 secret。失败时只写稳定 `ERR <code>\n`，不写认证或 Registry 的内部 cause。

## Gateway proof 线格式

Phase 7F 固定首版 Gateway 为带长度限制的 UTF-8 单行入口；WebSocket、protobuf 或二进制入口以后可以复用相同 proof message，但不得复用不同的字段语义。

客户端的第一行必须是：

```text
AUTH <uid> <subid> <server> <generation> <sequence> <proof>\n
```

其中：

- `uid`、`subid`、`server`、`proof` 使用无填充 `base64.RawURLEncoding`；解码后的 `uid`、`subid`、`server` 必须是非空合法 UTF-8。
- `generation` 和 `sequence` 是十进制无前导零的非零 `uint64`。
- `proof` 是 32 字节 HMAC-SHA-256 输出的 base64url 编码。
- 整行（含换行）最大 4096 字节；多余字段、空格字段、CR、无换行、非法 UTF-8、超限整数和无效编码全部拒绝。

proof 的输入是下列 UTF-8 字节，字段值使用同一行中的 base64url 文本，结尾必须有一个 `\n`：

```text
GSR-Gateway-Proof-v1\n
uid=<uid>\n
subid=<subid>\n
server=<server>\n
generation=<generation>\n
sequence=<sequence>\n
```

Gateway 以 Registry 中对应 ticket 的明文 secret 计算 `HMAC-SHA-256(secret, proof-input)`，并用常量时间比较。版本前缀、字段顺序和换行不可变；它们阻止不同协议版本或字段拼接规则产生同一签名输入。

`Sequence` 由客户端在同一 ticket 生命周期内严格递增；成功绑定才消耗序号。网络失败后的重试必须使用更大的序号，不能重放旧行。Gateway 仅在 `VerifyAndBind` 成功后才把连接交给 ProtocolMapper。认证失败时写稳定错误并关闭连接，不向 Runtime 发送任何业务 Command。

## Gateway 与 ProtocolMapper

Gateway Adapter 在认证成功后持有连接、读写循环、帧大小、空闲超时、基础限流和断线清理。它不能解释游戏 Command，也不能持有玩家、房间、对局或钱包的权威状态。

ProtocolMapper 是 Business Layer 的窄接口：

```go
type ProtocolMapper interface {
    Map(SessionIdentity, ClientPacket) (Route, error)
}

type Route struct {
    Target gsr.ServiceRef
    Command gsr.CommandID
    Payload any
    Call bool
}
```

Gateway 验证 `Route.Target` 与 `Route.Command` 的基本形状后，用 `Runtime.Send` 或 `Runtime.Call` 投递。它不为业务重试制造 `RequestID`；mapper/业务协议必须显式携带业务幂等键。`Call` 路由的 mapper 必须额外实现 `CallResponseMapper.EncodeCallResult`，由它把 result 编码成一个不含换行的 `ClientPacket`；Gateway 追加 `\n` 后写回客户端。业务 handler 的错误不泄漏 secret 或内部对象。

## 错误与可观测性

Tooling 对外返回稳定分类：`ErrUnauthorized`、`ErrTicketExpired`、`ErrInvalidProof`、`ErrProofReplay`、`ErrDuplicateLogin`、`ErrSessionCapacity` 和 `ErrSessionRevoked`。具体认证、解析、HMAC 或 Registry 内部错误可以保留为 wrapped cause，但客户端响应不得包含 cause。

Adapter 至少记录：登录成功/失败、ticket 签发失败、proof 成功/失败/重放、连接替换、过期清理与 ProtocolMapper 拒绝。记录中不得包含 token、secret、完整 proof 或未脱敏的身份材料。Core Metrics 与 `Runtime.Inspect()` 不因入口适配器新增专用 getter。

## 验收

Phase 7F 必须覆盖以下可重复行为：

1. 成功登录后，Gateway 用正确 proof 绑定连接，ProtocolMapper 生成 Command，目标 Service 收到带 `SessionIdentity` 的 payload。
2. proof 的任一字段、格式、HMAC 或 ticket 过期失败时，Gateway 不投递业务 Command。
3. 同一或更小 Sequence 被拒绝；更大 Sequence 可以重连；迟到 `Unbind` 不能删掉新连接。
4. 新的 SingleSession 登录撤销旧代际，旧 proof 和旧 Gateway 连接不能重新成为当前绑定。
5. Handshake、Registry 或 LoginService 任一步失败时不向客户端签发成功 ticket，且 secret 不进入 Command、日志或测试错误文本。
6. LoginService 不创建 goroutine；Login/Gateway Adapter 的连接任务由 Adapter 生命周期 owner 等待真实返回。
7. TCP 示例和全量 `go test ./...`、`go vet ./...`、`go test -race ./...` 通过。

## 非目标

- 不定义账号密码、平台 SDK、OAuth、TLS 证书或生产密钥协商算法。
- 不在 Core Runtime 增加 socket、token、ticket、SessionIdentity 或客户端协议 API。
- 不实现 WebSocket、HTTP、跨节点 SessionRegistry、持久化会话、全局踢人路由或多端策略。
- 不让 Gateway 直接持有业务 Service 指针或业务权威状态。
