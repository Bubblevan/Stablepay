package rpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stablepay/verification-service/internal/application"
	"github.com/stablepay/verification-service/internal/domain/entity"
	"github.com/stablepay/verification-service/kitex_gen/stablepay/common"
	"github.com/stablepay/verification-service/kitex_gen/stablepay/verification_service"
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
	f.records[record.AgentDID+"|"+record.SkillDID] = record
	return nil
}

func TestGetPurchaseProofMapsPersistedProofFields(t *testing.T) {
	repository := &fakePurchaseRepository{records: map[string]*entity.PurchaseRecord{
		"did:agent|did:skill": {AgentDID: "did:agent", SkillDID: "did:skill", TxID: "tx-1", AmountMinor: 5000000, Currency: 1, TxHash: "hash-1", CreatedAt: time.Unix(10, 0).UTC()},
	}}
	handler := NewHandler(application.NewService(repository))

	response, err := handler.GetPurchaseProof(context.Background(), &verification_service.GetPurchaseProofRequest{AgentDid: common.DID("did:agent"), SkillDid: common.DID("did:skill")})
	if err != nil || response.Base.Code != application.CodeOK || !response.Purchased {
		t.Fatalf("unexpected proof response=%+v err=%v", response, err)
	}
	if response.AmountMinor == nil || *response.AmountMinor != 5000000 || response.Currency == nil || *response.Currency != common.Currency(1) || response.TxHash == nil || *response.TxHash != common.TxHash("hash-1") {
		t.Fatalf("proof fields were not mapped: %+v", response)
	}
}

func TestBatchVerifyPurchasePreservesResponseOrder(t *testing.T) {
	repository := &fakePurchaseRepository{records: map[string]*entity.PurchaseRecord{
		"did:agent|did:skill-a": {AgentDID: "did:agent", SkillDID: "did:skill-a", TxID: "tx-a", CreatedAt: time.Unix(10, 0).UTC()},
	}}
	handler := NewHandler(application.NewService(repository))
	response, err := handler.BatchVerifyPurchase(context.Background(), &verification_service.BatchVerifyPurchaseRequest{AgentDid: common.DID("did:agent"), SkillDids: []common.DID{"did:skill-a", "did:skill-b"}})
	if err != nil || len(response.Items) != 2 {
		t.Fatalf("expected two ordered items, response=%+v err=%v", response, err)
	}
	if response.Items[0].SkillDid != common.DID("did:skill-a") || !response.Items[0].Purchased || response.Items[1].SkillDid != common.DID("did:skill-b") || response.Items[1].Purchased {
		t.Fatalf("batch order or purchased flags incorrect: %+v", response.Items)
	}
}
