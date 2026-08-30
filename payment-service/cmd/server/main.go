package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	kitexserver "github.com/cloudwego/kitex/server"
	"go.uber.org/zap"

	"github.com/stablepay/payment-service/internal/adapter/mq"
	"github.com/stablepay/payment-service/internal/adapter/repository"
	"github.com/stablepay/payment-service/internal/adapter/rpc"
	appservice "github.com/stablepay/payment-service/internal/application/service"
	domainservice "github.com/stablepay/payment-service/internal/domain/service"
	"github.com/stablepay/payment-service/internal/infrastructure/config"
	"github.com/stablepay/payment-service/internal/infrastructure/mysql"
	"github.com/stablepay/payment-service/internal/infrastructure/redis"
	paymentservice "github.com/stablepay/payment-service/kitex_gen/stablepay/payment_service/paymentservice"
	"github.com/stablepay/payment-service/pkg/utils"
)

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	logger, err := initLogger(cfg)
	if err != nil {
		log.Fatalf("failed to init logger: %v", err)
	}
	defer logger.Sync()

	logger.Info("Starting Payment Service",
		zap.String("version", cfg.App.Version),
		zap.String("env", cfg.App.Env),
	)

	db, err := mysql.NewMySQLConnection(cfg)
	if err != nil {
		logger.Fatal("Failed to connect to MySQL", zap.Error(err))
	}
	if cfg.App.Env == "local" {
		if err := mysql.AutoMigrate(db); err != nil {
			logger.Fatal("Failed to auto migrate MySQL tables", zap.Error(err))
		}
	} else {
		logger.Info("Skip MySQL AutoMigrate outside local environment", zap.String("env", cfg.App.Env))
	}

	redisClient, err := redis.NewRedisClient(cfg)
	if err != nil {
		logger.Fatal("Failed to connect to Redis", zap.Error(err))
	}
	defer redisClient.Close()

	mqProducer, err := mq.NewPaymentEventProducer(
		cfg.RocketMQ.NameServers, cfg.RocketMQ.ProducerGroup,
		cfg.RocketMQ.Topics["payment_events"], logger,
	)
	if err != nil {
		logger.Fatal("Failed to create MQ producer", zap.Error(err))
	}
	defer mqProducer.Shutdown()

	paymentRepo := repository.NewPaymentRepository(db)
	idempotencyRepo := repository.NewPaymentIdempotencyRepository(db)
	didClient, err := rpc.NewDIDServiceClient(cfg.RpcClients.DIDService.Address, cfg.RpcClients.DIDService.TimeoutMs, cfg.RpcClients.DIDService.RetryCount)
	if err != nil {
		logger.Fatal("Failed to init did-service Kitex client", zap.Error(err))
	}
	blockchainClient, err := rpc.NewBlockchainAdapterClient(cfg.RpcClients.BlockchainAdapter.Address, cfg.RpcClients.BlockchainAdapter.TimeoutMs, cfg.RpcClients.BlockchainAdapter.RetryCount)
	if err != nil {
		logger.Fatal("Failed to init blockchain-adapter Kitex client", zap.Error(err))
	}

	maxAmountMinor, _ := utils.StringToMinorUnit(cfg.Payment.MaxAmountUsdc)
	xRewardMinor, _ := utils.StringToMinorUnit(cfg.Payment.XRegistrationRewardUsdc)
	paymentValidator := domainservice.NewPaymentValidator(didClient, blockchainClient, maxAmountMinor)
	nonceChecker := domainservice.NewNonceChecker(redisClient)
	paymentConfig := &appservice.PaymentConfig{
		TimeoutMinutes: cfg.Payment.TimeoutMinutes, MaxRetryCount: cfg.Payment.MaxRetryCount,
		MaxAmountMinor: maxAmountMinor, PollIntervalSeconds: cfg.Payment.PollIntervalSeconds,
		MaxPollCount: cfg.Payment.MaxPollCount, TreasuryWalletAddress: cfg.Payment.TreasuryWalletAddress,
		XRegistrationRewardMinor: xRewardMinor, InternalApiKey: cfg.Security.InternalApiKey,
	}
	paymentAppService := appservice.NewPaymentApplicationService(
		paymentRepo, idempotencyRepo, paymentValidator, nonceChecker,
		blockchainClient, mqProducer, paymentConfig, logger,
	)
	if cfg.AgentHarness.Enabled {
		autoApproveMinor, err := utils.StringToMinorUnit(cfg.AgentHarness.AutoApproveMaxUsdc)
		if err != nil {
			logger.Fatal("invalid agent_harness.auto_approve_max_usdc", zap.Error(err))
		}
		maxIntentMinor, err := utils.StringToMinorUnit(cfg.AgentHarness.MaxIntentAmountUsdc)
		if err != nil {
			logger.Fatal("invalid agent_harness.max_intent_amount_usdc", zap.Error(err))
		}
		paymentAppService.SetAgentHarness(appservice.NewAgentPaymentHarness(redisClient, appservice.AgentHarnessConfig{
			Enabled: true, RequireIntentForPayment: cfg.AgentHarness.RequireIntentForPayment,
			AutoApproveMaxMinor: autoApproveMinor, MaxIntentAmountMinor: maxIntentMinor,
			IntentTTL:     time.Duration(cfg.AgentHarness.IntentTTLSeconds) * time.Second,
			PolicyVersion: cfg.AgentHarness.PolicyVersion, AllowedSkillDIDs: cfg.AgentHarness.AllowedSkillDIDs,
		}))
	}

	port, err := strconv.Atoi(cfg.Server.Rpc.Port)
	if err != nil || port <= 0 {
		logger.Fatal("invalid server.rpc.port", zap.String("port", cfg.Server.Rpc.Port), zap.Error(err))
	}
	addr := &net.TCPAddr{IP: net.IPv4zero, Port: port}
	handler := rpc.NewPaymentServiceHandler(paymentAppService)
	svr := paymentservice.NewServer(handler, kitexserver.WithServiceAddr(addr))
	setupGracefulShutdown(svr, logger)
	logger.Info("Payment Service started", zap.Int("rpc_port", port))
	if err := svr.Run(); err != nil {
		logger.Fatal("Payment Service stopped with error", zap.Error(err))
	}
}

func initLogger(cfg *config.Config) (*zap.Logger, error) {
	if cfg.App.Env == "production" {
		return zap.NewProduction()
	}
	return zap.NewDevelopment()
}

func setupGracefulShutdown(svr kitexserver.Server, logger *zap.Logger) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		logger.Info("Shutting down payment RPC server...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = ctx
		if err := svr.Stop(); err != nil {
			logger.Error("failed to stop payment RPC server", zap.Error(err))
		}
	}()
}
