# RFC-0240：性能模型

> 状态：草案  
> 范围：Core Runtime、Runtime Tooling  
> 依据：`docs/learn/007-Game-Service-Runtime详细设计与实现.md`

## 目的

本文定义 GSR 的性能方向。

## 核心结论

Service 不等于 goroutine。

```text
Service = State + Mailbox + Handler
```

执行由 Scheduler + 固定执行许可池管理。

## 主要压力

游戏服务器主要压力来自：

- Envelope 分配。
- Payload 分配。
- Timer 数量。
- Mailbox 堆积。
- Pending Call。
- GC 扫描。
- 慢 Command 阻塞 Worker。

## 第一版策略

第一版优先正确性：

- `Payload any`。
- channel Mailbox。
- Go timer。
- 固定执行许可池。

## 优化方向

后续优化：

- Ring buffer Mailbox。
- Timer Wheel。
- Envelope 对象池。
- Batch dispatch。
- Fair scheduling。
- CommandID 代码生成。
- 跨节点 protobuf 编码优化。

## Benchmark 路径

不要只测单个函数。

必须测完整路径：

```text
Call -> Router -> Mailbox -> Scheduler -> Handler -> Reply
```

指标：

- 每秒 Command 数。
- P50/P95/P99 延迟。
- 每条 Command 分配次数。
- Service 数量增长曲线。
- Timer 数量增长曲线。
- GC 暂停。

首个 Core 基线使用下列完整路径 benchmark：

- `BenchmarkSend`
- `BenchmarkCallReply`
- `BenchmarkManyServices`
- `BenchmarkTimerDelivery`

可重复执行：

```bash
go test ./runtime -run '^$' -bench 'Benchmark(Send|CallReply|ManyServices|TimerDelivery)$' -benchmem -benchtime=1000x -count=5
```

基线必须记录 Go 版本、CPU 架构、`ns/op`、`B/op` 和 `allocs/op`。不同机器的数据不能直接比较；同一环境出现稳定回退后，再用 profile 定位。没有基线数据时，不设置吞吐硬阈值，也不提前引入 Ring Buffer、对象池或 Timer Wheel。

首份结果见 [`2026-07-17 Core Runtime 基线`](../benchmarks/2026-07-17-core-runtime.md)。

## 禁止

第一版不要为了性能破坏模型：

- 不要让 Timer 直接改状态。
- 不要绕过 Mailbox。
- 不要本地调用直接调对象方法。
- 不要为远程单独设计 RPC Stub。
