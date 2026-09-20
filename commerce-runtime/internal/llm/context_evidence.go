package llm

import (
	"errors"
	"fmt"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/decision"
)

// ErrEvidenceOutsideContext means that a proposal cites a persisted record
// which was not part of the exact context sent to the model. Persistence alone
// never makes evidence authoritative for a proposal.
var ErrEvidenceOutsideContext = errors.New("proposal evidence is outside the decision context")

// ValidateProposalEvidenceAgainstContext is the application handoff boundary
// for model-generated proposals. It deliberately derives the allowlist from
// the immutable context snapshot rather than from a global evidence table.
func ValidateProposalEvidenceAgainstContext(ctx DecisionContext, proposal decision.DecisionProposal) error {
	if err := ctx.Validate(); err != nil {
		return err
	}
	if err := proposal.Validate(); err != nil {
		return err
	}
	known := ContextEvidenceRefs(ctx)
	for _, ref := range proposal.EvidenceRefs {
		ref = strings.TrimSpace(ref)
		if _, ok := known[ref]; !ok {
			return fmt.Errorf("%w: %s", ErrEvidenceOutsideContext, ref)
		}
	}
	return nil
}

// ContextEvidenceRefs returns only references present in the supplied
// DecisionContext. It includes runtime fact hashes/refs and the integrity
// references of retrieved records, but never queries persistence.
func ContextEvidenceRefs(ctx DecisionContext) map[string]struct{} {
	known := make(map[string]struct{})
	add := func(values ...string) {
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				known[value] = struct{}{}
			}
		}
	}
	if ctx.SelectedMerchant != nil {
		add(ctx.SelectedMerchant.CatalogSnapshotRef, ctx.SelectedMerchant.CatalogSnapshotHash)
	}
	add(ctx.CandidateSet.FactsRef, ctx.CandidateSet.PayloadHash)
	for _, candidate := range ctx.CandidateSet.Candidates {
		add(candidate.CatalogSnapshotRef, candidate.CatalogSnapshotHash)
	}
	if ctx.Recovery != nil {
		add(ctx.Recovery.FactsRef, ctx.Recovery.PayloadHash)
	}
	if ctx.LatestValidation != nil {
		add(ctx.LatestValidation.EvidenceRefs...)
		add(ctx.LatestValidation.PayloadHash)
	}
	for _, record := range ctx.RetrievedEvidence {
		add(record.EvidenceRef, record.SourceHash, record.PayloadHash, record.ChunkHash)
	}
	return known
}
