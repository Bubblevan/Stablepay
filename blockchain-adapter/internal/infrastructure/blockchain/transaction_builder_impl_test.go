package blockchain

import (
	"encoding/binary"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/stablepay/blockchain-adapter/internal/domain/gateway"
)

func TestBuildUnsignedTransactionConvertsBusinessMinorToTokenRaw(t *testing.T) {
	from := solana.NewWallet().PublicKey()
	to := solana.NewWallet().PublicKey()
	feePayer := solana.NewWallet().PublicKey()

	builder := &TransactionBuilderImpl{network: "devnet"}
	encoded, err := builder.BuildUnsignedTransaction(&gateway.BuildTransactionRequest{
		FromAddress:     from.String(),
		ToAddress:       to.String(),
		AmountMinor:     1,
		Currency:        "USDC",
		RecentBlockhash: solana.Hash{1}.String(),
		FeePayerAddress: feePayer.String(),
	})
	if err != nil {
		t.Fatalf("BuildUnsignedTransaction() error = %v", err)
	}

	tx, err := solana.TransactionFromBase64(encoded)
	if err != nil {
		t.Fatalf("decode transaction: %v", err)
	}
	if len(tx.Message.Instructions) != 1 {
		t.Fatalf("instruction count = %d, want 1", len(tx.Message.Instructions))
	}
	data := tx.Message.Instructions[0].Data
	if len(data) != 9 || data[0] != 3 {
		t.Fatalf("unexpected SPL transfer data: %v", data)
	}
	if got := binary.LittleEndian.Uint64(data[1:]); got != 10000 {
		t.Fatalf("encoded token amount = %d, want 10000 raw units", got)
	}
}
