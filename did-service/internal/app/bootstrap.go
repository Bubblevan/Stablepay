package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudwego/kitex/server"
	"github.com/stablepay/did-service/internal/adapter"
	application "github.com/stablepay/did-service/internal/application"
	"github.com/stablepay/did-service/internal/infrastructure/config"
	"github.com/stablepay/did-service/internal/infrastructure/repository"
	didservice "github.com/stablepay/did-service/kitex_gen/stablepay/did_service/didservice"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// Run is the DID service composition root. Production always uses the MySQL
// repository; tests inject their own repository implementation directly into
// the application service.
func Run() error {
	cfg, err := config.Load(config.ResolvePath())
	if err != nil {
		return fmt.Errorf("load config %s: %w", config.ResolvePath(), err)
	}

	db, err := gorm.Open(mysql.Open(cfg.GetMySQLDSN()), &gorm.Config{})
	if err != nil {
		return fmt.Errorf("open did database: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get did database handle: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetime) * time.Second)
	if err := db.AutoMigrate(&repository.DidIdentityModel{}); err != nil {
		return fmt.Errorf("migrate did database: %w", err)
	}

	didRepository := repository.NewDBDIDRepository(db)
	appService, err := application.NewDIDAppService(didRepository, cfg.Encryption.Key)
	if err != nil {
		return fmt.Errorf("create did application service: %w", err)
	}
	handler := adapter.NewDIDHandler(appService)
	addr, err := net.ResolveTCPAddr("tcp", cfg.GetAddr())
	if err != nil {
		return fmt.Errorf("resolve did service address: %w", err)
	}
	kitexServer := didservice.NewServer(handler, server.WithServiceAddr(addr))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErr := make(chan error, 1)
	go func() { serverErr <- kitexServer.Run() }()
	select {
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("did service stopped: %w", err)
		}
	case <-ctx.Done():
		if err := kitexServer.Stop(); err != nil {
			return fmt.Errorf("stop did service: %w", err)
		}
		if err := <-serverErr; err != nil {
			return fmt.Errorf("did service stopped: %w", err)
		}
	}
	return nil
}
