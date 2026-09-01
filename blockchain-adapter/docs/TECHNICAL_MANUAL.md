# Blockchain Adapter 技术手稿

Blockchain Adapter 在 Kitex `:8083` 提供链域能力，把 Solana SDK 和钱包细节隔离在 `internal/infrastructure/blockchain`。Application 层负责转账、构造交易、提交交易、余额和状态查询；Domain 层负责交易与 gas subsidy 规则；RPC Adapter 负责 canonical IDL 映射。

所有用户部分签名交易在提交或补签前都要校验真实 transaction bytes、账户、指令、fee payer、签名和转账数量。Hot wallet 私钥只由 infrastructure 读取。正式入口是 `cmd/server/main.go`，依赖版本更新后必须同步 `go.mod/go.sum/vendor` 并执行测试。
