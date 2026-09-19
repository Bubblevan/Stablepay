package sqlite

import (
	"context"
	"testing"

	"github.com/stablepay/merchant-server/internal/domain/repository"
)

func TestInvocationReceiptRoundTripIsDurable(t *testing.T) {
	repo, err := NewProductRepo(":memory:", true)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	want := &repository.InvocationReceipt{IdempotencyKey: "op-1", AgentDID: "did:agent:1", SKUID: "sku-1", ResponseJSON: []byte(`{"purchased":true}`)}
	if err := repo.SaveInvocationReceipt(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetInvocationReceipt(context.Background(), want.IdempotencyKey, want.AgentDID, want.SKUID)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.ResponseJSON) != string(want.ResponseJSON) {
		t.Fatalf("receipt mismatch: got=%s want=%s", got.ResponseJSON, want.ResponseJSON)
	}
}
