# Verification Service 技术手稿

Verification Service 在 Kitex `:8085` 提供购买验证和 Purchase Proof，在 RocketMQ `payment_events` 上消费支付结果。Application 负责验证用例，MySQL Repository 保存 Purchase，Consumer 负责事件转换和幂等更新，RPC Handler 只做协议映射。

启动顺序上，RocketMQ Consumer 和数据库连接成功是服务就绪条件之一；只有 RPC 端口启动而无法消费支付事件，不应视为完整 ready。正式入口是 `cmd/server/main.go`，配置通过环境变量加载。
