// Standalone CNY listener for local development; same domain and HTTP handler
// as payment-service/cmd/server's optional MySQL-backed listener.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/stablepay/payment-service/internal/fiat"
)

func main() {
	file := os.Getenv("FIAT_STORE_PATH")
	if file == "" {
		file = ".local-run/fiat-orders.json"
	}
	store, err := fiat.NewFileStore(file)
	if err != nil {
		log.Fatal(err)
	}
	service, token, err := fiat.FromEnv(store)
	if err != nil {
		log.Fatal(err)
	}
	address := os.Getenv("FIAT_HTTP_ADDR")
	if address == "" {
		address = "127.0.0.1:8092"
	}
	server, err := fiat.Listen(address, service, token)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("payment-service fiat listening on %s (mode=%s)", address, service.Mode())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
