// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// merchant-server is the runnable HTTP composition root used by local
// development, black-box contract tests, and the container image.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/hertz/pkg/app/server"

	"github.com/stablepay/merchant-server/config"
	"github.com/stablepay/merchant-server/internal/adapter"
	"github.com/stablepay/merchant-server/internal/adapter/handler"
	appPort "github.com/stablepay/merchant-server/internal/application/port"
	appSvc "github.com/stablepay/merchant-server/internal/application/service"
	domainSvc "github.com/stablepay/merchant-server/internal/domain/service"
	"github.com/stablepay/merchant-server/internal/infrastructure/client"
	"github.com/stablepay/merchant-server/internal/infrastructure/persistence/sqlite"
)

type blackBoxVerifier struct{}

func (blackBoxVerifier) VerifyPurchase(_ context.Context, request appPort.VerifyPurchaseRequest) (*appPort.VerifyPurchaseResult, error) {
	if strings.TrimSpace(request.PaymentSignature) == "" {
		return &appPort.VerifyPurchaseResult{Purchased: false}, nil
	}
	return &appPort.VerifyPurchaseResult{
		Purchased: true,
		TxID:      "blackbox-tx",
		TxHash:    "blackbox-hash",
		Proof:     map[string]any{"verifier": "blackbox-fake-gateway"},
	}, nil
}

func main() {
	cfg, err := config.Load("")
	if err != nil {
		log.Fatal(err)
	}
	if strings.ToLower(strings.TrimSpace(cfg.Database.Driver)) != "sqlite" {
		log.Fatalf("merchant-server command currently supports sqlite, got %q", cfg.Database.Driver)
	}

	repo, err := sqlite.NewProductRepo(cfg.Database.Path, cfg.Database.AutoMigrate)
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Close()
	ctx := context.Background()
	if cfg.Database.SeedEnabled {
		if err := repo.Seed(ctx, cfg.Merchant.SellerAddress); err != nil {
			log.Fatal(err)
		}
	}
	giftPool, err := appSvc.NewGiftCodeService(filepath.Join("data", "gift_codes.json"))
	if err != nil {
		log.Fatal(err)
	}
	if err := repo.SeedGiftCodes(ctx, giftPool.Codes()); err != nil {
		log.Fatal(err)
	}

	domain := domainSvc.NewProductDomainService(repo)
	var paymentVerifier appPort.PaymentVerifier = client.NewStablePayClient(cfg.StablePay.GatewayBaseURL, cfg.StablePay.APIKey)
	if strings.EqualFold(strings.TrimSpace(os.Getenv("MERCHANT_TEST_MODE")), "true") {
		paymentVerifier = blackBoxVerifier{}
	}
	app := appSvc.NewProductAppService(repo, domain, paymentVerifier, nil,
		cfg.Merchant.PublicBaseURL, cfg.Merchant.SellerAddress, cfg.Merchant.ProofSecret,
		cfg.StablePay.FacilitatorURL, cfg.Blockchain.USDCMint, cfg.Blockchain.SolanaNetwork)
	app.SetDeliveryResultRepository(repo)

	srv := server.New(server.WithHostPorts(fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)))
	hh := handler.NewHealthHandler(cfg.Server.Name, func() bool { return true })
	ph := handler.NewProductHandler(app, cfg.Merchant.SellerAddress, cfg.StablePay.FacilitatorURL)
	adapter.NewRouter(srv, hh, ph).Register()
	srv.Spin()
}
