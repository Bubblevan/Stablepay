package main

import (
	"context"
	"log"
	"net"

	"github.com/cloudwego/kitex/server"
	"github.com/stablepay/query-service/internal/adapter/rpc"
	"github.com/stablepay/query-service/internal/application"
	"github.com/stablepay/query-service/internal/infrastructure/config"
	"github.com/stablepay/query-service/internal/infrastructure/external"
	"github.com/stablepay/query-service/internal/infrastructure/persistence"
	querykitex "github.com/stablepay/query-service/kitex_gen/stablepay/query_service/queryservice"
)

func main() {
	cfg := config.Load()
	db, err := persistence.Open(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("open query database: %v", err)
	}
	repository := persistence.NewRepository(db)
	if err := repository.AutoMigrate(context.Background()); err != nil {
		log.Fatalf("migrate query database: %v", err)
	}
	balance, err := external.NewSolanaBalanceProvider(cfg.SolanaRPCEndpoint, cfg.USDCMint)
	if err != nil {
		log.Fatalf("create Solana balance provider: %v", err)
	}
	service := application.NewQueryService(repository, balance, application.Config{MonthlyLimitMinor: cfg.MonthlyLimitMinor})
	addr, err := net.ResolveTCPAddr("tcp", cfg.RPCAddress)
	if err != nil {
		log.Fatalf("resolve query service address: %v", err)
	}
	kitexServer := querykitex.NewServer(rpc.NewHandler(service), server.WithServiceAddr(addr))
	if err := kitexServer.Run(); err != nil {
		log.Fatalf("query service stopped: %v", err)
	}
}
