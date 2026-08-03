# GSR RFC 索引

本文是 `docs/rfcs/` 的阅读入口。

## 阅读原则

1. 先读术语和设计原则，再读具体模块。
2. 如果聊天记录、旧文档和 RFC 冲突，以 RFC 为准。
3. 如果实现和 RFC 冲突，要么修改实现，要么先更新 RFC。
4. 所有正式文档使用中文正文，API、类型名、文件名保留英文。
5. 需要理解既有裁决的原因时，先查决策索引，再阅读其链接的 RFC；索引不能覆盖 RFC。

## 项目状态

- [Core Foundation 待办列表](TODO.md)

## 基础约定

- [RFC-0000：术语表](rfcs/RFC-0000-Foundation-Glossary.md)
- [RFC-0001：设计原则](rfcs/RFC-0001-Foundation-Design-Principles.md)
- [RFC-0002：冲突裁决记录](rfcs/RFC-0002-Foundation-Conflict-Resolution.md)
- [RFC-0003：RFC 生命周期与写作规范](rfcs/RFC-0003-Foundation-RFC-Lifecycle.md)
- [设计决策索引](DECISIONS.md)

## Layer 1：Core Runtime（运行时内核）

- [RFC-0100：Service 模型](rfcs/RFC-0100-Core-Service.md)
- [RFC-0110：ServiceRef 与寻址](rfcs/RFC-0110-Core-ServiceRef.md)
- [RFC-0120：Command 模型](rfcs/RFC-0120-Core-Command.md)
- [RFC-0130：Send、Call 与 Reply](rfcs/RFC-0130-Core-Send-Call-Reply.md)
- [RFC-0140：Session 与 Pending Call](rfcs/RFC-0140-Core-Session-PendingCall.md)
- [RFC-0150：Mailbox 设计](rfcs/RFC-0150-Core-Mailbox.md)
- [RFC-0160：Scheduler 设计](rfcs/RFC-0160-Core-Scheduler.md)
- [RFC-0170：Timer 设计](rfcs/RFC-0170-Core-Timer.md)
- [RFC-0180：Service 生命周期](rfcs/RFC-0180-Core-Lifecycle.md)
- [RFC-0190：Cluster Data Plane](rfcs/RFC-0190-Core-Cluster-Data-Plane.md)
- [RFC-0191：Cluster Transport](rfcs/RFC-0191-Core-Cluster-Transport.md)
- [RFC-0192：Runtime Inspection](rfcs/RFC-0192-Core-Runtime-Inspection.md)

## Layer 2：Runtime Tooling（工具与工程化层）

- [RFC-0200：DiscoveryService](rfcs/RFC-0200-Tooling-Discovery.md)
- [RFC-0210：Snapshot 与恢复](rfcs/RFC-0210-Tooling-Snapshot.md)
- [RFC-0220：Supervisor 与故障隔离](rfcs/RFC-0220-Tooling-Supervisor.md)
- [RFC-0230：Monitor 与可观测性](rfcs/RFC-0230-Tooling-Monitor.md)
- [RFC-0240：性能模型](rfcs/RFC-0240-Tooling-Performance.md)
- [RFC-0250：Cluster Control Plane](rfcs/RFC-0250-Tooling-Cluster-Control-Plane.md)
- [RFC-0260：ServiceGroup 与路由策略](rfcs/RFC-0260-Tooling-ServiceGroup-Routing.md)
- [RFC-0270：Drain、热更新与访问者追踪](rfcs/RFC-0270-Tooling-Drain-Hot-Reload.md)
- [RFC-0271：Drain Guard](rfcs/RFC-0271-Tooling-Drain-Guard.md)
- [RFC-0272：受控 Drain 操作](rfcs/RFC-0272-Tooling-Controlled-Drain-Operation.md)
- [RFC-0273：受控 Node Stop 执行](rfcs/RFC-0273-Tooling-Node-Stop-Execution.md)
- [RFC-0274：人工恢复与补偿](rfcs/RFC-0274-Tooling-Manual-Recovery-Compensation.md)
- [RFC-0280：Command Record 与 Replay](rfcs/RFC-0280-Tooling-Command-Record-Replay.md)
- [RFC-0290：LoginService 与 Gateway 入口](rfcs/RFC-0290-Tooling-LoginService-Gateway.md)

## Layer 3：Business Layer（业务层）

- [RFC-0300：Business Layer 分层](rfcs/RFC-0300-Business-Layering.md)
- [RFC-0310：Battle 设计](rfcs/RFC-0310-Business-Battle.md)
- [RFC-0320：Timeline 设计](rfcs/RFC-0320-Business-Timeline.md)
- [RFC-0330：Room 设计](rfcs/RFC-0330-Business-Room.md)
- [RFC-0340：PlayerService 设计](rfcs/RFC-0340-Business-PlayerService.md)
- [RFC-0350：PlayerModule 与玩家业务组合](rfcs/RFC-0350-Business-PlayerModule.md)
- [RFC-0360：WalletService 设计](rfcs/RFC-0360-Business-WalletService.md)
- [RFC-0370：业务模板与状态归属](rfcs/RFC-0370-Business-Templates.md)

## 示例和路线图

- [RFC-0400：打地鼠示例](rfcs/RFC-0400-Example-Whack-Mole.md)
- [RFC-0410：宁海双扣 GameLogic 替换示例](rfcs/RFC-0410-Example-NHSK-GameLogic.md)
- [RFC-0500：开发路线图](rfcs/RFC-0500-Roadmap.md)
