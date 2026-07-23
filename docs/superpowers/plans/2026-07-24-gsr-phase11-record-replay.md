# GSR Phase 11 Record/Replay 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` task-by-task. 每项行为先以失败测试固定，再写最小实现并单独提交。

**Goal:** 在不修改 Core Envelope 的前提下，实现有界 Command 录制、版本化 JSON bundle、可组合 Codec 和隔离 Replay；普通录制错误不得改变业务 Command。

**Architecture:** `tooling/record.Decorator` 在目标 Service 的 Handle 前编码并 Send 一个 immutable entry；`RecorderService` 是每 Key 环形窗口的唯一 owner；`JSONArchive` 是 Handler 外显式 I/O；Replay 由调用方创建隔离 target，按连续 sequence Decode/Send。

**Tech Stack:** Go 1.23.3、标准库 `context`/`encoding/json`/`sync`/`time`、Core Service decorator 约定、TDD、Race Detector。

## Task 1：公开模型、验证与有界 RecorderService

**Files:**

- Create: `tooling/record/errors.go`
- Create: `tooling/record/types.go`
- Create: `tooling/record/validation.go`
- Create: `tooling/record/service.go`
- Test: `tooling/record/service_test.go`

- [ ] 写失败测试：无效 Key/version/ref/sequence/time/command/bytes、MaxEntries、Append 相同 sequence、List after/limit、按 Key 隔离、环形淘汰、Clear、深复制、Service Commands 与错误 Reply。
- [ ] 实现 FormatVersion=1、RecordEntry/Bundle、Codec/Redactor、RecorderConfig 与导出错误。`RecorderService` 处理 `0x02800101..03`，每 Key 保留连续 sequence 的 bounded ring；只在 Mailbox 改写状态。
- [ ] 在 fake ServiceContext 下验证 Append/List/Clear，随后运行 `go test ./tooling/record -run '^TestRecorder' -count=100` 和 race 选择器。
- [ ] Commit: `feat(record): 增加有界命令记录服务`。

## Task 2：实现透明 Decorator

**Files:**

- Create: `tooling/record/decorator.go`
- Test: `tooling/record/decorator_test.go`

- [ ] 写失败测试：nil/typed nil target、无效 recorder/key/codec、Commands 与 StartupCommand 透传、同一 Handle sequence、Source/Target 采集、Encode/Redact/Send 失败下 normal 仍 delegate、strict 不 delegate、payload 和返回 slices 隔离。
- [ ] 实现 Decorator 包装原 Service 的 Init/Handle/Stop/Close，不暴露 target；Handle 使用当前 CommandContext Source、Init 保存的 Self 和 Now，先 Encode/Redact/clone，再 Send Append，最后 delegate。
- [ ] normal 错误以 Logger 类别记录；strict 仅供测试，返回错误后不调用 target。Decorator 不创建 goroutine、不得保存 CommandContext。
- [ ] 运行 `go test ./tooling/record -run '^TestDecorator' -count=100` 与 race 测试。
- [ ] Commit: `feat(record): 在服务边界录制命令输入`。

## Task 3：实现 Client、JSONArchive 与 Replay

**Files:**

- Create: `tooling/record/client.go`
- Create: `tooling/record/archive.go`
- Create: `tooling/record/replay.go`
- Test: `tooling/record/archive_test.go`
- Test: `tooling/record/replay_test.go`

- [ ] 先写 Archive tests：JSON round-trip、未知版本、第二个 JSON 值、损坏数据、目录/路径拒绝、bytes 不泄漏。Archive API 接受 io.Reader/io.Writer 或调用方已打开文件，避免包内自行管理线上路径/保留期。
- [ ] 写 Replay tests：factory 创建新的 target、严格连续 sequence、decode 失败停止、send 失败停止、原 Runtime 未收到、payload clone、empty bundle 与 InitialState 透传。
- [ ] 实现 typed Client 的 response 校验，JSONArchive 的 FormatVersion 检查，以及 Replay 的 bundle/key/sequence 验证与逐条 Send。Replay 不启动 Runtime、不调用原 Handle、不连接 production transport。
- [ ] 运行 `go test ./tooling/record -run '^Test(Archive|Replay|Client)' -count=100` 与 race 选择器。
- [ ] Commit: `feat(record): 导出版本化记录并隔离回放`。

## Task 4：实现 Codec 与 Battle 导向集成验收

**Files:**

- Create: `tooling/record/codec.go`
- Test: `tooling/record/codec_test.go`
- Test: `tooling/record/integration_test.go`
- Modify: `docs/rfcs/RFC-0280-Tooling-Command-Record-Replay.md`
- Modify: `docs/rfcs/RFC-0500-Roadmap.md`
- Modify: `README.md`
- Modify: `CHANGELOG.md`

- [ ] 写 Codec tests：仅公开 Recorder Command 编码、request/response 精确类型、未知/私有 command fallback、尾随 JSON、无 fallback、深复制与不可信 response 拒绝。
- [ ] 集成测试在真实 Runtime 创建 Recorder + Decorated counter Service，录制 Send/Call/timer 到达的 Command，再由 factory 创建第二个 Runtime 重放并验证相同状态；normal record send 拒绝不影响 target。
- [ ] 将 RFC-0280 标为已接受并写日期，文档明确 JSONArchive 是显式 adapter、不是生产 retention 服务；更新路线图/README/CHANGELOG。
- [ ] 全量门禁：`go test ./...`、`go vet ./...`、`go test -race ./...`；Commit: `docs(record): 完成命令录制回放验收`。
