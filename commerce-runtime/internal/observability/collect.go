package observability

import (
	"context"
	"errors"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/repository"
)

// Collect reads only the existing repository projections. It is used by the
// public observability endpoint and is intentionally a read-only operation.
func Collect(ctx context.Context, store repository.TransitionStore, value *episode.CommerceEpisode, execution *repository.EpisodeExecutionStatus, approval *recovery.ParentApprovalRequest, decision *recovery.ParentDecisionFact, artifact *invocation.DeliveryArtifact, validation *invocation.ValidationEvidence) (EpisodeTrace, error) {
	if store == nil || value == nil {
		return EpisodeTrace{}, repository.ErrRepositoryUnavailable
	}
	events, err := store.ListByEpisode(ctx, value.EpisodeID)
	if err != nil {
		return EpisodeTrace{}, err
	}
	result := EpisodeTrace{SchemaVersion: SchemaVersion, Source: "runtime_api", SensitiveFieldsOmitted: true, Episode: value, Events: events, Execution: execution, ParentApproval: approval, ParentDecision: decision, Artifact: artifact, Validation: validation}
	if ledgerStore, ok := store.(repository.LedgerRepository); ok {
		result.Ledger, err = ledgerStore.ListLedgerEntries(ctx, value.EpisodeID)
		if err != nil {
			return EpisodeTrace{}, err
		}
	}
	if paymentStore, ok := store.(repository.PaymentIntentRepository); ok {
		result.PaymentIntents, err = paymentStore.ListPaymentIntents(ctx, value.EpisodeID)
		if err != nil {
			return EpisodeTrace{}, err
		}
	}
	if traceStore, ok := store.(repository.ModelDecisionTraceLister); ok {
		result.ModelDecisionTraces, err = traceStore.ListModelDecisionTraces(ctx, value.EpisodeID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return EpisodeTrace{}, err
		}
	}
	if memoryStore, ok := store.(memory.MemoryUseTraceStore); ok {
		result.MemoryUseTraces, err = memoryStore.ListMemoryUseTraces(ctx, value.EpisodeID)
		if err != nil && !errors.Is(err, repository.ErrNotFound) {
			return EpisodeTrace{}, err
		}
	}
	return result, nil
}
