package agentshopping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const purchaseReplayAttemptCount = 300
const purchaseReplayMarkerHeader = "X-Test-Purchase-Replay-Key"

var errInjectedPostCommitResponseLoss = errors.New("injected response loss after server commit")

type postCommitResponseLossTransport struct {
	base    http.RoundTripper
	target  string
	dropped atomic.Bool
}

func (t *postCommitResponseLossTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil || request.Header.Get(purchaseReplayMarkerHeader) != t.target || !t.dropped.CompareAndSwap(false, true) {
		return response, err
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	return nil, errInjectedPostCommitResponseLoss
}

type duplicateRequestBarrierTransport struct {
	base    http.RoundTripper
	target  string
	arrived atomic.Int32
	release chan struct{}
	once    sync.Once
}

func newDuplicateRequestBarrierTransport(base http.RoundTripper, target string) *duplicateRequestBarrierTransport {
	return &duplicateRequestBarrierTransport{base: base, target: target, release: make(chan struct{})}
}

func (t *duplicateRequestBarrierTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Header.Get(purchaseReplayMarkerHeader) == t.target {
		if t.arrived.Add(1) == 2 {
			t.once.Do(func() { close(t.release) })
		}
		select {
		case <-t.release:
		case <-time.After(15 * time.Second):
			return nil, errors.New("timed out waiting for the concurrent duplicate request")
		}
	}
	return t.base.RoundTrip(request)
}

type purchaseReplayTrial struct {
	TaskID             string  `json:"task_id"`
	MerchantID         string  `json:"merchant_id"`
	SKU                string  `json:"sku_id"`
	FaultMode          string  `json:"fault_mode"`
	OrderIdempotency   string  `json:"order_idempotency_key"`
	ResponseLost       bool    `json:"post_commit_response_lost"`
	APIRestarted       bool    `json:"api_recreated_before_retry"`
	ConcurrentRequests int     `json:"concurrent_checkout_requests"`
	OrderID            string  `json:"order_id"`
	PaymentID          string  `json:"payment_id"`
	OrderStatus        string  `json:"order_status"`
	PaymentStatus      string  `json:"mock_payment_status"`
	LatencyMillis      float64 `json:"end_to_end_latency_ms"`
}

type purchaseReplayAudit struct {
	Passed                          bool                  `json:"passed"`
	Lifecycle                       lifecycleAuditSummary `json:"lifecycle_audit"`
	DuplicateOrderIdempotencyGroups int64                 `json:"duplicate_order_idempotency_groups"`
	DuplicateOrdersPerTask          int64                 `json:"duplicate_orders_per_task"`
	DuplicatePaymentsPerOrder       int64                 `json:"duplicate_payments_per_order"`
	DuplicateSettlementsPerOrder    int64                 `json:"duplicate_settlements_per_order"`
	AmountOrCurrencyMismatches      int64                 `json:"amount_or_currency_mismatches"`
	ObservedSettlements             int64                 `json:"observed_settlements"`
	ObservedDeclines                int64                 `json:"observed_declines"`
	QueryErrors                     map[string]int64      `json:"query_errors,omitempty"`
	InvariantViolations             map[string]int64      `json:"invariant_violations,omitempty"`
}

type purchaseReplayMetrics struct {
	Status                   string              `json:"status"`
	RunID                    string              `json:"run_id"`
	TenantID                 string              `json:"tenant_id"`
	Database                 string              `json:"database"`
	AttemptCount             int                 `json:"logical_purchase_attempts"`
	UniqueOrders             int                 `json:"unique_orders"`
	UniquePayments           int                 `json:"unique_mock_payments"`
	SettledOrders            int                 `json:"settled_unique_orders"`
	DeclinedOrders           int                 `json:"declined_unique_orders"`
	PostCommitResponseLosses int                 `json:"injected_post_commit_response_losses"`
	APIRecreationsAfterLoss  int                 `json:"api_recreations_after_response_loss"`
	ConcurrentDuplicatePairs int                 `json:"concurrent_duplicate_request_pairs"`
	CheckoutRequests         int                 `json:"checkout_http_requests"`
	TaskP50Millis            float64             `json:"task_http_lifecycle_p50_ms"`
	TaskP95Millis            float64             `json:"task_http_lifecycle_p95_ms"`
	Hashes                   EvaluationHashes    `json:"hashes"`
	Audit                    purchaseReplayAudit `json:"audit"`
}

func TestMySQLHTTP300PurchaseReplayFaults(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_HTTP_300_PURCHASE_REPLAY") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_HTTP_300_PURCHASE_REPLAY=1 to run 300 isolated mock purchase/replay trials")
	}
	password := strings.TrimSpace(os.Getenv("SHOP_SIM_DB_PASSWORD"))
	if password == "" {
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

	allTasks, err := readLifecycleSuite(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	var intentRun httpLifecycleIntentRun
	if err := readLifecycleJSON(intentMetricsPath, &intentRun); err != nil {
		t.Fatal(err)
	}
	if intentRun.Provider != "openai-codex" || intentRun.Model != "gpt-6-luna" ||
		intentRun.Metrics.TaskCount != 500 || intentRun.Metrics.RawModelExactMatches != 500 || intentRun.Metrics.ExactIntentMatches != 500 {
		t.Fatalf("purchase replay source is not the locked 500-task GPT-6 Luna run: run=%q provider=%q model=%q task_count=%d",
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
	tasks, err := selectBalancedPurchaseReplayTasks(allTasks, purchaseReplayAttemptCount)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated purchase replay MySQL: %v", err)
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
	tenantID := "shopping-purchase-replay-" + suffix
	catalog, buyers, err := cloneLifecycleTenant(ctx, store, sourceTenant, tenantID, 20261010)
	if err != nil {
		t.Fatal(err)
	}
	store.tenant = tenantID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cleanupCancel()
		if err := cleanupMySQLIntegrationTenant(cleanupCtx, store, tenantID); err != nil {
			t.Errorf("clean isolated purchase replay tenant %q: %v", tenantID, err)
			return
		}
		counts, err := store.Counts(cleanupCtx)
		if err != nil {
			t.Errorf("verify purchase replay tenant cleanup: %v", err)
			return
		}
		for table, count := range counts {
			if count != 0 {
				t.Errorf("purchase replay tenant retained %d rows in %s", count, table)
			}
		}
	})
	if err := ValidateLockedSuite(catalog, allTasks); err != nil {
		t.Fatalf("locked task suite validation: %v", err)
	}
	if len(tasks) != purchaseReplayAttemptCount {
		t.Fatalf("selected %d tasks, want %d", len(tasks), purchaseReplayAttemptCount)
	}
	fullHashes, err := ComputeEvaluationHashes(catalog, allTasks, manifestBytes)
	if err != nil || fullHashes != intentRun.Hashes {
		t.Fatalf("locked intent source hashes do not match: computed=%+v stored=%+v err=%v", fullHashes, intentRun.Hashes, err)
	}
	hashes, err := ComputeEvaluationHashes(catalog, tasks, manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	merchantCatalog, err := LoadMerchantCatalog(ctx, sourceManifestPath, nil)
	if err != nil {
		t.Fatalf("load Merchant HTTP snapshot: %v", err)
	}
	if err := store.ValidateMerchantCatalogSnapshot(ctx, merchantCatalog); err != nil {
		t.Fatalf("Merchant HTTP snapshot differs from isolated MySQL seed: %v", err)
	}
	merchantHashes, err := ComputeEvaluationHashes(merchantCatalog, allTasks, manifestBytes)
	if err != nil || merchantHashes != fullHashes {
		t.Fatalf("Merchant HTTP snapshot hash differs from locked run: live=%+v mysql=%+v err=%v", merchantHashes, fullHashes, err)
	}
	parser := replayLifecycleIntents(t, allTasks, intentTrials, intentRun.PromptTemplateSHA256)
	apiHandler, err := NewHTTPAPIWithMerchantCatalog(store, parser, mysqlIntegrationAPIKey, merchantCatalog)
	if err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	startServer := func() error {
		handler, err := NewHTTPAPIWithMerchantCatalog(store, parser, mysqlIntegrationAPIKey, merchantCatalog)
		if err != nil {
			return err
		}
		server = httptest.NewServer(handler)
		return nil
	}
	server = httptest.NewServer(apiHandler)
	t.Cleanup(func() {
		if server != nil {
			server.Close()
		}
	})
	apiClient := &mysqlIntegrationAPI{client: server.Client(), server: server, key: mysqlIntegrationAPIKey}
	client := &lifecycleHTTPClient{api: apiClient, statusCounts: make(map[int]int64)}
	ready := client.do(t, http.MethodGet, "/readyz", nil)
	if ready.status != http.StatusOK {
		t.Fatalf("HTTP API readiness status=%d body=%s", ready.status, ready.body)
	}
	dataset := client.do(t, http.MethodGet, "/v1/dataset", nil)
	var datasetResponse struct {
		TenantID string `json:"tenant_id"`
	}
	if dataset.status != http.StatusOK || json.Unmarshal(dataset.body, &datasetResponse) != nil || datasetResponse.TenantID != tenantID {
		t.Fatalf("API escaped isolated tenant: status=%d body=%s", dataset.status, dataset.body)
	}

	outputDir, runID, err := lifecycleEvidenceDirectory(repoRoot, "purchase-replay-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema_version": "stablepay.agent-shopping-http-purchase-replay.v1", "run_id": runID,
		"status": "RUNNING", "tenant_id": tenantID, "database": SimulationDatabaseName,
		"source_intent_run_id": intentRun.RunID, "source_provider": parser.Provider(), "source_model": parser.Model(),
		"new_model_calls": 0, "payment_provider": "simulation-only", "real_settlement": false,
		"catalog_source": "merchant_http_snapshot", "catalog_merchants": len(merchantCatalog.Merchants),
		"catalog_products": len(merchantCatalog.Products), "task_count": len(tasks), "hashes": hashes,
		"fault_schedule":         map[string]int{"post_commit_response_loss_retry": 100, "post_commit_response_loss_api_recreate_retry": 100, "concurrent_duplicate_request_pair": 100},
		"idempotency_definition": "one unique order and at most one settled simulated payment per predeclared logical order idempotency key",
		"created_at_utc":         time.Now().UTC(),
	}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if !t.Failed() {
			return
		}
		manifest["status"] = "FAILED"
		manifest["audit_passed"] = false
		if err := writeLifecycleJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
			t.Errorf("mark failed purchase replay manifest: %v", err)
		}
	}()

	buyersByRegion := make(map[string][]BuyerRecord)
	for _, buyer := range buyers {
		buyersByRegion[buyer.HomeRegion] = append(buyersByRegion[buyer.HomeRegion], buyer)
	}
	usedBuyers := make(map[string]bool, len(tasks))
	trials := make([]purchaseReplayTrial, 0, len(tasks))
	latencies := make([]float64, 0, len(tasks))
	var settled, declined, responseLosses, apiRecreations, concurrentPairs, checkoutRequests int
	for index, task := range tasks {
		started := time.Now()
		buyer, ok := chooseLifecycleBuyer(buyersByRegion[task.Intent.Region], task.Intent.MaxPriceMinor, usedBuyers)
		if !ok {
			t.Fatalf("no unique buyer for %s in region %s", task.TaskID, task.Intent.Region)
		}
		usedBuyers[buyer.BuyerID] = true
		parsed := client.do(t, http.MethodPost, "/v1/intents/parse", map[string]any{
			"buyer_id": buyer.BuyerID, "prompt": task.Prompt, "idempotency_key": "purchase-replay-parse-" + task.TaskID,
		})
		var parseResult struct {
			TaskID     string      `json:"task_id"`
			TaskStatus string      `json:"task_status"`
			Intent     Intent      `json:"intent"`
			Candidates []Candidate `json:"candidates"`
			Provider   string      `json:"provider"`
			Model      string      `json:"model"`
			Consent    bool        `json:"buyer_confirmation_required"`
		}
		if parsed.status != http.StatusOK || json.Unmarshal(parsed.body, &parseResult) != nil ||
			parseResult.TaskID == "" || parseResult.TaskStatus != TaskStatusDiscovered || parseResult.Intent != task.Intent ||
			parseResult.Provider != "openai-codex" || parseResult.Model != "gpt-6-luna" || !parseResult.Consent || len(parseResult.Candidates) == 0 {
			t.Fatalf("intent replay/discovery failed for %s: status=%d body=%s", task.TaskID, parsed.status, parsed.body)
		}
		candidate := parseResult.Candidates[0]
		isGold := false
		for _, sku := range task.GoldSKUs {
			if candidate.Product.SKUID == sku {
				isGold = true
				break
			}
		}
		if !isGold {
			t.Fatalf("selected candidate %s for %s is outside its gold set", candidate.Product.SKUID, task.TaskID)
		}
		quote := issueHTTPQuote(t, client, parseResult.TaskID, buyer.BuyerID, candidate, task.Intent,
			"purchase-replay-quote-"+task.TaskID)
		confirmation := confirmHTTPLifecycle(t, client, quote, "purchase-replay-confirm-"+task.TaskID, http.StatusCreated)
		if confirmation.confirmation.ID == "" || !confirmation.confirmation.Accepted || confirmation.confirmation.AmountMinor != quote.AmountMinor {
			t.Fatalf("buyer consent was not bound to quote for %s", task.TaskID)
		}
		orderKey := "purchase-replay-order-" + task.TaskID
		orderRequest := map[string]any{"confirmation_id": confirmation.confirmation.ID, "idempotency_key": orderKey}
		marker := map[string]string{purchaseReplayMarkerHeader: orderKey}
		trial := purchaseReplayTrial{TaskID: task.TaskID, MerchantID: candidate.Product.MerchantID, SKU: candidate.Product.SKUID,
			OrderIdempotency: orderKey, ConcurrentRequests: 2}
		var checkout mysqlIntegrationCheckoutResponse
		var committedOrderID, committedPaymentID string
		if index%3 == 0 || index%3 == 1 {
			trial.ResponseLost = true
			responseLosses++
			base := client.api.client.Transport
			if base == nil {
				base = http.DefaultTransport
			}
			drop := &postCommitResponseLossTransport{base: base, target: orderKey}
			client.api.client.Transport = drop
			_, requestErr := client.api.doRawWithHeaders(http.MethodPost, "/v1/orders", orderRequest, marker)
			client.api.client.Transport = base
			client.requests++
			checkoutRequests++
			if !errors.Is(requestErr, errInjectedPostCommitResponseLoss) || !drop.dropped.Load() {
				t.Fatalf("expected injected post-commit response loss for %s, got %v dropped=%v", task.TaskID, requestErr, drop.dropped.Load())
			}
			var committedOrder OrderRecord
			if err := store.db.WithContext(ctx).Where("tenant_id = ? AND idempotency_key = ?", tenantID, orderKey).First(&committedOrder).Error; err != nil {
				t.Fatalf("response was lost before durable order commit for %s: %v", task.TaskID, err)
			}
			var committedPayment MockPaymentRecord
			if err := store.db.WithContext(ctx).Where("tenant_id = ? AND order_id = ?", tenantID, committedOrder.OrderID).First(&committedPayment).Error; err != nil {
				t.Fatalf("response was lost before durable payment effect for %s: %v", task.TaskID, err)
			}
			committedOrderID, committedPaymentID = committedOrder.OrderID, committedPayment.PaymentID
			if index%3 == 1 {
				server.Close()
				if err := startServer(); err != nil {
					t.Fatalf("recreate HTTP API after committed checkout: %v", err)
				}
				client.api.server, client.api.client = server, server.Client()
				trial.APIRestarted = true
			}
			checkout = checkoutHTTPLifecycle(t, client, confirmation.confirmation.ID, orderKey)
			checkoutRequests++
			if checkout.Order.ID != committedOrderID || checkout.Payment.ID != committedPaymentID {
				t.Fatalf("idempotent retry changed committed transaction identity for %s: order=%s/%s payment=%s/%s",
					task.TaskID, committedOrderID, checkout.Order.ID, committedPaymentID, checkout.Payment.ID)
			}
		} else {
			trial.FaultMode = "CONCURRENT_DUPLICATE_REPLAY"
			base := client.api.client.Transport
			if base == nil {
				base = http.DefaultTransport
			}
			barrier := newDuplicateRequestBarrierTransport(base, orderKey)
			client.api.client.Transport = barrier
			type rawResult struct {
				response mysqlIntegrationResponse
				err      error
			}
			results := make(chan rawResult, 2)
			var wait sync.WaitGroup
			for duplicate := 0; duplicate < 2; duplicate++ {
				wait.Add(1)
				go func() {
					defer wait.Done()
					response, err := client.api.doRawWithHeaders(http.MethodPost, "/v1/orders", orderRequest, marker)
					results <- rawResult{response: response, err: err}
				}()
			}
			wait.Wait()
			close(results)
			client.api.client.Transport = base
			client.requests += 2
			checkoutRequests += 2
			var first *mysqlIntegrationResponse
			for result := range results {
				if result.err != nil || result.response.status != http.StatusCreated {
					t.Fatalf("concurrent duplicate checkout failed for %s: status=%d err=%v body=%s", task.TaskID, result.response.status, result.err, result.response.body)
				}
				client.statusCounts[result.response.status]++
				if first == nil {
					copy := result.response
					first = &copy
				} else if !sameCheckoutIdentity(t, *first, result.response) {
					t.Fatalf("concurrent replay returned different order/payment identities for %s", task.TaskID)
				}
			}
			if first == nil {
				t.Fatalf("concurrent checkout returned no responses for %s", task.TaskID)
			}
			checkout = decodePurchaseReplayCheckout(t, *first)
			trial.FaultMode = "CONCURRENT_DUPLICATE_REPLAY"
			concurrentPairs++
		}
		if trial.FaultMode == "" {
			trial.FaultMode = "POST_COMMIT_RESPONSE_LOSS_RETRY"
			if trial.APIRestarted {
				trial.FaultMode = "POST_COMMIT_RESPONSE_LOSS_API_RECREATE_RETRY"
				apiRecreations++
			}
		}
		if checkout.Order.ID == "" || checkout.Payment.ID == "" || checkout.Order.IdempotencyKey != orderKey ||
			!checkout.SimulationOnly || checkout.RealSettlement || checkout.Payment.Provider != "simulation-only" ||
			!checkout.Payment.SimulationOnly || checkout.ChainTransaction != nil ||
			checkout.Order.Status != paymentTerminalTaskStatus(checkout.Payment.Status) {
			t.Fatalf("purchase result violates simulation/idempotency/status contract for %s: %+v", task.TaskID, checkout)
		}
		expectedStatus := simulatedPaymentStatus(orderKey)
		if checkout.Payment.Status != expectedStatus {
			t.Fatalf("mock outcome for %s=%s, deterministic policy expects %s", task.TaskID, checkout.Payment.Status, expectedStatus)
		}
		if checkout.Payment.Status == PaymentSettled {
			settled++
		} else if checkout.Payment.Status == PaymentDeclined {
			declined++
		} else {
			t.Fatalf("unexpected simulated payment state %q for %s", checkout.Payment.Status, task.TaskID)
		}
		trial.OrderID, trial.PaymentID = checkout.Order.ID, checkout.Payment.ID
		trial.OrderStatus, trial.PaymentStatus = checkout.Order.Status, checkout.Payment.Status
		trial.LatencyMillis = float64(time.Since(started).Microseconds()) / 1000
		latencies = append(latencies, trial.LatencyMillis)
		trials = append(trials, trial)
		if index == 0 || (index+1)%50 == 0 {
			t.Logf("purchase replay progress %d/%d settled=%d declined=%d response_losses=%d concurrent_pairs=%d",
				index+1, len(tasks), settled, declined, responseLosses, concurrentPairs)
		}
	}

	lifecycleAudit := auditLockedLifecycle(ctx, store, tenantID, len(tasks), 0, 0)
	audit := auditPurchaseReplay(ctx, store, tenantID, lifecycleAudit)
	if !audit.Passed || !audit.Lifecycle.Passed || audit.DuplicateOrderIdempotencyGroups != 0 || audit.DuplicateOrdersPerTask != 0 ||
		audit.DuplicatePaymentsPerOrder != 0 || audit.DuplicateSettlementsPerOrder != 0 || audit.AmountOrCurrencyMismatches != 0 ||
		audit.ObservedSettlements != int64(settled) || audit.ObservedDeclines != int64(declined) || settled+declined != len(tasks) {
		t.Fatalf("purchase replay SQL audit failed: %+v", audit)
	}
	uniqueOrders := make(map[string]bool, len(trials))
	uniquePayments := make(map[string]bool, len(trials))
	for _, trial := range trials {
		if uniqueOrders[trial.OrderID] || uniquePayments[trial.PaymentID] {
			t.Fatalf("duplicate order/payment identity across distinct logical tasks: %+v", trial)
		}
		uniqueOrders[trial.OrderID], uniquePayments[trial.PaymentID] = true, true
	}
	if len(uniqueOrders) != len(tasks) || len(uniquePayments) != len(tasks) || responseLosses != 200 || apiRecreations != 100 || concurrentPairs != 100 {
		t.Fatalf("purchase replay coverage mismatch: orders=%d payments=%d losses=%d api_recreations=%d concurrent=%d",
			len(uniqueOrders), len(uniquePayments), responseLosses, apiRecreations, concurrentPairs)
	}
	metrics := purchaseReplayMetrics{Status: "PASS", RunID: runID, TenantID: tenantID, Database: SimulationDatabaseName,
		AttemptCount: len(tasks), UniqueOrders: len(uniqueOrders), UniquePayments: len(uniquePayments),
		SettledOrders: settled, DeclinedOrders: declined, PostCommitResponseLosses: responseLosses,
		APIRecreationsAfterLoss: apiRecreations, ConcurrentDuplicatePairs: concurrentPairs, CheckoutRequests: checkoutRequests,
		TaskP50Millis: lifecyclePercentile(latencies, 0.50), TaskP95Millis: lifecyclePercentile(latencies, 0.95), Hashes: hashes, Audit: audit}
	manifest["status"] = metrics.Status
	manifest["checkout_http_requests"] = checkoutRequests
	manifest["api_recreations_after_response_loss"] = apiRecreations
	manifest["audit_passed"] = audit.Lifecycle.Passed
	if err := writeLifecycleJSON(filepath.Join(outputDir, "metrics.json"), metrics); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "audit-summary.json"), audit); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleLines(filepath.Join(outputDir, "trials.jsonl"), trials); err != nil {
		t.Fatal(err)
	}
	t.Logf("MYSQL_HTTP_PURCHASE_REPLAY_300 status=PASS attempts=%d settled=%d declined=%d dropped_responses=%d api_recreations=%d concurrent_pairs=%d duplicate_settlements=%d evidence=%s",
		len(tasks), settled, declined, responseLosses, apiRecreations, concurrentPairs, audit.DuplicateSettlementsPerOrder, outputDir)
}

func selectBalancedPurchaseReplayTasks(all []Task, count int) ([]Task, error) {
	if len(all) < count || count <= 0 {
		return nil, fmt.Errorf("cannot select %d purchase replay tasks from %d", count, len(all))
	}
	byMerchant := make(map[string][]Task)
	for _, task := range all {
		byMerchant[task.SourceMerchantID] = append(byMerchant[task.SourceMerchantID], task)
	}
	merchants := make([]string, 0, len(byMerchant))
	for merchant := range byMerchant {
		merchants = append(merchants, merchant)
	}
	sort.Strings(merchants)
	if len(merchants) != 12 || count%len(merchants) != 0 {
		return nil, fmt.Errorf("balanced replay suite requires 12 merchants and a divisible task count: %w", ErrInvalidEvaluationSuite)
	}
	perMerchant := count / len(merchants)
	selected := make([]Task, 0, count)
	for _, merchant := range merchants {
		if len(byMerchant[merchant]) < perMerchant {
			return nil, fmt.Errorf("merchant %s has only %d tasks, need %d", merchant, len(byMerchant[merchant]), perMerchant)
		}
		selected = append(selected, byMerchant[merchant][:perMerchant]...)
	}
	return selected, nil
}

func sameCheckoutIdentity(t *testing.T, left, right mysqlIntegrationResponse) bool {
	t.Helper()
	leftCheckout := decodePurchaseReplayCheckout(t, left)
	rightCheckout := decodePurchaseReplayCheckout(t, right)
	return leftCheckout.Order.ID == rightCheckout.Order.ID && leftCheckout.Payment.ID == rightCheckout.Payment.ID
}

func decodePurchaseReplayCheckout(t *testing.T, response mysqlIntegrationResponse) mysqlIntegrationCheckoutResponse {
	t.Helper()
	if response.status != http.StatusCreated {
		t.Fatalf("HTTP purchase checkout status=%d body=%s", response.status, response.body)
	}
	var result mysqlIntegrationCheckoutResponse
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func auditPurchaseReplay(ctx context.Context, store *MySQLStore, tenant string, lifecycle lifecycleAuditSummary) purchaseReplayAudit {
	queries := map[string]string{
		"duplicate_order_idempotency_groups": `SELECT COUNT(*) FROM (
			SELECT idempotency_key FROM sim_orders WHERE tenant_id=? GROUP BY idempotency_key HAVING COUNT(*)>1) AS duplicates`,
		"duplicate_orders_per_task": `SELECT COUNT(*) FROM (
			SELECT task_id FROM sim_orders WHERE tenant_id=? GROUP BY task_id HAVING COUNT(*)>1) AS duplicates`,
		"duplicate_payments_per_order": `SELECT COUNT(*) FROM (
			SELECT order_id FROM simulated_payments WHERE tenant_id=? GROUP BY order_id HAVING COUNT(*)>1) AS duplicates`,
		"duplicate_settlements_per_order": `SELECT COUNT(*) FROM (
			SELECT order_id FROM simulated_payments WHERE tenant_id=? AND status='SETTLED' GROUP BY order_id HAVING COUNT(*)>1) AS duplicates`,
		"amount_or_currency_mismatches": `SELECT COUNT(*) FROM sim_orders o JOIN simulated_payments p
			ON p.tenant_id=o.tenant_id AND p.order_id=o.order_id WHERE o.tenant_id=?
			AND (o.amount_minor<>p.amount_minor OR o.currency<>p.currency OR p.provider<>'simulation-only')`,
	}
	counts := make(map[string]int64, len(queries))
	queryErrors := make(map[string]int64)
	violations := make(map[string]int64)
	for name, query := range queries {
		var count int64
		if err := store.db.WithContext(ctx).Raw(query, tenant).Scan(&count).Error; err != nil {
			queryErrors[name] = 1
		} else {
			counts[name] = count
			if count != 0 {
				violations[name] = count
			}
		}
	}
	var settlements, declines int64
	if err := store.db.WithContext(ctx).Model(&MockPaymentRecord{}).Where("tenant_id=? AND status=?", tenant, PaymentSettled).Count(&settlements).Error; err != nil {
		queryErrors["settlements"] = 1
	}
	if err := store.db.WithContext(ctx).Model(&MockPaymentRecord{}).Where("tenant_id=? AND status=?", tenant, PaymentDeclined).Count(&declines).Error; err != nil {
		queryErrors["declines"] = 1
	}
	if !lifecycle.Passed {
		violations["lifecycle_audit"] = 1
	}
	return purchaseReplayAudit{Passed: len(queryErrors) == 0 && len(violations) == 0, Lifecycle: lifecycle,
		DuplicateOrderIdempotencyGroups: counts["duplicate_order_idempotency_groups"],
		DuplicateOrdersPerTask:          counts["duplicate_orders_per_task"],
		DuplicatePaymentsPerOrder:       counts["duplicate_payments_per_order"],
		DuplicateSettlementsPerOrder:    counts["duplicate_settlements_per_order"],
		AmountOrCurrencyMismatches:      counts["amount_or_currency_mismatches"],
		ObservedSettlements:             settlements, ObservedDeclines: declines,
		QueryErrors: queryErrors, InvariantViolations: violations}
}

func TestSelectBalancedPurchaseReplayTasks(t *testing.T) {
	all := make([]Task, 0, 12*41)
	for merchant := 0; merchant < 12; merchant++ {
		for index := 0; index < 41; index++ {
			all = append(all, Task{TaskID: fmt.Sprintf("m%02d-task-%02d", merchant, index),
				SourceMerchantID: fmt.Sprintf("merchant-%02d", merchant)})
		}
	}
	selected, err := selectBalancedPurchaseReplayTasks(all, purchaseReplayAttemptCount)
	if err != nil {
		t.Fatalf("select balanced replay tasks: %v", err)
	}
	if len(selected) != purchaseReplayAttemptCount {
		t.Fatalf("selected %d tasks, want %d", len(selected), purchaseReplayAttemptCount)
	}
	counts := make(map[string]int)
	seen := make(map[string]bool)
	for _, task := range selected {
		if seen[task.TaskID] {
			t.Fatalf("duplicate selected task %s", task.TaskID)
		}
		seen[task.TaskID] = true
		counts[task.SourceMerchantID]++
	}
	if len(counts) != 12 {
		t.Fatalf("selected tasks span %d merchants, want 12", len(counts))
	}
	for merchant, count := range counts {
		if count != 25 {
			t.Errorf("merchant %s has %d selected tasks, want 25", merchant, count)
		}
	}
}
