# StablePay Payment Service

## 1. 定位

`payment-service` 是内部支付编排服务。它只提供 Kitex RPC，默认监听 `:8888`；公网 HTTP 请求必须先到 api-gateway。

服务保留 Application、Domain、Repository 逻辑，负责把支付请求变成可审计的支付实体，并协调 DID 验签、余额检查、链上转账、状态轮询和支付事件发布。

## 2. 责任范围

- 幂等键和 nonce 防重放。
- 支付金额、币种、签名和支付策略校验。
- 支付生命周期：`CREATED -> PENDING -> CONFIRMED -> COMPLETED`，失败进入 `FAILED/CANCELLED`。
- 调用 did-service 验证签名和 DID 归属。
- 调用 blockchain-adapter 构造或提交链上交易。
- 将支付结果发布到 RocketMQ `payment_events`。
- 保存支付、幂等记录和链上状态。

Agent Payment Harness 属于 Application 内部策略能力；X 相关旧 Application 逻辑不作为公网接口，也没有对应的 Kitex RPC。

## 3. RPC 与依赖

| 连接 | 协议/端口 | 用途 |
|---|---|---|
| api-gateway -> payment-service | Kitex `:8888` | 支付、状态、历史、支付要求 |
| payment-service -> did-service | Kitex `:8081` | DID 签名验证 |
| payment-service -> blockchain-adapter | Kitex `:8083` | 余额、转账、交易状态 |
| payment-service -> MySQL | TCP `3306` | 支付与幂等数据 |
| payment-service -> Redis | TCP `6379` | nonce、intent 和短期状态 |
| payment-service -> RocketMQ | NameServer `9876` | 发布支付事件 |

## 4. 目录说明

```text
payment-service/
├── cmd/server/main.go                # 唯一启动入口，只启动 Kitex
├── internal/
│   ├── app/                          # 当前入口由 cmd 组装
│   ├── application/
│   │   ├── dto/                      # 用例输入输出与事件 DTO
│   │   └── service/                  # 支付用例、Agent 策略
│   ├── domain/
│   │   ├── entity/                   # Payment、幂等实体
│   │   ├── service/                  # 签名、金额、nonce 等规则
│   │   ├── vo/                       # 金额、签名、分页值对象
│   │   └── repository/               # Repository 接口
│   ├── adapter/
│   │   ├── rpc/                      # Kitex 客户端与 Payment RPC Handler
│   │   ├── repository/               # GORM Repository 实现
│   │   └── mq/                       # RocketMQ Producer
│   └── infrastructure/
│       ├── config/
│       ├── mysql/
│       └── redis/
├── kitex_gen/                        # payment-service.thrift 生成代码
├── config/                           # example/local/docker 配置
├── migrations/                       # 本服务数据库迁移
├── tests/                            # 集成测试说明
├── Dockerfile
├── Makefile
└── go.mod
```

## 5. Kitex 方法

契约源：`../stablepayai-idl/idl/payment-service.thrift`。

| RPC | 作用 |
|---|---|
| `InitiatePayment` | 创建并执行支付流程 |
| `GetPaymentStatus` | 查询支付状态 |
| `ListPaymentHistory` | 查询 Agent 支付历史 |
| `GetPaymentRequirement` | 返回购买所需金额和支付说明 |

`PaymentServiceHandler` 只做协议 DTO 与 Application DTO 的转换；业务规则不能写在生成代码或 RPC Handler 中。

## 6. 配置

```yaml
server:
  rpc:
    port: "8888"

rpc_clients:
  did_service:
    address: "did-service:8081"
  blockchain_adapter:
    address: "blockchain-adapter:8083"
```

生产环境用 `CONFIG_PATH` 指向挂载配置文件。数据库、Redis、RocketMQ 和钱包相关敏感配置不得提交到仓库。

## 7. 启动与测试

```powershell
go test ./...
go run ./cmd/server
```

本服务没有 `/health` HTTP 端点；容器探活应使用 Kitex/TCP 探针或平台级 RPC 探针。

修改支付契约时：

1. 修改 `stablepayai-idl/idl/payment-service.thrift`。
2. 重新生成 api-gateway 和 payment-service 的 `kitex_gen/`。
3. 执行六服务测试与契约检查。

## 8. 维护规则

- 不在本服务新增公网 HTTP Handler。
- 不在 Application/Domain 中创建 DB、Redis、MQ、RPC 客户端。
- `kitex_gen/` 禁止手工修改。
- 所有支付结果必须能通过支付记录和事件追踪。
