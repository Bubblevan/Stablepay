# Query Service 技术手稿

Query Service 的正式边界是 `cmd/server -> Application -> Repository/BalanceProvider -> Kitex`。它没有公网 HTTP Handler；网关的 Query Client 负责把 HTTP 参数转换为 canonical RPC 请求。

默认 RPC：`query-service:8084`。数据库默认使用独立的 `stablepay_query_db`，余额读取使用 Solana JSON-RPC。查询返回的金额使用最小单位整数和 canonical Currency 表示。

修改查询字段时，先改 `stablepayai-idl/idl/query-service.thrift`，再重新生成 query-service 和 api-gateway 的 Kitex 代码，最后执行根目录六服务测试脚本。
