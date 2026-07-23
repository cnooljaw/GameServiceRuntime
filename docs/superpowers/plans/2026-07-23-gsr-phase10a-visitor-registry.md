# Phase 10A：Visitor lease Registry 实施计划

> 状态：进行中
> 更新：2026-07-23
> 权威契约：[RFC-0270](../../rfcs/RFC-0270-Tooling-Drain-Hot-Reload.md)

## 目标

完成一个独立的访问者关系纵向切片：`VisitorRegistryService` 通过 Command 保存有 owner、AuthorityEpoch、Generation 和过期时间的 Target/Visitor lease；调用方只能通过类型化 Client 操作自己的 lease。它为后续 Drain 提供可查询的强、弱访问者事实，但不决定或执行下线。

## 固定边界

- 新增 `tooling/drain`；Core、Discovery 和 ServiceGroup 不反向引用它。
- Registry 是唯一状态 owner。关系、Generation 和 Timer 调度状态都只在它的 Mailbox Handler 中修改。
- `Visitor` 就是 lease owner。Acquire、Renew、Release 必须精确匹配 `CommandContext.Source()`；节点级 caller 只能查询，不能修改。
- 重 Acquire 生成新 Generation；Renew 保持 Generation、用新的过期时间 fence 迟到 Release；过期与旧 AuthorityEpoch 都返回稳定错误。
- Timer 只投递私有 sweep Command。创建首个 Timer 失败时 Acquire 不提交状态；空 Registry 不保留 Timer。
- Client、Codec 和 Service 不创建 goroutine；公开查询和成功响应均返回独立副本。
- 本切片不加入 Drain guard、ServiceGroup 版本发布、`Runtime.Stop`、Desired State、Controller、Reconcile、NodeAgent 动作或外部 Admin API。

## 切片 1：冻结契约与计划

1. 将 `docs/rfcs/RFC-0270-Tooling-Drain-Hot-Reload.md` 收窄为 Phase 10A，并冻结公开类型、Command ID、source fencing、过期、Timer 和 Codec 行为。
2. 同步 `docs/rfcs/RFC-0000-Foundation-Glossary.md`、`docs/DECISIONS.md`、`docs/rfcs/RFC-0500-Roadmap.md`、`docs/TODO.md` 和 `README.md` 的阶段状态。
3. 新增本计划；后续 Drain/Controller 必须使用新的独立契约，不能扩展 Registry 为调度器。
4. 检查：`go test ./runtime -run RFC -count=1`。
5. 提交：`docs(drain): 冻结 Phase 10A 访问者租约契约`。

## 切片 2：本地 lease 事实

1. 先新增 `tooling/drain/visitor_test.go` 的失败测试，覆盖 Acquire/List 深复制、强弱关系、重 Acquire Generation fencing、Renew 后迟到 Release、source owner 和校验错误。
2. 新增 `types.go`、`errors.go`、`commands.go`、`validation.go`、`copy.go` 和 `client.go`，使 Client 的参数与返回值在 Call 前后都经过校验。
3. 新增 `service.go`，在 Handler 内实现 map、epoch、Generation、Timer 排程、过期清理和 metrics；以现有 `tooling/discovery` 的 lease 语义为模板，但不引用 Discovery 类型。
4. 检查：`go test ./tooling/drain -run 'Acquire|Renew|Release|List|Owner|Lease' -count=100`。
5. 提交：`feat(drain): 增加访问者租约目录`。

## 切片 3：Codec 与双节点验收

1. 先新增 `tooling/drain/codec_test.go` 的失败测试，覆盖公开 payload、fallback、私有 sweep、畸形 JSON、尾随 JSON、类型错误和无效成功响应。
2. 新增 `codec.go`，只处理四个公开 Command，其他 Command 委托 fallback；不让 Codec 参与 Timer、身份或连接管理。
3. 新增 `remote_test.go`，让远端 Service caller 依次 Acquire、Renew、List、Release，并确认 Registry 领域错误跨 TCP 仍可识别。
4. 检查：`go test ./tooling/drain -count=100` 和 `go test -race ./tooling/drain -count=20`。
5. 提交：`feat(drain): 增加访问者租约远程编解码`。

## 切片 4：阶段验收与文档回填

1. 将 RFC-0270、路线图、TODO 和 README 从“待实现”回填为实际 Phase 10A 边界；补充一个只演示 Service caller 的 `examples/drain-runtime`，不伪装为完整 Drain。
2. 运行 `go test ./...`、`go vet ./...`、`go test -race ./...` 和 `go run ./examples/drain-runtime`。
3. 复核 `rg -n 'tooling/drain' runtime tooling/discovery tooling/servicegroup` 的依赖方向，并复查 RFC、公开 Go doc 和示例状态一致。
4. 提交：`docs(drain): 完成 Phase 10A 访问者租约验收`。

## 最终验收

```bash
go test ./...
go vet ./...
go test -race ./...
go run ./examples/drain-runtime
```

验收后，实际业务可让长期持有旧 Service 的依赖登记强或弱 lease，并让后续 Drain 编排可靠等待强访问者清零。本切片不解决流量切换、拒绝新请求、状态迁移或服务停止；这些仍需要下一份 Drain / Controller 契约。
