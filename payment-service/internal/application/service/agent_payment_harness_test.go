package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stablepay/payment-service/internal/application/dto"
	"github.com/stablepay/payment-service/pkg/constants"
)

type memoryIntentStore struct {
	mu   sync.Mutex
	data map[string]string
}

func newMemoryIntentStore() *memoryIntentStore { return &memoryIntentStore{data: map[string]string{}} }
func (s *memoryIntentStore) Set(_ context.Context, key string, value interface{}, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value.(string)
	return nil
}
func (s *memoryIntentStore) Get(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	return v, nil
}
func (s *memoryIntentStore) GetDel(_ context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		return "", fmt.Errorf("not found")
	}
	delete(s.data, key)
	return v, nil
}

func newTestHarness() *AgentPaymentHarness {
	return NewAgentPaymentHarness(newMemoryIntentStore(), AgentHarnessConfig{
		Enabled: true, RequireIntentForPayment: true,
		AutoApproveMaxMinor: 5_000_000, MaxIntentAmountMinor: 50_000_000,
		IntentTTL: time.Minute, PolicyVersion: "test-v1",
	})
}

func newIntentRequest(amount string) *dto.CreatePaymentIntentRequest {
	return &dto.CreatePaymentIntentRequest{
		AgentDID: "did:solana:agent", SkillDID: "did:solana:merchant",
		AmountStr: amount, Currency: "USDC", Purpose: "unlock verified premium report",
	}
}

func TestAgentPaymentHarness_AutoApprovalIsOneShot(t *testing.T) {
	h := newTestHarness()
	created, err := h.Create(context.Background(), newIntentRequest("3.00"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Status != intentApproved || created.Decision != "ALLOW" {
		t.Fatalf("unexpected decision: %#v", created)
	}

	err = h.Consume(context.Background(), created.IntentID, "did:solana:agent", "did:solana:merchant", 3_000_000, constants.CurrencyUSDC)
	if err != nil {
		t.Fatalf("first Consume: %v", err)
	}
	if err = h.Consume(context.Background(), created.IntentID, "did:solana:agent", "did:solana:merchant", 3_000_000, constants.CurrencyUSDC); err == nil {
		t.Fatal("second Consume must reject a replayed intent")
	}
}

func TestAgentPaymentHarness_RequiresSignedApprovalForHighValueIntent(t *testing.T) {
	h := newTestHarness()
	created, err := h.Create(context.Background(), newIntentRequest("15.00"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Status != intentPending || created.ApprovalPayload == "" {
		t.Fatalf("expected pending confirmation, got %#v", created)
	}

	approved, err := h.Approve(context.Background(), &dto.ApprovePaymentIntentRequest{
		IntentID: created.IntentID, AgentDID: "did:solana:agent",
	}, func(payload string) error {
		if payload != created.ApprovalPayload {
			t.Fatalf("approval payload changed: %q", payload)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.Status != intentApproved {
		t.Fatalf("status=%s", approved.Status)
	}
	if err = h.Consume(context.Background(), created.IntentID, "did:solana:agent", "did:solana:merchant", 15_000_000, constants.CurrencyUSDC); err != nil {
		t.Fatalf("Consume approved intent: %v", err)
	}
}

func TestAgentPaymentHarness_DeniesAmountOutsidePolicy(t *testing.T) {
	h := newTestHarness()
	created, err := h.Create(context.Background(), newIntentRequest("51.00"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Decision != "DENY" || created.Status != "REJECTED" {
		t.Fatalf("unexpected decision: %#v", created)
	}
	if len(created.ReasonCodes) != 1 || created.ReasonCodes[0] != "AMOUNT_EXCEEDS_AGENT_POLICY" {
		t.Fatalf("unexpected reasons: %#v", created.ReasonCodes)
	}
}
