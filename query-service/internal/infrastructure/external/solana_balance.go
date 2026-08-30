package external

import (
	"context"
	"fmt"
	"strings"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
)

type SolanaBalanceProvider struct {
	client *rpc.Client
	mint   solana.PublicKey
}

func NewSolanaBalanceProvider(endpoint, mint string) (*SolanaBalanceProvider, error) {
	mintKey, err := solana.PublicKeyFromBase58(mint)
	if err != nil {
		return nil, fmt.Errorf("parse USDC mint: %w", err)
	}
	return &SolanaBalanceProvider{client: rpc.New(endpoint), mint: mintKey}, nil
}

func (p *SolanaBalanceProvider) USDCBalanceMinor(ctx context.Context, agentDID string) (int64, error) {
	const prefix = "did:solana:"
	if !strings.HasPrefix(agentDID, prefix) || len(agentDID) <= len(prefix) {
		return 0, fmt.Errorf("invalid agent_did format")
	}
	wallet, err := solana.PublicKeyFromBase58(strings.TrimSpace(strings.TrimPrefix(agentDID, prefix)))
	if err != nil {
		return 0, fmt.Errorf("parse wallet address: %w", err)
	}
	accounts, err := p.client.GetTokenAccountsByOwner(ctx, wallet, &rpc.GetTokenAccountsConfig{Mint: &p.mint}, &rpc.GetTokenAccountsOpts{Commitment: rpc.CommitmentFinalized})
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "not found") || strings.Contains(message, "could not find") {
			return 0, nil
		}
		return 0, fmt.Errorf("query token accounts: %w", err)
	}
	if accounts == nil {
		return 0, nil
	}
	var total int64
	for _, account := range accounts.Value {
		if account.Account.Data == nil {
			continue
		}
		data := account.Account.Data.GetBinary()
		if len(data) < 72 {
			continue
		}
		amount := uint64(data[64]) | uint64(data[65])<<8 | uint64(data[66])<<16 | uint64(data[67])<<24 | uint64(data[68])<<32 | uint64(data[69])<<40 | uint64(data[70])<<48 | uint64(data[71])<<56
		total += int64(amount)
	}
	return total, nil
}
