# StablePay Blockchain Adapter

基于 COLA v5 架构的区块链适配器微服务，支持 Solana 网络的 SPL Token 转账和 Gas 补贴。

---

## 项目概览

| 属性 | 值 |
|------|-----|
| **架构** | COLA v5 (Clean Object-oriented & Layered Architecture) |
| **框架** | Kitex (RPC) |
| **区块链** | Solana (Devnet) |
| **数据库** | MySQL + GORM |
| **语言** | Go 1.21+ |

---

## 快速开始

### 1. 配置环境

```bash
# 复制配置模板
cp conf/dev.yaml.example conf/dev.yaml

# 编辑配置
cat conf/dev.yaml
```

```yaml
server:
  host: 0.0.0.0
  port: 8888

solana:
  network: devnet
  endpoint: https://api.devnet.solana.com
  hot_wallet_path: hotwallet/hotwallet.json
  subsidy_ratio: 1.0  # 100% 补贴，热钱包全额支付

mysql:
  dsn: "user:password@tcp(localhost:3306)/stablepay?charset=utf8mb4&parseTime=True"
```

### 2. 准备热钱包

```bash
# 生成热钱包密钥（或使用已有）
solana-keygen new --outfile hotwallet/hotwallet.json

# 请求空投（Devnet）
solana airdrop 2 $(solana-keygen pubkey hotwallet/hotwallet.json) --url devnet
```

### 3. 初始化数据库

```sql
CREATE DATABASE stablepay CHARACTER SET utf8mb4;

-- Gas 补贴表
CREATE TABLE gas_subsidies (
    id BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
    tx_id VARCHAR(255) NOT NULL,
    original_tx_hash VARCHAR(255) UNIQUE,
    gas_amount BIGINT UNSIGNED DEFAULT 0,
    subsidy_amount BIGINT UNSIGNED DEFAULT 0,
    agent_paid_amount BIGINT UNSIGNED DEFAULT 0,
    status VARCHAR(50) DEFAULT 'PENDING',
    fee_payer VARCHAR(255),
    network VARCHAR(50),
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_tx_hash (original_tx_hash),
    INDEX idx_status (status)
);
```

### 4. 启动服务

```bash
# 进入 start 模块
cd blockchain-adapter-start/cmd

# 运行
go run main.go
```

---

## 架构设计

### COLA v5 分层架构

```
┌─────────────────────────────────────────────────────────────────┐
│                        Client 层                                 │
│              Thrift IDL / DTO 定义                               │
├─────────────────────────────────────────────────────────────────┤
│                        Adapter 层                                │
│     RPC Adapter  ──►  Assembler  ──►  Command/Query              │
├─────────────────────────────────────────────────────────────────┤
│                        App 层                                    │
│     Command Service (写)  │  Query Service (读)                   │
├─────────────────────────────────────────────────────────────────┤
│                     Domain 层 ⭐ 核心                             │
│     Entity  ──►  Value Object  ──►  Domain Service  ──►  Gateway │
├─────────────────────────────────────────────────────────────────┤
│                     Infrastructure 层                            │
│     Repository  ──►  Blockchain Client  ──►  Convertor           │
├─────────────────────────────────────────────────────────────────┤
│                        Start 层                                  │
│              Dependency Injection  +  启动                        │
└─────────────────────────────────────────────────────────────────┘
```

### 目录结构

```
blockchain-adapter/
├── blockchain-adapter-client/          # Client 层
│   └── (Kitex 生成代码)
│
├── blockchain-adapter-adapter/         # Adapter 层
│   └── rpc/
│       ├── assembler/                  # 对象组装器
│       ├── transfer_rpc_adapter.go     # 转账适配器
│       ├── balance_rpc_adapter.go      # 余额适配器
│       ├── tx_status_rpc_adapter.go    # 交易状态适配器
│       └── blockchain_adapter_rpc.go   # 聚合 RPC
│
├── blockchain-adapter-app/             # App 层
│   └── service/
│       ├── transfer_cmd_service.go     # 转账命令服务
│       ├── balance_query_service.go    # 余额查询服务
│       └── tx_status_query_service.go  # 交易状态服务
│
├── blockchain-adapter-domain/ ⭐        # Domain 层（核心）
│   ├── entity/                         # 领域实体
│   │   ├── gas_subsidy_entity.go       # Gas补贴实体
│   │   └── transaction_entity.go       # 交易实体
│   ├── vo/                             # 值对象
│   │   ├── balance_vo.go
│   │   └── tx_status_vo.go
│   ├── service/                        # 领域服务
│   │   └── gas_subsidy_calculator.go   # 补贴计算器
│   └── gateway/                        # 网关接口（依赖倒置）
│       ├── repository_gateway.go       # 仓储接口
│       └── solana_gateway.go           # Solana接口
│
├── blockchain-adapter-infrastructure/  # Infrastructure 层
│   ├── persist/                        # 持久化对象
│   │   └── gas_subsidy_po.go
│   ├── repository/                     # 仓储实现
│   │   ├── db_init.go
│   │   ├── gas_subsidy_repository.go
│   │   └── convertor/
│   └── blockchain/                     # 区块链实现
│       ├── solana_gateway_impl.go
│       └── hotwallet_impl.go
│
├── blockchain-adapter-start/           # Start 层
│   └── cmd/main.go                     # 启动入口
│
├── legacy/                             # 遗留代码（Phase1-6）
│   └── ...
│
└── doc/                                # 文档
    ├── COLA_ARCHITECTURE.md            # 完整架构文档
    ├── COLA_QUICK_REFERENCE.md         # 快速参考
    ├── ARCHITECTURE_EVOLUTION.md       # 演进历程
    ├── SIMPLIFICATION_ISSUES.md        # 代码简化问题清单
    └── BUGFIX_SUMMARY.md               # Bug修复汇总
```

---

## 核心功能

### 1. 转账服务（Gas 补贴）

```
Client                    Adapter                    App                    Domain              Infrastructure
  │                         │                        │                       │                        │
  │  1. Transfer Request    │                        │                       │                        │
  │ ──────────────────────► │                        │                       │                        │
  │                         │  2. ToTransferCmd      │                       │                        │
  │                         │ ──────────────────────►│                       │                        │
  │                         │                        │ 3. NewGasSubsidyEntity│                        │
  │                         │                        │ ─────────────────────►│                        │
  │                         │                        │ 4. ExecuteTransfer    │                        │
  │                         │                        │ ──────────────────────────────────────────────►│
  │                         │                        │                       │                        │
  │                         │                        │                       │  5. Sign & Send        │
  │                         │                        │ ◄──────────────────────────────────────────────│
  │                         │                        │                       │                        │
  │                         │                        │  6. CalculateSubsidy  │                        │
  │                         │                        │ ─────────────────────►│                        │
  │                         │                        │                       │                        │
  │                         │                        │  7. Save Record       │                        │
  │                         │                        │ ◄──────────────────────────────────────────────│
  │                         │  8. ToTransferResponse │                       │                        │
  │  9. Response           │ ◄──────────────────────│                       │                        │
  │ ◄──────────────────────│                        │                       │                        │
```

### 2. Gas 补贴计算

```go
// 业务规则：100% 补贴（热钱包全额支付）
subsidyRatio := 1.0

// 实际 Gas: 0.005 SOL
actualGas := uint64(0.005 * 1e9)  // 5,000,000 lamports

// 计算
subsidyAmount := uint64(float64(actualGas) * subsidyRatio)  // 5,000,000 (平台全额承担)
agentPaidAmount := actualGas - subsidyAmount                  // 0 (Agent无需支付)

// 链上实际：热钱包支付 100% (5,000,000)
// 记录展示：平台全额补贴
```

---

## API 接口

### TransferStableCoin

```thrift
struct TransferStableCoinRequest {
    1: required RequestBase base,
    2: required string to_wallet_address,
    3: required i64 amount_minor,
    4: required string mint_address,
    5: required string signed_tx_base64,
    6: required string tx_id
}

struct TransferStableCoinResponse {
    1: required ResponseBase base,
    2: optional string tx_hash,
    3: optional string explorer_url
}
```

### GetWalletBalance

```thrift
struct GetWalletBalanceRequest {
    1: required RequestBase base,
    2: required string wallet_address,
    3: required string mint_address
}

struct GetWalletBalanceResponse {
    1: required ResponseBase base,
    2: optional string balance,
    3: optional i64 balance_minor
}
```

---

## 测试

```bash
# 单元测试
cd blockchain-adapter-domain && go test ./...
cd blockchain-adapter-app && go test ./...

# 集成测试（需要本地 MySQL 和 Solana Devnet）
cd blockchain-adapter-start/cmd && go run main.go

# 测试命令
curl -X POST http://localhost:8888 \
  -H "Content-Type: application/json" \
  -d '{
    "method": "GetWalletBalance",
    "params": {
      "wallet_address": "...",
      "mint_address": "..."
    }
  }'
```

---

## 文档索引

| 文档 | 内容 | 适合人群 |
|------|------|----------|
| [COLA_ARCHITECTURE.md](doc/COLA_ARCHITECTURE.md) | 完整架构设计 | 架构师、Senior |
| [COLA_QUICK_REFERENCE.md](doc/COLA_QUICK_REFERENCE.md) | 快速参考卡片 | 开发人员 |
| [ARCHITECTURE_EVOLUTION.md](doc/ARCHITECTURE_EVOLUTION.md) | 演进历程 | 团队了解背景 |

---

## 技术栈

| 组件 | 选择 | 说明 |
|------|------|------|
| RPC 框架 | Kitex | 字节跳动开源，高性能 |
| 协议 | Thrift | 跨语言支持 |
| 区块链 SDK | solana-go | Solana 官方 Go SDK |
| ORM | GORM | 成熟的 Go ORM |
| 架构 | COLA v5 | 阿里巴巴开源分层架构 |
| 配置 | YAML | 结构化配置 |

---

## 注意事项

### 1. Go 版本兼容性

项目使用 Go 1.21+ 开发。Kitex 依赖在 Go 1.25 开发版可能存在兼容性问题，建议使用 Go 1.21/1.22 稳定版。

### 2. 热钱包安全

- `hotwallet/hotwallet.json` 包含私钥，**请勿提交到 Git**
- 生产环境建议使用 KMS/HSM
- Devnet 测试密钥可接受，Mainnet 必须严格保护

### 3. Gas 补贴策略

- 当前实现：**100% 补贴**（热钱包全额支付 Gas 费用）
- 业务逻辑：用户无需支付任何 Gas 费用，提升用户体验
- 未来可扩展：根据用户等级动态调整补贴比例
- 风险控制：设置单日补贴上限，防止恶意刷取

---

## 贡献指南

### 添加新功能流程

1. **Domain 层**：定义 Entity、Gateway 接口
2. **Infrastructure 层**：实现 Gateway
3. **App 层**：创建 Service，编排领域逻辑
4. **Adapter 层**：创建 Adapter，处理 DTO 转换
5. **Start 层**：注入依赖，启动服务

详见 [COLA_QUICK_REFERENCE.md](doc/COLA_QUICK_REFERENCE.md)



---

*最后更新: 2026-03-17*
