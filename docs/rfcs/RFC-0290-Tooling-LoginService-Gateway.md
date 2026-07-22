# RFC-0290：LoginService 与 Gateway 入口

> 状态：草案
> 目标阶段：Phase 7F
> 范围：Runtime Tooling、客户端入口
> 依赖：[RFC-0100](RFC-0100-Core-Service.md)、[RFC-0130](RFC-0130-Core-Send-Call-Reply.md)
> 依据：Skynet `examples/login/client.lua`、`examples/login/logind.lua`、`examples/login/gated.lua`、`lualib/snax/loginserver.lua`

## 目的

本文定义 GSR 客户端登录入口的标准分工。

结论：

```text
Login Adapter 负责登录连接、challenge 和 secret 交换。
LoginService 负责认证状态与票据编排。
Gateway Adapter 负责游戏连接验证、绑定、收发包。
ProtocolMapper 负责把已认证业务包映射为 Command。
Core Runtime 不知道登录、socket、token、secret。
```

## Skynet 的参考标准

Skynet `examples/login` 的关键分工是：

```text
Client
  ↓ 连接登录端口
snax.loginserver / logind
  ↓ 完成 challenge、DH secret、HMAC、token 校验
  ↓ 调用 login_handler(server, uid, secret)
gated / msgserver
  ↓ 注册 uid、subid、secret
Client
  ↓ 连接游戏端口，使用 secret 计算 HMAC
gated / msgserver
  ↓ 验证登录证明并绑定连接
msgagent
  ↓ 处理已认证玩家请求
```

这个例子的重点不是具体加密算法，而是职责切分：

- `LoginService` 完成密钥交换和账号认证。
- `Gateway` 不重新交换 `secret`，只验证客户端持有同一个 `secret`。
- `Agent` 处理已认证会话，不负责登录握手。

GSR 保留“登录入口、游戏网关、已认证业务处理”三段职责，并进一步把连接 IO 留在 adapter：Login Adapter 承担 Skynet 登录服务里的网络握手部分，LoginService 只承担可由 Command 驱动的认证状态和票据编排。

如果业务仍然引入 `Agent` 或 `PlayerSessionService`，它只能处理已认证会话和玩家请求，不能承接登录握手、账号认证或 `secret` 交换。

## GSR 分层

```text
Business Layer
  ├── AuthProvider
  ├── ProtocolMapper
  └── PlayerService / RoomService / BattleService

Runtime Tooling
  ├── Login Adapter
  ├── LoginService
  ├── Gateway Adapter
  └── SessionRegistry

Core Runtime
  ├── Service
  ├── Command
  ├── Envelope
  └── Send / Call
```

`LoginService` 属于 Runtime Tooling。它提供通用登录状态和票据编排，但不监听 socket、不创建 goroutine，也不写死账号库、平台 SDK、渠道规则。登录连接和密钥协商由 Login Adapter 持有；adapter 把已验证的握手结果映射为 LoginService Command。

账号校验由业务提供 `AuthProvider`：

```go
type AuthProvider interface {
    VerifyLoginToken(ctx context.Context, token LoginToken) (AuthIdentity, error)
}
```

`AuthProvider` 属于 Business Layer 或业务 adapter。可能阻塞的远程认证不得直接占用 LoginService Handler；应由独立 AuthService 通过 Call 承担，或在进入 Runtime 前由 Login Adapter 完成后再投递认证结果 Command。

## 登录流程

第一版推荐流程：

```text
Client
  ↓ 1. connect Login Adapter
Login Adapter
  ↓ 2. challenge
Client
  ↓ 3. client key
Login Adapter
  ↓ 4. server key
Client / Login Adapter
  ↓ 5. derive secret
Client
  ↓ 6. HMAC(challenge, secret)
Client
  ↓ 7. encrypted login token
Login Adapter
  ↓ 8. AuthProvider/AuthService verifies token and SessionRegistry stores secret
LoginService
  ↓ 9. apply login policy and issue LoginTicket(uid, subid, SecretRef, expiresAt)
Client
  ↓ 10. connect Gateway with uid/subid/proof
Gateway Adapter
  ↓ 11. verify proof through SessionRegistry/LoginService
Gateway Adapter
  ↓ 12. bind connection to SessionIdentity
ProtocolMapper
  ↓ 13. map packet to Command
Runtime
  ↓ 14. Send / Call target Service
```

`secret` 的交换只发生在第 1 到第 8 步。第 9 步以后只传递 `SecretRef`，第 10 步以后只验证客户端知道对应 `secret`。

## 核心对象

```go
type LoginToken struct {
    Server string
    Raw    []byte
}

type AuthIdentity struct {
    AccountID string
    PlayerID  string
    Server    string
}

type LoginTicket struct {
    UID       string
    SubID     string
    SecretRef SecretRef
    ExpiresAt time.Time
}

type SessionIdentity struct {
    UID      string
    SubID    string
    PlayerID string
    Server   string
}
```

`SecretRef` 是密钥引用，不是明文密钥。明文 `secret` 只能存在于 Login Adapter、SessionRegistry 或 Gateway Adapter 的受控内存中。

## Login Adapter 责任

Login Adapter 负责：

- 监听登录连接并维护连接级超时。
- 生成 challenge，完成密钥协商和 HMAC 校验。
- 解密或解析登录 token。
- 调用 Runtime 外的 `AuthProvider`，或通过 Runtime Call 调用独立 AuthService。
- 把明文 `secret` 存入受控 SessionRegistry，并只把 `SecretRef` 和认证身份投递给 LoginService。
- 把 LoginService 签发的票据写回客户端。

Login Adapter 不持有玩家、房间、对局或钱包权威状态，也不把明文 `secret` 放进普通业务 Command。

## LoginService 责任

`LoginService` 负责：

- 接收 Login Adapter 已完成握手和账号校验的认证 Command。
- 编排 token 校验结果、重复登录策略和票据状态。
- 处理单点登录、多端登录或顶号策略。
- 签发 `LoginTicket`。
- 关联 Login Adapter 提供的 `SecretRef` 与票据。
- 返回 `subid` 或等价登录凭证给客户端。

`LoginService` 不负责：

- 监听连接、读写 socket 或维护网络 goroutine。
- 生成网络 challenge、执行连接级密钥协商或解析帧。
- 解析游戏业务包。
- 分发 `CmdJoinRoom`、`CmdMatch`、`CmdSettlement`。
- 持有玩家权威状态。
- 直接操作 Room、Battle、Wallet 的内部状态。

## Gateway Adapter 责任

`Gateway Adapter` 负责：

- 维护长连接。
- 解析包头、长度、序号和基础帧格式。
- 校验客户端登录证明。
- 绑定连接与 `SessionIdentity`。
- 处理断线、重连、踢下线。
- 限流、超时和基础安全检查。
- 将已认证包交给 `ProtocolMapper`。
- 将 Service 返回结果写回客户端。

`Gateway Adapter` 不负责：

- 交换 `secret`。
- 校验账号密码或平台 token。
- 理解业务命令语义。
- 持有玩家金币、房间、对局等权威状态。

## ProtocolMapper 责任

`ProtocolMapper` 只接收已认证上下文：

```text
SessionIdentity + ClientPacket
  ↓
CommandID + Payload
```

它可以知道客户端协议号和业务命令之间的映射，例如：

```text
1001 -> CmdJoinRoom
1002 -> CmdLeaveRoom
2001 -> CmdPlayerAction
```

但它不做登录握手，也不保存连接。

## 安全规则

1. 明文 `secret` 不进入普通业务 `Command`。
2. 明文 `secret` 不写入日志、快照、录制回放和错误返回。
3. `LoginTicket` 必须有过期时间。
4. Gateway 绑定连接时必须校验 `uid`、`subid`、`server` 和 proof。
5. 重连必须带递增序号或 nonce，避免旧 proof 被重复使用。
6. 顶号、禁止多端、多端共存属于 LoginService 策略，不属于 Core Runtime。
7. 断线清理由 Gateway 触发，但玩家权威状态由 `PlayerService`、`RoomService` 或 `BattleService` 决定。
8. Login Adapter 和 Gateway Adapter 拥有连接 IO；任何 Runtime Service 都不得直接创建网络 goroutine。

## 错误模型

第一版使用稳定错误码：

| 错误 | 含义 |
|-|-|
| `ErrUnauthorized` | token 无效、账号校验失败。 |
| `ErrForbidden` | 登录成功前置校验通过，但业务拒绝进入目标 server。 |
| `ErrDuplicateLogin` | 当前策略不允许重复登录。 |
| `ErrInvalidProof` | Gateway 登录证明校验失败。 |
| `ErrTicketExpired` | 登录票据过期。 |

错误码可以映射到客户端协议，但 Core Runtime 不知道这些错误。

## 与 Skynet 的取舍

保留：

- 登录服务和游戏网关分离。
- `secret` 在登录阶段交换，游戏连接阶段只验证。
- 登录服务处理重复登录策略。
- Gateway 只绑定连接和转发已认证请求。

不照搬：

- 不暴露 `PTYPE_CLIENT`。
- 不要求 Agent 使用 Skynet `msgserver` 模型。
- 不把 `secret` 作为普通业务参数到处传递。
- 不把登录协议写进 Core Runtime。

## 实现建议

第一版可以先实现接口和内存版本：

```text
InMemorySessionRegistry
LoginAdapter
SimpleLoginService
TCPGatewayAdapter 或 WebSocketGatewayAdapter
ProtocolMapper
```

验收目标：

```text
Client 登录成功
  ↓
Gateway 验证 proof
  ↓
ProtocolMapper 生成 Command
  ↓
PlayerService 收到带 SessionIdentity 的 Command
```

加密算法可以先采用标准库或成熟第三方库，不要求复刻 Skynet 的 DH/DES 细节。职责边界必须与本文一致。
