# Codex 开发指南

> 状态：当前工程约定

## 本章目标

本章说明如何与 Codex 一起持续演进 GSR，同时保持“先自己完成，再理解和审查实现”的开发节奏。

Codex 用于加快检索、实现、测试和审查；它不能替代对 Service 边界、失败语义和业务价值的理解。每次可见变更都应能回答：它解决什么实际问题，为什么归属这个模块，以及它明确不解决什么。

## 权威信息与设计记忆

阅读顺序如下：

```text
docs/SUMMARY.md
  -> docs/DECISIONS.md（定位“为什么”）
  -> 对应 RFC（公开契约）
  -> 源码与测试（当前实现）
  -> docs/TODO.md / RFC-0500（阶段状态）
```

`docs/DECISIONS.md` 是设计记忆的检索入口，记录关键结论和权威 RFC。它不替代 RFC。聊天记录只用于探索；一旦形成影响架构或 API 的结论，必须先更新 RFC，再更新决策索引。

## 单人开发节奏

当前项目默认直接在本地 `main` 工作，不为日常实现创建分支或 PR。每个通过门禁的纵向切片单独提交，方便回看每一步为何存在。

一次 Phase 按以下顺序推进：

1. 审核目标 RFC、相邻 RFC、现有代码和路线图。契约不足时先裁决并写回 RFC。
2. 用 CodeGraph 定位影响范围，再以源码和测试复核；图只用于导航。
3. 先写可见行为的失败测试，再写最小实现。Service 状态只经 Command 进入 Mailbox。
4. 运行质量门禁和示例，完成一个完整纵向切片后再继续扩展。
5. 以 RFC 契约和设计质量两个维度审查：owner 是否唯一、依赖是否单向、异步任务是否可关闭、错误是否可观测、Core 是否仍与业务解耦。
6. 收尾时说明业务作用、非目标和下一阶段依赖，并同步 RFC、路线图、README、示例或决策索引。

## 重要约束

- 使用 `Service`、`ServiceRef`、`Command`、`Send`、`Call`，不使用 Actor 术语。
- Service 不直接创建 goroutine。异步工作使用 Command、Timer、独立 Service，或由明确 owner 管理的有界外部 worker。
- Timer 只投递 Command。Service 之间只通过 `ServiceRef` 和 Command 通信。
- Core 提供通用能力；Discovery、Directory、Controller、Drain 等策略属于 Tooling；游戏领域状态属于 Business Layer。
- 在 RFC 未说明时，先按 Skynet 设计规则裁决，再检查源码；不能因为 Skynet 有某个机制就把它原样搬入 GSR。

## 阶段结束时的提问

完成一批提交后，优先检查这些问题：

- 这次提交在实际业务中让调用方、部署方或运维方多获得了什么？
- 它是否把未来的 Desired State、自动恢复、健康检查或业务策略过早压入 Core？
- 有没有用隐式缓存、channel、裸 goroutine 或对象指针绕过 Mailbox？
- 是否能通过本地与双节点示例解释真实调用链？
- 哪些能力刻意留给下一 Phase，留下它们的原因是什么？

## 质量门禁

每次准备提交前运行：

```bash
go test ./...
go vet ./...
go test -race ./...
```

并执行本次能力对应的示例。并发、关闭、超时或远端调用有变化时，增加针对性重复测试。完整执行约束以仓库根目录的 `AGENTS.md` 和 `skills/gsr-runtime/SKILL.md` 为准。
