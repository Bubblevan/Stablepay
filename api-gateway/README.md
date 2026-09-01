# StablePay API Gateway

## 1. 定位

`api-gateway` 是 StablePay 唯一的公网 HTTP 入口。它负责把 HTTP/JSON 请求转换为内部 Kitex RPC 请求，不承载 DID、支付、查询或验证领域规则。

默认监听 `:8080`，使用 Hertz。

## 2. 责任范围

- HTTP 路由和统一响应：`code/message/data/request_id/trace_id`。
- API Key、DID 签名和请求时间校验。
- `timestamp + nonce` 重放保护。
- IP、DID、路由级限流。
- 下游 Kitex 调用的超时和 failure retry。
- readiness、access log、recovery、request metadata middleware。
- `/pay`、`/verify` 等短路径兼容入口。

网关不直接访问业务数据库、Solana 或 RocketMQ；Payment 也不再通过 HTTP 调用 payment-service。

## 3. 通信端口

| 目标 | 协议 | 默认地址 | 客户端 |
|---|---|---|---|
| 公网/前端 -> api-gateway | Hertz HTTP | `:8080` | 浏览器、SDK、Agent |
| api-gateway -> did-service | Kitex | `did-service:8081` | `NewKitexDIDClient` |
| api-gateway -> payment-service | Kitex | `payment-service:8888` | `NewKitexPaymentClient` |
| api-gateway -> query-service | Kitex | `query-service:8084` | `NewKitexQueryClient` |
| api-gateway -> verification-service | Kitex | `verification-service:8085` | `NewKitexVerificationClient` |

容器部署时，`config/config.yaml` 或 `config/config.docker.yaml` 的地址必须填写服务 DNS 和 RPC 端口；宿主机端口映射不应写入业务代码。

## 4. 目录说明

```text
api-gateway/
├── cmd/server/main.go                # 唯一启动入口
├── internal/
│   ├── app/bootstrap.go               # Hertz、客户端、middleware 组装
│   ├── application/
│   │   ├── gateway.go                 # 路由到下游用例的编排
│   │   ├── contracts.go               # 下游客户端接口
│   │   └── money.go                   # HTTP 金额转最小单位
│   ├── domain/                        # RoutePolicy、Envelope、请求上下文
│   ├── adapter/http/                  # Handler、Router、Middleware
│   └── infrastructure/
│       ├── auth/                      # 签名、nonce、时间校验
│       ├── clients/                   # Kitex 下游客户端与测试替身
│       ├── config/                    # Viper 配置
│       ├── observability/             # 日志与 trace
│       ├── ratelimit/                 # Redis/内存限流
│       └── resilience/                # 超时、熔断、重试工具
├── kitex_gen/                         # DID/Payment/Query/Verification 生成代码
├── config/                            # local/docker 配置
├── tests/                             # 网关 HTTP 行为测试
├── Dockerfile
├── Makefile
└── go.mod
```

## 5. 请求流

```text
HTTP request
  -> recovery/request-meta/readiness
  -> auth/signature/nonce/rate-limit
  -> HTTP Handler
  -> application.Dispatch(route name)
  -> Kitex client
  -> downstream BaseResp
  -> HTTP response envelope
```

Payment 的 `amount` 在网关边界规范化为 `amount_minor`；Payment RPC 使用 `amount_minor`、`currency`、`signature`、`timestamp`、`nonce` 和可选 `signed_tx_base64`。

## 6. 配置

主配置：`config/config.yaml`。本地示例：`config/config.local.yaml.example`。

关键配置：

```yaml
server:
  address: ":8080"

resilience:
  default_timeout_ms: 1000
  default_retry: 1

downstream:
  did_service_addr: "did-service:8081"
  payment_service_addr: "payment-service:8888"
  verification_service_addr: "verification-service:8085"
  query_service_addr: "query-service:8084"
```

## 7. 开发与测试

```powershell
go test ./...
go run ./cmd/server -config config/config.yaml
```

健康检查：`GET /healthz`；就绪检查：`GET /readyz`。

修改 HTTP 路由时同步检查 `stablepayai-idl/openapi/`；修改内部 RPC 时只能修改 canonical IDL 并重新生成 `kitex_gen`。

## 8. 维护规则

- 网关可以有 HTTP adapter，其他业务服务默认不再添加公网 HTTP adapter。
- `kitex_gen/` 禁止手改。
- `internal/application` 不得导入 Hertz、Kitex 生成包或数据库 SDK。
- 下游客户端必须通过 `application` 接口注入，便于单测替换。
