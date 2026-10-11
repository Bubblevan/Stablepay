package agentshopping

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
	TaskID                      string  `json:"task_id"`
	MerchantID                  string  `json:"merchant_id"`
	SKU                         string  `json:"sku_id"`
	FaultMode                   string  `json:"fault_mode"`
	OrderIdempotency            string  `json:"order_idempotency_key"`
	ResponseLost                bool    `json:"post_commit_response_lost"`
	APIRestarted                bool    `json:"api_recreated_before_retry"`
	OSProcessRestarted          bool    `json:"os_process_restarted_before_retry"`
	ConcurrentRequests          int     `json:"concurrent_checkout_requests"`
	OrderID                     string  `json:"order_id"`
	PaymentID                   string  `json:"payment_id"`
	OrderStatus                 string  `json:"order_status"`
	PaymentStatus               string  `json:"mock_payment_status"`
	FulfillmentFaultScheduled   bool    `json:"delivery_failure_predeclared"`
	FulfillmentProviderAttempts int     `json:"fulfillment_provider_attempts,omitempty"`
	TransientDeliveryFailures   int     `json:"transient_delivery_failures_observed,omitempty"`
	FulfillmentRetryRequests    int     `json:"fulfillment_retry_http_requests,omitempty"`
	FulfillmentStatus           string  `json:"fulfillment_status,omitempty"`
	RefundStatus                string  `json:"mock_refund_status,omitempty"`
	FinalOrderStatus            string  `json:"final_order_status"`
	FulfillmentLatencyMillis    float64 `json:"fulfillment_http_latency_ms,omitempty"`
	LatencyMillis               float64 `json:"end_to_end_latency_ms"`
}

type purchaseReplayAudit struct {
	Passed                          bool                     `json:"passed"`
	Lifecycle                       lifecycleAuditSummary    `json:"lifecycle_audit"`
	DuplicateOrderIdempotencyGroups int64                    `json:"duplicate_order_idempotency_groups"`
	DuplicateOrdersPerTask          int64                    `json:"duplicate_orders_per_task"`
	DuplicatePaymentsPerOrder       int64                    `json:"duplicate_payments_per_order"`
	DuplicateSettlementsPerOrder    int64                    `json:"duplicate_settlements_per_order"`
	AmountOrCurrencyMismatches      int64                    `json:"amount_or_currency_mismatches"`
	ObservedSettlements             int64                    `json:"observed_settlements"`
	ObservedDeclines                int64                    `json:"observed_declines"`
	Fulfillment                     purchaseFulfillmentAudit `json:"fulfillment_audit"`
	QueryErrors                     map[string]int64         `json:"query_errors,omitempty"`
	InvariantViolations             map[string]int64         `json:"invariant_violations,omitempty"`
}

type purchaseFulfillmentAudit struct {
	Passed                                  bool             `json:"passed"`
	ObservedFulfilledOrders                 int64            `json:"observed_fulfilled_orders"`
	ObservedCompensatedOrders               int64            `json:"observed_compensated_orders"`
	ObservedRefunds                         int64            `json:"observed_mock_refunds"`
	ObservedProviderAttempts                int64            `json:"observed_provider_attempts"`
	RetryableFailureAttempts                int64            `json:"retryable_failure_attempts"`
	DuplicateFulfillmentsPerOrder           int64            `json:"duplicate_fulfillments_per_order"`
	DuplicateAttemptOrdinals                int64            `json:"duplicate_attempt_ordinals"`
	AttemptSequenceGaps                     int64            `json:"attempt_sequence_gaps"`
	RetryableFailuresWithoutRetry           int64            `json:"retryable_failures_without_retry"`
	OrphanFulfillmentAttempts               int64            `json:"orphan_fulfillment_attempts"`
	FinalAttemptCountMismatches             int64            `json:"final_attempt_count_mismatches"`
	TerminalAttemptOutcomeMismatches        int64            `json:"terminal_attempt_outcome_mismatches"`
	AttemptIdempotencyMismatches            int64            `json:"attempt_idempotency_mismatches"`
	DuplicateRefundsPerOrder                int64            `json:"duplicate_refunds_per_order"`
	SettledPaymentsWithoutFulfillment       int64            `json:"settled_payments_without_fulfillment"`
	DeclinedPaymentsWithFulfillmentOrRefund int64            `json:"declined_payments_with_fulfillment_or_refund"`
	FinalOrderOrTaskStateMismatches         int64            `json:"final_order_or_task_state_mismatches"`
	RefundAmountOrPaymentMismatches         int64            `json:"refund_amount_or_payment_mismatches"`
	ReservationStateMismatches              int64            `json:"reservation_state_mismatches"`
	NonSimulationFulfillmentEffects         int64            `json:"non_simulation_fulfillment_effects"`
	QueryErrors                             map[string]int64 `json:"query_errors"`
	InvariantViolations                     map[string]int64 `json:"invariant_violations"`
}

type purchaseReplayMetrics struct {
	Status                      string              `json:"status"`
	RunID                       string              `json:"run_id"`
	TenantID                    string              `json:"tenant_id"`
	Database                    string              `json:"database"`
	AttemptCount                int                 `json:"logical_purchase_attempts"`
	UniqueOrders                int                 `json:"unique_orders"`
	UniquePayments              int                 `json:"unique_mock_payments"`
	SettledOrders               int                 `json:"settled_unique_orders"`
	DeclinedOrders              int                 `json:"declined_unique_orders"`
	FulfillmentAttempts         int                 `json:"fulfillment_attempts"`
	FulfilledOrders             int                 `json:"fulfilled_orders"`
	CompensatedDeliveryFailures int                 `json:"compensated_delivery_failures"`
	FulfillmentReplayRequests   int                 `json:"fulfillment_idempotent_replay_requests"`
	FulfillmentConflictChecks   int                 `json:"fulfillment_conflict_checks"`
	FulfillmentHTTPRequests     int                 `json:"fulfillment_http_requests"`
	FulfillmentP50Millis        float64             `json:"fulfillment_http_p50_ms"`
	FulfillmentP95Millis        float64             `json:"fulfillment_http_p95_ms"`
	ProviderDeliveryAttempts    int                 `json:"provider_delivery_attempts"`
	TransientDeliveryFailures   int                 `json:"transient_delivery_failures"`
	RecoveredDeliveryFaults     int                 `json:"recovered_delivery_faults"`
	FulfillmentRetryRequests    int                 `json:"fulfillment_retry_http_requests"`
	PredeclaredDeliveryFailures int                 `json:"predeclared_transient_delivery_faults"`
	PostCommitResponseLosses    int                 `json:"injected_post_commit_response_losses"`
	APIRecreationsAfterLoss     int                 `json:"api_recreations_after_response_loss"`
	OSProcessRestarts           int                 `json:"os_process_restarts_after_response_loss"`
	ConcurrentDuplicatePairs    int                 `json:"concurrent_duplicate_request_pairs"`
	CheckoutRequests            int                 `json:"checkout_http_requests"`
	TaskP50Millis               float64             `json:"task_http_lifecycle_p50_ms"`
	TaskP95Millis               float64             `json:"task_http_lifecycle_p95_ms"`
	Hashes                      EvaluationHashes    `json:"hashes"`
	Audit                       purchaseReplayAudit `json:"audit"`
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
	var plannedFulfillmentAttempts, plannedDeliveryFailures int
	for _, task := range tasks {
		if simulatedPaymentStatus("purchase-replay-order-"+task.TaskID) != PaymentSettled {
			continue
		}
		plannedFulfillmentAttempts++
		if simulatedFulfillmentFailure(task.TaskID) {
			plannedDeliveryFailures++
		}
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
	store.mockFulfillmentFaults = make(map[string]MockFulfillmentFaultPlan, plannedFulfillmentAttempts)
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
	catalogSource := strings.TrimSpace(os.Getenv("SHOP_SIM_PURCHASE_REPLAY_CATALOG_SOURCE"))
	if catalogSource == "" {
		catalogSource = "merchant_http_snapshot"
	}
	var replayCatalog ImportedCatalog
	switch catalogSource {
	case "merchant_http_snapshot":
		replayCatalog, err = LoadMerchantCatalog(ctx, sourceManifestPath, nil)
		if err != nil {
			t.Fatalf("load Merchant HTTP snapshot: %v", err)
		}
		if err := store.ValidateMerchantCatalogSnapshot(ctx, replayCatalog); err != nil {
			t.Fatalf("Merchant HTTP snapshot differs from isolated MySQL seed: %v", err)
		}
		merchantHashes, hashErr := ComputeEvaluationHashes(replayCatalog, allTasks, manifestBytes)
		if hashErr != nil || merchantHashes != fullHashes {
			t.Fatalf("Merchant HTTP snapshot hash differs from locked run: live=%+v mysql=%+v err=%v", merchantHashes, fullHashes, hashErr)
		}
	case "locked_mysql_snapshot":
		// The cloned catalog already passed the locked intent-suite hash check above.
		// This explicit mode avoids silently mixing a newer Merchant HTTP catalog
		// with the frozen MySQL seed used by the replay tasks.
		replayCatalog = catalog
	default:
		t.Fatalf("unsupported purchase replay catalog source %q", catalogSource)
	}
	parser := replayLifecycleIntents(t, allTasks, intentTrials, intentRun.PromptTemplateSHA256)
	apiHandler, err := NewHTTPAPIWithMerchantCatalog(store, parser, mysqlIntegrationAPIKey, replayCatalog, mysqlIntegrationBuyerApprovalKey)
	if err != nil {
		t.Fatal(err)
	}
	var server *httptest.Server
	startServer := func() error {
		handler, err := NewHTTPAPIWithMerchantCatalog(store, parser, mysqlIntegrationAPIKey, replayCatalog, mysqlIntegrationBuyerApprovalKey)
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
		"schema_version": "stablepay.agent-shopping-http-purchase-replay.v3", "run_id": runID,
		"status": "RUNNING", "tenant_id": tenantID, "database": SimulationDatabaseName,
		"source_intent_run_id": intentRun.RunID, "source_provider": parser.Provider(), "source_model": parser.Model(),
		"new_model_calls": 0, "payment_provider": "simulation-only", "real_settlement": false,
		"fulfillment_provider": simulatedFulfillmentProvider, "real_fulfillment": false, "real_refund": false,
		"predeclared_transient_delivery_fault_count": plannedDeliveryFailures,
		"fulfillment_failure_schedule":               "SHA-256 of locked source task ID selects a one-shot retryable failure on the first delivery attempt; bounded same-key retry is expected to complete",
		"catalog_source":                             catalogSource, "catalog_merchants": len(replayCatalog.Merchants),
		"catalog_products": len(replayCatalog.Products), "task_count": len(tasks), "hashes": hashes,
		"fault_schedule": map[string]int{"post_commit_response_loss_retry": 100,
			"post_commit_response_loss_api_recreate_retry": 100, "os_process_restart_after_response_loss": 1,
			"concurrent_duplicate_request_pair": 100, "predeclared_transient_delivery_fault_cases": plannedDeliveryFailures},
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
	var osProcessRestarts int
	var processServerAddr string
	var processServerCommand *exec.Cmd
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
			parseResult.TaskID == "" || parseResult.TaskStatus != TaskStatusDiscovered || !sameChallengeIntent(parseResult.Intent, task.Intent) ||
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
		fulfillmentFaultScheduled := simulatedPaymentStatus(orderKey) == PaymentSettled && simulatedFulfillmentFailure(task.TaskID)
		if simulatedPaymentStatus(orderKey) == PaymentSettled {
			plan := MockFulfillmentFaultPlan{MaxAttempts: defaultMockFulfillmentMaxAttempts}
			if fulfillmentFaultScheduled {
				plan.TransientFailures = 1
			}
			store.mockFulfillmentFaults[parseResult.TaskID] = plan
		}
		trial := purchaseReplayTrial{TaskID: task.TaskID, MerchantID: candidate.Product.MerchantID, SKU: candidate.Product.SKUID,
			OrderIdempotency: orderKey, ConcurrentRequests: 2, FulfillmentFaultScheduled: fulfillmentFaultScheduled}
		var checkout mysqlIntegrationCheckoutResponse
		var committedOrderID, committedPaymentID string
		if index%3 == 0 || index%3 == 1 {
			useOSProcessRestart := index%3 == 1 && osProcessRestarts == 0
			if useOSProcessRestart {
				processServerAddr = unusedLoopbackAddress(t)
				processServerCommand = startPurchaseReplayProcessServer(t, processServerAddr, tenantID)
			}
			trial.ResponseLost = true
			responseLosses++
			base := http.RoundTripper(http.DefaultTransport)
			if !useOSProcessRestart && client.api.client.Transport != nil {
				base = client.api.client.Transport
			}
			drop := &postCommitResponseLossTransport{base: base, target: orderKey}
			var requestErr error
			if useOSProcessRestart {
				processClient := &http.Client{Transport: drop, Timeout: 30 * time.Second}
				_, requestErr = doPurchaseReplayProcessCheckout(processClient, processServerAddr, orderRequest, marker)
			} else {
				client.api.client.Transport = drop
				_, requestErr = client.api.doRawWithHeaders(http.MethodPost, "/v1/orders", orderRequest, marker)
				client.api.client.Transport = base
			}
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
				trial.APIRestarted = true
				if useOSProcessRestart {
					stopPurchaseReplayProcessServer(processServerCommand)
					processServerCommand = startPurchaseReplayProcessServer(t, processServerAddr, tenantID)
					processClient := &http.Client{Timeout: 30 * time.Second}
					response, err := doPurchaseReplayProcessCheckout(processClient, processServerAddr, orderRequest, nil)
					if err != nil || response.status != http.StatusCreated {
						t.Fatalf("retry after OS process restart failed for %s: status=%d err=%v body=%s",
							task.TaskID, response.status, err, response.body)
					}
					checkout = decodePurchaseReplayCheckout(t, response)
					trial.OSProcessRestarted = true
					osProcessRestarts++
				} else {
					server.Close()
					if err := startServer(); err != nil {
						t.Fatalf("recreate HTTP API after committed checkout: %v", err)
					}
					client.api.server, client.api.client = server, server.Client()
					checkout = checkoutHTTPLifecycle(t, client, confirmation.confirmation.ID, orderKey)
				}
				client.requests++
				checkoutRequests++
			} else {
				checkout = checkoutHTTPLifecycle(t, client, confirmation.confirmation.ID, orderKey)
				checkoutRequests++
			}
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
		trial.FinalOrderStatus = checkout.Order.Status
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
	if len(uniqueOrders) != len(tasks) || len(uniquePayments) != len(tasks) || responseLosses != 200 || apiRecreations != 100 ||
		osProcessRestarts != 1 || concurrentPairs != 100 {
		t.Fatalf("purchase replay coverage mismatch: orders=%d payments=%d losses=%d api_recreations=%d os_process_restarts=%d concurrent=%d",
			len(uniqueOrders), len(uniquePayments), responseLosses, apiRecreations, osProcessRestarts, concurrentPairs)
	}
	var fulfillmentAttempts, fulfilledOrders, compensatedFailures, fulfillmentReplayRequests, fulfillmentConflictChecks, fulfillmentHTTPRequests int
	var transientDeliveryFailures, recoveredDeliveryFaults, fulfillmentRetryRequests int
	var providerDeliveryAttempts int
	fulfillmentLatencies := make([]float64, 0, settled)
	for index := range trials {
		trial := &trials[index]
		if trial.PaymentStatus != PaymentSettled {
			continue
		}
		fulfillmentAttempts++
		fulfillmentKey := "purchase-replay-fulfillment-" + trial.TaskID
		started := time.Now()
		first := apiClient.fulfill(t, trial.OrderID, fulfillmentKey)
		fulfillmentLatencies = append(fulfillmentLatencies, float64(time.Since(started).Microseconds())/1000)
		fulfillmentHTTPRequests++
		if first.Order.ID != trial.OrderID || first.Payment.ID != trial.PaymentID || first.Payment.Status != PaymentSettled ||
			!first.Payment.SimulationOnly || !first.Fulfillment.SimulationOnly || first.Fulfillment.Provider != simulatedFulfillmentProvider {
			t.Fatalf("fulfillment escaped or changed its settled simulated order for %s: %+v", trial.TaskID, first)
		}
		for first.Fulfillment.Status == FulfillmentRetryPending {
			if !trial.FulfillmentFaultScheduled || !first.Fulfillment.Retryable || first.Order.Status != OrderPaid || first.Refund != nil ||
				first.Fulfillment.AttemptCount >= first.Fulfillment.MaxAttempts {
				t.Fatalf("invalid retry-pending fulfillment state for %s: %+v scheduled_transient=%v", trial.TaskID, first, trial.FulfillmentFaultScheduled)
			}
			if first.Fulfillment.RetryAfterMillis > 0 {
				time.Sleep(time.Duration(first.Fulfillment.RetryAfterMillis) * time.Millisecond)
			}
			retry := apiClient.fulfill(t, trial.OrderID, fulfillmentKey)
			fulfillmentHTTPRequests++
			fulfillmentRetryRequests++
			trial.FulfillmentRetryRequests++
			if retry.Order.ID != trial.OrderID || retry.Payment.ID != trial.PaymentID || retry.Payment.Status != PaymentSettled {
				t.Fatalf("same-key fulfillment retry changed order/payment identity for %s: %+v", trial.TaskID, retry)
			}
			first = retry
		}
		trial.FulfillmentProviderAttempts = first.Fulfillment.AttemptCount
		providerDeliveryAttempts += first.Fulfillment.AttemptCount
		trial.TransientDeliveryFailures = max(0, first.Fulfillment.AttemptCount-1)
		transientDeliveryFailures += trial.TransientDeliveryFailures
		if trial.FulfillmentFaultScheduled {
			if trial.TransientDeliveryFailures < 1 || first.Fulfillment.AttemptCount != trial.TransientDeliveryFailures+1 {
				t.Fatalf("predeclared transient delivery fault was not recovered with bounded attempts for %s: trial=%+v result=%+v",
					trial.TaskID, trial, first.Fulfillment)
			}
			recoveredDeliveryFaults++
		} else if trial.TransientDeliveryFailures != 0 || first.Fulfillment.AttemptCount != 1 {
			t.Fatalf("unfaulted delivery unexpectedly retried for %s: trial=%+v result=%+v", trial.TaskID, trial, first.Fulfillment)
		}
		switch first.Fulfillment.Status {
		case FulfillmentDelivered:
			if first.Order.Status != OrderFulfilled || first.Refund != nil || first.Fulfillment.DeliveryReference == "" {
				t.Fatalf("delivery outcome disagrees with its predeclared transient-fault schedule for %s: %+v scheduled_failure=%v",
					trial.TaskID, first, trial.FulfillmentFaultScheduled)
			}
			fulfilledOrders++
		case FulfillmentCompensated:
			if trial.FulfillmentFaultScheduled || first.Order.Status != OrderRefunded || first.Refund == nil ||
				first.Refund.Status != RefundCompleted || first.Refund.AmountMinor != first.Payment.AmountMinor ||
				first.Refund.Currency != first.Payment.Currency || !first.Refund.SimulationOnly || first.Fulfillment.FailureCode != "SIMULATED_DELIVERY_FAILURE" {
				t.Fatalf("compensation outcome disagrees with its predeclared schedule for %s: %+v scheduled_failure=%v",
					trial.TaskID, first, trial.FulfillmentFaultScheduled)
			}
			compensatedFailures++
			trial.RefundStatus = first.Refund.Status
		default:
			t.Fatalf("unexpected fulfillment state %q for %s", first.Fulfillment.Status, trial.TaskID)
		}
		trial.FulfillmentStatus = first.Fulfillment.Status
		trial.FinalOrderStatus = first.Order.Status

		replay := apiClient.fulfill(t, trial.OrderID, fulfillmentKey)
		fulfillmentHTTPRequests++
		fulfillmentReplayRequests++
		if replay.Order.ID != first.Order.ID || replay.Order.Status != first.Order.Status ||
			replay.Payment.ID != first.Payment.ID || replay.Fulfillment.ID != first.Fulfillment.ID ||
			replay.Fulfillment.Status != first.Fulfillment.Status || (replay.Refund == nil) != (first.Refund == nil) ||
			(first.Refund != nil && replay.Refund.ID != first.Refund.ID) {
			t.Fatalf("fulfillment idempotent replay changed committed facts for %s: first=%+v replay=%+v", trial.TaskID, first, replay)
		}
		if code := apiClient.fulfillError(t, trial.OrderID, fulfillmentKey+"-conflict", http.StatusConflict); code != "IDEMPOTENCY_CONFLICT" {
			t.Fatalf("conflicting fulfillment key for %s returned %q", trial.TaskID, code)
		}
		fulfillmentHTTPRequests++
		fulfillmentConflictChecks++
	}
	if fulfillmentAttempts != plannedFulfillmentAttempts || transientDeliveryFailures != plannedDeliveryFailures ||
		recoveredDeliveryFaults != plannedDeliveryFailures || compensatedFailures != 0 || fulfilledOrders != fulfillmentAttempts ||
		fulfillmentReplayRequests != fulfillmentAttempts ||
		fulfillmentConflictChecks != fulfillmentAttempts {
		t.Fatalf("fulfillment coverage mismatch: orders=%d/%d delivered=%d compensated=%d transient_faults=%d/%d recovered=%d replays=%d conflicts=%d",
			fulfillmentAttempts, plannedFulfillmentAttempts, fulfilledOrders, compensatedFailures, transientDeliveryFailures, plannedDeliveryFailures,
			recoveredDeliveryFaults,
			fulfillmentReplayRequests, fulfillmentConflictChecks)
	}
	audit.Fulfillment = auditPurchaseReplayFulfillment(ctx, store, tenantID)
	if !audit.Fulfillment.Passed || audit.Fulfillment.ObservedFulfilledOrders != int64(fulfilledOrders) ||
		audit.Fulfillment.ObservedCompensatedOrders != int64(compensatedFailures) ||
		audit.Fulfillment.ObservedRefunds != int64(compensatedFailures) ||
		audit.Fulfillment.ObservedProviderAttempts != int64(providerDeliveryAttempts) ||
		audit.Fulfillment.RetryableFailureAttempts != int64(transientDeliveryFailures) ||
		audit.Fulfillment.RetryableFailuresWithoutRetry != 0 || audit.Fulfillment.FinalAttemptCountMismatches != 0 {
		audit.Passed = false
		t.Fatalf("purchase fulfillment SQL audit failed: %+v", audit.Fulfillment)
	}
	audit.Passed = audit.Passed && audit.Fulfillment.Passed
	metrics := purchaseReplayMetrics{Status: "PASS", RunID: runID, TenantID: tenantID, Database: SimulationDatabaseName,
		AttemptCount: len(tasks), UniqueOrders: len(uniqueOrders), UniquePayments: len(uniquePayments),
		SettledOrders: settled, DeclinedOrders: declined, FulfillmentAttempts: fulfillmentAttempts,
		FulfilledOrders: fulfilledOrders, CompensatedDeliveryFailures: compensatedFailures,
		FulfillmentReplayRequests: fulfillmentReplayRequests, FulfillmentConflictChecks: fulfillmentConflictChecks,
		FulfillmentHTTPRequests:  fulfillmentHTTPRequests,
		FulfillmentP50Millis:     lifecyclePercentile(fulfillmentLatencies, 0.50),
		FulfillmentP95Millis:     lifecyclePercentile(fulfillmentLatencies, 0.95),
		ProviderDeliveryAttempts: providerDeliveryAttempts, TransientDeliveryFailures: transientDeliveryFailures,
		RecoveredDeliveryFaults: recoveredDeliveryFaults, FulfillmentRetryRequests: fulfillmentRetryRequests,
		PredeclaredDeliveryFailures: plannedDeliveryFailures, PostCommitResponseLosses: responseLosses,
		APIRecreationsAfterLoss: apiRecreations, OSProcessRestarts: osProcessRestarts,
		ConcurrentDuplicatePairs: concurrentPairs, CheckoutRequests: checkoutRequests,
		TaskP50Millis: lifecyclePercentile(latencies, 0.50), TaskP95Millis: lifecyclePercentile(latencies, 0.95), Hashes: hashes, Audit: audit}
	manifest["status"] = metrics.Status
	manifest["checkout_http_requests"] = checkoutRequests
	manifest["fulfillment_attempts"] = fulfillmentAttempts
	manifest["fulfilled_orders"] = fulfilledOrders
	manifest["compensated_delivery_failures"] = compensatedFailures
	manifest["transient_delivery_failures"] = transientDeliveryFailures
	manifest["recovered_delivery_faults"] = recoveredDeliveryFaults
	manifest["provider_delivery_attempts"] = providerDeliveryAttempts
	manifest["fulfillment_retry_http_requests"] = fulfillmentRetryRequests
	manifest["fulfillment_http_requests"] = fulfillmentHTTPRequests
	manifest["fulfillment_audit_passed"] = audit.Fulfillment.Passed
	manifest["api_recreations_after_response_loss"] = apiRecreations
	manifest["os_process_restarts_after_response_loss"] = osProcessRestarts
	manifest["audit_passed"] = audit.Passed
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
	t.Logf("MYSQL_HTTP_PURCHASE_REPLAY_300 status=PASS attempts=%d settled=%d declined=%d delivered=%d compensated=%d transient_faults=%d recovered=%d provider_attempts=%d fulfillment_p95_ms=%.3f dropped_responses=%d api_recreations=%d os_process_restarts=%d concurrent_pairs=%d duplicate_settlements=%d evidence=%s",
		len(tasks), settled, declined, fulfilledOrders, compensatedFailures, transientDeliveryFailures, recoveredDeliveryFaults,
		providerDeliveryAttempts, metrics.FulfillmentP95Millis,
		responseLosses, apiRecreations, osProcessRestarts, concurrentPairs, audit.DuplicateSettlementsPerOrder, outputDir)
}

const purchaseReplayProcessServerEnv = "SHOP_SIM_PURCHASE_REPLAY_PROCESS_SERVER"

func TestMySQLHTTPPurchaseReplayProcessServer(t *testing.T) {
	if os.Getenv(purchaseReplayProcessServerEnv) != "1" {
		return
	}
	password := strings.TrimSpace(os.Getenv("SHOP_SIM_DB_PASSWORD"))
	tenantID := strings.TrimSpace(os.Getenv("SHOP_SIM_PURCHASE_REPLAY_TENANT"))
	addr := strings.TrimSpace(os.Getenv("SHOP_SIM_PURCHASE_REPLAY_ADDR"))
	if password == "" || !strings.HasPrefix(tenantID, "shopping-purchase-replay-") {
		t.Fatal("process server requires the isolated simulation password and a purchase-replay tenant")
	}
	if host, _, err := net.SplitHostPort(addr); err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		t.Fatal("process server address must be a loopback TCP address")
	}
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	store.tenant = tenantID
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.Ping(ctx); err != nil {
		t.Fatal("isolated simulation MySQL is not ready")
	}
	api, err := NewHTTPAPI(store, mysqlIntegrationParser{}, mysqlIntegrationAPIKey, mysqlIntegrationBuyerApprovalKey)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal("could not bind isolated loopback process server")
	}
	server := &http.Server{Handler: api, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second}
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatal("isolated process server stopped unexpectedly")
	}
}

func unusedLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("reserve a loopback address for the isolated process server")
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal("release reserved loopback address")
	}
	return addr
}

func startPurchaseReplayProcessServer(t *testing.T, addr, tenantID string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("resolve test executable for isolated process server")
	}
	cmd := exec.Command(executable, "-test.run=^TestMySQLHTTPPurchaseReplayProcessServer$")
	replacements := map[string]string{
		purchaseReplayProcessServerEnv:    "1",
		"SHOP_SIM_PURCHASE_REPLAY_TENANT": tenantID,
		"SHOP_SIM_PURCHASE_REPLAY_ADDR":   addr,
	}
	cmd.Env = replaceProcessEnvironment(os.Environ(), replacements)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal("start isolated shopping API child process")
	}
	t.Cleanup(func() { stopPurchaseReplayProcessServer(cmd) })
	waitForPurchaseReplayProcessServer(t, cmd, addr)
	return cmd
}

func replaceProcessEnvironment(environment []string, replacements map[string]string) []string {
	filtered := make([]string, 0, len(environment)+len(replacements))
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if _, replaced := replacements[name]; !replaced {
			filtered = append(filtered, entry)
		}
	}
	for name, value := range replacements {
		filtered = append(filtered, name+"="+value)
	}
	return filtered
}

func waitForPurchaseReplayProcessServer(t *testing.T, cmd *exec.Cmd, addr string) {
	t.Helper()
	client := &http.Client{Timeout: 300 * time.Millisecond}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get("http://" + addr + "/readyz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		if cmd.ProcessState != nil {
			t.Fatal("isolated shopping API child process exited before readiness")
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("isolated shopping API child process did not become ready within 15 seconds")
}

func stopPurchaseReplayProcessServer(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}

func doPurchaseReplayProcessCheckout(client *http.Client, addr string, input any, headers map[string]string) (mysqlIntegrationResponse, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return mysqlIntegrationResponse{}, err
	}
	request, err := http.NewRequest(http.MethodPost, "http://"+addr+"/v1/orders", bytes.NewReader(encoded))
	if err != nil {
		return mysqlIntegrationResponse{}, err
	}
	request.Header.Set("X-Simulation-API-Key", mysqlIntegrationAPIKey)
	request.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return mysqlIntegrationResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return mysqlIntegrationResponse{}, err
	}
	return mysqlIntegrationResponse{status: response.StatusCode, body: body}, nil
}

func doPurchaseReplayProcessFulfillment(client *http.Client, addr, orderID, idempotencyKey string) (mysqlIntegrationResponse, error) {
	encoded, err := json.Marshal(map[string]string{"idempotency_key": idempotencyKey})
	if err != nil {
		return mysqlIntegrationResponse{}, err
	}
	request, err := http.NewRequest(http.MethodPost, "http://"+addr+"/v1/orders/"+orderID+"/fulfillment", bytes.NewReader(encoded))
	if err != nil {
		return mysqlIntegrationResponse{}, err
	}
	request.Header.Set("X-Simulation-API-Key", mysqlIntegrationAPIKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return mysqlIntegrationResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return mysqlIntegrationResponse{}, err
	}
	return mysqlIntegrationResponse{status: response.StatusCode, body: body}, nil
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

func auditPurchaseReplayFulfillment(ctx context.Context, store *MySQLStore, tenant string) purchaseFulfillmentAudit {
	queries := map[string]string{
		"duplicate_fulfillments_per_order": `SELECT COUNT(*) FROM (
			SELECT order_id FROM simulated_fulfillments WHERE tenant_id=? GROUP BY order_id HAVING COUNT(*)>1) AS duplicates`,
		"duplicate_attempt_ordinals": `SELECT COUNT(*) FROM (
			SELECT order_id, attempt_number FROM simulated_fulfillment_attempts WHERE tenant_id=?
			GROUP BY order_id, attempt_number HAVING COUNT(*)>1) AS duplicates`,
		"attempt_sequence_gaps": `SELECT COUNT(*) FROM (
			SELECT order_id FROM simulated_fulfillment_attempts WHERE tenant_id=? GROUP BY order_id
			HAVING MIN(attempt_number)<>1 OR COUNT(*)<>MAX(attempt_number)) AS gaps`,
		"retryable_failures_without_retry": `SELECT COUNT(*) FROM simulated_fulfillment_attempts a
			WHERE a.tenant_id=? AND a.status='RETRYABLE_FAILURE' AND NOT EXISTS (
			SELECT 1 FROM simulated_fulfillment_attempts later WHERE later.tenant_id=a.tenant_id
			AND later.order_id=a.order_id AND later.attempt_number>a.attempt_number)`,
		"orphan_fulfillment_attempts": `SELECT COUNT(*) FROM simulated_fulfillment_attempts a
			LEFT JOIN sim_orders o ON o.tenant_id=a.tenant_id AND o.order_id=a.order_id
			LEFT JOIN simulated_payments p ON p.tenant_id=a.tenant_id AND p.order_id=a.order_id
			WHERE a.tenant_id=? AND (o.order_id IS NULL OR p.payment_id IS NULL OR p.status<>'SETTLED'
			OR a.provider<>'simulation-only' OR a.data_origin<>'SYNTHETIC_SIMULATION'
			OR a.status NOT IN ('RETRYABLE_FAILURE','DELIVERED','PERMANENT_FAILURE'))`,
		"final_attempt_count_mismatches": `SELECT COUNT(*) FROM simulated_fulfillments f
			LEFT JOIN (SELECT tenant_id, order_id, COUNT(*) AS attempt_count FROM simulated_fulfillment_attempts
			WHERE tenant_id=? GROUP BY tenant_id, order_id) a ON a.tenant_id=f.tenant_id AND a.order_id=f.order_id
			WHERE f.tenant_id=? AND (a.attempt_count IS NULL OR f.attempt_count<>a.attempt_count)`,
		"terminal_attempt_outcome_mismatches": `SELECT COUNT(*) FROM simulated_fulfillments f
			LEFT JOIN (SELECT tenant_id, order_id, MAX(attempt_number) AS last_attempt FROM simulated_fulfillment_attempts
			WHERE tenant_id=? GROUP BY tenant_id, order_id) latest ON latest.tenant_id=f.tenant_id AND latest.order_id=f.order_id
			LEFT JOIN simulated_fulfillment_attempts a ON a.tenant_id=latest.tenant_id AND a.order_id=latest.order_id
			AND a.attempt_number=latest.last_attempt WHERE f.tenant_id=? AND (a.attempt_id IS NULL
			OR f.attempt_count<>a.attempt_number OR (f.status='DELIVERED' AND a.status<>'DELIVERED')
			OR (f.status='FAILED_COMPENSATED' AND a.status<>'PERMANENT_FAILURE'))`,
		"attempt_idempotency_mismatches": `SELECT COUNT(*) FROM simulated_fulfillment_attempts a
			JOIN simulated_fulfillments f ON f.tenant_id=a.tenant_id AND f.order_id=a.order_id
			WHERE a.tenant_id=? AND a.idempotency_key<>f.idempotency_key`,
		"duplicate_refunds_per_order": `SELECT COUNT(*) FROM (
			SELECT order_id FROM simulated_refunds WHERE tenant_id=? GROUP BY order_id HAVING COUNT(*)>1) AS duplicates`,
		"settled_payments_without_fulfillment": `SELECT COUNT(*) FROM simulated_payments p
			LEFT JOIN simulated_fulfillments f ON f.tenant_id=p.tenant_id AND f.order_id=p.order_id
			WHERE p.tenant_id=? AND p.status='SETTLED' AND f.fulfillment_id IS NULL`,
		"declined_payments_with_fulfillment_or_refund": `SELECT COUNT(*) FROM simulated_payments p
			LEFT JOIN simulated_fulfillments f ON f.tenant_id=p.tenant_id AND f.order_id=p.order_id
			LEFT JOIN simulated_refunds r ON r.tenant_id=p.tenant_id AND r.order_id=p.order_id
			WHERE p.tenant_id=? AND p.status='DECLINED' AND (f.fulfillment_id IS NOT NULL OR r.refund_id IS NOT NULL)`,
		"final_order_or_task_state_mismatches": `SELECT COUNT(*) FROM simulated_fulfillments f
			JOIN sim_orders o ON o.tenant_id=f.tenant_id AND o.order_id=f.order_id
			JOIN sim_shopping_tasks t ON t.tenant_id=o.tenant_id AND t.task_id=o.task_id
			WHERE f.tenant_id=? AND ((f.status='DELIVERED' AND (o.status<>'FULFILLED' OR t.status<>'FULFILLED'))
			OR (f.status='FAILED_COMPENSATED' AND (o.status<>'REFUNDED' OR t.status<>'REFUNDED'))
			OR f.status NOT IN ('DELIVERED','FAILED_COMPENSATED'))`,
		"refund_amount_or_payment_mismatches": `SELECT COUNT(*) FROM simulated_fulfillments f
			LEFT JOIN simulated_refunds r ON r.tenant_id=f.tenant_id AND r.order_id=f.order_id
			LEFT JOIN simulated_payments p ON p.tenant_id=f.tenant_id AND p.order_id=f.order_id
			WHERE f.tenant_id=? AND ((f.status='DELIVERED' AND r.refund_id IS NOT NULL)
			OR (f.status='FAILED_COMPENSATED' AND (r.refund_id IS NULL OR p.payment_id IS NULL OR p.status<>'SETTLED'
			OR r.payment_id<>p.payment_id OR r.amount_minor<>p.amount_minor OR r.currency<>p.currency OR r.status<>'REFUNDED')))
			OR EXISTS (SELECT 1 FROM simulated_refunds orphan WHERE orphan.tenant_id=? AND NOT EXISTS
			(SELECT 1 FROM simulated_fulfillments owner WHERE owner.tenant_id=orphan.tenant_id AND owner.order_id=orphan.order_id AND owner.status='FAILED_COMPENSATED'))`,
		"reservation_state_mismatches": `SELECT COUNT(*) FROM simulated_fulfillments f
			JOIN sim_inventory_reservations r ON r.tenant_id=f.tenant_id AND r.order_id=f.order_id
			WHERE f.tenant_id=? AND ((f.status='DELIVERED' AND r.status<>'CONSUMED')
			OR (f.status='FAILED_COMPENSATED' AND r.status<>'RETURNED'))`,
		"non_simulation_fulfillment_effects": `SELECT COUNT(*) FROM simulated_fulfillments f
			LEFT JOIN simulated_refunds r ON r.tenant_id=f.tenant_id AND r.order_id=f.order_id
			WHERE f.tenant_id=? AND (f.provider<>'simulation-only' OR f.data_origin<>'SYNTHETIC_SIMULATION'
			OR (r.refund_id IS NOT NULL AND (r.provider<>'simulation-only' OR r.data_origin<>'SYNTHETIC_SIMULATION')))`,
	}
	counts := make(map[string]int64, len(queries))
	queryErrors := make(map[string]int64)
	violations := make(map[string]int64)
	for name, query := range queries {
		args := []any{tenant}
		if name == "refund_amount_or_payment_mismatches" || name == "final_attempt_count_mismatches" ||
			name == "terminal_attempt_outcome_mismatches" {
			args = append(args, tenant)
		}
		var count int64
		if err := store.db.WithContext(ctx).Raw(query, args...).Scan(&count).Error; err != nil {
			queryErrors[name] = 1
		} else {
			counts[name] = count
			if count != 0 {
				violations[name] = count
			}
		}
	}
	var delivered, compensated, refunds, providerAttempts, retryableFailures int64
	for name, query := range map[string]struct {
		model  any
		status string
	}{
		"delivered":   {model: &MockFulfillmentRecord{}, status: FulfillmentDelivered},
		"compensated": {model: &MockFulfillmentRecord{}, status: FulfillmentCompensated},
		"refunds":     {model: &MockRefundRecord{}, status: RefundCompleted},
	} {
		var count int64
		if err := store.db.WithContext(ctx).Model(query.model).Where("tenant_id=? AND status=?", tenant, query.status).Count(&count).Error; err != nil {
			queryErrors[name] = 1
			continue
		}
		switch name {
		case "delivered":
			delivered = count
		case "compensated":
			compensated = count
		case "refunds":
			refunds = count
		}
	}
	if err := store.db.WithContext(ctx).Model(&MockFulfillmentAttemptRecord{}).Where("tenant_id=?", tenant).Count(&providerAttempts).Error; err != nil {
		queryErrors["provider_attempts"] = 1
	}
	if err := store.db.WithContext(ctx).Model(&MockFulfillmentAttemptRecord{}).
		Where("tenant_id=? AND status=?", tenant, FulfillmentAttemptRetryableFailure).Count(&retryableFailures).Error; err != nil {
		queryErrors["retryable_failures"] = 1
	}
	return purchaseFulfillmentAudit{Passed: len(queryErrors) == 0 && len(violations) == 0,
		ObservedFulfilledOrders: delivered, ObservedCompensatedOrders: compensated, ObservedRefunds: refunds,
		ObservedProviderAttempts: providerAttempts, RetryableFailureAttempts: retryableFailures,
		DuplicateFulfillmentsPerOrder:           counts["duplicate_fulfillments_per_order"],
		DuplicateAttemptOrdinals:                counts["duplicate_attempt_ordinals"],
		AttemptSequenceGaps:                     counts["attempt_sequence_gaps"],
		RetryableFailuresWithoutRetry:           counts["retryable_failures_without_retry"],
		OrphanFulfillmentAttempts:               counts["orphan_fulfillment_attempts"],
		FinalAttemptCountMismatches:             counts["final_attempt_count_mismatches"],
		TerminalAttemptOutcomeMismatches:        counts["terminal_attempt_outcome_mismatches"],
		AttemptIdempotencyMismatches:            counts["attempt_idempotency_mismatches"],
		DuplicateRefundsPerOrder:                counts["duplicate_refunds_per_order"],
		SettledPaymentsWithoutFulfillment:       counts["settled_payments_without_fulfillment"],
		DeclinedPaymentsWithFulfillmentOrRefund: counts["declined_payments_with_fulfillment_or_refund"],
		FinalOrderOrTaskStateMismatches:         counts["final_order_or_task_state_mismatches"],
		RefundAmountOrPaymentMismatches:         counts["refund_amount_or_payment_mismatches"],
		ReservationStateMismatches:              counts["reservation_state_mismatches"],
		NonSimulationFulfillmentEffects:         counts["non_simulation_fulfillment_effects"],
		QueryErrors:                             queryErrors, InvariantViolations: violations}
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
