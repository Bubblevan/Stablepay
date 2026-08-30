package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

// 默认 Mint 地址
const (
	defaultMainnetUSDCMint = "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" // Mainnet
	defaultDevnetUSDCMint  = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU" // Devnet
)

const defaultSolanaRPC = "https://api.devnet.solana.com"

// getDefaultUSDCMint returns the appropriate USDC mint address based on network
func getDefaultUSDCMint() string {
	// Check if we're on mainnet by looking at RPC URL
	rpcURL := getenvDefaultBalance("SOLANA_RPC_ENDPOINT", defaultSolanaRPC)
	if strings.Contains(rpcURL, "mainnet") {
		return defaultMainnetUSDCMint
	}
	return defaultDevnetUSDCMint
}

// queryOnchainUSDCBalanceMinor queries all token accounts for the given mint
// and returns the total balance in minor units (6 decimals for USDC)
func queryOnchainUSDCBalanceMinor(ctx context.Context, agentDID string) (int64, error) {
	start := time.Now()
	log.Printf("[BalanceQuery] Starting balance query for DID: %s", agentDID)

	wallet, err := walletFromDID(agentDID)
	if err != nil {
		log.Printf("[BalanceQuery] ERROR: Failed to parse DID: %v", err)
		return 0, err
	}
	log.Printf("[BalanceQuery] Parsed wallet address: %s (took %v)", wallet, time.Since(start))

	mint := getenvDefaultBalance("QUERY_BALANCE_USDC_MINT", getDefaultUSDCMint())
	rpcURL := getenvDefaultBalance("SOLANA_RPC_ENDPOINT", defaultSolanaRPC)
	log.Printf("[BalanceQuery] Configuration: RPC=%s, Mint=%s", rpcURL, mint)

	walletPubKey, err := solana.PublicKeyFromBase58(wallet)
	if err != nil {
		log.Printf("[BalanceQuery] ERROR: Invalid wallet address: %v", err)
		return 0, fmt.Errorf("invalid wallet address in did: %w", err)
	}
	mintPubKey, err := solana.PublicKeyFromBase58(mint)
	if err != nil {
		log.Printf("[BalanceQuery] ERROR: Invalid mint address: %v", err)
		return 0, fmt.Errorf("invalid mint address: %w", err)
	}

	client := rpc.New(rpcURL)

	// Query ALL token accounts owned by this wallet for the specified mint
	// This handles cases where USDC might be in non-ATA accounts
	log.Printf("[BalanceQuery] Calling GetTokenAccountsByOwner...")
	rpcStart := time.Now()
	accounts, err := client.GetTokenAccountsByOwner(
		ctx,
		walletPubKey,
		&rpc.GetTokenAccountsConfig{
			Mint: &mintPubKey,
		},
		&rpc.GetTokenAccountsOpts{
			Commitment: rpc.CommitmentFinalized,
		},
	)
	rpcDuration := time.Since(rpcStart)
	log.Printf("[BalanceQuery] GetTokenAccountsByOwner completed in %v", rpcDuration)

	if err != nil {
		// If no accounts found, return 0 (not an error)
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "could not find") || strings.Contains(msg, "not found") {
			log.Printf("[BalanceQuery] No token accounts found (returning 0)")
			return 0, nil
		}
		log.Printf("[BalanceQuery] ERROR: RPC call failed: %v", err)
		return 0, fmt.Errorf("failed to query token accounts: %w", err)
	}

	if accounts == nil || len(accounts.Value) == 0 {
		log.Printf("[BalanceQuery] Empty accounts result (returning 0)")
		return 0, nil
	}

	log.Printf("[BalanceQuery] Found %d token accounts", len(accounts.Value))

	// Sum up balances from all token accounts
	var totalBalance int64
	for i, account := range accounts.Value {
		if account.Account.Data == nil {
			log.Printf("[BalanceQuery] Account %d: no data, skipping", i)
			continue
		}

		data := account.Account.Data.GetBinary()
		if len(data) < 72 {
			log.Printf("[BalanceQuery] Account %d: data too short (%d bytes), skipping", i, len(data))
			continue
		}

		// Amount is at offset 64 (after mint and owner), 8 bytes, little-endian
		amount := uint64(data[64]) |
			uint64(data[65])<<8 |
			uint64(data[66])<<16 |
			uint64(data[67])<<24 |
			uint64(data[68])<<32 |
			uint64(data[69])<<40 |
			uint64(data[70])<<48 |
			uint64(data[71])<<56

		log.Printf("[BalanceQuery] Account %d: balance = %d", i, amount)
		totalBalance += int64(amount)
	}

	log.Printf("[BalanceQuery] SUCCESS: Total balance = %d, Total time = %v", totalBalance, time.Since(start))
	return totalBalance, nil
}

// queryOnchainUSDCBalanceMinorATA is the legacy method that only queries the ATA
func queryOnchainUSDCBalanceMinorATA(ctx context.Context, agentDID string) (int64, error) {
	start := time.Now()
	log.Printf("[BalanceQuery:ATA] Starting ATA balance query for DID: %s", agentDID)

	wallet, err := walletFromDID(agentDID)
	if err != nil {
		log.Printf("[BalanceQuery:ATA] ERROR: Failed to parse DID: %v", err)
		return 0, err
	}

	mint := getenvDefaultBalance("QUERY_BALANCE_USDC_MINT", getDefaultUSDCMint())
	rpcURL := getenvDefaultBalance("SOLANA_RPC_ENDPOINT", defaultSolanaRPC)
	log.Printf("[BalanceQuery:ATA] Configuration: RPC=%s, Mint=%s", rpcURL, mint)

	walletPubKey, err := solana.PublicKeyFromBase58(wallet)
	if err != nil {
		log.Printf("[BalanceQuery:ATA] ERROR: Invalid wallet address: %v", err)
		return 0, fmt.Errorf("invalid wallet address in did: %w", err)
	}
	mintPubKey, err := solana.PublicKeyFromBase58(mint)
	if err != nil {
		log.Printf("[BalanceQuery:ATA] ERROR: Invalid mint address: %v", err)
		return 0, fmt.Errorf("invalid mint address: %w", err)
	}

	ata, _, err := solana.FindAssociatedTokenAddress(walletPubKey, mintPubKey)
	if err != nil {
		log.Printf("[BalanceQuery:ATA] ERROR: Failed to find ATA: %v", err)
		return 0, fmt.Errorf("failed to find ATA: %w", err)
	}
	log.Printf("[BalanceQuery:ATA] ATA address: %s", ata.String())

	client := rpc.New(rpcURL)
	log.Printf("[BalanceQuery:ATA] Calling GetTokenAccountBalance...")
	rpcStart := time.Now()
	result, err := client.GetTokenAccountBalance(ctx, ata, rpc.CommitmentFinalized)
	rpcDuration := time.Since(rpcStart)
	log.Printf("[BalanceQuery:ATA] GetTokenAccountBalance completed in %v", rpcDuration)

	if err != nil {
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "could not find account") || strings.Contains(msg, "account not found") {
			log.Printf("[BalanceQuery:ATA] ATA not found (returning 0)")
			return 0, nil
		}
		log.Printf("[BalanceQuery:ATA] ERROR: RPC call failed: %v", err)
		return 0, fmt.Errorf("failed to get token account balance: %w", err)
	}
	if result == nil || result.Value == nil {
		log.Printf("[BalanceQuery:ATA] Empty result (returning 0)")
		return 0, nil
	}

	var amount int64
	if _, err := fmt.Sscanf(result.Value.Amount, "%d", &amount); err != nil {
		log.Printf("[BalanceQuery:ATA] ERROR: Failed to parse amount: %v", err)
		return 0, fmt.Errorf("failed to parse token amount: %w", err)
	}

	log.Printf("[BalanceQuery:ATA] SUCCESS: Balance = %d, Total time = %v", amount, time.Since(start))
	return amount, nil
}

func walletFromDID(did string) (string, error) {
	const prefix = "did:solana:"
	if !strings.HasPrefix(did, prefix) || len(did) <= len(prefix) {
		return "", fmt.Errorf("invalid agent_did format")
	}
	return strings.TrimSpace(did[len(prefix):]), nil
}

func getenvDefaultBalance(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
