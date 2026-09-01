# StablePay Blockchain Adapter

## 1. 定位

`blockchain-adapter` 是链域适配服务，当前实现面向 Solana SPL Token。它只提供 Kitex RPC，默认监听 `:8083`，隔离钱包、交易格式、签名、fee payer、Solana JSON-RPC 和链上状态细节。

Payment Service 不应直接依赖 Solana SDK；所有链上操作通过本服务完成。

## 2. 责任范围

- 构造 USDC/USDT SPL Token 转账交易。
- 校验真实 Solana transaction 编码、账户、指令和签名。
- 校验 fee payer 与预期钱包一致。
- 接受用户部分签名交易，由 hot wallet 补充 fee payer 签名。
- 提交签名交易并查询交易状态。
- 查询 token 余额。
- 记录 gas subsidy 和交易审计信息。

## 3. RPC 与依赖

| 连接 | 协议/端口 | 用途 |
|---|---|---|
| payment-service -> blockchain-adapter | Kitex `:8083` | 支付转账、余额和状态 |
| blockchain-adapter -> Solana | JSON-RPC/HTTPS `443` | 构造、签名、提交和查询交易 |
| blockchain-adapter -> MySQL | TCP `3306` | gas subsidy 与交易记录 |

## 4. 目录说明

```text
blockchain-adapter/
├── cmd/server/main.go                 # 唯一启动入口
├── internal/
│   ├── application/service/           # Command/Query 用例
│   │   ├── transfer_cmd_service.go   # 转账与格式校验
│   │   ├── build_tx_service.go       # 构造未签名交易
│   │   ├── submit_tx_service.go      # 提交已签名交易
│   │   ├── balance_query_service.go
│   │   └── tx_status_query_service.go
│   ├── domain/
│   │   ├── entity/                   # Transaction、GasSubsidy
│   │   ├── gateway/                  # Solana/Repository 接口
│   │   ├── service/                  # gas subsidy 领域规则
│   │   └── vo/                       # 链状态和值对象
│   ├── adapter/rpc/                  # Kitex RPC Handler 与 assembler
│   └── infrastructure/
│       ├── blockchain/               # Solana SDK、hot wallet、交易校验
│       ├── repository/               # GORM Repository
│       └── persist/                  # 持久化对象
├── kitex_gen/                        # blockchain-adapter.thrift 生成代码
├── config/                           # dev/docker/hot wallet 示例
├── migrations/
├── tests/
├── vendor/                           # go mod vendor 依赖快照
├── Dockerfile
├── Makefile
└── go.mod
```

## 5. Kitex 方法

| RPC | 作用 |
|---|---|
| `TransferStableCoin` | 执行或提交稳定币转账 |
| `GetBalance` | 查询钱包 token 余额 |
| `GetTxStatus` | 查询交易状态 |
| `BuildUnsignedTransaction` | 构造待签名交易 |
| `SubmitSignedTransaction` | 校验并提交已签名交易 |

契约源：`../stablepayai-idl/idl/blockchain-adapter.thrift`。

## 6. 交易安全边界

生产校验不是占位成功：

1. 解码 base64 transaction 并验证 canonical bytes。
2. 检查账户、header、签名槽位、recent blockhash 和 instruction 索引。
3. 对已有非零签名执行 Ed25519 校验。
4. 校验 fee payer、from signer、目标 token account、mint 和转账数量。
5. 部分签名交易只允许缺少待补充的 fee payer 签名。
6. 任何格式、签名、账户或金额不一致都返回错误，不提交交易。

## 7. 配置与启动

```yaml
server:
  host: "0.0.0.0"
  port: 8083

solana:
  network: "devnet"
  rpc_endpoint: "https://api.devnet.solana.com"
  hotwallet_path: "config/hotwallet.json"
```

```powershell
go run ./cmd/server -config config/dev.yaml
go test ./...
go mod tidy
go mod vendor
```

热钱包私钥只能通过受控文件或 Secret 挂载；`config/hotwallet.json.example` 不是可用私钥。

## 8. 维护规则

- Solana SDK 只能出现在 infrastructure/blockchain。
- RPC Handler 不实现交易业务规则。
- `kitex_gen/` 禁止手工修改。
- 修改交易语义时必须同步 canonical IDL、交易校验测试和 Payment 调用方。
- `vendor/` 与 `go.mod/go.sum` 必须一起更新并通过默认 vendor 构建。
