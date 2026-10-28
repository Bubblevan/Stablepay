package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/procurementmvp"
)

func main() {
	token := os.Getenv("FIAT_INTERNAL_TOKEN")
	if len(token) < 24 {
		log.Fatal("FIAT_INTERNAL_TOKEN must match payment-service and contain at least 24 characters")
	}
	storePath := os.Getenv("MVP_STORE_PATH")
	if storePath == "" {
		storePath = ".local-run/procurement-mvp.json"
	}
	store, err := procurementmvp.OpenStore(storePath)
	if err != nil {
		log.Fatal(err)
	}
	paymentURL := os.Getenv("FIAT_PAYMENT_URL")
	if paymentURL == "" {
		paymentURL = "http://127.0.0.1:8092"
	}
	service := &procurementmvp.Service{Store: store, Payment: procurementmvp.PaymentClient{BaseURL: paymentURL, Token: token}}
	if os.Getenv("MVP_USE_LLM") == "1" {
		provider, model := os.Getenv("LLM_PROVIDER"), os.Getenv("LLM_MODEL")
		if provider == "" {
			provider = "openai-compatible"
		}
		if model == "" {
			model = os.Getenv("LLM_MODEL_ID")
		}
		client, err := llm.NewConfiguredClient(provider, os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_API_KEY"), model, nil)
		if err != nil {
			log.Fatal("invalid MVP model configuration")
		}
		service.Planner = procurementmvp.IntentPlanner{Client: client}
	}
	address := os.Getenv("MVP_HTTP_ADDR")
	if address == "" {
		address = "127.0.0.1:8093"
	}
	origins := []string{"http://localhost:5173", "http://127.0.0.1:5173", "http://127.0.0.1:8093"}
	if raw := os.Getenv("MVP_PUBLIC_ORIGIN"); raw != "" {
		origins = []string{strings.TrimRight(raw, "/")}
	}
	api := procurementmvp.Handler(service, procurementmvp.HTTPConfig{AllowedOrigins: origins, SecureCookies: strings.HasPrefix(os.Getenv("MVP_PUBLIC_ORIGIN"), "https://"), CampusReview: os.Getenv("MVP_PROFILE") == "campus"})
	staticDir := os.Getenv("MVP_STATIC_DIR")
	if staticDir == "" {
		staticDir = "../stablepay-frontend/dist"
	}
	static := http.FileServer(http.Dir(staticDir))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			api.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/payathon" || r.URL.Path == "/campus" || r.URL.Path == "/stablepay" {
			http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
			return
		}
		static.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("commerce-runtime procurement MVP listening on %s", address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
