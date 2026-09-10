package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/stablepay/blockchain-adapter/internal/infrastructure/blockchain"
)

func main() {
	path := flag.String("path", "", "path to a local Solana hot wallet JSON file")
	flag.Parse()
	if *path == "" {
		log.Fatal("wallet path is required")
	}
	wallet, err := blockchain.NewHotWallet(*path)
	if err != nil {
		log.Fatalf("invalid hot wallet: %v", err)
	}
	fmt.Printf("hot wallet valid: address=%s\n", wallet.GetAddress())
}
