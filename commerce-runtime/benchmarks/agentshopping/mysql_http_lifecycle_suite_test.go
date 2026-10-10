package agentshopping

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type httpLifecycleIntentRun struct {
	RunID                string                `json:"run_id"`
	Provider             string                `json:"provider"`
	Model                string                `json:"model"`
	PromptTemplateSHA256 string                `json:"prompt_template_sha256"`
	Hashes               EvaluationHashes      `json:"hashes"`
	Metrics              IntentParseEvaluation `json:"metrics"`
}

type replayLifecycleIntentParser struct {
	provider     string
	model        string
	templateHash string
	intents      map[string]Intent
}

func (p replayLifecycleIntentParser) Provider() string { return p.provider }
func (p replayLifecycleIntentParser) Model() string    { return p.model }

func (p replayLifecycleIntentParser) Parse(_ context.Context, prompt, _ string, _ int64, _ Taxonomy) (Intent, error) {
	intent, ok := p.intents[prompt]
	if !ok {
		return Intent{}, errors.New("no recorded model result matches this synthetic prompt")
	}
	return intent, nil
}

func (p replayLifecycleIntentParser) ParseWithUsage(ctx context.Context, prompt, region string, budget int64,
	taxonomy Taxonomy) (Intent, ParseUsage, error) {
	intent, err := p.Parse(ctx, prompt, region, budget, taxonomy)
	usage := ParseUsage{TemplateSHA256: p.templateHash, PromptSHA256: hashValue(prompt),
		Reasoning: "none", UsageStatus: "NOT_APPLICABLE_RECORDED_RESPONSE_REPLAY",
		APICallsStatus: "NOT_APPLICABLE_REPLAY_NO_MODEL_CALL"}
	if err == nil {
		encoded, marshalErr := json.Marshal(intent)
		if marshalErr != nil {
			return Intent{}, usage, marshalErr
		}
		usage.ResponseSHA256 = hashValue(string(encoded))
	}
	return intent, usage, err
}

type httpLifecycleTrial struct {
	lifecycleSuiteTrial
	ParseExactReplay bool `json:"recorded_gpt6_luna_intent_exact"`
	HTTPRequests     int  `json:"http_requests"`
}

type httpLifecycleMetrics struct {
	Status                 string                `json:"status"`
	RunID                  string                `json:"run_id"`
	TenantID               string                `json:"tenant_id"`
	Database               string                `json:"database"`
	SourceIntentRunID      string                `json:"source_intent_run_id"`
	Provider               string                `json:"provider"`
	Model                  string                `json:"model"`
	ParserMode             string                `json:"parser_mode"`
	PaymentOutcomePolicy   string                `json:"payment_outcome_policy"`
	NewModelCalls          int                   `json:"new_model_calls"`
	TokenUsage             string                `json:"token_usage"`
	TaskCount              int                   `json:"task_count"`
	HTTPRequestCount       int64                 `json:"http_request_count"`
	HTTPStatusCounts       map[string]int64      `json:"http_status_counts"`
	ExpectedStaleConflicts int64                 `json:"expected_stale_quote_conflicts"`
	UnexpectedHTTPFailures int64                 `json:"unexpected_http_failures"`
	TaskP50Millis          float64               `json:"task_http_lifecycle_p50_ms"`
	TaskP95Millis          float64               `json:"task_http_lifecycle_p95_ms"`
	TasksPerSecond         float64               `json:"terminal_tasks_per_second"`
	Lifecycle              lifecycleSuiteMetrics `json:"lifecycle"`
}

type lifecycleHTTPClient struct {
	api          *mysqlIntegrationAPI
	statusCounts map[int]int64
	requests     int64
}

func (c *lifecycleHTTPClient) do(t *testing.T, method, path string, body any) mysqlIntegrationResponse {
	t.Helper()
	response := c.api.do(t, method, path, body)
	c.requests++
	c.statusCounts[response.status]++
	return response
}

func TestMySQLShoppingLocked500HTTPLifecycle(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_HTTP_LIFECYCLE") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_HTTP_LIFECYCLE=1 to run the recorded GPT-6 Luna 500-task HTTP lifecycle against isolated MySQL")
	}
	password := os.Getenv("SHOP_SIM_DB_PASSWORD")
	if strings.TrimSpace(password) == "" {
		t.Fatal("SHOP_SIM_DB_PASSWORD is required")
	}
	repoRoot, err := filepath.Abs(os.Getenv("SHOP_SIM_REPO_ROOT"))
	if err != nil || repoRoot == "." {
		t.Fatal("SHOP_SIM_REPO_ROOT must point to the StablePay repository")
	}
	suitePath, intentMetricsPath, sourceManifestPath := os.Getenv("SHOP_SIM_LOCKED_SUITE_PATH"),
		os.Getenv("SHOP_SIM_INTENT_METRICS_PATH"), os.Getenv("SHOP_SIM_SOURCE_MANIFEST_PATH")
	for label, path := range map[string]string{"locked suite": suitePath, "intent metrics": intentMetricsPath, "source manifest": sourceManifestPath} {
		if path == "" {
			t.Fatalf("missing %s path", label)
		}
	}

	tasks, err := readLifecycleSuite(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	var intentRun httpLifecycleIntentRun
	if err := readLifecycleJSON(intentMetricsPath, &intentRun); err != nil {
		t.Fatal(err)
	}
	if intentRun.Provider != "openai-codex" || intentRun.Model != "gpt-6-luna" ||
		intentRun.Metrics.TaskCount != 500 || intentRun.Metrics.RawModelExactMatches != 500 ||
		intentRun.Metrics.ExactIntentMatches != 500 {
		t.Fatalf("intent replay source is not the locked 500/500 GPT-6 Luna run: run=%q provider=%q model=%q metrics=%+v",
			intentRun.RunID, intentRun.Provider, intentRun.Model, intentRun.Metrics)
	}
	intentTrials, err := readLifecycleIntentTrials(filepath.Join(filepath.Dir(intentMetricsPath), "intent-trials.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(sourceManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated HTTP lifecycle MySQL: %v", err)
		}
	})
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	sourceTenant := store.tenant
	suffix := mysqlIntegrationSuffix(t)
	tenantID := "shopping-http-" + suffix
	catalog, buyers, err := cloneLifecycleTenant(ctx, store, sourceTenant, tenantID, 20261010)
	if err != nil {
		t.Fatal(err)
	}
	store.tenant = tenantID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cleanupCancel()
		if err := cleanupMySQLIntegrationTenant(cleanupCtx, store, tenantID); err != nil {
			t.Errorf("clean isolated HTTP lifecycle tenant %q: %v", tenantID, err)
			return
		}
		counts, err := store.Counts(cleanupCtx)
		if err != nil {
			t.Errorf("verify HTTP lifecycle tenant cleanup: %v", err)
			return
		}
		for table, count := range counts {
			if count != 0 {
				t.Errorf("HTTP lifecycle tenant retained %d rows in %s", count, table)
			}
		}
	})
	if len(tasks) != 500 || len(intentTrials) != len(tasks) {
		t.Fatalf("locked task dimensions tasks=%d intent_trials=%d, want 500/500", len(tasks), len(intentTrials))
	}
	if err := ValidateLockedSuite(catalog, tasks); err != nil {
		t.Fatalf("locked suite validation: %v", err)
	}
	hashes, err := ComputeEvaluationHashes(catalog, tasks, manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	if hashes != intentRun.Hashes {
		t.Fatalf("intent replay input hashes differ from locked catalog/suite: current=%+v source=%+v", hashes, intentRun.Hashes)
	}
	parser := replayLifecycleIntents(t, tasks, intentTrials, intentRun.PromptTemplateSHA256)
	store.tenant = tenantID
	api, err := NewHTTPAPI(store, parser, mysqlIntegrationAPIKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	rawClient := &mysqlIntegrationAPI{client: server.Client(), server: server, key: mysqlIntegrationAPIKey}
	client := &lifecycleHTTPClient{api: rawClient, statusCounts: make(map[int]int64)}

	ready := client.do(t, http.MethodGet, "/readyz", nil)
	if ready.status != http.StatusOK || !strings.Contains(string(ready.body), `"status":"ready"`) {
		t.Fatalf("HTTP lifecycle API readiness status=%d body=%s", ready.status, ready.body)
	}
	datasetResponse := client.do(t, http.MethodGet, "/v1/dataset", nil)
	var dataset struct {
		TenantID string `json:"tenant_id"`
	}
	if datasetResponse.status != http.StatusOK || json.Unmarshal(datasetResponse.body, &dataset) != nil || dataset.TenantID != tenantID {
		t.Fatalf("HTTP API escaped the isolated lifecycle tenant: status=%d body=%s", datasetResponse.status, datasetResponse.body)
	}
	outputDir, runID, err := lifecycleEvidenceDirectory(repoRoot, "http-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"schema_version": "stablepay.agent-shopping-http-lifecycle.v1", "run_id": runID,
		"status": "RUNNING", "database": SimulationDatabaseName, "tenant_id": tenantID,
		"source_tenant_id": sourceTenant, "seed": 20261010, "hashes": hashes,
		"intent_source":        "recorded GPT-6 Luna parsed intents; replayed through the HTTP API; no model call in this run",
		"source_intent_run_id": intentRun.RunID, "provider": parser.Provider(), "model": parser.Model(),
		"new_model_calls": 0, "token_usage": "NOT_APPLICABLE_RECORDED_RESPONSE_REPLAY",
		"payment_provider": "simulation-only", "payment_outcome_policy": "stable idempotency-key hash; 95% settled / 5% declined",
		"real_settlement":  false,
		"http_boundary":    "net/http loopback httptest server with persistent isolated MySQL",
		"api_key_recorded": false, "created_at_utc": time.Now().UTC()}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}

	metrics := lifecycleSuiteMetrics{RunID: runID, TenantID: tenantID, Database: SimulationDatabaseName,
		Seed: 20261010, Hashes: hashes, TaskCount: len(tasks), ModelCalls: 0,
		TokenUsage: "NOT_APPLICABLE_RECORDED_RESPONSE_REPLAY", StructuredB0ValidPlans: intentRun.Metrics.DownstreamBaseline.ValidPlans,
		StructuredB1ValidPlans: intentRun.Metrics.DownstreamTreatment.ValidPlans}
	byRegion := make(map[string][]BuyerRecord)
	for _, buyer := range buyers {
		byRegion[buyer.HomeRegion] = append(byRegion[buyer.HomeRegion], buyer)
	}
	usedBuyers := make(map[string]bool, len(tasks))
	trials := make([]httpLifecycleTrial, 0, len(tasks))
	latencies := make([]float64, 0, len(tasks))
	startedAll := time.Now()
	for index, task := range tasks {
		buyer, ok := chooseLifecycleBuyer(byRegion[task.Intent.Region], task.Intent.MaxPriceMinor, usedBuyers)
		if !ok {
			t.Fatalf("no unique HTTP-discovered buyer for %s in %s under %d minor units", task.TaskID, task.Intent.Region, task.Intent.MaxPriceMinor)
		}
		usedBuyers[buyer.BuyerID] = true
		beforeRequests := client.requests
		trial := runHTTPShoppingLifecycleTask(t, client, task, buyer)
		requestCount := int(client.requests - beforeRequests)
		trials = append(trials, httpLifecycleTrial{lifecycleSuiteTrial: trial, ParseExactReplay: true, HTTPRequests: requestCount})
		metrics.CandidateCountTotal += int64(trial.Candidates)
		if trial.SelectedIsGold {
			metrics.TopCandidateGoldHits++
		}
		if trial.PlanValid {
			metrics.ValidPlans++
		}
		if trial.OrderID != "" && (trial.OrderStatus == TaskStatusPaid || trial.OrderStatus == TaskStatusPaymentFailed) {
			metrics.CompletedFlows++
		}
		if trial.OrderStatus == TaskStatusPaid && trial.PaymentStatus == PaymentSettled {
			metrics.PaidOrders++
		}
		if trial.OrderStatus == TaskStatusPaymentFailed && trial.PaymentStatus == PaymentDeclined {
			metrics.DeclinedPayments++
		}
		if trial.Mutation == "PRICE_INCREASE" {
			metrics.PriceChangeCases++
		}
		if trial.Mutation == "STOCK_LOSS" {
			metrics.StockChangeCases++
		}
		if trial.StaleQuoteRejected {
			metrics.StaleQuoteConfirmations++
		}
		latencies = append(latencies, trial.LatencyMillis)
		if index == 0 || (index+1)%100 == 0 {
			t.Logf("HTTP lifecycle progress %d/%d plans=%d terminal=%d http_requests=%d",
				index+1, len(tasks), metrics.ValidPlans, metrics.CompletedFlows, client.requests)
		}
	}
	metrics.CandidateCountMean = float64(metrics.CandidateCountTotal) / float64(metrics.TaskCount)
	metrics.ValidPlanRate = float64(metrics.ValidPlans) / float64(metrics.TaskCount)
	metrics.CompletedFlowRate = float64(metrics.CompletedFlows) / float64(metrics.TaskCount)
	metrics.EndToEndP50Millis = lifecyclePercentile(latencies, 0.50)
	metrics.EndToEndP95Millis = lifecyclePercentile(latencies, 0.95)
	var expectedPaid, expectedDeclined int
	for _, task := range tasks {
		if simulatedPaymentStatus("http-lifecycle-order-"+task.TaskID) == PaymentSettled {
			expectedPaid++
		} else {
			expectedDeclined++
		}
	}
	if metrics.PaidOrders != expectedPaid || metrics.DeclinedPayments != expectedDeclined {
		t.Fatalf("mock payment outcomes differ from the stable idempotency-key schedule: got=%d/%d want=%d/%d",
			metrics.PaidOrders, metrics.DeclinedPayments, expectedPaid, expectedDeclined)
	}
	metrics.Audit = auditLockedLifecycle(ctx, store, tenantID, len(tasks), metrics.PriceChangeCases, metrics.StockChangeCases)

	statusCounts := make(map[string]int64, len(client.statusCounts))
	var unexpectedFailures int64
	for status, count := range client.statusCounts {
		statusCounts[fmt.Sprint(status)] = count
		if status >= http.StatusBadRequest && status != http.StatusConflict {
			unexpectedFailures += count
		}
	}
	var totalRequests int64
	for _, count := range client.statusCounts {
		totalRequests += count
	}
	passed := httpLifecyclePassGate(len(trials), metrics, client.statusCounts, unexpectedFailures)
	status := "FAIL"
	if passed {
		status = "PASS"
	}
	fullMetrics := httpLifecycleMetrics{Status: status, RunID: runID, TenantID: tenantID, Database: SimulationDatabaseName,
		SourceIntentRunID: intentRun.RunID, Provider: parser.Provider(), Model: parser.Model(),
		ParserMode: "recorded_response_replay", PaymentOutcomePolicy: "stable idempotency-key hash; 95% settled / 5% declined",
		NewModelCalls: 0, TokenUsage: "NOT_APPLICABLE_RECORDED_RESPONSE_REPLAY",
		TaskCount: len(tasks), HTTPRequestCount: totalRequests, HTTPStatusCounts: statusCounts,
		ExpectedStaleConflicts: int64(metrics.PriceChangeCases + metrics.StockChangeCases),
		UnexpectedHTTPFailures: unexpectedFailures, TaskP50Millis: metrics.EndToEndP50Millis,
		TaskP95Millis: metrics.EndToEndP95Millis, TasksPerSecond: float64(len(tasks)) / time.Since(startedAll).Seconds(),
		Lifecycle: metrics}
	manifest["status"] = fullMetrics.Status
	manifest["http_request_count"] = fullMetrics.HTTPRequestCount
	manifest["http_status_counts"] = fullMetrics.HTTPStatusCounts
	manifest["audit_passed"] = metrics.Audit.Passed
	for name, value := range map[string]any{"metrics.json": fullMetrics, "audit-summary.json": metrics.Audit} {
		if err := writeLifecycleJSON(filepath.Join(outputDir, name), value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeLifecycleLines(filepath.Join(outputDir, "trials.jsonl"), trials); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	t.Logf("MYSQL_HTTP_LIFECYCLE_500 status=%s tenant=%s intent_replay=%d/%d plans=%d/%d terminal=%d/%d paid=%d declined=%d price=%d stock=%d stale=%d audit=%v http=%d expected_conflicts=%d unexpected_failures=%d p95_ms=%.2f tasks_per_sec=%.2f evidence=%s",
		fullMetrics.Status, tenantID, intentRun.Metrics.RawModelExactMatches, len(tasks), metrics.ValidPlans, len(tasks),
		metrics.CompletedFlows, len(tasks), metrics.PaidOrders, metrics.DeclinedPayments, metrics.PriceChangeCases,
		metrics.StockChangeCases, metrics.StaleQuoteConfirmations, metrics.Audit.Passed, totalRequests,
		fullMetrics.ExpectedStaleConflicts, unexpectedFailures, fullMetrics.TaskP95Millis, fullMetrics.TasksPerSecond, outputDir)
	if !passed {
		t.Errorf("500-task HTTP lifecycle/audit gate failed: %s", outputDir)
	}
}

func httpLifecyclePassGate(trialCount int, metrics lifecycleSuiteMetrics, statusCounts map[int]int64,
	unexpectedFailures int64) bool {
	return trialCount == 500 && metrics.ValidPlans == 500 && metrics.TopCandidateGoldHits == 500 &&
		metrics.CompletedFlows == 500 && metrics.PaidOrders+metrics.DeclinedPayments == 500 &&
		metrics.PriceChangeCases == 50 && metrics.StockChangeCases == 50 && metrics.StaleQuoteConfirmations == 100 &&
		statusCounts[http.StatusConflict] == 100 && unexpectedFailures == 0 && metrics.Audit.Passed
}

func TestHTTPShoppingLifecyclePassGate(t *testing.T) {
	metrics := lifecycleSuiteMetrics{ValidPlans: 500, TopCandidateGoldHits: 500, CompletedFlows: 500,
		PaidOrders: 483, DeclinedPayments: 17, PriceChangeCases: 50, StockChangeCases: 50,
		StaleQuoteConfirmations: 100, Audit: lifecycleAuditSummary{Passed: true}}
	statuses := map[int]int64{http.StatusOK: 2152, http.StatusCreated: 2100, http.StatusConflict: 100}
	if !httpLifecyclePassGate(500, metrics, statuses, 0) {
		t.Fatal("valid locked HTTP lifecycle run should pass its gate")
	}
	metrics.Audit.Passed = false
	if httpLifecyclePassGate(500, metrics, statuses, 0) {
		t.Fatal("SQL audit failure must fail the locked HTTP lifecycle gate")
	}
	metrics.Audit.Passed = true
	statuses[http.StatusConflict] = 99
	if httpLifecyclePassGate(500, metrics, statuses, 0) {
		t.Fatal("unexpected stale-conflict count must fail the locked HTTP lifecycle gate")
	}
	statuses[http.StatusConflict] = 100
	if httpLifecyclePassGate(500, metrics, statuses, 1) {
		t.Fatal("unexpected HTTP failures must fail the locked HTTP lifecycle gate")
	}
}

func replayLifecycleIntents(t *testing.T, tasks []Task, trials []IntentParseTrial, templateHash string) replayLifecycleIntentParser {
	t.Helper()
	byTask := make(map[string]IntentParseTrial, len(trials))
	for _, trial := range trials {
		if _, exists := byTask[trial.TaskID]; exists {
			t.Fatalf("duplicate recorded intent result for %s", trial.TaskID)
		}
		byTask[trial.TaskID] = trial
	}
	intents := make(map[string]Intent, len(tasks))
	for _, task := range tasks {
		trial, ok := byTask[task.TaskID]
		if !ok || !trial.ParseValid || !trial.ExactIntentMatch || trial.RawIntent == nil ||
			*trial.RawIntent != task.Intent || trial.ParsedIntent != task.Intent {
			t.Fatalf("recorded GPT-6 Luna parse does not exactly match locked intent for %s", task.TaskID)
		}
		if _, exists := intents[task.Prompt]; exists {
			t.Fatalf("duplicate natural-language prompt in locked suite: %s", task.TaskID)
		}
		intents[task.Prompt] = trial.ParsedIntent
		delete(byTask, task.TaskID)
	}
	if len(byTask) != 0 {
		t.Fatalf("recorded intent artifact contains %d tasks outside the locked suite", len(byTask))
	}
	return replayLifecycleIntentParser{provider: "openai-codex", model: "gpt-6-luna", templateHash: templateHash, intents: intents}
}

func readLifecycleIntentTrials(path string) ([]IntentParseTrial, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	trials := make([]IntentParseTrial, 0, 500)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var trial IntentParseTrial
		if err := json.Unmarshal(scanner.Bytes(), &trial); err != nil {
			return nil, fmt.Errorf("decode recorded intent result %d: %w", len(trials)+1, err)
		}
		trials = append(trials, trial)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return trials, nil
}

func runHTTPShoppingLifecycleTask(t *testing.T, client *lifecycleHTTPClient, task Task, buyer BuyerRecord) lifecycleSuiteTrial {
	t.Helper()
	started := time.Now()
	trial := lifecycleSuiteTrial{TaskID: task.TaskID, Scenario: task.Scenario, Mutation: task.Mutation}
	parseKey := "http-lifecycle-parse-" + task.TaskID
	parsed := client.do(t, http.MethodPost, "/v1/intents/parse", map[string]any{
		"buyer_id": buyer.BuyerID, "prompt": task.Prompt, "idempotency_key": parseKey,
	})
	if parsed.status != http.StatusOK {
		t.Fatalf("parse %s status=%d body=%s", task.TaskID, parsed.status, parsed.body)
	}
	var parseResult struct {
		TaskID                    string      `json:"task_id"`
		TaskStatus                string      `json:"task_status"`
		Intent                    Intent      `json:"intent"`
		Candidates                []Candidate `json:"candidates"`
		Provider                  string      `json:"provider"`
		Model                     string      `json:"model"`
		BuyerConfirmationRequired bool        `json:"buyer_confirmation_required"`
	}
	if err := json.Unmarshal(parsed.body, &parseResult); err != nil {
		t.Fatal(err)
	}
	if parseResult.TaskID == "" || parseResult.TaskStatus != TaskStatusDiscovered || parseResult.Intent != task.Intent ||
		parseResult.Provider != "openai-codex" || parseResult.Model != "gpt-6-luna" || !parseResult.BuyerConfirmationRequired ||
		len(parseResult.Candidates) == 0 {
		t.Fatalf("HTTP parse/discovery result violates locked intent contract for %s: %+v", task.TaskID, parseResult)
	}
	candidate := parseResult.Candidates[0]
	trial.Candidates = len(parseResult.Candidates)
	trial.SelectedSKU = candidate.Product.SKUID
	trial.InitialPriceMinor, trial.FinalPriceMinor = candidate.Inventory.PriceMinor, candidate.Inventory.PriceMinor
	for _, gold := range task.GoldSKUs {
		if candidate.Product.SKUID == gold {
			trial.SelectedIsGold = true
			break
		}
	}
	quote := issueHTTPQuote(t, client, parseResult.TaskID, buyer.BuyerID, candidate, task.Intent,
		"initial-quote-"+task.TaskID)
	if task.Mutation != "NONE" {
		reason, key := "", "mutation-"+task.TaskID
		var price *int64
		var onHand *int
		switch task.Mutation {
		case "PRICE_INCREASE":
			newPrice := quote.AmountMinor + 1
			if newPrice > task.Intent.MaxPriceMinor {
				t.Fatalf("task %s has no budget headroom for locked price change", task.TaskID)
			}
			price, reason = &newPrice, "LOCKED_PRICE_INCREASE"
			trial.FinalPriceMinor = newPrice
		case "STOCK_LOSS":
			zero := 0
			onHand, reason = &zero, "LOCKED_STOCK_LOSS"
		default:
			t.Fatalf("unknown locked mutation %q", task.Mutation)
		}
		adjustLifecycleInventory(t, client, parseResult.TaskID, candidate, task.Intent.Region, reason, key, price, onHand)
		stale := confirmHTTPLifecycle(t, client, quote, "stale-confirm-"+task.TaskID, http.StatusConflict)
		if stale.errorCode != "STALE_QUOTE" {
			t.Fatalf("stale confirmation for %s returned %q, want STALE_QUOTE", task.TaskID, stale.errorCode)
		}
		trial.StaleQuoteRejected = true
		if task.Mutation == "STOCK_LOSS" {
			restored := candidate.Inventory.OnHand
			adjustLifecycleInventory(t, client, parseResult.TaskID, candidate, task.Intent.Region,
				"LOCKED_STOCK_RESTORED_FOR_RECONFIRMATION", "restore-"+task.TaskID, nil, &restored)
		}
		quote = issueHTTPQuote(t, client, parseResult.TaskID, buyer.BuyerID, candidate, task.Intent,
			"fresh-quote-"+task.TaskID)
	}
	confirmation := confirmHTTPLifecycle(t, client, quote, "accepted-confirm-"+task.TaskID, http.StatusCreated)
	if confirmation.confirmation.ID == "" || !confirmation.confirmation.Accepted || confirmation.confirmation.AmountMinor != quote.AmountMinor {
		t.Fatalf("fresh buyer consent was not persisted for %s: %+v", task.TaskID, confirmation.confirmation)
	}
	trial.FreshConsentRecorded = true
	trial.PlanValid = trial.SelectedIsGold && quote.AmountMinor <= task.Intent.MaxPriceMinor && trial.FreshConsentRecorded
	orderKey := "http-lifecycle-order-" + task.TaskID
	checkout := checkoutHTTPLifecycle(t, client, confirmation.confirmation.ID, orderKey)
	if checkout.Order.ID == "" || checkout.Payment.ID == "" || !checkout.SimulationOnly || checkout.RealSettlement ||
		!checkout.Payment.SimulationOnly || checkout.Payment.Provider != "simulation-only" || checkout.ChainTransaction != nil {
		t.Fatalf("checkout response escaped simulation-only payment boundary for %s: %+v", task.TaskID, checkout)
	}
	terminal := paymentTerminalTaskStatus(checkout.Payment.Status)
	if checkout.Order.Status != terminal {
		t.Fatalf("order/payment states disagree for %s: order=%s payment=%s", task.TaskID, checkout.Order.Status, checkout.Payment.Status)
	}
	trial.OrderID, trial.OrderStatus, trial.PaymentStatus = checkout.Order.ID, checkout.Order.Status, checkout.Payment.Status
	trial.TaskStatus = terminal

	replay := checkoutHTTPLifecycle(t, client, confirmation.confirmation.ID, orderKey)
	if replay.Order.ID != checkout.Order.ID || replay.Payment.ID != checkout.Payment.ID {
		t.Fatalf("idempotent checkout changed transaction identity for %s", task.TaskID)
	}
	readOrder := client.do(t, http.MethodGet, "/v1/orders/"+checkout.Order.ID, nil)
	var persisted struct {
		Order   Order       `json:"order"`
		Payment MockPayment `json:"payment"`
	}
	if readOrder.status != http.StatusOK || json.Unmarshal(readOrder.body, &persisted) != nil ||
		persisted.Order.ID != checkout.Order.ID || persisted.Payment.ID != checkout.Payment.ID || persisted.Order.Status != terminal {
		t.Fatalf("persisted HTTP order readback mismatch for %s: status=%d body=%s", task.TaskID, readOrder.status, readOrder.body)
	}
	readTask := client.do(t, http.MethodGet, "/v1/tasks/"+parseResult.TaskID, nil)
	var taskState struct {
		TaskStatus string `json:"task_status"`
		Provider   string `json:"provider"`
		Model      string `json:"model"`
	}
	if readTask.status != http.StatusOK || json.Unmarshal(readTask.body, &taskState) != nil ||
		taskState.TaskStatus != terminal || taskState.Provider != "openai-codex" || taskState.Model != "gpt-6-luna" {
		t.Fatalf("persisted HTTP task terminal state mismatch for %s: status=%d body=%s", task.TaskID, readTask.status, readTask.body)
	}
	parseReplay := client.do(t, http.MethodPost, "/v1/intents/parse", map[string]any{
		"buyer_id": buyer.BuyerID, "prompt": task.Prompt, "idempotency_key": parseKey,
	})
	var replayResult struct {
		TaskID           string `json:"task_id"`
		TaskStatus       string `json:"task_status"`
		IdempotentReplay bool   `json:"idempotent_replay"`
	}
	if parseReplay.status != http.StatusOK || json.Unmarshal(parseReplay.body, &replayResult) != nil ||
		replayResult.TaskID != parseResult.TaskID || replayResult.TaskStatus != terminal || !replayResult.IdempotentReplay {
		t.Fatalf("idempotent parse replay changed task identity/state for %s: status=%d body=%s", task.TaskID, parseReplay.status, parseReplay.body)
	}
	trial.LatencyMillis = float64(time.Since(started).Microseconds()) / 1000
	return trial
}

func issueHTTPQuote(t *testing.T, client *lifecycleHTTPClient, taskID, buyerID string, candidate Candidate,
	intent Intent, idempotencyKey string) Quote {
	t.Helper()
	response := client.do(t, http.MethodPost, "/v1/quotes", map[string]any{
		"task_id": taskID, "buyer_id": buyerID, "merchant_id": candidate.Product.MerchantID,
		"sku_id": candidate.Product.SKUID, "region": intent.Region, "max_price_minor": intent.MaxPriceMinor,
		"currency": "USDC", "idempotency_key": idempotencyKey,
	})
	if response.status != http.StatusCreated {
		t.Fatalf("HTTP quote status=%d body=%s", response.status, response.body)
	}
	var result mysqlIntegrationQuoteResponse
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	if result.Quote.ID == "" || result.Quote.AmountMinor > intent.MaxPriceMinor || result.Quote.SKUID != candidate.Product.SKUID {
		t.Fatalf("invalid HTTP quote returned: %+v", result.Quote)
	}
	return result.Quote
}

func adjustLifecycleInventory(t *testing.T, client *lifecycleHTTPClient, taskID string, candidate Candidate,
	region, reason, idempotencyKey string, price *int64, onHand *int) {
	t.Helper()
	request := map[string]any{"task_id": taskID, "merchant_id": candidate.Product.MerchantID,
		"sku_id": candidate.Product.SKUID, "region": region, "reason": reason, "idempotency_key": idempotencyKey}
	if price != nil {
		request["price_minor"] = *price
	}
	if onHand != nil {
		request["on_hand"] = *onHand
	}
	response := client.do(t, http.MethodPost, "/v1/admin/inventory-adjustments", request)
	if response.status != http.StatusOK {
		t.Fatalf("HTTP inventory adjustment status=%d body=%s", response.status, response.body)
	}
}

func confirmHTTPLifecycle(t *testing.T, client *lifecycleHTTPClient, quote Quote, key string, expectedStatus int) struct {
	confirmation Confirmation
	errorCode    string
} {
	t.Helper()
	response := client.do(t, http.MethodPost, "/v1/quotes/"+quote.ID+"/confirm", map[string]any{
		"buyer_id": quote.BuyerID, "accepted": true, "amount_minor": quote.AmountMinor, "idempotency_key": key,
	})
	if response.status != expectedStatus {
		t.Fatalf("HTTP confirmation status=%d want=%d body=%s", response.status, expectedStatus, response.body)
	}
	if expectedStatus == http.StatusConflict {
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal(response.body, &envelope); err != nil {
			t.Fatal(err)
		}
		return struct {
			confirmation Confirmation
			errorCode    string
		}{errorCode: envelope.Error.Code}
	}
	var result mysqlIntegrationConfirmationResponse
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	return struct {
		confirmation Confirmation
		errorCode    string
	}{confirmation: result.Confirmation}
}

func checkoutHTTPLifecycle(t *testing.T, client *lifecycleHTTPClient, confirmationID, idempotencyKey string) mysqlIntegrationCheckoutResponse {
	t.Helper()
	response := client.do(t, http.MethodPost, "/v1/orders", map[string]any{
		"confirmation_id": confirmationID, "idempotency_key": idempotencyKey,
	})
	if response.status != http.StatusCreated {
		t.Fatalf("HTTP checkout status=%d body=%s", response.status, response.body)
	}
	var result mysqlIntegrationCheckoutResponse
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
