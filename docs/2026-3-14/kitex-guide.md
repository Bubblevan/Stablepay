# Kitex 框架完整使用指南

## 1. Kitex 是什么？

### 1.1 基本概念

Kitex 是**字节跳动开源的 Go 微服务 RPC 框架**，基于 Thrift 协议实现服务间通信。

**类比理解**：
- 就像 Python 的 gRPC，但针对 Go 优化
- 自动生成客户端和服务端代码
- 内置服务治理（超时、重试、熔断等）

### 1.2 核心特性

| 特性 | 说明 |
|------|------|
| **高性能** | 基于 Netpoll 网络库，性能优于标准库 |
| **代码生成** | 从 Thrift 自动生成 Go 代码 |
| **服务治理** | 内置超时、重试、熔断、限流 |
| **扩展性** | 支持自定义中间件 |
| **多协议** | Thrift、Protobuf、gRPC 兼容 |

## 2. 快速开始

### 2.1 安装 Kitex 工具

```bash
# 安装 kitex 命令行工具
go install github.com/cloudwego/kitex/tool/cmd/kitex@latest

# 验证安装
kitex --version
# 输出: kitex version v0.x.x
```

### 2.2 创建最小服务

```bash
# 1. 创建项目目录
mkdir my-service && cd my-service

# 2. 初始化 go module
go mod init stablepay/my-service

# 3. 创建 Thrift 文件
mkdir -p idl
cat > idl/hello.thrift << 'EOF'
namespace go stablepay.my

service HelloService {
    string Hello(1: string name)
}
EOF

# 4. 生成代码
kitex -module stablepay/my-service -service my-service idl/hello.thrift

# 5. 查看生成的代码
ls -la kitex_gen/
```

### 2.3 实现 Handler

```go
// internal/handler/hello_handler.go

package handler

import (
    "context"
    "stablepay/my-service/kitex_gen/stablepay/my"
)

type HelloServiceImpl struct{}

func NewHelloServiceImpl() *HelloServiceImpl {
    return &HelloServiceImpl{}
}

func (s *HelloServiceImpl) Hello(ctx context.Context, name string) (string, error) {
    return "Hello, " + name + "!", nil
}
```

### 2.4 启动服务

```go
// main.go

package main

import (
    "log"
    "net"

    "github.com/cloudwego/kitex/server"
    "stablepay/my-service/internal/handler"
    "stablepay/my-service/kitex_gen/stablepay/my"
)

func main() {
    // 创建 handler
    h := handler.NewHelloServiceImpl()

    // 创建服务
    svr := my.NewServer(
        h,
        server.WithServiceAddr(&net.TCPAddr{
            IP:   net.IPv4(0, 0, 0, 0),
            Port: 8888,
        }),
    )

    // 运行
    if err := svr.Run(); err != nil {
        log.Fatal(err)
    }
}
```

### 2.5 调用服务

```go
// client/main.go

package main

import (
    "context"
    "fmt"
    "log"

    "github.com/cloudwego/kitex/client"
    "stablepay/my-service/kitex_gen/stablepay/my"
    "stablepay/my-service/kitex_gen/stablepay/my/myservice"
)

func main() {
    // 创建客户端
    cli, err := myservice.NewClient(
        "my-service",
        client.WithHostPorts("localhost:8888"),
    )
    if err != nil {
        log.Fatal(err)
    }

    // 调用
    resp, err := cli.Hello(context.Background(), "World")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(resp) // 输出: Hello, World!
}
```

## 3. Server 配置详解

### 3.1 基础配置

```go
package main

import (
    "github.com/cloudwego/kitex/server"
    "github.com/kitex-contrib/registry-nacos/nacos"
)

func main() {
    svr := myservice.NewServer(
        handler,

        // 1. 服务地址
        server.WithServiceAddr(&net.TCPAddr{
            IP:   net.IPv4(0, 0, 0, 0),
            Port: 8081,
        }),

        // 2. 服务名称（用于服务发现）
        server.WithServerBasicInfo(&rpcinfo.EndpointBasicInfo{
            ServiceName: "did-service",
        }),

        // 3. 超时配置
        server.WithReadWriteTimeout(time.Second * 5),

        // 4. 最大连接数
        server.WithMaxConnIdleTime(time.Minute * 10),

        // 5. 并发控制
        server.WithLimit(&limit.Option{
            MaxConnections: 1000,
            MaxQPS:         10000,
        }),
    )

    svr.Run()
}
```

### 3.2 高级配置

```go
func main() {
    svr := myservice.NewServer(
        handler,

        // 1. 使用多路复用（长连接复用）
        server.WithMuxTransport(),

        // 2. 设置统计间隔
        server.WithStatsLevel(stats.LevelDetailed),

        // 3. 自定义解码器（用于特殊序列化）
        server.WithPayloadCodec(thrift.NewThriftCodec()),

        // 4. 绑定服务发现注册
        // server.WithRegistry(registry.NewNacosRegistry()),

        // 5. 异常处理器
        server.WithErrorHandler(func(ctx context.Context, err error) error {
            log.Printf("handle error: %v", err)
            return err
        }),
    )

    svr.Run()
}
```

## 4. Client 配置详解

### 4.1 基础客户端

```go
import (
    "github.com/cloudwego/kitex/client"
)

func NewClient() (myservice.Client, error) {
    return myservice.NewClient(
        "my-service",  // 服务名

        // 1. 指定服务端地址（直连模式）
        client.WithHostPorts("localhost:8081"),

        // 2. 超时配置
        client.WithRPCTimeout(time.Second * 3),

        // 3. 连接超时
        client.WithConnectTimeout(time.Second),

        // 4. 失败重试
        client.WithFailureRetry(retry.NewFailurePolicy()),
    )
}
```

### 4.2 高级客户端配置

```go
func NewAdvancedClient() (myservice.Client, error) {
    return myservice.NewClient(
        "my-service",

        // 1. 连接池配置
        client.WithConnPool(&connpool.IdleConfig{
            MaxIdlePerAddress: 10,   // 每个地址最大空闲连接
            MaxIdleGlobal:     100,  // 全局最大空闲连接
            MaxIdleTimeout:    time.Minute,
        }),

        // 2. 熔断配置
        client.WithCircuitBreaker(circuitbreak.NewCBConfig(
            circuitbreak.WithMaxFail(5),
            circuitbreak.WithFailInterval(time.Second * 10),
        )),

        // 3. 负载均衡（多实例时使用）
        client.WithLoadBalancer(loadbalance.NewWeightedBalancer()),

        // 4. 中间件链
        client.WithMiddleware(mw.OpenTracingMiddleware()),

        // 5. 元数据传递
        client.WithMetaHandler(transmeta.ClientTTHeaderHandler),
    )
}
```

## 5. 中间件（Middleware）

### 5.1 什么是中间件？

中间件是**请求处理链上的拦截器**，可以在请求前后执行逻辑。

```
请求 → Middleware1 → Middleware2 → Handler → Middleware2 → Middleware1 → 响应
```

### 5.2 服务端中间件

```go
// internal/middleware/logger.go

package middleware

import (
    "context"
    "fmt"
    "time"

    "github.com/cloudwego/kitex/pkg/endpoint"
)

// LoggerMiddleware 日志中间件
func LoggerMiddleware(next endpoint.Endpoint) endpoint.Endpoint {
    return func(ctx context.Context, req, resp interface{}) error {
        start := time.Now()

        // 1. 请求前处理
        method := ctx.Value("method_name")
        fmt.Printf("[Request] method=%s, time=%s\n", method, start.Format("15:04:05"))

        // 2. 调用下一个处理器
        err := next(ctx, req, resp)

        // 3. 响应后处理
        duration := time.Since(start)
        if err != nil {
            fmt.Printf("[Error] method=%s, error=%v, duration=%v\n", method, err, duration)
        } else {
            fmt.Printf("[Success] method=%s, duration=%v\n", method, duration)
        }

        return err
    }
}

// 注册中间件
func main() {
    svr := myservice.NewServer(
        handler,
        server.WithMiddleware(LoggerMiddleware),
    )
    svr.Run()
}
```

### 5.3 客户端中间件

```go
// 客户端添加中间件
cli, _ := myservice.NewClient(
    "my-service",
    client.WithHostPorts("localhost:8081"),
    client.WithMiddleware(func(next endpoint.Endpoint) endpoint.Endpoint {
        return func(ctx context.Context, req, resp interface{}) error {
            // 添加请求头
            ctx = metainfo.WithValue(ctx, "trace_id", generateTraceID())

            // 发送请求
            err := next(ctx, req, resp)

            // 记录日志
            log.Printf("RPC call completed")

            return err
        }
    }),
)
```

### 5.4 常用中间件示例

#### 鉴权中间件

```go
func AuthMiddleware(secretKey string) endpoint.Middleware {
    return func(next endpoint.Endpoint) endpoint.Endpoint {
        return func(ctx context.Context, req, resp interface{}) error {
            // 从上下文获取 token
            token := metainfo.GetValue(ctx, "auth_token")

            // 验证 token
            if !validateToken(token, secretKey) {
                return fmt.Errorf("unauthorized")
            }

            return next(ctx, req, resp)
        }
    }
}
```

#### 限流中间件

```go
func RateLimitMiddleware(limit int) endpoint.Middleware {
    var count int32
    var lastReset time.Time

    return func(next endpoint.Endpoint) endpoint.Endpoint {
        return func(ctx context.Context, req, resp interface{}) error {
            // 简单计数限流
            if atomic.AddInt32(&count, 1) > int32(limit) {
                atomic.AddInt32(&count, -1)
                return fmt.Errorf("rate limit exceeded")
            }
            defer atomic.AddInt32(&count, -1)

            return next(ctx, req, resp)
        }
    }
}
```

## 6. 错误处理

### 6.1 Thrift 异常

```thrift
// idl/service.thrift

exception BusinessError {
    1: required i32 code
    2: required string message
    3: optional map<string, string> details
}

service MyService {
    string DoSomething(1: string input) throws (1: BusinessError err)
}
```

### 6.2 Handler 中抛出异常

```go
func (s *MyServiceImpl) DoSomething(ctx context.Context, input string) (string, error) {
    if input == "" {
        // 返回业务异常
        return "", &my.BusinessError{
            Code:    10001,
            Message: "input cannot be empty",
            Details: map[string]string{
                "field": "input",
            },
        }
    }

    return "success", nil
}
```

### 6.3 客户端捕获异常

```go
resp, err := cli.DoSomething(ctx, "")
if err != nil {
    // 判断是否为业务异常
    if bizErr, ok := err.(*my.BusinessError); ok {
        fmt.Printf("Business error: code=%d, message=%s\n", bizErr.Code, bizErr.Message)
    } else {
        fmt.Printf("System error: %v\n", err)
    }
}
```

## 7. 元数据传递

### 7.1 基本概念

微服务之间需要传递**上下文信息**（如 trace_id、user_id 等），但不属于业务参数。

### 7.2 使用 Metainfo

```go
import (
    "github.com/cloudwego/kitex/pkg/rpcinfo"
    "github.com/bytedance/gopkg/cloud/metainfo"
)

// 客户端：设置元数据
func callWithMetadata(ctx context.Context, cli myservice.Client) {
    // 设置持久化元数据（会传递给下游服务）
    ctx = metainfo.WithPersistentValue(ctx, "trace_id", "abc-123")
    ctx = metainfo.WithPersistentValue(ctx, "user_id", "user-456")

    // 设置临时元数据（仅当前请求有效）
    ctx = metainfo.WithValue(ctx, "request_time", time.Now().String())

    resp, err := cli.Hello(ctx, "World")
}

// 服务端：读取元数据
func (s *MyServiceImpl) Hello(ctx context.Context, name string) (string, error) {
    // 读取持久化元数据
    traceID, ok := metainfo.GetPersistentValue(ctx, "trace_id")
    if ok {
        log.Printf("trace_id: %s", traceID)
    }

    // 读取临时元数据
    reqTime, ok := metainfo.GetValue(ctx, "request_time")

    return "Hello, " + name, nil
}
```

## 8. 服务发现（进阶）

### 8.1 直连模式（开发期用）

```go
// 直接指定 IP:Port
cli, _ := myservice.NewClient(
    "my-service",
    client.WithHostPorts("127.0.0.1:8081"),
)
```

### 8.2 使用 Nacos 服务发现

```go
import (
    "github.com/kitex-contrib/registry-nacos/nacos"
)

func main() {
    // 创建 Nacos 注册中心
    r, err := nacos.NewNacosRegistry()
    if err != nil {
        log.Fatal(err)
    }

    // 服务端注册
    svr := myservice.NewServer(
        handler,
        server.WithRegistry(r),
    )

    // 客户端发现
    cli, _ := myservice.NewClient(
        "my-service",
        client.WithResolver(r),
    )
}
```

## 9. 完整示例：StablePay 服务间调用

### 9.1 场景描述

```
API Gateway (8080)
    ↓ RPC 调用
Payment Service (8081)
    ↓ RPC 调用
Blockchain Adapter (8082)
```

### 9.2 Payment Service 调用 Blockchain

```thrift
// idl/blockchain-adapter.thrift

namespace go stablepay.blockchain

struct TransferRequest {
    1: required string from_address
    2: required string to_address
    3: required string amount
    4: required string currency
}

struct TransferResponse {
    1: required string tx_hash
    2: required string status
    3: required string confirmed_at
}

service BlockchainAdapter {
    TransferResponse Transfer(1: TransferRequest req)
}
```

```go
// payment-service/internal/clients/blockchain_client.go

package clients

import (
    "context"
    "fmt"

    "github.com/cloudwego/kitex/client"
    "stablepay/payment-service/kitex_gen/stablepay/blockchain"
    "stablepay/payment-service/kitex_gen/stablepay/blockchain/blockchainadapter"
)

type BlockchainClient struct {
    cli blockchainadapter.Client
}

func NewBlockchainClient(addr string) (*BlockchainClient, error) {
    cli, err := blockchainadapter.NewClient(
        "blockchain-adapter",
        client.WithHostPorts(addr),
        client.WithRPCTimeout(time.Second * 30), // 链上操作可能较慢
    )
    if err != nil {
        return nil, fmt.Errorf("create blockchain client: %w", err)
    }

    return &BlockchainClient{cli: cli}, nil
}

func (c *BlockchainClient) Transfer(
    ctx context.Context,
    from, to, amount, currency string,
) (*blockchain.TransferResponse, error) {
    resp, err := c.cli.Transfer(ctx, &blockchain.TransferRequest{
        FromAddress: from,
        ToAddress:   to,
        Amount:      amount,
        Currency:    currency,
    })
    if err != nil {
        return nil, fmt.Errorf("transfer failed: %w", err)
    }

    return resp, nil
}
```

```go
// payment-service/internal/handler/payment_handler.go

type PaymentHandler struct {
    blockchainClient *clients.BlockchainClient
}

func (h *PaymentHandler) Pay(ctx context.Context, req *payment.PayRequest) (*payment.PayResponse, error) {
    // 1. 验证参数
    // ...

    // 2. 调用区块链服务转账
    transferResp, err := h.blockchainClient.Transfer(
        ctx,
        req.AgentAddress,
        req.SkillAddress,
        req.Amount,
        req.Currency,
    )
    if err != nil {
        return nil, fmt.Errorf("blockchain transfer failed: %w", err)
    }

    // 3. 保存交易记录
    // ...

    return &payment.PayResponse{
        TxID:        generateTxID(),
        TxHash:      transferResp.TxHash,
        Status:      transferResp.Status,
        ConfirmedAt: transferResp.ConfirmedAt,
    }, nil
}
```

## 10. 调试技巧

### 10.1 打印请求/响应

```go
// 在中间件中添加日志
func DebugMiddleware(next endpoint.Endpoint) endpoint.Endpoint {
    return func(ctx context.Context, req, resp interface{}) error {
        fmt.Printf("[Request] %+v\n", req)
        err := next(ctx, req, resp)
        fmt.Printf("[Response] %+v, error: %v\n", resp, err)
        return err
    }
}
```

### 10.2 使用 Kitex 内置工具

```go
import "github.com/cloudwego/kitex/pkg/klog"

// 启用调试日志
klog.SetLevel(klog.LevelDebug)

// 在代码中打印
klog.CtxDebugf(ctx, "processing request: %v", req)
```

### 10.3 超时调试

```go
// 检查超时设置
ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
defer cancel()

resp, err := cli.Hello(ctx, "World")
if err == context.DeadlineExceeded {
    log.Println("request timeout")
}
```

## 11. 性能优化

### 11.1 连接池调优

```go
client.WithConnPool(&connpool.IdleConfig{
    MaxIdlePerAddress: 100,    // 根据 QPS 调整
    MaxIdleGlobal:     1000,
    MaxIdleTimeout:    time.Minute * 5,
})
```

### 11.2 启用多路复用

```go
// 服务端启用
server.WithMuxTransport()

// 客户端使用共享连接
```

### 11.3 选择合适的序列化

```go
// Thrift 二进制（默认，性能好）
server.WithPayloadCodec(thrift.NewThriftCodec())

// Thrift 紧凑模式（包体积小）
server.WithPayloadCodec(thrift.NewThriftCodecWithConfig(thrift.FrugalRead|thrift.FrugalWrite))
```

## 12. 常见问题

### Q1: "connection refused" 错误

```
原因：客户端连不上服务端
解决：
1. 检查服务端是否启动
2. 检查地址和端口是否正确
3. 检查防火墙
```

### Q2: 请求超时

```go
// 增加超时时间
client.WithRPCTimeout(time.Second * 10)

// 或使用上下文控制
ctx, cancel := context.WithTimeout(ctx, time.Second*5)
defer cancel()
```

### Q3: 如何传递 HTTP Header 到 RPC？

```go
// API Gateway 中提取 Header，放入 metainfo
ctx = metainfo.WithPersistentValue(ctx, "user_agent", c.GetHeader("User-Agent"))

// RPC 服务中读取
userAgent, _ := metainfo.GetPersistentValue(ctx, "user_agent")
```

---

**相关文档**:
- [Thrift 生成 Go 接口指南](./thrift-to-go-guide.md)
- [Mock 测试指南](./mock-testing-guide.md)
- [单元测试指南](./unit-testing-guide.md)
