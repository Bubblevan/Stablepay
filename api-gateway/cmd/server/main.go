package main

import (
	"flag"
	"log"

	"github.com/stablepay/api-gateway/internal/app"
	"github.com/stablepay/api-gateway/internal/infrastructure/config"
	"github.com/stablepay/api-gateway/internal/infrastructure/observability"
)

func main() {
	configPath := flag.String("config", "config/config.yaml", "config file path")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config error: %v", err)
	}

	logger, err := observability.NewLogger(cfg.Logging)
	if err != nil {
		log.Fatalf("init logger error: %v", err)
	}
	defer func() {
		_ = logger.Sync()
	}()

	instance, err := app.New(cfg, logger)
	if err != nil {
		logger.Fatal("bootstrap failed", map[string]interface{}{"error": err.Error()})
	}
	logger.Info("starting api-gateway", map[string]interface{}{"address": cfg.Server.Address})
	if err := instance.Run(); err != nil {
		logger.Fatal("api-gateway stopped", map[string]interface{}{"error": err.Error()})
	}
}
