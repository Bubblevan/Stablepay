package adapters

import "testing"

func TestWalletFromDIDAcceptsCanonicalSolanaRepresentations(t *testing.T) {
	const wallet = "2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZKJWdR"
	for _, value := range []string{wallet, "did:solana:" + wallet} {
		got, err := walletFromDID(value)
		if err != nil {
			t.Fatalf("walletFromDID(%q): %v", value, err)
		}
		if got != wallet {
			t.Fatalf("walletFromDID(%q) = %q, want %q", value, got, wallet)
		}
	}
}

func TestWalletFromDIDRejectsNonSolanaIdentity(t *testing.T) {
	if _, err := walletFromDID("did:merchant:not-a-wallet"); err == nil {
		t.Fatal("walletFromDID accepted a non-Solana identity")
	}
}
