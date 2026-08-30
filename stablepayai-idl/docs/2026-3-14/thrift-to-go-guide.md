# Thrift 生成 Go 接口完整指南

## 1. 什么是 Thrift？

### 1.1 概念解释

Thrift 是一种**接口定义语言（IDL）**，用来描述服务之间的通信接口。

**类比理解**：
- 就像写 API 文档，但 Thrift 是机器可读的
- 写一次 Thrift 文件，可以生成多种语言的代码（Go、Java、Python 等）
- 确保不同服务之间的接口一致

### 1.2 为什么用 Thrift？

```
传统方式：
  服务A开发者："我定义了一个接口 /api/v1/did/create"
  服务B开发者："收到，我来调用"
  结果：字段对不上、类型不一致、反复沟通

Thrift 方式：
  写一个 did-service.thrift
  生成 Go 代码给服务A用
  生成 Go 代码给服务B用
  结果：双方代码完全一致，直接编译通过
```

## 2. Thrift 语法基础

### 2.1 基本结构

```thrift
// did-service.thrift

namespace go stablepay.did  // 生成的 Go 包名

// 1. 定义数据结构（struct）
struct DID {
    1: string did           // 1 是字段编号，必须唯一
    2: string public_key
    3: string wallet_address
    4: string created_at
}

struct CreateDIDRequest {
    1: string user_type     // "agent" 或 "developer"
    2: optional string metadata  // optional 表示可选字段
}

struct CreateDIDResponse {
    1: DID did
    2: string status
}

// 2. 定义异常
exception DIDNotFoundException {
    1: string message
    2: i32 code
}

// 3. 定义服务接口
service DIDService {
    // 创建 DID
    CreateDIDResponse CreateDID(1: CreateDIDRequest req)
        throws (1: DIDNotFoundException ex)

    // 查询 DID
    DID GetDID(1: string did_id)
        throws (1: DIDNotFoundException ex)

    // 验证签名
    bool VerifySignature(1: string did, 2: string message, 3: string signature)
}
```

### 2.2 常用数据类型

| Thrift 类型 | Go 类型 | 说明 |
|------------|---------|------|
| `bool` | `bool` | 布尔值 |
| `i8/i16/i32/i64` | `int8/int16/int32/int64` | 整数 |
| `double` | `float64` | 浮点数 |
| `string` | `string` | 字符串 |
| `binary` | `[]byte` | 二进制数据 |
| `list<T>` | `[]T` | 数组 |
| `set<T>` | `map[T]struct{}` | 集合 |
| `map<K, V>` | `map[K]V` | 字典 |
| `optional` | 指针类型 | 可选字段 |

### 2.3 命名规范

```thrift
// ✅ 正确的命名方式

// 文件名：小写下划线
// did_service.thrift

// 结构体：PascalCase
struct CreateDIDRequest {}
struct DIDInfo {}

// 字段：snake_case
struct User {
    1: string user_name
    2: i32 age
}

// 服务名：PascalCase
service DIDService {}
service PaymentService {}

// 方法名：PascalCase（Go 风格）
service DIDService {
    CreateDIDResponse CreateDID(1: CreateDIDRequest req)
    DID GetDIDByID(1: string did_id)
}
```

## 3. 生成 Go 代码

### 3.1 安装工具

```bash
# 1. 安装 thriftgo（Thrift 编译器）
go install github.com/cloudwego/thriftgo@latest

# 2. 安装 kitex 工具
go install github.com/cloudwego/kitex/tool/cmd/kitex@latest

# 3. 验证安装
thriftgo --version
kitex --version
```

### 3.2 项目结构准备

假设你的服务仓库结构：

```
did-service/                 # 你的服务仓库
├── idl/                     # IDL 文件目录
│   └── did_service.thrift   # Thrift 定义
├── cmd/
│   └── did-service/
│       └── main.go
├── internal/
│   └── handler/             # 生成的代码放这里
└── go.mod
```

### 3.3 生成代码命令

```bash
# 进入你的服务仓库
cd did-service

# 使用 kitex 生成代码
kitex \
  -module stablepay/did-service \      # go.mod 中的 module 名
  -service did-service \                # 服务名
  -gen-path internal/handler/kitex_gen \ # 生成代码的输出目录
  -type thrift \                        # 使用 thrift 协议
  idl/did_service.thrift                # Thrift 文件路径
```

### 3.4 生成的文件结构

运行命令后会生成：

```
internal/handler/kitex_gen/
└── stablepay/
    └── did/
        ├── didservice/              # 客户端代码
        │   ├── client.go            # 客户端创建
        │   ├── invoker.go           # 调用器
        │   └── didservice.go        # 接口定义
        ├── did_service.go           # 服务端接口
        └── k-did_service.go         # Kitex 专用代码
```

## 4. 实际示例：StablePay DID Service

### 4.1 完整 Thrift 定义

```thrift
// idl/did_service.thrift

namespace go stablepay.did

// 请求/响应结构
struct CreateDIDRequest {
    1: required string user_type    // required 表示必填
    2: optional string metadata
}

struct CreateDIDResponse {
    1: string did
    2: string public_key
    3: string wallet_address
    4: string created_at
    5: string status
}

struct GetDIDRequest {
    1: required string did_id
}

struct GetDIDResponse {
    1: string did
    2: string public_key
    3: string wallet_address
    4: string status
    5: string created_at
}

struct VerifySignatureRequest {
    1: required string did
    2: required string message
    3: required string signature
    4: required string timestamp
}

struct VerifySignatureResponse {
    1: bool valid
    2: optional string error_message
}

// 统一响应包装（可选）
struct BaseResponse {
    1: i32 code
    2: string message
    3: binary data
}

// 服务定义
service DIDService {
    // 创建新的 DID
    CreateDIDResponse CreateDID(1: CreateDIDRequest req)

    // 根据 DID 查询信息
    GetDIDResponse GetDID(1: GetDIDRequest req)

    // 验证签名
    VerifySignatureResponse VerifySignature(1: VerifySignatureRequest req)
}
```

### 4.2 生成代码

```bash
# 在项目根目录执行
kitex \
  -module stablepay/did-service \
  -service did-service \
  -gen-path internal/handler/kitex_gen \
  -type thrift \
  idl/did_service.thrift
```

### 4.3 生成的关键文件解析

#### 4.3.1 服务端接口（需要实现）

```go
// internal/handler/kitex_gen/stablepay/did/did_service.go

// DIDService 接口定义
// 你需要创建一个 struct 实现这个接口
type DIDService interface {
    CreateDID(ctx context.Context, req *CreateDIDRequest) (resp *CreateDIDResponse, err error)
    GetDID(ctx context.Context, req *GetDIDRequest) (resp *GetDIDResponse, err error)
    VerifySignature(ctx context.Context, req *VerifySignatureRequest) (resp *VerifySignatureResponse, err error)
}
```

#### 4.3.2 客户端代码（用来调用服务）

```go
// internal/handler/kitex_gen/stablepay/did/didservice/client.go

// 创建客户端的函数
func NewClient(destService string, opts ...client.Option) (Client, error) {
    // ...
}

// 使用示例（在其他服务中调用 did-service）
client, err := didservice.NewClient("did-service",
    client.WithHostPorts("localhost:8081"),
)
resp, err := client.CreateDID(context.Background(), &did.CreateDIDRequest{
    UserType: "agent",
})
```

## 5. 实现服务端 Handler

### 5.1 创建 handler 文件

```go
// internal/handler/did_handler.go

package handler

import (
    "context"
    "time"

    "github.com/google/uuid"
    "stablepay/did-service/internal/handler/kitex_gen/stablepay/did"
)

// DIDServiceImpl 实现 Thrift 生成的接口
type DIDServiceImpl struct{}

// 确保实现了接口
var _ did.DIDService = (*DIDServiceImpl)(nil)

// NewDIDServiceImpl 创建 Handler 实例
func NewDIDServiceImpl() *DIDServiceImpl {
    return &DIDServiceImpl{}
}

// CreateDID 实现创建 DID 接口
func (s *DIDServiceImpl) CreateDID(
    ctx context.Context,
    req *did.CreateDIDRequest,
) (*did.CreateDIDResponse, error) {
    // 1. 参数校验
    if req.UserType == "" {
        return nil, fmt.Errorf("user_type is required")
    }

    // 2. 生成 DID
    didID := "did:solana:" + uuid.New().String()

    // 3. 生成密钥对（实际项目调用加密库）
    publicKey := "mock_public_key_" + uuid.New().String()[:8]
    walletAddr := "mock_wallet_" + uuid.New().String()[:8]

    // 4. 保存到数据库（省略）

    // 5. 返回响应
    return &did.CreateDIDResponse{
        Did:           didID,
        PublicKey:     publicKey,
        WalletAddress: walletAddr,
        CreatedAt:     time.Now().Format(time.RFC3339),
        Status:        "active",
    }, nil
}

// GetDID 实现查询 DID 接口
func (s *DIDServiceImpl) GetDID(
    ctx context.Context,
    req *did.GetDIDRequest,
) (*did.GetDIDResponse, error) {
    // 1. 从数据库查询（省略）

    // 2. 返回结果
    return &did.GetDIDResponse{
        Did:           req.DidID,
        PublicKey:     "public_key_xxx",
        WalletAddress: "wallet_xxx",
        Status:        "active",
        CreatedAt:     time.Now().Format(time.RFC3339),
    }, nil
}

// VerifySignature 实现签名验证接口
func (s *DIDServiceImpl) VerifySignature(
    ctx context.Context,
    req *did.VerifySignatureRequest,
) (*did.VerifySignatureResponse, error) {
    // 1. 获取 DID 对应的公钥
    // 2. 验证签名
    // 3. 返回结果

    return &did.VerifySignatureResponse{
        Valid: req.Signature != "", // 简化判断
    }, nil
}
```

### 5.2 启动服务

```go
// cmd/did-service/main.go

package main

import (
    "log"

    "github.com/cloudwego/kitex/server"
    "stablepay/did-service/internal/handler"
    "stablepay/did-service/internal/handler/kitex_gen/stablepay/did"
)

func main() {
    // 1. 创建 handler 实例
    didHandler := handler.NewDIDServiceImpl()

    // 2. 创建服务
    svr := did.NewServer(
        didHandler,
        server.WithServerBasicInfo(&rpcinfo.EndpointBasicInfo{
            ServiceName: "did-service",
        }),
        server.WithServiceAddr(&net.TCPAddr{
            IP:   net.IPv4(0, 0, 0, 0),
            Port: 8081,  // 服务监听端口
        }),
    )

    // 3. 启动服务
    err := svr.Run()
    if err != nil {
        log.Fatal(err)
    }
}
```

## 6. 在客户端调用服务

### 6.1 API Gateway 中调用 DID Service

```go
// api-gateway/internal/clients/did_client.go

package clients

import (
    "context"
    "fmt"

    "github.com/cloudwego/kitex/client"
    "stablepay/api-gateway/internal/handler/kitex_gen/stablepay/did"
    "stablepay/api-gateway/internal/handler/kitex_gen/stablepay/did/didservice"
)

type DIDClient struct {
    client didservice.Client
}

// NewDIDClient 创建 DID 服务客户端
func NewDIDClient(addr string) (*DIDClient, error) {
    cli, err := didservice.NewClient(
        "did-service",
        client.WithHostPorts(addr),
    )
    if err != nil {
        return nil, fmt.Errorf("create did client: %w", err)
    }

    return &DIDClient{client: cli}, nil
}

// CreateDID 调用 DID 服务创建 DID
func (c *DIDClient) CreateDID(ctx context.Context, userType string) (*did.CreateDIDResponse, error) {
    resp, err := c.client.CreateDID(ctx, &did.CreateDIDRequest{
        UserType: userType,
    })
    if err != nil {
        return nil, fmt.Errorf("create did: %w", err)
    }
    return resp, nil
}

// GetDID 查询 DID 信息
func (c *DIDClient) GetDID(ctx context.Context, didID string) (*did.GetDIDResponse, error) {
    resp, err := c.client.GetDID(ctx, &did.GetDIDRequest{
        DidID: didID,
    })
    if err != nil {
        return nil, fmt.Errorf("get did: %w", err)
    }
    return resp, nil
}
```

### 6.2 配置和初始化

```go
// api-gateway/internal/app/bootstrap.go

func New(cfg *config.AppConfig) (*Instance, error) {
    // 1. 创建真实客户端（替换 Mock）
    didClient, err := clients.NewDIDClient(cfg.Downstream.DIDService)
    if err != nil {
        return nil, err
    }

    paymentClient, err := clients.NewPaymentClient(cfg.Downstream.PaymentService)
    if err != nil {
        return nil, err
    }

    // 2. 创建应用服务
    appService := application.NewService(didClient, paymentClient, ...)

    // 3. 设置路由
    // ...

    return &Instance{}, nil
}
```

## 7. 完整工作流总结

```
┌─────────────────────────────────────────────────────────────┐
│  Step 1: 定义 Thrift 接口                                    │
│  - 在 stablepayai-idl/idl/ 目录编写 .thrift 文件             │
│  - 定义 struct、service、方法                                 │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│  Step 2: 生成代码                                            │
│  - 在各服务仓库运行 kitex 命令                                │
│  - 生成 kitex_gen/ 目录                                       │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│  Step 3: 服务端实现 Handler                                  │
│  - 创建 handler.go 实现生成的接口                             │
│  - 编写业务逻辑                                               │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│  Step 4: 服务端启动                                          │
│  - main.go 中创建 server                                      │
│  - 监听指定端口                                               │
└─────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────┐
│  Step 5: 客户端调用                                          │
│  - 使用生成的 client 代码                                    │
│  - 配置服务端地址                                             │
└─────────────────────────────────────────────────────────────┘
```

## 8. 常见问题

### Q1: 修改 Thrift 后如何更新？

```bash
# 重新运行 kitex 命令即可
kitex -module stablepay/did-service -service did-service idl/did_service.thrift

# 注意：
# - 新增的字段用新的编号
# - 不要删除已有字段（向后兼容）
# - 可以修改 optional/required，但要小心
```

### Q2: 如何处理错误？

```thrift
// 在 Thrift 中定义异常
exception BusinessException {
    1: i32 code
    2: string message
}

service DIDService {
    CreateDIDResponse CreateDID(1: CreateDIDRequest req)
        throws (1: BusinessException ex)
}
```

```go
// Handler 中抛出异常
func (s *DIDServiceImpl) CreateDID(ctx context.Context, req *did.CreateDIDRequest) (*did.CreateDIDResponse, error) {
    if req.UserType == "" {
        return nil, &did.BusinessException{
            Code:    10001,
            Message: "user_type is required",
        }
    }
    // ...
}
```

### Q3: 如何传递上下文（如 trace_id）？

```go
// 客户端：注入 metadata
ctx := metainfo.WithValue(context.Background(), "trace_id", "xxx-xxx")
resp, err := client.CreateDID(ctx, req)

// 服务端：读取 metadata
traceID, ok := metainfo.GetValue(ctx, "trace_id")
```

## 9. 参考资源

- [Thrift 官方文档](https://thrift.apache.org/docs/idl)
- [Kitex 文档](https://www.cloudwego.io/docs/kitex/)
- [Thriftgo GitHub](https://github.com/cloudwego/thriftgo)

---

**下一步**：阅读 `kitex-guide.md` 了解 Kitex 框架的更多高级用法。
