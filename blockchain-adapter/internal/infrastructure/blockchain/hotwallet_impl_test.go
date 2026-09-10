package blockchain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/mr-tron/base58"
)

func TestNewHotWalletRejectsMissingFile(t *testing.T) {
	if _, err := NewHotWallet(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected missing wallet file to fail")
	}
}

func TestNewHotWalletValidatesKeyAndAddress(t *testing.T) {
	wallet := solana.NewWallet()
	valid := HotWalletImpl{
		Address: wallet.PublicKey().String(), PublicKey: wallet.PublicKey().String(),
		PrivateKey: base58.Encode(wallet.PrivateKey), Role: "hot_wallet",
	}
	path := filepath.Join(t.TempDir(), "wallet.json")
	data, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHotWallet(path); err != nil {
		t.Fatalf("expected valid wallet, got %v", err)
	}
	valid.Address = solana.NewWallet().PublicKey().String()
	data, _ = json.Marshal(valid)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHotWallet(path); err == nil {
		t.Fatal("expected mismatched wallet address to fail")
	}
}
