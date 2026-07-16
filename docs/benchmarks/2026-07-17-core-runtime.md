# 2026-07-17 Core Runtime 性能基线

## 环境

- Go：`go1.23.3`
- 系统：`darwin/arm64`
- CPU：Apple M2
- Benchmark 并行度标记：`-8`
- 代码：本文与 benchmark 测试所在提交

执行命令：

```bash
go test ./runtime -run '^$' -bench 'Benchmark(Send|CallReply|ManyServices|TimerDelivery)$' -benchmem -benchtime=1000x -count=5
```

## 结果

下表取 5 次运行的中位数。吞吐由 `1s / ns/op` 换算，只表示单调用者串行往返能力，不代表并行容量或生产 SLA。

| 路径 | ns/op | 约 ops/s | B/op | allocs/op |
|---|---:|---:|---:|---:|
| Send -> Handle -> 测试确认 | 2,847 | 351,000 | 448 | 5 |
| Call -> Reply | 2,691 | 372,000 | 680 | 9 |
| Call，1 个 Service | 2,471 | 405,000 | 680 | 9 |
| Call，100 个 Service | 2,637 | 379,000 | 695 | 9 |
| Call，1,000 个 Service | 2,865 | 349,000 | 874 | 11 |
| After(0) -> Handle -> 测试确认 | 6,334 | 158,000 | 656 | 8 |

`BenchmarkSend` 和 `BenchmarkTimerDelivery` 通过测试 channel 确认 Handler 已完成，因此包含确认同步成本；它们不能与只测入队的微基准直接比较。

## 当前判断

1. 从 1 个增长到 1,000 个 Service，串行 Call 中位耗时约增加 16%，当前没有证据要求替换 Registry 或 Mailbox。
2. Timer 完整投递约为 Send 完整投递的 2.2 倍，但这组基准没有覆盖大规模未到期 Timer 和 GC 曲线，不能据此引入 Timer Wheel。
3. Call 和 Timer 的分配次数值得后续 profile，但第一版继续优先保证语义，不立即使用对象池。

## 资源收敛

`TestRuntimeRepeatedCreateCloseReleasesOwnedResources` 每次创建 Runtime、完成 Call、保留 PendingCall 和长 Timer，再执行关闭。20 次循环后逐实例确认 Registry、PendingCall、Timer、任务表为空，并检查 scheduler goroutine 回到宽松基线。

以下命令已通过：

```bash
go test ./runtime -run 'TestRuntimeRepeatedCreateCloseReleasesOwnedResources' -count=10
go test -race ./runtime -run 'TestRuntimeRepeatedCreateCloseReleasesOwnedResources' -count=1
```
