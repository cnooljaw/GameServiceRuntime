# Phase 8：节点观测与 Heartbeat 实施计划

> 状态：已完成
> 更新：2026-07-23
> 权威契约：[RFC-0250](../../rfcs/RFC-0250-Tooling-Cluster-Control-Plane.md)

## 目标

完成可信集群内的节点事实闭环：`NodeAgentService` 在进入 `Running` 后自动注册并续租自己的 Discovery lease；`ClusterObserverService` 以静态 `NodeConfig` 查询受限的 Agent 报告并缓存 `Observed State`。它不包含 Service 期望状态、Controller 或任何修改型运维。

## 固定边界

- Core 仅新增通用 `StartupCommandDeclarer`：启动后的异步工作仍由 Command 进入 Mailbox，Core 不认识 Discovery 或 Control。
- NodeAgent 只持有 `ServiceContext`、Discovery `ServiceRef` 和自己的 lease；不持有其它 Service 指针，不创建 goroutine。
- Discovery lease 的 NodeID 一律取 `ServiceContext.Self().Node`；部署配置只提供地址和 Discovery `ServiceRef`。
- `NodeConfig` 是静态观测目标，不是 Desired State。Service Desired State、Controller、Reconcile 和 NodeAgent 动作留到 Phase 10。
- 每次 Discovery Call 都使用配置的有限 `CallTimeout`。注册失败或普通 Heartbeat 失败在下一次间隔重试；`ErrLeaseExpired` 在同一 Command 中重新注册。

## 切片 1：冻结文档与启动 Command 契约

1. 修改 `docs/rfcs/RFC-0100-Core-Service.md`、`RFC-0180-Core-Lifecycle.md`，冻结 `StartupCommandDeclarer` 的时序、Source、失败和 Stop 语义。
2. 重写 `docs/rfcs/RFC-0250-Tooling-Cluster-Control-Plane.md` 的阶段边界和 NodeAgent lease 契约；同步术语表、路线图、TODO 与本计划。
3. 检查：`rg -n "Phase 8A|ClusterControlService|ControlNode|静态 Desired State" docs` 只保留历史记录中的明确引用。
4. 提交：`docs(control): 冻结 Phase 8 心跳契约`。

## 切片 2：Core 启动 Command

1. 在 `runtime/lifecycle_init_test.go` 先写失败测试：启动 Command 在 `Running` 后进入 Mailbox、Source 为 Self、未声明 Command 拒绝创建、Stop/Close 不会额外执行它。
2. 在 `runtime/service.go` 增加导出接口和 Go doc；在 `runtime/runtime.go` 于状态切换后投递声明 Command。
3. 更新 `runtime` 的 API/生命周期测试和必要文档断言。
4. 检查：`go test ./runtime -run 'Startup|Lifecycle' -count=100` 与 `go test -race ./runtime -run 'Startup|Lifecycle' -count=20`。
5. 提交：`feat(runtime): 支持启动 Command`。

## 切片 3：NodeAgent 自动租约

1. 在 `tooling/control/node_agent_test.go` 先写失败测试：创建后自动 Register、定时 Heartbeat、租约失效后重新 Register、临时失败按间隔重试、Stop 最多注销当前 lease 一次。
2. 扩展 `NodeAgentConfig`，加入 Discovery `ServiceRef`、节点地址、HeartbeatInterval 与 CallTimeout；零值使用 RFC 默认值，非法配置返回 `ErrInvalidConfig`。
3. 让 NodeAgent 通过 `StartupCommandDeclarer` 把注册和续租放入私有 Command；所有状态只在 Handler 改变，Timer 只投递下一次 Command。
4. 更新 `tooling/control/commands.go` 与 Codec，确保私有 lease Command 不可由远程 Codec 编解码。
5. 检查：`go test ./tooling/control -run NodeAgent -count=100` 与 `go test -race ./tooling/control -run NodeAgent -count=20`。
6. 提交：`feat(control): NodeAgent 自动续租 Discovery`。

## 切片 4：完成只读节点观测

1. 在 `tooling/control/service_test.go`、`client_test.go` 先写 List、Detail、Refresh、Disabled、超时和无效远端回复测试。
2. 用 `ClusterObserverService` 的 Mailbox 保存冻结 NodeTarget、Observed State 和报告副本；Client 只暴露类型化 Call。
3. 完成 Codec 的本地与双节点 TCP 测试，保持 Discovery Codec 可组合。
4. 添加最小 `examples/control-runtime`，只展示两节点启动、Agent heartbeat 和只读刷新。
5. 检查：`go test ./tooling/control -count=100`、`go test -race ./tooling/control -count=20`。
6. 提交：`feat(control): 完成节点观测服务`。

## 最终验收

```bash
go test ./...
go vet ./...
go test -race ./...
go run ./examples/control-runtime
```

完成后把 RFC-0250 标为“已接受”，路线图和 CHANGELOG 写入 Phase 8 实际边界；不在同一提交加入 Controller、Desired State 或 NodeAgent 修改型命令。
