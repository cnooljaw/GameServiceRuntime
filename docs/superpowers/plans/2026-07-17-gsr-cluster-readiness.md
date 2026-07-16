# GSR Cluster 前工程门禁实施计划

> 状态：执行中

**目标：** 完成 `docs/TODO.md` 中 `CF-005` 至 `CF-008`，在开发 Cluster Data Plane 前固定 Session 安全性、测试结构、Service 并发规则和持续集成门禁。

**范围：** 只修改 Core 测试和小范围私有实现、工程检查及 CI 配置。不实现 Transport、远程 Send/Call、Discovery 或业务能力，不增加 Go 第三方依赖。

## Task 1：保护 SessionID 分配

**文件：** 修改 `docs/rfcs/RFC-0140-Core-Session-PendingCall.md`、`runtime/call.go`、`runtime/pending_test.go`。

1. 先增加 `TestPendingCallAllocationSkipsZeroAndActiveCollision`，把计数器放到 `math.MaxUint64`，并预占回绕后的 Session `1`。
2. 运行：

   ```bash
   go test ./runtime -run 'TestPendingCallAllocation' -count=1
   ```

   预期：当前实现分配 `0` 或覆盖活动 Session，测试失败。
3. 将 Session 分配改为循环：原子递增，跳过 `0`，在 PendingCall 锁内确认 ID 未占用后再登记。`uint64` 空间不现实地全部占满时继续寻找，不增加公开耗尽错误。
4. 在 RFC 中明确 `0` 保留给无 Session 的 Send，回绕不能覆盖活动调用。
5. 运行目标测试 100 次及 race，然后提交：`fix(runtime): 保护 Session 回绕与活动调用`。

## Task 2：拆分一致性测试

**文件：** 将 `runtime/conformance_test.go` 拆为：

- `runtime/scheduler_conformance_test.go`
- `runtime/lifecycle_conformance_test.go`
- `runtime/call_conformance_test.go`
- `runtime/observability_conformance_test.go`
- `runtime/command_registry_conformance_test.go`
- `runtime/conformance_fixtures_test.go`

1. 只移动测试和 fixture，不改断言、等待时间或生产代码。
2. 每个测试文件只导入实际使用的包；共享 Service fixture 集中放在 fixture 文件。
3. 删除原 1000 行混合文件，确认没有测试名称丢失或重复。
4. 运行：

   ```bash
   go test ./runtime -count=1
   go test -race ./runtime -count=1
   ```

5. 提交：`test(runtime): 按职责拆分一致性测试`。

## Task 3：检查 Service 裸 goroutine

**文件：** 新增 `runtime/service_goroutine_policy_test.go`；修改 `AGENTS.md`、`skills/gsr-runtime/SKILL.md`。

1. 使用 `go/parser` 和 `go/ast` 扫描仓库内非测试 `.go` 文件，按 receiver 汇总方法。
2. 只有同时具备 `Init`、`Handle`、`Stop`、`Close` 的类型才判定为 Service 实现；检查该类型所有方法中的 `go` 语句。
3. 增加内存源码测试，证明违规 Service 被发现、只有 `Close` 方法的 Runtime 类型不会误报。
4. 项目扫描排除 `.git`、`vendor`、隐藏目录和生成文件，不扫描 `_test.go`。
5. AGENTS 只补充“该规则由 AST 工程检查执行”；详细检测边界放入项目 Skill。
6. 运行测试及 race，然后提交：`test(policy): 禁止 Service 创建裸 goroutine`。

## Task 4：建立 CI 门禁

**文件：** 新增 `.github/workflows/ci.yml`。

1. 使用 `actions/checkout@v4`、`actions/setup-go@v5` 和 `go.mod` 中的 Go 版本。
2. 在 `ubuntu-latest` 依次运行：

   ```bash
   go test ./...
   go vet ./...
   go test -race ./...
   go run ./examples/local-runtime
   ```

3. 工作流触发 `push` 和 `pull_request`，不增加发布、写权限或外部服务。
4. 本地复跑同样命令，提交：`ci: 增加 Go 质量门禁`。

## Task 5：关闭 P2 待办

**文件：** 修改 `docs/TODO.md`、`docs/rfcs/RFC-0500-Roadmap.md` 和本计划。

1. 将 `CF-005` 至 `CF-008` 标记为已完成，更新路线图为“可以开始 Phase 5 Cluster Data Plane”。
2. 将本计划状态改为已完成。
3. 执行：

   ```bash
   go test ./...
   go vet ./...
   go test -race ./...
   go run ./examples/local-runtime
   git diff --check
   ```

4. 提交：`docs: 完成 Cluster 前工程门禁`。
