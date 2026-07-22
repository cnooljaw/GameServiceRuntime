# GSR Discovery 审查修正实施计划

> 状态：已完成（2026-07-22）

**目标：** 在可信集群网络前提下修复 Discovery 的程序状态一致性问题：保留清晰调用来源、稳定启动入口、跨 authority 重启的租约身份，以及不降级的错误和响应语义。

**裁决顺序：** RFC 优先；RFC 未说明时参考 Skynet dispatch 的 `source`、`cluster.query` 的节点级名字查询和框架 Timer 语义。Core 可以修改，但不得导入 Discovery 或业务类型。

## Task 1：更新 RFC

**文件：**

- `docs/rfcs/RFC-0100-Core-Service.md`
- `docs/rfcs/RFC-0110-Core-ServiceRef.md`
- `docs/rfcs/RFC-0130-Core-Send-Call-Reply.md`
- `docs/rfcs/RFC-0190-Core-Cluster-Data-Plane.md`
- `docs/rfcs/RFC-0191-Core-Cluster-Transport.md`
- `docs/rfcs/RFC-0200-Tooling-Discovery.md`
- 本计划

**步骤：**

1. 明确信任集群节点，但不信任错误程序状态；当前不增加 TLS、签名租约或零信任授权。
2. 恢复 `CommandContext.Source()`，定义 Runtime caller 为 `{Node: localNode, ID: 0}`，保证本地与远程一致。
3. 定义 `Runtime.ResolveRemote(ctx, node, name)`：通过保留的节点级 Core Command 查询目标节点本地 Registry，业务 Codec 不处理该 Command。
4. `NodeLease` 增加 `AuthorityEpoch`；Discovery 记录精确 `LeaseOwner`，普通注册要求来源 Node 与注册 Node 相同。
5. 领域错误使用类型化 Reply；Core、Timer、生命周期错误直接从 Handler 返回。
6. 成功 Reply 必须经过语义校验。
7. Timer 规则保持不变，不新增 `ServiceContext.CancelTimer`。

**提交：** `docs(review): 裁决 Discovery 来源与启动语义`

## Task 2：恢复 Command 来源

**文件：**

- `runtime/service.go`
- `runtime/runtime.go`
- `runtime/runtime_test.go`
- `runtime/cluster_test.go`

**失败测试：**

1. 外部本地 Send 和 Call 的 `Source()` 都是 `{Node: localNode, ID: 0}`。
2. Service 发起的本地 Call 暴露该 ServiceRef。
3. 远程 Call 暴露原始远端 Runtime caller。

**实现：**

- `CommandContext` 增加 `Source() ServiceRef`。
- `commandContext.Source` 返回 Envelope Source。
- `Runtime.Send` 显式使用本地 Runtime caller，不再把零值 Source 放入本地 Mailbox。

**验证：**

```bash
go test ./runtime -run 'Source' -count=100
go test -race ./runtime -run 'Source' -count=20
```

**提交：** `feat(runtime): 恢复 Command 调用来源`

## Task 3：实现稳定节点级名字查询

**文件：**

- `runtime/cluster.go`
- `runtime/errors.go`
- `runtime/cluster_test.go`
- `runtime/cluster_error_test.go`

**失败测试：**

1. `ResolveRemote` 通过节点和本地名字返回远端 `ServiceRef`。
2. 查询不存在名字保留 `ErrServiceNotFound`。
3. Bootstrap 请求和响应不调用业务 `ClusterCodec`。
4. 非法空名字、非法目标和伪造响应不能完成 PendingCall。

**实现：**

- `ServiceID(0)` 作为节点级 Core endpoint，只接受私有 Resolve Command 的 Call。
- 请求 payload 使用受限长度的名字字节，响应 payload 只编码 `ServiceID`，NodeID 来自已验证的 responder。
- 请求、响应继续使用 `WireEnvelope`、Session、PendingCall 和稳定 Runtime error。
- 其它发往 `ServiceID(0)` 的消息仍返回 `ErrInvalidClusterEnvelope`。

**验证：**

```bash
go test ./runtime -run 'ResolveRemote|ClusterRejects.*NodeEndpoint' -count=100
go test -race ./runtime -run 'ResolveRemote|ClusterRejects.*NodeEndpoint' -count=20
```

**提交：** `feat(cluster): 增加节点级远程名字查询`

## Task 4：修复租约身份与 owner

**文件：**

- `tooling/discovery/types.go`
- `tooling/discovery/errors.go`
- `tooling/discovery/commands.go`
- `tooling/discovery/client.go`
- `tooling/discovery/service.go`
- `tooling/discovery/node_test.go`
- `tooling/discovery/name_test.go`
- `tooling/discovery/remote_test.go`
- `tooling/discovery/codec_test.go`
- `tooling/discovery/benchmark_test.go`

**失败测试：**

1. 注册来源 Node 与请求 Node 不一致时返回 `ErrLeaseOwnerMismatch`。
2. Heartbeat、UnregisterNode 和名字写操作必须来自原始 `LeaseOwner`。
3. 同 NodeID 的新 owner 重新注册会生成新 Generation，并使旧 owner 失效。
4. 两个 Discovery authority 创建的租约拥有不同非零 `AuthorityEpoch`，旧 authority 的租约不能作用于新 authority。
5. Codec 保持新增 epoch 字段的稳定线格式。

**实现：**

- `NodeLease`、`NodeRecord` 增加 `AuthorityEpoch`。
- `NewService` 使用 `crypto/rand` 生成非零 epoch；Generation 只在该 epoch 内递增。
- 私有节点表同时保存 `NodeRecord` 和 `LeaseOwner`。
- owner 不进入公开 NodeRecord，不作为安全凭据。

**验证：**

```bash
go test ./tooling/discovery -run 'Lease|Owner|Authority|Name' -count=100
go test -race ./tooling/discovery -run 'Lease|Owner|Authority|Name' -count=20
```

**提交：** `fix(discovery): 完整约束租约身份与 owner`

## Task 5：保持错误与成功响应语义

**文件：**

- `tooling/discovery/client.go`
- `tooling/discovery/service.go`
- `tooling/discovery/commands.go`
- `tooling/discovery/node_test.go`
- `tooling/discovery/name_test.go`
- `tooling/discovery/remote_test.go`

**失败测试：**

1. `ServiceContext.After` 返回的 Core 错误从 Handler 直接返回，不变成 `ErrInvalidResponse`。
2. 零 Lease、NodeRecord、ServiceRef 和无效节点列表均返回 `ErrInvalidResponse`。
3. 有领域错误的 Reply 允许数据字段为零值。

**实现：**

- 只将已知 Discovery 领域错误写入 Reply。
- 未知、基础设施错误直接返回。
- Client 在 `Error == none` 后校验成功数据不变量。

**验证：**

```bash
go test ./tooling/discovery -run 'InvalidResponse|InfrastructureError' -count=100
go test -race ./tooling/discovery -run 'InvalidResponse|InfrastructureError' -count=20
```

**提交：** `fix(discovery): 保持错误与成功响应语义`

## Task 6：文档、完整验收与重新标记版本

**文件：**

- `README.md`
- `CHANGELOG.md`
- `docs/GSR-Book/03-第三篇-Cluster/03-Discovery.md`
- `docs/rfcs/RFC-0500-Roadmap.md`
- 本计划

**步骤：**

1. 示例改为先用 `ResolveRemote` 获取 `.discovery`，不跨 Runtime 直接传递动态 ServiceRef。
2. 更新可信网络、owner、epoch、bootstrap 和错误边界说明。
3. 双轴 Review 清零 P1/P2。
4. 运行：

```bash
go test ./... -count=1
go vet ./...
go test -race ./... -count=1
go test ./tooling/discovery -count=100
go run ./examples/local-runtime
go run ./examples/cluster-runtime
go run ./examples/discovery-runtime
git diff --check
```

5. 创建修正发布提交，将未推送的本地 `v0.2.0` 附注标签移动到该提交，不执行 `git push`。

**提交：** `docs(release): 修正 Discovery v0.2.0`

## 完成标准

- 可信网络前提不变，没有引入认证体系。
- 每个 Command 有稳定、可观测的 Source。
- Discovery 可仅凭节点地址和 `.discovery` 名字完成远程 bootstrap。
- authority 重启后旧租约不会重新有效。
- 租约 owner 错误不会修改节点或名字状态。
- Core 错误不降级，成功响应不接受无效零值。
- 全量测试、vet、race、重复测试和示例通过。

## 执行结果

- 已按六个垂直切片完成 RFC、Core Source、远端名字查询、Discovery 租约身份、错误语义和发布文档。
- 双轴审查未发现遗留 P1/P2；可信内网前提保持不变，没有引入认证体系。
- `go test ./... -count=1`、`go vet ./...`、`go test -race ./... -count=1` 和 `go test ./tooling/discovery -count=100` 通过。
- `local-runtime`、`cluster-runtime` 和 `discovery-runtime` 示例通过；Discovery 示例输出 `.config -> node-b/2`。
