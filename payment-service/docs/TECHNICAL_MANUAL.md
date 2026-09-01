# Payment Service 技术手稿

Payment Service 是支付用例的唯一执行边界，Kitex 监听 `:8888`。Application 保留支付状态、幂等、nonce、策略、链上执行和事件发布逻辑；Domain 保存 Payment 规则与接口；Repository、Redis、RocketMQ 和外部 RPC 客户端由启动层注入。

请求路径是 `api-gateway -> Payment Kitex Handler -> Application -> DID/Blockchain/DB/MQ`。支付成功或失败后发布 `payment_events`，由 verification-service 消费。Payment Service 没有公网 HTTP 端口，正式入口是 `cmd/server/main.go`。
