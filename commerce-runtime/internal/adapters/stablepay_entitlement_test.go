package adapters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStablePayVerificationEntitlementCanonicalizesRawSolanaPayee(t *testing.T) {
	const wallet = "FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z"
	const txID = "tx-entitlement-canonical"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("skill_did"); got != "did:solana:"+wallet {
			t.Fatalf("skill_did query = %q, want canonical Solana DID", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"message":"success","data":{"purchased":true,"tx_id":"` + txID + `","tx_hash":"hash-entitlement"}}`))
	}))
	defer server.Close()

	adapter := NewStablePayVerificationEntitlement(server.URL, "test-api-key")
	result, err := adapter.Verify(context.Background(), EntitlementQuery{
		IntentID:     "intent-entitlement-canonical",
		RequesterDID: "did:solana:agent",
		PayeeDID:     wallet,
		TxID:         txID,
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if result.Status != EntitlementValid || result.TxID != txID {
		t.Fatalf("Verify() result = %+v, want valid entitlement for %s", result, txID)
	}
}
