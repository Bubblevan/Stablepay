package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	baseURL := os.Getenv("SHOP_SIM_BASE_URL")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:18090"
	}
	bridge, err := newBridge(baseURL, os.Getenv("SHOP_SIM_API_KEY"), nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agentshopping-mcp: invalid local simulation configuration")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bridge.serve(ctx, os.Stdin, os.Stdout); err != nil {
		// Never print the API key, request headers, or raw upstream errors.
		fmt.Fprintf(os.Stderr, "agentshopping-mcp: stdio bridge stopped (%T)\n", err)
		os.Exit(1)
	}
}
