package decision

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func TestRuntimeGuardMemoryRefsMustComeFromCurrentContext(t *testing.T) {
	current, _, _, now := guardFixtures(t)
	current.State = episode.StateRecovering
	proposal := DecisionProposal{ProposalID: "s7-memory-proposal", EpisodeID: current.EpisodeID, BasedOnEventSequence: current.Version - 1, ProposedAction: trace.ActionStop, MemoryRefs: []string{"memory://known"}, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
	known := map[string]struct{}{"memory://known": {}}
	if _, err := (RuntimeGuard{}).EvaluateWithMemory(current, proposal, nil, known, trace.Observation{Type: trace.ObservationPolicyDenied}, now); err != nil {
		t.Fatal(err)
	}
	proposal.MemoryRefs = []string{"memory://outside"}
	if _, err := (RuntimeGuard{}).EvaluateWithMemory(current, proposal, nil, known, trace.Observation{Type: trace.ObservationPolicyDenied}, now); !errors.Is(err, ErrUnknownMemoryReference) {
		t.Fatalf("outside memory ref error=%v", err)
	}
}

func TestMemoryCannotAddCandidateOutsideCurrentCandidateSet(t *testing.T) {
	current, _, _, now := guardFixtures(t)
	current.State = episode.StateDiscovering
	candidateSet := catalog.CandidateSet{CandidateSetID: "candidate-set", EpisodeID: current.EpisodeID, RequestID: current.RequestID, QueryHash: "sha256:query", GeneratedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), FactsRef: "candidate-set://candidate-set", Candidates: []catalog.Candidate{{MerchantDID: "did:merchant:eligible", CapabilityID: "capability-1", PayeeDID: "did:payee:eligible", CatalogVersion: "v1", CatalogSnapshotHash: "sha256:" + strings.Repeat("a", 64), CatalogSnapshotRef: "catalog://eligible", CatalogValidFrom: now.Add(-time.Minute), CatalogValidUntil: now.Add(time.Minute), Eligibility: catalog.EligibilityFacts{StatusActive: true, NotExpired: true, TaskTypeCompatible: true, InputContentTypeCompatible: true, OutputContentTypeCompatible: true, ProtocolCompatible: true, CurrencyCompatible: true, PriceHintWithinBudget: true, SemanticConstraintsCompatible: true}}}}
	hash, err := candidateSet.PayloadHashFor()
	if err != nil {
		t.Fatal(err)
	}
	candidateSet.PayloadHash = hash
	proposal := DecisionProposal{ProposalID: "memory-candidate", EpisodeID: current.EpisodeID, BasedOnEventSequence: current.Version - 1, ProposedAction: trace.ActionSelectMerchant, CandidateSetID: candidateSet.CandidateSetID, Target: &ProposalTarget{MerchantDID: "did:merchant:from-memory", CapabilityID: "capability-1"}, MemoryRefs: []string{"memory://historical-eligible"}, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
	_, err = (RuntimeGuard{}).EvaluateMerchantSelectionWithMemory(current, proposal, nil, map[string]struct{}{"memory://historical-eligible": {}}, trace.Observation{Type: trace.ObservationCandidatesFound}, now, &candidateSet)
	if !errors.Is(err, ErrMerchantNotInCandidateSet) {
		t.Fatalf("memory recommendation bypassed candidate boundary: %v", err)
	}
}
