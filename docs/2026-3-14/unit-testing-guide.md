# Go 单元测试完整指南

## 1. 测试基础

### 1.1 为什么需要测试？

```
没有测试的代码：
  - 修改时提心吊胆
  - 上线后频繁出 bug
  - 重构时不敢下手

有测试的代码：
  - 修改后有信心
  - 问题在开发阶段发现
  - 重构有保障
```

### 1.2 测试金字塔

```
        /\
       /  \
      / E2E \      端到端测试（慢，少）测试整个系统
     /--------\
    /Integration\  集成测试（中）测试多个组件协作
   /--------------\
  /   Unit Tests    \ 单元测试（快，多）测试单个函数
 /---------------------
```

**建议比例**：70% 单元测试 + 20% 集成测试 + 10% 端到端测试

### 1.3 Go 测试命令

```bash
# 运行所有测试
go test ./...

# 运行指定包的测试
go test ./internal/handler

# 显示详细输出
go test -v ./...

# 运行特定测试函数
go test -v -run TestCreateDID ./...

# 生成覆盖率报告
go test -cover ./...
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# 并行运行测试
go test -parallel 4 ./...

# 测试失败立即停止
go test -failfast ./...
```

## 2. 基本测试写法

### 2.1 最简单的测试

```go
// internal/handler/did_handler.go

func Add(a, b int) int {
    return a + b
}

// internal/handler/did_handler_test.go

package handler

import "testing"

func TestAdd(t *testing.T) {
    result := Add(2, 3)
    if result != 5 {
        t.Errorf("Add(2, 3) = %d; want 5", result)
    }
}
```

### 2.2 表驱动测试（推荐）

```go
func TestAdd(t *testing.T) {
    tests := []struct {
        name     string
        a, b     int
        expected int
    }{
        {"positive", 2, 3, 5},
        {"negative", -2, -3, -5},
        {"mixed", -2, 3, 1},
        {"zero", 0, 5, 5},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result := Add(tt.a, tt.b)
            if result != tt.expected {
                t.Errorf("Add(%d, %d) = %d; want %d",
                    tt.a, tt.b, result, tt.expected)
            }
        })
    }
}
```

### 2.3 使用 testify 简化断言

```bash
# 安装 testify
go get -u github.com/stretchr/testify
```

```go
import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestAddWithTestify(t *testing.T) {
    result := Add(2, 3)

    // assert - 失败继续执行
    assert.Equal(t, 5, result, "Add should return correct sum")
    assert.NotNil(t, result)

    // require - 失败立即停止
    require.NoError(t, err, "Should not have error")
    // 这行不会执行如果上面有错误
    assert.Equal(t, expected, actual)
}

// 常用断言
assert.Equal(t, expected, actual)
assert.NotEqual(t, unexpected, actual)
assert.Nil(t, object)
assert.NotNil(t, object)
assert.True(t, condition)
assert.False(t, condition)
assert.Error(t, err)
assert.NoError(t, err)
assert.Contains(t, slice, element)
assert.Len(t, slice, length)
```

## 3. Handler 测试

### 3.1 测试 DID Handler

```go
// internal/handler/did_handler_test.go

package handler

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
    "stablepay/did-service/internal/handler/kitex_gen/stablepay/did"
    "stablepay/did-service/internal/mocks"
)

func TestDIDHandler_CreateDID(t *testing.T) {
    // 表驱动测试
    tests := []struct {
        name        string
        request     *did.CreateDIDRequest
        setupMock   func(*mocks.MockDIDRepository)
        expectedResp *did.CreateDIDResponse
        expectedErr  bool
        errContains  string
    }{
        {
            name: "success_agent",
            request: &did.CreateDIDRequest{
                UserType: "agent",
                Metadata: "test",
            },
            setupMock: func(repo *mocks.MockDIDRepository) {
                repo.On("Save", mock.Anything, mock.AnythingOfType("*domain.DID")).
                    Return(nil)
            },
            expectedErr: false,
        },
        {
            name: "success_developer",
            request: &did.CreateDIDRequest{
                UserType: "developer",
            },
            setupMock: func(repo *mocks.MockDIDRepository) {
                repo.On("Save", mock.Anything, mock.Anything).Return(nil)
            },
            expectedErr: false,
        },
        {
            name: "missing_user_type",
            request: &did.CreateDIDRequest{
                UserType: "",
            },
            setupMock:   func(repo *mocks.MockDIDRepository) {},
            expectedErr: true,
            errContains: "user_type is required",
        },
        {
            name: "invalid_user_type",
            request: &did.CreateDIDRequest{
                UserType: "invalid",
            },
            setupMock:   func(repo *mocks.MockDIDRepository) {},
            expectedErr: true,
            errContains: "invalid user_type",
        },
        {
            name: "database_error",
            request: &did.CreateDIDRequest{
                UserType: "agent",
            },
            setupMock: func(repo *mocks.MockDIDRepository) {
                repo.On("Save", mock.Anything, mock.Anything).
                    Return(errors.New("connection refused"))
            },
            expectedErr: true,
            errContains: "save did failed",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 1. 创建 Mock
            mockRepo := new(mocks.MockDIDRepository)
            tt.setupMock(mockRepo)

            // 2. 创建 Handler
            handler := NewDIDHandler(mockRepo)

            // 3. 执行测试
            resp, err := handler.CreateDID(context.Background(), tt.request)

            // 4. 验证结果
            if tt.expectedErr {
                assert.Error(t, err)
                if tt.errContains != "" {
                    assert.Contains(t, err.Error(), tt.errContains)
                }
            } else {
                assert.NoError(t, err)
                assert.NotNil(t, resp)
                assert.NotEmpty(t, resp.Did)
                assert.NotEmpty(t, resp.PublicKey)
                assert.NotEmpty(t, resp.WalletAddress)
                assert.Equal(t, "active", resp.Status)
                assert.Equal(t, tt.request.UserType, getUserTypeFromDID(resp.Did))
            }

            // 5. 验证 Mock 调用
            mockRepo.AssertExpectations(t)
        })
    }
}

func TestDIDHandler_GetDID(t *testing.T) {
    tests := []struct {
        name        string
        didID       string
        setupMock   func(*mocks.MockDIDRepository)
        expectedErr bool
        errType     string
    }{
        {
            name:  "success",
            didID: "did:solana:test123",
            setupMock: func(repo *mocks.MockDIDRepository) {
                repo.On("FindByID", mock.Anything, "did:solana:test123").
                    Return(&domain.DID{
                        ID:            "did:solana:test123",
                        PublicKey:     "pub123",
                        WalletAddress: "wallet123",
                        Status:        "active",
                    }, nil)
            },
            expectedErr: false,
        },
        {
            name:        "empty_did",
            didID:       "",
            setupMock:   func(repo *mocks.MockDIDRepository) {},
            expectedErr: true,
            errType:     "validation",
        },
        {
            name:  "not_found",
            didID: "did:solana:nonexistent",
            setupMock: func(repo *mocks.MockDIDRepository) {
                repo.On("FindByID", mock.Anything, "did:solana:nonexistent").
                    Return(nil, repository.ErrNotFound)
            },
            expectedErr: true,
            errType:     "not_found",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mockRepo := new(mocks.MockDIDRepository)
            tt.setupMock(mockRepo)

            handler := NewDIDHandler(mockRepo)

            req := &did.GetDIDRequest{DidID: tt.didID}
            resp, err := handler.GetDID(context.Background(), req)

            if tt.expectedErr {
                assert.Error(t, err)
                if tt.errType == "not_found" {
                    assert.IsType(t, &did.DIDNotFoundException{}, err)
                }
            } else {
                assert.NoError(t, err)
                assert.Equal(t, tt.didID, resp.Did)
            }

            mockRepo.AssertExpectations(t)
        })
    }
}
```

### 3.2 Mock Repository 实现

```go
// internal/mocks/mock_repository.go

package mocks

import (
    "context"
    "stablepay/did-service/internal/domain"
    "github.com/stretchr/testify/mock"
)

type MockDIDRepository struct {
    mock.Mock
}

func (m *MockDIDRepository) Save(ctx context.Context, did *domain.DID) error {
    args := m.Called(ctx, did)
    return args.Error(0)
}

func (m *MockDIDRepository) FindByID(ctx context.Context, id string) (*domain.DID, error) {
    args := m.Called(ctx, id)
    if args.Get(0) == nil {
        return nil, args.Error(1)
    }
    return args.Get(0).(*domain.DID), args.Error(1)
}

func (m *MockDIDRepository) Update(ctx context.Context, did *domain.DID) error {
    args := m.Called(ctx, did)
    return args.Error(0)
}

func (m *MockDIDRepository) Delete(ctx context.Context, id string) error {
    args := m.Called(ctx, id)
    return args.Error(0)
}
```

## 4. HTTP 接口测试

### 4.1 使用 httptest

```go
// api-gateway/internal/interfaces/http/handler_test.go

package http

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/cloudwego/hertz/pkg/app/server"
    "github.com/stretchr/testify/assert"
    "stablepay/api-gateway/internal/application"
    "stablepay/api-gateway/internal/infrastructure/clients"
)

func TestHandler_CreateDID(t *testing.T) {
    // 1. 创建 Mock 客户端
    mockDIDClient := clients.NewMockDIDClient()
    mockPaymentClient := clients.NewMockPaymentClient()
    mockVerificationClient := clients.NewMockVerificationClient()
    mockQueryClient := clients.NewMockQueryClient()

    // 2. 创建应用服务
    appService := application.NewService(
        mockDIDClient,
        mockPaymentClient,
        mockVerificationClient,
        mockQueryClient,
    )

    // 3. 创建 Handler
    handler := NewHandler(appService)

    // 4. 创建 Hertz 服务器
    h := server.Default()
    h.POST("/api/v1/did", handler.HandleRequest)

    // 5. 构造请求
    reqBody := map[string]interface{}{
        "user_type": "agent",
        "metadata":  "test metadata",
    }
    jsonBody, _ := json.Marshal(reqBody)

    req := httptest.NewRequest(
        http.MethodPost,
        "/api/v1/did",
        bytes.NewReader(jsonBody),
    )
    req.Header.Set("Content-Type", "application/json")

    // 6. 执行请求
    w := httptest.NewRecorder()
    h.ServeHTTP(w, req)

    // 7. 验证响应
    assert.Equal(t, http.StatusOK, w.Code)

    var resp map[string]interface{}
    err := json.Unmarshal(w.Body.Bytes(), &resp)
    assert.NoError(t, err)

    // 验证响应字段
    assert.NotEmpty(t, resp["did"])
    assert.NotEmpty(t, resp["public_key"])
    assert.NotEmpty(t, resp["wallet_address"])
    assert.Equal(t, "agent", resp["user_type"])
    assert.Equal(t, float64(0), resp["code"]) // 成功 code 为 0
}

func TestHandler_CreateDID_InvalidRequest(t *testing.T) {
    // 测试无效请求
    mockClient := clients.NewMockDIDClient()
    appService := application.NewService(mockClient, nil, nil, nil)
    handler := NewHandler(appService)

    h := server.Default()
    h.POST("/api/v1/did", handler.HandleRequest)

    // 发送缺少 user_type 的请求
    reqBody := map[string]interface{}{
        "metadata": "test",
    }
    jsonBody, _ := json.Marshal(reqBody)

    req := httptest.NewRequest(
        http.MethodPost,
        "/api/v1/did",
        bytes.NewReader(jsonBody),
    )
    req.Header.Set("Content-Type", "application/json")

    w := httptest.NewRecorder()
    h.ServeHTTP(w, req)

    // 验证返回 400 错误
    assert.Equal(t, http.StatusBadRequest, w.Code)
}
```

### 4.2 集成路由测试

```go
func TestRoutes(t *testing.T) {
    // 构建完整的路由
    cfg := loadTestConfig()
    app, err := app.New(cfg, true) // true = use mock
    assert.NoError(t, err)

    tests := []struct {
        name       string
        method     string
        path       string
        body       interface{}
        wantStatus int
    }{
        {"create_did", "POST", "/api/v1/did", map[string]string{"user_type": "agent"}, 200},
        {"get_did", "GET", "/api/v1/did/did:solana:test", nil, 200},
        {"pay", "POST", "/api/v1/pay", map[string]interface{}{
            "agent_did": "did:solana:agent1",
            "skill_did": "did:solana:skill1",
            "amount": 100,
            "currency": "USDC",
        }, 200},
        {"verify", "GET", "/api/v1/verify?agent=did:solana:agent1&skill=did:solana:skill1", nil, 200},
        {"get_balance", "GET", "/api/v1/balance?agent_did=did:solana:agent1", nil, 200},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            var body []byte
            if tt.body != nil {
                body, _ = json.Marshal(tt.body)
            }

            req := httptest.NewRequest(tt.method, tt.path, bytes.NewReader(body))
            if body != nil {
                req.Header.Set("Content-Type", "application/json")
            }

            w := httptest.NewRecorder()
            app.Engine.ServeHTTP(w, req)

            assert.Equal(t, tt.wantStatus, w.Code, "Response body: %s", w.Body.String())
        })
    }
}
```

## 5. 数据库测试

### 5.1 使用内存数据库

```go
// internal/repository/did_repository_test.go

package repository

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/suite"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"
    "stablepay/did-service/internal/domain"
)

type DIDRepositoryTestSuite struct {
    suite.Suite
    db   *gorm.DB
    repo DIDRepository
}

func (s *DIDRepositoryTestSuite) SetupSuite() {
    // 使用 SQLite 内存数据库
    db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
    if err != nil {
        s.T().Fatal(err)
    }

    // 自动迁移表结构
    db.AutoMigrate(&domain.DID{})

    s.db = db
    s.repo = NewDIDRepository(db)
}

func (s *DIDRepositoryTestSuite) TearDownTest() {
    // 每个测试后清理数据
    s.db.Exec("DELETE FROM dids")
}

func (s *DIDRepositoryTestSuite) TestSave() {
    did := &domain.DID{
        ID:            "did:solana:test123",
        PublicKey:     "pub123",
        WalletAddress: "wallet123",
        UserType:      "agent",
        Status:        "active",
    }

    err := s.repo.Save(context.Background(), did)
    assert.NoError(s.T(), err)

    // 验证保存成功
    found, err := s.repo.FindByID(context.Background(), "did:solana:test123")
    assert.NoError(s.T(), err)
    assert.Equal(s.T(), "did:solana:test123", found.ID)
}

func (s *DIDRepositoryTestSuite) TestFindByID_NotFound() {
    _, err := s.repo.FindByID(context.Background(), "did:solana:nonexistent")
    assert.Error(s.T(), err)
    assert.Equal(s.T(), ErrNotFound, err)
}

func (s *DIDRepositoryTestSuite) TestUpdate() {
    // 先创建
    did := &domain.DID{
        ID:            "did:solana:test123",
        PublicKey:     "pub123",
        WalletAddress: "wallet123",
        UserType:      "agent",
        Status:        "active",
    }
    s.repo.Save(context.Background(), did)

    // 更新
    did.Status = "disabled"
    err := s.repo.Update(context.Background(), did)
    assert.NoError(s.T(), err)

    // 验证更新
    found, _ := s.repo.FindByID(context.Background(), "did:solana:test123")
    assert.Equal(s.T(), "disabled", found.Status)
}

// 运行测试套件
func TestDIDRepositorySuite(t *testing.T) {
    suite.Run(t, new(DIDRepositoryTestSuite))
}
```

### 5.2 使用 Testcontainers（更真实的测试）

```go
// 使用 Docker 运行真实数据库

import (
    "context"
    "testing"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/mysql"
)

func TestWithMySQL(t *testing.T) {
    ctx := context.Background()

    // 启动 MySQL 容器
    mysqlContainer, err := mysql.Run(ctx,
        "mysql:8.0",
        mysql.WithDatabase("test"),
        mysql.WithUsername("test"),
        mysql.WithPassword("test"),
    )
    if err != nil {
        t.Fatal(err)
    }
    defer mysqlContainer.Terminate(ctx)

    // 获取连接信息
    host, _ := mysqlContainer.Host(ctx)
    port, _ := mysqlContainer.MappedPort(ctx, "3306")

    // 连接数据库并测试
    dsn := fmt.Sprintf("test:test@tcp(%s:%s)/test?parseTime=true", host, port.Port())
    db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
    // ... 执行测试
}
```

## 6. 并发测试

### 6.1 测试并发安全

```go
func TestDIDHandler_CreateDID_Concurrent(t *testing.T) {
    mockRepo := new(mocks.MockDIDRepository)

    // 允许多次调用
    mockRepo.On("Save", mock.Anything, mock.Anything).Return(nil)

    handler := NewDIDHandler(mockRepo)

    // 并发创建 100 个 DID
    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(index int) {
            defer wg.Done()

            req := &did.CreateDIDRequest{
                UserType: "agent",
            }
            resp, err := handler.CreateDID(context.Background(), req)

            assert.NoError(t, err)
            assert.NotNil(t, resp)
            assert.NotEmpty(t, resp.Did)
        }(i)
    }

    wg.Wait()

    // 验证 Save 被调用了 100 次
    mockRepo.AssertNumberOfCalls(t, "Save", 100)
}
```

### 6.2 竞争检测

```bash
# 运行竞争检测
go test -race ./...

# 竞争检测会报告数据竞争问题
```

```go
// 有竞争问题的代码
func TestRace(t *testing.T) {
    var counter int
    var wg sync.WaitGroup

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            counter++ // 数据竞争！
        }()
    }

    wg.Wait()
}

// 修复后
func TestNoRace(t *testing.T) {
    var counter int64
    var wg sync.WaitGroup

    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            atomic.AddInt64(&counter, 1)
        }()
    }

    wg.Wait()
}
```

## 7. 基准测试

### 7.1 编写 Benchmark

```go
func BenchmarkAdd(b *testing.B) {
    for i := 0; i < b.N; i++ {
        Add(2, 3)
    }
}

func BenchmarkDIDHandler_CreateDID(b *testing.B) {
    mockRepo := new(mocks.MockDIDRepository)
    mockRepo.On("Save", mock.Anything, mock.Anything).Return(nil)

    handler := NewDIDHandler(mockRepo)
    req := &did.CreateDIDRequest{UserType: "agent"}

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        handler.CreateDID(context.Background(), req)
    }
}
```

### 7.2 运行基准测试

```bash
# 运行基准测试
go test -bench=. ./...

# 运行特定基准测试
go test -bench=BenchmarkAdd ./...

# 运行并分配内存
go test -bench=. -benchmem ./...

# 运行 10 秒
go test -bench=. -benchtime=10s ./...

# 对比优化前后
go test -bench=. -count=5 ./... > old.txt
# 优化代码
go test -bench=. -count=5 ./... > new.txt
# 使用 benchstat 对比
benchstat old.txt new.txt
```

## 8. 测试工具和实践

### 8.1 测试助手函数

```go
// testutil/helpers.go

package testutil

import (
    "context"
    "testing"
    "time"
)

// NewTestContext 创建带超时的测试上下文
func NewTestContext(t *testing.T) (context.Context, context.CancelFunc) {
    return context.WithTimeout(context.Background(), 5*time.Second)
}

// Must 如果 err != nil 则终止测试
func Must(t *testing.T, err error) {
    t.Helper()
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}

// MustNotNil 如果 obj == nil 则终止测试
func MustNotNil(t *testing.T, obj interface{}) {
    t.Helper()
    if obj == nil {
        t.Fatal("expected non-nil object")
    }
}

// PanicRecover 恢复 panic 并记录
func PanicRecover(t *testing.T) {
    t.Helper()
    if r := recover(); r != nil {
        t.Errorf("panic recovered: %v", r)
    }
}
```

### 8.2 测试数据工厂

```go
// testutil/factories.go

package testutil

import (
    "stablepay/did-service/internal/domain"
    "stablepay/did-service/internal/handler/kitex_gen/stablepay/did"
)

// DIDFactory 创建测试用的 DID
func DIDFactory(opts ...DIDOption) *domain.DID {
    d := &domain.DID{
        ID:            "did:solana:test_" + RandomString(8),
        PublicKey:     "pub_" + RandomString(16),
        WalletAddress: "wallet_" + RandomString(16),
        UserType:      "agent",
        Status:        "active",
    }

    for _, opt := range opts {
        opt(d)
    }

    return d
}

type DIDOption func(*domain.DID)

func WithUserType(userType string) DIDOption {
    return func(d *domain.DID) {
        d.UserType = userType
    }
}

func WithStatus(status string) DIDOption {
    return func(d *domain.DID) {
        d.Status = status
    }
}

// CreateDIDRequestFactory 创建测试请求
func CreateDIDRequestFactory(opts ...RequestOption) *did.CreateDIDRequest {
    r := &did.CreateDIDRequest{
        UserType: "agent",
        Metadata: "test",
    }

    for _, opt := range opts {
        opt(r)
    }

    return r
}

type RequestOption func(*did.CreateDIDRequest)

func WithUserTypeRequest(userType string) RequestOption {
    return func(r *did.CreateDIDRequest) {
        r.UserType = userType
    }
}

// 使用示例
func TestSomething(t *testing.T) {
    did := testutil.DIDFactory(
        testutil.WithUserType("developer"),
        testutil.WithStatus("disabled"),
    )

    req := testutil.CreateDIDRequestFactory(
        testutil.WithUserTypeRequest("agent"),
    )
}
```

## 9. CI/CD 中的测试

### 9.1 GitHub Actions 配置

```yaml
# .github/workflows/test.yml

name: Tests

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest

    steps:
    - uses: actions/checkout@v3

    - name: Set up Go
      uses: actions/setup-go@v4
      with:
        go-version: '1.21'

    - name: Cache Go modules
      uses: actions/cache@v3
      with:
        path: ~/go/pkg/mod
        key: ${{ runner.os }}-go-${{ hashFiles('**/go.sum') }}

    - name: Download dependencies
      run: go mod download

    - name: Run tests
      run: go test -v -race -coverprofile=coverage.out ./...

    - name: Upload coverage
      uses: codecov/codecov-action@v3
      with:
        file: ./coverage.out

    - name: Run benchmarks
      run: go test -bench=. -benchtime=1s ./...
```

## 10. 测试最佳实践总结

### DO（推荐做）

```go
// ✅ 使用表驱动测试
func TestXxx(t *testing.T) {
    tests := []struct{...}
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {...})
    }
}

// ✅ 清晰的测试名称
func TestDIDHandler_CreateDID_Success
func TestDIDHandler_CreateDID_InvalidInput
func TestDIDHandler_CreateDID_DatabaseError

// ✅ 独立的测试
每个测试不依赖其他测试的执行顺序

// ✅ 使用 testify
assert.Equal(t, expected, actual)

// ✅ 测试边界情况
空值、最大值、最小值、非法值

// ✅ 检查错误消息
assert.Contains(t, err.Error(), "expected message")
```

### DON'T（避免做）

```go
// ❌ 没有断言的测试
func TestXxx(t *testing.T) {
    DoSomething() // 没有验证结果
}

// ❌ 测试之间相互依赖
func TestA(t *testing.T) {
    createData()
}

func TestB(t *testing.T) {
    // 假设 TestA 已经运行
    useData()
}

// ❌ 测试过多逻辑
一个测试函数测试一个场景

// ❌ 忽略错误
result, _ := DoSomething() // ❌
result, err := DoSomething() // ✅
require.NoError(t, err)
```

---

**相关文档**:
- [Thrift 生成 Go 接口指南](./thrift-to-go-guide.md)
- [Kitex 框架指南](./kitex-guide.md)
- [Mock 测试指南](./mock-testing-guide.md)
- [Handler 编写指南](./handler-guide.md)
