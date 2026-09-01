# StablePay 微服务技术手稿

## 1. 系统边界

StablePay 采用“一个公共 HTTP 入口 + 多个内部 RPC 服务 + 一个事件消费者”的结构。

```text
Client
  |
  | HTTP/JSON :8080
  v
api-gateway
  |-- Kitex --> did-service          :8081
  |-- Kitex --> payment-service      :8888
  |-- Kitex --> query-service        :8084
  `-- Kitex --> verification-service :8085

payment-service -- Kitex --> blockchain-adapter :8083
payment-service -- RocketMQ event --> verification-service
blockchain-adapter -- JSON-RPC --> Solana
```

只有 `api-gateway` 面向公网提供 HTTP API。业务服务之间不通过 HTTP 互调；服务间请求使用 canonical Thrift IDL 生成的 Kitex 客户端和服务端。

## 2. 端口与通信矩阵

| 服务 | 对外角色 | 协议 | 默认端口 | 主要下游 |
|---|---|---|---:|---|
| api-gateway | 公网入口 | Hertz HTTP | 8080 | DID、Payment、Query、Verification |
| did-service | DID 权威服务 | Kitex RPC | 8081 | MySQL |
| payment-service | 支付编排服务 | Kitex RPC | 8888 | DID、Blockchain、MySQL、Redis、RocketMQ |
| blockchain-adapter | 链适配服务 | Kitex RPC | 8083 | Solana RPC、MySQL |
| query-service | 查询服务 | Kitex RPC | 8084 | MySQL、Solana RPC（余额） |
| verification-service | 购买验证服务 | Kitex RPC | 8085 | MySQL、RocketMQ |

端口是容器内部服务发现使用的端口，不等同于宿主机映射端口。网关配置中的 `*_service_addr` 必须使用容器 DNS 名称和上述 RPC 端口。

## 3. 契约源与生成代码

```text
stablepayai-idl/
├── idl/       # 唯一 Thrift 契约源
├── openapi/   # 公网 HTTP 描述
├── events/    # RocketMQ 事件样例与约定
└── scripts/   # 生成及契约检查
```

当前 RPC 契约：

- `did-service.thrift`：创建、注册、查询 DID，验签，更新 DID 配置。
- `payment-service.thrift`：发起支付、查询状态、支付历史、支付要求。
- `query-service.thrift`：余额、交易、收入、销售记录查询。
- `verification-service.thrift`：购买验证、批量验证、Purchase Proof。
- `blockchain-adapter.thrift`：转账、余额、交易状态、构造交易、提交签名交易。

每个服务的 `kitex_gen/` 都是生成物，禁止手工修改。修改流程是：修改 canonical IDL、重新生成所有受影响服务、执行契约检查和六服务测试。

## 4. 服务内部分层

标准服务目录如下：

```text
<service>/
├── cmd/server/main.go       # 唯一正式启动入口
├── internal/
│   ├── app/                 # 依赖组装、生命周期
│   ├── application/         # 用例编排、DTO
│   ├── domain/              # 实体、值对象、领域规则、仓储接口
│   ├── adapter/             # RPC、消息、持久化等外部适配
│   └── infrastructure/     # 数据库、缓存、链节点、配置、观测
├── kitex_gen/               # IDL 生成代码，只读
├── config/                  # example/local/docker 配置
├── migrations/              # 本服务数据库变更
├── scripts/                 # 运维及测试辅助脚本
├── tests/                   # 集成/契约测试说明
├── Dockerfile
├── Makefile
└── go.mod
```

依赖方向必须保持：`adapter -> application -> domain`；`infrastructure` 实现 domain/application 所需接口；`cmd/server` 负责组装，不在领域层读取环境变量或创建数据库连接。

## 5. 请求与事件边界

- HTTP 请求只在网关完成认证、签名元数据提取、nonce 防重放、限流、统一响应包装和路由分发。
- Kitex 请求使用对应 IDL 的 `BaseReq`，响应使用 `BaseResp`。
- 金额在业务边界转换为最小单位整数；链上 token 精度由 blockchain-adapter 处理。
- 支付状态通过 Payment Application 产生，通过 RocketMQ 发布支付事件。
- verification-service 只消费支付事件并更新购买记录，不反向调用 payment-service 的旧 HTTP 接口。
- `stablepay-common` 只用于非通信基础能力；不得把其类型当作 RPC 契约源。

## 6. 启动与测试

单服务：

```powershell
go test ./...
go run ./cmd/server
```

全服务契约与单测：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-six-services.ps1
```

该检查包含六个模块的 `go test ./...` 和 canonical IDL/生成接口/旧契约副本检查。它不是带真实 MySQL、Redis、RocketMQ、Solana 节点的端到端验收；真实环境验收需要另行启动基础设施。

## 7. 不允许的结构

- 在服务根目录保留第二个生产 `main.go`。
- 在 payment、query、verification 等内部服务重新暴露公网 HTTP API。
- 复制一份 `idl/` 或 `stablepayai-idl/` 到服务目录。
- 手工修改 `kitex_gen/`。
- 在 Application/Domain 中直接创建 DB、Redis、MQ、Solana 客户端。
- 用“永远返回成功”的 mock 校验替代生产链路校验。
