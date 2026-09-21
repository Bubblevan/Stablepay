package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/stablepay/commerce-runtime/config"
	"github.com/stablepay/commerce-runtime/internal/api"
	"github.com/stablepay/commerce-runtime/internal/composition"
)

func main() {
	cfg := config.FromEnv()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root, err := composition.NewProduction(ctx, cfg)
	if err != nil {
		log.Fatalf("commerce-runtime composition failed: %v", err)
	}
	defer root.Close()
	if err := root.Runner.ResumePersisted(ctx); err != nil {
		log.Printf("commerce-runtime persisted episode resume scan unavailable: %v", err)
	}
	if err := root.Runner.StartSupervisor(ctx); err != nil {
		log.Fatalf("commerce-runtime supervisor failed to start: %v", err)
	}
	handler := api.NewServer(root.Runtime, root.Store, root.Runner, api.AuthConfig{Token: cfg.APIToken, AllowInsecure: cfg.AllowInsecure}, root.Ready, root.Variant)
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("commerce-runtime S10 listening on %s (version=%s)", cfg.HTTPAddr, cfg.RuntimeVersion)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("commerce-runtime HTTP server failed: %v", err)
	}
}
