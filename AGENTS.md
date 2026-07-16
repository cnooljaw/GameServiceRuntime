# AGENTS.md

## Source Of Truth

- 设计和公开 API 以 `docs/rfcs/` 为准，阅读顺序见 `docs/SUMMARY.md`。
- 代码与 RFC 冲突时，先修改 RFC 并说明裁决，再修改代码。
- Core Runtime 不得引用 `game/`、`examples/` 或业务领域类型。

## Go Rules

- 使用 Go 1.23.3 和标准库；新增第三方依赖必须先说明必要性。
- 每个可见行为先写失败测试，再写最小实现。
- 导出符号必须有 Go doc；未导出结构保持包内私有。
- 提交前运行 `go test ./...`、`go vet ./...` 和 `go test -race ./...`。

## Runtime Rules

- 只使用 `Service`、`ServiceRef`、`Command`、`Send`、`Call` 命名；禁止 `Actor`、`Spawn`、`Ask`、`Tell`、`PTYPE`。
- Service 状态只能在 Mailbox 消费的 handler 内修改。
- Service 之间只能通过 `ServiceRef` 和 Command 通信，不持有另一个 Service 指针。
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
