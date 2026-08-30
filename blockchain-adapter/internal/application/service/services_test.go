package service

import (
	"context"
	"testing"
	"time"

	"github.com/stablepay/blockchain-adapter/internal/domain/entity"
	"github.com/stablepay/blockchain-adapter/internal/domain/gateway"
	"github.com/stablepay/blockchain-adapter/internal/domain/vo"
)

type fakeSolanaGateway struct {
	network       string
	tokenBalance  uint64
	solBalance    uint64
	recentBlock   string
	feePayer      string
	lastMint      string
	lastBuildFrom string
}

func (f *fakeSolanaGateway) GetNetwork() string { return f.network }
func (f *fakeSolanaGateway) GetBalance(context.Context, string) (uint64, error) {
	return f.solBalance, nil
}
func (f *fakeSolanaGateway) GetTokenBalance(_ context.Context, _, mint string) (uint64, error) {
	f.lastMint = mint
	return f.tokenBalance, nil
}
func (f *fakeSolanaGateway) GetRecentBlockhash(context.Context) (string, error) {
	return f.recentBlock, nil
}
func (f *fakeSolanaGateway) BuildSPLTransferTx(_ context.Context, from, _, _ string, _ uint64) (string, error) {
	f.lastBuildFrom = from
	return "built", nil
}
func (f *fakeSolanaGateway) SendTransaction(context.Context, string) (string, error) {
	return "tx-hash", nil
}
func (f *fakeSolanaGateway) GetTransactionStatus(context.Context, string) (*vo.TxStatusVO, error) {
	return &vo.TxStatusVO{}, nil
}
func (f *fakeSolanaGateway) WaitForConfirmation(context.Context, string, time.Duration) (*vo.TxStatusVO, error) {
	return &vo.TxStatusVO{}, nil
}
func (f *fakeSolanaGateway) GetExplorerURL(txHash string) string {
	return "https://explorer.test/" + txHash
}
func (f *fakeSolanaGateway) GetFeePayerAddress() string { return f.feePayer }
func (f *fakeSolanaGateway) EstimateFee(context.Context) (int64, error) {
	return 5000, nil
}
func (f *fakeSolanaGateway) ValidatePartiallySignedTransaction(string) error { return nil }
func (f *fakeSolanaGateway) ValidateFeePayer(string, string) error           { return nil }
func (f *fakeSolanaGateway) ValidateTransferTransaction(string, string, string, string, uint64) error {
	return nil
}

type fakeTransactionBuilder struct{}

func (f *fakeTransactionBuilder) BuildSOLTransferTx(string, string, uint64, string) (string, error) {
	return "", nil
}
func (f *fakeTransactionBuilder) BuildSOLTransferTxWithFeePayer(string, string, string, uint64, string) (string, error) {
	return "", nil
}
func (f *fakeTransactionBuilder) BuildUnsignedTransaction(*gateway.BuildTransactionRequest) (string, error) {
	return "unsigned-tx", nil
}

func (f *fakeTransactionBuilder) DeserializeBase64Tx(string) (*entity.TransactionEntity, error) {
	return nil, nil
}
func (f *fakeTransactionBuilder) SerializeToBase64(*entity.TransactionEntity) (string, error) {
	return "", nil
}
func (f *fakeTransactionBuilder) SignTransaction(*entity.TransactionEntity, string) error { return nil }
func (f *fakeTransactionBuilder) ValidateTransfer(string, string, uint64, uint64) error   { return nil }

func TestBalanceQueryServiceResolvesDevnetUSDC(t *testing.T) {
	gateway := &fakeSolanaGateway{network: "devnet", tokenBalance: 123456}
	service := NewBalanceQueryService(gateway)

	got, err := service.GetBalance(context.Background(), &BalanceQuery{
		WalletAddress: "wallet-1",
		Currency:      "USDC",
	})
	if err != nil {
		t.Fatalf("GetBalance() error = %v", err)
	}
	if got.Balance != 123456 || got.Decimals != 6 || got.Symbol != "USDC" {
		t.Fatalf("unexpected balance result: %+v", got)
	}
	if gateway.lastMint != "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU" {
		t.Fatalf("unexpected mint address: %s", gateway.lastMint)
	}
}

func TestBalanceQueryServiceRejectsMissingWallet(t *testing.T) {
	service := NewBalanceQueryService(&fakeSolanaGateway{})
	if _, err := service.GetBalance(context.Background(), &BalanceQuery{}); err == nil {
		t.Fatal("GetBalance() should reject an empty wallet address")
	}
}

func TestTransferCmdValidation(t *testing.T) {
	service := &TransferCmdService{}
	if err := service.validateTransferParams(&TransferCmd{}); err == nil {
		t.Fatal("validateTransferParams() should reject an empty destination")
	}
	cmd := &TransferCmd{ToAddress: "recipient", AmountMinor: 1}
	if err := service.validateTransferParams(cmd); err != nil {
		t.Fatalf("validateTransferParams() error = %v", err)
	}
	if cmd.TxID == "" {
		t.Fatal("validateTransferParams() should generate a TxID")
	}
}

func TestMapChainStatusToEntity(t *testing.T) {
	tests := map[string]string{
		"confirmed": "completed",
		"finalized": "completed",
		"failed":    "failed",
		"pending":   "pending",
	}
	for input, want := range tests {
		if got := string(mapChainStatusToEntity(input)); got != want {
			t.Errorf("mapChainStatusToEntity(%q) = %q, want %q", input, got, want)
		}
	}
}
