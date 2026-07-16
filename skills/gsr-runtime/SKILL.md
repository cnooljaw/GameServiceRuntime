---
name: gsr-runtime
description: 当实现、评审或修改 GSR Go Service Runtime、Service、Command、Mailbox、Scheduler、Timer、Cluster 边界或相关 RFC 时使用。确保实现遵守 GSR 的 Service 模型和 RFC 约束。
---

# GSR Runtime

1. 先读 `docs/SUMMARY.md`，再读本次修改对应的 RFC。
2. Core Runtime 只理解 Service、ServiceRef、Command、Envelope、Mailbox、Scheduler、Timer、生命周期和 Cluster 数据面。
3. 不引入 Actor、PTYPE、跨 Service 指针、业务领域类型或 Timer 回调改状态。
4. Service 内状态由 Mailbox 串行 handler 写入；跨 Service 使用 Send 或 Call。
5. 先写失败测试；完成后执行 `go test ./...`、`go vet ./...` 和 `go test -race ./...`。
6. 新增导出 API 时，同时更新对应 RFC 或说明 RFC 无需变化的原因。
