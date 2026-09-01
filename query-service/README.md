# StablePay Query Service

## 1. 定位

`query-service` 是只读查询服务。它只提供 Kitex RPC，默认监听 `:8084`，不提供正式 HTTP API；所有公网查询由 api-gateway 转换后调用 RPC。

## 2. 责任范围

- 查询 Agent 余额摘要。
- 查询交易流水。
- 查询 Skill 收入汇总和销售记录。
- 从 MySQL 读取业务交易数据，并在余额摘要场景调用 Solana balance provider。

该服务不写支付状态、不消费支付事件，也不参与签名认证和限流。

## 3. RPC 与依赖

| 连接 | 协议/端口 | 用途 |
|---|---|---|
| api-gateway -> query-service | Kitex `:8084` | 公网查询 API 的内部实现 |
| query-service -> MySQL | TCP `3306` | 交易和销售数据 |
| query-service -> Solana RPC | HTTPS `443` | 链上余额查询 |

## 4. 目录说明

```text
query-service/
├── cmd/server/main.go                 # 唯一启动入口，只启动 Kitex
├── internal/
│   ├── application/query_service.go  # 查询用例与分页编排
│   ├── domain/
│   │   ├── entity/transaction.go     # 查询所需领域实体
│   │   └── repository.go              # 查询 Repository 接口
│   ├── adapter/rpc/handler.go         # Kitex RPC -> Application
│   └── infrastructure/
│       ├── config/                    # 环境变量配置
│       ├── persistence/               # MySQL Repository
│       └── external/                  # Solana balance provider
├── kitex_gen/                         # query-service.thrift 生成代码
├── config/config.example.yaml
├── migrations/
├── tests/
├── Dockerfile
├── Makefile
└── go.mod
```

## 5. Kitex 方法

| RPC | 作用 |
|---|---|
| `GetBalanceSummary` | 查询余额、月度消费和限额 |
| `ListTransactions` | 分页查询交易流水 |
| `GetRevenueSummary` | 查询 Skill 收入汇总和趋势 |
| `ListSales` | 分页查询销售记录 |

## 6. 配置与启动

```text
QUERY_RPC_ADDRESS=:8084
QUERY_MYSQL_DSN=...
SOLANA_RPC_ENDPOINT=https://api.devnet.solana.com
QUERY_BALANCE_USDC_MINT=4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU
```

```powershell
go run ./cmd/server
go test ./...
```

`config/config.example.yaml` 中的旧 `http_address` 不属于正式接口；服务启动代码只读取 `QUERY_RPC_ADDRESS`。

## 7. 维护规则

- 查询用例保持只读，不在 Query Service 创建支付或购买记录。
- 分页统一使用 canonical IDL 的 `PageRequest/PageResult`。
- Solana RPC 调用必须通过 `infrastructure/external` 注入。
- `kitex_gen/` 禁止手工修改。
