# RFC-0280：Command Record 与 Replay

> 状态：已接受
> 接受日期：2026-07-24
> 实现日期：2026-07-24
> 目标阶段：Phase 11
> 范围：Runtime Tooling、Debug、Business Layer
> 依赖：[RFC-0100](RFC-0100-Core-Service.md)、[RFC-0130](RFC-0130-Core-Send-Call-Reply.md)、[RFC-0170](RFC-0170-Core-Timer.md)、[RFC-0210](RFC-0210-Tooling-Snapshot.md)
> 依据：以 Service Handle 边界复现输入，而不改变 Core Envelope

## 目的

本文定义可选的 Command 录制与隔离 Replay。它帮助测试和排障复现一个 Service（优先 Battle）的输入顺序；它不替代 Snapshot、业务账本、审计日志、持续备份或线上流量镜像。

## 目标

- 以 Service decorator 在目标 `Handle` 的同一 Mailbox 串行边界记录已进入的 Command。
- 交付有版本的 `RecordEntry`、有界内存 `RecorderService`、可组合 Cluster Codec、可导入导出的 `RecordBundle` 与独立 Replay runner。
- 录制失败默认不影响业务 Command；测试可选 strict 模式使录制投递失败立即返回。
- Deterministic Replay 由业务注入的 Clock/Random/外部输入实现；Timer 到达本身作为普通 Command 被记录和重放。

## 非目标

- 不修改 Core `Service`、`Envelope`、Session 或 `Runtime.Inspect`，不记录私有函数调用、goroutine 调度、内部 PendingCall 或对象指针。
- 不提供线上永久记录服务、数据库 schema、跨节点全量流量捕获、密钥托管、自动脱敏推断或生产故障恢复。
- 不承诺从一次 Record 推断 Reply 顺序；需要验证输出时，业务应记录稳定投影或在 Replay 后查询 Snapshot。

## 分层与依赖

```text
Business Service Mailbox
  -> RecordingDecorator (encode one immutable input)
  -> RecorderService (owns bounded record state)
  -> optional external Archive adapter (owns file / object-store I/O)

isolated composition root
  -> new Runtime + new target Service
  -> ReplayRunner feeds RecordEntry in sequence
```

`tooling/record` 只依赖 Core；`game` 可以提供 RecordCodec、Clock 和 Random 的业务实现，但 Core 不引用 Record。Decorator 不暴露被包装 Service；RecorderService 不调用业务 Service；文件或对象存储 adapter 不得在任何 Service Handler 中执行阻塞 I/O。

## 公开契约

包路径为 `tooling/record`：

```go
const FormatVersion uint16 = 1

type StableKey string
type Sequence uint64

type RecordEntry struct {
    FormatVersion uint16
    TargetKey     StableKey
    Target        gsr.ServiceRef
    Source        gsr.ServiceRef
    Sequence      Sequence
    RecordedAt    time.Time
    Command       gsr.CommandID
    Payload       []byte
    TraceID       string
}

type RecordBundle struct {
    FormatVersion uint16
    TargetKey     StableKey
    InitialState  []byte
    Entries       []RecordEntry
}

type CommandCodec interface {
    Encode(gsr.CommandID, any) ([]byte, error)
    Decode(gsr.CommandID, []byte) (any, error)
}
type Redactor interface { Redact(gsr.CommandID, []byte) ([]byte, error) }

type RecorderConfig struct {
    MaxEntries int
}
func NewRecorderService(RecorderConfig) (*RecorderService, error)
func NewDecorator(gsr.Service, gsr.ServiceRef, StableKey, CommandCodec, Redactor, bool) (*Decorator, error)

type Client interface {
    Append(context.Context, gsr.ServiceRef, RecordEntry) error
    List(context.Context, gsr.ServiceRef, StableKey, Sequence, int) ([]RecordEntry, error)
    Clear(context.Context, gsr.ServiceRef, StableKey) error
}
func NewClient(CommandCaller) (Client, error)

type Archive interface {
    Save(context.Context, RecordBundle) error
    Load(context.Context, StableKey) (RecordBundle, error)
}
func NewJSONArchive(directory string) (*JSONArchive, error)
func NewClusterCodec(fallback gsr.ClusterCodec) gsr.ClusterCodec

type ReplayRuntime interface {
    Send(gsr.ServiceRef, gsr.CommandID, any) error
}
type ReplayTarget struct { Runtime ReplayRuntime; Ref gsr.ServiceRef }
type TargetFactory func(context.Context, RecordBundle) (ReplayTarget, error)
func Replay(context.Context, RecordBundle, CommandCodec, TargetFactory) error
```

`Decorator` 的参数依次为被包装 Service、Recorder Ref、稳定 TargetKey、Codec、可选 Redactor 与 strict 标志。Codec/Redactor 只处理 payload bytes，不得保存 ServiceContext；空 Key、无效 Ref、nil Codec、`MaxEntries <= 0` 与 `FormatVersion != 1` 均为配置或输入错误。`InitialState` 是业务自行编码的可选 Snapshot 投影，Record 模块不解释它。

Command ID 固定为：

```text
0x02800101 AppendRecord       RecorderService
0x02800102 ListRecords        RecorderService
0x02800103 ClearRecords       RecorderService
```

## 状态与生命周期

Decorator 在先 delegate 前编码原 Command，分配其目标 Key 的单调 `Sequence`，再通过 `ServiceContext.Send` 投递 `AppendRecord`。它的 sequence 属于被包装 Service 的 Mailbox 顺序；被包装 Service 的 Init/Stop/Close 和实际 Command 行为不变。普通模式下 Encode、Redact 或 Send 失败只写入 logger，不阻断 delegate；strict 模式返回错误并不 delegate，因此只允许测试 Runtime 使用。

RecorderService 是条目的唯一 owner。每 Key 保存按 Sequence 排序的环形窗口，超过 `MaxEntries` 淘汰最旧条目；List 的 `after` 为排他游标，limit 必须在 `1..MaxEntries`。Clear 仅清空指定 Key，操作结果与 List 均为深拷贝。

Archive 是组合根或外部 adapter 的所有者。它只能消费已经从 RecorderService 取得的 `RecordBundle`；Record Bundle 编码为 UTF-8 JSON，顶层和每条 Entry 都必须带 FormatVersion。第一版的 `NewJSONArchive(directory)` 要求调用方提供一个已有目录；它以 StableKey 的不可逆文件名在该目录内原子替换保存、按相同 Key 加载。调用方仍决定目录、保留期、加密、访问控制与上传；它不是 Service。`NewClusterCodec` 只编码三个 Recorder 公开 Command，未识别的 Command 交给可选 fallback。

## Replay 与确定性

Replay 要求 TargetFactory 创建全新的隔离 Runtime、目标 Service 和可选初始业务状态。它验证 Bundle、Entry 版本、Key 与严格连续 Sequence，然后按顺序 `Send` Decode 后的 Command。Replay 不连接生产 Transport，不复用原 Ref，不调用原 Runtime，也不直接调用 Handle。

普通 Replay 只保证输入顺序。需要确定性时，目标服务必须从业务注入的 `Clock`、`Random` 和外部输入 provider 读取值：Record 负责保存相应 Command payload（例如 timer fire、随机 seed、IO result），不试图劫持 `time.Now` 或全局随机数。Battle 的完整 ServiceRef、Command Record Sequence、随机 seed、`TimelineID/Revision`、小局身份和结算结果必须包含在其业务 Command/Snapshot 投影中。

## 错误与失败语义

- 版本不支持、Key/Sequence 不合法、Codec Encode/Decode 失败、Redactor 失败、Bundle 损坏和 TargetFactory 失败使用可区分的导出错误；Replay 在任何一条失败处停止。
- 普通录制的失败不改变目标业务结果；strict 是测试专用显式选择。
- Recorder 满时淘汰而非阻塞 Mailbox；被淘汰窗口不可声称为完整 Replay，Archive 必须保留起始 Sequence。
- Archive I/O、磁盘满、网络失败或脱敏策略由 adapter 返回给其调用方；不得反向使已处理业务 Command 回滚。

## 并发与所有权

Decorator、RecorderService 和 Replay target 的状态只在各自 Mailbox 或调用方串行流程中改变；它们不得创建 goroutine。需要异步导出时，组合根可提供固定上限的 Archive runner：其 Close 必须取消接收、等待真实 I/O 返回，结果通过独立 Command 或调用方显式查询收敛。所有 bytes、Entries、Bundles 和 payload 均复制，Codec 不得返回可被后续修改的内部 buffer。

## 可观测性

Recorder 的 `List` 返回每 Key 已保留条目及其最早/最新 Sequence；Core Metrics（只经 `Inspect().Metrics`）提供经 StableKey 哈希后的保留条目 gauge、全局淘汰计数和普通模式录制失败计数。日志只能包含 Key、Command、Sequence、长度和错误类别，绝不写 payload 明文。业务 TraceID 是不透明字符串，不自动生成或解释。

## 验收

- Decorator 记录同一目标 Service 的 Command 顺序、Source、Key、深拷贝 payload，并不改变普通 Handle 行为。
- strict/normal 录制失败、环形淘汰、Clear/List 游标、Codec 与 Redactor 异常均有单元测试。
- JSON Bundle 可 round-trip，未知版本、非连续 Sequence、错误 Key/Command payload 必须失败。
- 隔离 Replay 证明原 Runtime 未收到 Command；通用 Timer Command 已可录制与重放。随机 seed、业务状态投影及其 Battle 确定性验收由 RFC-0400 的实际业务组合完成。
- Cluster Codec 与其他 Tooling Codec 可组合；`go test -race ./...` 不出现共享 buffer 或状态竞争。
