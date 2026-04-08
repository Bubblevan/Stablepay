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
