# Command Record 与 Replay：记录输入，而不是偷看内存

“线上 Battle 出问题，把对象 dump 下来调试。”

“dump 能告诉你这些状态是怎么一步步变成现在这样的吗？”

Snapshot 保存一个时刻，Record 保存进入状态 owner 的输入顺序。

## 记录边界

`tooling/record.Decorator` 包装目标 Service 的 Handle：

```text
Command 进入 Decorator
  -> CommandCodec.Encode
  -> Redactor
  -> Send AppendRecordCommand
  -> 调用真实 Service.Handle
```

记录发生在 Handle 边界，不修改 Core Envelope，也不让业务 Service 写文件。

## RecordEntry

```go
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
```

`StableKey` 跨实例保持，例如 `battle/battle-42`。`Sequence` 表示同一个目标输入窗口内的顺序。

## RecorderService

RecorderService 在自己的 Mailbox 内维护每个 key 的有界窗口：

```go
recorder, err := record.NewRecorderService(record.RecorderConfig{
    MaxEntries: 4096,
})
```

Decorator 使用 Send，默认不阻塞目标业务。如果 Recorder Mailbox 满，记录可能丢失，指标和日志必须暴露这个事实。

要求审计级不丢失时，需要更强的持久日志协议，不能把 panic/热路径变成无界阻塞。

## Archive 在 Service 外

```go
archive, err := record.NewJSONArchive(directory)
err = archive.Save(ctx, bundle)
```

Archive I/O 不在目标 Handler，也不在 RecorderService Handler。导出者先通过 typed Client 读取记录，再由组合根保存文件或对象存储。

## 敏感信息

Command payload 可能包含账号、ticket 或聊天内容。记录前使用 `Redactor`：

```go
type Redactor interface {
    Redact(command gsr.CommandID, payload []byte) ([]byte, error)
}
```

不要依赖事后清洗已经落盘的数据。

## 隔离 Replay

```go
err := record.Replay(ctx, bundle, codec, factory)
```

Factory 必须创建隔离 Runtime 和新目标：

```text
RecordBundle
  -> validate format/key/sequence
  -> TargetFactory
  -> isolated Runtime
  -> Decode each entry
  -> Send in sequence
  -> close isolated target
```

Replay 不把 Command 发回生产实例，也不复用原 `ServiceRef`。

## Snapshot 与 Record 组合

```go
type RecordBundle struct {
    InitialState []byte
    Entries      []RecordEntry
}
```

先用 InitialState 构造目标，再重放后续输入，可以避免从游戏创建开始保留无限记录。

前提是：

- Snapshot schema 明确；
- Command codec 稳定；
- 随机种子和时间相关输入可重现；
- 外部副作用在 Replay 环境中被替换。

## 打地鼠示例

```bash
go test ./examples/whackmole -run Replay -v
go test ./tests/scenarios -run WhackMoleReplay -v
```

示例记录 Battle Command，在隔离 Runtime 中重建 Logic 状态。

## 对照源码

- `tooling/record/decorator.go`
- `tooling/record/service.go`
- `tooling/record/archive.go`
- `tooling/record/replay.go`
- `examples/whackmole/replay_test.go`

## 本章小结

Record/Replay 的价值不是“录下所有字节”，而是选择一个稳定的状态 owner 边界，保存可编码、可脱敏、可按顺序重放的业务输入。
