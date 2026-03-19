# API Gateway Mock 客户端迁移到 RPC 客户端指南

## 概述

本指南说明如何将 api-gateway 中的 Mock 客户端替换为 Kitex RPC 客户端，使其能够真实调用下游服务（did-service, payment-service, verification-service, query-service, blockchain-adapter）。

## 当前架构 vs 目标架构

### 当前架构（Mock 模式）
```
Browser --HTTP--> API Gateway --Mock--> (内存中返回假数据)
```

### 目标架构（RPC 模式）
```
Browser --HTTP--> API Gateway --RPC--> did-service (端口 8081)
                                  --RPC--> payment-service (端口 8082)
                                  --RPC--> blockchain-adapter (端口 8083)
                                  --RPC--> verification-service (端口 8084)
                                  --RPC--> query-service (端口 8085)
```

## 迁移步骤

### Step 1: 确认 Kitex 客户端代码已生成

确保 api-gateway 已经生成了所有下游服务的 Kitex 客户端代码：

```bash
cd D:\MyLab\StablePay\api-gateway

# 检查生成的客户端代码
ls kitex_gen/stablepay/
# 应该看到:
# - blockchain_adapter/
# - common/
# - did_service/
# - payment_service/
# - query_service/
# - verification_service/
```

每个服务目录下应该有 `{service}service/client.go` 文件。

### Step 2: 更新应用层接口

修改 `internal_backup/application/contracts.go`（迁移后改为 `internal/application/contracts.go`）：

```go
package application

import "context"

// 保持接口定义不变，但实现会换成 RPC 客户端
type DIDServiceClient interface {
	CreateDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	VerifyDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	GetDID(ctx context.Context, did string) (map[string]interface{}, int, int, error)
	VerifySignature(ctx context.Context, req map[string]interface{}) (bool, error)
}

type PaymentServiceClient interface {
	Pay(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	GetPayment(ctx context.Context, txID string) (map[string]interface{}, int, int, error)
	GetPaymentHistory(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
}

type VerificationServiceClient interface {
	Verify(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	BatchVerify(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	GetProof(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
}

type QueryServiceClient interface {
	GetBalance(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	GetTransactions(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	GetRevenue(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
}
```

### Step 3: 创建 RPC 客户端实现

在 `internal/infrastructure/clients/` 目录下创建新的 RPC 客户端实现文件。

#### 3.1 DID RPC 客户端 (`did_rpc_client.go`)

```go
package clients

import (
	"context"
	"fmt"

	"github.com/cloudwego/kitex/client"
	did_service "stablepay/api-gateway/kitex_gen/stablepay/did_service"
	"stablepay/api-gateway/kitex_gen/stablepay/did_service/didservice"
)

// RPCDIDClient 使用 Kitex RPC 调用 did-service
type RPCDIDClient struct {
	client didservice.Client
}

// NewRPCDIDClient 创建 RPC DID 客户端
// addr: 服务地址，如 "localhost:8081"
func NewRPCDIDClient(addr string) (*RPCDIDClient, error) {
	c, err := didservice.NewClient(
		"did-service",
		client.WithHostPorts(addr),
		// 可以添加更多选项，如超时、重试等
	)
	if err != nil {
		return nil, fmt.Errorf("create did client: %w", err)
	}
	return &RPCDIDClient{client: c}, nil
}

func (r *RPCDIDClient) CreateDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	// 构造 RPC 请求
	rpcReq := &did_service.CreateDIDRequest{
		// 从 req map 中提取字段
		UserType: getString(req, "user_type"),
		Metadata: getString(req, "metadata"),
	}

	// 调用 RPC
	resp, err := r.client.CreateDID(ctx, rpcReq)
	if err != nil {
		return nil, 500, 30001, err
	}

	// 转换响应为 map
	result := map[string]interface{}{
		"did":            resp.Did,
		"public_key":     resp.PublicKey,
		"wallet_address": resp.WalletAddress,
		"user_type":      resp.UserType,
		"created_at":     resp.CreatedAt,
	}

	return result, 200, 0, nil
}

func (r *RPCDIDClient) VerifyDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	// 类似实现...
	return map[string]interface{}{"valid": true}, 200, 0, nil
}

func (r *RPCDIDClient) GetDID(ctx context.Context, did string) (map[string]interface{}, int, int, error) {
	// 类似实现...
	return map[string]interface{}{"did": did}, 200, 0, nil
}

func (r *RPCDIDClient) VerifySignature(ctx context.Context, req map[string]interface{}) (bool, error) {
	// 类似实现...
	return true, nil
}

// 辅助函数
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
```

#### 3.2 Payment RPC 客户端 (`payment_rpc_client.go`)

```go
package clients

import (
	"context"
	"fmt"
	"strconv"

	"github.com/cloudwego/kitex/client"
	"stablepay/api-gateway/kitex_gen/stablepay/common"
	payment_service "stablepay/api-gateway/kitex_gen/stablepay/payment_service"
	"stablepay/api-gateway/kitex_gen/stablepay/payment_service/paymentservice"
)

// RPCPaymentClient 使用 Kitex RPC 调用 payment-service
type RPCPaymentClient struct {
	client paymentservice.Client
}

// NewRPCPaymentClient 创建 RPC Payment 客户端
func NewRPCPaymentClient(addr string) (*RPCPaymentClient, error) {
	c, err := paymentservice.NewClient(
		"payment-service",
		client.WithHostPorts(addr),
	)
	if err != nil {
		return nil, fmt.Errorf("create payment client: %w", err)
	}
	return &RPCPaymentClient{client: c}, nil
}

func (r *RPCPaymentClient) Pay(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	// 转换金额为最小单位
	amountMinor, err := parseAmountMinor(req)
	if err != nil {
		return nil, 400, 10001, err
	}

	// 转换货币类型
	currency := parseCurrency(getString(req, "currency"))

	// 构造 RPC 请求
	rpcReq := &payment_service.InitiatePaymentRequest{
		AgentDid:    getString(req, "agent_did"),
		SkillDid:    getString(req, "skill_did"),
		AmountMinor: amountMinor,
		Currency:    currency,
	}

	// 可选字段
	if sig := getString(req, "signature"); sig != "" {
		rpcReq.Signature = &sig
	}
	if ts := getString(req, "timestamp"); ts != "" {
		rpcReq.Timestamp = &ts
	}

	// 调用 RPC
	resp, err := r.client.InitiatePayment(ctx, rpcReq)
	if err != nil {
		return nil, 500, 30001, err
	}

	// 转换响应
	result := map[string]interface{}{
		"tx_id":     resp.TxId,
		"tx_hash":   resp.TxHash,
		"status":    statusToString(resp.Status),
		"created_at": resp.CreatedAt,
	}

	if resp.ConfirmedAt != nil {
		result["confirmed_at"] = *resp.ConfirmedAt
	}

	return result, 200, 0, nil
}

func (r *RPCPaymentClient) GetPayment(ctx context.Context, txID string) (map[string]interface{}, int, int, error) {
	rpcReq := &payment_service.GetPaymentStatusRequest{
		TxId: txID,
	}

	resp, err := r.client.GetPaymentStatus(ctx, rpcReq)
	if err != nil {
		return nil, 500, 30001, err
	}

	result := map[string]interface{}{
		"tx_id":  resp.TxId,
		"status": statusToString(resp.Status),
	}

	if resp.TxHash != nil {
		result["tx_hash"] = *resp.TxHash
	}
	if resp.ConfirmedAt != nil {
		result["confirmed_at"] = *resp.ConfirmedAt
	}

	return result, 200, 0, nil
}

func (r *RPCPaymentClient) GetPaymentHistory(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	// 分页参数
	page := &common.PageRequest{
		Limit:  20,
		Offset: 0,
	}

	rpcReq := &payment_service.ListPaymentHistoryRequest{
		AgentDid: getString(req, "agent_did"),
		Page:     page,
	}

	resp, err := r.client.ListPaymentHistory(ctx, rpcReq)
	if err != nil {
		return nil, 500, 30001, err
	}

	// 转换 items
	items := make([]map[string]interface{}, len(resp.Items))
	for i, item := range resp.Items {
		items[i] = map[string]interface{}{
			"tx_id":      item.TxId,
			"skill_did":  item.SkillDid,
			"amount":     minorToString(item.AmountMinor),
			"currency":   currencyToString(item.Currency),
			"status":     statusToString(item.Status),
			"created_at": item.CreatedAt,
		}
	}

	result := map[string]interface{}{
		"items": items,
		"total": resp.Page.Total,
	}

	return result, 200, 0, nil
}

// 辅助函数

func parseAmountMinor(req map[string]interface{}) (int64, error) {
	// 优先使用 amount_minor
	if v, ok := req["amount_minor"]; ok {
		switch val := v.(type) {
		case int64:
			return val, nil
		case float64:
			return int64(val), nil
		case string:
			return strconv.ParseInt(val, 10, 64)
		}
	}

	// 否则转换 amount
	if v, ok := req["amount"]; ok {
		amountStr := fmt.Sprintf("%v", v)
		// 这里应该使用 utils.StringToMinorUnit，简化处理
		amount, _ := strconv.ParseFloat(amountStr, 64)
		return int64(amount * 1000000), nil
	}

	return 0, fmt.Errorf("amount not found")
}

func parseCurrency(s string) common.Currency {
	if s == "USDT" {
		return common.Currency_USDT
	}
	return common.Currency_USDC
}

func statusToString(s common.PaymentStatus) string {
	switch s {
	case common.PaymentStatus_PENDING:
		return "pending"
	case common.PaymentStatus_CONFIRMED:
		return "confirmed"
	case common.PaymentStatus_FAILED:
		return "failed"
	}
	return "unknown"
}

func minorToString(minor int64) string {
	// 简化转换，实际应该用 decimal
	return fmt.Sprintf("%.6f", float64(minor)/1000000)
}

func currencyToString(c common.Currency) string {
	if c == common.Currency_USDT {
		return "USDT"
	}
	return "USDC"
}
```

### Step 4: 修改 bootstrap.go

修改 `internal_backup/app/bootstrap.go`（迁移后改为 `internal/app/bootstrap.go`）：

```go
package app

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/redis/go-redis/v9"
	"stablepay/api-gateway/internal/application"
	"stablepay/api-gateway/internal/infrastructure/auth"
	"stablepay/api-gateway/internal/infrastructure/clients"
	"stablepay/api-gateway/internal/infrastructure/config"
	"stablepay/api-gateway/internal/infrastructure/observability"
	"stablepay/api-gateway/internal/infrastructure/ratelimit"
	"stablepay/api-gateway/internal/interfaces/http"
	"stablepay/api-gateway/internal/interfaces/http/middleware"
)

type Instance struct {
	Engine     *server.Hertz
	ReadyState *middleware.ReadyState
}

// New 创建应用实例
// useMock: 是否使用 Mock 客户端（开发/测试阶段用 true，联调用 false）
func New(cfg *config.AppConfig, useMock bool, logger *observability.Logger) (*Instance, error) {
	h := server.Default(
		server.WithHostPorts(cfg.Server.Address),
		server.WithReadTimeout(time.Duration(cfg.Server.ReadTimeoutMS)*time.Millisecond),
		server.WithWriteTimeout(time.Duration(cfg.Server.WriteTimeoutMS)*time.Millisecond),
	)

	var limiter ratelimit.Limiter
	var nonceStore auth.NonceStore

	if cfg.Redis.Enabled {
		rdb := redis.NewClient(&redis.Options{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		if err := rdb.Ping(context.Background()).Err(); err == nil {
			limiter = ratelimit.NewRedisLimiter(rdb, "stablepay:rl:")
			nonceStore = auth.NewRedisNonceStore(rdb, "stablepay:nonce:")
		} else {
			logger.Warn("redis disabled due to ping failure", map[string]interface{}{"error": err.Error()})
		}
	}
	if limiter == nil {
		limiter = ratelimit.NewMemoryLimiter()
	}
	if nonceStore == nil {
		nonceStore = auth.NewMemoryNonceStore()
	}

	// 创建客户端（根据 useMock 参数选择）
	var (
		didClient          application.DIDServiceClient
		paymentClient      application.PaymentServiceClient
		verificationClient application.VerificationServiceClient
		queryClient        application.QueryServiceClient
	)

	if useMock {
		// 使用 Mock 客户端（开发/测试）
		didClient = clients.NewMockDIDClient()
		paymentClient = clients.NewMockPaymentClient()
		verificationClient = clients.NewMockVerificationClient()
		queryClient = clients.NewMockQueryClient()
	} else {
		// 使用 RPC 客户端（联调/生产）
		var err error

		didClient, err = clients.NewRPCDIDClient(cfg.Downstream.DIDService)
		if err != nil {
			return nil, fmt.Errorf("create did client: %w", err)
		}

		paymentClient, err = clients.NewRPCPaymentClient(cfg.Downstream.PaymentService)
		if err != nil {
			return nil, fmt.Errorf("create payment client: %w", err)
		}

		verificationClient, err = clients.NewRPCVerificationClient(cfg.Downstream.VerificationService)
		if err != nil {
			return nil, fmt.Errorf("create verification client: %w", err)
		}

		queryClient, err = clients.NewRPCQueryClient(cfg.Downstream.QueryService)
		if err != nil {
			return nil, fmt.Errorf("create query client: %w", err)
		}
	}

	appService := application.NewService(didClient, paymentClient, verificationClient, queryClient)
	handler := http.NewHandler(appService)
	readyState := middleware.NewReadyState()

	h.Use(
		middleware.Recovery(),
		middleware.RequestMeta(),
		middleware.ReadinessGuard(readyState),
		middleware.AuthExtract(),
		middleware.AccessLog(logger),
	)
	http.RegisterRoutes(
		h,
		handler,
		cfg.Routes,
		readyState,
		middleware.AuthVerify(cfg.Security, didClient),
		middleware.ReplayProtection(cfg.Security, nonceStore),
		middleware.RateLimit(limiter),
	)

	return &Instance{Engine: h, ReadyState: readyState}, nil
}

func (i *Instance) Run() error {
	if i.Engine == nil {
		return fmt.Errorf("engine not initialized")
	}
	return i.Engine.Run()
}
```

### Step 5: 更新配置文件

在 `configs/config.yaml` 中添加下游服务地址配置：

```yaml
# 下游服务配置
downstream:
  did_service: "localhost:8081"
  payment_service: "localhost:8082"
  blockchain_adapter: "localhost:8083"
  verification_service: "localhost:8084"
  query_service: "localhost:8085"

# 或者使用服务发现（生产环境）
# downstream:
#   did_service: "did-service.stablepay.svc.cluster.local:8081"
#   ...
```

更新 `internal/infrastructure/config/config.go` 添加配置结构：

```go
type AppConfig struct {
	Server    ServerConfig
	Security  SecurityConfig
	Redis     RedisConfig
	Routes    []RouteConfig
	Downstream DownstreamConfig  // 添加这一行
}

type DownstreamConfig struct {
	DIDService          string `mapstructure:"did_service"`
	PaymentService      string `mapstructure:"payment_service"`
	BlockchainAdapter   string `mapstructure:"blockchain_adapter"`
	VerificationService string `mapstructure:"verification_service"`
	QueryService        string `mapstructure:"query_service"`
}
```

### Step 6: 修改入口文件

修改 `cmd/api-gateway/main.go`：

```go
package main

import (
	"log"
	"os"

	"stablepay/api-gateway/internal/app"
	"stablepay/api-gateway/internal/infrastructure/config"
	"stablepay/api-gateway/internal/infrastructure/observability"
)

func main() {
	// 加载配置
	cfg, err := config.Load("configs/config.yaml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// 初始化日志
	logger := observability.NewLogger()

	// 判断使用 Mock 还是 RPC
	// 可以通过环境变量或配置文件控制
	useMock := os.Getenv("USE_MOCK") == "true" || cfg.Environment == "test"

	// 创建应用实例
	instance, err := app.New(cfg, useMock, logger)
	if err != nil {
		log.Fatalf("failed to create app: %v", err)
	}

	logger.Info("api-gateway starting", map[string]interface{}{
		"address":  cfg.Server.Address,
		"use_mock": useMock,
	})

	// 启动服务
	if err := instance.Run(); err != nil {
		log.Fatalf("failed to run: %v", err)
	}
}
```

## 启动方式

### 开发阶段（使用 Mock）

```bash
# 方式1: 环境变量
export USE_MOCK=true
go run cmd/api-gateway/main.go

# 方式2: 配置文件设置 environment: test
```

### 联调阶段（使用 RPC）

```bash
# 先启动所有下游服务
go run ../did-service/cmd/did-service/main.go          # 端口 8081
go run ../payment-service/cmd/payment-service/main.go  # 端口 8082
# ... 其他服务

# 然后启动 API Gateway（不使用 Mock）
export USE_MOCK=false
go run cmd/api-gateway/main.go
```

## 验证迁移成功

1. **检查日志**：启动时应该显示连接下游服务的日志
2. **测试接口**：调用 API Gateway 的接口，观察是否真实调用了下游服务
3. **错误处理**：下游服务不可用时，应该返回适当的错误信息

```bash
# 测试 DID 创建
curl -X POST http://localhost:8080/api/v1/did \
  -H "Content-Type: application/json" \
  -d '{"user_type": "agent"}'

# 测试支付
curl -X POST http://localhost:8080/api/v1/pay \
  -H "Content-Type: application/json" \
  -H "X-Idempotency-Key: test-123" \
  -d '{
    "agent_did": "did:solana:agent123",
    "skill_did": "did:solana:skill456",
    "amount": "5.00",
    "currency": "USDC"
  }'
```

## 常见问题

### 1. 连接超时

```go
// 在创建客户端时添加超时配置
client.WithRPCTimeout(3 * time.Second),
client.WithConnectTimeout(3 * time.Second),
```

### 2. 服务发现

生产环境应该使用服务发现（如 etcd、nacos）：

```go
import "github.com/kitex-contrib/registry-etcd"

// 使用 etcd 服务发现
r, err := etcd.NewEtcdResolver([]string{"127.0.0.1:2379"})
if err != nil {
    log.Fatal(err)
}

client, err := didservice.NewClient(
    "did-service",
    client.WithResolver(r),  // 使用服务发现，不需要指定 HostPorts
)
```

### 3. 负载均衡

```go
import "github.com/cloudwego/kitex/pkg/loadbalance"

client.WithLoadBalancer(loadbalance.NewWeightedBalancer()),
```

## 目录结构变化

```
api-gateway/
├── cmd/api-gateway/main.go           # 修改：添加 useMock 参数
├── internal/                         # 从 internal_backup 迁移
│   ├── app/bootstrap.go             # 修改：支持 Mock/RPC 切换
│   ├── application/
│   │   ├── contracts.go             # 不变
│   │   └── gateway.go               # 不变
│   └── infrastructure/clients/
│       ├── mock_clients.go          # 不变（开发用）
│       ├── did_rpc_client.go        # 新增
│       ├── payment_rpc_client.go    # 新增
│       ├── verification_rpc_client.go  # 新增
│       └── query_rpc_client.go      # 新增
├── kitex_gen/                        # Kitex 生成（已完成）
└── configs/config.yaml              # 修改：添加 downstream 配置
```
