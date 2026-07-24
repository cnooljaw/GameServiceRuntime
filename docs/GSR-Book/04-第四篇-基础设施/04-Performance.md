# Performance：先找到瓶颈属于谁

“我们把 Mailbox 换成 Ring Buffer，再加对象池，性能肯定更好。”

老周问：“现在最慢的是 Mailbox、Handler、网络、广播，还是数据库？”

没有数据时，优化只是把复杂度提前写进架构。

## 性能模型

一条本地 Send 大致经过：

```text
校验 Runtime
  -> 查 Registry
  -> 接受边界
  -> Mailbox push
  -> Ready Queue
  -> worker dispatch
  -> Handler
```

Call 还增加：

```text
Pending Call + Session + wait + Reply
```

远程调用再增加 Codec、framing、TCP 和对端调度。

## 现有基准

Core 基准在：

```bash
go test ./runtime -run '^$' -bench . -benchmem
```

业务基准在：

```bash
go test ./examples/whackmole \
  -run '^$' \
  -bench '^BenchmarkKick' \
  -benchmem \
  -count=3
```

当前 Apple M2 的一次测量：

| 场景 | 延迟范围 | 分配 |
| --- | ---: | ---: |
| 单 Battle 连续 Kick | 约 3.1–3.3 µs/op | 约 777 B，12 allocs/op |
| 64 Battle、4 worker | 约 2.0–3.4 µs/op | 约 777 B，12–13 allocs/op |

这些数字只测轻量 Mailbox、Call/Reply 和 Logic 路径。它们不包含真实网络、大规模广播、持久化或复杂规则。

## 串行热点

单个 Battle 是一个串行热点：

```text
throughput ≈ 1 / average handler time
```

增加 worker 可以并行更多 Battle，不能并行同一个 Battle。

因此优化顺序应是：

1. 缩短 Handler；
2. 把 I/O 移给有界外部 runner；
3. 减少 Snapshot、payload 和广播复制；
4. 按 Battle/Player 等 owner 分片；
5. 压测证明必要后再拆 owner。

## 不能只看 ns/op

生产系统还要观察：

- p50、p95、p99；
- Mailbox 排队时间和深度；
- `ErrMailboxFull`；
- Handler duration；
- Timer 投递失败；
- Remote Call 成功/失败；
- Broadcast 人数和部分失败；
- GC、allocs/op；
- Stop/Close 尾延迟。

平均 3µs 不能掩盖偶发 300ms Handler。

## 基准如何写

基准应明确隔离变量：

```go
func BenchmarkKickSingleBattle(b *testing.B) {
    runtime, refs := newBenchmarkBattles(b, 1)
    b.ReportAllocs()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, err := runtime.Call(
            context.Background(),
            refs[0],
            KickCommand,
            request,
        )
        if err != nil {
            b.Fatal(err)
        }
    }
}
```

如果基准同时创建 Service、等待真实 Timer、写文件和打印日志，结果无法说明 Command 热路径。

## 什么时候使用对象池

只有 profile 证明某个稳定对象分配是主要热点，并且池不会破坏所有权时，才考虑复用。

尤其不要池化：

- 可能逃逸到 Handler 外的 payload；
- 返回给调用方的快照；
- 跨 goroutine 所有权不清的对象；
- 依赖延迟 Reply 的状态。

## 当前性能待办

`docs/TODO.md` 的 BIZ-001 仍需补：

- 不同参与者数量的 Broadcast；
- p50/p95/p99；
- Mailbox 拒绝和队列等待；
- 固定机器的可比较报告。

## 本章小结

GSR 的第一性能原则不是“无锁”，而是“先知道时间花在谁拥有的边界上”。正确的 owner 让优化可以局部发生；错误的共享状态会让任何快路径都难以证明。
