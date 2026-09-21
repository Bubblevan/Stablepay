// Command s11-live-local starts the deterministic S11 live-local runtime.
// It is intentionally test-only: it uses the real Runtime/API/application
// path with local merchant, payment and verification adapters.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/stablepay/commerce-runtime/internal/livelocal"
)

func main() {
	set := flag.NewFlagSet("s11-live-local", flag.ExitOnError)
	addr := set.String("addr", "127.0.0.1:18090", "HTTP listen address")
	memoryMode := set.String("memory", envOr("S11_LIVE_LOCAL_MEMORY", "on"), "memory mode: on or off")
	llmMode := set.String("llm", envOr("S11_LIVE_LOCAL_LLM", "rule"), "provider mode: rule or local")
	set.Parse(os.Args[1:])
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root, err := livelocal.New(ctx, livelocal.Config{MemoryMode: *memoryMode, LLMMode: *llmMode})
	if err != nil {
		log.Fatalf("s11 live-local composition failed: %v", err)
	}
	defer root.Close()
	server := &http.Server{Addr: *addr, Handler: root.Handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("s11 live-local listening on %s variant=%s", *addr, root.Variant.ConfigHash)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("s11 live-local server failed: %v", err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
