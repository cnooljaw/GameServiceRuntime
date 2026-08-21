# AGENTS.md

## Source Of Truth

- 设计和公开 API 以 `docs/rfcs/` 为准，阅读顺序见 `docs/SUMMARY.md`。
- 需要追溯“为什么这样设计”时，先查 `docs/DECISIONS.md`，再阅读其中链接的完整 RFC；决策索引只用于检索，不是第二份契约。
- RFC 未说明时，先按 Skynet 的设计规则裁决；仍不明确再检查 Skynet 源码实现，并把结论写回 RFC。
- 代码与 RFC 冲突时，先修改 RFC 并说明裁决，再修改代码。
- 聊天记录、`AGENTS.md` 和 Skill 不是公开契约。形成可复用的设计结论时，先更新或新增 RFC，再更新决策索引、路线图和实现。
- Core Runtime 不得引用 `game/`、`examples/` 或业务领域类型。

## Working Mode

- 当前采用单人、边实现边学习的工作方式：先完成可验证的纵向切片，再结合 RFC、Skynet 设计思想和模块边界审查实现是否优雅；不要以聊天结论替代源码、测试和文档复核。
- 默认直接在本地 `main` 提交；除非用户明确要求，不创建分支、PR 或远端操作。
- 进入新 Phase 前，先审核目标 RFC、相邻 RFC、已有代码和 `RFC-0500`/`docs/TODO.md`；发现契约缺口时先写清裁决，再实现。
- 实现宁海双扣示例时必须遵守 GSR 的 Service、Command、Mailbox、Timer、生命周期和 adapter 边界，不照搬旧容器结构。每完成一个可验证功能切片，都要回查 `nhsk`、`gamelogic`、`gamemaster`、`gamecore`、`protocol`、`nbgame_core` 中对应入口及测试，记录“已一致、有意偏差、发现遗漏”三类结论。有意偏差必须链接既有 RFC；发现未裁决的偏差或遗漏时先停下实现并更新 RFC。参考目录的原业务代码、配置和资源不修改；`.codegraph/` 等分析工具元数据可以写入。
- 每个 Phase 收尾都要说明：这次能力在实际业务中的作用、明确不解决什么，以及下一阶段为什么仍需要它。

## Go Rules

- 使用 Go 1.23.3 和标准库；新增第三方依赖必须先说明必要性。
- 每个可见行为先写失败测试，再写最小实现。
- 导出符号必须有 Go doc；未导出结构保持包内私有。
- 提交前运行 `go test ./...`、`go vet ./...` 和 `go test -race ./...`。

## Runtime Rules

- 只使用 `Service`、`ServiceRef`、`Command`、`Send`、`Call` 命名；禁止 `Actor`、`Spawn`、`Ask`、`Tell`、`PTYPE`。
- 业务状态变化只能通过 Command 进入 Mailbox handler；`Stop`、`Close` 只做 Runtime 串行调度的清理。
- Service 之间只能通过 `ServiceRef` 和 Command 通信，不持有另一个 Service 指针。
- Service 实现不得直接创建 goroutine；外部阻塞工作使用 Core Runner 的 `Await` 或 `Submit`，纯调度使用 Command、Timer 或独立 Service，直接 `go` 语句由 AST 测试检查。Runtime 创建的执行任务必须追踪到真实返回。
- 生产代码中的直接 `go` 默认禁止。只有 Runtime 内部任务、Transport/连接 Adapter 的 I/O owner、或 Core Runner 无法表达且经 RFC 裁决的固定外部任务可以例外；例外必须由明确生命周期 owner 持有，具备取消或关闭入口、并在 `Close` 中等待真实返回。它不得直接修改 Service 状态，也不得保存或在 Handler 外使用 `CommandContext`、`ServiceContext`。新增例外先更新 RFC 并补关闭、超时和泄漏测试。
- Timer 只能投递 Command，不能执行业务回调。
- `Session` 只关联 Call/Reply；业务幂等使用 `RequestID`。
- `Runtime.Inspect()` 是 Core 唯一只读观测入口；Metrics 快照只通过 `Inspect().Metrics` 获取。

## Skills

- 实现或评审 Runtime 时，先读取 `skills/gsr-runtime/SKILL.md`。
- 修改 RFC 或 README 时使用 `technical-writing`。
- 设计 Service、adapter、模块边界时使用 `deep-module-design`。
- 实现计划前使用 `create-plan`；创建或修改项目 Skill 时使用 `skill-creator`。
- 代码评审使用 `two-axis-code-review`。
- 依赖或影响分析优先使用已初始化的 CodeGraph，并以源码和测试复核图查询结果。

## Git

- 每个通过测试的垂直切片单独提交，提交信息使用中文。
- 不提交 `.DS_Store`、二进制构建产物或本地密钥。
