package agentshopping

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

const (
	esciHTTPReferenceRunEnv = "STABLEPAY_RUN_ESCI_HTTP_MYSQL_E2E"
	esciHTTPReferenceCutoff = 5
)

type esciHTTPOracleParser struct {
	intentsByQuery map[string]Intent
}

func (esciHTTPOracleParser) Provider() string { return "public-reference-oracle" }
func (esciHTTPOracleParser) Model() string    { return "structured-intent-fixture" }

func (p esciHTTPOracleParser) Parse(_ context.Context, prompt, region string, budget int64, _ Taxonomy) (Intent, error) {
	intent, ok := p.intentsByQuery[prompt]
	if !ok {
		return Intent{}, fmt.Errorf("ESCI query is not in the frozen reference sample")
	}
	intent.Region = region
	intent.MaxPriceMinor = budget
	intent.ConfirmationNeeded = true
	return intent, nil
}

func (p esciHTTPOracleParser) ParseWithUsage(ctx context.Context, prompt, region string, budget int64, taxonomy Taxonomy) (Intent, ParseUsage, error) {
	intent, err := p.Parse(ctx, prompt, region, budget, taxonomy)
	return intent, ParseUsage{UsageStatus: "NOT_APPLICABLE_NO_MODEL_CALL", Reasoning: "none"}, err
}

type esciHTTPReferenceSampleManifest struct {
	Dataset       string `json:"dataset"`
	SourceSplit   string `json:"source_split"`
	SourceSHA256  string `json:"source_sha256"`
	SampleSHA256  string `json:"sample_sha256"`
	QueryCount    int    `json:"sample_query_count"`
	CandidateRows int    `json:"sample_candidate_pair_count"`
}

type esciHTTPReferenceIntentResponse struct {
	TaskID                    string      `json:"task_id"`
	TaskStatus                string      `json:"task_status"`
	Intent                    Intent      `json:"intent"`
	Candidates                []Candidate `json:"candidates"`
	BuyerConfirmationRequired bool        `json:"buyer_confirmation_required"`
}

type esciHTTPReferenceTrial struct {
	QueryID               string   `json:"query_id"`
	QuerySHA256           string   `json:"query_sha256"`
	JudgedCandidateCount  int      `json:"judged_candidate_count"`
	ExactTargetCount      int      `json:"exact_target_count"`
	ExactRetrievedAt5     int      `json:"exact_retrieved_at_5"`
	RecallAt5             float64  `json:"exact_target_recall_at_5"`
	QueryHitAt5           bool     `json:"exact_target_query_hit_at_5"`
	NDCGAt5               float64  `json:"esci_ndcg_at_5"`
	Top5Labels            []string `json:"top_5_esci_labels"`
	APIOrderMatchesRanker bool     `json:"api_order_matches_runtime_ranker"`
	HTTPStatus            int      `json:"http_status"`
	ResponseCandidates    int      `json:"response_candidate_count"`
	LatencyMillis         float64  `json:"http_search_latency_ms"`
}

type esciHTTPReferenceMetrics struct {
	SchemaVersion                string           `json:"schema_version"`
	Status                       string           `json:"status"`
	RunID                        string           `json:"run_id"`
	TenantID                     string           `json:"tenant_id"`
	Dataset                      string           `json:"dataset"`
	DatasetSplit                 string           `json:"dataset_split"`
	SampleSHA256                 string           `json:"sample_sha256"`
	SourceSHA256                 string           `json:"source_sha256"`
	QueryCount                   int              `json:"query_count"`
	JudgedCandidateRows          int              `json:"judged_candidate_rows"`
	MerchantListingRows          int              `json:"merchant_listing_rows"`
	SearchRegion                 string           `json:"synthetic_search_region"`
	Parser                       string           `json:"intent_source"`
	NewModelCalls                int              `json:"new_model_calls"`
	HTTPRequestCount             int              `json:"http_request_count"`
	HTTPStatusCounts             map[string]int   `json:"http_status_counts"`
	RankerOrderMatches           int              `json:"api_order_matches_runtime_ranker"`
	ExactTargets                 int              `json:"exact_targets"`
	ExactTargetsRetrievedAt5     int              `json:"exact_targets_retrieved_at_5"`
	ExactRecallMacroAt5          float64          `json:"exact_target_recall_macro_at_5"`
	ExactRecallMicroAt5          float64          `json:"exact_target_recall_micro_at_5"`
	ExactTargetQueryHitRateAt5   float64          `json:"exact_target_query_hit_rate_at_5"`
	NDCGMacroAt5                 float64          `json:"esci_ndcg_macro_at_5"`
	TaskHTTPP50Millis            float64          `json:"task_http_p50_ms"`
	TaskHTTPP95Millis            float64          `json:"task_http_p95_ms"`
	QueriesPerSecond             float64          `json:"queries_per_second"`
	ObservedTenantRows           map[string]int64 `json:"observed_tenant_rows_before_cleanup"`
	TaskStatuses                 map[string]int64 `json:"task_statuses"`
	ForbiddenPurchaseSideEffects map[string]int64 `json:"forbidden_purchase_side_effects"`
	TenantCleanupVerified        bool             `json:"tenant_cleanup_verified"`
	Scope                        string           `json:"scope"`
}

type esciHTTPReferenceAudit struct {
	Passed            bool             `json:"passed"`
	ExpectedRows      map[string]int64 `json:"expected_rows"`
	ObservedRows      map[string]int64 `json:"observed_rows"`
	TaskStatuses      map[string]int64 `json:"task_statuses"`
	ForbiddenEffects  map[string]int64 `json:"forbidden_purchase_side_effects"`
	TenantRowsRemoved bool             `json:"tenant_rows_removed"`
	Violations        []string         `json:"violations"`
}

// TestMySQLHTTPEsciReferenceSearch replays a fixed, already-local ESCI test
// sample through the authenticated shopping HTTP API and an isolated MySQL
// tenant. The parser supplies only the sample's structured routing tuple; the
// buyer query text still drives the real catalog ranker. It is a judged-pool
// search reference, not a full-catalog, intent-quality, or purchase metric.
func TestMySQLHTTPEsciReferenceSearch(t *testing.T) {
	if os.Getenv(esciHTTPReferenceRunEnv) != "1" {
		t.Skip("set STABLEPAY_RUN_ESCI_HTTP_MYSQL_E2E=1 to run the local ESCI HTTP/MySQL reference evaluation")
	}
	if strings.TrimSpace(os.Getenv("SHOP_SIM_DB_PASSWORD")) == "" {
		t.Fatal("SHOP_SIM_DB_PASSWORD is required for the ESCI HTTP/MySQL reference evaluation")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	samplePath := strings.TrimSpace(os.Getenv("STABLEPAY_ESCI_SAMPLE_PATH"))
	if samplePath == "" {
		samplePath = filepath.Join(repoRoot, ".local-run", "agent-shopping", "reference-data", "esci-us-test-20261011",
			"sparse-exact-disjoint-20261011", "esci-us-test-500-queries.jsonl")
	}
	samplePath, err = filepath.Abs(samplePath)
	if err != nil {
		t.Fatal(err)
	}
	referenceRoot, err := filepath.Abs(filepath.Join(repoRoot, ".local-run", "agent-shopping", "reference-data"))
	if err != nil {
		t.Fatal(err)
	}
	relativeSample, err := filepath.Rel(referenceRoot, samplePath)
	if err != nil || filepath.IsAbs(relativeSample) || relativeSample == ".." || strings.HasPrefix(relativeSample, ".."+string(os.PathSeparator)) {
		t.Fatalf("ESCI sample must remain under the local reference-data directory: %s", samplePath)
	}
	info, err := os.Stat(samplePath)
	if err != nil {
		t.Fatalf("open pre-staged ESCI sample: %v", err)
	}
	if info.Size() > 20*1024*1024 {
		t.Fatalf("refusing ESCI reference sample larger than 20 MiB: %d bytes", info.Size())
	}
	sampleBytes, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatal(err)
	}
	sampleDigest := sha256.Sum256(sampleBytes)
	sampleHash := "sha256:" + hex.EncodeToString(sampleDigest[:])
	groups, err := readESCIReferenceGroups(sampleBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 500 {
		t.Fatalf("expected exactly 500 ESCI queries, got %d", len(groups))
	}
	manifestPath := filepath.Join(filepath.Dir(samplePath), "reference-sample-manifest.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read ESCI sample manifest: %v", err)
	}
	var sampleManifest esciHTTPReferenceSampleManifest
	if err := json.Unmarshal(manifestBytes, &sampleManifest); err != nil {
		t.Fatalf("decode ESCI sample manifest: %v", err)
	}
	if sampleManifest.SourceSplit != "test" || sampleManifest.SampleSHA256 != sampleHash ||
		sampleManifest.QueryCount != len(groups) || sampleManifest.CandidateRows <= 0 {
		t.Fatalf("ESCI sample provenance manifest does not match the local sample: %+v", sampleManifest)
	}

	const merchantID = "esci-reference-merchant"
	const category = "esci-reference"
	const format = "judged-candidate"
	region := Regions[0]
	const buyerBudget = int64(100000)
	queryIntents := make(map[string]Intent, len(groups))
	buyers := make([]BuyerRecord, 0, len(groups))
	products := make([]Product, 0, sampleManifest.CandidateRows)
	productRows := make([]ProductRecord, 0, sampleManifest.CandidateRows)
	inventoryRows := make([]InventoryRecord, 0, sampleManifest.CandidateRows)
	labelsByQuerySKU := make(map[string]map[string]string, len(groups))
	seenQueries := make(map[string]bool, len(groups))
	now := time.Now().UTC()
	for _, group := range groups {
		if seenQueries[group.Query] {
			t.Fatalf("ESCI sample has repeated query wording, which the reference-only intent router cannot disambiguate: %s", group.QueryID)
		}
		seenQueries[group.Query] = true
		intent := Intent{Category: category, Item: group.QueryID, RequiredFormat: format,
			Region: region, MaxPriceMinor: buyerBudget, ConfirmationNeeded: true}
		queryIntents[group.Query] = intent
		buyerID := "esci-buyer-" + StableID(group.QueryID)[:24]
		buyers = append(buyers, BuyerRecord{TenantID: "pending", BuyerID: buyerID, DisplayName: "ESCI Reference Buyer",
			HomeRegion: region, Segment: "public-reference", BudgetMinor: buyerBudget, PreferencesJSON: []byte(`{}`),
			DataOrigin: syntheticOrigin, CreatedAt: now})
		labelsByQuerySKU[group.QueryID] = make(map[string]string, len(group.Candidates))
		for _, candidate := range group.Candidates {
			if candidate.ProductID == "" || (candidate.Label != "E" && candidate.Label != "S" && candidate.Label != "C" && candidate.Label != "I") {
				t.Fatalf("invalid ESCI candidate in query %s", group.QueryID)
			}
			skuID := "esci-" + StableID("esci-reference-sku", group.QueryID, candidate.ProductID)
			if _, exists := labelsByQuerySKU[group.QueryID][skuID]; exists {
				t.Fatalf("duplicate candidate product %s in query %s", candidate.ProductID, group.QueryID)
			}
			tags := []string{}
			if strings.TrimSpace(candidate.BulletPoint) != "" {
				tags = append(tags, candidate.BulletPoint)
			}
			attributes := map[string]any{}
			if candidate.Brand != "" {
				attributes["brand"] = candidate.Brand
			}
			if candidate.Color != "" {
				attributes["color"] = candidate.Color
			}
			product := Product{ID: StableID("product", merchantID, skuID), MerchantID: merchantID, SKUID: skuID,
				Category: category, Item: group.QueryID, Format: format, Title: candidate.Title,
				Description: candidate.Description, Price: FormatMinorUnits(1000), PriceMinor: 1000,
				Currency: "USDC", Tags: tags, Attributes: attributes, Status: "active"}
			tagsJSON, err := json.Marshal(tags)
			if err != nil {
				t.Fatal(err)
			}
			attributesJSON, err := json.Marshal(attributes)
			if err != nil {
				t.Fatal(err)
			}
			products = append(products, product)
			productRows = append(productRows, ProductRecord{TenantID: "pending", SKUID: skuID, MerchantID: merchantID,
				Category: category, Item: group.QueryID, Format: format, Title: candidate.Title,
				Description: candidate.Description, BasePrice: 1000, Currency: "USDC", Status: "active",
				TagsJSON: tagsJSON, AttributesJSON: attributesJSON, DataOrigin: syntheticOrigin, CreatedAt: now, UpdatedAt: now})
			inventoryRows = append(inventoryRows, InventoryRecord{TenantID: "pending", MerchantID: merchantID, SKUID: skuID,
				Region: region, OnHand: 1, Eligible: true, PriceMinor: 1000, Currency: "USDC", Revision: 1,
				UpdatedAt: now, DataOrigin: syntheticOrigin})
			labelsByQuerySKU[group.QueryID][skuID] = candidate.Label
		}
	}
	if len(products) != sampleManifest.CandidateRows {
		t.Fatalf("sample manifest reports %d candidates; decoded %d", sampleManifest.CandidateRows, len(products))
	}
	if len(queryIntents) != len(groups) {
		t.Fatalf("intent routing map has %d entries for %d queries", len(queryIntents), len(groups))
	}

	store, err := OpenLocalMySQLStore(os.Getenv("SHOP_SIM_DB_PASSWORD"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close ESCI reference MySQL store: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tenantID := "shopping-esci-http-" + mysqlIntegrationSuffix(t)
	store.tenant = tenantID
	cleanupVerified := false
	t.Cleanup(func() {
		if cleanupVerified {
			return
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cleanupCancel()
		if err := cleanupMySQLIntegrationTenant(cleanupCtx, store, tenantID); err != nil {
			t.Errorf("clean ESCI reference tenant %q: %v", tenantID, err)
		}
	})
	merchant := MerchantRecord{TenantID: tenantID, MerchantID: merchantID, Name: "ESCI Public Reference Fixture",
		Category: "reference", DID: "did:sim:" + merchantID, DataOrigin: syntheticOrigin, CreatedAt: now}
	for index := range buyers {
		buyers[index].TenantID = tenantID
	}
	for index := range productRows {
		productRows[index].TenantID = tenantID
		inventoryRows[index].TenantID = tenantID
	}
	if err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&merchant).Error; err != nil {
			return err
		}
		if err := tx.CreateInBatches(&productRows, 250).Error; err != nil {
			return err
		}
		if err := tx.CreateInBatches(&inventoryRows, 250).Error; err != nil {
			return err
		}
		return tx.CreateInBatches(&buyers, 250).Error
	}); err != nil {
		t.Fatal(err)
	}
	catalog := ImportedCatalog{Merchants: []MerchantRecord{merchant}, Products: products}
	parser := esciHTTPOracleParser{intentsByQuery: queryIntents}
	api, err := NewHTTPAPIWithMerchantCatalog(store, parser, mysqlIntegrationAPIKey, catalog, mysqlIntegrationBuyerApprovalKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client := &mysqlIntegrationAPI{client: server.Client(), server: server, key: mysqlIntegrationAPIKey}

	latencies := make([]float64, 0, len(groups))
	trials := make([]esciHTTPReferenceTrial, 0, len(groups))
	statusCounts := map[string]int{}
	macroRecall, macroNDCG := 0.0, 0.0
	totalExact, totalRetrieved, queryHits, orderMatches := 0, 0, 0, 0
	startedAll := time.Now()
	for _, group := range groups {
		buyerID := "esci-buyer-" + StableID(group.QueryID)[:24]
		started := time.Now()
		response := client.do(t, http.MethodPost, "/v1/intents/parse", map[string]any{
			"buyer_id": buyerID, "prompt": group.Query, "idempotency_key": "esci-search-" + StableID(group.QueryID),
		})
		latencyMillis := float64(time.Since(started).Microseconds()) / 1000
		latencies = append(latencies, latencyMillis)
		statusCounts[fmt.Sprint(response.status)]++
		if response.status != http.StatusOK {
			t.Fatalf("ESCI search HTTP status=%d query_id=%s body=%s", response.status, group.QueryID, response.body)
		}
		var decoded esciHTTPReferenceIntentResponse
		if err := json.Unmarshal(response.body, &decoded); err != nil {
			t.Fatalf("decode ESCI search response for %s: %v", group.QueryID, err)
		}
		if decoded.TaskID == "" || decoded.TaskStatus != TaskStatusDiscovered || !decoded.BuyerConfirmationRequired {
			t.Fatalf("ESCI search did not preserve the discovery/consent boundary for %s: %+v", group.QueryID, decoded)
		}
		if len(decoded.Candidates) != minInt(20, len(group.Candidates)) {
			t.Fatalf("ESCI query %s returned %d candidates; expected top %d", group.QueryID, len(decoded.Candidates), minInt(20, len(group.Candidates)))
		}

		intent := queryIntents[group.Query]
		intent = intent.WithSearchText(group.Query)
		intent.discoveryDocs = api.catalogDiscovery.documentsForIntent(intent)
		discoveryProducts := api.catalogDiscovery.productsForIntent(intent)
		expectedRanked := rankDiscoveryProducts(discoveryProducts, intent, DefaultDiscoveryCandidateWindow)
		orderMatchesThisTask := len(decoded.Candidates) == minInt(20, len(expectedRanked))
		for index := range decoded.Candidates {
			if index >= len(expectedRanked) || decoded.Candidates[index].Product.SKUID != expectedRanked[index].SKUID {
				orderMatchesThisTask = false
				break
			}
		}
		if !orderMatchesThisTask {
			t.Fatalf("HTTP/MySQL response order differs from runtime discovery order for %s", group.QueryID)
		}
		orderMatches++

		labelsBySKU := labelsByQuerySKU[group.QueryID]
		exactCount, exactAt5 := 0, 0
		for _, candidate := range group.Candidates {
			if candidate.Label == "E" {
				exactCount++
			}
		}
		if exactCount == 0 {
			t.Fatalf("ESCI query %s has no Exact labels", group.QueryID)
		}
		limit := minInt(esciHTTPReferenceCutoff, len(decoded.Candidates))
		topLabels := make([]string, 0, limit)
		dcg := 0.0
		for index := 0; index < limit; index++ {
			label, ok := labelsBySKU[decoded.Candidates[index].Product.SKUID]
			if !ok {
				t.Fatalf("API returned an SKU without a query-specific ESCI judgment for %s", group.QueryID)
			}
			topLabels = append(topLabels, label)
			if label == "E" {
				exactAt5++
			}
			grade := esciLabelGrade(label)
			dcg += (math.Pow(2, float64(grade)) - 1) / math.Log2(float64(index+2))
		}
		idealGrades := make([]int, 0, len(group.Candidates))
		for _, candidate := range group.Candidates {
			idealGrades = append(idealGrades, esciLabelGrade(candidate.Label))
		}
		sort.Sort(sort.Reverse(sort.IntSlice(idealGrades)))
		idcg := 0.0
		for index := 0; index < limit; index++ {
			idcg += (math.Pow(2, float64(idealGrades[index])) - 1) / math.Log2(float64(index+2))
		}
		ndcg := 0.0
		if idcg > 0 {
			ndcg = dcg / idcg
		}
		recall := float64(exactAt5) / float64(exactCount)
		queryHit := exactAt5 > 0
		queryDigest := sha256.Sum256([]byte(group.Query))
		trials = append(trials, esciHTTPReferenceTrial{QueryID: group.QueryID,
			QuerySHA256: "sha256:" + hex.EncodeToString(queryDigest[:]), JudgedCandidateCount: len(group.Candidates),
			ExactTargetCount: exactCount, ExactRetrievedAt5: exactAt5, RecallAt5: recall, QueryHitAt5: queryHit,
			NDCGAt5: ndcg, Top5Labels: topLabels, APIOrderMatchesRanker: orderMatchesThisTask,
			HTTPStatus: response.status, ResponseCandidates: len(decoded.Candidates), LatencyMillis: latencyMillis})
		macroRecall += recall
		macroNDCG += ndcg
		totalExact += exactCount
		totalRetrieved += exactAt5
		if queryHit {
			queryHits++
		}
	}
	elapsedSeconds := time.Since(startedAll).Seconds()

	observedRows, err := store.Counts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var statusRows []struct {
		Status string
		Count  int64
	}
	if err := store.db.WithContext(ctx).Model(&ShoppingTaskRecord{}).Select("status, COUNT(*) AS count").
		Where("tenant_id = ?", tenantID).Group("status").Scan(&statusRows).Error; err != nil {
		t.Fatal(err)
	}
	taskStatuses := make(map[string]int64, len(statusRows))
	for _, row := range statusRows {
		taskStatuses[row.Status] = row.Count
	}
	forbiddenEffects := make(map[string]int64)
	for _, table := range []string{"quotes", "confirmations", "confirmation_attempts", "orders", "reservations",
		"inventory_adjustments", "simulated_payments", "simulated_fulfillment_attempts"} {
		forbiddenEffects[table] = observedRows[table]
	}
	expectedRows := map[string]int64{"merchants": 1, "products": int64(sampleManifest.CandidateRows),
		"inventory": int64(sampleManifest.CandidateRows), "buyers": int64(len(groups)), "tasks": int64(len(groups)),
		"intent_parse_traces": int64(len(groups))}
	violations := make([]string, 0)
	for table, expected := range expectedRows {
		if observedRows[table] != expected {
			violations = append(violations, fmt.Sprintf("%s=%d want %d", table, observedRows[table], expected))
		}
	}
	for table, count := range forbiddenEffects {
		if count != 0 {
			violations = append(violations, fmt.Sprintf("unexpected %s side effects=%d", table, count))
		}
	}
	if len(taskStatuses) != 1 || taskStatuses[TaskStatusDiscovered] != int64(len(groups)) {
		violations = append(violations, fmt.Sprintf("unexpected task statuses: %v", taskStatuses))
	}
	if orderMatches != len(groups) {
		violations = append(violations, fmt.Sprintf("API/ranker order matches=%d want %d", orderMatches, len(groups)))
	}
	if len(violations) > 0 {
		t.Fatalf("ESCI HTTP/MySQL audit failed: %s", strings.Join(violations, "; "))
	}

	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
	if err := cleanupMySQLIntegrationTenant(cleanupCtx, store, tenantID); err != nil {
		cleanupCancel()
		t.Fatalf("clean ESCI reference tenant: %v", err)
	}
	cleanedRows, err := store.Counts(cleanupCtx)
	cleanupCancel()
	if err != nil {
		t.Fatal(err)
	}
	for table, count := range cleanedRows {
		if count != 0 {
			t.Fatalf("ESCI reference tenant cleanup left %d rows in %s", count, table)
		}
	}
	cleanupVerified = true

	metrics := esciHTTPReferenceMetrics{SchemaVersion: "stablepay.esci-http-mysql-reference.v1", Status: "PASS",
		RunID: "esci-http-mysql-" + time.Now().UTC().Format("20060102T150405.000000000Z"), TenantID: tenantID,
		Dataset: sampleManifest.Dataset, DatasetSplit: sampleManifest.SourceSplit, SampleSHA256: sampleHash,
		SourceSHA256: sampleManifest.SourceSHA256, QueryCount: len(groups), JudgedCandidateRows: len(products),
		MerchantListingRows: len(products), SearchRegion: region, Parser: "deterministic structured-intent fixture; no LLM",
		NewModelCalls: 0, HTTPRequestCount: len(groups), HTTPStatusCounts: statusCounts,
		RankerOrderMatches: orderMatches, ExactTargets: totalExact, ExactTargetsRetrievedAt5: totalRetrieved,
		ExactRecallMacroAt5: macroRecall / float64(len(groups)), ExactRecallMicroAt5: float64(totalRetrieved) / float64(totalExact),
		ExactTargetQueryHitRateAt5: float64(queryHits) / float64(len(groups)), NDCGMacroAt5: macroNDCG / float64(len(groups)),
		TaskHTTPP50Millis: lifecyclePercentile(latencies, 0.50), TaskHTTPP95Millis: lifecyclePercentile(latencies, 0.95),
		QueriesPerSecond:   float64(len(groups)) / math.Max(0.001, elapsedSeconds),
		ObservedTenantRows: observedRows, TaskStatuses: taskStatuses, ForbiddenPurchaseSideEffects: forbiddenEffects,
		TenantCleanupVerified: cleanupVerified,
		Scope:                 "Authenticated in-process HTTP API plus isolated MySQL overlay; ESCI test-split judged candidates only; deterministic reference intent routing; synthetic USDC price/stock/region overlay; no unjudged full-catalog negatives, real inventory, purchase, payment, fulfillment, or model-quality measurement."}
	audit := esciHTTPReferenceAudit{Passed: true, ExpectedRows: expectedRows, ObservedRows: observedRows,
		TaskStatuses: taskStatuses, ForbiddenEffects: forbiddenEffects, TenantRowsRemoved: cleanupVerified,
		Violations: violations}

	outputRoot := filepath.Join(repoRoot, ".local-run", "agent-shopping", "reference-evaluations")
	if err := os.MkdirAll(outputRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	outputDir := filepath.Join(outputRoot, metrics.RunID)
	if err := os.Mkdir(outputDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "metrics.json"), metrics); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "audit-summary.json"), audit); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"schema_version": metrics.SchemaVersion, "run_id": metrics.RunID, "status": metrics.Status,
		"sample_path":   filepath.ToSlash(filepath.Join(".local-run", "agent-shopping", "reference-data", relativeSample)),
		"sample_sha256": sampleHash, "sample_manifest_sha256": hashBytesHex(manifestBytes),
		"dataset_split": sampleManifest.SourceSplit, "task_count": len(groups), "candidate_count": len(products),
		"tenant_cleanup_verified": cleanupVerified, "new_model_calls": 0, "raw_query_text_written": false,
		"catalog_source":   "public ESCI judged candidates projected into one synthetic Merchant fixture",
		"payment_provider": "NOT_USED", "real_settlement": false, "scope": metrics.Scope}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleLines(filepath.Join(outputDir, "trials.jsonl"), trials); err != nil {
		t.Fatal(err)
	}
	t.Logf("ESCI_HTTP_MYSQL_REFERENCE status=PASS queries=%d candidates=%d Exact=%d Recall@5=%.4f/%.4f hit=%.4f NDCG@5=%.4f API_rank_order=%d/%d P95_ms=%.3f cleanup=%v evidence=%s",
		metrics.QueryCount, metrics.JudgedCandidateRows, metrics.ExactTargets, metrics.ExactRecallMacroAt5,
		metrics.ExactRecallMicroAt5, metrics.ExactTargetQueryHitRateAt5, metrics.NDCGMacroAt5,
		metrics.RankerOrderMatches, metrics.QueryCount, metrics.TaskHTTPP95Millis, metrics.TenantCleanupVerified, outputDir)
}

func esciLabelGrade(label string) int {
	return map[string]int{"E": 3, "S": 2, "C": 1, "I": 0}[label]
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func hashBytesHex(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}
