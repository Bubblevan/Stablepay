# StablePay Common

StablePay Common 是一个Go语言公共库集合，提供支付系统相关的核心功能模块。

## 📦 模块介绍

### 🔄 TCC分布式事务模块 (`tcc`)

完整的TCC（Try-Confirm-Cancel）分布式事务解决方案，支持跨服务的分布式事务管理。

**主要特性:**
- 完整的Try-Confirm-Cancel三个阶段实现
- 自动回滚机制，Try阶段失败时自动执行Cancel操作
- 基于GORM的存储层，支持多种数据库
- 完整的事务状态跟踪和查询
- 包含完整的单元测试和集成测试

**适用场景:**
- 跨服务的分布式事务
- 支付系统的资金流转
- 订单系统的库存扣减
- 任何需要保证数据一致性的分布式操作

### 💰 货币处理模块 (`money`)

高精度货币处理库，专为支付系统设计，避免浮点数精度问题。

**主要特性:**
- 高精度计算，使用 big.Int 和 decimal.Decimal 内部存储
- 多货币支持，支持任意货币类型
- 链信息管理，统一管理链类型、链信息和原生代币
- 链特定币种，支持同一币种在不同链上的精度差异 (如 BSC 上 USDT 为 18 位，Ethereum 上为 6 位)
- 跨链汇总，支持不同链上同币种的金额汇总计算
- 地址验证，支持 EVM、TRON、Solana 等链的地址格式验证和标准化
- 合约地址管理，内置各链代币的合约地址信息
- 汇率转换系统，支持多种转换方式
- 严格的货币类型验证
- 完整的单元测试覆盖

**支持的链:**
- Ethereum (EVM)
- BSC (EVM)
- TRON (TVM)
- Solana (SVM)
- Polygon (EVM)

**支持的币种:**
- 稳定币: USDT、USDC (支持各链版本)
- 原生代币: ETH、BNB、TRX、SOL、MATIC
- 法币: USD、CNY 等

**适用场景:**
- 支付系统的金额计算
- 多币种、多链交易处理
- 跨链资产汇总和展示
- 链上地址验证
- 汇率转换和计算
- 财务系统的精确计算

### 📝 日志系统模块 (`log`)

基于配置文件的统一日志配置系统，支持本地日志记录、阿里云SLS上传和Trace ID自动透传。

**主要特性:**
- 统一配置管理，一个配置文件管理所有日志相关配置
- 多目标日志记录，同时支持本地日志和阿里云SLS日志服务
- 自动Trace ID透传，HTTP和RPC调用自动透传Trace信息
- 业务日志支持，支持支付、API、数据库、性能等业务日志
- 中间件支持，支持Gin、Echo、标准库HTTP和Kitex RPC
- 配置验证，自动验证配置的完整性和正确性

**适用场景:**
- 微服务架构的日志收集
- 分布式系统的链路追踪
- 业务日志的统一管理
- 性能监控和问题排查


## 🏗️ 项目结构

```
stablepay-common/
├── README.md
└──     ├── tcc/                    # TCC分布式事务模块
    │   ├── go.mod
    │   ├── types.go
    │   ├── coordinator.go
    │   ├── transaction_manager.go
    │   ├── storage.go
    │   ├── example.go
    │   ├── tcc_test.go
    │   └── cmd/
    │       └── main.go
    ├── money/                  # 货币处理模块
    │   ├── go.mod
    │   ├── money.go
    │   ├── money_test.go
    │   ├── currency.go
    │   └── README.md
    └── log/                   # 日志系统模块
        ├── go.mod
        ├── config.go
        ├── sls_client.go
        ├── local_logger.go
        ├── tracelog.go
        ├── context_propagator.go
        ├── http_middleware.go
        ├── rpc_middleware.go
        ├── logger.go
        ├── global.go
        ├── example.go
        ├── log_test.go
        ├── README.md
        └── cmd/
            └── main.go
```

## 🚀 快速开始

### 安装TCC模块

```bash
cd tcc
go mod tidy
go test -v
```

### 安装Money模块

```bash
cd money
go mod tidy
go test -v
```

### 安装Log模块

```bash
cd log
go mod tidy
go test -v
```


## 📋 系统要求

- Go 1.24 或更高版本
- 支持的操作系统: Linux, macOS, Windows

## 🧪 测试

每个模块都包含完整的单元测试和集成测试：

```bash
# 测试TCC模块
cd tcc && go test -v

# 测试Money模块
cd money && go test -v

# 测试Log模块
cd log && go test -v
```

## 📚 文档

每个模块都有详细的文档说明：

- [TCC模块文档](tcc/README.md)
- [Money模块文档](money/README.md)
- [Log模块文档](log/README.md)

## 🔧 技术栈

- **语言**: Go 1.24+
- **ORM**: GORM
- **数据库**: SQLite, MySQL, PostgreSQL
- **日志**: Logrus, 阿里云SLS
- **链路追踪**: OpenTelemetry, 阿里云ARMS
- **HTTP框架**: Gin, Echo
- **RPC框架**: Kitex
- **测试**: Testify
- **精度计算**: Decimal

## 📄 许可证

MIT License

## 👥 协作者

- 项目维护者: StablePay Team
- 贡献者: 欢迎提交Issue和Pull Request