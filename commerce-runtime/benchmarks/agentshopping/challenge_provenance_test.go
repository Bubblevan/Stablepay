package agentshopping

import (
	"strings"
	"testing"
)

func TestChallengeTaskProvenanceRequiresIndependentAuthorsAndSnapshotBoundLabels(t *testing.T) {
	hashes := ChallengeEvaluationHashes{
		CatalogSHA256:   "sha256:" + strings.Repeat("e", 64),
		InventorySHA256: "sha256:" + strings.Repeat("f", 64),
	}
	task := ChallengeTask{TaskID: "human-001", ExpectedDisposition: ChallengePlan, Provenance: &ChallengeTaskProvenance{
		PromptSource: ChallengePromptSourceHumanAuthored, PromptAuthorID: "author-01",
		PromptWrittenWithoutGeneratedTemplate: true, AcceptedSubstituteLabelsExplicit: true,
		GoldLabelReviewStatus: ChallengeGoldReviewAdjudicated,
		GoldReviewerIDs:       []string{"reviewer-01", "reviewer-02"}, GoldAdjudicatorID: "adjudicator-01",
		ReviewCatalogSHA256: hashes.CatalogSHA256, ReviewInventorySHA256: hashes.InventorySHA256,
		PromptRecordsSHA256:        "sha256:" + strings.Repeat("a", 64),
		ReviewerAAnnotationsSHA256: "sha256:" + strings.Repeat("b", 64),
		ReviewerBAnnotationsSHA256: "sha256:" + strings.Repeat("c", 64),
		AdjudicationsSHA256:        "sha256:" + strings.Repeat("d", 64),
		ModelBlind:                 true, LabelsFrozenBeforeModelRun: true,
	}}
	if err := ValidateChallengeTaskProvenance([]ChallengeTask{task}, hashes); err != nil {
		t.Fatalf("expected complete provenance declarations to pass: %v", err)
	}
	if got := ChallengeTaskProvenanceStatus([]ChallengeTask{task}, hashes); got != ChallengeProvenanceDeclared {
		t.Fatalf("unexpected provenance status %q", got)
	}
	agreementTask := task
	agreementProvenance := *task.Provenance
	agreementProvenance.GoldLabelReviewStatus = ChallengeGoldReviewAgreement
	agreementProvenance.GoldAdjudicatorID = ""
	agreementTask.Provenance = &agreementProvenance
	if err := ValidateChallengeTaskProvenance([]ChallengeTask{agreementTask}, hashes); err != nil {
		t.Fatalf("independent reviewer agreement should not require a third adjudicator: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*ChallengeTaskProvenance)
	}{
		{name: "snapshot mismatch", mutate: func(p *ChallengeTaskProvenance) { p.ReviewInventorySHA256 = "sha256:other" }},
		{name: "reviewer is author", mutate: func(p *ChallengeTaskProvenance) { p.GoldReviewerIDs[0] = p.PromptAuthorID }},
		{name: "not model blind", mutate: func(p *ChallengeTaskProvenance) { p.ModelBlind = false }},
		{name: "missing accepted-substitute decision", mutate: func(p *ChallengeTaskProvenance) { p.AcceptedSubstituteLabelsExplicit = false }},
		{name: "one reviewer", mutate: func(p *ChallengeTaskProvenance) { p.GoldReviewerIDs = []string{"reviewer-01"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copyTask := task
			copyProvenance := *task.Provenance
			copyProvenance.GoldReviewerIDs = append([]string(nil), task.Provenance.GoldReviewerIDs...)
			test.mutate(&copyProvenance)
			copyTask.Provenance = &copyProvenance
			if err := ValidateChallengeTaskProvenance([]ChallengeTask{copyTask}, hashes); err == nil {
				t.Fatal("expected provenance gate to reject declaration")
			}
			if got := ChallengeTaskProvenanceStatus([]ChallengeTask{copyTask}, hashes); got != ChallengeProvenanceUnverified {
				t.Fatalf("unexpected provenance status %q", got)
			}
		})
	}
}

func TestChallengeTaskProvenanceMissingDeclarationRemainsUnverified(t *testing.T) {
	task := ChallengeTask{TaskID: "generated-001"}
	if err := ValidateChallengeTaskProvenance([]ChallengeTask{task}, ChallengeEvaluationHashes{}); err == nil {
		t.Fatal("expected missing provenance to fail")
	}
	if got := ChallengeTaskProvenanceStatus([]ChallengeTask{task}, ChallengeEvaluationHashes{}); got != ChallengeProvenanceUnverified {
		t.Fatalf("unexpected provenance status %q", got)
	}
}

func TestHumanChallengeTaskRequiresIndependentBuyerContext(t *testing.T) {
	task := ChallengeTask{TaskID: "human-context", Provenance: &ChallengeTaskProvenance{},
		Intent: Intent{Region: "CN-EAST", MaxPriceMinor: 500}}
	if err := validateHumanTaskContext(task); err == nil {
		t.Fatal("human-reviewed task must not derive parser context from its gold intent")
	}
	task.BuyerDefaultRegion = "CN-WEST"
	task.BuyerBudgetMinor = 900
	if err := validateHumanTaskContext(task); err != nil {
		t.Fatalf("independent buyer context should be accepted: %v", err)
	}
	task.BuyerBudgetMinor = 400
	if err := validateHumanTaskContext(task); err == nil {
		t.Fatal("gold intent budget must not exceed the declared buyer budget cap")
	}
}
