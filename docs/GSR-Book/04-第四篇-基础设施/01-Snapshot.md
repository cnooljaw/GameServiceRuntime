# Snapshot

> 状态：已实现
>
> 公开契约：[RFC-0210](../../rfcs/RFC-0210-Tooling-Snapshot.md)

## 本章目标

理解 GSR 如何在不绕过 Mailbox、不修改 Core `Service` 接口的前提下采集和加载业务状态快照。

## Snapshot 解决什么

Snapshot 保存一个稳定业务 Key 在某个 Revision 的状态。它可以作为 Player 等长期 Service 的恢复输入，也可以用于重连投影和调试。

Snapshot 不等于：

- `Runtime.Inspect()`：Inspection 只描述 Runtime 当前运行事实，不能恢复业务状态。
- 数据库事务：Wallet 仍以账本、幂等记录和审计日志为权威。
- Command Record：Record 保存输入序列，Snapshot 保存某一时刻的状态。

## 为什么不用 Stateful

GSR 不给 Service 增加以下接口：

```go
type Stateful interface {
    Snapshot() ([]byte, error)
    Restore([]byte) error
}
```

直接调用 `Snapshot()` 会绕过 Mailbox 读取运行中状态；直接调用 `Restore()` 会绕过 Command 修改运行中状态。两者还会把业务 Schema 和持久化策略压入 Core。

Phase 7D 使用现有消息管道：

```text
Manager.Capture
  -> Call CaptureCommand(Key)
  -> target Service Handle
  -> CaptureResponse(owner Key, State)
  -> validate owner Key and State before copy
  -> Store.Save -> canonical Snapshot
```

目标 Service 拥有稳定 Key，只在 Handler 中验证请求 Key 并序列化内存状态。Store IO 发生在 Call 返回之后，不占用目标 Service 的 Handler。

## 数据模型

```go
type State struct {
    Schema   string
    Version  uint32
    Revision uint64
    Payload  []byte
}

type Snapshot struct {
    Key        Key
    Source     gsr.ServiceRef
    CapturedAt time.Time
    State      State
}

type CaptureRequest struct {
    Key Key
}

type CaptureResponse struct {
    Key   Key
    State State
}

type Store interface {
    Save(context.Context, Snapshot) (Snapshot, error)
    Load(context.Context, Key) (Snapshot, error)
}
```

- `Key` 是跨实例稳定的业务身份，由目标 Service 持有，不使用会变化的 `ServiceRef`。
- Manager 发送期望 Key，并拒绝 Service 返回的无效或不匹配 owner Key，避免把一个 Service 的状态保存到另一个业务实体名下。
- `Version` 描述 payload Schema。
- `Revision` 由状态 owner 在每次可见状态变化后单调递增。
- Key 和 Schema 必须是合法 UTF-8。`Payload` 默认不超过 1 MiB；nil 无效，空状态使用非 nil 空切片。

## MemoryStore

`MemoryStore` 对同一 Key 原子比较 Revision：

- 更高 Revision 替换旧值。
- 更低 Revision 返回 `ErrStaleSnapshot`。
- 同 Revision、同 State 幂等成功，保留并返回已有 canonical Snapshot；新的 Source 和 CapturedAt 不覆盖旧 metadata。
- 同 Revision、不同 State 返回 `ErrSnapshotConflict`。

Save 和 Load 都返回独立副本。Manager 返回 Store 真正保留的 canonical Snapshot，因此 Capture 成功结果与后续 Load 一致。调用方修改 Payload 不会影响 Store 或后续读取。

## 受限恢复

恢复不修改旧实例：

```text
Manager.Load(Key)
  -> validate business Schema
  -> construct a new Service object
  -> Runtime.CreateService
  -> receive a new ServiceRef
```

完整示例：

```bash
go run ./examples/snapshot-runtime
```

预期输出：`2`。

## Cluster

`snapshot.NewCodec(fallback)` 处理 `CaptureCommand` 的请求和响应，其它 Command 委托 fallback。请求和响应都携带稳定 Key。JSON 字段使用稳定 `snake_case`；Codec 允许新增未知字段，但拒绝非法 UTF-8、缺失必填字段、格式错误和尾随第二个 JSON 值。

## 当前限制

- 只提供内存 Store。
- 不提供原地 Restore、自动重启或进程崩溃恢复。
- 不提供数据库、对象存储、压缩、加密或增量快照。
- Snapshot payload 的脱敏、访问控制和保留时间由业务与 Store adapter 负责。

## 验证

```bash
go test ./tooling/snapshot -count=100
go test -race ./tooling/snapshot
go run ./examples/snapshot-runtime
```
