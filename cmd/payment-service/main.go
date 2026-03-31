package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"go.uber.org/zap"

	"github.com/stablepay/payment-service/internal/adapter/http/handler"
	"github.com/stablepay/payment-service/internal/adapter/http/router"
	"github.com/stablepay/payment-service/internal/adapter/mq"
	"github.com/stablepay/payment-service/internal/adapter/repository"
	"github.com/stablepay/payment-service/internal/adapter/rpc"
	appservice "github.com/stablepay/payment-service/internal/application/service"
	"github.com/stablepay/payment-service/internal/domain/service"
	"github.com/stablepay/payment-service/internal/infrastructure/config"
	"github.com/stablepay/payment-service/internal/infrastructure/mysql"
	"github.com/stablepay/payment-service/internal/infrastructure/redis"
	"github.com/stablepay/payment-service/pkg/utils"
)

func main() {
	configPath := ""
	if cp := os.Getenv("CONFIG_PATH"); cp != "" {
		configPath = cp
	}
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		hlog.Fatalf("Failed to load config: %v", err)
	}

	logger, err := initLogger(cfg)
	if err != nil {
		hlog.Fatalf("Failed to init logger: %v", err)
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
	if err := mysql.AutoMigrate(db); err != nil {
		logger.Fatal("Failed to auto migrate MySQL tables", zap.Error(err))
	}

	redisClient, err := redis.NewRedisClient(cfg)
	if err != nil {
		logger.Fatal("Failed to connect to Redis", zap.Error(err))
	}
	defer redisClient.Close()

	mqProducer, err := mq.NewPaymentEventProducer(
		cfg.RocketMQ.NameServers,
		cfg.RocketMQ.ProducerGroup,
		cfg.RocketMQ.Topics["payment_events"],
		logger,
	)
	if err != nil {
		logger.Fatal("Failed to create MQ producer", zap.Error(err))
	}
	defer mqProducer.Shutdown()

	paymentRepo := repository.NewPaymentRepository(db)
	idempotencyRepo := repository.NewPaymentIdempotencyRepository(db)

	didClient := rpc.NewDIDServiceClient(
		cfg.RpcClients.DIDService.Address,
		cfg.RpcClients.DIDService.TimeoutMs,
		cfg.RpcClients.DIDService.RetryCount,
	)
	blockchainClient := rpc.NewBlockchainAdapterClient(
		cfg.RpcClients.BlockchainAdapter.Address,
		cfg.RpcClients.BlockchainAdapter.TimeoutMs,
		cfg.RpcClients.BlockchainAdapter.RetryCount,
	)

	maxAmountMinor, _ := utils.StringToMinorUnit(cfg.Payment.MaxAmountUsdc)
	paymentValidator := service.NewPaymentValidator(didClient, blockchainClient, maxAmountMinor)
	nonceChecker := service.NewNonceChecker(redisClient)

	paymentConfig := &appservice.PaymentConfig{
		TimeoutMinutes:      cfg.Payment.TimeoutMinutes,
		MaxRetryCount:       cfg.Payment.MaxRetryCount,
		MaxAmountMinor:      maxAmountMinor,
		PollIntervalSeconds: cfg.Payment.PollIntervalSeconds,
		MaxPollCount:        cfg.Payment.MaxPollCount,
	}
	paymentAppService := appservice.NewPaymentApplicationService(
		paymentRepo,
		idempotencyRepo,
		paymentValidator,
		nonceChecker,
		blockchainClient,
		mqProducer,
		paymentConfig,
		logger,
	)

	paymentHandler := handler.NewPaymentHandler(paymentAppService, logger)
	h := server.Default(server.WithHostPorts(":" + cfg.Server.Http.Port))
	router.RegisterRoutes(h, paymentHandler)

	setupGracefulShutdown(h, logger)

	logger.Info("Payment Service started", zap.String("port", cfg.Server.Http.Port))
	h.Spin()
}

func initLogger(cfg *config.Config) (*zap.Logger, error) {
	if cfg.App.Env == "production" {
		return zap.NewProduction()
	}
	return zap.NewDevelopment()
}

func setupGracefulShutdown(h *server.Hertz, logger *zap.Logger) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		logger.Info("Shutting down server...")

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		_ = ctx
		logger.Info("Server stopped")
	}()
}
