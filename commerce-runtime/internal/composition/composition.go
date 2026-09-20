// Package composition is the S10 production composition root. It wires the
// already-proven domain/application components; it does not contain a second
// payment, merchant, LLM, catalog or memory implementation.
package composition

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	kitexclient "github.com/cloudwego/kitex/client"
	"gorm.io/gorm"

	"github.com/stablepay/commerce-runtime/config"
	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/evidence"
	"github.com/stablepay/commerce-runtime/internal/infrastructure/mysql"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/runtime"
	"github.com/stablepay/commerce-runtime/internal/validator"

	kitexpaymentservice "github.com/stablepay/payment-service/kitex_gen/stablepay/payment_service/paymentservice"
)

type Composition struct {
	Config config.Config
	DB     *gorm.DB
	Store  *mysql.Store

	Catalog     repository.DiscoveryRepository
	Evidence    evidence.PersistentStore
	Memory      memory.MemoryStore
	DeepSeek    *llm.LLMDecisionProvider
	Merchant    adapters.MerchantAdapter
	DID         adapters.DIDAdapter
	Payment     *adapters.KitexPaymentAdapter
	Entitlement adapters.EntitlementAdapter
	Validator   *validator.Registry
	Runtime     *application.Service
	Runner      *runtime.Runner
	Credential  *adapters.RealCredentialProvider
}

func NewProduction(ctx context.Context, cfg config.Config) (*Composition, error) {
	if strings.TrimSpace(cfg.MySQLDSN) == "" {
		return nil, errors.New("COMMERCE_RUNTIME_MYSQL_DSN is required")
	}
	if strings.TrimSpace(cfg.LLMBaseURL) == "" || strings.TrimSpace(cfg.LLMModel) == "" {
		return nil, errors.New("LLM_BASE_URL and LLM_MODEL are required")
	}
	network := firstEnvironmentValue("COMMERCE_RUNTIME_SETTLEMENT_NETWORK", "SOLANA_NETWORK")
	assets := settlementAssets()
	if network == "" || len(assets) == 0 {
		return nil, errors.New("settlement network and at least one configured payment asset are required")
	}
	if strings.TrimSpace(cfg.APIToken) == "" && !cfg.AllowInsecure {
		return nil, errors.New("COMMERCE_RUNTIME_API_TOKEN is required unless COMMERCE_RUNTIME_ALLOW_INSECURE=true")
	}

	db, err := mysql.Open(cfg.MySQLDSN)
	if err != nil {
		return nil, fmt.Errorf("open MySQL: %w", err)
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			if sqlDB, closeErr := db.DB(); closeErr == nil {
				_ = sqlDB.Close()
			}
		}
	}()
	if err := mysql.AutoMigrate(ctx, db); err != nil {
		return nil, fmt.Errorf("migrate MySQL: %w", err)
	}
	store := mysql.NewStore(db)

	deepseek, err := llm.NewProviderFromConfig(cfg.LLMProvider, cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, llm.WithProviderTTL(2*time.Minute))
	if err != nil {
		return nil, fmt.Errorf("configure DeepSeek: %w", err)
	}
	credential, err := adapters.NewRealCredentialProvider(adapters.RealCredentialProviderConfig{AgentKeypairPath: cfg.AgentKeypairPath, DIDServiceAddr: cfg.DIDServiceAddr, BlockchainAddr: cfg.BlockchainAdapterAddr, RPCTimeout: 90 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("configure payment credentials: %w", err)
	}
	paymentClient, err := kitexpaymentservice.NewClient("payment-service", kitexclient.WithHostPorts(cfg.PaymentServiceAddr), kitexclient.WithRPCTimeout(90*time.Second))
	if err != nil {
		return nil, fmt.Errorf("configure payment-service client: %w", err)
	}
	kitexPayment := &adapters.KitexPaymentAdapter{Client: paymentClient, Credentials: credential}
	merchant := adapters.NewHTTPMerchantAdapter(&http.Client{Timeout: cfg.MerchantTimeout})
	verification := adapters.NewStablePayVerificationEntitlement(cfg.GatewayBaseURL, cfg.GatewayAPIKey)
	validatorRegistry := validator.NewBuiltinRegistry()
	service := application.NewService(store,
		application.WithRuntimeVersion(cfg.RuntimeVersion),
		application.WithMerchantAdapter(merchant),
		application.WithSettlementPolicy(application.SettlementPolicy{Network: network, Assets: assets}),
		application.WithPaymentAdapters(application.PaymentDependencies{DID: adapters.RealDIDPolicyAdapter{}, Payment: kitexPayment, Status: kitexPayment, Entitlement: verification}),
		application.WithLLMDecisionProvider(deepseek),
		application.WithPersistentEvidenceStore(store),
		application.WithMemoryStore(store),
		application.WithValidatorRegistry(validatorRegistry),
		application.WithRuleRecoveryFallback(false),
	)
	runner := runtime.NewRunner(service, store, runtime.WithMaxSteps(cfg.MaxRunnerSteps), runtime.WithCredentialProvider(credential))
	closeOnError = false
	return &Composition{Config: cfg, DB: db, Store: store, Catalog: store, Evidence: store, Memory: store, DeepSeek: deepseek, Merchant: merchant, DID: adapters.RealDIDPolicyAdapter{}, Payment: kitexPayment, Entitlement: verification, Validator: validatorRegistry, Runtime: service, Runner: runner, Credential: credential}, nil
}

func (c *Composition) Close() error {
	if c == nil || c.DB == nil {
		return nil
	}
	db, err := c.DB.DB()
	if err != nil {
		return err
	}
	return db.Close()
}

func (c *Composition) Ready(ctx context.Context) error {
	if c == nil || c.DB == nil || c.Runtime == nil || c.Runner == nil || c.DeepSeek == nil || c.Payment == nil || c.Merchant == nil {
		return errors.New("production composition is incomplete")
	}
	db, err := c.DB.DB()
	if err != nil {
		return err
	}
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("mysql is not ready: %w", err)
	}
	return nil
}

func settlementAssets() map[string]string {
	assets := map[string]string{}
	if value := firstEnvironmentValue("COMMERCE_RUNTIME_USDC_MINT", "USDC_MINT"); value != "" {
		assets["USDC"] = value
	}
	if value := firstEnvironmentValue("COMMERCE_RUNTIME_USDT_MINT", "USDT_MINT"); value != "" {
		assets["USDT"] = value
	}
	return assets
}

func firstEnvironmentValue(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
