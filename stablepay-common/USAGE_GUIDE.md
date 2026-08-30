# StablePay Common 使用指南

## 概述

`stablepay-common` 是团队的公共工具库，包含货币处理、日志、错误码、ID生成等通用功能。

**Codeup 地址**: https://codeup.aliyun.com/6878738f874c52f1221c8c29/stablepayai/stablepay-common

**Go 模块地址**: `code.wenfu.cn/stablepayai/stablepay-common`

---

## 1. 添加依赖

### 方式1: 使用 go get (推荐)

```bash
# 在各自的服务目录下执行
cd D:\MyLab\StablePay\payment-service
# 或 cd D:\MyLab\StablePay\api-gateway
# 或 cd D:\MyLab\StablePay\did-service

# 添加 common 依赖
go get code.wenfu.cn/stablepayai/stablepay-common@master

# 下载并整理依赖
go mod tidy
```

### 方式2: 手动修改 go.mod

在项目的 `go.mod` 文件中添加：

```go
module github.com/stablepay/payment-service

go 1.21

require (
    // ... 其他依赖
    code.wenfu.cn/stablepayai/stablepay-common v0.0.0-20260312091917-17bfa21343e0
)
```

然后执行：
```bash
go mod download
go mod tidy
```

### 首次使用配置 (重要)

如果是第一次使用公司内网 Codeup，需要配置 Git 凭证：

```bash
# 配置 Git 使用个人访问令牌 (PAT)
# 在 Codeup 个人设置 -> 个人设置 -> HTTPS密码 或 个人访问令牌 生成

git config --global url."https://username:token@codeup.aliyun.com/".insteadOf "https://codeup.aliyun.com/"

# 或者在 go 命令中直接指定
export GOPRIVATE=code.wenfu.cn
go env -w GOPRIVATE=code.wenfu.cn
```

---

## 2. 模块使用说明

stablepay-common 包含以下子模块，使用时需要**单独导入**：

```
stablepay-common/
├── chainmoney/      # 链上金额处理
├── constants/       # 错误码和常量
├── dts/            # 数据传输服务 (MQ相关)
├── id_generator/   # ID生成器
├── log/            # 日志系统
├── money/          # 货币金额处理
└── tcc/            # TCC分布式事务
```

---

## 3. 各模块使用示例

### 3.1 constants - 错误码 (最常用)

**用途**: 统一错误码定义，避免各服务错误码冲突

**导入**:
```go
import "code.wenfu.cn/stablepayai/stablepay-common/constants"
```

**使用示例**:
```go
package main

import (
    "code.wenfu.cn/stablepayai/stablepay-common/constants"
    "fmt"
)

func main() {
    // 使用系统错误码
    errCode := constants.SystemErrorCode{
        Code:    constants.SystemErrorCodeDatabaseConnection,
        Message: "数据库连接失败",
    }

    // 使用业务错误码
    bizCode := constants.BusinessErrorCode{
        Code:    constants.BusinessErrorCodeInsufficientBalance,
        Message: "余额不足",
    }

    fmt.Printf("系统错误: %d - %s\n", errCode.Code, errCode.Message)
    fmt.Printf("业务错误: %d - %s\n", bizCode.Code, bizCode.Message)
}
```

**错误码对照表**:

| 模块 | 常量名 | 值 | 说明 |
|------|--------|-----|------|
| 系统 | `SystemErrorCodeDatabaseConnection` | 30001 | 数据库连接错误 |
| 系统 | `SystemErrorCodeServiceUnavailable` | 30002 | 服务不可用 |
| 业务 | `BusinessErrorCodeInsufficientBalance` | 20001 | 余额不足 |
| 业务 | `BusinessErrorCodePaymentAlreadyExists` | 20002 | 支付已存在 |
| 基础 | `BaseErrorCodeInvalidParameters` | 10001 | 参数无效 |
| 基础 | `BaseErrorCodeResourceNotFound` | 10002 | 资源不存在 |

---

### 3.2 money - 货币金额处理

**用途**: 高精度金额计算，避免 float 精度问题

**导入**:
```go
import "code.wenfu.cn/stablepayai/stablepay-common/money"
```

**使用示例**:
```go
package main

import (
    "code.wenfu.cn/stablepayai/stablepay-common/money"
    "fmt"
)

func main() {
    // 创建金额对象 (USDC 有 6 位小数)
    amount, err := money.NewFromString("10.50", "USDC")
    if err != nil {
        panic(err)
    }

    // 获取最小单位表示 (用于链上转账)
    amountMinor := amount.MinorUnit() // 10500000
    fmt.Printf("最小单位: %d\n", amountMinor)

    // 从最小单位创建
    amount2 := money.NewFromMinor(5000000, "USDC") // 5.00 USDC

    // 金额计算
    sum := amount.Add(amount2)
    fmt.Printf("总和: %s\n", sum.String()) // 15.50 USDC

    // 比较
    if amount.GreaterThan(amount2) {
        fmt.Println("amount 更大")
    }
}
```

---

### 3.3 chainmoney - 链上金额处理

**用途**: 多链金额处理，支持不同链上币种精度差异

**导入**:
```go
import "code.wenfu.cn/stablepayai/stablepay-common/chainmoney"
```

**使用示例**:
```go
package main

import (
    "code.wenfu.cn/stablepayai/stablepay-common/chainmoney"
    "fmt"
)

func main() {
    // 注册链和币种 (通常在 init 或 main 中做一次)
    registry := chainmoney.NewRegistry()

    // 添加 Ethereum USDC (6位精度)
    registry.Register(chainmoney.ChainEthereum, "USDC", 6, "0xA0b86a33E6441b171DFe8AB68192e4E64c8b7e74")

    // 添加 BSC USDT (18位精度)
    registry.Register(chainmoney.ChainBSC, "USDT", 18, "0x55d398326f99059fF775485246999027B3197955")

    // 创建链上金额
    ethUSDC, _ := chainmoney.New("10.50", "USDC", chainmoney.ChainEthereum)
    bscUSDT, _ := chainmoney.New("5.00", "USDT", chainmoney.ChainBSC)

    // 获取链上转账用的值
    fmt.Printf("Ethereum USDC 链上值: %s\n", ethUSDC.ChainValue())
    fmt.Printf("BSC USDT 链上值: %s\n", bscUSDT.ChainValue())

    // 跨链汇总 (统一换算成 USD)
    total := registry.SumToUSD(ethUSDC, bscUSDT)
    fmt.Printf("总价值: %s USD\n", total)
}
```

---

### 3.4 id_generator - ID生成器

**用途**: 生成全局唯一 ID (雪花算法)

**导入**:
```go
import "code.wenfu.cn/stablepayai/stablepay-common/id_generator"
```

**使用示例**:
```go
package main

import (
    "code.wenfu.cn/stablepayai/stablepay-common/id_generator"
    "fmt"
)

func main() {
    // 初始化 (每个服务一个节点ID，不能重复)
    gen, err := id_generator.New(1) // 节点ID = 1
    if err != nil {
        panic(err)
    }

    // 生成唯一ID
    id := gen.NextID()
    fmt.Printf("生成的ID: %d\n", id)

    // 生成字符串ID
    strID := gen.NextString()
    fmt.Printf("字符串ID: %s\n", strID)

    // 解析ID获取时间
    t := id_generator.ParseTime(id)
    fmt.Printf("ID生成时间: %v\n", t)
}
```

**注意**: 每个服务实例需要一个唯一的节点ID (1-1023)，建议通过配置文件传入。

---

### 3.5 log - 日志系统

**用途**: 统一日志格式，支持阿里云 SLS

**导入**:
```go
import "code.wenfu.cn/stablepayai/stablepay-common/log"
```

**使用示例**:
```go
package main

import (
    "code.wenfu.cn/stablepayai/stablepay-common/log"
    "context"
)

func main() {
    // 初始化日志
    logger, err := log.New(&log.Config{
        Level:      "info",
        OutputPath: "./logs/app.log",
        EnableSLS:  false, // 测试环境关闭，生产开启
    })
    if err != nil {
        panic(err)
    }

    // 普通日志
    logger.Info("服务启动")
    logger.Error("发生错误", "error", "connection refused")

    // 带上下文的日志 (自动透传 Trace ID)
    ctx := context.WithValue(context.Background(), "trace_id", "abc123")
    logger.InfoContext(ctx, "处理请求", "user_id", "123")
}
```

**在 Kitex 中使用**:
```go
import (
    "github.com/cloudwego/kitex/pkg/klog"
    "code.wenfu.cn/stablepayai/stablepay-common/log"
)

func init() {
    logger, _ := log.New(&log.Config{...})
    klog.SetLogger(logger)
}
```

---

### 3.6 tcc - 分布式事务

**用途**: 跨服务分布式事务管理

**导入**:
```go
import "code.wenfu.cn/stablepayai/stablepay-common/tcc"
```

**使用示例**:
```go
package main

import (
    "code.wenfu.cn/stablepayai/stablepay-common/tcc"
    "context"
)

func main() {
    // 创建事务管理器
    tm, _ := tcc.NewTransactionManager(db)

    // 定义 TCC 参与者
    participants := []tcc.Participant{
        {
            Name: "payment",
            Try: func(ctx context.Context) error {
                // 预扣款
                return nil
            },
            Confirm: func(ctx context.Context) error {
                // 确认扣款
                return nil
            },
            Cancel: func(ctx context.Context) error {
                // 回滚扣款
                return nil
            },
        },
    }

    // 执行事务
    txID, err := tm.Execute(context.Background(), participants)
    if err != nil {
        // 处理错误
    }
}
```

**注意**: 一期可以先不用 TCC，直接单服务事务即可。

---

### 3.7 dts - 数据传输服务 (MQ)

**用途**: MQ 消息发送和接收

**导入**:
```go
import "code.wenfu.cn/stablepayai/stablepay-common/dts"
```

**使用示例**:
```go
package main

import (
    "code.wenfu.cn/stablepayai/stablepay-common/dts"
    "context"
)

func main() {
    // 创建生产者
    producer, _ := dts.NewProducer(&dts.Config{
        Brokers: []string{"localhost:9092"},
        Topic:   "payment_events",
    })

    // 发送消息
    err := producer.Send(context.Background(), &dts.Event{
        Type: "payment_succeeded",
        Data: []byte(`{"tx_id": "123"}`),
    })
}
```

---

## 4. 完整项目示例

### go.mod

```go
module github.com/stablepay/payment-service

go 1.21

require (
    github.com/cloudwego/kitex v0.16.1
    github.com/google/uuid v1.6.0
    gorm.io/driver/mysql v1.5.6
    gorm.io/gorm v1.25.10

    // 公司公共库
    code.wenfu.cn/stablepayai/stablepay-common v0.0.0-20260312091917-17bfa21343e0
)

require (
    // indirect dependencies...
)
```

### 业务代码中使用

```go
package service

import (
    "code.wenfu.cn/stablepayai/stablepay-common/constants"
    "code.wenfu.cn/stablepayai/stablepay-common/money"
    "code.wenfu.cn/stablepayai/stablepay-common/id_generator"
    "github.com/cloudwego/kitex/pkg/klog"
)

type PaymentService struct {
    idGen *id_generator.Generator
}

func New() *PaymentService {
    gen, _ := id_generator.New(1)
    return &PaymentService{idGen: gen}
}

func (s *PaymentService) ProcessPayment(amountStr string) error {
    // 1. 金额解析
    amount, err := money.NewFromString(amountStr, "USDC")
    if err != nil {
        return &constants.BusinessErrorCode{
            Code:    constants.BaseErrorCodeInvalidParameters,
            Message: "金额格式错误",
        }
    }

    // 2. 生成交易ID
    txID := s.idGen.NextString()

    // 3. 业务处理...
    klog.Infof("处理支付 tx_id=%s amount=%s", txID, amount.String())

    return nil
}
```

---

## 5. 常见问题

### Q1: go get 失败，提示 401 或 403

**解决**: 配置 Git 凭证

```bash
# 方式1: 使用 .netrc 文件
echo "machine codeup.aliyun.com login your-username password your-token" >> ~/.netrc

# 方式2: 使用 Git URL rewrite
git config --global url."https://username:token@codeup.aliyun.com/".insteadOf "https://codeup.aliyun.com/"

# 方式3: 使用 GOPRIVATE
go env -w GOPRIVATE=code.wenfu.cn
go env -w GONOSUMDB=code.wenfu.cn
```

### Q2: 导入后找不到包

**解决**: 确保使用正确的导入路径

```go
// ✅ 正确
import "code.wenfu.cn/stablepayai/stablepay-common/constants"

// ❌ 错误 (不要加子目录)
import "code.wenfu.cn/stablepayai/stablepay-common/constants/base_codes"
```

### Q3: 版本冲突

**解决**: 在项目根目录执行

```bash
go get code.wenfu.cn/stablepayai/stablepay-common@master
go mod tidy
go mod vendor  # 如果使用 vendor 模式
```

### Q4: 如何查看 common 库的最新版本

```bash
# 查看可用版本
go list -m -versions code.wenfu.cn/stablepayai/stablepay-common

# 更新到最新
go get -u code.wenfu.cn/stablepayai/stablepay-common@master
```

---

## 6. 团队规范

1. **错误码统一使用** `constants` 包，不要自己定义
2. **金额计算统一使用** `money` 或 `chainmoney`，不要用 float64
3. **日志统一使用** `log` 包，不要直接用标准库 log
4. **ID 生成统一使用** `id_generator`，不要用 UUID (性能差)

---

## 7. 相关文档

- [README.md](./README.md) - 各模块详细介绍
- [money/README.md](./money/README.md) - 货币模块详细文档
- [Codeup 仓库](https://codeup.aliyun.com/6878738f874c52f1221c8c29/stablepayai/stablepay-common)

---

**维护**: StablePay 架构组
**更新时间**: 2026-03-17
