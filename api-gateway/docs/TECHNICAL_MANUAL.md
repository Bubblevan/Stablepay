# api-gateway 技术手稿

api-gateway 是唯一公网 HTTP 进程，Hertz 监听 `:8080`。请求在 middleware 完成恢复、请求元数据、认证、nonce 防重放和限流后，由 `application.Dispatch` 按路由名选择下游 Kitex 客户端。

下游地址：DID `:8081`、Payment `:8888`、Query `:8084`、Verification `:8085`。每个客户端配置 RPC timeout 和 failure retry；网关不直接访问业务数据库、RocketMQ 或 Solana。

修改路由看 `internal/adapter/http/router.go` 和 `config/config.yaml`；修改 RPC 字段只能从 `stablepayai-idl/idl/` 开始，并重新生成 `kitex_gen/`。启动入口是 `cmd/server/main.go`，测试使用 `go test ./...`。
