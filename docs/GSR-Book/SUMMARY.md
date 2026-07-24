# 《GSR：从 Service 到游戏业务的 Go Runtime 实战》

本书以当前源码为章节地图，以 RFC 为设计依据。第一次阅读建议从前言顺序读到 Runtime 内核，再跳到游戏层和打地鼠实战。

## 前言

- [前言：先让一局游戏跑起来](00-前言.md)

## 第一篇：设计理念

- [为什么不是 Actor 框架](01-第一篇-设计理念/01-为什么不是Actor框架.md)
- [为什么选择 Service](01-第一篇-设计理念/02-为什么选择Service.md)
- [设计原则：把复杂度放回它的 owner](01-第一篇-设计理念/03-设计原则.md)

## 第二篇：Runtime 内核

对应 `runtime/`。

- [Service：四个方法背后的运行契约](02-第二篇-Runtime内核/01-Service.md)
- [ServiceRef：地址、名字与实例不是一回事](02-第二篇-Runtime内核/02-ServiceRef.md)
- [Command、Send、Call 与 Reply](02-第二篇-Runtime内核/03-Command.md)
- [Mailbox：一扇门比十把锁更容易证明](02-第二篇-Runtime内核/04-Mailbox.md)
- [Scheduler：并行的是 Service，不是同一份状态](02-第二篇-Runtime内核/05-Scheduler.md)
- [Timer：未来仍然是一条 Command](02-第二篇-Runtime内核/06-Timer.md)
- [Lifecycle：超时以后，函数可能还活着](02-第二篇-Runtime内核/07-Lifecycle.md)
- [Runtime Inspection：只读快照，不是控制台后门](02-第二篇-Runtime内核/08-Inspection.md)

## 第三篇：Cluster 与寻址

对应 `runtime/cluster.go`、`transport/tcp/`、`tooling/discovery/` 和 `tooling/servicegroup/`。

- [Cluster Data Plane：让远程 Command 仍然像 Command](03-第三篇-Cluster/01-Cluster.md)
- [TCP Transport：网络归网络，业务归 Service](03-第三篇-Cluster/02-Transport.md)
- [Discovery：当调用方连节点也不该知道](03-第三篇-Cluster/03-Discovery.md)
- [Session 与 Pending Call：传输关联不是业务幂等](03-第三篇-Cluster/04-Session.md)
- [ServiceGroup：版本化成员事实与显式路由](03-第三篇-Cluster/05-ServiceGroup.md)

## 第四篇：Runtime Tooling

对应 `tooling/`。

- [Snapshot：不绕过 Mailbox 读取状态](04-第四篇-基础设施/01-Snapshot.md)
- [Supervisor：创建替代实例，而不是复活旧对象](04-第四篇-基础设施/02-Supervisor.md)
- [Monitor：把 Inspection 变成稳定报告](04-第四篇-基础设施/03-Monitor.md)
- [Performance：先找到瓶颈属于谁](04-第四篇-基础设施/04-Performance.md)
- [客户端入口：从认证连接到业务 Command](04-第四篇-基础设施/05-客户端入口.md)
- [Control Plane：先看见，再决定，再执行](04-第四篇-基础设施/06-ControlPlane.md)
- [Drain、Stop 与恢复：热更新是一条状态机](04-第四篇-基础设施/07-Drain与恢复.md)
- [Command Record 与 Replay：记录输入，而不是偷看内存](04-第四篇-基础设施/08-RecordReplay.md)

## 第五篇：Business Layer

对应 `game/`。

- [Game Layer：业务模板不是第二套 Runtime](05-第五篇-游戏层/01-GameLayer.md)
- [Battle：一局游戏，一个串行 owner](05-第五篇-游戏层/02-Battle.md)
- [Timeline：把游戏时间变成可 fencing 的意图](05-第五篇-游戏层/03-Timeline.md)
- [Room：成员与 Battle 索引的 owner](05-第五篇-游戏层/04-Room.md)
- [Player 与 PlayerModule：长期状态的组合边界](05-第五篇-游戏层/05-Player.md)
- [Wallet 与 LedgerRunner：资金流程不能相信超时](05-第五篇-游戏层/06-Wallet.md)

## 第六篇：完整实践

对应 `examples/`、`tests/scenarios/` 和项目协作流程。

- [打地鼠实战：从一条 Kick 看完整 Runtime](06-第六篇-实践/01-打地鼠实战.md)
- [源码阅读与协作指南](06-第六篇-实践/02-Codex开发指南.md)
- [开发路线图：已经有什么，还缺什么](06-第六篇-实践/03-开发路线图.md)

## 配套命令

```bash
go run ./examples/local-runtime
go run ./examples/whackmole
go test ./...
go test -race ./...
```

正式契约入口：[GSR RFC 索引](../SUMMARY.md)。
