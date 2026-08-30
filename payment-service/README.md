# Payment Service

Payment Service is an internal Kitex RPC service. Public HTTP requests terminate at api-gateway; this service does not expose a public HTTP listener. The RPC endpoint defaults to `:8888` and keeps the existing Application/Domain/Repository implementation behind the generated contract.

StablePay AI 支付服务 - 处理 HTTP 402 协议支付流程的核心服务。

## 项目概述

Payment Service 负责：
- HTTP 402 Payment Required 协议处理
- 支付流程控制与状态管理
- 签名验证（调用 DID Service）
- 链上交易协调（调用 Blockchain Adapter）
- 支付历史查询
- 支付事件发布（RocketMQ）

## 技术栈

- **语言**: Go 1.21+
- **HTTP 框架**: [Hertz](https://github.com/cloudwego/hertz) (CloudWeGo)
- **RPC 框架**: [Kitex](https://github.com/cloudwego/kitex) (CloudWeGo)
- **数据库**: MySQL 8.0+
- **缓存**: Redis 7.0+
- **消息队列**: RocketMQ 5.0+

## 项目结构

```
payment-service/
├── api/                      # 接入层 (Adapter)
│   └── http/                 # Hertz HTTP 处理器
│       ├── handler/          # HTTP 处理器
│       └── router/           # 路由配置
├── cmd/
│   └── payment-service/      # 主入口
│       └── main.go
├── config/                   # 配置文件
│   ├── config.yaml           # 默认配置
│   └── config.example.yaml   # 配置示例
├── internal/                 # 内部实现
│   ├── adapter/              # 适配器层
│   │   ├── repository/       # 数据仓库实现
│   │   ├── rpc/              # RPC 客户端
│   │   └── mq/               # MQ 生产者
│   ├── application/          # 应用层
│   │   ├── dto/              # 数据传输对象
│   │   └── service/          # 应用服务
│   ├── domain/               # 领域层
│   │   ├── entity/           # 领域实体
│   │   ├── service/          # 领域服务
│   │   ├── vo/               # 值对象
│   │   └── repository/       # 仓库接口
│   └── infrastructure/       # 基础设施层
│       ├── config/           # 配置管理
│       ├── mysql/            # MySQL 连接
│       └── redis/            # Redis 连接
├── pkg/                      # 公共包
│   ├── constants/            # 常量定义
│   ├── errors/               # 错误码定义
│   └── utils/                # 工具函数
├── scripts/                  # 数据库脚本
│   └── init_db.sql           # 数据库初始化脚本
├── go.mod
└── README.md
```

## 快速开始

### 环境要求

- Go 1.21+
- MySQL 8.0+
- Redis 7.0+
- RocketMQ 5.0+

### 安装依赖

```bash
go mod download
```

### 配置

1. 复制配置文件
```bash
cp config/config.example.yaml config/config.yaml
```

2. 修改 `config/config.yaml` 中的数据库、Redis、RocketMQ 连接信息

### 数据库初始化

```bash
mysql -u root -p < scripts/init_db.sql
```

### 运行

```bash
go run ./cmd/server
```

### 测试

```bash
go test ./...
```

## 核心功能

### Agent Payment Harness（可选治理层）

支付服务新增了服务端 Agent Payment Harness，将 Agent 的“付款提议”与“链上执行”分离为固定的 `Normalize → Policy → Approval → Execute` DAG。它支持金额策略、商户 DID 白名单、DID 二次确认签名、短期一次性 intent 与原有支付链路的幂等衔接。详见 [Agent Payment Harness](docs/agent_payment_harness.md)。

### 支付状态机

```
CREATED -> PENDING -> CONFIRMED -> COMPLETED
   |         |          |
   v         v          v
  FAILED <- FAILED <- FAILED
   |
   v
CANCELLED
```

### HTTP 接口

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | /api/v1/pay | 发起支付 |
| GET | /api/v1/pay/:tx_id | 查询支付状态 |
| GET | /api/v1/pay/history | 查询支付历史 |
| GET | /api/v1/pay/require | 获取支付要求（HTTP 402） |

### 短链兼容（API Gateway 映射）

| 方法 | 路径 | 映射到 |
|------|------|--------|
| GET | /pay?skill=...&price=... | /api/v1/pay/require |
| GET | /verify?skill=...&agent=... | /api/v1/verify |

## 幂等性设计

- 客户端生成 `X-Idempotency-Key` 请求头
- 服务端以 `agent_did + skill_did + idempotency_key` 做幂等保护
- 幂等键有效期 30 分钟

## 安全特性

- 签名有效期检查（5分钟）
- Nonce 防重放攻击
- 金额上限限制（1000 USDC）
- 请求限流

## 错误码

| 错误码 | 说明 |
|--------|------|
| 0 | 成功 |
| 10001 | 参数错误 |
| 10004 | 签名验证失败 |
| 20001 | 余额不足 |
| 20002 | 重复支付 |
| 20003 | 区块链网络错误 |
| 20005 | 支付金额超限 |
| 30001 | 内部服务器错误 |

## 下游依赖

### DID Service
- 验证 DID 签名
- 获取 DID 对应的钱包地址

### Blockchain Adapter
- 执行链上转账
- 查询交易状态
- 查询余额

