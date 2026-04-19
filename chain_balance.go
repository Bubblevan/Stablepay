package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

const defaultDevnetUSDCMint = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"
const defaultSolanaRPC = "https://api.devnet.solana.com"

// queryOnchainUSDCBalanceMinor queries all token accounts for the given mint
// and returns the total balance in minor units (lamports for USDC = 6 decimals)
func queryOnchainUSDCBalanceMinor(ctx context.Context, agentDID string) (int64, error) {
	wallet, err := walletFromDID(agentDID)
	if err != nil {
		return 0, err
	}

	mint := getenvDefaultBalance("QUERY_BALANCE_USDC_MINT", defaultDevnetUSDCMint)
	rpcURL := getenvDefaultBalance("QUERY_BALANCE_SOLANA_RPC", defaultSolanaRPC)

	walletPubKey, err := solana.PublicKeyFromBase58(wallet)
	if err != nil {
		return 0, fmt.Errorf("invalid wallet address in did: %w", err)
	}
	mintPubKey, err := solana.PublicKeyFromBase58(mint)
	if err != nil {
		return 0, fmt.Errorf("invalid mint address: %w", err)
	}

	client := rpc.New(rpcURL)

	// Query ALL token accounts owned by this wallet for the specified mint
	// This handles cases where USDC might be in non-ATA accounts
	accounts, err := client.GetTokenAccountsByOwner(
		ctx,
		walletPubKey,
		&rpc.GetTokenAccountsConfig{
			Mint: &mintPubKey,
		},
		rpc.CommitmentFinalized,
	)
	if err != nil {
		// If no accounts found, return 0 (not an error)
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "could not find") || strings.Contains(msg, "not found") {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to query token accounts: %w", err)
	}

	if accounts == nil || len(accounts.Value) == 0 {
		// No token accounts found for this mint
		return 0, nil
	}

	// Sum up balances from all token accounts
	var totalBalance int64
	for _, account := range accounts.Value {
		if account.Account == nil || account.Account.Data == nil {
			continue
		}

		// Parse the account data to get the balance
		// Token account data layout: mint(32) + owner(32) + amount(8) + ...
		data := account.Account.Data.GetBinary()
		if len(data) < 72 { // minimum size for a token account
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

		totalBalance += int64(amount)
	}

	return totalBalance, nil
}

// queryOnchainUSDCBalanceMinorATA is the legacy method that only queries the ATA
// Kept for backward compatibility if needed
func queryOnchainUSDCBalanceMinorATA(ctx context.Context, agentDID string) (int64, error) {
	wallet, err := walletFromDID(agentDID)
	if err != nil {
		return 0, err
	}

	mint := getenvDefaultBalance("QUERY_BALANCE_USDC_MINT", defaultDevnetUSDCMint)
	rpcURL := getenvDefaultBalance("QUERY_BALANCE_SOLANA_RPC", defaultSolanaRPC)

	walletPubKey, err := solana.PublicKeyFromBase58(wallet)
	if err != nil {
		return 0, fmt.Errorf("invalid wallet address in did: %w", err)
	}
	mintPubKey, err := solana.PublicKeyFromBase58(mint)
	if err != nil {
		return 0, fmt.Errorf("invalid mint address: %w", err)
	}

	ata, _, err := solana.FindAssociatedTokenAddress(walletPubKey, mintPubKey)
	if err != nil {
		return 0, fmt.Errorf("failed to find ATA: %w", err)
	}

	client := rpc.New(rpcURL)
	result, err := client.GetTokenAccountBalance(ctx, ata, rpc.CommitmentFinalized)
	if err != nil {
		// ATA 不存在时按 0 处理；其余错误上抛
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "could not find account") || strings.Contains(msg, "account not found") {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get token account balance: %w", err)
	}
	if result == nil || result.Value == nil {
		return 0, nil
	}

	var amount int64
	if _, err := fmt.Sscanf(result.Value.Amount, "%d", &amount); err != nil {
		return 0, fmt.Errorf("failed to parse token amount: %w", err)
	}
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
