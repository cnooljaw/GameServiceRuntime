# Transport

> 状态：已实现
>
> 规范：[RFC-0191](../../rfcs/RFC-0191-Core-Cluster-Transport.md)

## 边界

`ClusterTransport` 负责连接、握手、WireEnvelope 帧、发送顺序和断线通知。`ClusterCodec` 负责按 CommandID 编解码业务 Payload。

Transport 不调用 Service，不管理 PendingCall，也不理解业务 Command。

## TCP 实现

`transport/tcp` 当前提供：

- 版本握手和目标 NodeID 校验。
- `uint32` 大端长度前缀与有界字段。
- 出站帧分配前大小预检。
- 持久双向连接和确定性重复连接选择。
- 同节点拨号合并，不同节点并行建连。
- 写超时、断线检测和 Unavailable 通知。

第一版使用静态 Peer 地址。自动重连、动态地址更新和 ServiceGroup 路由属于后续 Tooling。

## 信任边界

当前握手只声明 NodeID 和协议版本，不提供身份认证、完整性保护或加密。TCP Cluster 端口只能部署在可信内网，并由监听地址、防火墙或安全组阻止公网和非集群主机访问。

跨不可信网络时应在 Transport 层增加 TLS/mTLS 或等价认证，不修改 Service、Envelope、Send/Call 和 Core Runtime 接口。
