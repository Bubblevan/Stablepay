# DID Service 技术手稿

DID Service 是 DID 权威边界，Kitex 监听 `:8081`。`internal/application` 编排 DID 用例，`internal/domain` 保存实体和 Repository 接口，MySQL Repository 与加密能力位于 infrastructure，`internal/adapter` 只负责 Kitex 映射。

正式运行必须使用数据库 Repository；内存实现只允许测试替身。配置由 `CONFIG_PATH` 指定，正式入口为 `cmd/server/main.go`。DID 签名验证统一由本服务完成，Payment 和网关不复制验签规则。
