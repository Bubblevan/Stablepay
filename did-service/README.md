# StablePay DID Service

## 1. 定位

`did-service` 是 StablePay 的 DID 权威服务。它只提供 Kitex RPC，默认监听 `:8081`，负责 DID 文档、密钥登记、DID 查询和签名验证。

它不负责支付、余额、链上交易或公网 HTTP 路由。api-gateway 和 payment-service 通过 RPC 调用它。

## 2. 责任范围

- 创建和注册 Agent/Developer DID。
- 保存 DID 公钥、钱包地址、状态、版本和元数据。
- 查询 DID Document 的当前权威信息。
- 验证签名、时间戳和 nonce。
- 通过乐观锁更新 DID 配置。
- 使用加密组件保护需要持久化的敏感数据。

## 3. RPC 与依赖

| 连接 | 协议/端口 | 用途 |
|---|---|---|
| api-gateway -> did-service | Kitex `:8081` | 公网 DID API 的内部实现 |
| payment-service -> did-service | Kitex `:8081` | 支付签名和 DID 归属校验 |
| did-service -> MySQL | TCP `3306` | DID 和配置持久化 |

## 4. 目录说明

```text
did-service/
├── cmd/server/main.go                # 唯一启动入口
├── internal/
│   ├── app/bootstrap.go              # 配置、Repository、Application、Kitex 组装
│   ├── application/did_app_service.go # DID 用例
│   ├── domain/
│   │   ├── entity/did.go             # DID 领域实体
│   │   └── gateway/did_repository.go # Repository 接口
│   ├── adapter/did_handler.go        # Kitex RPC -> Application
│   └── infrastructure/
│       ├── config/                   # YAML 与 CONFIG_PATH
│       ├── repository/               # MySQL Repository 实现
│       └── encryption/               # AES 等敏感数据保护
├── kitex_gen/                        # did-service.thrift 生成代码
├── config/                           # dev/docker 配置
├── migrations/
├── scripts/rpc-test*/                # RPC 验证工具
├── tests/
├── Dockerfile
├── Makefile
└── go.mod
```

生产环境使用数据库 Repository；内存实现只允许出现在单元测试替身中，不作为正式启动默认值。

## 5. Kitex 方法

| RPC | 作用 |
|---|---|
| `CreateDID` | 创建 DID 和初始公钥配置 |
| `RegisterDID` | 登记外部 DID |
| `GetDID` | 查询 DID Document 所需权威数据 |
| `VerifySignature` | 按 DID 公钥验证签名 |
| `UpdateDIDConfig` | 版本化更新 DID 配置 |

## 6. 配置与启动

```yaml
server:
  host: "0.0.0.0"
  port: 8081
```

```powershell
$env:CONFIG_PATH = "config/docker.yaml"
go run ./cmd/server
go test ./...
```

Docker/Kubernetes 必须通过 `CONFIG_PATH` 指向挂载配置，不应依赖工作目录下恰好存在的配置文件。

## 7. 维护规则

- DID 校验必须经过 DID Service，其他服务不得复制公钥查询和验签逻辑。
- 领域层不直接读环境变量或数据库。
- `kitex_gen/` 只能由 canonical IDL 重新生成。
- DID Document 扩展字段要保持版本兼容，并通过 `UpdateDIDConfig` 的版本控制更新。
