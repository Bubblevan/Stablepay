package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/benchmarks/agentshopping"
)

type challengeReviewerPacketManifest struct {
	SchemaVersion             string   `json:"schema_version"`
	Status                    string   `json:"status"`
	TaskCount                 int      `json:"task_count"`
	MasterPromptFileSHA256    string   `json:"master_prompt_file_sha256"`
	ReviewerAPacketFileSHA256 string   `json:"reviewer_a_packet_file_sha256"`
	ReviewerBPacketFileSHA256 string   `json:"reviewer_b_packet_file_sha256"`
	ReviewerCatalogSHA256     string   `json:"reviewer_catalog_file_sha256"`
	CatalogProducts           int      `json:"catalog_products"`
	CatalogOffers             int      `json:"catalog_offers"`
	CatalogSnapshotSHA256     string   `json:"catalog_snapshot_sha256"`
	InventorySnapshotSHA256   string   `json:"inventory_snapshot_sha256"`
	SourceManifestSHA256      string   `json:"source_manifest_sha256"`
	CatalogDataOrigin         string   `json:"catalog_data_origin"`
	ReviewerVisibleFields     []string `json:"reviewer_visible_fields"`
	SharedReferenceFiles      []string `json:"shared_reference_files"`
	MetadataWithheld          []string `json:"metadata_withheld"`
}

func buildChallengeReviewerPacketManifest(masterPromptHash, reviewerAHash, reviewerBHash, reviewerCatalogHash string,
	taskCount, catalogProducts, catalogOffers int, hashes agentshopping.ChallengeEvaluationHashes) challengeReviewerPacketManifest {
	return challengeReviewerPacketManifest{SchemaVersion: "stablepay.agent-shopping-reviewer-packets.v2",
		Status: "METADATA_STRIPPED_SHUFFLED_AND_PINNED_CATALOG_PROCESS_SELF_ATTESTED", TaskCount: taskCount,
		MasterPromptFileSHA256: masterPromptHash, ReviewerAPacketFileSHA256: reviewerAHash,
		ReviewerBPacketFileSHA256: reviewerBHash, ReviewerCatalogSHA256: reviewerCatalogHash,
		CatalogProducts: catalogProducts, CatalogOffers: catalogOffers,
		CatalogSnapshotSHA256: hashes.CatalogSHA256, InventorySnapshotSHA256: hashes.InventorySHA256,
		SourceManifestSHA256: hashes.ManifestSHA256, CatalogDataOrigin: "SYNTHETIC",
		ReviewerVisibleFields: []string{"task_id", "prompt", "buyer_default_region", "buyer_budget_minor"},
		SharedReferenceFiles:  []string{"reviewer-catalog.jsonl"},
		MetadataWithheld: []string{"scenario", "prompt_author_id", "prompt_written_without_generated_template", "model_blind",
			"merchant_did", "merchant_wallet_address", "internal_substitution_group"}}
}

func verifyChallengeReviewerPacketManifest(manifestBytes []byte, masterPromptsPath, reviewerAPromptsPath,
	reviewerBPromptsPath, reviewerCatalogPath string, catalog agentshopping.ImportedCatalog,
	sourceManifestBytes []byte) error {
	var stored challengeReviewerPacketManifest
	decoder := json.NewDecoder(bytes.NewReader(manifestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stored); err != nil {
		return fmt.Errorf("decode packet manifest: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("packet manifest contains trailing JSON data")
	}
	prompts, err := readReviewJSONLines[agentshopping.ChallengePromptRecord](masterPromptsPath, false)
	if err != nil {
		return fmt.Errorf("read frozen master prompts: %w", err)
	}
	masterHash, err := hashFileSHA256(masterPromptsPath)
	if err != nil {
		return err
	}
	reviewerAHash, err := hashFileSHA256(reviewerAPromptsPath)
	if err != nil {
		return err
	}
	reviewerBHash, err := hashFileSHA256(reviewerBPromptsPath)
	if err != nil {
		return err
	}
	reviewerCatalogHash, err := hashFileSHA256(reviewerCatalogPath)
	if err != nil {
		return err
	}
	records, err := buildChallengeReviewerCatalog(catalog)
	if err != nil {
		return err
	}
	hashes, err := agentshopping.ComputeChallengeHashes(catalog, nil, sourceManifestBytes)
	if err != nil {
		return err
	}
	offers := 0
	for _, record := range records {
		offers += len(record.Offers)
	}
	expected := buildChallengeReviewerPacketManifest(masterHash, reviewerAHash, reviewerBHash,
		reviewerCatalogHash, len(prompts), len(records), offers, hashes)
	if !reflect.DeepEqual(stored, expected) {
		return errors.New("packet manifest hashes or metadata do not match the supplied frozen review inputs")
	}
	return nil
}

type challengeReviewerCatalogOffer struct {
	Region     string `json:"region"`
	Price      string `json:"price"`
	PriceMinor int64  `json:"price_minor"`
	Currency   string `json:"currency"`
	OnHand     int    `json:"on_hand"`
	Eligible   bool   `json:"eligible"`
	InStock    bool   `json:"in_stock"`
	Revision   int64  `json:"revision"`
}

// challengeReviewerCatalogRecord exposes only synthetic buyer-visible listing
// and offer facts. DID/address data and internal substitution-policy groups
// are intentionally excluded from reviewer materials.
type challengeReviewerCatalogRecord struct {
	MerchantID        string                          `json:"merchant_id"`
	MerchantName      string                          `json:"merchant_name"`
	SKUID             string                          `json:"sku_id"`
	Category          string                          `json:"category"`
	Title             string                          `json:"title"`
	Description       string                          `json:"description"`
	Tags              []string                        `json:"tags,omitempty"`
	Attributes        map[string]any                  `json:"attributes,omitempty"`
	Status            string                          `json:"status"`
	Item              string                          `json:"item"`
	Format            string                          `json:"format"`
	Style             string                          `json:"style"`
	ListingPrice      string                          `json:"listing_price"`
	ListingPriceMinor int64                           `json:"listing_price_minor"`
	Currency          string                          `json:"currency"`
	Offers            []challengeReviewerCatalogOffer `json:"offers"`
}

func buildChallengeReviewerCatalog(catalog agentshopping.ImportedCatalog) ([]challengeReviewerCatalogRecord, error) {
	merchantNames := make(map[string]string, len(catalog.Merchants))
	for _, merchant := range catalog.Merchants {
		if merchant.MerchantID == "" || merchant.Name == "" || merchantNames[merchant.MerchantID] != "" {
			return nil, errors.New("review catalog contains an invalid or duplicate merchant")
		}
		merchantNames[merchant.MerchantID] = merchant.Name
	}
	offersByProduct := make(map[string][]challengeReviewerCatalogOffer, len(catalog.Products))
	seenOffers := make(map[string]bool, len(catalog.Inventory))
	for _, offer := range catalog.Inventory {
		key := offer.MerchantID + "\x00" + offer.SKUID
		offerKey := key + "\x00" + offer.Region
		if offer.TenantID != agentshopping.SimulationTenantID || offer.MerchantID == "" || offer.SKUID == "" ||
			offer.Region == "" || offer.PriceMinor < 0 || offer.OnHand < 0 || offer.Revision <= 0 || seenOffers[offerKey] {
			return nil, errors.New("review catalog contains an invalid or duplicate inventory offer")
		}
		seenOffers[offerKey] = true
		offersByProduct[key] = append(offersByProduct[key], challengeReviewerCatalogOffer{
			Region: offer.Region, Price: agentshopping.FormatMinorUnits(offer.PriceMinor), PriceMinor: offer.PriceMinor,
			Currency: "USDC", OnHand: offer.OnHand, Eligible: offer.Eligible,
			InStock: offer.Eligible && offer.OnHand > 0, Revision: offer.Revision,
		})
	}
	records := make([]challengeReviewerCatalogRecord, 0, len(catalog.Products))
	seenProducts := make(map[string]bool, len(catalog.Products))
	for _, product := range catalog.Products {
		key := product.MerchantID + "\x00" + product.SKUID
		if product.MerchantID == "" || product.SKUID == "" || product.Title == "" || product.Status != "active" ||
			merchantNames[product.MerchantID] == "" || seenProducts[key] {
			return nil, errors.New("review catalog contains an invalid or duplicate product")
		}
		seenProducts[key] = true
		offers := offersByProduct[key]
		if len(offers) == 0 {
			return nil, fmt.Errorf("review catalog product %s/%s has no regional offers", product.MerchantID, product.SKUID)
		}
		sort.Slice(offers, func(i, j int) bool { return offers[i].Region < offers[j].Region })
		records = append(records, challengeReviewerCatalogRecord{MerchantID: product.MerchantID,
			MerchantName: merchantNames[product.MerchantID], SKUID: product.SKUID, Category: product.Category,
			Title: product.Title, Description: product.Description, Tags: append([]string(nil), product.Tags...),
			Attributes: product.Attributes, Status: product.Status, Item: product.Item, Format: product.Format,
			Style: product.Style, ListingPrice: product.Price, ListingPriceMinor: product.PriceMinor,
			Currency: product.Currency, Offers: append([]challengeReviewerCatalogOffer(nil), offers...)})
	}
	if len(seenOffers) != len(catalog.Inventory) || len(seenProducts) != len(catalog.Products) {
		return nil, errors.New("review catalog failed its source row-count check")
	}
	for key := range offersByProduct {
		if !seenProducts[key] {
			return nil, errors.New("review catalog contains offers for a product missing from the listing snapshot")
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].MerchantID != records[j].MerchantID {
			return records[i].MerchantID < records[j].MerchantID
		}
		return records[i].SKUID < records[j].SKUID
	})
	return records, nil
}

func challengeReviewerCatalogJSONLines(catalog agentshopping.ImportedCatalog) ([]byte, error) {
	records, err := buildChallengeReviewerCatalog(catalog)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	for _, record := range records {
		encoded, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		output.Write(encoded)
		output.WriteByte('\n')
	}
	return output.Bytes(), nil
}

func prepareChallengeReview(args []string) error {
	flags := flag.NewFlagSet("prepare-challenge-review", flag.ContinueOnError)
	manifestPath := flags.String("manifest", filepath.Join(".local-run", "simulated-merchants", "manifest.json"), "local synthetic Merchant manifest (the 12 loopback fixtures must be running)")
	seed := flags.Int64("seed", 20261010, "expected synthetic Merchant catalog seed")
	promptsPath := flags.String("prompts", "", "master prompt-record JSONL (required)")
	outputDir := flags.String("out-dir", "", "new output directory directly under .local-run/agent-shopping/challenge-suites (required)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *promptsPath == "" || *outputDir == "" {
		return errors.New("prepare-challenge-review requires --prompts, --out-dir, and no positional arguments")
	}
	challengeRoot, err := filepath.Abs(filepath.Join(".local-run", "agent-shopping", "challenge-suites"))
	if err != nil {
		return err
	}
	challengeRootReal, err := filepath.EvalSymlinks(challengeRoot)
	if err != nil {
		return fmt.Errorf("resolve challenge-suite root: %w", err)
	}
	localRunReal, err := filepath.EvalSymlinks(filepath.Dir(filepath.Dir(challengeRoot)))
	if err != nil || !pathWithinDirectory(localRunReal, challengeRootReal) {
		return errors.New("challenge-suite root resolves outside the local .local-run directory")
	}
	manifestAbs, err := filepath.Abs(*manifestPath)
	if err != nil {
		return err
	}
	if filepath.Base(filepath.Dir(manifestAbs)) != "simulated-merchants" ||
		filepath.Base(filepath.Dir(filepath.Dir(manifestAbs))) != ".local-run" {
		return errors.New("source manifest must be under .local-run/simulated-merchants")
	}
	manifestReal, err := resolveRegularFileUnder(localRunReal, manifestAbs)
	if err != nil {
		return fmt.Errorf("resolve source manifest: %w", err)
	}
	promptsAbs, err := filepath.Abs(*promptsPath)
	if err != nil {
		return err
	}
	promptsReal, err := resolveRegularFileUnder(challengeRootReal, promptsAbs)
	if err != nil {
		return fmt.Errorf("resolve master prompts: %w", err)
	}
	outputAbs, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	if !samePath(filepath.Dir(outputAbs), challengeRoot) || samePath(outputAbs, challengeRoot) {
		return errors.New("review packet output must be a new direct child of .local-run/agent-shopping/challenge-suites")
	}
	outputParentReal, err := filepath.EvalSymlinks(filepath.Dir(outputAbs))
	if err != nil || !samePath(outputParentReal, challengeRootReal) || !pathWithinDirectory(localRunReal, outputParentReal) {
		return errors.New("review packet output parent resolves outside the permitted challenge-suite directory")
	}
	if _, err := os.Lstat(outputAbs); err == nil {
		return fmt.Errorf("review packet output already exists; refusing to overwrite: %s", outputAbs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	prompts, err := readReviewJSONLines[agentshopping.ChallengePromptRecord](promptsReal, false)
	if err != nil {
		return fmt.Errorf("read master prompts: %w", err)
	}
	packetA, packetB, err := agentshopping.CreateChallengeReviewerPromptPackets(prompts)
	if err != nil {
		return err
	}
	manifestBytes, err := os.ReadFile(manifestReal)
	if err != nil {
		return fmt.Errorf("read source manifest: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	catalog, err := agentshopping.LoadMerchantCatalog(ctx, manifestReal, nil)
	if err != nil {
		return fmt.Errorf("load pinned synthetic catalog (start the 12 local Merchant fixtures first): %w", err)
	}
	if catalog.Seed != *seed {
		return fmt.Errorf("source seed %d differs from requested seed %d", catalog.Seed, *seed)
	}
	catalogRecords, err := buildChallengeReviewerCatalog(catalog)
	if err != nil {
		return err
	}
	catalogHashes, err := agentshopping.ComputeChallengeHashes(catalog, nil, manifestBytes)
	if err != nil {
		return fmt.Errorf("hash pinned review catalog: %w", err)
	}
	staging, err := os.MkdirTemp(challengeRootReal, ".review-packet-staging-")
	if err != nil {
		return fmt.Errorf("create reviewer-packet staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	pathA := filepath.Join(staging, "reviewer-a-prompts.jsonl")
	pathB := filepath.Join(staging, "reviewer-b-prompts.jsonl")
	if err := writeJSONLines(pathA, packetA); err != nil {
		return fmt.Errorf("write reviewer A prompt packet: %w", err)
	}
	if err := writeJSONLines(pathB, packetB); err != nil {
		return fmt.Errorf("write reviewer B prompt packet: %w", err)
	}
	catalogPath := filepath.Join(staging, "reviewer-catalog.jsonl")
	if err := writeJSONLines(catalogPath, catalogRecords); err != nil {
		return fmt.Errorf("write pinned reviewer catalog: %w", err)
	}
	masterHash, err := hashFileSHA256(promptsReal)
	if err != nil {
		return err
	}
	packetAHash, err := hashFileSHA256(pathA)
	if err != nil {
		return err
	}
	packetBHash, err := hashFileSHA256(pathB)
	if err != nil {
		return err
	}
	catalogHash, err := hashFileSHA256(catalogPath)
	if err != nil {
		return err
	}
	offerCount := 0
	for _, record := range catalogRecords {
		offerCount += len(record.Offers)
	}
	packetManifest := buildChallengeReviewerPacketManifest(masterHash, packetAHash, packetBHash,
		catalogHash, len(prompts), len(catalogRecords), offerCount, catalogHashes)
	if err := writeJSONFile(filepath.Join(staging, "packet-manifest.json"), packetManifest); err != nil {
		return fmt.Errorf("write reviewer packet manifest: %w", err)
	}
	if err := os.Rename(staging, outputAbs); err != nil {
		return fmt.Errorf("publish reviewer prompt packets: %w", err)
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"status": packetManifest.Status, "task_count": len(prompts), "output_dir": outputAbs,
		"reviewer_a_prompts":       filepath.Join(outputAbs, "reviewer-a-prompts.jsonl"),
		"reviewer_b_prompts":       filepath.Join(outputAbs, "reviewer-b-prompts.jsonl"),
		"reviewer_catalog":         filepath.Join(outputAbs, "reviewer-catalog.jsonl"),
		"packet_manifest":          filepath.Join(outputAbs, "packet-manifest.json"),
		"reviewer_a_packet_sha256": packetAHash, "reviewer_b_packet_sha256": packetBHash,
		"reviewer_catalog_sha256": catalogHash, "catalog_products": len(catalogRecords), "catalog_offers": offerCount,
		"catalog_snapshot_sha256": catalogHashes.CatalogSHA256, "inventory_snapshot_sha256": catalogHashes.InventorySHA256,
	})
}

func finalizeChallengeReview(args []string) error {
	flags := flag.NewFlagSet("finalize-challenge-review", flag.ContinueOnError)
	manifestPath := flags.String("manifest", filepath.Join(".local-run", "simulated-merchants", "manifest.json"), "local synthetic Merchant manifest")
	seed := flags.Int64("seed", 20261010, "expected synthetic Merchant catalog seed")
	promptsPath := flags.String("prompts", "", "blind prompt-record JSONL under the challenge-suites directory (required)")
	reviewerAPromptsPath := flags.String("reviewer-a-prompts", "", "prepared metadata-stripped prompt packet returned to reviewer A (required)")
	reviewerBPromptsPath := flags.String("reviewer-b-prompts", "", "prepared metadata-stripped prompt packet returned to reviewer B (required)")
	reviewerCatalogPath := flags.String("reviewer-catalog", "", "exact pinned reviewer-catalog.jsonl supplied with the prepared prompt packets (required)")
	packetManifestPath := flags.String("packet-manifest", "", "exact packet-manifest.json created with the reviewer materials (required)")
	reviewerAPath := flags.String("reviewer-a", "", "first independent annotation JSONL under the challenge-suites directory (required)")
	reviewerBPath := flags.String("reviewer-b", "", "second independent annotation JSONL under the challenge-suites directory (required)")
	adjudicationsPath := flags.String("adjudications", "", "adjudication JSONL under the challenge-suites directory; may be empty but file is required")
	outputDir := flags.String("out-dir", "", "new output directory directly under .local-run/agent-shopping/challenge-suites (required)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *promptsPath == "" || *reviewerAPromptsPath == "" || *reviewerBPromptsPath == "" || *reviewerCatalogPath == "" || *packetManifestPath == "" ||
		*reviewerAPath == "" || *reviewerBPath == "" ||
		*adjudicationsPath == "" || *outputDir == "" {
		return errors.New("finalize-challenge-review requires --prompts, --reviewer-a-prompts, --reviewer-b-prompts, --reviewer-catalog, --packet-manifest, --reviewer-a, --reviewer-b, --adjudications, --out-dir and no positional arguments")
	}

	manifestAbs, err := filepath.Abs(*manifestPath)
	if err != nil {
		return err
	}
	manifestDir := filepath.Dir(manifestAbs)
	if filepath.Base(manifestDir) != "simulated-merchants" || filepath.Base(filepath.Dir(manifestDir)) != ".local-run" {
		return errors.New("source manifest must be under .local-run/simulated-merchants")
	}
	localRunRoot := filepath.Dir(manifestDir)
	challengeRoot := filepath.Join(localRunRoot, "agent-shopping", "challenge-suites")
	outputAbs, err := filepath.Abs(*outputDir)
	if err != nil {
		return err
	}
	if !samePath(filepath.Dir(outputAbs), challengeRoot) || samePath(outputAbs, challengeRoot) {
		return errors.New("review output must be a new direct child directory under .local-run/agent-shopping/challenge-suites")
	}
	if _, err := os.Lstat(outputAbs); err == nil {
		return fmt.Errorf("review output directory already exists; refusing to overwrite: %s", outputAbs)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	localRunReal, err := filepath.EvalSymlinks(localRunRoot)
	if err != nil {
		return fmt.Errorf("resolve local-run root: %w", err)
	}
	manifestReal, err := resolveRegularFileUnder(localRunReal, manifestAbs)
	if err != nil {
		return fmt.Errorf("resolve source manifest: %w", err)
	}
	if err := os.MkdirAll(challengeRoot, 0o700); err != nil {
		return fmt.Errorf("create challenge-suite directory: %w", err)
	}
	challengeRootReal, err := filepath.EvalSymlinks(challengeRoot)
	if err != nil || !pathWithinDirectory(localRunReal, challengeRootReal) {
		return errors.New("challenge-suite directory resolves outside the local .local-run root")
	}

	inputPaths := []string{*promptsPath, *reviewerAPromptsPath, *reviewerBPromptsPath, *reviewerCatalogPath, *packetManifestPath,
		*reviewerAPath, *reviewerBPath, *adjudicationsPath}
	resolvedInputs := make([]string, len(inputPaths))
	for i, inputPath := range inputPaths {
		absolute, absErr := filepath.Abs(inputPath)
		if absErr != nil {
			return absErr
		}
		resolvedInputs[i], err = resolveRegularFileUnder(challengeRootReal, absolute)
		if err != nil {
			return fmt.Errorf("resolve review input %q: %w", inputPath, err)
		}
	}
	manifestBytes, err := os.ReadFile(manifestReal)
	if err != nil {
		return fmt.Errorf("read source manifest: %w", err)
	}
	prompts, err := readReviewJSONLines[agentshopping.ChallengePromptRecord](resolvedInputs[0], false)
	if err != nil {
		return fmt.Errorf("read blind prompt records: %w", err)
	}
	reviewerAPrompts, err := readReviewJSONLines[agentshopping.ChallengeReviewerPromptRecord](resolvedInputs[1], false)
	if err != nil {
		return fmt.Errorf("read reviewer A prompt packet: %w", err)
	}
	reviewerBPrompts, err := readReviewJSONLines[agentshopping.ChallengeReviewerPromptRecord](resolvedInputs[2], false)
	if err != nil {
		return fmt.Errorf("read reviewer B prompt packet: %w", err)
	}
	reviewerCatalogBytes, err := os.ReadFile(resolvedInputs[3])
	if err != nil {
		return fmt.Errorf("read reviewer catalog reference: %w", err)
	}
	packetManifestBytes, err := os.ReadFile(resolvedInputs[4])
	if err != nil {
		return fmt.Errorf("read reviewer packet manifest: %w", err)
	}
	reviewerA, err := readReviewJSONLines[agentshopping.ChallengeReviewAnnotation](resolvedInputs[5], false)
	if err != nil {
		return fmt.Errorf("read reviewer A annotations: %w", err)
	}
	reviewerB, err := readReviewJSONLines[agentshopping.ChallengeReviewAnnotation](resolvedInputs[6], false)
	if err != nil {
		return fmt.Errorf("read reviewer B annotations: %w", err)
	}
	adjudications, err := readReviewJSONLines[agentshopping.ChallengeAdjudicationRecord](resolvedInputs[7], true)
	if err != nil {
		return fmt.Errorf("read adjudications: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	catalog, err := agentshopping.LoadMerchantCatalog(ctx, manifestReal, nil)
	if err != nil {
		return err
	}
	if catalog.Seed != *seed {
		return fmt.Errorf("source seed %d differs from requested seed %d", catalog.Seed, *seed)
	}
	expectedReviewerCatalogBytes, err := challengeReviewerCatalogJSONLines(catalog)
	if err != nil {
		return err
	}
	if !bytes.Equal(reviewerCatalogBytes, expectedReviewerCatalogBytes) {
		return errors.New("reviewer-catalog.jsonl differs from the current pinned product/inventory snapshot")
	}
	if err := verifyChallengeReviewerPacketManifest(packetManifestBytes, resolvedInputs[0], resolvedInputs[1],
		resolvedInputs[2], resolvedInputs[3], catalog, manifestBytes); err != nil {
		return fmt.Errorf("verify prepared review packet: %w", err)
	}
	tasks, audit, err := agentshopping.FinalizeHumanReviewedChallengeTasksWithPackets(catalog, manifestBytes,
		prompts, reviewerAPrompts, reviewerBPrompts, reviewerA, reviewerB, adjudications)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(challengeRoot, 0o700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(challengeRoot, ".review-staging-")
	if err != nil {
		return fmt.Errorf("create local review staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	if err := writeJSONLines(filepath.Join(staging, "prompts.jsonl"), prompts); err != nil {
		return fmt.Errorf("write prompts.jsonl: %w", err)
	}
	if err := writeJSONLines(filepath.Join(staging, "reviewer-a-prompts.jsonl"), reviewerAPrompts); err != nil {
		return fmt.Errorf("write reviewer-a-prompts.jsonl: %w", err)
	}
	if err := writeJSONLines(filepath.Join(staging, "reviewer-b-prompts.jsonl"), reviewerBPrompts); err != nil {
		return fmt.Errorf("write reviewer-b-prompts.jsonl: %w", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "reviewer-catalog.jsonl"), reviewerCatalogBytes, 0o600); err != nil {
		return fmt.Errorf("write reviewer-catalog.jsonl: %w", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "packet-manifest.json"), packetManifestBytes, 0o600); err != nil {
		return fmt.Errorf("write packet-manifest.json: %w", err)
	}
	if err := writeJSONLines(filepath.Join(staging, "reviewer-a.jsonl"), reviewerA); err != nil {
		return fmt.Errorf("write reviewer-a.jsonl: %w", err)
	}
	if err := writeJSONLines(filepath.Join(staging, "reviewer-b.jsonl"), reviewerB); err != nil {
		return fmt.Errorf("write reviewer-b.jsonl: %w", err)
	}
	if err := writeJSONLines(filepath.Join(staging, "adjudications.jsonl"), adjudications); err != nil {
		return fmt.Errorf("write adjudications.jsonl: %w", err)
	}
	if err := writeJSONLines(filepath.Join(staging, "tasks.jsonl"), tasks); err != nil {
		return fmt.Errorf("write tasks.jsonl: %w", err)
	}
	if err := writeJSONFile(filepath.Join(staging, "review-audit.json"), audit); err != nil {
		return fmt.Errorf("write review audit: %w", err)
	}
	if err := os.Rename(staging, outputAbs); err != nil {
		return fmt.Errorf("publish finalized human-review bundle: %w", err)
	}
	manifestHash := sha256.Sum256(manifestBytes)
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"output_dir": outputAbs, "tasks_file": filepath.Join(outputAbs, "tasks.jsonl"),
		"review_audit_file":     filepath.Join(outputAbs, "review-audit.json"),
		"reviewer_catalog_file": filepath.Join(outputAbs, "reviewer-catalog.jsonl"),
		"packet_manifest_file":  filepath.Join(outputAbs, "packet-manifest.json"),
		"task_count":            audit.TaskCount, "scenario_task_counts": audit.ScenarioTaskCounts,
		"expected_disposition_task_counts": audit.DispositionTaskCounts,
		"agreement_task_count":             audit.AgreementTaskCount,
		"adjudicated_task_count":           audit.AdjudicatedTaskCount, "reviewer_ids": audit.ReviewerIDs,
		"reviewer_agreement": audit.Agreement,
		"tasks_sha256":       audit.Hashes.TasksSHA256, "prompt_records_sha256": audit.PromptRecordsSHA256,
		"reviewer_a_prompt_packet_sha256": audit.ReviewerAPromptPacketSHA256,
		"reviewer_b_prompt_packet_sha256": audit.ReviewerBPromptPacketSHA256,
		"reviewer_a_annotations_sha256":   audit.ReviewerAAnnotationsSHA256,
		"reviewer_b_annotations_sha256":   audit.ReviewerBAnnotationsSHA256,
		"adjudications_sha256":            audit.AdjudicationsSHA256, "source_manifest_sha256": "sha256:" + hex.EncodeToString(manifestHash[:]),
		"status": "BUNDLE_RECOMPUTABLE_IDENTITIES_AND_PROCESS_SELF_ATTESTED",
	})
}

func verifyChallengeReviewBundle(bundleDir, tasksPath string, catalog agentshopping.ImportedCatalog,
	manifestBytes []byte) (agentshopping.ChallengeReviewAudit, string, error) {
	if !samePath(tasksPath, filepath.Join(bundleDir, "tasks.jsonl")) {
		return agentshopping.ChallengeReviewAudit{}, "", errors.New("--tasks must point to tasks.jsonl in the supplied --review-bundle")
	}
	names := []string{"prompts.jsonl", "reviewer-a-prompts.jsonl", "reviewer-b-prompts.jsonl",
		"reviewer-catalog.jsonl", "packet-manifest.json", "reviewer-a.jsonl", "reviewer-b.jsonl", "adjudications.jsonl", "tasks.jsonl", "review-audit.json"}
	paths := make(map[string]string, len(names))
	for _, name := range names {
		path := filepath.Join(bundleDir, name)
		info, err := os.Lstat(path)
		if err != nil {
			return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("review bundle is missing %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("review bundle entry %s is not a regular file", name)
		}
		paths[name] = path
	}
	reviewerCatalogBytes, err := os.ReadFile(paths["reviewer-catalog.jsonl"])
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("read bundled reviewer catalog: %w", err)
	}
	expectedReviewerCatalogBytes, err := challengeReviewerCatalogJSONLines(catalog)
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", err
	}
	if !bytes.Equal(reviewerCatalogBytes, expectedReviewerCatalogBytes) {
		return agentshopping.ChallengeReviewAudit{}, "", errors.New("bundled reviewer catalog differs from the pinned product/inventory snapshot")
	}
	packetManifestBytes, err := os.ReadFile(paths["packet-manifest.json"])
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("read bundled packet manifest: %w", err)
	}
	if err := verifyChallengeReviewerPacketManifest(packetManifestBytes, paths["prompts.jsonl"],
		paths["reviewer-a-prompts.jsonl"], paths["reviewer-b-prompts.jsonl"], paths["reviewer-catalog.jsonl"],
		catalog, manifestBytes); err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("verify bundled packet manifest: %w", err)
	}
	prompts, err := readReviewJSONLines[agentshopping.ChallengePromptRecord](paths["prompts.jsonl"], false)
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("read bundled prompt records: %w", err)
	}
	reviewerAPrompts, err := readReviewJSONLines[agentshopping.ChallengeReviewerPromptRecord](paths["reviewer-a-prompts.jsonl"], false)
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("read bundled reviewer A prompt packet: %w", err)
	}
	reviewerBPrompts, err := readReviewJSONLines[agentshopping.ChallengeReviewerPromptRecord](paths["reviewer-b-prompts.jsonl"], false)
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("read bundled reviewer B prompt packet: %w", err)
	}
	reviewerA, err := readReviewJSONLines[agentshopping.ChallengeReviewAnnotation](paths["reviewer-a.jsonl"], false)
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("read bundled reviewer A annotations: %w", err)
	}
	reviewerB, err := readReviewJSONLines[agentshopping.ChallengeReviewAnnotation](paths["reviewer-b.jsonl"], false)
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("read bundled reviewer B annotations: %w", err)
	}
	adjudications, err := readReviewJSONLines[agentshopping.ChallengeAdjudicationRecord](paths["adjudications.jsonl"], true)
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("read bundled adjudications: %w", err)
	}
	recomputedTasks, recomputedAudit, err := agentshopping.FinalizeHumanReviewedChallengeTasksWithPackets(catalog, manifestBytes,
		prompts, reviewerAPrompts, reviewerBPrompts, reviewerA, reviewerB, adjudications)
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", err
	}
	storedTasks, err := readChallengeTasks(paths["tasks.jsonl"])
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("read finalized tasks: %w", err)
	}
	storedHashes, err := agentshopping.ComputeChallengeHashes(catalog, storedTasks, manifestBytes)
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", err
	}
	if storedHashes != recomputedAudit.Hashes || len(storedTasks) != len(recomputedTasks) {
		return agentshopping.ChallengeReviewAudit{}, "", errors.New("finalized task file does not match the independently recomputed review packet")
	}
	for index := range storedTasks {
		stored, marshalErr := json.Marshal(storedTasks[index])
		if marshalErr != nil {
			return agentshopping.ChallengeReviewAudit{}, "", marshalErr
		}
		recomputed, marshalErr := json.Marshal(recomputedTasks[index])
		if marshalErr != nil {
			return agentshopping.ChallengeReviewAudit{}, "", marshalErr
		}
		if !bytes.Equal(stored, recomputed) {
			return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("finalized task %s differs from the recomputed human-review result", storedTasks[index].TaskID)
		}
	}
	auditBytes, err := os.ReadFile(paths["review-audit.json"])
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", err
	}
	var storedAudit agentshopping.ChallengeReviewAudit
	decoder := json.NewDecoder(bytes.NewReader(auditBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&storedAudit); err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", fmt.Errorf("decode review audit: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return agentshopping.ChallengeReviewAudit{}, "", errors.New("review audit contains trailing JSON data")
	}
	storedAuditJSON, err := json.Marshal(storedAudit)
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", err
	}
	recomputedAuditJSON, err := json.Marshal(recomputedAudit)
	if err != nil {
		return agentshopping.ChallengeReviewAudit{}, "", err
	}
	if !bytes.Equal(storedAuditJSON, recomputedAuditJSON) {
		return agentshopping.ChallengeReviewAudit{}, "", errors.New("review-audit.json does not match the recomputed review packet")
	}
	auditDigest := sha256.Sum256(auditBytes)
	return recomputedAudit, "sha256:" + hex.EncodeToString(auditDigest[:]), nil
}

func readReviewJSONLines[T any](path string, allowEmpty bool) ([]T, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var records []T
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(line))
		decoder.DisallowUnknownFields()
		var record T
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("decode line %d: %w", lineNumber, err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, fmt.Errorf("line %d contains trailing JSON data", lineNumber)
		}
		records = append(records, record)
		if len(records) > 500 {
			return nil, errors.New("review input exceeds the 500-task maximum")
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(records) == 0 && !allowEmpty {
		return nil, errors.New("review input is empty")
	}
	return records, nil
}

func resolveRegularFileUnder(root, path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	if !pathWithinDirectory(root, resolved) {
		return "", errors.New("path resolves outside the permitted local directory")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("path is not a regular file")
	}
	return resolved, nil
}
