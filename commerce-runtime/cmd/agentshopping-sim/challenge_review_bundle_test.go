package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stablepay/commerce-runtime/benchmarks/agentshopping"
)

func TestVerifyChallengeReviewBundleRecomputesAllInputsAndFinalTasks(t *testing.T) {
	catalog := agentshopping.ImportedCatalog{
		Merchants: []agentshopping.MerchantRecord{{TenantID: agentshopping.SimulationTenantID,
			MerchantID: "merchant-1", Name: "Synthetic Merchant", DID: "did:sim:private-do-not-export"}},
		Products: []agentshopping.Product{{SKUID: "beans-1", MerchantID: "merchant-1", Category: "coffee", Title: "Coffee beans", Item: "beans",
			Format: "bag", Currency: "USDC", Status: "active", SubstitutionGroup: "internal-policy-secret"}},
		Inventory: []agentshopping.Inventory{{TenantID: agentshopping.SimulationTenantID, MerchantID: "merchant-1", SKUID: "beans-1",
			Region: "CN-EAST", OnHand: 1, Eligible: true, PriceMinor: 500, Revision: 1}},
	}
	manifestBytes := []byte("pinned source manifest")
	intent := agentshopping.Intent{Category: "coffee", Item: "beans", RequiredFormat: "bag", Region: "CN-EAST",
		MaxPriceMinor: 800, ConfirmationNeeded: true}
	prompts := []agentshopping.ChallengePromptRecord{{TaskID: "human-001", Scenario: "STANDARD", Prompt: "Buy a bag of beans, max 8 USDC.",
		PromptAuthorID: "author-01", PromptWrittenWithoutGeneratedTemplate: true, ModelBlind: true,
		BuyerDefaultRegion: "CN-EAST", BuyerBudgetMinor: 1000}}
	label := agentshopping.ChallengeReviewLabel{ExpectedDisposition: agentshopping.ChallengePlan, Intent: intent,
		GoldSKUs: []string{"beans-1"}, RelevantSKUs: []string{"beans-1"}, AcceptedSubstituteSKUs: []string{}}
	reviewerA := []agentshopping.ChallengeReviewAnnotation{{TaskID: "human-001", ReviewerID: "reviewer-01", ModelBlind: true,
		LabelsFrozenBeforeModelRun: true, Label: label}}
	reviewerB := []agentshopping.ChallengeReviewAnnotation{{TaskID: "human-001", ReviewerID: "reviewer-02", ModelBlind: true,
		LabelsFrozenBeforeModelRun: true, Label: label}}
	reviewerAPrompts, reviewerBPrompts, err := agentshopping.CreateChallengeReviewerPromptPackets(prompts)
	if err != nil {
		t.Fatalf("prepare reviewer prompt packets: %v", err)
	}
	reviewerA[0].TaskID = reviewerAPrompts[0].TaskID
	reviewerB[0].TaskID = reviewerBPrompts[0].TaskID
	tasks, audit, err := agentshopping.FinalizeHumanReviewedChallengeTasksWithPackets(catalog, manifestBytes,
		prompts, reviewerAPrompts, reviewerBPrompts, reviewerA, reviewerB, nil)
	if err != nil {
		t.Fatalf("build fixture review bundle: %v", err)
	}
	bundleDir := filepath.Join(t.TempDir(), "bundle")
	if err := os.MkdirAll(bundleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLines(filepath.Join(bundleDir, "prompts.jsonl"), prompts); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLines(filepath.Join(bundleDir, "reviewer-a-prompts.jsonl"), reviewerAPrompts); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLines(filepath.Join(bundleDir, "reviewer-b-prompts.jsonl"), reviewerBPrompts); err != nil {
		t.Fatal(err)
	}
	reviewerCatalogBytes, err := challengeReviewerCatalogJSONLines(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "reviewer-catalog.jsonl"), reviewerCatalogBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	masterHash, err := hashFileSHA256(filepath.Join(bundleDir, "prompts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	reviewerAHash, err := hashFileSHA256(filepath.Join(bundleDir, "reviewer-a-prompts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	reviewerBHash, err := hashFileSHA256(filepath.Join(bundleDir, "reviewer-b-prompts.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	reviewerCatalogHash, err := hashFileSHA256(filepath.Join(bundleDir, "reviewer-catalog.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	catalogRecords, err := buildChallengeReviewerCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	catalogHashes, err := agentshopping.ComputeChallengeHashes(catalog, nil, manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	offerCount := 0
	for _, record := range catalogRecords {
		offerCount += len(record.Offers)
	}
	packetManifest := buildChallengeReviewerPacketManifest(masterHash, reviewerAHash, reviewerBHash,
		reviewerCatalogHash, len(prompts), len(catalogRecords), offerCount, catalogHashes)
	if err := writeJSONFile(filepath.Join(bundleDir, "packet-manifest.json"), packetManifest); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLines(filepath.Join(bundleDir, "reviewer-a.jsonl"), reviewerA); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLines(filepath.Join(bundleDir, "reviewer-b.jsonl"), reviewerB); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLines(filepath.Join(bundleDir, "adjudications.jsonl"), []agentshopping.ChallengeAdjudicationRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLines(filepath.Join(bundleDir, "tasks.jsonl"), tasks); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(filepath.Join(bundleDir, "review-audit.json"), audit); err != nil {
		t.Fatal(err)
	}
	verifiedAudit, digest, err := verifyChallengeReviewBundle(bundleDir, filepath.Join(bundleDir, "tasks.jsonl"), catalog, manifestBytes)
	if err != nil || verifiedAudit.Hashes != audit.Hashes || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("valid bundle should recompute: audit=%+v digest=%q err=%v", verifiedAudit, digest, err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "reviewer-catalog.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := verifyChallengeReviewBundle(bundleDir, filepath.Join(bundleDir, "tasks.jsonl"), catalog, manifestBytes); err == nil {
		t.Fatal("review bundle accepted a reviewer catalog that differs from the pinned snapshot")
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "reviewer-catalog.jsonl"), reviewerCatalogBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	packetManifestBytes, err := os.ReadFile(filepath.Join(bundleDir, "packet-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "packet-manifest.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := verifyChallengeReviewBundle(bundleDir, filepath.Join(bundleDir, "tasks.jsonl"), catalog, manifestBytes); err == nil {
		t.Fatal("review bundle accepted a packet manifest that does not bind its files")
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "packet-manifest.json"), packetManifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	annotationPath := filepath.Join(bundleDir, "reviewer-a.jsonl")
	if err := os.Remove(annotationPath); err != nil {
		t.Fatal(err)
	}
	mutated := append([]agentshopping.ChallengeReviewAnnotation(nil), reviewerA...)
	mutated[0].LabelsFrozenBeforeModelRun = false
	if err := writeJSONLines(annotationPath, mutated); err != nil {
		t.Fatal(err)
	}
	if _, _, err := verifyChallengeReviewBundle(bundleDir, filepath.Join(bundleDir, "tasks.jsonl"), catalog, manifestBytes); err == nil {
		t.Fatal("review bundle accepted a changed annotation stream")
	}
	if err := os.Remove(annotationPath); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLines(annotationPath, reviewerA); err != nil {
		t.Fatal(err)
	}
	packetPath := filepath.Join(bundleDir, "reviewer-a-prompts.jsonl")
	mutatedPacket := append([]agentshopping.ChallengeReviewerPromptRecord(nil), reviewerAPrompts...)
	mutatedPacket[0].Prompt = "A changed prompt not seen in the master file."
	if err := os.Remove(packetPath); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLines(packetPath, mutatedPacket); err != nil {
		t.Fatal(err)
	}
	if _, _, err := verifyChallengeReviewBundle(bundleDir, filepath.Join(bundleDir, "tasks.jsonl"), catalog, manifestBytes); err == nil {
		t.Fatal("review bundle accepted a changed metadata-stripped prompt packet")
	}
	if err := os.Remove(packetPath); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLines(packetPath, reviewerAPrompts); err != nil {
		t.Fatal(err)
	}
	changedTasks, err := readChallengeTasks(filepath.Join(bundleDir, "tasks.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	changedTasks[0].Scenario = "MUTATED_AFTER_REVIEW"
	if err := os.Remove(filepath.Join(bundleDir, "tasks.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLines(filepath.Join(bundleDir, "tasks.jsonl"), changedTasks); err != nil {
		t.Fatal(err)
	}
	if _, _, err := verifyChallengeReviewBundle(bundleDir, filepath.Join(bundleDir, "tasks.jsonl"), catalog, manifestBytes); err == nil {
		t.Fatal("review bundle accepted finalized tasks that differ from its source annotations")
	}
}

func TestChallengeReviewerCatalogReferenceOmitsInternalMerchantIdentityAndPolicy(t *testing.T) {
	catalog := agentshopping.ImportedCatalog{
		Merchants: []agentshopping.MerchantRecord{{TenantID: agentshopping.SimulationTenantID,
			MerchantID: "merchant-1", Name: "Synthetic Merchant", DID: "did:sim:wallet-secret"}},
		Products: []agentshopping.Product{{SKUID: "beans-1", MerchantID: "merchant-1", Category: "coffee",
			Title: "Coffee beans", Description: "Synthetic listing", Price: "5.00", PriceMinor: 500,
			Currency: "USDC", Status: "active", Item: "beans", Format: "bag", Style: "chocolate",
			SubstitutionGroup: "private-substitution-group"}},
		Inventory: []agentshopping.Inventory{{TenantID: agentshopping.SimulationTenantID, MerchantID: "merchant-1",
			SKUID: "beans-1", Region: "CN-EAST", OnHand: 2, Eligible: true, PriceMinor: 550, Revision: 3}},
	}
	encoded, err := challengeReviewerCatalogJSONLines(catalog)
	if err != nil {
		t.Fatalf("build reviewer catalog: %v", err)
	}
	text := string(encoded)
	for _, withheld := range []string{"wallet-secret", "private-substitution-group", "substitution_group", "\"did\""} {
		if strings.Contains(text, withheld) {
			t.Fatalf("reviewer catalog leaked withheld value %q: %s", withheld, text)
		}
	}
	if !strings.Contains(text, `"price_minor":550`) || !strings.Contains(text, `"in_stock":true`) ||
		!strings.Contains(text, `"listing_price_minor":500`) {
		t.Fatalf("reviewer catalog omitted listing/live-offer facts: %s", text)
	}
}
