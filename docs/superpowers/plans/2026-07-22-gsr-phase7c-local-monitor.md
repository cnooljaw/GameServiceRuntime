# GSR Phase 7C 本地 Monitor 实施计划

> 状态：已完成（2026-07-22）

**目标：** 在不增加 HTTP、远程管理 Command 或后台任务的前提下，把 `Runtime.Inspect()` 转换为稳定、可序列化的本地 Monitor Report，并输出 Runtime 与业务 Metrics。

**边界：** Monitor 属于 `tooling/monitor`，只依赖 Core `RuntimeInspection`。Core 只增加通用 Metrics 枚举副本和 RFC 已要求的远程 Call 结果计数。

## Task 1：冻结 Monitor 契约

**文件：**

- `docs/rfcs/RFC-0230-Tooling-Monitor.md`
- `docs/rfcs/RFC-0500-Roadmap.md`
- `docs/TODO.md`
- 本计划

**步骤：**

1. 定义 `Inspector`、`Monitor`、`Report`、JSON 字段和稳定状态字符串。
2. 明确 MetricsSnapshot 枚举副本和远程 Call 指标。
3. 明确不实现 HTTP、CLI、Prometheus、MonitorService 和 NodeAgent。
4. 提交：`docs(monitor): 冻结本地观测输出契约`。

## Task 2：开放 Metrics 快照枚举

**文件：**

- `runtime/metrics.go`
- `runtime/inspection_test.go`
- `docs/rfcs/RFC-0192-Core-Runtime-Inspection.md`

**失败测试：**

1. `Counters`、`Gauges`、`Durations` 返回当前快照全部指标。
2. 修改返回 map 不影响原快照和后续 Inspection。
3. 空快照返回非 nil 空 map。

**实现：** 为 `MetricsSnapshot` 增加三个只读复制方法，不公开 collector 或写接口。

**验证：**

```bash
go test ./runtime -run 'MetricsSnapshot' -count=100
go test -race ./runtime -run 'MetricsSnapshot' -count=20
```

**提交：** `feat(runtime): 开放指标快照枚举`

## Task 3：补齐远程 Call 结果指标

**文件：**

- `runtime/call.go`
- `runtime/cluster_test.go`

**失败测试：**

1. 成功远程 Call 增加 `remote_calls_succeeded_total`。
2. 发送失败、远端 Handler 错误或超时增加 `remote_calls_failed_total`。
3. 本地 Call 不增加远程结果指标。

**实现：** 每个进入 Runtime 远程 Call 路径的请求只在最终返回点记录一次结果。

**验证：**

```bash
go test ./runtime -run 'RemoteCall.*Metric' -count=100
go test -race ./runtime -run 'RemoteCall.*Metric' -count=20
```

**提交：** `feat(runtime): 记录远程 Call 结果指标`

## Task 4：实现本地 Monitor Report

**文件：**

- `tooling/monitor/errors.go`
- `tooling/monitor/report.go`
- `tooling/monitor/monitor.go`
- `tooling/monitor/monitor_test.go`

**失败测试：**

1. `Capture` 转换 Runtime、Service 和 Task 的稳定状态字符串。
2. Service、Task、PendingCall、Timer 和三类 Metrics 完整输出。
3. 修改第一次 Report 不影响第二次 Report 或 Runtime。
4. unknown 枚举稳定输出 `unknown`。

**实现：** `Monitor` 只保存窄 `Inspector`，每次 Capture 生成新切片和 map，不缓存 Report。

**验证：**

```bash
go test ./tooling/monitor -run 'Capture' -count=100
go test -race ./tooling/monitor -run 'Capture' -count=20
```

**提交：** `feat(monitor): 增加本地运行报告`

## Task 5：实现 JSON 输出

**文件：**

- `tooling/monitor/monitor.go`
- `tooling/monitor/json_test.go`

**失败测试：**

1. JSON 字段为稳定 `snake_case`。
2. 空切片和 map 输出 `[]`、`{}`，不输出 `null`。
3. nil writer 返回 `ErrInvalidWriter`。
4. writer 错误原样返回且 writer 不被关闭。
5. 一次 `WriteJSON` 只调用一次 `Inspect`。

**实现：** 使用 `encoding/json.Encoder` 写入一个 Report 和换行，不启动 HTTP。

**验证：**

```bash
go test ./tooling/monitor -run 'JSON|Writer' -count=100
go test -race ./tooling/monitor -run 'JSON|Writer' -count=20
```

**提交：** `feat(monitor): 增加稳定 JSON 输出`

## Task 6：示例、文档与发布门禁

**文件：**

- `examples/monitor-runtime/main.go`
- `README.md`
- `CHANGELOG.md`
- `docs/GSR-Book/04-第四篇-基础设施/03-Monitor.md`
- `docs/rfcs/RFC-0230-Tooling-Monitor.md`
- `docs/rfcs/RFC-0500-Roadmap.md`
- `docs/TODO.md`
- 本计划

**步骤：**

1. 示例创建本地 Runtime 和 Service，输出一份 JSON Report。
2. 教程说明本地 Monitor 与 Phase 8 NodeAgent 的边界。
3. 双轴 Review 清零 P1/P2。
4. 执行：

```bash
go test ./... -count=1
go vet ./...
go test -race ./... -count=1
go test ./tooling/monitor -count=100
go run ./examples/monitor-runtime
git diff --check
```

5. 把 RFC 状态和路线图更新为已完成；更新本地未推送版本标签，不执行 `git push`。

**提交：** `docs(monitor): 完成 Phase 7C 本地观测`

## 完成标准

- Monitor 只依赖 `Runtime.Inspect()`，无 Service 指针和私有 Registry。
- Core Metrics 可以完整枚举，返回值保持副本语义。
- 远程 Call 每次只记录一个成功或失败结果。
- Report 和 JSON 字段稳定，空集合不为 null。
- 无 HTTP、远程 Command、Monitor Service、goroutine 或第三方依赖。
- 全量测试、vet、race、重复测试和示例通过。

## 实施结果

- `5878c2c` 开放 `MetricsSnapshot` 指标枚举副本。
- `02eedb7` 在远程 Call 统一出口记录成功或失败指标。
- `ea21fcb` 增加本地 Monitor Report 和状态转换。
- `a487c0d` 增加稳定 JSON 输出和 writer 契约。
- 示例、教程、RFC、路线图和发布记录在 Phase 7C 收口提交中同步。
