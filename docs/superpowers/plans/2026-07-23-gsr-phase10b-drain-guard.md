# Phase 10B：Drain Guard 实施计划

> 权威契约：[RFC-0271](../../rfcs/RFC-0271-Tooling-Drain-Guard.md)
>
> 前置能力：[RFC-0270](../../rfcs/RFC-0270-Tooling-Drain-Hot-Reload.md) 的 Visitor lease Registry

## 目标

完成一个可以单独验收的旧实例入口闸门：受信任协调 Service 在目标 Service 的 Mailbox 中开始 Drain；随后明确列出的外部 Command 被拒绝，内部清理 Command 继续通过。

## 约束

- Guard 是 `tooling/drain` decorator，不改 Core 生命周期、ServiceSet 或 Directory。
- Begin 只做目标单 Mailbox 状态切换；不可逆，不实现 Resume 或 Stop。
- ServiceGroup 切换、调用超时后的未知提交、回滚、Visitor 等待和 Runtime Stop 都后置到含 `RequestID`、授权、审计和人工恢复记录的控制面操作契约。
- Guard/Client/Codec 不创建 goroutine；所有状态改变都在目标 Service 的 Handler 内完成。

## 实施切片

1. 冻结 `GuardConfig`、`DrainStatus`、控制 Command、精确 source fencing、指标、Codec 和不可逆语义；同步索引、路线图和决策。
2. 先写失败测试：配置校验、Command 合并、Begin 前后流量、内部 Command、来源、幂等时间、指标、Codec 及双节点 TCP。
3. 以 decorator 实现最小状态机，并扩展已有 `drain.NewCodec` 与错误码；不触碰 Visitor Registry 行为。
4. 运行质量门禁，回填 README、Book、TODO、Changelog 和路线图；单独提交代码及验收文档。

## 完成定义

业务组合根现在可以给会直接接收新请求的旧 Service 加上显式入口闸门，保证 Begin 之后缓存旧 `ServiceRef` 也无法继续进入已列出的外部工作。它仍不能切流、等待访问者、恢复旧实例或自动停止 Service；这些能力需要后续受控 Drain 操作。

