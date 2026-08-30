package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stablepay/verification-service/internal/domain/entity"
)

type fakePurchaseRepository struct {
	records map[string]*entity.PurchaseRecord
}

func (f *fakePurchaseRepository) Find(_ context.Context, agentDID, skillDID string) (*entity.PurchaseRecord, error) {
	record, ok := f.records[agentDID+"|"+skillDID]
	if !ok {
		return nil, errors.New("not found")
	}
	return record, nil
}

func (f *fakePurchaseRepository) Create(_ context.Context, record *entity.PurchaseRecord) error {
	if f.records == nil {
		f.records = map[string]*entity.PurchaseRecord{}
	}
	f.records[record.AgentDID+"|"+record.SkillDID] = record
	return nil
}

func TestGetPurchaseProofReturnsPersistedProofFields(t *testing.T) {
	createdAt := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	repository := &fakePurchaseRepository{records: map[string]*entity.PurchaseRecord{
		"did:agent|did:skill": {AgentDID: "did:agent", SkillDID: "did:skill", TxID: "tx-1", AmountMinor: 1250000, Currency: 1, TxHash: "hash-1", CreatedAt: createdAt},
	}}
	service := NewService(repository)

	result, err := service.GetPurchaseProof(context.Background(), "did:agent", "did:skill")
	if err != nil || !result.Purchased {
		t.Fatalf("expected purchased proof, got result=%+v err=%v", result, err)
	}
	if result.TxID != "tx-1" || result.TxHash != "hash-1" || result.AmountMinor == nil || *result.AmountMinor != 1250000 || result.Currency == nil || *result.Currency != 1 {
		t.Fatalf("proof fields were not preserved: %+v", result)
	}
}

func TestBatchVerifyPurchasePreservesOrderAndNotFoundResult(t *testing.T) {
	repository := &fakePurchaseRepository{records: map[string]*entity.PurchaseRecord{
		"did:agent|did:skill-a": {AgentDID: "did:agent", SkillDID: "did:skill-a", TxID: "tx-a", CreatedAt: time.Unix(10, 0).UTC()},
	}}
	service := NewService(repository)

	items, code, _, err := service.BatchVerifyPurchase(context.Background(), "did:agent", []string{"did:skill-a", "did:skill-b"})
	if err != nil || code != CodeOK || len(items) != 2 {
		t.Fatalf("unexpected batch result=%+v code=%d err=%v", items, code, err)
	}
	if !items[0].Purchased || items[0].TxID != "tx-a" || items[1].Purchased {
		t.Fatalf("batch results incorrect: %+v", items)
	}
}

func TestVerifyPurchaseValidatesRequiredDIDs(t *testing.T) {
	service := NewService(&fakePurchaseRepository{})
	result, err := service.VerifyPurchase(context.Background(), "", "did:skill")
	if err != nil || result.Code != CodeInvalidParameters {
		t.Fatalf("expected invalid-parameters result, got result=%+v err=%v", result, err)
	}
}
