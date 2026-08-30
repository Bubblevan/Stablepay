# Blockchain Adapter Service - 详细技术设计文档

**版本**: v1.0  
**日期**: 2026-03-15  
**服务**: StablePay Blockchain Adapter  
**语言**: Go 1.21+  

---

## 目录

1. [总体架构设计](#1-总体架构设计)
2. [文件级详细设计文档](#2-文件级详细设计文档)
3. [接口控制文档](#3-接口控制文档)
4. [配置与构建指南](#4-配置与构建指南)
5. [测试策略](#5-测试策略)

---

## 1. 总体架构设计

### 1.1 系统上下文图

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                                    外部系统                                       │
│  ┌──────────────┐      ┌──────────────────┐      ┌─────────────────────────────┐ │
│  │   Solana     │      │   MySQL          │      │   Payment Service           │ │
│  │   Network    │      │   Database       │      │   (Upstream)                │ │
│  │              │      │                  │      │                             │ │
│  │  • Devnet    │◄────►│  • gas_subsidies │      │  • Initiate payment         │ │
│  │  • Mainnet   │RPC   │  • tx_records    │◄────►│  • Query balance            │ │
│  │              │      │                  │      │  • Check tx status          │ │
│  └──────────────┘      └──────────────────┘      └─────────────────────────────┘ │
│         ▲                      ▲                            │                   │
│         │                      │                            │ Kitex RPC         │
│         │                      │                            ▼                   │
└─────────┼──────────────────────┼────────────────────────────────────────────────┘
          │                      │
          │                      │
┌─────────┼──────────────────────┼────────────────────────────────────────────────┐
│         │                      │                                                │
│         │      ┌───────────────┴────────────────────────────────────┐          │
│         │      │          Blockchain Adapter Service                 │          │
│         │      │                                                   │          │
│         │      │  ┌─────────────────────────────────────────────┐  │          │
│         │      │  │         Handler Layer (接口层)               │  │          │
│         │      │  │  • TransferHandler                           │  │          │
│         │      │  │  • BalanceHandler                            │  │          │
│         │      │  │  • TxStatusHandler                           │  │          │
│         │      │  └────────────────────┬────────────────────────┘  │          │
│         │      │                       │                           │          │
│         │      │  ┌────────────────────▼────────────────────────┐  │          │
│         │      │  │        Service Layer (业务层)               │  │          │
│         │      │  │  • TransferService                           │  │          │
│         │      │  │  • BalanceService                            │  │          │
│         │      │  │  • TxStatusService                           │  │          │
│         │      │  └────────────────────┬────────────────────────┘  │          │
│         │      │                       │                           │          │
│         │      │  ┌────────────────────▼────────────────────────┐  │          │
│         │      │  │         DAL / Client Layer                  │  │          │
│         └──────┼──┤  • GasSubsidyDAL (MySQL)                    │  │          │
│                │  │  • Solana Client (RPC)                      │  │          │
│                │  │  • HotWallet Manager                        │  │          │
│                │  └─────────────────────────────────────────────┘  │          │
│                │                                                   │          │
│                └───────────────────────────────────────────────────┘          │
│                                                                                │
└────────────────────────────────────────────────────────────────────────────────┘
```

### 1.2 架构图（分层展示）

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Application Layer                               │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                     cmd/server/main.go                               │    │
│  │  - 服务启动入口                                                      │    │
│  │  - 依赖注入与初始化                                                   │    │
│  │  - 生命周期管理                                                      │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
├─────────────────────────────────────────────────────────────────────────────┤
│                              Handler Layer                                   │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐          │
│  │ transfer.go      │  │ balance.go       │  │ tx_status.go     │          │
│  │                  │  │                  │  │                  │          │
│  │ TransferHandler  │  │ BalanceHandler   │  │ TxStatusHandler  │          │
│  │ - 参数校验       │  │ - 参数校验       │  │ - 参数校验       │          │
│  │ - 类型转换       │  │ - 类型转换       │  │ - 类型转换       │          │
│  │ - 错误映射       │  │ - 错误映射       │  │ - 错误映射       │          │
│  └──────────────────┘  └──────────────────┘  └──────────────────┘          │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                         handler.go                                  │    │
│  │              BlockchainAdapterHandler (聚合Handler)                  │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
├─────────────────────────────────────────────────────────────────────────────┤
│                              Service Layer                                   │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐          │
│  │ transfer_service │  │ balance_service  │  │ tx_status_service│          │
│  │                  │  │                  │  │                  │          │
│  │ TransferService  │  │ BalanceService   │  │ TxStatusService  │          │
│  │ - 转账流程编排   │  │ - 余额查询       │  │ - 状态查询       │          │
│  │ - Gas补贴计算    │  │ - 单位转换       │  │ - 状态同步       │          │
│  │ - 交易签名       │  │ - 批量查询       │  │ - 批量同步       │          │
│  └──────────────────┘  └──────────────────┘  └──────────────────┘          │
├─────────────────────────────────────────────────────────────────────────────┤
│                           Data Access Layer                                  │
│  ┌──────────────────────────┐  ┌─────────────────────────────────────────┐  │
│  │  db/gas_subsidy.go       │  │  db/init.go                             │  │
│  │  GasSubsidyDAL           │  │  - 数据库连接初始化                      │  │
│  │  - CRUD操作             │  │  - 连接池配置                            │  │
│  │  - 统计查询             │  │  - 自动迁移                              │  │
│  └──────────────────────────┘  └─────────────────────────────────────────┘  │
├─────────────────────────────────────────────────────────────────────────────┤
│                         Blockchain Client Layer                              │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐          │
│  │ client.go        │  │ transaction.go   │  │ hotwallet.go     │          │
│  │                  │  │                  │  │                  │          │
│  │ Client           │  │ TransactionBuilder│  │ HotWallet        │          │
│  │ - RPC调用        │  │ - 交易构建       │  │ - 密钥管理       │          │
│  │ - 余额查询       │  │ - 签名           │  │ - 签名           │          │
│  │ - 交易发送       │  │ - FeePayer设置   │  │ - 验证           │          │
│  └──────────────────┘  └──────────────────┘  └──────────────────┘          │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                         spl_token.go                                │    │
│  │              SPL Token支持 (USDC/USDT)                              │    │
│  │  - ATA计算                                                         │    │
│  │  - Token转账指令构建                                                │    │
│  │  - 金额单位转换                                                     │    │
│  └─────────────────────────────────────────────────────────────────────┘    │
├─────────────────────────────────────────────────────────────────────────────┤
│                           Infrastructure Layer                               │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐          │
│  │ Kitex (RPC)      │  │ GORM (MySQL)     │  │ solana-go (SDK)  │          │
│  │                  │  │                  │  │                  │          │
│  │ - 服务注册       │  │ - ORM映射        │  │ - RPC客户端      │          │
│  │ - 序列化         │  │ - 连接池         │  │ - 交易构建       │          │
│  │ - 网络通信       │  │ - 查询构建       │  │ - 加密操作       │          │
│  └──────────────────┘  └──────────────────┘  └──────────────────┘          │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 1.3 模块划分

| 模块名 | 职责 | 源文件 | 依赖模块 |
|--------|------|--------|----------|
| **启动模块** | 服务初始化与生命周期管理 | `cmd/server/main.go` | 所有其他模块 |
| **Handler模块** | RPC接口实现，参数校验与响应构造 | `internal/handler/*.go` | Service模块, Kitex |
| **Service模块** | 业务逻辑编排，流程控制 | `internal/service/*.go` | DAL模块, Solana模块 |
| **DAL模块** | 数据访问，MySQL操作 | `data-access-layer/db/*.go` | GORM, MySQL |
| **SolanaClient模块** | 区块链RPC交互 | `internal/solana/client.go` | solana-go |
| **Transaction模块** | 交易构建与签名 | `internal/solana/transaction.go` | solana-go, crypto |
| **HotWallet模块** | 热钱包密钥管理 | `internal/solana/hotwallet.go` | base58, solana-go |
| **SPLToken模块** | 代币转账支持 | `internal/solana/spl_token.go` | solana-go |

### 1.4 关键设计决策

#### 1.4.1 技术选型

| 决策项 | 选择 | 理由 |
|--------|------|------|
| **RPC框架** | CloudWeGo Kitex | 字节跳动开源，高性能，支持Thrift |
| **数据库** | MySQL + GORM | 成熟稳定，GORM开发效率高 |
| **区块链SDK** | solana-go | 社区最成熟的Go SDK |
| **配置格式** | YAML | 人类可读，支持注释 |
| **密钥存储** | JSON文件 (当前) | POC阶段，生产环境应使用KMS |

#### 1.4.2 设计模式

| 模式 | 应用场景 | 实现文件 |
|------|----------|----------|
| **分层架构** | 整体架构 | 所有文件 |
| **依赖注入** | 服务初始化 | `main.go` |
| **建造者模式** | 交易构建 | `transaction.go` |
| **仓储模式** | 数据访问 | `gas_subsidy.go` |
| **策略模式** | 错误映射 | `handler/*.go` |

#### 1.4.3 Gas补贴机制

```
设计决策: 热钱包全额补贴 (100%)

理由:
1. 降低用户门槛 - 用户无需持有SOL
2. 简化实现 - 无需计算复杂分摊
3. 商业模型 - 平台承担Gas成本换取用户增长

实现:
- FeePayer = 热钱包地址
- 双签名交易: 用户签名 + 热钱包签名
- 记录实际Gas费用到数据库
```

---

## 2. 文件级详细设计文档

---

### 2.1 `cmd/server/main.go`

#### 2.1.1 文件头信息

| 属性 | 说明 |
|------|------|
| **文件作用** | 微服务启动入口，负责依赖注入和生命周期管理 |
| **所属模块** | 启动模块 |
| **依赖关系** | 依赖所有内部模块（handler, service, db, solana） |

#### 2.1.2 数据结构定义

```go
// Config 服务总配置
type Config struct {
    Solana SolanaConfig `yaml:"solana"`
    MySQL  MySQLConfig  `yaml:"mysql"`
}

// SolanaConfig Solana网络配置
type SolanaConfig struct {
    Network       string `yaml:"network"`        // devnet/testnet/mainnet
    RPCEndpoint   string `yaml:"rpc_endpoint"`   // RPC节点地址
    WSEndpoint    string `yaml:"ws_endpoint"`    // WebSocket地址
    HotWalletPath string `yaml:"hotwallet_path"` // 热钱包配置文件路径
}

// MySQLConfig 数据库配置
type MySQLConfig struct {
    DSN string `yaml:"dsn"` // 数据库连接字符串
}
```

#### 2.1.3 函数接口定义

**主函数**

```go
func main()
```
- **功能**: 服务启动入口
- **执行流程**:
  1. 解析命令行参数（配置文件路径）
  2. 加载YAML配置
  3. 初始化数据库连接
  4. 初始化Solana客户端
  5. 加载热钱包
  6. 初始化DAL和Services
  7. 初始化Handlers
  8. 启动Kitex RPC服务
- **前置条件**: 配置文件存在且格式正确
- **后置条件**: RPC服务监听端口，等待请求
- **注意事项**: 失败会调用log.Fatalf，程序退出

**配置加载函数**

```go
func loadConfig(path string) (*Config, error)
```
- **功能**: 从YAML文件加载配置
- **参数**: `path` - 配置文件路径
- **返回值**: `(*Config, error)` - 配置对象或错误
- **前置条件**: 文件存在且可读
- **后置条件**: 返回的配置包含默认值

#### 2.1.4 内部数据流与依赖关系

```
main()
│
├─► loadConfig() ──► Config对象
│
├─► db.Init() ──► *gorm.DB ──► db.AutoMigrate()
│
├─► solana.NewClient() ──► *Client
│
├─► solana.LoadHotWallet() ──► *HotWallet ──► Validate()
│
├─► db.NewGasSubsidyDAL() ──► *GasSubsidyDAL
│   │
│   └─► *gorm.DB
│
├─► service.NewTransferService() ──► *TransferService
│   │
│   ├─► *Client
│   ├─► *HotWallet
│   └─► *GasSubsidyDAL
│
├─► service.NewBalanceService() ──► *BalanceService
│   │
│   └─► *Client
│
├─► service.NewTxStatusService() ──► *TxStatusService
│   │
│   ├─► *Client
│   └─► *GasSubsidyDAL
│
├─► handler.NewTransferHandler() ──► *TransferHandler
│   │
│   └─► *TransferService
│
├─► handler.NewBalanceHandler() ──► *BalanceHandler
│   │
│   └─► *BalanceService
│
├─► handler.NewTxStatusHandler() ──► *TxStatusHandler
│   │
│   └─► *TxStatusService
│
├─► handler.NewBlockchainAdapterHandler() ──► *BlockchainAdapterHandler
│   │
│   ├─► *TransferHandler
│   ├─► *BalanceHandler
│   └─► *TxStatusHandler
│
└─► blockchainadapterservice.NewServer() ──► server.Server
    │
    └─► *BlockchainAdapterHandler
```

---

### 2.2 `internal/solana/client.go`

#### 2.2.1 文件头信息

| 属性 | 说明 |
|------|------|
| **文件作用** | Solana RPC客户端封装，提供网络交互能力 |
| **所属模块** | SolanaClient模块 |
| **依赖关系** | `github.com/gagliardetto/solana-go`, `github.com/gagliardetto/solana-go/rpc` |

#### 2.2.2 数据结构定义

```go
// Network Solana网络类型
type Network string

const (
    DevNet      Network = "devnet"       // 开发网
    LocalNet    Network = "localnet"     // 本地测试网
    MainNetBeta Network = "mainnet-beta" // 主网
)

// Client Solana RPC客户端
type Client struct {
    rpcClient *rpc.Client  // 底层RPC客户端
    network   Network      // 当前网络类型
    endpoint  string       // RPC端点URL
}
```

#### 2.2.3 函数接口定义

**构造函数**

```go
func NewClient(network string) (*Client, error)
```
- **功能**: 创建Solana RPC客户端
- **参数**: `network` - 网络类型（"devnet"/"localnet"/"mainnet-beta"）
- **返回值**: `(*Client, error)` - 客户端实例或错误
- **前置条件**: 网络类型有效
- **后置条件**: 客户端可连接到指定网络
- **错误处理**: 无效网络类型返回错误

**余额查询**

```go
func (c *Client) GetBalance(address string) (uint64, error)
```
- **功能**: 查询账户SOL余额
- **参数**: `address` - Base58编码的钱包地址
- **返回值**: `(uint64, error)` - 余额（lamports）或错误
- **前置条件**: 地址格式正确
- **超时**: 10秒
- **Commitment**: Finalized（最安全的确认级别）

**Token余额查询**

```go
func (c *Client) GetTokenBalance(walletAddress string, mintAddress string) (uint64, error)
```
- **功能**: 查询SPL Token余额（USDC/USDT）
- **参数**: 
  - `walletAddress` - 钱包地址
  - `mintAddress` - Token Mint地址
- **返回值**: `(uint64, error)` - Token余额或错误
- **实现逻辑**: 
  1. 计算Associated Token Account (ATA)地址
  2. 查询ATA余额
  3. ATA不存在返回0

**获取Blockhash**

```go
func (c *Client) GetRecentBlockhash() (solana.Hash, error)
```
- **功能**: 获取最新blockhash用于交易
- **返回值**: `(solana.Hash, error)` - Blockhash或错误
- **说明**: Blockhash有效期约90秒
- **超时**: 10秒

**发送交易**

```go
func (c *Client) SendTransaction(tx *solana.Transaction) (string, error)
```
- **功能**: 发送已签名的交易到网络
- **参数**: `tx` - 完整签名的交易对象
- **返回值**: `(string, error)` - 交易签名（Base58）或错误
- **前置条件**: 交易已完整签名
- **超时**: 30秒

**等待确认**

```go
func (c *Client) WaitForConfirmation(sig string, timeout time.Duration) (*rpc.GetTransactionResult, error)
```
- **功能**: 轮询等待交易确认
- **参数**:
  - `sig` - 交易签名
  - `timeout` - 最大等待时间
- **返回值**: `(*rpc.GetTransactionResult, error)` - 交易详情或超时错误
- **轮询间隔**: 3秒

#### 2.2.4 内部数据流

```
GetBalance(address)
│
├─► solana.PublicKeyFromBase58(address) ──► PublicKey
│
├─► rpcClient.GetBalance(ctx, pubKey, CommitmentFinalized)
│   │
│   └─► HTTP POST to Solana RPC
│
└─► result.Value ──► uint64

GetTokenBalance(wallet, mint)
│
├─► solana.PublicKeyFromBase58(wallet) ──► walletPubKey
│
├─► solana.PublicKeyFromBase58(mint) ──► mintPubKey
│
├─► solana.FindAssociatedTokenAddress(walletPubKey, mintPubKey) ──► ata
│
├─► rpcClient.GetTokenAccountBalance(ctx, ata, CommitmentFinalized)
│
└─► parse result.Value.Amount ──► uint64
```

---

### 2.3 `internal/solana/transaction.go`

#### 2.3.1 文件头信息

| 属性 | 说明 |
|------|------|
| **文件作用** | Solana交易构建、签名和序列化 |
| **所属模块** | Transaction模块 |
| **依赖关系** | `solana-go`, `solana-go/programs/system`, `base58` |

#### 2.3.2 数据结构定义

```go
// TransactionBuilder 交易构建器（无状态）
type TransactionBuilder struct{}

// 预估交易费用常量
const DEFAULT_TRANSACTION_FEE = 5000 // lamports
```

#### 2.3.3 函数接口定义

**构建SOL转账交易**

```go
func (tb *TransactionBuilder) BuildSOLTransferTx(
    from solana.PublicKey,
    to solana.PublicKey,
    amountLamports uint64,
    recentBlockHash solana.Hash,
) (*solana.Transaction, error)
```
- **功能**: 构建原生SOL转账交易
- **参数**:
  - `from` - 付款方公钥
  - `to` - 收款方公钥
  - `amountLamports` - 转账金额（lamports）
  - `recentBlockHash` - 最新blockhash
- **返回值**: `(*solana.Transaction, error)` - 未签名交易或错误
- **指令**: `SystemProgram.Transfer`

**构建带FeePayer的SOL转账**

```go
func (tb *TransactionBuilder) BuildSOLTransferTxWithFeePayer(
    from solana.PublicKey,
    to solana.PublicKey,
    feePayer solana.PublicKey,
    amountLamports uint64,
    recentBlockHash solana.Hash,
) (*solana.Transaction, error)
```
- **功能**: 构建带自定义FeePayer的交易（Gas补贴核心）
- **关键逻辑**: `tx.Message.AccountKeys[0] = feePayer`
- **说明**: AccountKeys[0] 是手续费的支付账户

**Base64序列化/反序列化**

```go
// 反序列化
func DeserializeBase64Tx(base64Tx string) (*solana.Transaction, error)

// 序列化
func SerializeToBase64(tx *solana.Transaction) (string, error)
```
- **用途**: 前后端传输交易数据
- **编码**: Standard Base64

**签名函数**

```go
// 单签名
func SignTransaction(tx *solana.Transaction, privateKey []byte) error

// 多签名
func SignTransactionWithMultipleKeys(tx *solana.Transaction, keys [][]byte) error

// 使用solana.PrivateKey签名
func SignTransactionWithKey(tx *solana.Transaction, privateKey solana.PrivateKey) error
```

**验证函数**

```go
func ValidateTransfer(from, to string, amountLamports, balance uint64) error
```
- **验证项**:
  1. 地址格式正确
  2. 地址不为零
  3. from和to不同
  4. 金额大于0
  5. 余额充足（金额+手续费）

#### 2.3.4 核心算法：多签名流程

```
Gas补贴交易签名流程:

1. 前端构建交易
   tx := solana.NewTransaction(instructions, blockhash)
   tx.Message.AccountKeys[0] = hotWalletPubkey  // 设置FeePayer

2. 前端签名（用户私钥）
   tx.Sign(func(key PublicKey) *PrivateKey {
       if key == userPubkey {
           return &userPrivKey
       }
       return nil
   })
   // 此时tx.Signatures[0] = userSignature

3. 序列化并发送到后端
   base64Tx := tx.MarshalBase64()

4. 后端反序列化
   tx, _ := DeserializeBase64Tx(base64Tx)

5. 后端签名（热钱包私钥）
   SignTransactionWithKey(tx, hotWalletPrivKey)
   // 此时tx.Signatures补充热钱包签名

6. 发送交易
   sig, _ := client.SendTransaction(tx)

签名顺序:
- tx.Message.AccountKeys = [feePayer, sender, ...]
- tx.Signatures = [feePayerSig, senderSig, ...]
```

---

### 2.4 `internal/solana/hotwallet.go`

#### 2.4.1 文件头信息

| 属性 | 说明 |
|------|------|
| **文件作用** | 热钱包密钥管理和签名功能 |
| **所属模块** | HotWallet模块 |
| **依赖关系** | `solana-go`, `mr-tron/base58` |

#### 2.4.2 数据结构定义

```go
// HotWallet StablePay官方热钱包
type HotWallet struct {
    Address     string `json:"address"`      // Solana地址
    DID         string `json:"did"`          // W3C DID标识符
    PublicKey   string `json:"public_key"`   // Base58公钥
    PrivateKey  string `json:"private_key"`  // Base58私钥（64字节扩展格式）
    Role        string `json:"role"`         // 钱包角色
    Description string `json:"description"`  // 描述信息
}
```

**约束**:
- `Address` = Base58(PublicKey)
- `DID` = "did:solana:" + Address
- `PrivateKey` = 64字节扩展Ed25519私钥（Base58编码）

#### 2.4.3 函数接口定义

**加载热钱包**

```go
func LoadHotWallet(path string) (*HotWallet, error)
```
- **功能**: 从JSON文件加载热钱包配置
- **参数**: `path` - JSON文件路径
- **返回值**: `(*HotWallet, error)` - 热钱包实例或错误
- **前置条件**: 文件存在且格式正确
- **后置条件**: 返回的热钱包可直接使用

**获取私钥对象**

```go
func (hw *HotWallet) GetPrivateKey() (solana.PrivateKey, error)
```
- **功能**: 将Base58私钥解码为solana.PrivateKey
- **返回值**: `(solana.PrivateKey, error)` - 私钥对象或错误
- **实现**: `base58.Decode(hw.PrivateKey)` → `solana.PrivateKey(bytes)`

**验证热钱包**

```go
func (hw *HotWallet) Validate() error
```
- **功能**: 验证热钱包配置完整性和密钥匹配
- **验证项**:
  1. 所有必要字段非空
  2. 地址格式正确
  3. 私钥可解码（64字节）
  4. 公钥从私钥派生后与地址匹配
- **返回值**: 验证失败返回错误

**签名交易**

```go
func (hw *HotWallet) SignTransaction(tx *solana.Transaction) error
```
- **功能**: 使用热钱包私钥签名交易
- **参数**: `tx` - 待签名交易（会原地修改）
- **前置条件**: 热钱包已验证
- **错误处理**: 私钥解码失败、签名失败返回错误

---

### 2.5 `internal/solana/spl_token.go`

#### 2.5.1 文件头信息

| 属性 | 说明 |
|------|------|
| **文件作用** | SPL Token（USDC/USDT）转账支持 |
| **所属模块** | SPLToken模块 |
| **依赖关系** | `solana-go`, `solana-go/programs/token`, `solana-go/programs/associated-token-account` |

#### 2.5.2 数据结构定义

```go
// SPLTokenTransferRequest SPL Token转账请求
type SPLTokenTransferRequest struct {
    From     string // 付款方地址
    To       string // 收款方地址
    Mint     string // Token Mint地址
    Amount   uint64 // 转账金额（最小单位）
    Decimals uint8  // Token精度（USDC/USDT=6）
}

// 常用Token Mint地址（预定义）
var (
    USDCDevnet  = solana.MustPublicKeyFromBase58("4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU")
    USDTDevnet  = solana.MustPublicKeyFromBase58("BQcdHdAQW1hczDbBi9hiegXAR7A18QhzhCoXFBtBj9QA")
    USDCMainnet = solana.MustPublicKeyFromBase58("EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v")
    USDTMainnet = solana.MustPublicKeyFromBase58("Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB")
)
```

#### 2.5.3 函数接口定义

**构建Token转账交易**

```go
func (tb *TransactionBuilder) BuildSPLTokenTransferTx(
    req *SPLTokenTransferRequest,
    feePayer solana.PublicKey,
    recentBlockHash solana.Hash,
) (*solana.Transaction, error)
```
- **功能**: 构建SPL Token转账交易
- **前提**: 收款方ATA已存在
- **指令**: `TokenProgram.Transfer`

**构建带ATA创建的Token转账**

```go
func (tb *TransactionBuilder) BuildSPLTokenTransferTxWithCreateATA(
    req *SPLTokenTransferRequest,
    feePayer solana.PublicKey,
    recentBlockHash solana.Hash,
) (*solana.Transaction, error)
```
- **功能**: 构建Token转账交易，自动创建收款方ATA
- **指令序列**:
  1. `AssociatedTokenAccountProgram.Create`（创建ATA，幂等）
  2. `TokenProgram.Transfer`（转账）
- **费用**: 创建ATA需要支付租金（约0.002 SOL）

**金额单位转换**

```go
// 可读金额 → 最小单位
// 1.5 USDC (6 decimals) → 1500000
func AmountToMinorUnits(amount float64, decimals uint8) uint64

// 最小单位 → 可读金额
// 1500000 → 1.5
func MinorUnitsToAmount(minorUnits uint64, decimals uint8) float64
```

---

### 2.6 `data-access-layer/db/gas_subsidy.go`

#### 2.6.1 文件头信息

| 属性 | 说明 |
|------|------|
| **文件作用** | Gas补贴记录的数据访问层 |
| **所属模块** | DAL模块 |
| **依赖关系** | `gorm.io/gorm` |

#### 2.6.2 数据结构定义

```go
// GasSubsidyStatus 补贴状态枚举
type GasSubsidyStatus string

const (
    SubsidyPending   GasSubsidyStatus = "pending"   // 待处理
    SubsidyCompleted GasSubsidyStatus = "completed" // 已完成
    SubsidyFailed    GasSubsidyStatus = "failed"    // 失败
)

// GasSubsidyRecord Gas补贴记录模型（对应数据库表）
type GasSubsidyRecord struct {
    ID              uint64           `gorm:"primaryKey;autoIncrement"`
    TxID            string           `gorm:"size:64;index;not null"`    // 业务交易ID
    OriginalTxHash  string           `gorm:"size:88;index;not null"`    // 链上交易哈希
    GasAmount       uint64           `gorm:"not null"`                  // 总Gas费用
    SubsidyAmount   uint64           `gorm:"not null"`                  // 补贴金额
    AgentPaidAmount uint64           `gorm:"not null"`                  // Agent承担金额
    SubsidyTxHash   string           `gorm:"size:88"`                   // 补贴交易哈希
    Status          GasSubsidyStatus `gorm:"size:20;index;not null"`    // 状态
    FeePayer        string           `gorm:"size:44;not null"`          // FeePayer地址
    Network         string           `gorm:"size:20;not null"`          // 网络类型
    CreatedAt       time.Time        `gorm:"index"`
    UpdatedAt       time.Time
}

// 表名: gas_subsidies
```

**数据库索引**:
- `idx_tx_id`: TxID索引
- `idx_original_tx_hash`: 交易哈希索引
- `idx_status`: 状态索引
- `idx_created_at`: 创建时间索引

#### 2.6.3 函数接口定义

**创建记录**

```go
func (dal *GasSubsidyDAL) Create(record *GasSubsidyRecord) error
func (dal *GasSubsidyDAL) CreateSubsidyRecord(txID, txHash, feePayer, network string, estimatedFee uint64) (*GasSubsidyRecord, error)
```

**查询记录**

```go
func (dal *GasSubsidyDAL) GetByID(id uint64) (*GasSubsidyRecord, error)
func (dal *GasSubsidyDAL) GetByTxHash(txHash string) (*GasSubsidyRecord, error)
func (dal *GasSubsidyDAL) List(limit, offset int) ([]*GasSubsidyRecord, error)
func (dal *GasSubsidyDAL) ListByStatus(status GasSubsidyStatus, limit, offset int) ([]*GasSubsidyRecord, error)
```

**更新记录**

```go
func (dal *GasSubsidyDAL) UpdateStatus(id uint64, status GasSubsidyStatus, gasAmount uint64) error
func (dal *GasSubsidyDAL) FinalizeSubsidy(id uint64, actualGas uint64, subsidyRatio float64) error
```

**统计查询**

```go
func (dal *GasSubsidyDAL) GetStats() (*SubsidyStats, error)
```

---

### 2.7 `internal/service/transfer_service.go`

#### 2.7.1 文件头信息

| 属性 | 说明 |
|------|------|
| **文件作用** | 转账业务逻辑编排，Gas补贴核心流程 |
| **所属模块** | Service模块 |
| **依赖关系** | `solana`, `db`, `blockchain_adapter`, `common` |

#### 2.7.2 数据结构定义

```go
// TransferRequest 内部转账请求
type TransferRequest struct {
    SignedTxBase64 string          // Base64编码的部分签名交易
    FromAddress    string          // 付款方地址
    ToAddress      string          // 收款方地址
    AmountMinor    int64           // 转账金额（最小单位）
    Currency       common.Currency // 币种
    TxID           string          // 业务交易ID
}

// TransferResult 转账结果
type TransferResult struct {
    TxHash      string                        // 交易哈希
    Status      blockchain_adapter.TxStatus   // 状态
    ExplorerURL string                        // 浏览器链接
}
```

#### 2.7.3 核心流程：TransferStableCoin

```go
func (s *TransferService) TransferStableCoin(ctx, req) (*TransferResult, error)
```

**执行流程**:

```
1. 反序列化交易
   DeserializeBase64Tx(req.SignedTxBase64)
   │
   ▼
2. 验证交易参数
   validateTransaction(tx, req)
   │
   ├─► 检查FeePayer是热钱包
   ├─► 检查交易包含指令
   └─► 检查已部分签名
   │
   ▼
3. 热钱包二次签名
   hotWallet.SignTransaction(tx)
   │
   ▼
4. 创建补贴记录（Pending）
   subsidyDAL.CreateSubsidyRecord(...)
   │
   ▼
5. 发送交易
   client.SendTransaction(tx)
   │
   ▼
6. 等待确认（30秒超时）
   client.WaitForConfirmation(txHash, 30s)
   │
   ├─► 超时 → 返回PENDING状态
   └─► 成功 → 继续
   │
   ▼
7. 更新补贴记录
   FinalizeSubsidy(recordID, actualFee, 1.0)
   │
   ▼
8. 返回结果
   &TransferResult{TxHash, Status, ExplorerURL}
```

---

## 3. 接口控制文档

### 3.1 RPC接口汇总

| 接口名 | 请求类型 | 响应类型 | 功能 |
|--------|----------|----------|------|
| `TransferStableCoin` | `TransferStableCoinRequest` | `TransferStableCoinResponse` | 执行稳定币转账并代付Gas |
| `GetBalance` | `GetBalanceRequest` | `GetBalanceResponse` | 查询钱包Token余额 |
| `GetTxStatus` | `GetTxStatusRequest` | `GetTxStatusResponse` | 查询交易状态 |

### 3.2 Thrift IDL定义

```thrift
// blockchain-adapter.thrift

service BlockchainAdapterService {
    TransferStableCoinResponse TransferStableCoin(1: TransferStableCoinRequest req),
    GetBalanceResponse GetBalance(1: GetBalanceRequest req),
    GetTxStatusResponse GetTxStatus(1: GetTxStatusRequest req),
}

struct TransferStableCoinRequest {
    1: common.BaseReq base,
    2: optional string from_wallet_address,
    3: string to_wallet_address,
    4: i64 amount_minor,
    5: common.Currency currency,
    6: optional string signed_tx_base64,
}

struct TransferStableCoinResponse {
    1: common.BaseResp base,
    2: common.TxHash tx_hash,
    3: TxStatus status,
    4: optional string confirmed_at,
}
```

### 3.3 错误码映射

| 错误码 | 值 | 含义 | 触发场景 |
|--------|-----|------|----------|
| SUCCESS | 0 | 成功 | 操作成功完成 |
| INVALID_PARAMETERS | 10001 | 参数错误 | 请求参数校验失败 |
| INSUFFICIENT_BALANCE | 20001 | 余额不足 | 用户Token余额不足 |
| BLOCKCHAIN_NETWORK_ERROR | 20003 | 网络错误 | Solana RPC调用失败 |
| GAS_SUBSIDY_FAILED | 20004 | 补贴失败 | Gas补贴记录创建或更新失败 |
| INTERNAL_SERVER_ERROR | 30001 | 内部错误 | 未知错误 |

---

## 4. 配置与构建指南

### 4.1 配置文件

**文件**: `conf/dev.yaml`

```yaml
solana:
  network: "devnet"                           # devnet/testnet/mainnet-beta
  rpc_endpoint: "https://api.devnet.solana.com"
  ws_endpoint: "wss://api.devnet.solana.com"
  hotwallet_path: "conf/hotwallet.json"

mysql:
  dsn: "user:pass@tcp(127.0.0.1:3306)/db?charset=utf8mb4&parseTime=True&loc=Local"
```

### 4.2 热钱包配置

**文件**: `conf/hotwallet.json`

```json
{
  "address": "8H3WykS8arWGqPkD2jy4Ler77nyvirSuKx2jfgpZiSeQ",
  "did": "did:solana:8H3WykS8arWGqPkD2jy4Ler77nyvirSuKx2jfgpZiSeQ",
  "public_key": "8H3WykS8arWGqPkD2jy4Ler77nyvirSuKx2jfgpZiSeQ",
  "private_key": "47aZvipsPFjZfsDsD7eGQtkedFMmRUsNXcBfJdEabseWPPjxQzh8A1Qa8s4RppeuGq31aoJaekCkF77XKQbo8qkG",
  "role": "stablepay_hot_wallet",
  "description": "Gas fee subsidy wallet"
}
```

### 4.3 构建命令

```bash
# 下载依赖
go mod tidy

# 编译
go build -o blockchain-adapter cmd/server/main.go

# 运行
go run cmd/server/main.go -config conf/dev.yaml
```

---

## 5. 测试策略

### 5.1 单元测试

| 目标函数 | 测试输入 | 期望输出 | 测试目的 |
|----------|----------|----------|----------|
| `DeserializeBase64Tx` | 有效的Base64交易 | `*solana.Transaction` | 验证反序列化正确 |
| `ValidateTransfer` | 余额<金额+手续费 | `insufficient balance`错误 | 验证余额检查 |
| `AmountToMinorUnits` | 1.5, decimals=6 | 1500000 | 验证金额转换 |

### 5.2 集成测试路径

**路径1: 完整转账流程**
```
1. 构建部分签名交易（模拟前端）
2. 调用TransferStableCoin
3. 验证交易已发送到网络
4. 轮询等待确认
5. 验证数据库记录
```

**路径2: 余额查询**
```
1. 准备已知余额的地址
2. 调用GetBalance
3. 验证返回余额正确
```

**路径3: 错误处理**
```
1. 发送无效地址
2. 验证返回INVALID_PARAMETERS
3. 验证数据库无记录
```

---

**文档版本**: v1.0  
**最后更新**: 2026-03-15
