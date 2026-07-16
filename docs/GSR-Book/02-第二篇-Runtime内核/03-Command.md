# Command

> 状态：已实现
>
> 规范：[RFC-0120](../../rfcs/RFC-0120-Core-Command.md)

## 唯一业务入口

```go
type Command struct {
    ID      CommandID
    Payload any
}
```

业务分发只看 `CommandID`。GSR 不引入 `PTYPE_LUA`、`PTYPE_SOCKET` 等第二套协议分发概念。

Service 通过 `CommandDeclarer` 声明接受的 Command。Runtime 在创建时复制并冻结命令集，未声明 Command 在进入 Mailbox 前返回 `ErrCommandNotRegistered`。

## Envelope

`Envelope` 是 Runtime 内部投递结构，补充 Source、Target、Session 和 CallPath：

```text
Envelope -> Mailbox -> Scheduler -> Command -> Service.Handle
```

本地投递直接携带 `Payload any`。跨节点时 `ClusterCodec` 根据 `CommandID` 编解码 Payload；Transport 只处理 bytes，不理解业务含义。

## 规则

- Timer 到期也生成 Command。
- Cluster 调用也进入同一 Command Dispatcher。
- Command Handler 不直接解析 TCP、WebSocket 或 protobuf 帧。
- 第一版保留 `Payload any`；类型安全包装和代码生成属于后续扩展。
