# RFC-0210：Snapshot 与受限恢复

> 状态：已接受
> 目标阶段：Phase 7D
> 接受日期：2026-07-22
> 范围：Runtime Tooling、Business Layer
> 依赖：[RFC-0100](RFC-0100-Core-Service.md)、[RFC-0130](RFC-0130-Core-Send-Call-Reply.md)、[RFC-0180](RFC-0180-Core-Lifecycle.md)

## 目的

本文定义版本化业务状态快照的采集、保存和加载边界。Snapshot 保存某一状态修订号对应的业务状态，用于显式恢复、重连投影和调试；它不是 Runtime Inspection、数据库事务或 Command Record。

## 目标

Phase 7D 必须提供：

- 通过 Call 串行进入目标 Service 的快照采集协议。
- 带稳定业务键、Schema、版本和状态修订号的快照模型。
- 可替换的 `Store` 接口和并发安全的内存实现。
- 快照大小、响应类型、旧修订号和同修订号冲突校验。
- 本地与 Cluster 可组合的 Codec。
- 由外层组合根加载快照并构造新 Service 的受限恢复示例。

## 非目标

Phase 7D 不实现：

- 修改 Core `Service`、`ServiceContext` 或 `Runtime.Inspect()`。
- 对运行中 Service 直接调用 `Snapshot()` 或 `Restore()`。
- Runtime 自动恢复、Supervisor、进程崩溃恢复或跨节点迁移。
- 数据库、对象存储、文件格式、压缩、加密或增量快照。
- 在 Service Handler 内执行外部存储 IO。
- Snapshot 与 Command Record/Replay 互相替代。
- Wallet 账本、幂等表或审计日志。

## 核心裁决

Core 不增加以下接口：

```go
type Stateful interface {
    Snapshot() ([]byte, error)
    Restore([]byte) error
}
```

原因：`Snapshot()` 会绕过 Mailbox 读取运行中状态，`Restore()` 会绕过 Command 修改运行中状态；两者还会把业务 Schema 和存储策略压入 Core。

第一版使用以下流程：

```text
Manager.Capture
  -> Runtime.Call(target, CaptureCommand, CaptureRequest{Key})
  -> target Service serializes state in Handle
  -> CaptureResponse{owner Key, State}
  -> validate owner Key and State before copy
  -> build and copy candidate Snapshot
  -> Store.Save outside the target Handler
  -> return Store canonical Snapshot
```

恢复使用新实例，不复活旧实例：

```text
Manager.Load(stable Key)
  -> application validates business schema
  -> application constructs Service with restored initial state
  -> Runtime.CreateService(new instance)
```

构造完成前的对象尚未注册，也不接收 Command；恢复不会与运行中 Handler 并发。

## 分层与依赖

```text
Business Service
  -> implements CaptureCommand in Handle
  -> owns stable Key, payload schema and state revision

tooling/snapshot Manager
  -> depends on narrow CommandCaller and Store
  -> validates, copies, timestamps and persists Snapshot

Store adapter
  -> owns persistence IO and atomic replacement semantics

runtime
  -> knows only Call, Command and ServiceRef
```

`runtime` 不得导入 `tooling/snapshot`。Business Layer 可以导入快照协议类型，但快照 payload 的业务 Schema 仍由具体 Service 所有。

## 公开契约

包路径为 `tooling/snapshot`。

```go
const CaptureCommand gsr.CommandID = 0x02000201

type Key struct {
    Namespace string
    ID        string
}

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

type CommandCaller interface {
    Call(context.Context, gsr.ServiceRef, gsr.CommandID, any) (any, error)
}

type Config struct {
    MaxPayloadBytes int
    Now             func() time.Time
}

func NewManager(CommandCaller, Store, Config) (*Manager, error)
func (m *Manager) Capture(context.Context, gsr.ServiceRef, Key) (Snapshot, error)
func (m *Manager) Load(context.Context, Key) (Snapshot, error)
func NewMemoryStore() *MemoryStore
func NewCodec(fallback gsr.ClusterCodec) gsr.ClusterCodec
```

`CaptureCommand` 是稳定协议号。目标 Service 必须通过 `CommandDeclarer` 声明它，在状态中持有自己的稳定 `Key`，校验 `CaptureRequest.Key`，并在 `Handle` 中回复带 owner Key 的 `CaptureResponse`。Manager 必须拒绝无效或与请求不一致的响应 Key，且不得调用 Store。调用方不得绕过 `Manager` 拼装远程线格式。

## 数据不变量

`Key` 是跨 Service 重建保持稳定的业务键，不是 `ServiceRef`：

- `Namespace` 必须是合法 UTF-8，已经去除首尾空白且非空，UTF-8 字节长度不超过 128。
- `ID` 必须是合法 UTF-8，已经去除首尾空白且非空，UTF-8 字节长度不超过 256。
- `Source.Node` 非空，`Source.ID` 非零。
- `Schema` 必须是合法 UTF-8，已经去除首尾空白且非空，UTF-8 字节长度不超过 128。
- `Version` 非零，表示 payload Schema 版本。
- `Revision` 非零，并由状态 owner 在每次可见业务状态变化后单调递增。
- `Payload` 必须是非 nil 字节切片；空状态使用非 nil 空切片。
- `CapturedAt` 非零，由 `Manager` 的时间源生成。

默认 `MaxPayloadBytes` 为 1 MiB。配置值小于零返回 `ErrInvalidConfig`；零使用默认值。

返回的 `Snapshot`、`State.Payload` 和内存 Store 内容必须是独立副本。调用方修改结果不能影响 Store、Manager 或后续 Load。

## 保存顺序

`Store.Save` 对同一 `Key` 必须原子执行以下规则，并在成功时返回真正保留的独立 canonical Snapshot：

1. 不存在旧值时保存并返回新值。
2. 新 `Revision` 大于旧值时替换并返回新值。
3. 新 `Revision` 小于旧值时返回 `ErrStaleSnapshot`。
4. `Revision` 相同且 `State` 完全一致时幂等成功，保留并返回已有值；新的 Source 和 CapturedAt 不覆盖旧 metadata。
5. `Revision` 相同但 Schema、Version 或 Payload 不同，返回 `ErrSnapshotConflict`。

不同 `Key` 之间不承诺事务。第一版不提供 Delete；保留时间和删除策略由具体 Store adapter 定义。

`MemoryStore` 的零值可直接使用。nil `*MemoryStore` 调用返回 `ErrInvalidConfig`，不得 panic。

## 错误语义

稳定错误：

```text
ErrInvalidConfig
ErrInvalidContext
ErrInvalidKey
ErrInvalidTarget
ErrInvalidState
ErrPayloadTooLarge
ErrSnapshotNotFound
ErrStaleSnapshot
ErrSnapshotConflict
ErrInvalidResponse
ErrUnsupportedCommand
```

规则：

1. nil interface 和动态类型为 nil 的 `CommandCaller`、`Store`、context 必须在使用前得到稳定错误，不得 panic。
2. Call 的 context 取消、超时和 Core 错误原样返回。
3. 响应类型错误、owner Key 无效或不匹配返回 `ErrInvalidResponse`；State 字段不满足不变量或 payload 超限返回对应 Snapshot 错误。
4. Store adapter 的基础设施错误原样返回；`Manager` 不伪装为 Core 错误。
5. 保存失败时 `Capture` 返回零值 Snapshot 和错误，避免调用方把未持久化结果误认为成功。
6. Store 成功返回的 canonical Snapshot 如果 Key、State 或结构违反契约，Manager 返回 `ErrInvalidResponse`。

## 并发与性能

- 目标 Service 的状态读取和序列化发生在一个 `Handle` 调用中，与该 Service 的其它 Command 串行。
- Handler 只生成内存 payload，不调用 `Store`，不创建 goroutine。
- `Manager` 不保存可变快照缓存，不创建 goroutine，可被并发调用；注入的 CommandCaller、Store 和时间源也必须支持调用方所需的并发度。
- `MemoryStore` 必须并发安全，但不得把锁跨到调用方或 Codec。
- 快照序列化会占用目标 Service 的执行许可，因此业务 Schema 应保持有界；大对象、分块和增量算法留到有基准数据后再设计。

## Cluster Codec

`NewCodec(fallback)` 只处理 `CaptureCommand` 的 `CaptureRequest` 和 `CaptureResponse`：

- 使用稳定 `snake_case` JSON 字段。
- 请求和响应都携带稳定 Key；响应 Key 由状态 owner 生成。
- 编码和解码拒绝非法 UTF-8 JSON。
- 解码允许新增未知字段，但拒绝格式错误和尾随第二个 JSON 值。
- 未识别 Command 委托给 fallback；无 fallback 时返回 `ErrUnsupportedCommand`。
- 类型不匹配返回 `ErrInvalidResponse`。
- Codec 只处理 payload，不访问 Store，不修改 Snapshot 修订号。

## 安全与业务边界

Snapshot payload 可能包含用户数据。具体 Service 和持久化 adapter 必须决定脱敏、加密、访问控制和保留时间。

明文 `secret`、连接句柄、Service 指针、锁、Timer 对象和数据库连接不得进入 Snapshot。Wallet 仍以持久化账本、幂等记录和审计日志为权威；内存 Snapshot 只能作为缓存或恢复加速。

## 与 Record/Replay 的关系

Snapshot 保存状态；Record 保存输入序列。推荐的后续组合是：

```text
Initial Snapshot + later Command Record -> isolated Replay
```

Phase 7D 不引入 Recorder 类型。Command Record/Replay 以 [RFC-0280](RFC-0280-Tooling-Command-Record-Replay.md) 为准。

## 验收

必须覆盖：

- Capture Command 在目标 Service 的 Mailbox 顺序中执行。
- Capture 后的 Store IO 不占用目标 Service Handler。
- Key、目标、Schema、Version、Revision、nil payload 和大小上限校验。
- 请求 Key、响应 owner Key 和 target/key 错配拒绝。
- Call 取消、Core 错误、错误响应类型和远程 Codec 往返。
- MemoryStore 的独立副本、并发 Save/Load、旧修订号和同修订号冲突。
- 相同修订号和相同 State 的幂等保存返回已有 canonical Snapshot。
- 通过 Load 在组合根构造新 Service，旧 `ServiceRef` 不被复用。
- Service、Manager 和 MemoryStore 不创建 goroutine。
- 全量测试、`go vet`、Race Detector、重复测试和本地示例。

## 实现状态

Phase 7D 已完成。2026-07-22 独立 Review 发现并修复：

- nil Payload 在复制前拒绝，非 nil 空 Payload 在本地和 Cluster 往返后保持有效。
- 稳定 Key 由状态 owner 返回并由 Manager 核对，target/key 错配不会进入 Store。
- Store 返回真正保留的独立 canonical Snapshot，Capture 结果与后续 Load 一致。
- Key、Schema 和 JSON wire 拒绝非法 UTF-8。
- RFC 状态、范围、依赖、接受日期和尾随空白由仓库测试约束。

本地、双节点、恢复、Race Detector、100 次 Snapshot 重复测试和全部示例均已通过。修复记录见 [GSR Phase 7D Snapshot Contract Fixes](../superpowers/plans/2026-07-22-gsr-phase7d-snapshot-contract-fixes.md)。数据库 Store、自动恢复和 Supervisor 仍按本文非目标留在后续阶段。
