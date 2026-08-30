package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudwego/kitex/server"
	"github.com/stablepay/verification-service/internal/adapter/rpc"
	"github.com/stablepay/verification-service/internal/application"
	"github.com/stablepay/verification-service/internal/infrastructure/config"
	"github.com/stablepay/verification-service/internal/infrastructure/messaging"
	mysqlrepo "github.com/stablepay/verification-service/internal/infrastructure/persistence/mysql"
	verification_service "github.com/stablepay/verification-service/kitex_gen/stablepay/verification_service/verificationservice"
)

// Run is the only application composition root for verification-service.
// All adapters and infrastructure dependencies are created here and injected
// into the application service before the Kitex server starts.
func Run() error {
	cfg := config.Load()

	db, err := mysqlrepo.Open(cfg.MySQLDSN)
	if err != nil {
		return fmt.Errorf("open verification database: %w", err)
	}
	if err := mysqlrepo.AutoMigrate(context.Background(), db); err != nil {
		return fmt.Errorf("migrate verification database: %w", err)
	}
	purchaseRepository := mysqlrepo.NewPurchaseRepository(db)
	service := application.NewService(purchaseRepository)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	paymentConsumer := messaging.NewConsumer(cfg.RocketNameservers, cfg.RocketGroup, cfg.RocketTopic, purchaseRepository)
	if err := paymentConsumer.Start(ctx); err != nil {
		return fmt.Errorf("start payment event consumer: %w", err)
	}
	defer paymentConsumer.Close()

	addr, err := net.ResolveTCPAddr("tcp", cfg.RPCAddress)
	if err != nil {
		return fmt.Errorf("resolve verification service address: %w", err)
	}
	kitexServer := verification_service.NewServer(rpc.NewHandler(service), server.WithServiceAddr(addr))
	serverErr := make(chan error, 1)
	go func() { serverErr <- kitexServer.Run() }()
	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("verification service stopped: %w", err)
		}
	case <-ctx.Done():
		if err := kitexServer.Stop(); err != nil {
			return fmt.Errorf("stop verification service: %w", err)
		}
		if err := <-serverErr; err != nil {
			return fmt.Errorf("verification service stopped: %w", err)
		}
	}
	return nil
}
