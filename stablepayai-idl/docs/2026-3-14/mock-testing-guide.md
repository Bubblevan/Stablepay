# Mock 测试完整指南

## 1. 什么是 Mock？

### 1.1 概念解释

**Mock（模拟）** 是用假的对象替代真实的依赖，用于隔离测试目标。

**现实类比**：
```
真实情况：
  你要测试一辆汽车（你的服务）
  但汽车需要发动机（依赖服务）
  发动机还没造好，怎么测试汽车？

Mock 方式：
  用一个假的发动机（Mock）
  它不会真的运转，但会返回预期的数据
  这样就可以先测试汽车的其他部件
```

### 1.2 为什么要用 Mock？

| 场景 | 说明 |
|------|------|
| **依赖未就绪** | 其他服务还没写好，先测试自己的服务 |
| **隔离测试** | 测试失败时，确定是自己的问题还是依赖的问题 |
| **控制数据** | 模拟各种边界情况（如余额不足、网络超时）|
| **提高速度** | 不发起真实网络请求，测试更快 |

### 1.3 Mock vs Stub vs Fake

| 类型 | 特点 | 使用场景 |
|------|------|---------|
| **Mock** | 验证行为（是否被调用、调用几次）| 需要验证交互逻辑 |
| **Stub** | 返回固定数据 | 简单替代依赖 |
| **Fake** | 简化实现（如内存数据库）| 需要部分真实逻辑 |

## 2. Go 中的 Mock 工具

### 2.1 常用工具对比

| 工具 | 特点 | 适用场景 |
|------|------|---------|
| **gomock** | 官方推荐，功能强大 | 需要验证调用行为 |
| ** testify/mock** | 简单易用 | 快速创建 Mock |
| **自己写** | 灵活控制 | 简单场景 |

### 2.2 安装 gomock

```bash
# 安装 gomock 工具
go install github.com/golang/mock/mockgen@latest

# 验证安装
mockgen --version
```

## 3. 手动编写 Mock（推荐初学者）

### 3.1 最简单的 Mock

```go
// 假设你有一个接口
type PaymentClient interface {
    Pay(ctx context.Context, amount float64) (string, error)
    GetBalance(ctx context.Context) (float64, error)
}

// 手动编写 Mock
type MockPaymentClient struct {
    PayFunc         func(ctx context.Context, amount float64) (string, error)
    GetBalanceFunc  func(ctx context.Context) (float64, error)
    PayCallCount    int
    LastPayAmount   float64
}

func (m *MockPaymentClient) Pay(ctx context.Context, amount float64) (string, error) {
    m.PayCallCount++
    m.LastPayAmount = amount
    if m.PayFunc != nil {
        return m.PayFunc(ctx, amount)
    }
    return "mock_tx_id", nil
}

func (m *MockPaymentClient) GetBalance(ctx context.Context) (float64, error) {
    if m.GetBalanceFunc != nil {
        return m.GetBalanceFunc(ctx)
    }
    return 100.0, nil
}
```

### 3.2 使用 Mock 进行测试

```go
// payment_service_test.go

func TestPaymentService_ProcessOrder(t *testing.T) {
    // 1. 创建 Mock
    mockClient := &MockPaymentClient{
        PayFunc: func(ctx context.Context, amount float64) (string, error) {
            if amount > 1000 {
                return "", errors.New("amount too large")
            }
            return "tx_12345", nil
        },
        GetBalanceFunc: func(ctx context.Context) (float64, error) {
            return 500.0, nil
        },
    }

    // 2. 创建被测服务，注入 Mock
    service := NewPaymentService(mockClient)

    // 3. 执行测试
    txID, err := service.ProcessOrder(context.Background(), 100.0)

    // 4. 验证结果
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
    if txID != "tx_12345" {
        t.Errorf("expected tx_12345, got %s", txID)
    }

    // 5. 验证 Mock 被调用
    if mockClient.PayCallCount != 1 {
        t.Errorf("Pay should be called once, got %d", mockClient.PayCallCount)
    }
    if mockClient.LastPayAmount != 100.0 {
        t.Errorf("expected amount 100.0, got %f", mockClient.LastPayAmount)
    }
}

// 测试边界情况
func TestPaymentService_ProcessOrder_AmountTooLarge(t *testing.T) {
    mockClient := &MockPaymentClient{
        PayFunc: func(ctx context.Context, amount float64) (string, error) {
            return "", errors.New("amount too large")
        },
    }

    service := NewPaymentService(mockClient)
    _, err := service.ProcessOrder(context.Background(), 2000.0)

    if err == nil {
        t.Error("expected error for large amount")
    }
}
```

## 4. 使用 gomock 自动生成

### 4.1 生成 Mock 代码

```bash
# 为接口生成 Mock
mockgen -source=client.go -destination=mock_client.go -package=mocks
```

### 4.2 实际示例

```go
// client.go - 原始接口
package payment

type BlockchainClient interface {
    Transfer(ctx context.Context, from, to, amount string) (*TransferResult, error)
    GetTransactionStatus(ctx context.Context, txHash string) (string, error)
}
```

```bash
# 生成 Mock
mockgen -source=client.go -destination=mocks/mock_blockchain.go -package=mocks
```

```go
// mocks/mock_blockchain.go - 生成的代码（部分）

package mocks

type MockBlockchainClient struct {
    ctrl     *gomock.Controller
    recorder *MockBlockchainClientMockRecorder
}

func NewMockBlockchainClient(ctrl *gomock.Controller) *MockBlockchainClient {
    mock := &MockBlockchainClient{ctrl: ctrl}
    mock.recorder = &MockBlockchainClientMockRecorder{mock}
    return mock
}

func (m *MockBlockchainClient) Transfer(ctx context.Context, from, to, amount string) (*TransferResult, error) {
    m.ctrl.T.Helper()
    ret := m.ctrl.Call(m, "Transfer", ctx, from, to, amount)
    // ...
}

// EXPECT 方法用于设置预期
func (m *MockBlockchainClient) EXPECT() *MockBlockchainClientMockRecorder {
    return m.recorder
}
```

### 4.3 使用生成的 Mock

```go
import (
    "testing"
    "github.com/golang/mock/gomock"
    "stablepay/payment-service/mocks"
)

func TestPaymentService_Pay(t *testing.T) {
    // 1. 创建 gomock 控制器
    ctrl := gomock.NewController(t)
    defer ctrl.Finish()

    // 2. 创建 Mock 对象
    mockBlockchain := mocks.NewMockBlockchainClient(ctrl)

    // 3. 设置预期行为
    mockBlockchain.EXPECT().
        Transfer(gomock.Any(), "addr1", "addr2", "100.00").
        Return(&TransferResult{TxHash: "0x123"}, nil).
        Times(1)  // 预期调用 1 次

    // 4. 创建服务并注入 Mock
    service := NewPaymentService(mockBlockchain)

    // 5. 执行测试
    result, err := service.Pay(context.Background(), "addr1", "addr2", "100.00")

    // 6. 验证
    if err != nil {
        t.Errorf("unexpected error: %v", err)
    }
    if result.TxHash != "0x123" {
        t.Errorf("unexpected tx hash: %s", result.TxHash)
    }
}

// 测试错误情况
func TestPaymentService_Pay_BlockchainError(t *testing.T) {
    ctrl := gomock.NewController(t)
    defer ctrl.Finish()

    mockBlockchain := mocks.NewMockBlockchainClient(ctrl)

    // 设置返回错误
    mockBlockchain.EXPECT().
        Transfer(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
        Return(nil, errors.New("insufficient balance")).
        Times(1)

    service := NewPaymentService(mockBlockchain)
    _, err := service.Pay(context.Background(), "addr1", "addr2", "100.00")

    if err == nil {
        t.Error("expected error")
    }
}
```

### 4.4 gomock 高级用法

```go
// 参数匹配器
mock.EXPECT().
    Method(gomock.Any(),              // 任意值
          gomock.Eq("exact_value"),   // 精确匹配
          gomock.Not("forbidden"),    // 不匹配
          gomock.Len(2))              // 长度匹配

// 多次调用返回不同值
gomock.InOrder(
    mock.EXPECT().GetBalance().Return(100.0, nil),
    mock.EXPECT().GetBalance().Return(90.0, nil),
    mock.EXPECT().GetBalance().Return(80.0, nil),
)

// 条件返回
mock.EXPECT().
    Pay(gomock.Any(), gomock.Any()).
    DoAndReturn(func(ctx context.Context, amount float64) (string, error) {
        if amount > 1000 {
            return "", errors.New("too large")
        }
        return fmt.Sprintf("tx_%f", amount), nil
    }).
    AnyTimes()
```

## 5. API Gateway 中的 Mock 实践

### 5.1 当前代码分析

```go
// api-gateway/internal/infrastructure/clients/mock_clients.go

// 你们现在的 Mock 实现
type MockDIDClient struct{}

func (m *MockDIDClient) CreateDID(_ context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
    return map[string]interface{}{
        "did":            "did:solana:" + uuid.NewString(),
        "public_key":     "mock_public_key",
        "wallet_address": "mock_wallet_address",
    }, 200, 0, nil
}
```

### 5.2 增强 Mock 支持测试

```go
// 增强版 Mock，支持测试场景

type ConfigurableMockDIDClient struct {
    CreateDIDFunc      func(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
    VerifyDIDFunc      func(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
    GetDIDFunc         func(ctx context.Context, did string) (map[string]interface{}, int, int, error)

    // 调用记录
    CreateDIDCalls     []map[string]interface{}
    VerifyDIDCalls     []map[string]interface{}
    GetDIDCalls        []string
}

func NewConfigurableMockDIDClient() *ConfigurableMockDIDClient {
    return &ConfigurableMockDIDClient{
        CreateDIDCalls: make([]map[string]interface{}, 0),
        VerifyDIDCalls: make([]map[string]interface{}, 0),
        GetDIDCalls:    make([]string, 0),
    }
}

func (m *ConfigurableMockDIDClient) CreateDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
    m.CreateDIDCalls = append(m.CreateDIDCalls, req)

    if m.CreateDIDFunc != nil {
        return m.CreateDIDFunc(ctx, req)
    }

    // 默认返回
    return map[string]interface{}{
        "did":            "did:solana:test_" + uuid.NewString(),
        "public_key":     "mock_public_key",
        "wallet_address": "mock_wallet_address_" + req["user_type"].(string),
        "user_type":      req["user_type"],
        "created_at":     time.Now().UTC().Format(time.RFC3339),
    }, 200, 0, nil
}

func (m *ConfigurableMockDIDClient) VerifyDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
    m.VerifyDIDCalls = append(m.VerifyDIDCalls, req)

    if m.VerifyDIDFunc != nil {
        return m.VerifyDIDFunc(ctx, req)
    }

    return map[string]interface{}{"valid": true}, 200, 0, nil
}

func (m *ConfigurableMockDIDClient) GetDID(ctx context.Context, did string) (map[string]interface{}, int, int, error) {
    m.GetDIDCalls = append(m.GetDIDCalls, did)

    if m.GetDIDFunc != nil {
        return m.GetDIDFunc(ctx, did)
    }

    return map[string]interface{}{
        "did":            did,
        "public_key":     "mock_public_key",
        "wallet_address": "mock_wallet_address",
        "status":         "active",
    }, 200, 0, nil
}

func (m *ConfigurableMockDIDClient) VerifySignature(_ context.Context, req map[string]interface{}) (bool, error) {
    return req["signature"] != "", nil
}
```

### 5.3 在测试中切换 Mock 和真实客户端

```go
// api-gateway/internal/app/bootstrap.go

func New(cfg *config.AppConfig, useMock bool) (*Instance, error) {
    var (
        didClient          application.DIDServiceClient
        paymentClient      application.PaymentServiceClient
        verificationClient application.VerificationServiceClient
        queryClient        application.QueryServiceClient
    )

    if useMock || cfg.Environment == "test" {
        // 使用 Mock
        didClient = clients.NewMockDIDClient()
        paymentClient = clients.NewMockPaymentClient()
        verificationClient = clients.NewMockVerificationClient()
        queryClient = clients.NewMockQueryClient()
    } else {
        // 使用真实客户端
        var err error
        didClient, err = clients.NewDIDClient(cfg.Downstream.DIDService)
        if err != nil {
            return nil, fmt.Errorf("create did client: %w", err)
        }

        paymentClient, err = clients.NewPaymentClient(cfg.Downstream.PaymentService)
        if err != nil {
            return nil, fmt.Errorf("create payment client: %w", err)
        }
        // ...
    }

    // ...
}
```

## 6. Mock 测试最佳实践

### 6.1 测试金字塔

```
        /\
       /  \
      / E2E \         端到端测试（少）- 不用 Mock
     /--------\
    / Integration \    集成测试（中）- 部分 Mock
   /----------------\
  /   Unit Tests      \  单元测试（多）- 大量 Mock
 /----------------------\
```

### 6.2 Mock 使用原则

```go
// ✅ 好的做法：只 Mock 直接依赖
func TestOrderService(t *testing.T) {
    // Mock 数据库
    mockDB := NewMockDB()

    // 被测服务
    service := NewOrderService(mockDB)

    // 测试
    order, err := service.CreateOrder(...)
}

// ❌ 坏的做法：Mock 太多层
func TestOrderService(t *testing.T) {
    mockDB := NewMockDB()
    mockCache := NewMockCache()
    mockMQ := NewMockMQ()
    mockRPC := NewMockRPC()
    mockLog := NewMockLogger()
    // ... 太复杂，测试失去意义
}
```

### 6.3 命名规范

```go
// Mock 结构体命名
Mock{InterfaceName}

// 示例：
MockPaymentClient      // 好的
PaymentClientMock      // 不推荐
MockPayment            // 不够清晰

// Mock 方法命名
{MethodName}Func       // 可配置的函数
{MethodName}Calls      // 调用记录
{MethodName}Returns    // 预设返回值
```

### 6.4 Mock 数据工厂

```go
// testutil/factories.go

package testutil

import "stablepay/common"

// CreateMockDID 创建标准 Mock DID
func CreateMockDID() map[string]interface{} {
    return map[string]interface{}{
        "did":            "did:solana:mock_12345",
        "public_key":     "pub_mock_12345",
        "wallet_address": "wallet_mock_12345",
        "status":         "active",
        "created_at":     "2024-01-01T00:00:00Z",
    }
}

// CreateMockPaymentResponse 创建标准支付响应
func CreateMockPaymentResponse() map[string]interface{} {
    return map[string]interface{}{
        "tx_id":        "pay_mock_12345",
        "tx_hash":      "0xmockhash",
        "status":       "confirmed",
        "confirmed_at": "2024-01-01T00:00:00Z",
    }
}

// CreateMockBalance 创建余额数据
func CreateMockBalance(amount string) map[string]interface{} {
    return map[string]interface{}{
        "balance":       amount,
        "currency":      "USDC",
        "monthly_spent": "0.00",
        "monthly_limit": "1000.00",
    }
}
```

## 7. 完整测试示例

### 7.1 测试 Payment Handler

```go
// internal/handler/payment_handler_test.go

package handler

import (
    "context"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "stablepay/payment-service/internal/mocks"
)

func TestPaymentHandler_ProcessPayment(t *testing.T) {
    tests := []struct {
        name           string
        setupMock      func(*mocks.MockBlockchainClient, *mocks.MockDB)
        request        PaymentRequest
        expectedError  bool
        expectedTxHash string
    }{
        {
            name: "successful payment",
            setupMock: func(mockBC *mocks.MockBlockchainClient, mockDB *mocks.MockDB) {
                mockBC.TransferFunc = func(ctx context.Context, from, to, amount string) (*TransferResult, error) {
                    return &TransferResult{TxHash: "0xabc123"}, nil
                }
                mockDB.SaveFunc = func(tx *Transaction) error {
                    return nil
                }
            },
            request: PaymentRequest{
                From:   "addr1",
                To:     "addr2",
                Amount: "100.00",
            },
            expectedError:  false,
            expectedTxHash: "0xabc123",
        },
        {
            name: "insufficient balance",
            setupMock: func(mockBC *mocks.MockBlockchainClient, mockDB *mocks.MockDB) {
                mockBC.TransferFunc = func(ctx context.Context, from, to, amount string) (*TransferResult, error) {
                    return nil, errors.New("insufficient balance")
                }
            },
            request: PaymentRequest{
                From:   "addr1",
                To:     "addr2",
                Amount: "1000000.00",
            },
            expectedError: true,
        },
        {
            name: "database save failure",
            setupMock: func(mockBC *mocks.MockBlockchainClient, mockDB *mocks.MockDB) {
                mockBC.TransferFunc = func(ctx context.Context, from, to, amount string) (*TransferResult, error) {
                    return &TransferResult{TxHash: "0xabc123"}, nil
                }
                mockDB.SaveFunc = func(tx *Transaction) error {
                    return errors.New("db connection lost")
                }
            },
            request: PaymentRequest{
                From:   "addr1",
                To:     "addr2",
                Amount: "100.00",
            },
            expectedError: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 创建 Mock
            mockBC := &mocks.MockBlockchainClient{}
            mockDB := &mocks.MockDB{}
            tt.setupMock(mockBC, mockDB)

            // 创建 Handler
            handler := NewPaymentHandler(mockBC, mockDB)

            // 执行
            result, err := handler.ProcessPayment(context.Background(), tt.request)

            // 验证
            if tt.expectedError {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
                assert.Equal(t, tt.expectedTxHash, result.TxHash)
            }
        })
    }
}
```

### 7.2 测试 API Gateway 路由

```go
// api-gateway/tests/integration_test.go

package tests

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/stretchr/testify/assert"
    "stablepay/api-gateway/internal/app"
    "stablepay/api-gateway/internal/infrastructure/config"
)

func TestCreateDIDEndpoint(t *testing.T) {
    // 1. 创建测试应用（使用 Mock）
    cfg := &config.AppConfig{
        Server: config.ServerConfig{
            Address: ":0",
        },
        Environment: "test",
    }

    instance, err := app.New(cfg, true) // true = use mock
    assert.NoError(t, err)

    // 2. 构造请求
    reqBody := map[string]interface{}{
        "user_type": "agent",
        "metadata":  "test",
    }
    jsonBody, _ := json.Marshal(reqBody)

    req := httptest.NewRequest(
        http.MethodPost,
        "/api/v1/did",
        bytes.NewReader(jsonBody),
    )
    req.Header.Set("Content-Type", "application/json")

    // 3. 执行请求
    w := httptest.NewRecorder()
    instance.Engine.ServeHTTP(w, req)

    // 4. 验证响应
    assert.Equal(t, http.StatusOK, w.Code)

    var resp map[string]interface{}
    err = json.Unmarshal(w.Body.Bytes(), &resp)
    assert.NoError(t, err)

    assert.NotEmpty(t, resp["did"])
    assert.NotEmpty(t, resp["public_key"])
    assert.Equal(t, "agent", resp["user_type"])
}
```

## 8. Mock 转真实客户端的切换策略

### 8.1 配置文件控制

```yaml
# configs/config.yaml

environment: "development"  # development / test / production

# 下游服务配置
downstream:
  did_service: "localhost:8081"
  payment_service: "localhost:8082"

# Mock 配置
use_mock:
  development: true   # 开发环境用 Mock
  test: true          # 测试环境用 Mock
  production: false   # 生产环境用真实服务
```

### 8.2 代码实现

```go
func NewClients(cfg *config.AppConfig) (Clients, error) {
    useMock := cfg.UseMock[cfg.Environment]

    if useMock {
        return NewMockClients(), nil
    }

    return NewRealClients(cfg.Downstream)
}
```

## 9. 常见问题

### Q1: Mock 太多，测试变得脆弱

**解决方案**：
- 减少 Mock 层级，只 Mock 直接依赖
- 使用集成测试补充

### Q2: Mock 行为和真实服务不一致

**解决方案**：
- 定期运行集成测试验证
- 记录真实服务的响应作为 Mock 数据

### Q3: 不知道 Mock 是否被调用

**解决方案**：
- 添加调用计数器
- 使用 gomock 的验证功能

---

**相关文档**:
- [Thrift 生成 Go 接口指南](./thrift-to-go-guide.md)
- [Kitex 框架指南](./kitex-guide.md)
- [单元测试指南](./unit-testing-guide.md)
