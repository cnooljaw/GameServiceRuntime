# GSR Cluster Data Plane 实施计划

> 状态：执行中

**目标：** 完成 `RFC-0190`、`RFC-0191` 定义的 Phase 5，使 `Runtime.Send`、`Runtime.Call` 和 `ServiceContext.Call` 可以通过同一套 API 访问远程 `ServiceRef`，并由 TCP Transport 提供节点握手、WireEnvelope 传输和断线通知。

**范围：** 只实现 Core Cluster 数据面及 TCP adapter。不实现 Discovery、ServiceGroup、自动重连策略、管理 API、节点运维命令或业务路由，不增加第三方依赖。

## Task 1：冻结 Cluster 契约

**文件：** 修改 `docs/rfcs/RFC-0130-Core-Send-Call-Reply.md`、`docs/rfcs/RFC-0140-Core-Session-PendingCall.md`、`docs/rfcs/RFC-0190-Core-Cluster-Data-Plane.md`、`docs/rfcs/RFC-0191-Core-Cluster-Transport.md`。

1. 明确 Cluster Runtime 使用独立的可失败构造函数 `NewClusterRuntime`，本地 `NewRuntime` 保持不变。
2. 冻结 `ClusterTransport`、`ClusterEvents`、`ClusterCodec`、`WireEnvelope` 的最小公开接口。
3. 将 `CallPath` 纳入 WireEnvelope，保证跨节点同步调用环仍能检测。
4. Reply 同时携带 responder 和 caller，PendingCall 必须校验 caller、responder、Session 三者。
5. 明确 TCP Transport 只编码 WireEnvelope 和帧；Command/Reply payload 由 `ClusterCodec` 按 `CommandID + Response` 编解码。
6. 提交：`docs(cluster): 冻结数据面契约`。

## Task 2：实现远程 Router

**文件：** 新增 `runtime/cluster.go`、`runtime/cluster_test.go`；修改 `runtime/runtime.go`、`runtime/call.go`、`runtime/service.go`、`runtime/errors.go`、`runtime/pending_test.go`。

1. 先用测试内存 Transport 建立两个 Runtime，增加 Remote Send 和 Remote Call/Reply 失败测试。
2. 增加远程 handler error、错误 responder、超时后晚到 Reply、节点断线和跨节点调用环测试。
3. 实现远程路由：本地目标进入 Mailbox，远程目标编码为 WireEnvelope 并交给 Transport。
4. 入站 Command 必须校验握手 peer、Source.Node、Target.Node、Session 和 CallPath，再解码并投递本地 Mailbox。
5. 入站 Reply 必须验证 responder 是原 target、caller 是原 source；过期或伪造 Reply 不得完成 PendingCall。
6. Transport 断线时，以该节点为目标的 PendingCall 立即返回 `ErrRemoteUnavailable`。
7. 运行：

   ```bash
   go test ./runtime -run 'TestRemote|TestPendingCall|TestCluster' -count=30
   go test -race ./runtime -run 'TestRemote|TestCluster' -count=10
   ```

8. 提交：`feat(cluster): 实现远程消息路由`。

## Task 3：实现 TCP Transport

**文件：** 新增 `transport/tcp/transport.go`、`transport/tcp/wire.go`、`transport/tcp/transport_test.go`、`transport/tcp/wire_test.go`。

1. 先增加握手版本不匹配、节点身份不匹配、帧超限和 WireEnvelope 往返测试。
2. 使用标准库实现版本化握手和 `uint32` 长度前缀帧；限制 NodeID、CallPath、payload 和整帧大小。
3. 每个 peer 复用一条全双工连接；并发写串行化，读循环持续解帧并触发 `ClusterEvents.Receive`。
4. 同时建连时按 NodeID 确定首选连接方向，避免两端保留不同物理连接。
5. 当前连接断开时只通知一次 `ClusterEvents.Unavailable`；Runtime 关闭引起的断线不重复上报。
6. 运行：

   ```bash
   go test ./transport/tcp -count=30
   go test -race ./transport/tcp -count=10
   ```

7. 提交：`feat(cluster): 增加 TCP Transport`。

## Task 4：端到端验收与收口

**文件：** 新增 `examples/cluster-runtime/main.go`；修改 `.github/workflows/ci.yml`、`docs/rfcs/RFC-0500-Roadmap.md` 和本计划。

1. 使用两个真实 TCP Runtime 演示远程 Call，输出固定为 `hello cluster`。
2. CI 增加 Cluster 示例执行及输出断言。
3. 运行完整验收：

   ```bash
   go test ./... -count=1
   go vet ./...
   go test -race ./... -count=1
   go run ./examples/local-runtime
   go run ./examples/cluster-runtime
   git diff --check
   ```

4. 将 Roadmap 的下一阶段更新为 Phase 6 Core Runtime 验证，将本计划标记为完成。
5. 提交：`docs(cluster): 完成数据面阶段`。
