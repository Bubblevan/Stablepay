package decision

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

// ProviderAllowedAction is a positive authority allowlist. New actions are
// forbidden until explicitly added here; runtime-owned observations and facts
// never enter through this list.
func ProviderAllowedAction(action trace.ActionType) bool {
	switch action {
	case trace.ActionSelectMerchant, trace.ActionRetrySameMerchant,
		trace.ActionSwitchMerchant, trace.ActionRediscover, trace.ActionAskParent,
		trace.ActionStop:
		return true
	default:
		return false
	}
}

type RecoveryProposalInput struct {
	Episode            *episode.CommerceEpisode
	Context            *recovery.RecoveryContext
	CandidateSet       *catalog.CandidateSet
	Now                time.Time
	EvidenceRefs       []string
	ParentAllowed      bool
	RediscoveryAllowed bool
}

// RuleRecoveryProvider is deliberately not an LLM adapter. It produces only
// a proposal from persisted runtime facts; the runtime guard remains the
// authority that validates and commits it.
type RuleRecoveryProvider struct{}

func (RuleRecoveryProvider) ProposeRecovery(_ context.Context, input RecoveryProposalInput) (DecisionProposal, error) {
	if input.Episode == nil || input.Context == nil || input.Episode.State != episode.StateRecovering {
		return DecisionProposal{}, errors.New("recovery proposal requires RECOVERING episode")
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	input.Now = now
	if !now.Before(input.Episode.DeadlineAt) {
		return recoveryProposal(input, trace.ActionStop, "deadline exceeded")
	}
	// S5 gives each selected merchant one recovery retry. Further recovery
	// must select another candidate or rediscover; this also prevents a rule
	// provider from oscillating on the same merchant.
	if input.Episode.RetryCount < 1 {
		return recoveryProposal(input, trace.ActionRetrySameMerchant, "delivery retry remains")
	}
	if input.CandidateSet != nil {
		attempted := make(map[string]struct{}, len(input.Context.AttemptedMerchants))
		for _, m := range input.Context.AttemptedMerchants {
			attempted[m] = struct{}{}
		}
		for _, candidate := range input.CandidateSet.Candidates {
			if _, ok := attempted[candidate.MerchantDID]; ok || candidate.MerchantDID == input.Episode.SelectedMerchantDID {
				continue
			}
			proposal := baseRecoveryProposal(input, trace.ActionSwitchMerchant, "unattempted candidate remains")
			proposal.CandidateSetID = input.CandidateSet.CandidateSetID
			proposal.Target = &ProposalTarget{MerchantDID: candidate.MerchantDID, CapabilityID: candidate.CapabilityID, CatalogVersion: candidate.CatalogVersion, CatalogSnapshotHash: candidate.CatalogSnapshotHash, CatalogSnapshotRef: candidate.CatalogSnapshotRef}
			return proposal, nil
		}
	}
	if input.RediscoveryAllowed {
		return recoveryProposal(input, trace.ActionRediscover, "candidate set exhausted")
	}
	if input.ParentAllowed {
		return recoveryProposal(input, trace.ActionAskParent, "recovery requires parent decision")
	}
	return recoveryProposal(input, trace.ActionStop, "recovery exhausted")
}

func recoveryProposal(input RecoveryProposalInput, action trace.ActionType, rationale string) (DecisionProposal, error) {
	if !ProviderAllowedAction(action) {
		return DecisionProposal{}, ErrActionNotAllowed
	}
	return baseRecoveryProposal(input, action, rationale), nil
}
func baseRecoveryProposal(input RecoveryProposalInput, action trace.ActionType, rationale string) DecisionProposal {
	refs := append([]string(nil), input.EvidenceRefs...)
	return DecisionProposal{ProposalID: "recovery:" + input.Episode.EpisodeID + ":" + strings.ToLower(string(action)), EpisodeID: input.Episode.EpisodeID, BasedOnEventSequence: input.Episode.Version - 1, ProposedAction: action, Rationale: rationale, EvidenceRefs: refs, Confidence: 1, CreatedAt: input.Now.Add(-time.Nanosecond), ExpiresAt: input.Now.Add(time.Minute)}
}
