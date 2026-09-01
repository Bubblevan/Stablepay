# StablePay Verification Service

## 1. 定位

`verification-service` 是购买权属和 Purchase Proof 服务。它只提供 Kitex RPC，默认监听 `:8085`，并通过 RocketMQ 消费 payment-service 发布的支付事件。

公网验证请求由 api-gateway 接收；verification-service 不提供旧 M0 HTTP API，也不通过 HTTP 调用 payment-service。

## 2. 责任范围

- 根据 Agent DID、Skill DID 和支付记录验证购买状态。
- 提供单条和批量购买验证。
- 生成 Purchase Proof，供调用方审计购买事实。
- 消费 `payment_events`，将确认/失败事件幂等写入购买记录。
- 维护购买记录的状态和时间。

## 3. RPC 与事件

| 连接 | 协议/端口 | 用途 |
|---|---|---|
| api-gateway -> verification-service | Kitex `:8085` | 验证和 Proof 查询 |
| verification-service <- payment-service | RocketMQ `payment_events` | 支付状态事件 |
| verification-service -> MySQL | TCP `3306` | Purchase 记录 |

事件消费是最终一致链路：支付成功事件先由 payment-service 发布，再由 verification-service 更新购买状态。重复消息必须安全，不应重复创建购买记录。

## 4. 目录说明

```text
verification-service/
├── cmd/server/main.go                 # 唯一启动入口
├── internal/
│   ├── app/bootstrap.go               # DB、Consumer、Application、Kitex 组装
│   ├── application/service.go         # 验证、批量验证、Purchase Proof 用例
│   ├── domain/
│   │   ├── entity/purchase.go          # Purchase 领域实体
│   │   └── repository.go               # Repository 接口
│   ├── adapter/rpc/handler.go          # Kitex RPC -> Application
│   └── infrastructure/
│       ├── config/                    # RPC、DB、RocketMQ 配置
│       ├── persistence/mysql/         # MySQL Repository
│       └── messaging/                 # RocketMQ Consumer
├── kitex_gen/                         # verification-service.thrift 生成代码
├── config/
├── migrations/
├── scripts/                           # 数据修复与 RPC 验证工具
├── tests/
├── Dockerfile
├── Makefile
└── go.mod
```

## 5. Kitex 方法

| RPC | 作用 |
|---|---|
| `VerifyPurchase` | 验证一个 Agent 是否购买 Skill |
| `BatchVerifyPurchase` | 批量验证购买状态 |
| `GetPurchaseProof` | 获取可审计的 Purchase Proof |

## 6. 配置与启动

```text
VERIFICATION_RPC_ADDRESS=:8085
VERIFICATION_MYSQL_DSN=...
ROCKETMQ_NAMESERVER=rocketmq-nameserver:9876
VERIFICATION_ROCKETMQ_GROUP=verification_group
VERIFICATION_ROCKETMQ_TOPIC=payment_events
```

```powershell
go run ./cmd/server
go test ./...
```

生产环境必须同时保证 RocketMQ Consumer 已连接；仅启动 Kitex 端口而没有事件消费，不算服务就绪。

## 7. 维护规则

- Purchase 状态由支付事件和本地 Repository 驱动，不能用内存状态替代生产数据库。
- 消费者必须按事件唯一标识实现幂等。
- Proof 字段变更必须同步 canonical IDL 和网关响应映射。
- `kitex_gen/` 禁止手工修改。
