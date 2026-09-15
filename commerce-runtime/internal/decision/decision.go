// Package decision contains proposals and the deterministic Runtime Guard.
package decision

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type DecisionProposal struct {
	ProposalID           string           `json:"proposal_id"`
	EpisodeID            string           `json:"episode_id"`
	BasedOnEventSequence uint64           `json:"based_on_event_sequence"`
	ProposedAction       trace.ActionType `json:"proposed_action"`
	Target               *ProposalTarget  `json:"target,omitempty"`
	Rationale            string           `json:"rationale,omitempty"`
	EvidenceRefs         []string         `json:"evidence_refs,omitempty"`
	Confidence           float64          `json:"confidence,omitempty"`
	ModelRef             string           `json:"model_ref,omitempty"`
	ExpiresAt            time.Time        `json:"expires_at"`
	CreatedAt            time.Time        `json:"created_at"`
}

type ProposalTarget struct {
	MerchantDID  string `json:"merchant_did,omitempty"`
	CapabilityID string `json:"capability_id,omitempty"`
}

var (
	ErrInvalidProposal           = errors.New("invalid decision proposal")
	ErrProposalForAnotherEpisode = errors.New("proposal belongs to another episode")
	ErrProposalExpired           = errors.New("decision proposal has expired")
	ErrFutureEventSequence       = errors.New("proposal references a future event sequence")
	ErrStaleEventSequence        = errors.New("proposal references a stale event sequence")
	ErrUnknownEvidenceReference  = errors.New("proposal references unknown evidence")
	ErrActionNotAllowed          = errors.New("proposed action is not allowed in the current state")
	ErrProposalAfterTerminal     = errors.New("proposal cannot be committed after terminal state")
	ErrAttemptLimit              = errors.New("episode attempt limit has been reached")
)

func (p DecisionProposal) Validate() error {
	if strings.TrimSpace(p.ProposalID) == "" || strings.TrimSpace(p.EpisodeID) == "" || !trace.KnownAction(p.ProposedAction) {
		return ErrInvalidProposal
	}
	if p.ExpiresAt.IsZero() || p.CreatedAt.IsZero() || !p.ExpiresAt.After(p.CreatedAt) {
		return ErrInvalidProposal
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return ErrInvalidProposal
	}
	seen := make(map[string]struct{}, len(p.EvidenceRefs))
	for _, ref := range p.EvidenceRefs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return ErrInvalidProposal
		}
		if _, ok := seen[ref]; ok {
			return ErrInvalidProposal
		}
		seen[ref] = struct{}{}
	}
	return nil
}

type GuardResult struct {
	Verdict   trace.RuntimeVerdict
	NextState episode.State
}

type RuntimeGuard struct{}

func NewRuntimeGuard() RuntimeGuard { return RuntimeGuard{} }

// Evaluate verifies proposal authority without allowing the proposal to select
// an arbitrary next state. knownEvidenceRefs is built from persisted events.
func (RuntimeGuard) Evaluate(current *episode.CommerceEpisode, proposal DecisionProposal, knownEvidenceRefs map[string]struct{}, observation trace.Observation, now time.Time) (GuardResult, error) {
	if current == nil {
		return GuardResult{}, ErrInvalidProposal
	}
	if err := proposal.Validate(); err != nil {
		return GuardResult{}, err
	}
	checks := make([]trace.RuntimeCheck, 0, 8)
	addFailure := func(name string, err error) (GuardResult, error) {
		verdict := trace.RuntimeVerdict{Allowed: false, Checks: append(checks, trace.Check(name, false, err.Error())), Reason: err.Error()}
		return GuardResult{Verdict: verdict}, err
	}
	if proposal.EpisodeID != current.EpisodeID {
		return addFailure("episode_match", ErrProposalForAnotherEpisode)
	}
	checks = append(checks, trace.Check("episode_match", true, "proposal targets current episode"))
	if episode.IsTerminal(current.State) {
		return addFailure("terminal_state", ErrProposalAfterTerminal)
	}
	if !proposal.ExpiresAt.After(now) {
		return addFailure("proposal_expiry", ErrProposalExpired)
	}
	if proposal.BasedOnEventSequence > current.Version-1 {
		return addFailure("event_sequence_future", ErrFutureEventSequence)
	}
	if proposal.BasedOnEventSequence < current.Version-1 {
		return addFailure("event_sequence_stale", ErrStaleEventSequence)
	}
	checks = append(checks, trace.Check("event_sequence", true, fmt.Sprintf("sequence=%d", proposal.BasedOnEventSequence)))
	for _, ref := range proposal.EvidenceRefs {
		if _, ok := knownEvidenceRefs[ref]; !ok {
			return addFailure("evidence_reference", fmt.Errorf("%w: %s", ErrUnknownEvidenceReference, ref))
		}
	}
	checks = append(checks, trace.Check("evidence_references", true, "all proposal evidence is known"))
	next, allowed := episode.StateForAction(current.State, proposal.ProposedAction, observation.Type)
	if !allowed {
		return addFailure("state_action", fmt.Errorf("%w: %s in %s", ErrActionNotAllowed, proposal.ProposedAction, current.State))
	}
	checks = append(checks, trace.Check("state_action", true, fmt.Sprintf("%s -> %s", current.State, next)))
	if !now.Before(current.DeadlineAt) {
		return addFailure("episode_deadline", episode.ErrEpisodeExpired)
	}
	if current.ActionCount >= current.MaxTotalAttempts {
		return addFailure("total_attempts", ErrAttemptLimit)
	}
	if proposal.ProposedAction == trace.ActionCreatePayment && current.PaymentAttemptCount >= current.MaxPaymentAttempts {
		return addFailure("payment_attempts", ErrAttemptLimit)
	}
	if proposal.ProposedAction == trace.ActionRetrySameMerchant && current.RetryCount >= current.MaxDeliveryAttempts-1 {
		return addFailure("delivery_retries", ErrAttemptLimit)
	}
	if proposal.ProposedAction == trace.ActionInvoke && current.State == episode.StateInvokingDelivery && current.DeliveryAttemptCount >= current.MaxDeliveryAttempts {
		return addFailure("delivery_attempts", ErrAttemptLimit)
	}
	checks = append(checks, trace.Check("attempt_limits", true, "attempt limits remain"))
	checks = append(checks, trace.Check("episode_deadline", true, "episode is before deadline"))
	return GuardResult{Verdict: trace.RuntimeVerdict{Allowed: true, Checks: checks}, NextState: next}, nil
}

// StaticDecisionProvider is a test/initial-rule provider. It only returns a
// proposal and has no repository access or write capability.
type DecisionProvider interface {
	Propose(ctx context.Context, input ProposalInput) (DecisionProposal, error)
}

type ProposalInput struct {
	EpisodeID            string
	CurrentEventSequence uint64
	Action               trace.ActionType
	Now                  time.Time
	TTL                  time.Duration
}

type StaticDecisionProvider struct{ Proposal DecisionProposal }

func (p StaticDecisionProvider) Propose(_ context.Context, _ ProposalInput) (DecisionProposal, error) {
	if err := p.Proposal.Validate(); err != nil {
		return DecisionProposal{}, err
	}
	return p.Proposal, nil
}
