package memory

import (
	"context"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/recovery"
)

// MemoryRetrievalPlan controls only which advisory records are placed in an
// LLM context. It never grants authority over candidates, payment, delivery,
// entitlement, or parent approval.
type MemoryRetrievalPlan struct {
	Enabled                bool
	IncludeCurrentMerchant bool
	IncludeCandidates      bool
	IncludeRequester       bool
	IncludeParentSession   bool
	CandidateLimit         int
	PerCandidateLimit      int
	TotalLimit             int
	Reason                 string
}

type EpisodeSnapshot struct {
	State                episode.State
	SelectedMerchantDID  string
	SelectedCapabilityID string
	RequesterDID         string
	ParentSessionID      string
}

type MemoryRetrievalPolicy interface {
	Plan(EpisodeSnapshot, *recovery.RecoveryContext, *catalog.CandidateSet) MemoryRetrievalPlan
}

type DeterministicMemoryRetrievalPolicy struct{}

// TODO(S9): learned retrieval, credit assignment, RL, and self-evolution are
// intentionally out of scope for the S7.1 substrate.

func (DeterministicMemoryRetrievalPolicy) Plan(ep EpisodeSnapshot, recoveryContext *recovery.RecoveryContext, candidateSet *catalog.CandidateSet) MemoryRetrievalPlan {
	plan := MemoryRetrievalPlan{CandidateLimit: 8, PerCandidateLimit: 2, TotalLimit: 16}
	if episode.IsTerminal(ep.State) {
		plan.Reason = "terminal_no_llm_memory_context"
		return plan
	}
	switch ep.State {
	case episode.StateRecovering:
		plan.Enabled = true
		plan.IncludeCurrentMerchant = strings.TrimSpace(ep.SelectedMerchantDID) != "" || recoveryContext != nil
		plan.IncludeCandidates = candidateSet != nil
		plan.IncludeRequester = true
		plan.IncludeParentSession = true
		plan.Reason = "recovering_candidate_and_history_context"
	case episode.StateDiscovering:
		plan.Enabled = candidateSet != nil
		plan.IncludeCandidates = candidateSet != nil
		plan.IncludeRequester = true
		plan.IncludeParentSession = true
		plan.Reason = "discovering_candidate_history_context"
	case episode.StateNegotiating:
		plan.Enabled = true
		plan.IncludeCurrentMerchant = true
		plan.IncludeRequester = true
		plan.IncludeParentSession = true
		plan.Reason = "negotiating_current_history_context"
	default:
		plan.Reason = "payment_or_delivery_state_memory_disabled"
	}
	return plan
}

type MemoryWriteDecision string

const (
	MemoryWriteAdd    MemoryWriteDecision = "ADD"
	MemoryWriteUpdate MemoryWriteDecision = "UPDATE"
	MemoryWriteDelete MemoryWriteDecision = "DELETE"
	MemoryWriteNoop   MemoryWriteDecision = "NOOP"
)

type MemoryWritePolicy interface {
	Decide(context.Context, MemoryMutation, *MemoryRecord) (MemoryWriteDecision, error)
}

// DeterministicWritePolicy is intentionally not learned. A new identity is
// ADD; a validated new observation for an existing identity is UPDATE. The
// projector/store resolves exact observation replay as NOOP. DELETE is never
// selected automatically; learned write policies remain an S9 concern.
// TODO(S9): evaluate learned ADD/UPDATE/DELETE/NOOP and retrieval policies
// only after this deterministic substrate has an explicit offline evaluation.
type DeterministicWritePolicy struct{}

func (DeterministicWritePolicy) Decide(_ context.Context, candidate MemoryMutation, existing *MemoryRecord) (MemoryWriteDecision, error) {
	if candidate.Record == nil || candidate.Observation == nil {
		return "", ErrInvalidMemory
	}
	if existing == nil {
		return MemoryWriteAdd, nil
	}
	if existing.MemoryID != candidate.Record.MemoryID || !sameIdentityForPolicy(existing, candidate.Record) {
		return "", ErrMemoryConflict
	}
	for _, episodeID := range existing.SourceEpisodeIDs {
		if episodeID == candidate.Observation.SourceEpisodeID {
			for _, eventRef := range existing.SourceEventRefs {
				if eventRef == candidate.Observation.SourceEventRef {
					return MemoryWriteNoop, nil
				}
			}
		}
	}
	return MemoryWriteUpdate, nil
}

func sameIdentityForPolicy(left, right *MemoryRecord) bool {
	return left.Type == right.Type && left.Scope == right.Scope && left.RequesterDID == right.RequesterDID && left.ParentSessionID == right.ParentSessionID && left.MerchantDID == right.MerchantDID && left.CapabilityID == right.CapabilityID && left.CatalogVersion == right.CatalogVersion && left.CatalogSnapshotHash == right.CatalogSnapshotHash
}
