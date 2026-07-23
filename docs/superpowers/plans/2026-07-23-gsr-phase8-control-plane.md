# Phase 8A：Cluster Control Plane 实施计划

> 状态：执行中
> 依据：[RFC-0250](../../rfcs/RFC-0250-Tooling-Cluster-Control-Plane.md)
> 范围：tooling/control、双节点 TCP 示例、文档验收

## 目标

实现可信集群内的只读控制面：NodeAgent 返回本地 Monitor 报告，ClusterControl 保存静态 Desired State 并按 Command 刷新单节点 Observed State。第一版没有外部 Admin API 和修改型命令。

## 边界

- 新建 tooling/control，可依赖 tooling/monitor 与 runtime；Core 不导入 Tooling。
- NodeAgent 和 ClusterControl 都是普通 Service；全部可变状态通过 Mailbox Handler 修改，Service 不创建 goroutine。
- Agent 只接受 ControlNode 中 ID 非零的 Service 来源；这不是外部用户认证。
- 组合根提供稳定名字解析后的 Agent ServiceRef；ControlService 不在 Handler 内直接使用 Runtime.ResolveRemote 或执行配置 I/O。
- 远端 Agent 失败是目标节点的新观测状态，不是对其它节点的全局失败。

## 切片 1：冻结契约与计划

文件：

- 修改 docs/rfcs/RFC-0250-Tooling-Cluster-Control-Plane.md。
- 新增本计划。
- 修改 docs/rfcs/RFC-0500-Roadmap.md 与 docs/TODO.md，标记 Phase 8A 执行中。

步骤：

1. 将 RFC 状态改为“待实现”，补齐目标、非目标、公开 API、Source 检查、缓存、副本、失败、观测与验收。
2. 裁决只读 Agent 先行；把认证、授权、审计和高危运维命令明确留后。
3. 提交：docs(control): 冻结 Phase 8A 只读控制面契约。

验证：

~~~bash
go test ./runtime -run TestRFCMetadataPolicy -count=10
git diff --check
~~~

预期：RFC 元数据和 Markdown 差异检查通过。

## 切片 2：类型、错误与 Codec

文件：

- 新增 tooling/control/types.go、errors.go、commands.go、validation.go、codec.go。
- 新增 tooling/control/codec_test.go、validation_test.go。

步骤：

1. 先写失败测试：nil 动态接口、重复 NodeID、非法 Agent Ref、Disabled 节点、错误 payload 类型、无 fallback、尾随 JSON 和无效成功 response。
2. 定义公开 Desired/Observed/Detail/Config/Client seams 与私有 request/response；所有 JSON 字段使用固定 snake_case。
3. 实现按 CommandID 分派的标准库 JSON Codec；其它 Command 委托 fallback，内部结构只在成功 response 后校验。
4. 提交：feat(control): 定义只读控制面协议。

验证：

~~~bash
go test ./tooling/control -run 'Codec|Validation' -count=50
go test -race ./tooling/control -run 'Codec|Validation' -count=10
~~~

预期：所有错误路径返回稳定 sentinel，Codec 不接受尾随内容。

## 切片 3：NodeAgentService

文件：

- 新增 tooling/control/node_agent.go、node_agent_test.go。

步骤：

1. 先写失败测试：非 ControlNode、节点级 Source、错误 payload 均不调用 Reporter；返回 Report 被调用方修改后不影响下一次 Capture。
2. 实现只读 Agent，只在通过 Source 检查后调用 Reporter 一次，并以领域 response Reply。
3. 在 AST goroutine 门禁和并发测试中确认 Agent 没有后台任务。
4. 提交：feat(control): 增加只读节点代理服务。

验证：

~~~bash
go test ./tooling/control -run NodeAgent -count=100
go test -race ./tooling/control -run NodeAgent -count=20
go test ./runtime -run ServiceImplementation -count=1
~~~

预期：只有配置 ControlService 可读取本地报告，所有返回值独立。

## 切片 4：ClusterControlService 与 Client

文件：

- 新增 tooling/control/service.go、client.go、service_test.go、client_test.go。

步骤：

1. 先写失败测试：初始状态、排序、Disabled、未知节点、Get 无网络 I/O、成功刷新、超时/断线/坏 response 仅更新目标节点、缓存副本和关闭竞争。
2. 实现单节点刷新；使用 ServiceContext.Call 与配置 timeout，并把 Agent 基础设施失败归一为稳定 Observed State。
3. 写入 RFC 指定 Metrics；不改变 Core Metrics API。
4. 提交：feat(control): 汇总节点期望与观测状态。

验证：

~~~bash
go test ./tooling/control -run 'Control|Client' -count=100
go test -race ./tooling/control -run 'Control|Client' -count=20
~~~

预期：刷新严格串行，迟到结果不会覆盖已完成状态。

## 切片 5：远程路径、示例和验收

文件：

- 新增 tooling/control/remote_test.go、examples/control-runtime/main.go。
- 新增 docs/GSR-Book/04-第四篇-基础设施/06-Cluster-Control-Plane.md，并更新 docs/GSR-Book/SUMMARY.md。
- 更新 README.md、CHANGELOG.md、docs/TODO.md、docs/rfcs/RFC-0250-Tooling-Cluster-Control-Plane.md、docs/rfcs/RFC-0500-Roadmap.md 和本计划。

步骤：

1. 用两条真实 TCP Runtime 验证 ControlService 到远端 NodeAgent 的 Call，示例输出 node、status 与 service_count。
2. 验证 Codec 与 Discovery Codec 的 fallback 组合；Core 依赖图不得包含 tooling/control。
3. 完成双轴 Review；所有验收通过后把 RFC 改为“已接受”并写入接受日期。
4. 提交：docs(control): 完成 Phase 8A 验收。

验证：

~~~bash
go test ./...
go vet ./...
go test -race ./...
go run ./examples/control-runtime
git diff --check
~~~

预期：全仓质量门禁通过，示例只展示只读节点详情，不开放修改型管理操作。

## 审查清单

- [ ] RFC 中每个公开类型、Command、状态和错误都有测试。
- [ ] 所有 Report、Detail、slice 和 map 都是独立副本。
- [ ] Agent 的 Source 检查在 Capture 前完成；外部节点级 caller 无法直接读取。
- [ ] 不存在 Service 直接 go、Timer、HTTP、配置 I/O 或第二套 RPC。
- [ ] Core 依赖图不包含 tooling/control。
- [ ] 每一切片通过对应重复与 Race 测试后单独提交。
