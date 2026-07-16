# GSR — Game Service Runtime

A modern Go Game Service Runtime inspired by Skynet.

GSR is not an Actor framework, not a gRPC microservice framework, and not a generic RPC wrapper.
It is a game-oriented Service Runtime built around:

- Service
- ServiceRef
- Command
- Send / Call
- Mailbox
- Scheduler
- Timer
- Cluster Transport
- Discovery
- Snapshot
- Supervisor
- Monitor
- Game Layer

The `docs/rfcs` directory is the source of truth. Code should be implemented from RFCs, not from ad-hoc prompts.

## 本地 Runtime 示例

```bash
go test ./...
go run ./examples/local-runtime
```

预期输出：`hello`。
