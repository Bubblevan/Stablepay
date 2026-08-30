# DTS 模块

DTS (Data Transfer Service) 数据迁移服务模块，提供阿里云 DTS 数据同步事件的统一处理框架。

## 主要特性

- **CloudEvents 格式解析**: 支持解析 DTS 发送的 CloudEvents 标准格式消息
- **数据类型自动转换**: 自动处理 DTS 传输的各种数据类型（包括时间、数值、文本等）
- **字符编码支持**: 支持 UTF-8、GBK、latin1、UTF-16 等多种字符集编码
- **统一事件抽象**: 提供 Event 接口统一封装 DTS 事件
- **表路由机制**: 支持根据表名路由到不同的业务处理器
- **数据映射错误处理**: 区分数据映射错误和系统错误，避免无效重试

## 快速开始

### 1. 添加依赖

```go
module your-module

go 1.24.0

require (
    code.wenfu.cn/stablepay/stablepay-common/dts v0.0.0
    github.com/apache/rocketmq-clients/golang/v5 v5.1.3
)

// 本地开发时使用 replace
replace code.wenfu.cn/stablepay/stablepay-common/dts => ../stablepay-common/dts
```

### 2. 实现业务处理器

```go
type MyTableHandler struct {
    repo *MyRepository
}

func (h *MyTableHandler) Handle(ctx context.Context, event *dts.DTSEventAdapter) error {
    switch event.GetType() {
    case dts.OperationInsert, dts.OperationUpdate:
        after := event.GetAfterImage()
        entity, err := h.mapToEntity(after)
        if err != nil {
            return dts.NewDataMappingError("my_table", "", "map failed", after, err)
        }
        return h.repo.Save(ctx, entity)

    case dts.OperationDelete:
        before := event.GetBeforeImage()
        id, _ := dts.GetStringValue(before, "id")
        return h.repo.Delete(ctx, id)
    }
    return nil
}
```

### 3. 定义表路由并启动

```go
// 创建路由
router := func(tableName string) (dts.Handler, bool) {
    switch tableName {
    case "my_table":
        return myHandler, true
    default:
        return nil, false
    }
}

// 创建处理器
handler := dts.NewMessageHandler(router)

// 处理消息
err := handler.HandleMessage(ctx, messageView)
```

详细示例请查看 [example/main.go](./example/main.go)。

## 核心组件

### 1. Event 接口 (`event.go`)

统一的事件抽象接口：

```go
type Event interface {
    GetID() string            // 获取事件ID（幂等键）
    GetType() string          // 获取事件类型
    GetOccurredOn() time.Time // 获取事件发生时间
    GetData() any             // 获取事件数据
    GetSource() string        // 获取事件来源
    GetTopic() string         // 获取原始 topic 信息
}
```

### 2. DTSEventAdapter (`adapter.go`)

DTS 事件适配器：

```go
adapter, err := dts.NewDTSEventAdapter(cloudEventID, rawDTSEvent, topic)
if err != nil {
    return err
}

// 获取变更数据
before := adapter.GetBeforeImage()  // 变更前数据
after := adapter.GetAfterImage()    // 变更后数据
table := adapter.GetTableName()     // 表名
operation := adapter.GetType()      // 操作类型：INSERT/UPDATE/DELETE/DDL
```

### 3. MessageHandler (`handler.go`)

DTS 消息处理器：

```go
router := func(tableName string) (dts.Handler, bool) {
    switch tableName {
    case "payment_order":
        return paymentOrderHandler, true
    default:
        return nil, false
    }
}

handler := dts.NewMessageHandler(router, myLogger)
err := handler.HandleMessage(ctx, messageView)
```

### 4. Handler 接口

业务处理器接口：

```go
type Handler interface {
    Handle(ctx context.Context, event *DTSEventAdapter) error
}
```

### 5. DataMappingError (`errors.go`)

数据映射错误，用于标记消息格式问题（应该跳过而不是重试）：

```go
if err != nil {
    return dts.NewDataMappingError("payment_order", "amount", "invalid format", rawData, err)
}
```

### 6. 工具函数 (`util.go`)

```go
// 预处理数值字段
data := dts.PreprocessNumericFields(rawData, []string{"amount", "status", "version"})

// 从 map 中获取各种类型的值
strVal, ok := dts.GetStringValue(data, "name")
intVal, ok := dts.GetInt64Value(data, "count")
floatVal, ok := dts.GetFloat64Value(data, "rate")
```

## 完整使用示例

```go
package main

import (
    "context"
    "code.wenfu.cn/stablepay/stablepay-common/dts"
    "github.com/apache/rocketmq-clients/golang/v5"
)

// PaymentOrderHandler 支付订单处理器
type PaymentOrderHandler struct {
    repo *PaymentOrderRepository
}

func (h *PaymentOrderHandler) Handle(ctx context.Context, event *dts.DTSEventAdapter) error {
    switch event.GetType() {
    case dts.OperationInsert, dts.OperationUpdate:
        after := event.GetAfterImage()
        if after == nil {
            return fmt.Errorf("after image is nil")
        }

        // 数据映射
        order, err := h.mapToPaymentOrder(after)
        if err != nil {
            return dts.NewDataMappingError("payment_order", "", "map failed", after, err)
        }

        // 保存到本地数据库
        if err := h.repo.Save(ctx, order); err != nil {
            return fmt.Errorf("failed to save: %w", err)
        }

    case dts.OperationDelete:
        before := event.GetBeforeImage()
        paymentID, ok := dts.GetStringValue(before, "payment_id")
        if !ok {
            return dts.NewDataMappingError("payment_order", "payment_id", "not found", before, nil)
        }
        h.repo.Delete(ctx, paymentID)
    }

    return nil
}

func (h *PaymentOrderHandler) mapToPaymentOrder(data map[string]any) (*PaymentOrder, error) {
    // 预处理数值字段
    data = dts.PreprocessNumericFields(data, []string{"amount", "status"})

    order := &PaymentOrder{}
    if v, ok := dts.GetStringValue(data, "payment_id"); ok {
        order.PaymentID = v
    }
    if v, ok := dts.GetFloat64Value(data, "amount"); ok {
        order.Amount = v
    }
    if v, ok := dts.GetInt64Value(data, "status"); ok {
        order.Status = int(v)
    }

    return order, nil
}

// TableRouter 表路由
func TableRouter(paymentHandler *PaymentOrderHandler) dts.TableRouter {
    return func(tableName string) (dts.Handler, bool) {
        switch tableName {
        case "payment_order":
            return paymentHandler, true
        default:
            return nil, false
        }
    }
}

// 在你的 MQ 消费者中直接使用 dts.MessageHandler
handler := dts.NewMessageHandler(router, logger)

// 处理消息
err := handler.HandleMessage(ctx, messageView)
```

## API 参考

### 事件类型常量

```go
const (
    OperationInsert = "INSERT"
    OperationUpdate = "UPDATE"
    OperationDelete = "DELETE"
    OperationDDL    = "DDL"
)
```

### DTSEventAdapter 方法

| 方法 | 说明 |
|------|------|
| `GetID()` | 幂等键 |
| `GetType()` | INSERT/UPDATE/DELETE/DDL |
| `GetTableName()` | 表名 |
| `GetDatabaseName()` | 数据库名 |
| `GetSource()` | 数据源 |
| `GetTopic()` | RocketMQ topic |
| `GetOccurredOn()` | 事件发生时间 |
| `GetBeforeImage()` | 变更前数据 (map[string]any) |
| `GetAfterImage()` | 变更后数据 (map[string]any) |
| `GetPosition()` | DTS位点 |

### 工具函数

```go
// 预处理数值字段
data := dts.PreprocessNumericFields(rawData, []string{"amount", "status"})

// 获取字段值
strVal, ok := dts.GetStringValue(data, "name")
intVal, ok := dts.GetInt64Value(data, "count")
floatVal, ok := dts.GetFloat64Value(data, "rate")

// 数据映射错误
err := dts.NewDataMappingError("table", "field", "message", rawData, cause)
if dts.IsDataMappingError(err) { /* 跳过 */ }
```

## 最佳实践

### 1. 数据映射错误处理

当数据格式不正确时，返回 `DataMappingError`，让上层跳过而不是重试：

```go
entity, err := mapToEntity(after)
if err != nil {
    return dts.NewDataMappingError("my_table", "", "invalid data", after, err)
}
```

### 2. 数值字段预处理

DTS 传输的数字字段可能是字符串类型，需要预处理：

```go
data = dts.PreprocessNumericFields(data, []string{"amount", "status", "version"})
```

### 3. 自定义日志

```go
type MyLogger struct{}

func (l *MyLogger) Warn(ctx context.Context, msg string, fields map[string]any) {
    // 记录警告日志
}

func (l *MyLogger) Error(ctx context.Context, msg string, fields map[string]any) {
    // 记录错误日志
}

handler := dts.NewMessageHandler(router, &MyLogger{})
```

## 支持的字符编码

- UTF-8 / UTF-8-MB4
- GBK / GB2312
- Big5
- Latin1 / ISO-8859-1
- UTF-16 / UTF-16BE / UTF-16LE

## 数据类型支持

- 文本类型（自动字符集转换）
- 整数类型（tinyint, int, bigint）
- 浮点数类型（float, double, decimal）
- 时间类型（timestamp, datetime, date）
- JSON 类型
- BLOB/BINARY 类型

## 运行测试

```bash
cd dts
go mod tidy
go test -v
```
