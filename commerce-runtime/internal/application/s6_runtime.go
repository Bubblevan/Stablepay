package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/evidence"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type S6DecisionRequest struct {
	EpisodeID string
	Query     string
}

type S6DecisionResult struct {
	Context      llm.DecisionContext
	Proposal     decision.DecisionProposal
	Trace        llm.ModelDecisionTrace
	UsedFallback bool
}

// BuildDecisionContext is the only application read path used to assemble an
// LLM context. It selects bounded facts and never serializes the event log.
func (s *Service) BuildDecisionContext(ctx context.Context, request S6DecisionRequest) (llm.DecisionContext, error) {
	if s == nil || s.store == nil {
		return llm.DecisionContext{}, repository.ErrRepositoryUnavailable
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return llm.DecisionContext{}, err
	}
	var candidateSet *catalog.CandidateSet
	var recoveryContext *recovery.RecoveryContext
	if store, storeErr := s.s5Store(); storeErr == nil {
		if value, getErr := store.GetRecoveryContextByEpisode(ctx, current.EpisodeID); getErr == nil {
			recoveryContext = value
		} else if !errors.Is(getErr, repository.ErrNotFound) && !errors.Is(getErr, repository.ErrFactNotFound) {
			return llm.DecisionContext{}, getErr
		}
	}
	candidateSetID := current.SelectedCandidateSetID
	if candidateSetID == "" && recoveryContext != nil {
		candidateSetID = recoveryContext.CandidateSetID
	}
	if candidateSetID != "" {
		if store, storeErr := s.discoveryStore(); storeErr == nil {
			if value, getErr := store.GetCandidateSet(ctx, candidateSetID); getErr == nil {
				candidateSet = value
			} else if !errors.Is(getErr, repository.ErrNotFound) {
				return llm.DecisionContext{}, getErr
			}
		}
	}
	var latestValidation *invocation.ValidationEvidence
	if len(current.ValidationEvidenceRefs) > 0 {
		if store, storeErr := s.s4Store(); storeErr == nil {
			latestValidation, err = store.GetValidationEvidence(ctx, current.ValidationEvidenceRefs[len(current.ValidationEvidenceRefs)-1])
			if err != nil && !errors.Is(err, repository.ErrNotFound) && !errors.Is(err, repository.ErrFactNotFound) {
				return llm.DecisionContext{}, err
			}
		}
	}
	var retrieved []evidence.EvidenceRecord
	if s.evidenceRetriever != nil && strings.TrimSpace(request.Query) != "" {
		retrieved, err = s.evidenceRetriever.Retrieve(ctx, evidence.RetrievalQuery{Query: request.Query, AllowedSourceTypes: []evidence.SourceType{evidence.SourceMerchantCapabilityDoc, evidence.SourceProtocolDoc, evidence.SourceMerchantConstraint}, Limit: 8, Now: s.clock().UTC()})
		if err != nil {
			return llm.DecisionContext{}, err
		}
		for index := range retrieved {
			if store, ok := s.store.(repository.EvidenceRepository); ok {
				if err := store.SaveEvidenceRecord(ctx, &retrieved[index]); err != nil {
					return llm.DecisionContext{}, err
				}
			}
		}
	}
	return llm.BuildDecisionContext(llm.ContextInput{Episode: current, AllowedActions: s6AllowedActions(current.State), CandidateSet: candidateSet, Recovery: recoveryContext, LatestValidation: latestValidation, RetrievedEvidence: retrieved})
}

// ProposeRecoveryDecision invokes the configured LLM provider. It returns a
// proposal and trace only; no episode, payment, ledger, merchant, or parent
// side effect occurs here.
func (s *Service) ProposeRecoveryDecision(ctx context.Context, request S6DecisionRequest) (S6DecisionResult, error) {
	contextValue, err := s.BuildDecisionContext(ctx, request)
	if err != nil {
		return S6DecisionResult{}, err
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return S6DecisionResult{}, err
	}
	if current.State != episode.StateRecovering {
		return S6DecisionResult{}, decision.ErrActionNotAllowed
	}
	if s.llmProvider != nil {
		result, providerErr := s.llmProvider.ProposeWithTrace(ctx, contextValue)
		if providerErr == nil {
			if err := s.persistModelTrace(ctx, &result.Trace); err != nil {
				return S6DecisionResult{}, err
			}
			return S6DecisionResult{Context: contextValue, Proposal: result.Proposal, Trace: result.Trace}, nil
		}
		if result.Trace.Validate() != nil {
			now := s.clock().UTC()
			result.Trace = llm.ModelDecisionTrace{TraceID: "llm-error:" + current.EpisodeID + ":" + fmt.Sprint(current.Version), EpisodeID: current.EpisodeID, Provider: "llm", ModelRef: "configured", ContextHash: contextHashOrEmpty(contextValue), EvidenceRefs: fallbackEvidenceRefs(contextValue), RequestStartedAt: now, ResponseReceivedAt: now, Status: llm.TraceTransportError, ErrorCode: "LLM_UNAVAILABLE"}
		}
		if err := s.persistModelTrace(ctx, &result.Trace); err != nil {
			return S6DecisionResult{}, err
		}
		if !s.allowRuleFallback {
			return S6DecisionResult{}, providerErr
		}
		fallback, fallbackErr := s.ruleFallback(ctx, contextValue, current, request.Query, providerErr)
		if fallbackErr != nil {
			return S6DecisionResult{}, fallbackErr
		}
		return fallback, nil
	}
	if !s.allowRuleFallback {
		return S6DecisionResult{}, llm.ErrLLMUnavailable
	}
	return s.ruleFallback(ctx, contextValue, current, request.Query, llm.ErrLLMUnavailable)
}

// ExecuteRecoveryDecision is the explicit LLM-to-runtime handoff. The model
// output becomes an ordinary proposal and must pass the existing CommitProposal
// guard before any recovery transition can be written.
func (s *Service) ExecuteRecoveryDecision(ctx context.Context, request S6DecisionRequest) (CommitResult, S6DecisionResult, error) {
	result, err := s.ProposeRecoveryDecision(ctx, request)
	if err != nil {
		return CommitResult{}, result, err
	}
	if evidenceErr := llm.ValidateProposalEvidenceAgainstContext(result.Context, result.Proposal); evidenceErr != nil {
		rejected := result.Trace
		rejected.TraceID = result.Trace.TraceID + ":context"
		rejected.Status = llm.TraceGuardRejected
		rejected.ErrorCode = "CONTEXT_EVIDENCE_MISMATCH"
		rejected.ResponseReceivedAt = s.clock().UTC()
		_ = s.persistModelTrace(ctx, &rejected)
		return CommitResult{}, result, evidenceErr
	}
	key := "s6:" + result.Proposal.ProposalID
	commit, commitErr := s.CommitProposal(ctx, CommitRequest{Proposal: result.Proposal, Action: trace.Action{Type: result.Proposal.ProposedAction, IdempotencyKey: key}, Actor: func() string {
		if result.UsedFallback {
			return "rule-fallback"
		}
		return "llm"
	}(), TraceID: result.Trace.TraceID})
	if commitErr != nil {
		rejected := result.Trace
		rejected.TraceID = result.Trace.TraceID + ":guard"
		rejected.Status = llm.TraceGuardRejected
		rejected.ErrorCode = errorCodeForS6(commitErr)
		rejected.ResponseReceivedAt = s.clock().UTC()
		_ = s.persistModelTrace(ctx, &rejected)
		return CommitResult{}, result, commitErr
	}
	return commit, result, nil
}

func (s *Service) ruleFallback(ctx context.Context, contextValue llm.DecisionContext, current *episode.CommerceEpisode, query string, cause error) (S6DecisionResult, error) {
	if contextValue.Recovery == nil {
		return S6DecisionResult{}, fmt.Errorf("%w: rule fallback requires a persisted recovery context", llm.ErrLLMUnavailable)
	}
	var recoveryContext *recovery.RecoveryContext
	if store, err := s.s5Store(); err == nil {
		recoveryContext, err = store.GetRecoveryContextByEpisode(ctx, current.EpisodeID)
		if err != nil {
			return S6DecisionResult{}, err
		}
	}
	var candidateSet *catalog.CandidateSet
	if contextValue.CandidateSet.CandidateSetID != "" {
		if store, err := s.discoveryStore(); err == nil {
			candidateSet, err = store.GetCandidateSet(ctx, contextValue.CandidateSet.CandidateSetID)
			if err != nil {
				return S6DecisionResult{}, err
			}
		}
	}
	proposal, err := (decision.RuleRecoveryProvider{}).ProposeRecovery(ctx, decision.RecoveryProposalInput{Episode: current, Context: recoveryContext, CandidateSet: candidateSet, Now: s.clock().UTC(), EvidenceRefs: fallbackEvidenceRefs(contextValue), ParentAllowed: true, RediscoveryAllowed: true})
	if err != nil {
		return S6DecisionResult{}, err
	}
	proposal.ModelRef = "rule-recovery-fallback"
	now := s.clock().UTC()
	traceValue := llm.ModelDecisionTrace{TraceID: "rule-fallback:" + current.EpisodeID + ":" + fmt.Sprint(current.Version), EpisodeID: current.EpisodeID, Provider: "rule", ModelRef: "rule-recovery", ContextHash: contextHashOrEmpty(contextValue), EvidenceRefs: fallbackEvidenceRefs(contextValue), RequestStartedAt: now, ResponseReceivedAt: now, Status: llm.TraceFallback, ErrorCode: "LLM_UNAVAILABLE", FallbackReason: cause.Error()}
	if err := s.persistModelTrace(ctx, &traceValue); err != nil {
		return S6DecisionResult{}, err
	}
	return S6DecisionResult{Context: contextValue, Proposal: proposal, Trace: traceValue, UsedFallback: true}, nil
}

func (s *Service) persistModelTrace(ctx context.Context, value *llm.ModelDecisionTrace) error {
	if value == nil {
		return llm.ErrInvalidModelOutput
	}
	if err := value.Validate(); err != nil {
		return err
	}
	store, ok := s.store.(repository.ModelDecisionTraceRepository)
	if !ok {
		return repository.ErrRepositoryUnavailable
	}
	return store.SaveModelDecisionTrace(ctx, value)
}

func s6AllowedActions(state episode.State) []trace.ActionType {
	switch state {
	case episode.StateRecovering:
		return []trace.ActionType{trace.ActionRetrySameMerchant, trace.ActionSwitchMerchant, trace.ActionRediscover, trace.ActionAskParent, trace.ActionStop}
	case episode.StateDiscovering:
		return []trace.ActionType{trace.ActionSelectMerchant, trace.ActionStop}
	default:
		return []trace.ActionType{trace.ActionStop}
	}
}

func fallbackEvidenceRefs(value llm.DecisionContext) []string {
	refs := make([]string, 0, len(value.RetrievedEvidence)+2)
	for _, record := range value.RetrievedEvidence {
		refs = append(refs, record.EvidenceRef)
	}
	if value.Recovery != nil {
		refs = append(refs, value.Recovery.FactsRef, value.Recovery.PayloadHash)
	}
	if value.CandidateSet.FactsRef != "" {
		refs = append(refs, value.CandidateSet.FactsRef, value.CandidateSet.PayloadHash)
	}
	return uniqueS6Strings(refs)
}

func contextHashOrEmpty(value llm.DecisionContext) string {
	hash, _ := value.Hash()
	return hash
}

func uniqueS6Strings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func errorCodeForS6(err error) string {
	if errors.Is(err, decision.ErrStaleEventSequence) {
		return "STALE_EVENT_SEQUENCE"
	}
	if errors.Is(err, decision.ErrUnknownEvidenceReference) {
		return "UNKNOWN_EVIDENCE"
	}
	if errors.Is(err, decision.ErrMerchantNotInCandidateSet) {
		return "HALLUCINATED_TARGET"
	}
	if errors.Is(err, decision.ErrProposalExpired) {
		return "PROPOSAL_EXPIRED"
	}
	return "GUARD_REJECTED"
}

var _ = time.Time{}
