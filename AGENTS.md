# AGENTS.md

## Source Of Truth

- 设计和公开 API 以 `docs/rfcs/` 为准，阅读顺序见 `docs/SUMMARY.md`。
- RFC 未说明时，先按 Skynet 的设计规则裁决；仍不明确再检查 Skynet 源码实现，并把结论写回 RFC。
- 代码与 RFC 冲突时，先修改 RFC 并说明裁决，再修改代码。
- Core Runtime 不得引用 `game/`、`examples/` 或业务领域类型。

## Go Rules

- 使用 Go 1.23.3 和标准库；新增第三方依赖必须先说明必要性。
- 每个可见行为先写失败测试，再写最小实现。
- 导出符号必须有 Go doc；未导出结构保持包内私有。
- 提交前运行 `go test ./...`、`go vet ./...` 和 `go test -race ./...`。

## Runtime Rules

- 只使用 `Service`、`ServiceRef`、`Command`、`Send`、`Call` 命名；禁止 `Actor`、`Spawn`、`Ask`、`Tell`、`PTYPE`。
- 业务状态变化只能通过 Command 进入 Mailbox handler；`Stop`、`Close` 只做 Runtime 串行调度的清理。
- Service 之间只能通过 `ServiceRef` 和 Command 通信，不持有另一个 Service 指针。
- Service 实现不得直接创建 goroutine；异步工作使用 Command、Timer 或独立 Service，直接 `go` 语句由 AST 测试检查。Runtime 创建的执行任务必须追踪到真实返回。
- Timer 只能投递 Command，不能执行业务回调。
- `Session` 只关联 Call/Reply；业务幂等使用 `RequestID`。

## Skills

- 实现或评审 Runtime 时，先读取 `skills/gsr-runtime/SKILL.md`。
- 修改 RFC 或 README 时使用 `technical-writing`。
- 设计 Service、adapter、模块边界时使用 `deep-module-design`。
- 实现计划前使用 `create-plan`；创建或修改项目 Skill 时使用 `skill-creator`。
- 代码评审使用 `clean-coder-review` 或 `two-axis-code-review`。

## Git

- 每个通过测试的垂直切片单独提交，提交信息使用中文。
- 不提交 `.DS_Store`、二进制构建产物或本地密钥。
