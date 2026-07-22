# Phase 7F：客户端入口实施计划

> 状态：已完成
> 依据：[RFC-0290](../../rfcs/RFC-0290-Tooling-LoginService-Gateway.md)
> 范围：`tooling/entry`、TCP 示例、文档验收

## 目标

实现一个可复验的最小客户端入口：Login Adapter 通过可替换 Handshake 获得已认证身份和 secret，LoginService 在 Mailbox 中签发带 Generation 的 ticket，Gateway Adapter 用固定 proof 行验证并绑定连接，Business Layer 的 ProtocolMapper 将已认证业务行映射为 `Send` 或 `Call`。

首版只实现 `SingleSession`、内存 SessionRegistry 和 TCP 行协议。账号体系、生产握手、WebSocket、持久化会话、跨节点 registry 和多端策略不在本阶段。

## 边界

- 新建 `tooling/entry`。它依赖 `runtime`，Core 不导入它。
- `loginService` 只持有策略状态；`SessionRegistry` 用私有锁封装 secret、ticket、proof 序号和连接绑定。
- Login/Gateway Adapter 是组合根启动和关闭的非 Service 连接 IO owner；Service 代码不得创建 goroutine。
- `ProtocolMapper` 由示例业务实现，Tooling 只依赖它的窄接口。
- secret 只进入 Handshake、Registry 和 HMAC 计算；测试断言错误与日志均不打印 secret。

## 切片 1：会话材料与 proof

文件：

- 新增 `tooling/entry/types.go`、`errors.go`、`validation.go`、`proof.go`、`registry.go`。
- 新增 `tooling/entry/registry_test.go`、`proof_test.go`。

先写失败测试：无效 identity/secret、容量满、ticket 过期、HMAC 字段篡改、重复或倒退 Sequence、并发 VerifyAndBind、连接替换和迟到 Unbind。

实现 `InMemorySessionRegistry`，使 `Issue`、`VerifyAndBind`、`Unbind` 和 `Revoke` 在同一私有同步边界内完成。proof 输入和 `AUTH` 解析严格遵循 RFC 的 `GSR-Gateway-Proof-v1`。

验证：

```bash
go test ./tooling/entry -run 'Registry|Proof' -count=100
go test -race ./tooling/entry -run 'Registry|Proof' -count=20
```

提交：`feat(入口): 增加会话注册表和 proof 校验`。

## 切片 2：LoginService

文件：

- 新增 `tooling/entry/commands.go`、`service.go`、`client.go`。
- 新增 `tooling/entry/service_test.go`、`client_test.go`。

先写失败测试：payload 形状错误、Registry 失败不返回 ticket、同账号同 server 新登录撤销旧 Generation、UID/SubID 不可预测、Service 关闭后的 Client 错误。

实现私有 `loginService` 和类型化 Client。`IssueTicket` Command 只传 identity、SecretRef 和过期时间，绝不传 secret；成功 Reply 后 ticket 已经写入 Registry。运行 Runtime AST goroutine 门禁确保 Service 没有直接 `go`。

验证：

```bash
go test ./tooling/entry -run 'LoginService|Client' -count=100
go test ./runtime -run ServiceImplementation -count=1
```

提交：`feat(入口): 增加登录票据服务`。

## 切片 3：TCP Adapter 与业务映射

文件：

- 新增 `tooling/entry/login_adapter.go`、`gateway_adapter.go`、`line_codec.go`。
- 新增 `tooling/entry/login_adapter_test.go`、`gateway_adapter_test.go`。

先写失败测试：Handshake 失败、Registry/Call 失败不写成功 ticket、无效 AUTH 行不调用 mapper、proof 重放不投递 Command、连接替换关闭旧连接、mapper 返回非法 Route、Adapter Close 等待 accept 与连接任务真实返回。

实现：

- Login Adapter 监听 TCP，调用 `Handshake.Accept`，复制并登记 secret，再通过 LoginService Client 签发 ticket。
- Gateway Adapter 读取一行 `AUTH`，调用 Registry 原子绑定；随后把长度受限的业务行交给 ProtocolMapper 并使用窄 Runtime Send/Call 接口投递。
- Adapter 使用显式 WaitGroup 与 Close，连接任务真实返回后才完成关闭。

验证：

```bash
go test ./tooling/entry -run 'LoginAdapter|GatewayAdapter' -count=30
go test -race ./tooling/entry -count=10
```

提交：`feat(入口): 打通 TCP 登录与网关入口`。

## 切片 4：示例、文档和验收

文件：

- 新增 `examples/client-entry/main.go`。
- 新增 `docs/GSR-Book/04-第四篇-基础设施/05-客户端入口.md`，并更新 `docs/GSR-Book/SUMMARY.md`。
- 更新 `README.md`、`CHANGELOG.md`、`docs/TODO.md`、`docs/rfcs/RFC-0290-Tooling-LoginService-Gateway.md`、`docs/rfcs/RFC-0500-Roadmap.md` 和本计划。

示例必须输出已认证 Command 的 `SessionIdentity`，并明确使用测试 Handshake，不能被误作公网安全实现。RFC 审查与实现一致后改为“已接受”并写入接受日期。

验证：

```bash
go test ./...
go vet ./...
go test -race ./...
go run ./examples/local-runtime
go run ./examples/cluster-runtime
go run ./examples/discovery-runtime
go run ./examples/monitor-runtime
go run ./examples/snapshot-runtime
go run ./examples/supervisor-runtime
go run ./examples/client-entry
```

提交：`docs(入口): 完成 Phase 7F 验收`。

## 审查清单

- RFC 所有公开符号、proof 输入和错误分类都由测试覆盖。
- 所有 secret 副本在不再需要时清零；任何错误、日志和业务 Command 均无 secret。
- Core 依赖图不包含 `tooling/entry`，业务 mapper 不持有 Service 指针。
- SessionRegistry 的对外快照为副本；任何迟到清理都以 connection ID 和 Generation fencing。
- Adapter 关闭不会提前报告完成，也不会泄漏 listener 或连接 goroutine。
