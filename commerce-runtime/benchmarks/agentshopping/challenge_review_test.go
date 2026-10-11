package agentshopping

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCreateChallengeReviewerPromptPacketsStripMetadataAndBindContent(t *testing.T) {
	master := []ChallengePromptRecord{
		{TaskID: "task-001", Scenario: "S01", Prompt: "Buy coffee beans under 8 USDC.", PromptAuthorID: "author-a",
			PromptWrittenWithoutGeneratedTemplate: true, ModelBlind: true, BuyerDefaultRegion: "CN-EAST", BuyerBudgetMinor: 1000},
		{TaskID: "task-002", Scenario: "S08", Prompt: "Find a product under 1 USDC.", PromptAuthorID: "author-b",
			PromptWrittenWithoutGeneratedTemplate: true, ModelBlind: true, BuyerDefaultRegion: "CN-WEST", BuyerBudgetMinor: 100},
		{TaskID: "task-003", Scenario: "S13", Prompt: "I need something suitable.", PromptAuthorID: "author-a",
			PromptWrittenWithoutGeneratedTemplate: true, ModelBlind: true, BuyerDefaultRegion: "CN-EAST", BuyerBudgetMinor: 2000},
	}
	packetA, packetB, err := CreateChallengeReviewerPromptPackets(master)
	if err != nil {
		t.Fatalf("create blinded reviewer packets: %v", err)
	}
	_, reviewerAMap, err := validateChallengeReviewerPromptPacket(master, packetA, "A")
	if err != nil {
		t.Fatal(err)
	}
	_, reviewerBMap, err := validateChallengeReviewerPromptPacket(master, packetB, "B")
	if err != nil {
		t.Fatal(err)
	}
	if err := requireDistinctChallengeReviewerPromptAliases(reviewerAMap, reviewerBMap); err != nil {
		t.Fatalf("generated reviewer aliases overlap: %v", err)
	}
	if err := requireDistinctChallengeReviewerPromptAliases(
		map[string]string{"review-00000000000000000000000000000001": "task-001"},
		map[string]string{"review-00000000000000000000000000000001": "task-001"}); err == nil {
		t.Fatal("reviewer alias reused across packets was accepted")
	}
	sameOrder := true
	for index := range packetA {
		if reviewerAMap[packetA[index].TaskID] != reviewerBMap[packetB[index].TaskID] {
			sameOrder = false
			break
		}
	}
	if sameOrder && len(packetA) > 1 {
		t.Fatal("reviewer packets unexpectedly have the same task order")
	}
	masterIDs := map[string]bool{}
	for _, prompt := range master {
		masterIDs[prompt.TaskID] = true
	}
	packetBIDs := map[string]bool{}
	for _, row := range packetB {
		packetBIDs[row.TaskID] = true
	}
	for name, packet := range map[string][]ChallengeReviewerPromptRecord{"A": packetA, "B": packetB} {
		_, aliases, err := validateChallengeReviewerPromptPacket(master, packet, name)
		if err != nil {
			t.Fatalf("validate reviewer %s packet: %v", name, err)
		}
		if len(aliases) != len(master) {
			t.Fatalf("reviewer %s alias map has %d tasks, want %d", name, len(aliases), len(master))
		}
		for _, row := range packet {
			if !strings.HasPrefix(row.TaskID, "review-") || len(row.TaskID) != len("review-")+32 || masterIDs[row.TaskID] {
				t.Fatalf("reviewer %s packet task ID is not a neutral random alias: %q", name, row.TaskID)
			}
			if name == "A" && packetBIDs[row.TaskID] {
				t.Fatalf("reviewer-specific task alias was reused across packets: %q", row.TaskID)
			}
			encoded, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			if len(fields) != 4 || fields["scenario"] != nil || fields["prompt_author_id"] != nil ||
				fields["task_id"] == nil || fields["prompt"] == nil || fields["buyer_default_region"] == nil || fields["buyer_budget_minor"] == nil {
				t.Fatalf("reviewer %s packet exposes metadata or omits visible context: %s", name, encoded)
			}
		}
	}

	tampered := append([]ChallengeReviewerPromptRecord(nil), packetA...)
	tampered[0].Prompt += " modified"
	if _, _, err := validateChallengeReviewerPromptPacket(master, tampered, "A"); err == nil {
		t.Fatal("review packet with prompt content changed after authoring was accepted")
	}
	missing := append([]ChallengeReviewerPromptRecord(nil), packetA[:len(packetA)-1]...)
	if _, _, err := validateChallengeReviewerPromptPacket(master, missing, "A"); err == nil {
		t.Fatal("incomplete reviewer prompt packet was accepted")
	}
	invalidAlias := append([]ChallengeReviewerPromptRecord(nil), packetA...)
	invalidAlias[0].TaskID = "task-001"
	if _, _, err := validateChallengeReviewerPromptPacket(master, invalidAlias, "A"); err == nil {
		t.Fatal("canonical task ID was accepted in a blinded reviewer packet")
	}
	duplicateContext := append([]ChallengePromptRecord(nil), master...)
	duplicateContext[1].Prompt = duplicateContext[0].Prompt
	duplicateContext[1].BuyerDefaultRegion = duplicateContext[0].BuyerDefaultRegion
	duplicateContext[1].BuyerBudgetMinor = duplicateContext[0].BuyerBudgetMinor
	if _, _, err := CreateChallengeReviewerPromptPackets(duplicateContext); err == nil {
		t.Fatal("unambiguously unbindable duplicate prompt context was packetized")
	}
}

func TestFinalizeHumanReviewedChallengeTasksRequiresIndependentReviewAndAdjudicates(t *testing.T) {
	catalog := ImportedCatalog{
		Products: []Product{{SKUID: "beans-1", MerchantID: "merchant-1", Category: "coffee", Item: "beans",
			Format: "bag", Currency: "USDC", Status: "active", Attributes: map[string]any{"roast_level": "light"}}},
		Inventory: []Inventory{{TenantID: SimulationTenantID, MerchantID: "merchant-1", SKUID: "beans-1",
			Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 500, Revision: 1}},
	}
	intent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", PreferredStyle: "floral",
		RequiredAttributes: map[string]string{"roast_level": "light"}, Region: "CN-EAST", MaxPriceMinor: 800,
		ConfirmationNeeded: true}
	labelA := ChallengeReviewLabel{ExpectedDisposition: ChallengePlan, Intent: intent,
		GoldSKUs: []string{"beans-1"}, RelevantSKUs: []string{"beans-1"}, AcceptedSubstituteSKUs: []string{}}
	labelB := labelA
	prompts := []ChallengePromptRecord{{TaskID: "human-001", Scenario: "STANDARD", Prompt: "我想买一袋浅烘咖啡豆，预算 8 USDC。",
		PromptAuthorID: "author-01", PromptWrittenWithoutGeneratedTemplate: true, ModelBlind: true,
		BuyerDefaultRegion: "CN-EAST", BuyerBudgetMinor: 1000}}
	reviewerA := []ChallengeReviewAnnotation{{TaskID: "human-001", ReviewerID: "reviewer-01", ModelBlind: true,
		LabelsFrozenBeforeModelRun: true, Label: labelA}}
	reviewerB := []ChallengeReviewAnnotation{{TaskID: "human-001", ReviewerID: "reviewer-02", ModelBlind: true,
		LabelsFrozenBeforeModelRun: true, Label: labelB}}
	manifestBytes := []byte("pinned local merchant manifest")

	tasks, audit, err := FinalizeHumanReviewedChallengeTasks(catalog, manifestBytes, prompts, reviewerA, reviewerB, nil)
	if err != nil {
		t.Fatalf("finalize agreeing annotations: %v", err)
	}
	if len(tasks) != 1 || audit.TaskCount != 1 || audit.AgreementTaskCount != 1 || audit.AdjudicatedTaskCount != 0 ||
		audit.ReviewerIDs != [2]string{"reviewer-01", "reviewer-02"} || tasks[0].Provenance.GoldLabelReviewStatus != ChallengeGoldReviewAgreement {
		t.Fatalf("unexpected agreement result: tasks=%+v audit=%+v", tasks, audit)
	}
	if audit.ScenarioTaskCounts["STANDARD"] != 1 || audit.DispositionTaskCounts[ChallengePlan] != 1 {
		t.Fatalf("single-task mix counts are wrong: scenarios=%v dispositions=%v", audit.ScenarioTaskCounts, audit.DispositionTaskCounts)
	}
	if audit.Agreement.CompleteLabels.ExactMatchRate != 1 || audit.Agreement.Disposition.ExactMatchRate != 1 ||
		audit.Agreement.Disposition.CohenKappa != nil || audit.Agreement.Disposition.CohenKappaStatus != "UNDEFINED_DEGENERATE_MARGINALS" ||
		audit.Agreement.Intent.ExactMatchRate != 1 || audit.Agreement.FeasibleGoldSKUs.MacroJaccard != 1 ||
		audit.Agreement.RelevantSKUs.MicroJaccard != 1 || audit.Agreement.AcceptedSubstituteSKUs.MicroJaccard != 1 {
		t.Fatalf("single-class reviewer agreement metrics are wrong: %+v", audit.Agreement)
	}
	if err := ValidateChallengeTaskProvenance(tasks, audit.Hashes); err != nil {
		t.Fatalf("finalized review provenance did not validate: %v", err)
	}
	reviewedReport, _, err := EvaluateChallengeTasks(catalog, tasks,
		[]BatchIntentResult{{TaskID: tasks[0].TaskID, Intent: intent}}, 20)
	if err != nil {
		t.Fatalf("evaluate finalized human-review task: %v", err)
	}
	if reviewedReport.TargetProductRecallAtK.Status != "AVAILABLE_DECLARED_DOUBLE_REVIEW_NOT_EXTERNALLY_VERIFIED" ||
		reviewedReport.TargetProductRecallAtK.DeclaredDoubleReviewedTaskCount != 1 ||
		reviewedReport.TargetProductRecallAtK.UnverifiedLabeledPlanTaskCount != 0 ||
		reviewedReport.TargetProductTaskHitAtK.Status != reviewedReport.TargetProductRecallAtK.Status {
		t.Fatalf("human-review declaration was not distinguished in recall status: %+v", reviewedReport.TargetProductRecallAtK)
	}
	mismatchedSnapshotTask := tasks[0]
	mismatchedProvenance := *tasks[0].Provenance
	mismatchedProvenance.ReviewInventorySHA256 = "sha256:" + strings.Repeat("0", 64)
	mismatchedSnapshotTask.Provenance = &mismatchedProvenance
	mismatchedSnapshotReport, _, err := EvaluateChallengeTasks(catalog, []ChallengeTask{mismatchedSnapshotTask},
		[]BatchIntentResult{{TaskID: mismatchedSnapshotTask.TaskID, Intent: intent}}, 20)
	if err != nil {
		t.Fatalf("evaluate label with mismatched review snapshot as diagnostic: %v", err)
	}
	if mismatchedSnapshotReport.TargetProductRecallAtK.Status != "AVAILABLE_UNVERIFIED_RELEVANCE_LABELS" ||
		mismatchedSnapshotReport.TargetProductRecallAtK.UnverifiedLabeledPlanTaskCount != 1 {
		t.Fatalf("snapshot-mismatched labels were credited as double-reviewed: %+v", mismatchedSnapshotReport.TargetProductRecallAtK)
	}
	if !validSHA256Digest(audit.PromptRecordsSHA256) || !validSHA256Digest(audit.ReviewerAPromptPacketSHA256) ||
		!validSHA256Digest(audit.ReviewerBPromptPacketSHA256) || !validSHA256Digest(audit.ReviewerAAnnotationsSHA256) ||
		!validSHA256Digest(audit.ReviewerBAnnotationsSHA256) || !validSHA256Digest(audit.AdjudicationsSHA256) {
		t.Fatalf("review packet hashes are incomplete: %+v", audit)
	}

	reviewerB[0].Label.Intent.MaxPriceMinor = 900
	if _, _, err := FinalizeHumanReviewedChallengeTasks(catalog, manifestBytes, prompts, reviewerA, reviewerB, nil); err == nil {
		t.Fatal("reviewer disagreement without adjudication was accepted")
	}
	adjudications := []ChallengeAdjudicationRecord{{TaskID: "human-001", AdjudicatorID: "adjudicator-01",
		FinalLabel: labelA, Rationale: "The prompt explicitly caps the buyer's budget at 8 USDC."}}
	resolved, resolvedAudit, err := FinalizeHumanReviewedChallengeTasks(catalog, manifestBytes,
		prompts, reviewerA, reviewerB, adjudications)
	if err != nil {
		t.Fatalf("finalize adjudicated disagreement: %v", err)
	}
	if resolvedAudit.AgreementTaskCount != 0 || resolvedAudit.AdjudicatedTaskCount != 1 ||
		resolved[0].Provenance.GoldLabelReviewStatus != ChallengeGoldReviewAdjudicated ||
		resolved[0].Provenance.GoldAdjudicatorID != "adjudicator-01" || resolved[0].Intent.MaxPriceMinor != 800 {
		t.Fatalf("adjudication was not reflected in final tasks: task=%+v audit=%+v", resolved[0], resolvedAudit)
	}
	missingAdjudicatorSubstitutes := append([]ChallengeAdjudicationRecord(nil), adjudications...)
	missingAdjudicatorSubstitutes[0].FinalLabel.AcceptedSubstituteSKUs = nil
	if _, _, err := FinalizeHumanReviewedChallengeTasks(catalog, manifestBytes,
		prompts, reviewerA, reviewerB, missingAdjudicatorSubstitutes); err == nil {
		t.Fatal("adjudication without an explicit accepted-substitute set was accepted")
	}
	if err := ValidateChallengeTaskProvenance(resolved, resolvedAudit.Hashes); err != nil {
		t.Fatalf("adjudicated provenance did not validate: %v", err)
	}
	if _, _, err := FinalizeHumanReviewedChallengeTasks(catalog, manifestBytes,
		prompts, reviewerA, reviewerB, append(adjudications, ChallengeAdjudicationRecord{TaskID: "unknown-task",
			AdjudicatorID: "adjudicator-02", FinalLabel: labelA, Rationale: "not part of suite"})); err == nil {
		t.Fatal("unbound adjudication record was accepted")
	}
}

func TestFinalizeHumanReviewedChallengeTasksReportsDimensionSpecificAgreement(t *testing.T) {
	catalog := ImportedCatalog{
		Products: []Product{
			{SKUID: "beans-1", MerchantID: "merchant-1", Category: "coffee", Item: "beans", Format: "bag", Currency: "USDC", Status: "active"},
			{SKUID: "beans-2", MerchantID: "merchant-2", Category: "coffee", Item: "beans", Format: "bag", Currency: "USDC", Status: "active"},
		},
		Inventory: []Inventory{
			{TenantID: SimulationTenantID, MerchantID: "merchant-1", SKUID: "beans-1", Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 500, Revision: 1},
			{TenantID: SimulationTenantID, MerchantID: "merchant-2", SKUID: "beans-2", Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 500, Revision: 1},
		},
	}
	planIntent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", Region: "CN-EAST", MaxPriceMinor: 1000, ConfirmationNeeded: true}
	planA := ChallengeReviewLabel{ExpectedDisposition: ChallengePlan, Intent: planIntent,
		GoldSKUs: []string{"beans-1"}, RelevantSKUs: []string{"beans-1", "beans-2"}, AcceptedSubstituteSKUs: []string{}}
	planB := planA
	planB.Intent.MaxPriceMinor = 900
	planB.RelevantSKUs = []string{"beans-1"}
	noPlan := ChallengeReviewLabel{ExpectedDisposition: ChallengeNoPlan,
		Intent:                 Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", Region: "CN-EAST", MaxPriceMinor: 400, ConfirmationNeeded: true},
		AcceptedSubstituteSKUs: []string{}}
	prompts := []ChallengePromptRecord{
		{TaskID: "human-001", Scenario: "PLAN", Prompt: "Find beans within 10 USDC.", PromptAuthorID: "author-01", PromptWrittenWithoutGeneratedTemplate: true, ModelBlind: true, BuyerDefaultRegion: "CN-EAST", BuyerBudgetMinor: 1000},
		{TaskID: "human-002", Scenario: " plan ", Prompt: "Find beans within 10 USDC, second case.", PromptAuthorID: "author-01", PromptWrittenWithoutGeneratedTemplate: true, ModelBlind: true, BuyerDefaultRegion: "CN-EAST", BuyerBudgetMinor: 1000},
		{TaskID: "human-003", Scenario: "no_plan", Prompt: "Find beans under 4 USDC.", PromptAuthorID: "author-01", PromptWrittenWithoutGeneratedTemplate: true, ModelBlind: true, BuyerDefaultRegion: "CN-EAST", BuyerBudgetMinor: 400},
	}
	makeReviewer := func(id string, labels ...ChallengeReviewLabel) []ChallengeReviewAnnotation {
		annotations := make([]ChallengeReviewAnnotation, len(labels))
		for index, label := range labels {
			annotations[index] = ChallengeReviewAnnotation{TaskID: prompts[index].TaskID, ReviewerID: id,
				ModelBlind: true, LabelsFrozenBeforeModelRun: true, Label: label}
		}
		return annotations
	}
	reviewerA := makeReviewer("reviewer-01", planA, planA, noPlan)
	reviewerB := makeReviewer("reviewer-02", planA, planB, noPlan)
	adjudications := []ChallengeAdjudicationRecord{{TaskID: "human-002", AdjudicatorID: "adjudicator-01",
		FinalLabel: planA, Rationale: "The gold budget is the explicitly stated buyer cap."}}

	_, audit, err := FinalizeHumanReviewedChallengeTasks(catalog, []byte("pinned manifest"), prompts,
		reviewerA, reviewerB, adjudications)
	if err != nil {
		t.Fatalf("finalize mixed agreement packet: %v", err)
	}
	if audit.ScenarioTaskCounts["PLAN"] != 2 || audit.ScenarioTaskCounts["NO_PLAN"] != 1 ||
		audit.DispositionTaskCounts[ChallengePlan] != 2 || audit.DispositionTaskCounts[ChallengeNoPlan] != 1 {
		t.Fatalf("mixed-suite task counts are wrong: scenarios=%v dispositions=%v", audit.ScenarioTaskCounts, audit.DispositionTaskCounts)
	}
	agreement := audit.Agreement
	if agreement.CompleteLabels.ComparedTaskCount != 3 || agreement.CompleteLabels.ExactMatchCount != 2 || agreement.CompleteLabels.ExactMatchRate != 2.0/3.0 {
		t.Fatalf("complete-label agreement mismatch: %+v", agreement.CompleteLabels)
	}
	if agreement.Disposition.ExactMatchCount != 3 || agreement.Disposition.ExactMatchRate != 1 ||
		agreement.Disposition.CohenKappaStatus != "DEFINED" || agreement.Disposition.CohenKappa == nil || *agreement.Disposition.CohenKappa != 1 {
		t.Fatalf("disposition agreement mismatch: %+v", agreement.Disposition)
	}
	if agreement.Intent.ExactMatchCount != 2 || agreement.Intent.ExactMatchRate != 2.0/3.0 {
		t.Fatalf("intent agreement mismatch: %+v", agreement.Intent)
	}
	if agreement.FeasibleGoldSKUs.ComparedTaskCount != 2 || agreement.FeasibleGoldSKUs.ExactMatchRate != 1 ||
		agreement.RelevantSKUs.ComparedTaskCount != 2 || agreement.RelevantSKUs.ExactMatchCount != 1 ||
		agreement.RelevantSKUs.MacroJaccard != 0.75 || agreement.RelevantSKUs.MicroJaccard != 0.75 {
		t.Fatalf("SKU agreement mismatch: gold=%+v relevant=%+v", agreement.FeasibleGoldSKUs, agreement.RelevantSKUs)
	}
	if agreement.AcceptedSubstituteSKUs.ComparedTaskCount != 3 || agreement.AcceptedSubstituteSKUs.ExactMatchRate != 1 {
		t.Fatalf("accepted-substitute agreement should include explicit empty sets: %+v", agreement.AcceptedSubstituteSKUs)
	}
	if agreement.RequiredMissingFields.ComparedTaskCount != 0 {
		t.Fatalf("missing-field agreement should have an explicit zero denominator: %+v", agreement.RequiredMissingFields)
	}
}

func TestFinalizeHumanReviewedChallengeTasksRejectsIdentityAndBlindnessViolations(t *testing.T) {
	catalog := ImportedCatalog{
		Products: []Product{{SKUID: "beans-1", MerchantID: "merchant-1", Category: "coffee", Item: "beans",
			Format: "bag", Currency: "USDC", Status: "active"}},
		Inventory: []Inventory{{TenantID: SimulationTenantID, MerchantID: "merchant-1", SKUID: "beans-1",
			Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 500, Revision: 1}},
	}
	label := ChallengeReviewLabel{ExpectedDisposition: ChallengePlan,
		Intent: Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", Region: "CN-EAST",
			MaxPriceMinor: 800, ConfirmationNeeded: true}, GoldSKUs: []string{"beans-1"}, RelevantSKUs: []string{"beans-1"},
		AcceptedSubstituteSKUs: []string{}}
	prompts := []ChallengePromptRecord{{TaskID: "human-001", Scenario: "STANDARD", Prompt: "Buy a bag of beans.",
		PromptAuthorID: "author-01", PromptWrittenWithoutGeneratedTemplate: true, ModelBlind: true,
		BuyerDefaultRegion: "CN-EAST", BuyerBudgetMinor: 1000}}
	reviewerA := []ChallengeReviewAnnotation{{TaskID: "human-001", ReviewerID: "reviewer-01", ModelBlind: true,
		LabelsFrozenBeforeModelRun: true, Label: label}}
	reviewerB := []ChallengeReviewAnnotation{{TaskID: "human-001", ReviewerID: "reviewer-02", ModelBlind: true,
		LabelsFrozenBeforeModelRun: true, Label: label}}
	manifestBytes := []byte("manifest")

	badPrompt := append([]ChallengePromptRecord(nil), prompts...)
	badPrompt[0].ModelBlind = false
	if _, _, err := FinalizeHumanReviewedChallengeTasks(catalog, manifestBytes, badPrompt, reviewerA, reviewerB, nil); err == nil {
		t.Fatal("prompt author model-blind declaration was not enforced")
	}
	badReviewer := append([]ChallengeReviewAnnotation(nil), reviewerB...)
	badReviewer[0].ReviewerID = "reviewer-01"
	if _, _, err := FinalizeHumanReviewedChallengeTasks(catalog, manifestBytes, prompts, reviewerA, badReviewer, nil); err == nil {
		t.Fatal("duplicate reviewer pseudonym was accepted")
	}
	badAuthor := append([]ChallengePromptRecord(nil), prompts...)
	badAuthor[0].PromptAuthorID = "reviewer-01"
	if _, _, err := FinalizeHumanReviewedChallengeTasks(catalog, manifestBytes, badAuthor, reviewerA, reviewerB, nil); err == nil {
		t.Fatal("reviewer who also authored the prompt was accepted")
	}
	badFrozen := append([]ChallengeReviewAnnotation(nil), reviewerA...)
	badFrozen[0].LabelsFrozenBeforeModelRun = false
	if _, _, err := FinalizeHumanReviewedChallengeTasks(catalog, manifestBytes, prompts, badFrozen, reviewerB, nil); err == nil {
		t.Fatal("review labels not frozen before model run were accepted")
	}
	missingSubstituteDecision := append([]ChallengeReviewAnnotation(nil), reviewerA...)
	missingSubstituteDecision[0].Label.AcceptedSubstituteSKUs = nil
	if _, _, err := FinalizeHumanReviewedChallengeTasks(catalog, manifestBytes, prompts, missingSubstituteDecision, reviewerB, nil); err == nil {
		t.Fatal("plan annotation without an explicit accepted-substitute set was accepted")
	}
}

func TestFinalizeHumanReviewedChallengeTasksRejectsInvalidRawReviewerLabelBeforeAdjudication(t *testing.T) {
	catalog := ImportedCatalog{
		Products: []Product{{SKUID: "beans-1", MerchantID: "merchant-1", Category: "coffee", Item: "beans",
			Format: "bag", Currency: "USDC", Status: "active"}},
		Inventory: []Inventory{{TenantID: SimulationTenantID, MerchantID: "merchant-1", SKUID: "beans-1",
			Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 500, Revision: 1}},
	}
	intent := Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", Region: "CN-EAST",
		MaxPriceMinor: 800, ConfirmationNeeded: true}
	validLabel := ChallengeReviewLabel{ExpectedDisposition: ChallengePlan, Intent: intent,
		GoldSKUs: []string{"beans-1"}, RelevantSKUs: []string{"beans-1"}, AcceptedSubstituteSKUs: []string{}}
	invalidRawLabel := validLabel
	invalidRawLabel.RelevantSKUs = []string{"not-in-the-pinned-catalog"}
	prompts := []ChallengePromptRecord{{TaskID: "human-raw-invalid-001", Scenario: "STANDARD",
		Prompt: "Buy a bag of beans under 8 USDC.", PromptAuthorID: "author-01",
		PromptWrittenWithoutGeneratedTemplate: true, ModelBlind: true,
		BuyerDefaultRegion: "CN-EAST", BuyerBudgetMinor: 1000}}
	reviewerA := []ChallengeReviewAnnotation{{TaskID: prompts[0].TaskID, ReviewerID: "reviewer-01",
		ModelBlind: true, LabelsFrozenBeforeModelRun: true, Label: invalidRawLabel}}
	reviewerB := []ChallengeReviewAnnotation{{TaskID: prompts[0].TaskID, ReviewerID: "reviewer-02",
		ModelBlind: true, LabelsFrozenBeforeModelRun: true, Label: validLabel}}
	adjudications := []ChallengeAdjudicationRecord{{TaskID: prompts[0].TaskID,
		AdjudicatorID: "adjudicator-01", FinalLabel: validLabel,
		Rationale: "The exact target SKU is the only listing matching the prompt."}}

	if _, _, err := FinalizeHumanReviewedChallengeTasks(catalog, []byte("manifest"),
		prompts, reviewerA, reviewerB, adjudications); err == nil {
		t.Fatal("invalid raw reviewer label was hidden by a valid adjudicated final label")
	}
}

func TestFinalizeHumanReviewedChallengeTasksRejectsExactPromptContextDuplicates(t *testing.T) {
	catalog := ImportedCatalog{
		Products: []Product{{SKUID: "beans-1", MerchantID: "merchant-1", Category: "coffee", Item: "beans",
			Format: "bag", Currency: "USDC", Status: "active"}},
		Inventory: []Inventory{{TenantID: SimulationTenantID, MerchantID: "merchant-1", SKUID: "beans-1",
			Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 500, Revision: 1}},
	}
	label := ChallengeReviewLabel{ExpectedDisposition: ChallengePlan,
		Intent: Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", Region: "CN-EAST",
			MaxPriceMinor: 800, ConfirmationNeeded: true},
		GoldSKUs: []string{"beans-1"}, RelevantSKUs: []string{"beans-1"}, AcceptedSubstituteSKUs: []string{}}
	prompts := []ChallengePromptRecord{
		{TaskID: "human-001", Scenario: "STANDARD", Prompt: "Buy a bag of beans.",
			PromptAuthorID: "author-01", PromptWrittenWithoutGeneratedTemplate: true, ModelBlind: true,
			BuyerDefaultRegion: "CN-EAST", BuyerBudgetMinor: 1000},
		{TaskID: "human-002", Scenario: "STANDARD", Prompt: "  buy   a bag of beans.\n",
			PromptAuthorID: "author-01", PromptWrittenWithoutGeneratedTemplate: true, ModelBlind: true,
			BuyerDefaultRegion: "CN-EAST", BuyerBudgetMinor: 1000},
	}
	makeReviews := func(reviewerID string) []ChallengeReviewAnnotation {
		return []ChallengeReviewAnnotation{
			{TaskID: prompts[0].TaskID, ReviewerID: reviewerID, ModelBlind: true, LabelsFrozenBeforeModelRun: true, Label: label},
			{TaskID: prompts[1].TaskID, ReviewerID: reviewerID, ModelBlind: true, LabelsFrozenBeforeModelRun: true, Label: label},
		}
	}
	if _, _, err := FinalizeHumanReviewedChallengeTasks(catalog, []byte("manifest"), prompts,
		makeReviews("reviewer-01"), makeReviews("reviewer-02"), nil); err == nil {
		t.Fatal("duplicate normalized prompt/context pair was accepted as an independent task")
	}
}
