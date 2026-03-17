


# Blockchain Adapter Service

**项目**: StablePay AI  
**服务名称**: `blockchain-adapter`  
**框架**: CloudWeGo (Kitex)  
**语言**: Golang 1.21+  

---

## 📖 服务简介

`blockchain-adapter` 是 StablePay AI 支付基础设施的底层核心微服务。它专门负责与 Solana 区块链网络进行交互，提供稳定、可靠的链上交易执行与状态查询能力。

本服务**不包含**任何业务规则判断和用户身份管理（DID 密钥管理已剥离至 `did-service`），是纯粹的执行引擎。其最核心的功能是实现了基于 Solana 机制的 **Gas 费代付（FeePayer 补贴）** 功能。

## ✨ 核心能力

*   **稳定币转账执行 (SPL Token)**：支持处理 USDC/USDT 等 Solana SPL Token 的链上转账。
*   **Gas 费补贴 (FeePayer)**：接收来自客户端的部分签名交易（Partial Sign Tx），使用官方热钱包（HotWallet）进行二次签名，实现代理支付网络 Gas 费。
*   **链上数据查询**：提供实时的钱包余额查询（Token Balance）和交易状态追踪（Tx Status）。
*   **补贴记录留存**：将代付的 Gas 费用明细持久化记录到数据库（`gas_subsidies` 表），供后续对账与结算使用。

---

## 📂 目录结构

```text
blockchain-adapter/
├── idl/                              # RPC 接口定义层
│   ├── common.thrift                 # 公共结构体定义
│   └── blockchain-adapter.thrift             # 本服务内部 RPC 契约定义
│
├── kitex_gen/                        # Kitex 自动生成的桩代码目录 (勿手动修改)
│
├── conf/                             # 配置文件目录
│   ├── dev.yaml                      # 开发环境配置 (MySQL DSN, Solana RPC Endpoints等)
│   └── hotwallet.json                # 🔴 StablePay 官方热钱包私钥 (仅用于 Gas 代付，严禁泄露)
│
├── cmd/
│   └── server/
│       └── main.go                   # Kitex 微服务启动入口
│
├── internal/                         # 核心业务逻辑实现
│   ├── handler/                      # RPC 接口实现层 (Controller)
│   │   ├── transfer.go               # 实现 TransferStableCoin 
│   │   ├── balance.go                # 实现 GetBalance
│   │   └── tx_status.go              # 实现 GetTxStatus
│   │
│   ├── service/                      # 业务编排层 (Service)
│   │   ├── transfer_service.go       # 解析交易、补签 Gas、上链、记录补贴
│   │   ├── balance_service.go        # 余额查询逻辑
│   │   └── tx_status_service.go      # 链上状态查询与枚举映射
│   │
│   ├── solana/                       # 区块链底层交互层
│   │   ├── client.go                 # 封装 Solana RPC Client
│   │   ├── spl_token.go              # 处理 USDC/USDT 转账解析与构建
│   │   ├── transaction.go            # 核心：交易反序列化与 FeePayer 双签逻辑
│   │   └── hotwallet.go              # 加载并管理热钱包密钥
│   │
│   └── dal/                          # 数据访问层
│       └── db/
│           ├── init.go               # MySQL GORM 初始化
│           └── gas_subsidy.go        # 记录 Gas 补贴消耗明细
│
├── pkg/                              # 公共工具包
│   └── utils/
│       └── base64_util.go            # Base64 编解码工具
│
├── go.mod                            # Go 依赖管理
└── build.sh                          # 编译与打包脚本
```

---

## 🔌 RPC 接口契约 (Thrift)

本服务通过 Kitex 暴露内部 RPC 接口，主要供 `Payment Service` 和 `Query Service` 调用。

| 接口名称 | 描述 | 核心入参 | 核心出参 |
| :--- | :--- | :--- | :--- |
| `TransferStableCoin` | 执行稳定币转账并代付 Gas | `signed_tx_base64` (前端部分签名的Base64) <br> `amount_minor` (金额) | `tx_hash` (链上交易哈希)<br> `status` (PENDING/CONFIRMED) |
| `GetBalance` | 查询钱包指定币种余额 | `wallet_address` (Base58)<br> `currency` (USDC/USDT) | `balance_minor` (最小单位余额) |
| `GetTxStatus` | 查询链上交易确认状态 | `tx_hash` (Base58 交易哈希) | `status` (CONFIRMED/FAILED) |

---

## 🔄 核心业务流：Gas 费补贴 (FeePayer) 机制

为了降低 AI Agent 生态的支付门槛，本服务实现了免 SOL 手续费支付的完整闭环。开发者对接时需严格遵循以下数据流：

1. **前端/Agent 端**：构建一笔 USDC 转账的 `Transaction`，将其 `feePayer` 强制设置为 **StablePay 官方热钱包地址**。
2. **前端/Agent 端**：使用用户自己的私钥对该 `Transaction` 进行**部分签名 (Partial Sign)**。
3. **前端 -> Payment Service**：将部分签名后的交易序列化为 `Base64` 字符串（即 `signed_tx_base64`），并附带业务验签数据发起 HTTP 请求。
4. **Payment Service -> Adapter**：Payment 业务校验通过后，通过 Kitex RPC 调用本服务的 `TransferStableCoin` 接口。
5. **Adapter 处理**：
   - 反序列化 `Base64` 恢复交易对象。
   - 使用 `conf/hotwallet.json` 中的热钱包私钥对该交易进行**二次签名 (补签 Gas)**。
   - 通过 Solana RPC 节点广播交易并等待网络确认。
   - 提取实际消耗的 Lamports，持久化到 MySQL `gas_subsidies` 表中。
6. **返回结果**：返回最终的 `TxHash`。

---

## 🛠️ 开发与运行指南

### 1. 环境准备
* 安装 Go 1.21+
* 安装 Kitex 代码生成工具：
  ```bash
  go install github.com/cloudwego/kitex/tool/cmd/kitex@latest
  go install github.com/cloudwego/thriftgo@latest
  ```

### 2. 代码生成
如需更新 Thrift 接口，请在项目根目录下执行：
```bash
kitex -module stablepay.blockchain_adapter idl/blockchain-adapter.thrift
```

### 3. 配置说明
在 `conf/dev.yaml` 中配置必要的环境变量：
```yaml
solana:
  network: "devnet" 
  rpc_endpoint: "https://api.devnet.solana.com"
  ws_endpoint: "wss://api.devnet.solana.com"

mysql:
  dsn: "user:pass@tcp(127.0.0.1:3306)/stablepay_payment_db?charset=utf8mb4&parseTime=True&loc=Local"
```
**注意**：在本地开发前，请确保 `conf/hotwallet.json` 文件存在并包含有效的测试网/主网 Ed25519 密钥对，且该钱包拥有足够的 SOL 用于支付 Gas。

### 4. 编译与启动
```bash
# 整理依赖
go mod tidy

# 运行服务
go run cmd/server/main.go
```

---

## ⚠️ 注意事项与规范
1. **热钱包安全**：`conf/hotwallet.json` 绝对不能提交到 Git 仓库，请确保其已被加入 `.gitignore`。
2. **幂等性**：Solana 链上交易自带近期区块哈希（Recent Blockhash）机制，天然具备防重放特性，若 `tx_hash` 已存在于网络，调用 `SendTransaction` 将安全地失败或返回已确认。
3. **超时控制**：上链确认具有不确定性，Kitex RPC 调用应设置合理的超时时间（建议 30s）。若触发超时，上游服务应通过 `GetTxStatus` 进行轮询补偿，而不应盲目重试转账。