package recovery

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func TestS5FactsUseCanonicalContentHashes(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	approval := ParentApprovalRequest{
		ApprovalID: "approval-1", EpisodeID: "episode-1", RecoveryID: "recovery-1",
		ReasonCode: ReasonBudgetInsufficient, RequestedAction: "SWITCH_MERCHANT", ApprovalScope: BudgetIncrease,
		CurrentBudgetMinor: 1000, ConsumedMinor: 700, AvailableMinor: 300, SunkCostMinor: 700,
		RequestedBudgetIncreaseMinor: 500, ExpiresAt: now.Add(time.Hour), FactsRef: "parent-approval://1", CreatedAt: now,
	}
	if err := approval.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	if err := approval.Validate(); err != nil {
		t.Fatal(err)
	}
	if approval.PayloadHash == "sha256:"+hashReferenceForTest(approval.FactsRef) {
		t.Fatal("approval payload hash was derived only from FactsRef")
	}
	mutated := approval
	mutated.RequestedBudgetIncreaseMinor++
	if err := mutated.Validate(); err == nil {
		t.Fatal("mutated approval passed canonical payload validation")
	}

	decision := ParentDecisionFact{ApprovalID: approval.ApprovalID, EpisodeID: approval.EpisodeID, Decision: Approve, ActorRef: "parent:1", OccurredAt: now, FactsRef: "parent-decision://1"}
	if err := decision.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	if err := decision.Validate(); err != nil {
		t.Fatal(err)
	}
	amendment := BudgetAmendment{AmendmentID: "amendment-1", EpisodeID: approval.EpisodeID, OldLimitMinor: 1000, NewLimitMinor: 1500, DeltaMinor: 500, ApprovedBy: "parent:1", ApprovalRef: approval.ApprovalID, OccurredAt: now, FactsRef: "budget-amendment://1"}
	if err := amendment.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	if err := amendment.Validate(); err != nil {
		t.Fatal(err)
	}
}

func hashReferenceForTest(value string) string {
	// This deliberately is not the production hash implementation. It keeps
	// the assertion independent from ledger package internals.
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
