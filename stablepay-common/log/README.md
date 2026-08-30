# StablePay Common Log 模块接入文档

## 概述

StablePay Common Log 是一个功能完整的日志系统,支持结构化日志、链路追踪、SLS日志服务等特性。

## 核心特性

- 基于 Zap 的高性能结构化日志
- 自动 Trace ID 透传 (HTTP/RPC)
- 支持多种输出方式 (stdout/file/SLS)
- 日志级别动态控制
- 日志文件自动轮转
- 完整的 Context 传播机制

## 快速开始

### 1. 安装依赖

```bash
go get code.wenfu.cn/stablepay/stablepay-common/log
```

### 2. 基础配置

创建配置文件 `config.yaml`:

```yaml
logging:
  # === 基础配置 ===
  service_name: "your-service-name"
  version: "1.0.0"
  environment: "development"

  # === 日志输出配置 ===
  enabled: true
  level: "info"
  format: "json"
  output: "both"  # stdout, file, both, sls
  file_path: "./logs/app.log"

  # === 文件轮转配置 ===
  max_size: 100
  max_age: 30
  max_backups: 10
  compress: true
  daily_rotation: true
  date_format: "2006-01-02"

  # === 链路追踪配置（嵌套在 logging 下）===
  tracing:
    enabled: true
    backend: "jaeger"  # jaeger, aliyun, otlp
    service_name: "your-service-name"
    version: "1.0.0"
    environment: "development"

    # Jaeger 配置（开发环境）
    jaeger:
      endpoint: "http://localhost:14268/api/traces"
      agent_host: "localhost"
      agent_port: 6831

    # 阿里云链路追踪配置（生产环境使用）
    aliyun:
      endpoint: ""
      token: ""
      project: ""
      instance: "default"
      service_name: ""
      service_ip: "127.0.0.1"
      service_port: 8080

    # OTLP配置（如使用其他OpenTelemetry后端）
    otlp:
      endpoint: "http://localhost:4317"
      protocol: "grpc"  # grpc, http
      headers: {}

    # 采样配置
    sampling:
      type: "parentbased"  # always, never, traceidratio, parentbased
      ratio: 1.0  # 采样比例 (0.0-1.0)

  # === SLS日志配置（嵌套在 logging 下）===
  sls:
    enabled: false
    endpoint: ""
    access_key_id: ""
    access_key_secret: ""
    project: ""
    logstore: ""
    topic: "application"
    source: "your-service-name"
```

### 3. 初始化 Logger

```go
import (
    "code.wenfu.cn/stablepay/stablepay-common/log"
)

func main() {
    // 创建配置
    config := log.NewConfig("your-service", "1.0.0")
    config.Logging.Level = "info"
    config.Logging.Output = "both"
    config.Logging.FilePath = "./logs/app.log"

    // 启用链路追踪
    config.Tracing.Enabled = true
    config.Tracing.Backend = "jaeger"

    // 初始化 logger
    logger, err := log.NewLogger(config)
    if err != nil {
        panic(err)
    }
    defer logger.Sync()

    // 设置为全局 logger
    log.SetGlobalLogger(logger)
}
```

### 4. 嵌套配置适配器模式（推荐）

对于使用 YAML 配置文件的服务，推荐使用嵌套配置结构（如上面的配置示例所示），然后通过适配器函数转换为 stablepay-common/log 所需的扁平结构。

#### 适配器实现示例

```go
package logger

import (
    "fmt"
    "os"
    "path/filepath"

    stablepaylog "code.wenfu.cn/stablepay/stablepay-common/log"
    "your-service/config"
)

// NewLogger 创建 stablepay-common 日志实例
// 将嵌套的配置结构转换为 stablepay-common/log 的扁平结构
func NewLogger(cfg *config.LogConfig) (*stablepaylog.Logger, error) {
    // 验证必需的配置
    if cfg.ServiceName == "" {
        return nil, fmt.Errorf("日志配置错误: service_name 不能为空")
    }

    // 创建 stablepay-common/log 配置（扁平结构）
    stablepayConfig := &stablepaylog.Config{
        ServiceName: cfg.ServiceName,
        Version:     cfg.Version,
        Environment: cfg.Environment,
        Logging: stablepaylog.LoggingConfig{
            Enabled:       cfg.Enabled,
            Level:         cfg.Level,
            Format:        cfg.Format,
            Output:        cfg.Output,
            FilePath:      cfg.FilePath,
            MaxSize:       cfg.MaxSize,
            MaxAge:        cfg.MaxAge,
            MaxBackups:    cfg.MaxBackups,
            Compress:      cfg.Compress,
            DailyRotation: cfg.DailyRotation,
        },
        // 注意：Tracing 和 SLS 在扁平结构中是顶层字段
        // 从嵌套的 cfg.Tracing 映射到扁平的 stablepayConfig.Tracing
        Tracing: stablepaylog.TracingConfig{
            Enabled:     cfg.Tracing.Enabled,
            Backend:     cfg.Tracing.Backend,
            ServiceName: cfg.ServiceName,
            Version:     cfg.Version,
            Environment: cfg.Environment,
            Jaeger: stablepaylog.JaegerConfig{
                Endpoint:  cfg.Tracing.Jaeger.Endpoint,
                AgentHost: cfg.Tracing.Jaeger.AgentHost,
                AgentPort: cfg.Tracing.Jaeger.AgentPort,
            },
            Aliyun: stablepaylog.AliyunTracingConfig{
                Endpoint: cfg.Tracing.Aliyun.Endpoint,
                Token:    cfg.Tracing.Aliyun.Token,
                Project:  cfg.Tracing.Aliyun.Project,
            },
        },
        // 从嵌套的 cfg.SLS 映射到扁平的 stablepayConfig.SLS
        SLS: stablepaylog.SLSConfig{
            Enabled:         cfg.SLS.Enabled,
            Endpoint:        cfg.SLS.Endpoint,
            AccessKeyID:     cfg.SLS.AccessKeyID,
            AccessKeySecret: cfg.SLS.AccessKeySecret,
            Project:         cfg.SLS.Project,
            Logstore:        cfg.SLS.Logstore,
            Topic:           cfg.SLS.Topic,
            Source:          cfg.SLS.Source,
        },
    }

    // 如果输出到文件，确保日志目录存在
    if cfg.Output == "file" || cfg.Output == "both" {
        dir := filepath.Dir(cfg.FilePath)
        if err := os.MkdirAll(dir, 0755); err != nil {
            return nil, fmt.Errorf("创建日志目录失败: %w", err)
        }
    }

    // 创建 logger 实例
    logger, err := stablepaylog.NewLogger(stablepayConfig)
    if err != nil {
        return nil, fmt.Errorf("创建日志实例失败: %w", err)
    }

    // 设置为全局 logger（推荐）
    stablepaylog.SetGlobalLogger(logger)

    return logger, nil
}
```

#### 使用适配器

```go
import (
    "your-service/config"
    "your-service/pkg/logger"
)

func main() {
    // 加载配置（嵌套结构）
    cfg, err := config.LoadConfig()
    if err != nil {
        panic(err)
    }

    // 使用适配器创建 logger
    appLogger, err := logger.NewLogger(&cfg.Logging)
    if err != nil {
        panic(err)
    }
    defer appLogger.Sync()

    // 现在可以使用全局日志函数
    commonlog.InfoNoCtx("服务启动成功", map[string]interface{}{
        "port": cfg.Server.Port,
    })
}
```

#### 适配器模式优势

1. **配置文件组织清晰**: 嵌套结构让相关配置分组更明显
2. **统一接口**: 底层使用 stablepay-common/log 的标准接口
3. **易于维护**: 配置变更只需修改适配器函数
4. **兼容性好**: 不影响现有服务的配置结构

## 链路追踪 (OpenTelemetry)

### 自动 Trace Propagation

从 v0.0.0-20251113064431 版本开始,log 模块会自动初始化 OpenTelemetry TracerProvider,确保 trace context 在服务间正确传播。

#### 工作原理

1. **TracerProvider 初始化**: 当 `tracing.enabled = true` 时,`NewLogger()` 会自动调用 `initTracerProvider()` 设置全局 TracerProvider
2. **W3C TraceContext Propagator**: 自动设置 W3C TraceContext 传播器,用于在 HTTP/RPC headers 中传递 trace 信息
3. **Kitex 集成**: 配合 `kitex-contrib/obs-opentelemetry` 自动处理 trace 注入和提取

#### 关键代码位置

**logger.go:130-136**
```go
// 如果启用了tracing,自动初始化TracerProvider
if config.Tracing.Enabled {
    // 初始化TracerProvider,这样ExtractTraceFromRPCRequest就不会返回0000了
    if err := initTracerProvider(&config.Tracing); err != nil {
        return nil, fmt.Errorf("初始化TracerProvider失败: %w", err)
    }
}
```

**context.go:530-546**
```go
// initTracerProvider 初始化TracerProvider,确保ExtractTraceFromRPCRequest能生成有效的trace信息
func initTracerProvider(config *TracingConfig) error {
    // 创建一个简单的TracerProvider,不依赖复杂的配置
    // 关键是设置全局的TracerProvider,这样tracing.NewClientSuite()才能正常工作
    tp := sdktrace.NewTracerProvider()
    otel.SetTracerProvider(tp)

    // 设置全局传播器 - 这是trace propagation的关键
    // W3C TraceContext标准确保trace ID能在服务间传递
    otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
        propagation.TraceContext{},
        propagation.Baggage{},
    ))

    return nil
}
```

### Kitex RPC 集成

#### 客户端配置

使用 `middleware.GetKitexClientOptions()` 获取预配置的 Kitex client options:

```go
import (
    "code.wenfu.cn/stablepay/stablepay-common/log/middleware"
    "github.com/cloudwego/kitex/client"
)

// 创建 RPC 客户端
cli, err := yourservice.NewClient(
    "service-name",
    client.WithHostPorts("127.0.0.1:8888"),
    // 添加 OpenTelemetry trace 中间件
    middleware.GetKitexClientOptions()...,
)
```

`GetKitexClientOptions()` 会自动配置:
- `tracing.NewClientSuite()`: 自动从 context 提取 trace 并注入到 RPC 元数据
- `transmeta.ClientTTHeaderHandler`: 支持自定义元数据传递

#### 服务端配置

使用 `middleware.GetKitexServerOptions()` 获取预配置的 Kitex server options:

```go
import (
    "code.wenfu.cn/stablepay/stablepay-common/log/middleware"
    "github.com/cloudwego/kitex/server"
)

// 创建 RPC 服务器
svr := yourservice.NewServer(
    handler,
    server.WithServiceAddr(addr),
    // 添加 OpenTelemetry trace 中间件
    middleware.GetKitexServerOptions()...,
)
```

`GetKitexServerOptions()` 会自动配置:
- `tracing.NewServerSuite()`: 自动从 RPC 元数据提取 trace 并恢复到 context
- `transmeta.ServerTTHeaderHandler`: 支持自定义元数据接收

### 验证 Trace Propagation

测试 trace ID 是否正确传播:

1. **启动服务**: 确保所有服务都启用了 tracing
2. **发起请求**: 从上游服务调用下游服务
3. **检查日志**: 确认上下游服务的 `trace_id` 一致

示例日志:

**PayAdmin (上游服务):**
```json
{
  "level": "info",
  "trace_id": "d2a8392920dc95d2db2dfb2acae59d2c",
  "span_id": "7c026e1c1734953b",
  "message": "RPC调用: GetMerchantInfo"
}
```

**MerchantCore (下游服务):**
```json
{
  "level": "info",
  "trace_id": "d2a8392920dc95d2db2dfb2acae59d2c",
  "span_id": "ffb5be53ec706e3a",
  "message": "RPC: GetMerchantInfo called"
}
```

✅ **trace_id 一致,表示 trace propagation 工作正常!**

### 常见问题排查

#### 问题: Trace ID 为 00000000000000000000000000000000

**原因**: TracerProvider 未正确初始化

**解决方案**:
1. 确认 `tracing.enabled = true`
2. 确认使用的 stablepay-common 版本 >= v0.0.0-20251113064431
3. 检查 logger 初始化是否成功

```bash
# 更新到最新版本
go get code.wenfu.cn/stablepay/stablepay-common@latest
go mod tidy
```

#### 问题: 上下游服务 Trace ID 不一致

**原因**: Kitex client/server 未配置 OpenTelemetry suite

**解决方案**:
使用 `middleware.GetKitexClientOptions()` 和 `middleware.GetKitexServerOptions()`

```go
// 客户端
cli, err := service.NewClient(
    "service-name",
    middleware.GetKitexClientOptions()...,
)

// 服务端
svr := service.NewServer(
    handler,
    middleware.GetKitexServerOptions()...,
)
```

## 日志记录

### 基础用法

```go
import (
    "context"
    "code.wenfu.cn/stablepay/stablepay-common/log"
)

func HandleRequest(ctx context.Context) {
    logger := log.GetLoggerFromContext(ctx)

    // Info 级别日志
    logger.Info(ctx, "处理请求", map[string]interface{}{
        "user_id": "user_123",
        "action": "create_order",
    })

    // Error 级别日志
    logger.Error(ctx, "处理失败", map[string]interface{}{
        "error": err.Error(),
        "user_id": "user_123",
    })
}
```

### Context 传播

```go
// 将 logger 注入到 context
ctx = log.WithLogger(ctx, logger)

// 设置 request ID
ctx = log.WithRequestID(ctx, "req_123")

// 设置 user ID
ctx = log.WithUserID(ctx, "user_456")

// 后续代码可以从 context 获取这些信息
logger := log.GetLoggerFromContext(ctx)
requestID := log.GetRequestID(ctx)
userID := log.GetUserID(ctx)
```

## HTTP 中间件

### Hertz 框架（推荐）

**StablePay 所有项目统一使用 CloudWeGo Hertz 框架**,log 模块已实现 Hertz trace 中间件支持。

```go
import (
    "code.wenfu.cn/stablepay/stablepay-common/log/middleware"
    "github.com/cloudwego/hertz/pkg/app/server"
)

h := server.Default()

// 添加 trace 中间件(自动注入 trace_id 到 context 和响应头)
h.Use(middleware.HertzTrace("your-service"))
```

#### Hertz 中间件功能

**HertzTrace 中间件**:
- 自动为每个请求生成或提取 trace_id
- 将 trace_id 注入到响应头 `X-Trace-ID`
- 自动从请求头提取上游服务的 trace context
- 支持 OpenTelemetry 标准的 trace propagation
- 将 trace context 注入到请求 context，供业务代码使用

#### 完整示例

```go
package main

import (
    "context"
    "code.wenfu.cn/stablepay/stablepay-common/log"
    "code.wenfu.cn/stablepay/stablepay-common/log/middleware"
    "github.com/cloudwego/hertz/pkg/app"
    "github.com/cloudwego/hertz/pkg/app/server"
)

func main() {
    // 初始化 logger
    config := log.NewConfig("your-service", "1.0.0")
    logger, _ := log.NewLogger(config)
    defer logger.Sync()

    // 设置全局 logger
    log.SetGlobalLogger(logger)

    // 创建 Hertz 服务器
    h := server.Default()

    // 注册 trace 中间件
    h.Use(middleware.HertzTrace("your-service"))

    // 注册路由
    h.GET("/api/test", func(ctx context.Context, c *app.RequestContext) {
        // 使用全局 logger 记录日志(自动包含 trace_id)
        log.InfoNoCtx("处理测试请求", map[string]interface{}{
            "path": string(c.Path()),
        })

        c.JSON(200, map[string]string{
            "message": "success",
        })
    })

    h.Spin()
}
```

## 版本历史

### v0.0.0-20251113064431 (2025-11-13)

**重要更新: 修复 OpenTelemetry TracerProvider 初始化**

#### 修改内容

1. **logger.go**: 修改 initTracerProvider 调用,传递 TracingConfig 参数
2. **context.go**: 修改 initTracerProvider 函数签名,接受 TracingConfig 参数,添加详细注释说明 TracerProvider 和 Propagator 的作用
3. **tracing_middleware.go**: 更新注释,说明不再需要手动传递 trace 信息

#### 问题修复

修复了 trace ID 无法在服务间传播的问题:
- PayAdmin 调用 MerchantCore 时,trace ID 不一致
- 根本原因: initTracerProvider 需要正确设置全局 TracerProvider
- 解决方案: 确保在 NewLogger 时调用 initTracerProvider 设置全局 provider

#### 技术说明

OpenTelemetry trace propagation 需要两个关键组件:
1. **TracerProvider**: 创建和管理 tracer
2. **TextMapPropagator**: 在 HTTP/RPC headers 中传播 trace context

kitex-contrib/obs-opentelemetry 的 tracing.NewClientSuite() 会自动:
- 从 context 提取 trace 信息
- 注入到 RPC 元数据中传递给下游服务

## 最佳实践

1. **始终通过 Context 传递 Logger**: 使用 `log.WithLogger(ctx, logger)` 和 `log.GetLoggerFromContext(ctx)`
2. **启用 Tracing**: 在生产环境启用 tracing 配置,便于问题排查
3. **使用结构化日志**: 使用 map 传递日志字段,而不是字符串拼接
4. **设置 Request ID**: 在请求入口设置 request_id,便于追踪请求链路
5. **合理使用日志级别**:
    - Debug: 详细的调试信息
    - Info: 一般的业务流程信息
    - Warn: 警告信息,不影响业务
    - Error: 错误信息,影响业务

## 依赖说明

- `go.uber.org/zap`: 高性能日志库
- `go.opentelemetry.io/otel`: OpenTelemetry SDK
- `github.com/cloudwego/kitex`: 字节跳动 RPC 框架
- `github.com/kitex-contrib/obs-opentelemetry`: Kitex OpenTelemetry 集成
- `gopkg.in/natefinch/lumberjack.v2`: 日志文件轮转

## 支持

如有问题,请联系 StablePay 团队或提交 Issue。