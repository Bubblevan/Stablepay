package agentshopping

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMySQLHTTPOSProcessRestartPurchaseReplay(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_HTTP_PROCESS_RESTART") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_HTTP_PROCESS_RESTART=1 to run one isolated OS-process restart purchase replay")
	}
	password := strings.TrimSpace(os.Getenv("SHOP_SIM_DB_PASSWORD"))
	if password == "" {
		t.Fatal("SHOP_SIM_DB_PASSWORD is required")
	}
	repoRoot, err := filepath.Abs(os.Getenv("SHOP_SIM_REPO_ROOT"))
	if err != nil || repoRoot == "." {
		t.Fatal("SHOP_SIM_REPO_ROOT must point to the StablePay repository")
	}
	suitePath := strings.TrimSpace(os.Getenv("SHOP_SIM_LOCKED_SUITE_PATH"))
	intentMetricsPath := strings.TrimSpace(os.Getenv("SHOP_SIM_INTENT_METRICS_PATH"))
	sourceManifestPath := strings.TrimSpace(os.Getenv("SHOP_SIM_SOURCE_MANIFEST_PATH"))
	for label, path := range map[string]string{
		"locked suite": suitePath, "intent metrics": intentMetricsPath, "source manifest": sourceManifestPath,
	} {
		if path == "" {
			t.Fatalf("missing %s path", label)
		}
	}

	allTasks, err := readLifecycleSuite(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	var intentRun httpLifecycleIntentRun
	if err := readLifecycleJSON(intentMetricsPath, &intentRun); err != nil {
		t.Fatal(err)
	}
	if intentRun.Provider != "openai-codex" || intentRun.Model != "gpt-6-luna" ||
		intentRun.Metrics.TaskCount != 500 || intentRun.Metrics.ExactIntentMatches != 500 {
		t.Fatalf("source is not the locked 500-task GPT-6 Luna run: run=%q provider=%q model=%q count=%d",
			intentRun.RunID, intentRun.Provider, intentRun.Model, intentRun.Metrics.TaskCount)
	}
	intentTrials, err := readLifecycleIntentTrials(filepath.Join(filepath.Dir(intentMetricsPath), "intent-trials.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(sourceManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated process-restart MySQL store: %v", err)
		}
	})
	if err := store.Ping(ctx); err != nil {
		t.Fatal("isolated simulation MySQL is not ready")
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal("isolated simulation schema migration failed")
	}
	sourceTenant := store.tenant
	suffix := mysqlIntegrationSuffix(t)
	tenantID := "shopping-purchase-replay-process-" + suffix
	catalog, buyers, err := cloneLifecycleTenant(ctx, store, sourceTenant, tenantID, 20261010)
	if err != nil {
		t.Fatal(err)
	}
	store.tenant = tenantID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cleanupCancel()
		if err := cleanupMySQLIntegrationTenant(cleanupCtx, store, tenantID); err != nil {
			t.Errorf("clean isolated process-restart tenant %q: %v", tenantID, err)
			return
		}
		counts, err := store.Counts(cleanupCtx)
		if err != nil {
			t.Errorf("verify process-restart tenant cleanup: %v", err)
			return
		}
		for table, count := range counts {
			if count != 0 {
				t.Errorf("process-restart tenant retained %d rows in %s", count, table)
			}
		}
	})
	if err := ValidateLockedSuite(catalog, allTasks); err != nil {
		t.Fatalf("locked task suite validation: %v", err)
	}
	fullHashes, err := ComputeEvaluationHashes(catalog, allTasks, manifestBytes)
	if err != nil || fullHashes != intentRun.Hashes {
		t.Fatalf("locked task/catalog hashes differ from GPT-6 Luna run: computed=%+v stored=%+v err=%v",
			fullHashes, intentRun.Hashes, err)
	}
	if len(allTasks) == 0 {
		t.Fatal("locked task suite is empty")
	}
	task := allTasks[0]
	var recordedIntent *IntentParseTrial
	for i := range intentTrials {
		if intentTrials[i].TaskID == task.TaskID {
			recordedIntent = &intentTrials[i]
			break
		}
	}
	if recordedIntent == nil {
		t.Fatalf("locked intent run has no result for task %s", task.TaskID)
	}
	parser := replayLifecycleIntents(t, []Task{task}, []IntentParseTrial{*recordedIntent}, intentRun.PromptTemplateSHA256)
	apiHandler, err := NewHTTPAPI(store, parser, mysqlIntegrationAPIKey, mysqlIntegrationBuyerApprovalKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(apiHandler)
	t.Cleanup(server.Close)
	api := &mysqlIntegrationAPI{client: server.Client(), server: server, key: mysqlIntegrationAPIKey}
	client := &lifecycleHTTPClient{api: api, statusCounts: make(map[int]int64)}

	buyersByRegion := make(map[string][]BuyerRecord)
	for _, buyer := range buyers {
		buyersByRegion[buyer.HomeRegion] = append(buyersByRegion[buyer.HomeRegion], buyer)
	}
	buyer, ok := chooseLifecycleBuyer(buyersByRegion[task.Intent.Region], task.Intent.MaxPriceMinor, map[string]bool{})
	if !ok {
		t.Fatalf("no buyer for task %s in region %s", task.TaskID, task.Intent.Region)
	}
	parsed := client.do(t, http.MethodPost, "/v1/intents/parse", map[string]any{
		"buyer_id": buyer.BuyerID, "prompt": task.Prompt, "idempotency_key": "process-restart-parse-" + task.TaskID,
	})
	var parseResult struct {
		TaskID     string      `json:"task_id"`
		TaskStatus string      `json:"task_status"`
		Intent     Intent      `json:"intent"`
		Candidates []Candidate `json:"candidates"`
	}
	if parsed.status != http.StatusOK || json.Unmarshal(parsed.body, &parseResult) != nil || parseResult.TaskID == "" ||
		parseResult.TaskStatus != TaskStatusDiscovered || !sameChallengeIntent(parseResult.Intent, task.Intent) {
		t.Fatalf("could not create the isolated process-restart shopping task: status=%d", parsed.status)
	}
	var candidate *Candidate
	for i := range parseResult.Candidates {
		for _, sku := range task.GoldSKUs {
			if parseResult.Candidates[i].Product.SKUID == sku {
				candidate = &parseResult.Candidates[i]
				break
			}
		}
		if candidate != nil {
			break
		}
	}
	if candidate == nil {
		t.Fatalf("task %s has no feasible gold candidate in the MySQL-backed catalog", task.TaskID)
	}
	quote := issueHTTPQuote(t, client, parseResult.TaskID, buyer.BuyerID, *candidate, task.Intent,
		"process-restart-quote-"+task.TaskID)
	confirmation := confirmHTTPLifecycle(t, client, quote, "process-restart-confirm-"+task.TaskID, http.StatusCreated)
	if !confirmation.confirmation.Accepted || confirmation.confirmation.AmountMinor != quote.AmountMinor {
		t.Fatal("quote confirmation was not durably bound to its amount")
	}
	orderKey := "process-restart-order-" + task.TaskID
	orderRequest := map[string]any{"confirmation_id": confirmation.confirmation.ID, "idempotency_key": orderKey}
	marker := map[string]string{purchaseReplayMarkerHeader: orderKey}
	trial := purchaseReplayTrial{TaskID: task.TaskID, MerchantID: candidate.Product.MerchantID, SKU: candidate.Product.SKUID,
		FaultMode: "POST_COMMIT_RESPONSE_LOSS_OS_PROCESS_RESTART", OrderIdempotency: orderKey,
		ResponseLost: true, APIRestarted: true, OSProcessRestarted: true, ConcurrentRequests: 1}
	addr := unusedLoopbackAddress(t)
	process := startPurchaseReplayProcessServer(t, addr, tenantID)
	drop := &postCommitResponseLossTransport{base: http.DefaultTransport, target: orderKey}
	firstClient := &http.Client{Transport: drop, Timeout: 30 * time.Second}
	started := time.Now()
	_, requestErr := doPurchaseReplayProcessCheckout(firstClient, addr, orderRequest, marker)
	if !errors.Is(requestErr, errInjectedPostCommitResponseLoss) || !drop.dropped.Load() {
		t.Fatalf("expected client response loss after committed process checkout, observed err=%v dropped=%v", requestErr, drop.dropped.Load())
	}
	var committedOrder OrderRecord
	if err := store.db.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", tenantID, orderKey).First(&committedOrder).Error; err != nil {
		t.Fatalf("order was not durable before process restart: %v", err)
	}
	var committedPayment MockPaymentRecord
	if err := store.db.WithContext(ctx).Where("tenant_id = ? AND order_id = ?", tenantID, committedOrder.OrderID).First(&committedPayment).Error; err != nil {
		t.Fatalf("mock payment effect was not durable before process restart: %v", err)
	}
	stopPurchaseReplayProcessServer(process)
	process = startPurchaseReplayProcessServer(t, addr, tenantID)
	retryClient := &http.Client{Timeout: 30 * time.Second}
	retry, err := doPurchaseReplayProcessCheckout(retryClient, addr, orderRequest, nil)
	if err != nil || retry.status != http.StatusCreated {
		t.Fatalf("retry after OS process restart failed: status=%d err=%v body=%s", retry.status, err, retry.body)
	}
	checkout := decodePurchaseReplayCheckout(t, retry)
	if checkout.Order.ID != committedOrder.OrderID || checkout.Payment.ID != committedPayment.PaymentID ||
		checkout.Order.IdempotencyKey != orderKey || checkout.Payment.Provider != "simulation-only" ||
		!checkout.SimulationOnly || checkout.RealSettlement || checkout.ChainTransaction != nil {
		t.Fatalf("retry after OS process restart changed or escaped the mock transaction: order=%+v payment=%+v",
			checkout.Order, checkout.Payment)
	}
	stopPurchaseReplayProcessServer(process)
	trial.OrderID, trial.PaymentID = checkout.Order.ID, checkout.Payment.ID
	trial.OrderStatus, trial.PaymentStatus = checkout.Order.Status, checkout.Payment.Status
	trial.LatencyMillis = float64(time.Since(started).Microseconds()) / 1000

	lifecycleAudit := auditLockedLifecycle(ctx, store, tenantID, 1, 0, 0)
	audit := auditPurchaseReplay(ctx, store, tenantID, lifecycleAudit)
	if !audit.Passed || !audit.Lifecycle.Passed || audit.DuplicateOrderIdempotencyGroups != 0 ||
		audit.DuplicateOrdersPerTask != 0 || audit.DuplicatePaymentsPerOrder != 0 ||
		audit.DuplicateSettlementsPerOrder != 0 || audit.AmountOrCurrencyMismatches != 0 {
		t.Fatalf("live SQL audit failed after OS process restart: %+v", audit)
	}
	hashes, err := ComputeEvaluationHashes(catalog, []Task{task}, manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	outputDir, runID, err := lifecycleEvidenceDirectory(repoRoot, "os-process-restart-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema_version": "stablepay.agent-shopping-os-process-restart-replay.v1", "run_id": runID,
		"status": "PASS", "tenant_id": tenantID, "database": SimulationDatabaseName,
		"source_intent_run_id": intentRun.RunID, "source_provider": intentRun.Provider, "source_model": intentRun.Model,
		"new_model_calls": 0, "payment_provider": "simulation-only", "real_settlement": false,
		"catalog_source": "isolated_mysql_snapshot", "task_count": 1, "processes_started": 2,
		"post_commit_response_loss": true, "retry_after_os_process_restart": true,
		"api_key_recorded": false, "hashes": hashes, "created_at_utc": time.Now().UTC(),
	}
	metrics := map[string]any{
		"status": "PASS", "run_id": runID, "task_count": 1, "unique_orders": 1, "unique_mock_payments": 1,
		"os_process_restarts": 1, "response_losses": 1, "retry_returned_same_order_and_payment": true,
		"order_id": checkout.Order.ID, "payment_id": checkout.Payment.ID, "order_status": checkout.Order.Status,
		"mock_payment_status": checkout.Payment.Status, "payment_provider": "simulation-only", "real_settlement": false,
		"amount_minor": checkout.Payment.AmountMinor, "currency": checkout.Payment.Currency,
		"duplicate_order_idempotency_groups": audit.DuplicateOrderIdempotencyGroups,
		"duplicate_orders_per_task":          audit.DuplicateOrdersPerTask, "duplicate_payments_per_order": audit.DuplicatePaymentsPerOrder,
		"duplicate_settlements_per_order": audit.DuplicateSettlementsPerOrder,
		"amount_or_currency_mismatches":   audit.AmountOrCurrencyMismatches, "audit": audit, "hashes": hashes,
	}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "metrics.json"), metrics); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "audit-summary.json"), audit); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleLines(filepath.Join(outputDir, "trials.jsonl"), []purchaseReplayTrial{trial}); err != nil {
		t.Fatal(err)
	}
	t.Logf("MYSQL_HTTP_OS_PROCESS_RESTART status=PASS task=%s order=%s payment=%s outcome=%s evidence=%s",
		task.TaskID, checkout.Order.ID, checkout.Payment.ID, checkout.Payment.Status, outputDir)
}
