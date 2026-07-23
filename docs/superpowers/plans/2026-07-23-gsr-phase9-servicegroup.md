# Phase 9：ServiceGroup 与路由策略实施计划

> 状态：执行中
> 更新：2026-07-23
> 权威契约：[RFC-0260](../../rfcs/RFC-0260-Tooling-ServiceGroup-Routing.md)

## 目标

完成 ServiceGroup 事实、订阅和路由闭环：独立 `DirectoryService` 以 compare-and-set 保存带 AuthorityEpoch 的完整 ServiceSet，订阅 Service 通过 lease 接收完整快照，Router 对调用方已经持有的 ServiceSet 执行 Hash、RoundRobin 或 Broadcast。Core Runtime 与 Discovery 均不增加 ServiceGroup 概念。

## 固定边界

- `tooling/servicegroup` 是唯一新增包；Core 与 Discovery 不反向依赖它。
- Directory 是 ServiceSet 的唯一状态 owner。发布者只能提交期望版本和完整内容，新版本由 Directory 分配。
- Directory 重建生成新 AuthorityEpoch；Revision 只在同一 epoch 的同一 Group 内单调递增。
- Watch 使用 `ServiceRef + Command` 和有限 lease，不返回 channel、不创建 goroutine。
- Router 接收显式 ServiceSet，不按 GroupName 隐式 Call Directory，也不维护后台缓存。
- `Direct` 不实现为 RoutingPolicy；单目标继续使用 Runtime Send/Call。
- Phase 9 不实现 Controller、Desired State、自动注册进组、健康检查、扩缩容、Drain 或回滚编排。

## 切片 1：冻结 RFC 与实施契约

1. 重写 `docs/rfcs/RFC-0260-Tooling-ServiceGroup-Routing.md`，冻结版本身份、CAS、Watch lease、Codec、路由和失败语义。
2. 同步术语表、冲突裁决、路线图与 `RFC-0270` 的后续版本语义。
3. 新增本计划并检查 Discovery/Core 文档没有获得 ServiceSet owner 职责。
4. 检查：`rg -n "Discovery 只保存 ServiceSet|Direct.*RoutingPolicy|Version uint64" docs/rfcs` 不得命中有效规范。
5. 提交：`docs(servicegroup): 冻结 Phase 9 路由契约`。

## 切片 2：Directory 版本化事实

1. 新增 `tooling/servicegroup/service_test.go`、`client_test.go` 和 `codec_test.go` 的失败测试：新组 revision 1、陈旧 CAS、epoch fencing、空组、权限、稳定排序、去重和深复制。
2. 新增 `types.go`、`errors.go`、`commands.go`、`validation.go`、`copy.go`，固定公开类型、错误和 wire payload。
3. 实现 `NewDirectoryService`、Publish、Get、类型化 Client 和可组合 Codec；私有 sweep Command 暂只声明、不进入 Codec。
4. 检查：`go test ./tooling/servicegroup -run 'Directory|Publish|Get|Codec' -count=100`。
5. 提交：`feat(servicegroup): 增加版本化服务组目录`。

## 切片 3：Watch lease

1. 先写失败测试：先订阅后创建、当前快照、重订阅 generation、续订、取消、过期、旧 authority/generation fencing 和通知失败不回滚。
2. Directory 在 Mailbox 内保存 watcher；Runtime Timer 只投递私有过期清理 Command。
3. 完成 `ServiceSetChangedCommand` 的公共 payload 和 Codec；通知发送完整独立 ServiceSet。
4. 检查：`go test ./tooling/servicegroup -run 'Watch|Notify|Sweep' -count=100` 和对应 race 测试。
5. 提交：`feat(servicegroup): 增加服务组订阅租约`。

## 切片 4：路由策略与 Router

1. 先写失败测试：FNV-1a Hash 固定映射、空 key、RoundRobin 顺序和并发、Broadcast 稳定顺序与部分失败。
2. 实现 `RoutingPolicy`、`Hash`、`RoundRobin`、`Broadcast`；所有策略只使用输入 ServiceSet。
3. 实现 Router Send/Call，校验 policy 结果属于输入集合且不重复；多目标 Send 顺序尝试并返回 `BroadcastError`，Call 拒绝多目标。
4. 检查：`go test ./tooling/servicegroup -run 'Hash|RoundRobin|Broadcast|Router' -count=100` 和对应 race 测试。
5. 提交：`feat(servicegroup): 增加服务组路由策略`。

## 切片 5：跨节点闭环与验收

1. 增加双节点 TCP 测试：Publisher 跨节点 Publish/Get，Subscriber Watch 收到完整通知，Router 把 Command 投递到远端成员。
2. 增加 `examples/servicegroup-runtime`，展示显式 Get/Watch 缓存和 Hash/广播，不引入业务领域类型。
3. 将 RFC-0260、路线图、TODO、README 和 GSR Book 回填为实际边界。
4. 检查：全量测试、vet、race 和示例均通过。
5. 分别提交跨节点实现与最终文档验收。

## 最终验收

```bash
go test ./...
go vet ./...
go test -race ./...
go run ./examples/servicegroup-runtime
```

验收后进入 Phase 10。不得在 Phase 9 的“完成”提交中顺带加入 Controller、Desired State、Drain、自动扩缩容或修改型 NodeAgent 命令。
