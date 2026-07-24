# 源码阅读与协作指南

这章写给准备修改 GSR 的人，也写给与 Codex 协作的维护者。

## 权威信息顺序

遇到设计问题，按顺序查：

```text
docs/SUMMARY.md
  -> docs/DECISIONS.md
  -> 目标 RFC
  -> 相邻 RFC
  -> 源码
  -> 测试
```

决策索引用于检索原因，不是第二份契约。

如果代码与 RFC 冲突：

1. 先确认是否是实现错误；
2. 如果需要改变契约，先修改 RFC 并写清裁决；
3. 再修改代码和测试；
4. 更新决策索引、路线图和本书。

聊天结论不能代替文档。

## 按源码阅读

推荐第一条路径：

```text
runtime/types.go
runtime/service.go
runtime/runtime.go
runtime/call.go
runtime/mailbox.go
runtime/scheduler.go
runtime/timer.go
runtime/lifecycle.go
runtime/inspection.go
```

第二条路径：

```text
examples/local-runtime
examples/cluster-runtime
examples/discovery-runtime
examples/servicegroup-runtime
examples/whackmole
```

第三条路径按业务：

```text
game/types.go
game/battle.go
game/timeline.go
game/player.go
game/wallet.go
```

## 一个 Phase 的工作方式

进入新 Phase 前：

1. 阅读目标 RFC、相邻 RFC；
2. 阅读 RFC-0500 和 `docs/TODO.md`；
3. 检查已有源码和测试；
4. 确认 owner、公开 API、失败语义、非目标和验收；
5. 契约缺口先写 RFC；
6. 写失败测试；
7. 最小实现；
8. 做“契约一致性 + 代码质量”双轴评审；
9. 运行完整门禁；
10. 中文提交。

## 测试归位

测试放在它保护的边界旁边。

包内单元/契约测试：

```text
runtime/*_test.go
tooling/<module>/*_test.go
game/*_test.go
examples/whackmole/*_test.go
```

跨多个真实组件的流程：

```text
tests/scenarios/
```

当前仓库不单独维护一个内容重复的 `tests/integration/`。如果未来出现大量跨包但非端到端测试，可以新增目录，但必须先定义与 scenarios 的边界。

## 测试先写什么

一个公开行为至少覆盖：

- 正常；
- 重复；
- 非法 payload；
- 来源错误；
- 超时；
- 迟到结果；
- Stop/Close 竞争；
- panic；
- 返回副本；
- 资源泄漏。

并发敏感测试增加重复次数：

```bash
go test ./runtime -run TestName -count=100
go test -race ./...
```

不要用长 `time.Sleep` 代替可控时钟、Command 或完成信号。

## 代码边界检查

新增 Service 时检查：

```text
是否直接写 go 语句？
是否保存另一个 Service 指针？
是否在 Handler 外改状态？
是否把 Context 保存到字段？
Timer 是否执行回调？
Stop 是否制造业务终态？
```

新增 Tooling 时检查：

```text
是否只依赖 Core 公开副本？
是否为了自己增加 Runtime getter？
是否混合 observed、desired 和 action？
是否有身份、RequestID 和审计？
外部 runner 是否有界、可关闭、等待真实返回？
```

## CodeGraph 与源码

跨文件分析先同步 CodeGraph，用它定位 symbol、caller、callee 和影响范围。图用于导航，最终以源码和测试复核。

不要用文本搜索猜完整调用图；也不要只相信图而跳过编译器和测试。

## 文档写法

RFC 写：

- 目的；
- 公开契约；
- 状态与生命周期；
- 错误与失败；
- 并发与 owner；
- 可观测性；
- 非目标；
- 验收。

教程写：

- 场景；
- 错误方案；
- 为什么；
- 当前 API；
- 完整例子；
- 边界。

不要把教程复制成第二份 RFC。

## 提交前门禁

```bash
go test ./...
go vet ./...
go test -race ./...
git diff --check
```

如果改了 example，还要运行对应程序。改了 benchmark，要记录机器、命令和适用范围。

## Phase 收尾报告

每个 Phase 至少说明：

1. 新能力解决什么实际业务问题；
2. 适用哪些场景；
3. 明确不解决什么；
4. 为什么下一阶段仍需要；
5. 测试和基准结果；
6. 提交与工作区状态。

## 本章小结

GSR 的开发流程与 Runtime 设计使用同一原则：让每个结论有 owner，让每次变化经过明确入口，让失败留下可追溯事实。
