package blockchain

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/stablepay/blockchain-adapter/internal/domain/gateway"
)

func TestValidatePartiallySignedTransactionVerifiesEd25519Signature(t *testing.T) {
	txBase64, _, _, payer := newSignedTokenTransfer(t, 42)
	gateway := &SolanaGatewayImpl{network: "devnet", feePayerAddress: payer.String()}

	if err := gateway.ValidatePartiallySignedTransaction(txBase64); err != nil {
		t.Fatalf("valid partial transaction rejected: %v", err)
	}

	tx, err := solana.TransactionFromBase64(txBase64)
	if err != nil {
		t.Fatal(err)
	}
	for index, key := range tx.Message.AccountKeys {
		if key.Equals(payer) {
			tx.Signatures[index][0] ^= 0xff
			break
		}
	}
	tampered, err := tx.ToBase64()
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.ValidatePartiallySignedTransaction(tampered); err == nil {
		t.Fatal("tampered signature should be rejected")
	}
}

func TestValidateFeePayerAndTransferIntent(t *testing.T) {
	txBase64, owner, recipient, payer := newSignedTokenTransfer(t, 42)
	gateway := &SolanaGatewayImpl{network: "devnet", feePayerAddress: payer.String()}

	if err := gateway.ValidateFeePayer(txBase64, payer.String()); err != nil {
		t.Fatalf("valid fee payer rejected: %v", err)
	}
	if err := gateway.ValidateFeePayer(txBase64, owner.String()); err == nil {
		t.Fatal("fee payer mismatch should be rejected")
	}
	if err := gateway.ValidateTransferTransaction(txBase64, owner.String(), recipient.String(), "USDC", 42); err != nil {
		t.Fatalf("valid transfer rejected: %v", err)
	}
	if err := gateway.ValidateTransferTransaction(txBase64, owner.String(), recipient.String(), "USDC", 43); err == nil {
		t.Fatal("amount mismatch should be rejected")
	}
}

func TestValidatePartiallySignedTransactionRejectsUnsignedTransaction(t *testing.T) {
	_, _, _, payer := newSignedTokenTransfer(t, 42)
	wallet := solana.NewWallet()
	mint := solana.MustPublicKeyFromBase58(gateway.USDCDevnet)
	fromATA, _, err := solana.FindAssociatedTokenAddress(wallet.PublicKey(), mint)
	if err != nil {
		t.Fatal(err)
	}
	toATA, _, err := solana.FindAssociatedTokenAddress(payer, mint)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := token.NewTransferInstruction(42, fromATA, toATA, wallet.PublicKey(), nil).ValidateAndBuild()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := solana.NewTransaction([]solana.Instruction{ix}, solana.Hash{1}, solana.TransactionPayer(payer))
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := tx.ToBase64()
	if err != nil {
		t.Fatal(err)
	}
	gateway := &SolanaGatewayImpl{network: "devnet", feePayerAddress: payer.String()}
	if err := gateway.ValidatePartiallySignedTransaction(unsigned); err == nil {
		t.Fatal("unsigned transaction should be rejected")
	}
}

func newSignedTokenTransfer(t *testing.T, amount uint64) (string, solana.PublicKey, solana.PublicKey, solana.PublicKey) {
	t.Helper()
	owner := solana.NewWallet()
	recipient := solana.NewWallet()
	payer := solana.NewWallet()
	mint := solana.MustPublicKeyFromBase58(gateway.USDCDevnet)
	fromATA, _, err := solana.FindAssociatedTokenAddress(owner.PublicKey(), mint)
	if err != nil {
		t.Fatal(err)
	}
	toATA, _, err := solana.FindAssociatedTokenAddress(recipient.PublicKey(), mint)
	if err != nil {
		t.Fatal(err)
	}
	ix, err := token.NewTransferInstruction(amount, fromATA, toATA, owner.PublicKey(), nil).ValidateAndBuild()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := solana.NewTransaction([]solana.Instruction{ix}, solana.Hash{1}, solana.TransactionPayer(payer.PublicKey()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.PartialSign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(owner.PublicKey()) {
			return &owner.PrivateKey
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	encoded, err := tx.ToBase64()
	if err != nil {
		t.Fatal(err)
	}
	return encoded, owner.PublicKey(), recipient.PublicKey(), payer.PublicKey()
}
