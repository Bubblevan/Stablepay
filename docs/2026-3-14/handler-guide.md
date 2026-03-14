# Handler 编写完整指南

## 1. 什么是 Handler？

### 1.1 概念解释

**Handler（处理器）** 是处理业务逻辑的核心组件，接收请求、处理数据、返回响应。

**类比理解**：
```
餐厅场景：
  顾客（Client）→ 服务员（Router）→ 厨师（Handler）→ 出菜（Response）

微服务场景：
  请求 → API Gateway → Handler → 响应
              ↓
        调用其他服务/RPC
```

### 1.2 Handler 的职责

| 职责 | 说明 | 示例 |
|------|------|------|
| **参数校验** | 检查请求参数合法性 | 检查 DID 格式是否正确 |
| **业务逻辑** | 执行核心业务流程 | 创建 DID、处理支付 |
| **调用依赖** | 调用其他服务或数据库 | 调用区块链服务转账 |
| **异常处理** | 处理各种错误情况 | 余额不足、网络超时 |
| **响应封装** | 返回统一格式的响应 | 成功/失败的标准格式 |

## 2. Handler 的结构

### 2.1 标准 Handler 结构

```go
// internal/handler/did_handler.go

package handler

import (
    "context"
    "fmt"

    "stablepay/did-service/internal/handler/kitex_gen/stablepay/did"
)

// DIDHandler 处理 DID 相关业务
type DIDHandler struct {
    // 依赖注入
    db      *Database      // 数据库
    cache   *Cache         // 缓存
    rpcClient *RPCClient   // RPC 客户端（如果需要调用其他服务）
}

// NewDIDHandler 创建 Handler 实例（依赖注入）
func NewDIDHandler(db *Database, cache *Cache) *DIDHandler {
    return &DIDHandler{
        db:    db,
        cache: cache,
    }
}

// 确保实现了接口（编译期检查）
var _ did.DIDService = (*DIDHandler)(nil)

// CreateDID 实现创建 DID 接口
func (h *DIDHandler) CreateDID(
    ctx context.Context,
    req *did.CreateDIDRequest,
) (*did.CreateDIDResponse, error) {
    // 1. 参数校验
    if err := h.validateCreateDIDRequest(req); err != nil {
        return nil, err
    }

    // 2. 业务逻辑
    result, err := h.doCreateDID(ctx, req)
    if err != nil {
        return nil, err
    }

    // 3. 返回响应
    return result, nil
}

// 私有方法：参数校验
func (h *DIDHandler) validateCreateDIDRequest(req *did.CreateDIDRequest) error {
    if req.UserType == "" {
        return fmt.Errorf("user_type is required")
    }
    if req.UserType != "agent" && req.UserType != "developer" {
        return fmt.Errorf("invalid user_type: %s", req.UserType)
    }
    return nil
}

// 私有方法：执行业务逻辑
func (h *DIDHandler) doCreateDID(
    ctx context.Context,
    req *did.CreateDIDRequest,
) (*did.CreateDIDResponse, error) {
    // 实现业务逻辑
    // ...
}
```

### 2.2 分层架构

```
handler/
├── handler.go           # Handler 接口定义（Thrift 生成）
├── did_handler.go       # 业务 Handler 实现
│   ├── 参数校验层
│   ├── 业务逻辑层
│   └── 响应封装层
└── converter.go         # 数据转换工具
```

## 3. 完整示例：DID Handler

### 3.1 Thrift 生成的基础代码

```go
// kitex_gen/stablepay/did/did_service.go

// DIDService 接口（Thrift 自动生成）
type DIDService interface {
    CreateDID(ctx context.Context, req *CreateDIDRequest) (resp *CreateDIDResponse, err error)
    GetDID(ctx context.Context, req *GetDIDRequest) (resp *GetDIDResponse, err error)
    VerifySignature(ctx context.Context, req *VerifySignatureRequest) (resp *VerifySignatureResponse, err error)
}

// 请求/响应结构（Thrift 自动生成）
type CreateDIDRequest struct {
    UserType string `thrift:"user_type,1" json:"user_type"`
    Metadata string `thrift:"metadata,2" json:"metadata"`
}

type CreateDIDResponse struct {
    Did           string `thrift:"did,1" json:"did"`
    PublicKey     string `thrift:"public_key,2" json:"public_key"`
    WalletAddress string `thrift:"wallet_address,3" json:"wallet_address"`
    Status        string `thrift:"status,4" json:"status"`
    CreatedAt     string `thrift:"created_at,5" json:"created_at"`
}
```

### 3.2 Handler 实现

```go
// internal/handler/did_handler.go

package handler

import (
    "context"
    "crypto/ed25519"
    "encoding/base64"
    "fmt"
    "time"

    "github.com/google/uuid"
    "stablepay/did-service/internal/domain"
    "stablepay/did-service/internal/handler/kitex_gen/stablepay/did"
    "stablepay/did-service/internal/repository"
)

// DIDHandler 实现 DID 服务接口
type DIDHandler struct {
    repo repository.DIDRepository
}

// NewDIDHandler 创建 Handler
func NewDIDHandler(repo repository.DIDRepository) *DIDHandler {
    return &DIDHandler{repo: repo}
}

// 编译期检查接口实现
var _ did.DIDService = (*DIDHandler)(nil)

// CreateDID 创建新的 DID
func (h *DIDHandler) CreateDID(
    ctx context.Context,
    req *did.CreateDIDRequest,
) (*did.CreateDIDResponse, error) {
    // ========== 第1层：参数校验 ==========
    if err := validateCreateRequest(req); err != nil {
        return nil, fmt.Errorf("invalid request: %w", err)
    }

    // ========== 第2层：业务处理 ==========
    // 2.1 生成密钥对
    publicKey, privateKey, err := generateKeyPair()
    if err != nil {
        return nil, fmt.Errorf("generate key pair failed: %w", err)
    }

    // 2.2 构造 DID
    didID := fmt.Sprintf("did:solana:%s", base58Encode(publicKey))

    // 2.3 创建领域对象
    didEntity := &domain.DID{
        ID:            didID,
        PublicKey:     base58Encode(publicKey),
        WalletAddress: deriveWalletAddress(publicKey),
        UserType:      req.UserType,
        Status:        "active",
        CreatedAt:     time.Now(),
        Metadata:      req.Metadata,
    }

    // 2.4 保存到数据库
    if err := h.repo.Save(ctx, didEntity); err != nil {
        return nil, fmt.Errorf("save did failed: %w", err)
    }

    // 2.5 保存私钥（实际应该加密存储）
    // ...

    // ========== 第3层：响应封装 ==========
    return &did.CreateDIDResponse{
        Did:           didEntity.ID,
        PublicKey:     didEntity.PublicKey,
        WalletAddress: didEntity.WalletAddress,
        Status:        didEntity.Status,
        CreatedAt:     didEntity.CreatedAt.Format(time.RFC3339),
    }, nil
}

// GetDID 查询 DID 信息
func (h *DIDHandler) GetDID(
    ctx context.Context,
    req *did.GetDIDRequest,
) (*did.GetDIDResponse, error) {
    // 参数校验
    if req.DidID == "" {
        return nil, fmt.Errorf("did_id is required")
    }

    // 查询数据库
    didEntity, err := h.repo.FindByID(ctx, req.DidID)
    if err != nil {
        if err == repository.ErrNotFound {
            return nil, &did.DIDNotFoundException{
                Code:    10001,
                Message: fmt.Sprintf("DID not found: %s", req.DidID),
            }
        }
        return nil, fmt.Errorf("query did failed: %w", err)
    }

    // 返回响应
    return &did.GetDIDResponse{
        Did:           didEntity.ID,
        PublicKey:     didEntity.PublicKey,
        WalletAddress: didEntity.WalletAddress,
        Status:        didEntity.Status,
        CreatedAt:     didEntity.CreatedAt.Format(time.RFC3339),
    }, nil
}

// VerifySignature 验证签名
func (h *DIDHandler) VerifySignature(
    ctx context.Context,
    req *did.VerifySignatureRequest,
) (*did.VerifySignatureResponse, error) {
    // 参数校验
    if req.Did == "" || req.Message == "" || req.Signature == "" {
        return &did.VerifySignatureResponse{
            Valid:         false,
            ErrorMessage:  strPtr("missing required fields"),
        }, nil
    }

    // 查询 DID 获取公钥
    didEntity, err := h.repo.FindByID(ctx, req.DidID)
    if err != nil {
        return &did.VerifySignatureResponse{
            Valid:        false,
            ErrorMessage: strPtr("DID not found"),
        }, nil
    }

    // 解码公钥
    publicKey, err := base58Decode(didEntity.PublicKey)
    if err != nil {
        return nil, fmt.Errorf("decode public key failed: %w", err)
    }

    // 解码签名
    sig, err := base64.StdEncoding.DecodeString(req.Signature)
    if err != nil {
        return &did.VerifySignatureResponse{
            Valid:        false,
            ErrorMessage: strPtr("invalid signature format"),
        }, nil
    }

    // 验证签名
    messageBytes := []byte(req.Message)
    valid := ed25519.Verify(publicKey, messageBytes, sig)

    return &did.VerifySignatureResponse{
        Valid: valid,
    }, nil
}

// ============ 私有方法 ============

func validateCreateRequest(req *did.CreateDIDRequest) error {
    if req.UserType == "" {
        return fmt.Errorf("user_type is required")
    }
    if req.UserType != "agent" && req.UserType != "developer" {
        return fmt.Errorf("invalid user_type, must be 'agent' or 'developer'")
    }
    return nil
}

func generateKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
    return ed25519.GenerateKey(nil)
}

func base58Encode(data []byte) string {
    // 使用 base58 编码（Solana 标准）
    // 这里简化实现，实际使用 github.com/mr-tron/base58
    return base64.StdEncoding.EncodeToString(data)
}

func base58Decode(s string) ([]byte, error) {
    return base64.StdEncoding.DecodeString(s)
}

func deriveWalletAddress(publicKey []byte) string {
    // 简化实现，实际应该使用 Solana 的地址派生算法
    return base58Encode(publicKey)
}

func strPtr(s string) *string {
    return &s
}
```

### 3.3 Repository 接口定义

```go
// internal/repository/did_repository.go

package repository

import (
    "context"
    "errors"
    "stablepay/did-service/internal/domain"
)

var ErrNotFound = errors.New("record not found")

// DIDRepository 定义数据访问接口
type DIDRepository interface {
    Save(ctx context.Context, did *domain.DID) error
    FindByID(ctx context.Context, id string) (*domain.DID, error)
    Update(ctx context.Context, did *domain.DID) error
    Delete(ctx context.Context, id string) error
}
```

### 3.4 领域对象定义

```go
// internal/domain/did.go

package domain

import "time"

// DID 领域对象
type DID struct {
    ID            string
    PublicKey     string
    PrivateKey    string // 加密存储
    WalletAddress string
    UserType      string // agent / developer
    Status        string // active / disabled
    CreatedAt     time.Time
    UpdatedAt     time.Time
    Metadata      string
}
```

## 4. Payment Handler 示例

### 4.1 复杂业务场景

```go
// internal/handler/payment_handler.go

package handler

import (
    "context"
    "fmt"
    "time"

    "github.com/google/uuid"
    "stablepay/payment-service/internal/clients"
    "stablepay/payment-service/internal/handler/kitex_gen/stablepay/payment"
    "stablepay/payment-service/internal/repository"
)

// PaymentHandler 处理支付业务
type PaymentHandler struct {
    txRepo       repository.TransactionRepository
    blockchain   *clients.BlockchainClient  // 调用区块链服务
    didClient    *clients.DIDClient         // 调用 DID 服务
}

func NewPaymentHandler(
    txRepo repository.TransactionRepository,
    blockchain *clients.BlockchainClient,
    didClient *clients.DIDClient,
) *PaymentHandler {
    return &PaymentHandler{
        txRepo:     txRepo,
        blockchain: blockchain,
        didClient:  didClient,
    }
}

// Pay 处理支付请求
func (h *PaymentHandler) Pay(
    ctx context.Context,
    req *payment.PayRequest,
) (*payment.PayResponse, error) {
    // ========== 第1层：参数校验 ==========
    if err := h.validatePayRequest(req); err != nil {
        return nil, fmt.Errorf("invalid request: %w", err)
    }

    // ========== 第2层：前置检查 ==========
    // 2.1 验证 Agent DID 存在
    agentDID, err := h.didClient.GetDID(ctx, req.AgentDid)
    if err != nil {
        return nil, fmt.Errorf("agent DID not found: %w", err)
    }

    // 2.2 验证 Skill DID 存在
    skillDID, err := h.didClient.GetDID(ctx, req.SkillDid)
    if err != nil {
        return nil, fmt.Errorf("skill DID not found: %w", err)
    }

    // 2.3 检查余额（调用区块链）
    balance, err := h.blockchain.GetBalance(ctx, agentDID.WalletAddress)
    if err != nil {
        return nil, fmt.Errorf("check balance failed: %w", err)
    }
    if balance < req.Amount {
        return nil, &payment.InsufficientBalanceException{
            Code:    20001,
            Message: fmt.Sprintf("insufficient balance: have %.2f, need %.2f", balance, req.Amount),
        }
    }

    // ========== 第3层：执行支付 ==========
    // 3.1 创建交易记录（pending 状态）
    txID := uuid.New().String()
    txRecord := &repository.Transaction{
        ID:        txID,
        AgentDID:  req.AgentDid,
        SkillDID:  req.SkillDid,
        Amount:    req.Amount,
        Currency:  req.Currency,
        Status:    "pending",
        CreatedAt: time.Now(),
    }
    if err := h.txRepo.Save(ctx, txRecord); err != nil {
        return nil, fmt.Errorf("save transaction failed: %w", err)
    }

    // 3.2 执行链上转账
    transferResult, err := h.blockchain.Transfer(ctx, clients.TransferRequest{
        FromAddress: agentDID.WalletAddress,
        ToAddress:   skillDID.WalletAddress,
        Amount:      req.Amount,
        Currency:    req.Currency,
    })

    if err != nil {
        // 更新交易状态为失败
        txRecord.Status = "failed"
        txRecord.ErrorMessage = err.Error()
        h.txRepo.Update(ctx, txRecord)

        return nil, fmt.Errorf("blockchain transfer failed: %w", err)
    }

    // 3.3 更新交易记录为成功
    txRecord.Status = "confirmed"
    txRecord.TxHash = transferResult.TxHash
    txRecord.ConfirmedAt = time.Now()
    if err := h.txRepo.Update(ctx, txRecord); err != nil {
        // 这里只是记录更新失败，不返回错误，因为转账已经成功了
        // 应该记录日志，后续补偿
        fmt.Printf("WARN: failed to update transaction status: %v\n", err)
    }

    // ========== 第4层：返回响应 ==========
    return &payment.PayResponse{
        TxID:        txID,
        TxHash:      transferResult.TxHash,
        Status:      "confirmed",
        ConfirmedAt: txRecord.ConfirmedAt.Format(time.RFC3339),
    }, nil
}

// validatePayRequest 参数校验
func (h *PaymentHandler) validatePayRequest(req *payment.PayRequest) error {
    if req.AgentDid == "" {
        return fmt.Errorf("agent_did is required")
    }
    if req.SkillDid == "" {
        return fmt.Errorf("skill_did is required")
    }
    if req.Amount <= 0 {
        return fmt.Errorf("amount must be positive")
    }
    if req.Currency == "" {
        return fmt.Errorf("currency is required")
    }
    if req.Currency != "USDC" && req.Currency != "USDT" {
        return fmt.Errorf("unsupported currency: %s", req.Currency)
    }
    return nil
}
```

## 5. Handler 设计模式

### 5.1 模板方法模式

```go
// 基础 Handler 模板
type BaseHandler struct {
    validators []Validator
    middleware []Middleware
}

func (h *BaseHandler) Handle(ctx context.Context, req interface{}) (interface{}, error) {
    // 1. 执行中间件前置逻辑
    for _, mw := range h.middleware {
        if err := mw.Before(ctx, req); err != nil {
            return nil, err
        }
    }

    // 2. 参数校验
    for _, v := range h.validators {
        if err := v.Validate(req); err != nil {
            return nil, err
        }
    }

    // 3. 执行业务逻辑（子类实现）
    resp, err := h.doHandle(ctx, req)

    // 4. 执行中间件后置逻辑
    for i := len(h.middleware) - 1; i >= 0; i-- {
        h.middleware[i].After(ctx, resp, err)
    }

    return resp, err
}
```

### 5.2 职责链模式

```go
// 处理链
type HandlerChain struct {
    handlers []Handler
}

func (c *HandlerChain) Add(h Handler) {
    c.handlers = append(c.handlers, h)
}

func (c *HandlerChain) Execute(ctx context.Context, req interface{}) (interface{}, error) {
    for _, h := range c.handlers {
        resp, err := h.Handle(ctx, req)
        if err != nil {
            return nil, err
        }
        // 转换请求，传递给下一个 Handler
        req = resp
    }
    return req, nil
}
```

## 6. 错误处理最佳实践

### 6.1 错误分类

```go
// 定义错误类型
var (
    // 客户端错误（4xx）
    ErrInvalidRequest = errors.New("invalid request")
    ErrUnauthorized   = errors.New("unauthorized")
    ErrNotFound       = errors.New("not found")

    // 服务端错误（5xx）
    ErrInternal       = errors.New("internal error")
    ErrDatabase       = errors.New("database error")
    ErrExternal       = errors.New("external service error")
)

// 包装错误
func wrapError(err error, category string) error {
    return fmt.Errorf("[%s] %w", category, err)
}
```

### 6.2 Handler 中的错误处理

```go
func (h *Handler) SomeMethod(ctx context.Context, req *Request) (*Response, error) {
    // 业务错误 -> 返回特定异常
    if req.InvalidField != "" {
        return nil, &BusinessException{
            Code:    10001,
            Message: "invalid field",
        }
    }

    // 数据库错误 -> 包装后返回
    result, err := h.repo.Find(ctx, req.ID)
    if err != nil {
        if errors.Is(err, repository.ErrNotFound) {
            return nil, &BusinessException{
                Code:    10002,
                Message: "record not found",
            }
        }
        // 内部错误，不暴露细节
        return nil, fmt.Errorf("database error: %w", ErrInternal)
    }

    // 外部服务错误 -> 包装后返回
    resp, err := h.externalClient.Call(ctx, result)
    if err != nil {
        return nil, fmt.Errorf("external service error: %w", ErrExternal)
    }

    return resp, nil
}
```

## 7. 日志记录

### 7.1 结构化日志

```go
func (h *Handler) CreateDID(ctx context.Context, req *did.CreateDIDRequest) (*did.CreateDIDResponse, error) {
    // 记录请求
    h.logger.Info("CreateDID request",
        "user_type", req.UserType,
        "trace_id", getTraceID(ctx),
    )

    // 执行业务
    resp, err := h.doCreateDID(ctx, req)

    // 记录结果
    if err != nil {
        h.logger.Error("CreateDID failed",
            "error", err.Error(),
            "user_type", req.UserType,
        )
        return nil, err
    }

    h.logger.Info("CreateDID success",
        "did", resp.Did,
        "wallet", resp.WalletAddress,
    )

    return resp, nil
}
```

### 7.2 日志上下文

```go
func getTraceID(ctx context.Context) string {
    if id := ctx.Value("trace_id"); id != nil {
        return id.(string)
    }
    return "unknown"
}
```

## 8. 性能优化

### 8.1 连接池复用

```go
// 在 Handler 初始化时创建客户端，不要每次请求都创建
type Handler struct {
    db     *sql.DB      // 复用数据库连接池
    cache  *redis.Client // 复用 Redis 连接
    client RPCClient     // 复用 RPC 连接
}
```

### 8.2 异步处理

```go
func (h *Handler) Pay(ctx context.Context, req *Request) (*Response, error) {
    // 同步处理核心逻辑
    result, err := h.processCore(ctx, req)
    if err != nil {
        return nil, err
    }

    // 异步处理非核心逻辑
    go func() {
        // 发送通知
        h.sendNotification(ctx, result)
        // 更新统计
        h.updateStats(ctx, result)
    }()

    return result, nil
}
```

### 8.3 缓存优化

```go
func (h *Handler) GetDID(ctx context.Context, req *GetDIDRequest) (*GetDIDResponse, error) {
    cacheKey := fmt.Sprintf("did:%s", req.DidID)

    // 1. 先查缓存
    if cached, err := h.cache.Get(ctx, cacheKey); err == nil {
        return cached.(*GetDIDResponse), nil
    }

    // 2. 查数据库
    did, err := h.repo.FindByID(ctx, req.DidID)
    if err != nil {
        return nil, err
    }

    resp := convertToResponse(did)

    // 3. 写入缓存
    h.cache.Set(ctx, cacheKey, resp, time.Hour)

    return resp, nil
}
```

## 9. 常见陷阱

### 陷阱1：Handler 太臃肿

```go
// ❌ 坏的实践：Handler 做了所有事情
func (h *Handler) DoEverything(ctx context.Context, req *Request) (*Response, error) {
    // 校验参数
    // 查询数据库
    // 调用 RPC
    // 处理业务逻辑
    // 写数据库
    // 发送消息
    // 记录日志
    // ... 几百行代码
}

// ✅ 好的实践：分层处理
func (h *Handler) Handle(ctx context.Context, req *Request) (*Response, error) {
    // 只负责协调
    if err := h.validator.Validate(req); err != nil {
        return nil, err
    }

    result, err := h.service.Process(ctx, req)
    if err != nil {
        return nil, err
    }

    return h.converter.ToResponse(result), nil
}
```

### 陷阱2：不处理上下文取消

```go
// ❌ 坏的实践
func (h *Handler) LongOperation(ctx context.Context) error {
    for i := 0; i < 1000; i++ {
        doWork() // 不检查 ctx 是否被取消
    }
    return nil
}

// ✅ 好的实践
func (h *Handler) LongOperation(ctx context.Context) error {
    for i := 0; i < 1000; i++ {
        select {
        case <-ctx.Done():
            return ctx.Err() // 及时响应取消信号
        default:
            doWork()
        }
    }
    return nil
}
```

### 陷阱3：忽略资源释放

```go
// ❌ 坏的实践
func (h *Handler) Query(ctx context.Context) (*Response, error) {
    rows, _ := h.db.Query("SELECT * FROM table")
    // 如果这里返回错误，rows 不会被关闭
    if someCondition {
        return nil, errors.New("error")
    }
    rows.Close()
    return &Response{}, nil
}

// ✅ 好的实践
func (h *Handler) Query(ctx context.Context) (*Response, error) {
    rows, err := h.db.Query("SELECT * FROM table")
    if err != nil {
        return nil, err
    }
    defer rows.Close() // 确保关闭

    // ... 处理 rows

    return &Response{}, nil
}
```

## 10. 参考目录结构

```
service-name/
├── cmd/
│   └── service-name/
│       └── main.go              # 入口
├── internal/
│   ├── handler/                 # Handler 实现
│   │   ├── kitex_gen/          # Thrift 生成代码
│   │   ├── did_handler.go      # Handler 实现
│   │   └── payment_handler.go
│   ├── service/                 # 业务逻辑层（可选）
│   │   ├── did_service.go
│   │   └── payment_service.go
│   ├── domain/                  # 领域对象
│   │   ├── did.go
│   │   └── transaction.go
│   ├── repository/              # 数据访问层
│   │   ├── did_repository.go
│   │   └── transaction_repository.go
│   └── clients/                 # 外部客户端
│       ├── blockchain_client.go
│       └── did_client.go
├── idl/
│   └── service.thrift          # Thrift 定义
├── configs/
│   └── config.yaml
├── go.mod
└── README.md
```

---

**相关文档**:
- [Thrift 生成 Go 接口指南](./thrift-to-go-guide.md)
- [Kitex 框架指南](./kitex-guide.md)
- [Mock 测试指南](./mock-testing-guide.md)
- [单元测试指南](./unit-testing-guide.md)
