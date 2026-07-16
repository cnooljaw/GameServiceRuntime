# GSR Cluster Review 修复计划

> 状态：执行中

**目标：** 按 2026-07-17 Cluster 提交 review 裁决修复其余 P1/P2，同时保持 Core Runtime 简洁。当前 TCP Transport 明确限定为可信内网，不在本轮加入认证协议。

## Task 1：修复远程错误响应

**文件：** 修改 `runtime/cluster.go`、`runtime/cluster_test.go`，新增或修改 Runtime 包内错误映射测试。

1. 增加 Reply payload 编码失败测试，要求 caller 和 responder 都收到 `ErrPayloadEncode`，不能等待超时。
2. 增加三节点调用测试：A Call B，B Call 不可用的 C，A 仍可通过 `errors.Is` 识别 `ErrRemoteUnavailable`。
3. 将全部稳定 Runtime 错误集中映射为版本化 wire error code；未知业务错误仍返回 `RemoteError`。
4. Reply 编码失败时发送无业务 Payload 的错误响应。
5. 提交：`fix(cluster): 保证远程错误响应`。

## Task 2：保留构造清理错误

**文件：** 修改 `runtime/cluster.go`、`runtime/cluster_test.go`。

1. 让失败 Transport 的 `Close` 返回测试错误，要求 `NewClusterRuntime` 的结果同时匹配 `ErrClusterStart` 和清理错误。
2. 使用 `errors.Join` 汇总启动、Transport 清理和 Runtime 清理结果。
3. 与 Task 1 一起提交。

## Task 3：隔离节点建连

**文件：** 修改 `transport/tcp/transport.go`、`transport/tcp/transport_test.go`。

1. 增加测试：一个节点停在握手超时期间，另一个健康节点仍能立即完成首次建连和发送。
2. 用 `map[NodeID]*dialAttempt` 合并同节点拨号；不同节点不共享拨号锁。
3. 提交：`fix(cluster): 隔离节点建连与帧分配`。

## Task 4：出站帧预检

**文件：** 修改 `transport/tcp/wire.go`、`transport/tcp/wire_test.go`。

1. 增加 allocation 测试，超大 payload 在返回 `ErrFrameTooLarge` 前不得建立同尺寸 Buffer。
2. 编码前计算完整帧大小并校验所有有界字段，通过后一次性分配目标 Buffer。
3. 与 Task 3 一起提交。

## Task 5：全量验收

1. 运行：

   ```bash
   go test ./... -count=1
   go vet ./...
   go test -race ./... -count=1
   go test ./runtime ./transport/tcp -count=30
   go run ./examples/local-runtime
   go run ./examples/cluster-runtime
   git diff --check
   ```

2. 将本计划状态改为完成，提交收口文档。
