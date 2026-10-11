package api

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/composition"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/episode"
	mysqlstore "github.com/stablepay/commerce-runtime/internal/infrastructure/mysql"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/repository"
	runtime "github.com/stablepay/commerce-runtime/internal/runtime"
)

const (
	m1MatrixScenarioEnv = "STABLEPAY_M1_MATRIX_50_E2E"
	m1MatrixAuthToken   = "m1-fixed-50-matrix-local-only"
	m1MatrixDevnetMint  = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"
	m1MatrixSeller      = "NBZtLpE3u95YfirCqMyjZg5p9LZ1f9rjtFnCeuibeCy"
)

var m1MatrixMoneyPattern = regexp.MustCompile(`\$([0-9]+(?:\.[0-9]{1,2})?)`)

type m1MatrixTask struct {
	TaskID          string `json:"task_id"`
	Scenario        string `json:"scenario"`
	ExpectedOutcome string `json:"expected_outcome"`
	Prompt          string `json:"prompt"`
}

type m1MatrixParsedIntent struct {
	CategoryTag string
	BudgetMinor int64
}

type m1MatrixTrial struct {
	TaskID              string            `json:"task_id"`
	Prompt              string            `json:"prompt"`
	Scenario            string            `json:"scenario"`
	ExpectedOutcome     string            `json:"expected_outcome"`
	ObservedOutcome     string            `json:"observed_outcome"`
	OutcomeCorrect      bool              `json:"outcome_correct"`
	IntentSource        string            `json:"intent_source"`
	TestCorrelationID   string            `json:"test_correlation_id"`
	RuntimeTraceID      string            `json:"runtime_trace_id,omitempty"`
	QueryHash           string            `json:"query_hash"`
	CandidateCount      int               `json:"candidate_count"`
	SelectedCandidate   catalog.Candidate `json:"selected_candidate,omitempty"`
	EpisodeID           string            `json:"episode_id,omitempty"`
	FinalState          string            `json:"final_state"`
	PaymentIntentID     string            `json:"payment_intent_id,omitempty"`
	TransactionID       string            `json:"transaction_id,omitempty"`
	InitialQuoteHash    string            `json:"initial_quote_hash,omitempty"`
	FinalQuoteHash      string            `json:"final_quote_hash,omitempty"`
	BuyerApprovedQuote  string            `json:"buyer_approved_quote_hash,omitempty"`
	InitialQuoteAmount  int64             `json:"initial_quote_amount_minor,omitempty"`
	FinalQuoteAmount    int64             `json:"final_quote_amount_minor,omitempty"`
	BuyerApprovedAmount int64             `json:"buyer_approved_amount_minor,omitempty"`
	DeliveryEvidence    string            `json:"delivery_evidence_ref,omitempty"`
	BuyerReconfirmed    bool              `json:"buyer_reconfirmed_after_price_change"`
	PaymentIntentCount  int               `json:"payment_intent_count"`
	SettlementRows      int               `json:"payment_settled_rows"`
	ValidDelivery       bool              `json:"valid_delivery"`
	TaskE2EMillis       float64           `json:"task_e2e_ms"`
	PaymentSettledAt    time.Time         `json:"payment_settled_at,omitempty"`
	DeliveryReceivedAt  time.Time         `json:"delivery_received_at,omitempty"`
	PaymentToDeliveryMS float64           `json:"payment_to_delivery_ms"`
}

type m1MatrixMetrics struct {
	SchemaVersion                     string         `json:"schema_version"`
	RunID                             string         `json:"run_id"`
	SuiteSHA256                       string         `json:"suite_sha256"`
	TaskCount                         int            `json:"task_count"`
	CorrectTerminalOutcomes           int            `json:"correct_terminal_outcomes"`
	CorrectTerminalOutcomeRate        float64        `json:"correct_terminal_outcome_rate"`
	PurchaseAuthorizedTasks           int            `json:"purchase_authorized_tasks"`
	PurchaseTasksFulfilled            int            `json:"purchase_tasks_fulfilled"`
	PurchaseTaskDeliveryRate          float64        `json:"purchase_task_delivery_rate"`
	ConfirmedPayments                 int            `json:"confirmed_payments"`
	PostPaymentValidDeliveries        int            `json:"post_payment_valid_deliveries"`
	PostPaymentValidDeliveryRate      float64        `json:"post_payment_valid_delivery_rate"`
	PriceChangeTasks                  int            `json:"price_change_tasks"`
	PriceChangeReconfirmed            int            `json:"price_change_reconfirmed"`
	BudgetRefusals                    int            `json:"budget_refusals"`
	Clarifications                    int            `json:"clarifications"`
	BuyerRejections                   int            `json:"buyer_rejections"`
	PaymentIntentRows                 int            `json:"payment_intent_rows"`
	PaymentSettledLedgerRows          int            `json:"payment_settled_ledger_rows"`
	DuplicatePaymentIntentRows        int            `json:"duplicate_payment_intent_rows"`
	DuplicatePaymentSettledLedgerRows int            `json:"duplicate_payment_settled_ledger_rows"`
	DuplicateTransactionIDs           int            `json:"duplicate_transaction_ids"`
	UnexpectedEconomicEffects         int            `json:"unexpected_economic_effects"`
	PaymentAdapterSubmits             int32          `json:"payment_adapter_submits"`
	MerchantDeliveryCalls             int32          `json:"merchant_delivery_calls"`
	TaskE2EP50Millis                  float64        `json:"task_e2e_p50_ms"`
	TaskE2EP95Millis                  float64        `json:"task_e2e_p95_ms"`
	PaymentToDeliveryP50Millis        float64        `json:"payment_to_delivery_p50_ms"`
	PaymentToDeliveryP95Millis        float64        `json:"payment_to_delivery_p95_ms"`
	MappedCapabilityCount             int            `json:"mapped_capability_count"`
	MerchantCatalogSourceCount        int            `json:"merchant_catalog_source_count"`
	ScenarioCounts                    map[string]int `json:"scenario_counts"`
	RealModelCalls                    bool           `json:"real_model_calls"`
	RealPaymentCalls                  bool           `json:"real_payment_calls"`
	PersistenceBackend                string         `json:"persistence_backend"`
	MySQLAudit                        string         `json:"mysql_audit"`
}

type m1MatrixMockMerchant struct {
	prices         map[string]int64
	payees         map[string]string
	refreshDelta   int64
	deliveries     atomic.Int32
	initialQuotes  atomic.Int32
	refreshedQuote atomic.Int32
}

type m1MatrixMockPayment struct {
	submits     atomic.Int32
	mu          sync.Mutex
	confirmedAt map[string]time.Time
}

func (p *m1MatrixMockPayment) Submit(_ context.Context, request adapters.PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	p.submits.Add(1)
	confirmedAt := time.Now()
	p.mu.Lock()
	if p.confirmedAt == nil {
		p.confirmedAt = make(map[string]time.Time)
	}
	p.confirmedAt[request.Intent.EpisodeID] = confirmedAt
	p.mu.Unlock()
	return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, IntentID: request.Intent.IntentID,
		TxID: "tx:" + request.Intent.IntentID, TxHash: "hash:" + request.Intent.IntentID,
		AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency}, nil
}

func (p *m1MatrixMockPayment) confirmationTime(episodeID string) (time.Time, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	value, ok := p.confirmedAt[episodeID]
	return value, ok
}

func (m *m1MatrixMockMerchant) Invoke(_ context.Context, request adapters.MerchantInvokeRequest) (adapters.MerchantInvokeResult, error) {
	now := time.Now().UTC()
	if request.Phase == invocation.PhaseInitial || request.Phase == invocation.PhaseQuoteRefresh {
		parsed, err := url.Parse(request.Endpoint.Endpoint)
		if err != nil {
			return adapters.MerchantInvokeResult{}, err
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) < 2 || parts[len(parts)-1] != "execute" {
			return adapters.MerchantInvokeResult{}, fmt.Errorf("mock Merchant received unexpected resource path")
		}
		sku := parts[len(parts)-2]
		amountMinor, ok := m.prices[sku]
		if !ok {
			return adapters.MerchantInvokeResult{}, fmt.Errorf("mock Merchant has no mapped price for SKU %q", sku)
		}
		if request.Phase == invocation.PhaseInitial {
			m.initialQuotes.Add(1)
		} else {
			m.refreshedQuote.Add(1)
			amountMinor += m.refreshDelta
		}
		challenge, err := json.Marshal(map[string]any{
			"x402Version": 2,
			"resource":    map[string]any{"url": request.Endpoint.Endpoint},
			"accepts": []map[string]any{{
				"scheme": "exact", "network": "solana:devnet", "amount": strconv.FormatInt(amountMinor*10_000, 10),
				"asset": m1MatrixDevnetMint, "payTo": m.payees[sku], "maxTimeoutSeconds": 300,
				"extra": map[string]any{"currency": "USDC", "productId": sku, "version": "2"},
			}},
		})
		if err != nil {
			return adapters.MerchantInvokeResult{}, err
		}
		return adapters.MerchantInvokeResult{HTTPStatus: http.StatusPaymentRequired, ContentType: "application/json",
			Headers: map[string]string{"PAYMENT-REQUIRED": base64.StdEncoding.EncodeToString(challenge)}, PayloadRef: "m1-mock://quote/" + sku,
			PayloadHash: invocation.PayloadHash(challenge), Body: challenge, OccurredAt: now}, nil
	}
	if request.Phase != invocation.PhaseDelivery || request.PaymentIntentID == "" {
		return adapters.MerchantInvokeResult{}, fmt.Errorf("mock delivery was not bound to a PaymentIntent")
	}
	m.deliveries.Add(1)
	body, err := json.Marshal(map[string]string{"episode_id": request.EpisodeID, "capability_id": request.CapabilityID, "result": "deterministic local mock delivery"})
	if err != nil {
		return adapters.MerchantInvokeResult{}, err
	}
	return adapters.MerchantInvokeResult{HTTPStatus: http.StatusOK, ContentType: "application/json", PayloadRef: "m1-mock://delivery/" + request.EpisodeID,
		PayloadHash: invocation.PayloadHash(body), Body: body, OccurredAt: now}, nil
}

func TestM1Fixed50ScenarioMatrixMockE2E(t *testing.T) {
	if os.Getenv(m1MatrixScenarioEnv) != "1" {
		t.Skip("set STABLEPAY_M1_MATRIX_50_E2E=1 to run the fixed 50-task Runtime shopping matrix")
	}

	root, err := m1FindWorkspaceRoot()
	if err != nil {
		t.Fatal(err)
	}
	tasksPath := filepath.Join(root, "commerce-runtime", "testdata", "m1-fixed-50.jsonl")
	taskBytes, err := os.ReadFile(tasksPath)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := m1MatrixReadTasks(taskBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 50 {
		t.Fatalf("fixed M1 suite has %d tasks, expected exactly 50", len(tasks))
	}
	expectedOutcomes := map[string]string{
		"purchase": "FULFILLED", "price_change": "FULFILLED_AFTER_RECONFIRMATION", "budget_refusal": "BUDGET_REFUSED",
		"clarify": "CLARIFY_REQUIRED", "buyer_reject": "BUYER_REJECTED",
	}
	expectedScenarioCounts := map[string]int{"purchase": 30, "price_change": 10, "budget_refusal": 4, "clarify": 3, "buyer_reject": 3}
	actualScenarioCounts := make(map[string]int, len(expectedScenarioCounts))
	for index, task := range tasks {
		if task.TaskID != fmt.Sprintf("m1-%03d", index+1) || expectedOutcomes[task.Scenario] != task.ExpectedOutcome {
			t.Fatalf("fixed M1 task order or expected outcome is invalid at row %d: %#v", index+1, task)
		}
		actualScenarioCounts[task.Scenario]++
	}
	for scenario, expectedCount := range expectedScenarioCounts {
		if actualScenarioCounts[scenario] != expectedCount {
			t.Fatalf("fixed M1 scenario mix changed: %s=%d, expected %d", scenario, actualScenarioCounts[scenario], expectedCount)
		}
	}
	taskDigest := sha256.Sum256(taskBytes)
	suiteHash := "sha256:" + hex.EncodeToString(taskDigest[:])

	var store repository.TransitionStore = repository.NewInMemoryStore()
	storeName, mysqlAudit := "repository.InMemoryStore", "NOT_RUN_IN_MEMORY_BACKEND"
	if os.Getenv("COMMERCE_RUNTIME_M1_MYSQL_E2E") == "1" {
		dsn := strings.TrimSpace(os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"))
		if !strings.Contains(dsn, "tcp(127.0.0.1:") || !strings.Contains(dsn, ")/stablepay_commerce_m1_test?") {
			t.Fatal("M1 matrix MySQL run requires the isolated loopback stablepay_commerce_m1_test database")
		}
		db, err := mysqlstore.Open(dsn)
		if err != nil {
			t.Fatal(err)
		}
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = sqlDB.Close() })
		if err := sqlDB.PingContext(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := mysqlstore.AutoMigrate(context.Background(), db); err != nil {
			t.Fatal(err)
		}
		store = mysqlstore.NewStore(db)
		storeName, mysqlAudit = "isolated MySQL stablepay_commerce_m1_test", "PASSED_PER_EPISODE_INTENT_AND_LEDGER_AUDIT"
	}

	manifestPath := filepath.Join(root, ".local-run", "simulated-merchants", "runtime-capabilities-devnet.json")
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read local Merchant-to-Runtime mapping manifest: %v", err)
	}
	manifest, err := composition.LoadMerchantCatalogManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	allowedSources := map[string]bool{"sim-m02": true, "sim-m05": true, "sim-m11": true}
	filtered := manifest.Sources[:0]
	for _, source := range manifest.Sources {
		if allowedSources[source.SourceID] {
			filtered = append(filtered, source)
		}
	}
	manifest.Sources = filtered
	if len(manifest.Sources) != 3 {
		t.Fatalf("expected exactly three mapped local Merchant sources, found %d", len(manifest.Sources))
	}
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	catalogReader, err := composition.NewHTTPMerchantCatalogReader(&http.Client{Timeout: 12 * time.Second}, manifest)
	if err != nil {
		t.Fatal(err)
	}
	readCtx, cancelRead := context.WithTimeout(context.Background(), 90*time.Second)
	capabilities, err := catalogReader.ReadCapabilities(readCtx)
	cancelRead()
	if err != nil {
		t.Fatalf("read the three selected live local Merchant product APIs: %v", err)
	}
	if len(capabilities) != 3 {
		t.Fatalf("expected three active mapped product capabilities, found %d", len(capabilities))
	}
	prices := make(map[string]int64, len(capabilities))
	payees := make(map[string]string, len(capabilities))
	tagsByProduct := make(map[string][]string, len(capabilities))
	for _, capability := range capabilities {
		if capability.PriceHintMinor == nil || *capability.PriceHintMinor <= 0 || capability.PriceHintCurrency != "USDC" {
			t.Fatalf("mapped Merchant SKU lacks a valid USDC price hint: merchant=%s sku=%s", capability.MerchantDID, capability.ProductID)
		}
		if capability.PayeeDID != m1MatrixSeller {
			t.Fatalf("mapped Merchant SKU payee mismatch: merchant=%s sku=%s payee=%s", capability.MerchantDID, capability.ProductID, capability.PayeeDID)
		}
		if len(capability.SemanticTags) == 0 {
			t.Fatalf("mapped Merchant SKU lacks operator-authored category metadata: %s", capability.ProductID)
		}
		prices[capability.ProductID] = *capability.PriceHintMinor
		payees[capability.ProductID] = capability.PayeeDID
		tagsByProduct[capability.ProductID] = capability.SemanticTags
	}

	merchant := &m1MatrixMockMerchant{prices: prices, payees: payees, refreshDelta: 5}
	paymentAdapter := &m1MatrixMockPayment{}
	entitlement := &m1MockEntitlement{}
	service := application.NewService(store,
		application.WithUnconstrainedSettlementPolicyForTests(),
		application.WithMerchantAdapter(merchant),
		application.WithPaymentAdapters(application.PaymentDependencies{DID: apiDID{}, Payment: paymentAdapter, Entitlement: entitlement}),
		application.WithBuyerQuoteValidator(&currentBuyerQuote{}),
	)
	for _, capability := range capabilities {
		if err := service.RegisterCapabilityVersion(context.Background(), capability); err != nil {
			t.Fatalf("register mapped Merchant SKU %s: %v", capability.ProductID, err)
		}
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	requesterDID := "did:solana:" + solana.PublicKeyFromBytes(publicKey).String()
	runner := runtime.NewRunner(service, store)
	server := httptest.NewServer(NewServer(service, store, nil, AuthConfig{Token: m1MatrixAuthToken}, nil))
	defer server.Close()
	intentStore, ok := store.(repository.PaymentIntentRepository)
	if !ok {
		t.Fatal("Runtime store does not expose PaymentIntent audit reads")
	}
	ledgerStore, ok := store.(repository.LedgerRepository)
	if !ok {
		t.Fatal("Runtime store does not expose Ledger audit reads")
	}

	metrics := m1MatrixMetrics{SchemaVersion: "stablepay.m1-fixed-50-scenario-matrix.v1", SuiteSHA256: suiteHash,
		TaskCount: len(tasks), PurchaseAuthorizedTasks: 40, PriceChangeTasks: 10, MappedCapabilityCount: len(capabilities),
		MerchantCatalogSourceCount: len(manifest.Sources), ScenarioCounts: map[string]int{}, RealModelCalls: false,
		RealPaymentCalls: false, PersistenceBackend: storeName, MySQLAudit: mysqlAudit}
	trials := make([]m1MatrixTrial, 0, len(tasks))
	taskDurations := make([]float64, 0, len(tasks))
	paymentDeliveryDurations := make([]float64, 0, 40)
	txIDs := make(map[string]int)

	for _, task := range tasks {
		started := time.Now()
		metrics.ScenarioCounts[task.Scenario]++
		parsed := m1MatrixParseNaturalLanguage(task.Prompt)
		request := m1MatrixAcquireRequest(task, requesterDID, parsed, time.Now().UTC())
		query, err := catalog.FromAcquireCapabilityRequest(request)
		if err != nil {
			t.Fatalf("task %s parse fixture intent: %v", task.TaskID, err)
		}
		searchBody, err := json.Marshal(catalogSearchPayload{Query: query, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		search := doJSON(t, server.URL+"/v1/catalog/search", http.MethodPost, m1MatrixAuthToken, "", searchBody)
		if search.Code != http.StatusOK {
			t.Fatalf("task %s catalog search status=%d body=%s", task.TaskID, search.Code, search.Body.String())
		}
		var searchResult application.CatalogSearchResult
		if err := json.Unmarshal(search.Body.Bytes(), &searchResult); err != nil {
			t.Fatalf("task %s decode catalog result: %v", task.TaskID, err)
		}
		trial := m1MatrixTrial{TaskID: task.TaskID, Prompt: task.Prompt, Scenario: task.Scenario, ExpectedOutcome: task.ExpectedOutcome,
			IntentSource: "RULE_BASELINE_NO_LLM", TestCorrelationID: task.TaskID, QueryHash: searchResult.QueryHash, CandidateCount: searchResult.TotalCandidates}
		if task.Scenario == "budget_refusal" || task.Scenario == "clarify" {
			if len(searchResult.Candidates) != 0 || searchResult.TotalCandidates != 0 {
				t.Fatalf("task %s should not have an eligible product candidate: total=%d candidates=%#v", task.TaskID, searchResult.TotalCandidates, searchResult.Candidates)
			}
			if task.Scenario == "budget_refusal" {
				matchingCapability := false
				for _, capability := range capabilities {
					if m1MatrixHasTag(capability.SemanticTags, parsed.CategoryTag) {
						matchingCapability = true
						if capability.PriceHintMinor == nil || *capability.PriceHintMinor <= parsed.BudgetMinor {
							t.Fatalf("task %s is not a genuine budget refusal: matching SKU %s is within budget %d", task.TaskID, capability.ProductID, parsed.BudgetMinor)
						}
					}
				}
				if !matchingCapability {
					t.Fatalf("task %s budget refusal has no capability for requested category %s", task.TaskID, parsed.CategoryTag)
				}
			}
			trial.ObservedOutcome = task.ExpectedOutcome
			trial.FinalState = "NO_EPISODE_NO_PURCHASE_SIDE_EFFECT"
			trial.OutcomeCorrect = true
			if task.Scenario == "budget_refusal" {
				metrics.BudgetRefusals++
			} else {
				metrics.Clarifications++
			}
			trial.TaskE2EMillis = float64(time.Since(started).Nanoseconds()) / float64(time.Millisecond)
			taskDurations = append(taskDurations, trial.TaskE2EMillis)
			trials = append(trials, trial)
			metrics.CorrectTerminalOutcomes++
			continue
		}
		if len(searchResult.Candidates) != 1 {
			t.Fatalf("task %s expected exactly one eligible product, got %d", task.TaskID, len(searchResult.Candidates))
		}
		selected := searchResult.Candidates[0]
		if selected.ProductID == "" || selected.PayeeDID != m1MatrixSeller {
			t.Fatalf("task %s selected an invalid SKU/payee: %#v", task.TaskID, selected)
		}
		if !m1MatrixHasTag(tagsByProduct[selected.ProductID], parsed.CategoryTag) {
			t.Fatalf("task %s catalog search selected SKU %s outside requested category %s", task.TaskID, selected.ProductID, parsed.CategoryTag)
		}
		trial.SelectedCandidate = selected
		request.PurchaseSelection = &contract.PurchaseSelection{MerchantDID: selected.MerchantDID, CapabilityID: selected.CapabilityID, ProductID: selected.ProductID}
		requestBody, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		createdResponse := doJSON(t, server.URL+"/v1/episodes", http.MethodPost, m1MatrixAuthToken, request.RequestID, requestBody)
		if createdResponse.Code != http.StatusAccepted {
			t.Fatalf("task %s create Runtime Episode status=%d body=%s", task.TaskID, createdResponse.Code, createdResponse.Body.String())
		}
		var created struct {
			Episode *episode.CommerceEpisode `json:"episode"`
		}
		if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil || created.Episode == nil {
			t.Fatalf("task %s decode Episode: err=%v body=%s", task.TaskID, err, createdResponse.Body.String())
		}
		episodeID := created.Episode.EpisodeID
		trial.EpisodeID = episodeID
		paused, err := runner.RunEpisode(context.Background(), episodeID)
		if err != nil || paused.Episode.State != episode.StateAwaitingBuyer || !paused.Paused {
			t.Fatalf("task %s did not stop at the buyer-approval gate: state=%v paused=%v err=%v", task.TaskID, paused.Episode.State, paused.Paused, err)
		}
		beforeApproval, err := intentStore.ListPaymentIntents(context.Background(), episodeID)
		if err != nil || len(beforeApproval) != 0 || paymentAdapter.submits.Load() != int32(metrics.ConfirmedPayments) {
			t.Fatalf("task %s created economic effects before buyer response: intents=%d submits=%d err=%v", task.TaskID, len(beforeApproval), paymentAdapter.submits.Load(), err)
		}
		offer, err := m1MatrixGetBuyerOffer(t, server.URL, episodeID)
		if err != nil {
			t.Fatalf("task %s get BuyerOffer: %v", task.TaskID, err)
		}
		if offer.ProductID != selected.ProductID || offer.PayeeDID != m1MatrixSeller || offer.AmountMinor > parsed.BudgetMinor {
			t.Fatalf("task %s offer does not match selected SKU, seller, or budget: %#v", task.TaskID, offer)
		}
		trial.InitialQuoteHash, trial.InitialQuoteAmount = offer.QuoteHash, offer.AmountMinor
		trial.FinalQuoteHash, trial.FinalQuoteAmount = offer.QuoteHash, offer.AmountMinor

		if task.Scenario == "buyer_reject" {
			rejected := m1MatrixPostBuyerConfirmation(t, server.URL, episodeID, offer, privateKey, false, "reject-"+task.TaskID)
			if rejected.Code != http.StatusAccepted {
				t.Fatalf("task %s signed buyer rejection status=%d body=%s", task.TaskID, rejected.Code, rejected.Body.String())
			}
			final, err := store.Get(context.Background(), episodeID)
			if err != nil || final.State != episode.StateAborted {
				t.Fatalf("task %s did not enter ABORTED after signed rejection: state=%v err=%v", task.TaskID, final.State, err)
			}
			intents, err := intentStore.ListPaymentIntents(context.Background(), episodeID)
			if err != nil || len(intents) != 0 {
				t.Fatalf("task %s buyer rejection created PaymentIntent rows=%d err=%v", task.TaskID, len(intents), err)
			}
			entries, err := ledgerStore.ListLedgerEntries(context.Background(), episodeID)
			if err != nil || countM1Settlements(entries) != 0 || paymentAdapter.submits.Load() != int32(metrics.ConfirmedPayments) {
				t.Fatalf("task %s buyer rejection caused a payment side effect: settled=%d submits=%d err=%v", task.TaskID, countM1Settlements(entries), paymentAdapter.submits.Load(), err)
			}
			trial.ObservedOutcome, trial.FinalState, trial.OutcomeCorrect = "BUYER_REJECTED", string(final.State), true
			trial.TaskE2EMillis = float64(time.Since(started).Nanoseconds()) / float64(time.Millisecond)
			taskDurations = append(taskDurations, trial.TaskE2EMillis)
			trials = append(trials, trial)
			metrics.BuyerRejections++
			metrics.CorrectTerminalOutcomes++
			continue
		}

		if task.Scenario == "price_change" {
			oldOffer := offer
			refreshPath := server.URL + "/v1/episodes/" + episodeID + "/buyer-offers/refresh"
			refreshed := doJSON(t, refreshPath, http.MethodPost, m1MatrixAuthToken, "refresh-"+task.TaskID, nil)
			if refreshed.Code != http.StatusAccepted {
				t.Fatalf("task %s Runtime quote refresh status=%d body=%s", task.TaskID, refreshed.Code, refreshed.Body.String())
			}
			var refreshedPayload struct {
				BuyerOffer *application.BuyerOffer `json:"buyer_offer"`
			}
			if err := json.Unmarshal(refreshed.Body.Bytes(), &refreshedPayload); err != nil || refreshedPayload.BuyerOffer == nil {
				t.Fatalf("task %s decode refreshed BuyerOffer: err=%v", task.TaskID, err)
			}
			offer = *refreshedPayload.BuyerOffer
			if offer.QuoteHash == oldOffer.QuoteHash || offer.AmountMinor != oldOffer.AmountMinor+merchant.refreshDelta {
				t.Fatalf("task %s refreshed quote did not change the exact amount: old=%d new=%d", task.TaskID, oldOffer.AmountMinor, offer.AmountMinor)
			}
			trial.FinalQuoteHash, trial.FinalQuoteAmount = offer.QuoteHash, offer.AmountMinor
			staleConfirmation := m1MatrixPostBuyerConfirmation(t, server.URL, episodeID, oldOffer, privateKey, true, "stale-"+task.TaskID)
			if staleConfirmation.Code != http.StatusConflict {
				t.Fatalf("task %s stale buyer signature should be rejected, got HTTP %d body=%s", task.TaskID, staleConfirmation.Code, staleConfirmation.Body.String())
			}
			intents, err := intentStore.ListPaymentIntents(context.Background(), episodeID)
			if err != nil || len(intents) != 0 || paymentAdapter.submits.Load() != int32(metrics.ConfirmedPayments) {
				t.Fatalf("task %s stale quote caused payment effect: intents=%d submits=%d err=%v", task.TaskID, len(intents), paymentAdapter.submits.Load(), err)
			}
			trial.BuyerReconfirmed = true
			metrics.PriceChangeReconfirmed++
		}

		confirmed := m1MatrixPostBuyerConfirmation(t, server.URL, episodeID, offer, privateKey, true, "approve-"+task.TaskID)
		if confirmed.Code != http.StatusAccepted {
			t.Fatalf("task %s exact-offer buyer approval status=%d body=%s", task.TaskID, confirmed.Code, confirmed.Body.String())
		}
		trial.BuyerApprovedQuote, trial.BuyerApprovedAmount = offer.QuoteHash, offer.AmountMinor
		completed, err := runner.RunEpisode(context.Background(), episodeID)
		runtimeCompletedAt := time.Now()
		if err != nil || completed.Episode.State != episode.StateFulfilled {
			t.Fatalf("task %s did not complete via Commerce Runtime: state=%v err=%v", task.TaskID, completed.Episode.State, err)
		}
		paymentConfirmedAt, ok := paymentAdapter.confirmationTime(episodeID)
		if !ok || runtimeCompletedAt.Before(paymentConfirmedAt) {
			t.Fatalf("task %s lacks monotonic mock payment-confirmation/Runtime-completion timestamps", task.TaskID)
		}
		finalResponse := doJSON(t, server.URL+"/v1/episodes/"+episodeID, http.MethodGet, m1MatrixAuthToken, "", nil)
		var final struct {
			Episode    *episode.CommerceEpisode       `json:"episode"`
			Artifact   *invocation.DeliveryArtifact   `json:"artifact"`
			Validation *invocation.ValidationEvidence `json:"validation"`
		}
		if finalResponse.Code != http.StatusOK || json.Unmarshal(finalResponse.Body.Bytes(), &final) != nil || final.Episode == nil {
			t.Fatalf("task %s final Runtime status failed: HTTP=%d body=%s", task.TaskID, finalResponse.Code, finalResponse.Body.String())
		}
		intents, err := intentStore.ListPaymentIntents(context.Background(), episodeID)
		if err != nil || len(intents) != 1 || intents[0].Status != payment.IntentConfirmed {
			t.Fatalf("task %s expected one confirmed PaymentIntent; count=%d err=%v", task.TaskID, len(intents), err)
		}
		entries, err := ledgerStore.ListLedgerEntries(context.Background(), episodeID)
		if err != nil {
			t.Fatal(err)
		}
		settlementCount := countM1Settlements(entries)
		var settledAt time.Time
		for _, entry := range entries {
			if entry.Type == ledger.EntryPaymentSettled {
				settledAt = entry.OccurredAt
				trial.RuntimeTraceID = entry.TraceID
			}
		}
		validDelivery := final.Validation != nil && final.Validation.Valid && final.Artifact != nil &&
			final.Artifact.PaymentIntentID == intents[0].IntentID && final.Artifact.HTTPStatus == http.StatusOK
		if settlementCount != 1 || settledAt.IsZero() || !validDelivery || final.Episode.State != episode.StateFulfilled {
			t.Fatalf("task %s failed payment/delivery audit: settlements=%d valid=%v state=%s", task.TaskID, settlementCount, validDelivery, final.Episode.State)
		}
		if final.Artifact.ReceivedAt.Before(settledAt) {
			t.Fatalf("task %s delivery artifact timestamp precedes PAYMENT_SETTLED", task.TaskID)
		}
		paymentToDeliveryMS := float64(runtimeCompletedAt.Sub(paymentConfirmedAt).Nanoseconds()) / float64(time.Millisecond)

		// An identical acquire replay and terminal resume must not create a
		// second intent, settlement, submit, or delivery.
		replay := doJSON(t, server.URL+"/v1/episodes", http.MethodPost, m1MatrixAuthToken, request.RequestID, requestBody)
		var replayPayload struct {
			Replayed bool `json:"replayed"`
		}
		if replay.Code != http.StatusOK || json.Unmarshal(replay.Body.Bytes(), &replayPayload) != nil || !replayPayload.Replayed {
			t.Fatalf("task %s acquire replay was not idempotent: HTTP=%d body=%s", task.TaskID, replay.Code, replay.Body.String())
		}
		if _, err := runner.ResumeEpisode(context.Background(), episodeID); err != nil {
			t.Fatalf("task %s terminal resume: %v", task.TaskID, err)
		}
		intentsAfterReplay, err := intentStore.ListPaymentIntents(context.Background(), episodeID)
		if err != nil || len(intentsAfterReplay) != 1 || paymentAdapter.submits.Load() != int32(metrics.ConfirmedPayments+1) {
			// The current task's submit is included before ConfirmedPayments is
			// incremented below, so exactly one beyond the previous total is expected.
			t.Fatalf("task %s replay changed payment effects: intents=%d submits=%d err=%v", task.TaskID, len(intentsAfterReplay), paymentAdapter.submits.Load(), err)
		}
		if merchant.deliveries.Load() != int32(metrics.PurchaseTasksFulfilled+1) {
			t.Fatalf("task %s replay repeated delivery: delivery_calls=%d expected=%d", task.TaskID, merchant.deliveries.Load(), metrics.PurchaseTasksFulfilled+1)
		}

		trial.PaymentIntentID = intents[0].IntentID
		trial.TransactionID = intents[0].TxID
		trial.DeliveryEvidence = final.Artifact.PayloadRef
		trial.PaymentSettledAt = settledAt
		trial.DeliveryReceivedAt = final.Artifact.ReceivedAt
		trial.PaymentIntentCount = len(intents)
		trial.SettlementRows = settlementCount
		trial.ValidDelivery = validDelivery
		trial.ObservedOutcome = task.ExpectedOutcome
		trial.FinalState = string(final.Episode.State)
		trial.OutcomeCorrect = true
		trial.PaymentToDeliveryMS = paymentToDeliveryMS
		trial.TaskE2EMillis = float64(time.Since(started).Nanoseconds()) / float64(time.Millisecond)
		taskDurations = append(taskDurations, trial.TaskE2EMillis)
		paymentDeliveryDurations = append(paymentDeliveryDurations, paymentToDeliveryMS)
		trials = append(trials, trial)
		metrics.CorrectTerminalOutcomes++
		metrics.PurchaseTasksFulfilled++
		metrics.ConfirmedPayments++
		metrics.PostPaymentValidDeliveries++
		metrics.PaymentIntentRows += len(intents)
		metrics.PaymentSettledLedgerRows += settlementCount
		txIDs[intents[0].TxID]++
	}

	metrics.RunID = "m1-runtime-fixed-50-matrix-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	metrics.CorrectTerminalOutcomeRate = float64(metrics.CorrectTerminalOutcomes) / float64(metrics.TaskCount)
	metrics.PurchaseTaskDeliveryRate = float64(metrics.PurchaseTasksFulfilled) / float64(metrics.PurchaseAuthorizedTasks)
	metrics.PostPaymentValidDeliveryRate = float64(metrics.PostPaymentValidDeliveries) / float64(metrics.ConfirmedPayments)
	metrics.PaymentAdapterSubmits = paymentAdapter.submits.Load()
	metrics.MerchantDeliveryCalls = merchant.deliveries.Load()
	metrics.TaskE2EP50Millis = m1NearestRankPercentile(taskDurations, 0.50)
	metrics.TaskE2EP95Millis = m1NearestRankPercentile(taskDurations, 0.95)
	metrics.PaymentToDeliveryP50Millis = m1NearestRankPercentile(paymentDeliveryDurations, 0.50)
	metrics.PaymentToDeliveryP95Millis = m1NearestRankPercentile(paymentDeliveryDurations, 0.95)
	metrics.DuplicatePaymentIntentRows = metrics.PaymentIntentRows - metrics.ConfirmedPayments
	metrics.DuplicatePaymentSettledLedgerRows = metrics.PaymentSettledLedgerRows - metrics.ConfirmedPayments
	metrics.UnexpectedEconomicEffects = absM1Int(int(metrics.PaymentAdapterSubmits)-metrics.ConfirmedPayments) +
		absM1Int(int(metrics.MerchantDeliveryCalls)-metrics.PurchaseTasksFulfilled) +
		absM1Int(metrics.PaymentSettledLedgerRows-metrics.ConfirmedPayments) +
		absM1Int(metrics.PaymentIntentRows-metrics.ConfirmedPayments)
	for _, appearances := range txIDs {
		if appearances > 1 {
			metrics.DuplicateTransactionIDs += appearances - 1
		}
	}
	if metrics.CorrectTerminalOutcomes != 50 || metrics.PurchaseTasksFulfilled != 40 || metrics.ConfirmedPayments != 40 ||
		metrics.PostPaymentValidDeliveries != 40 || metrics.PriceChangeReconfirmed != 10 || metrics.BudgetRefusals != 4 ||
		metrics.Clarifications != 3 || metrics.BuyerRejections != 3 || metrics.PaymentAdapterSubmits != 40 ||
		metrics.MerchantDeliveryCalls != 40 || metrics.DuplicatePaymentIntentRows != 0 || metrics.DuplicatePaymentSettledLedgerRows != 0 ||
		metrics.DuplicateTransactionIDs != 0 || metrics.UnexpectedEconomicEffects != 0 {
		t.Fatalf("M1 fixed 50-task matrix invariant failed: %+v", metrics)
	}

	manifestDigest := sha256.Sum256(manifestBytes)
	artifactDir, err := m1MatrixWriteArtifacts(root, metrics, taskBytes, trials, map[string]any{
		"schema_version": "stablepay.m1-fixed-50-scenario-matrix-manifest.v1", "run_id": metrics.RunID,
		"created_at_utc": time.Now().UTC(), "git_sha": os.Getenv("STABLEPAY_M1_GIT_SHA"),
		"source_modified": os.Getenv("STABLEPAY_SOURCE_MODIFIED") == "true", "task_count": len(tasks), "task_suite_sha256": suiteHash,
		"merchant_mapping_manifest_sha256": "sha256:" + hex.EncodeToString(manifestDigest[:]), "merchant_sources_used": []string{"sim-m02", "sim-m05", "sim-m11"},
		"mapped_capability_count": len(capabilities), "intent_source": "RULE_BASELINE_NO_LLM", "runtime_entry": "authenticated Runtime HTTP catalog/search, episode acquire, BuyerOffer, signed BuyerConfirmation, status",
		"merchant_product_catalog": "read from the three local simulated Merchant HTTP product APIs; execution phase uses deterministic mock adapter",
		"payment_adapter":          "deterministic mock", "entitlement_adapter": "deterministic mock", "real_model_calls": false,
		"real_payment_calls": false, "chain_settlement": false, "persistence_backend": storeName, "mysql_audit": mysqlAudit,
		"metric_definitions": map[string]string{
			"correct_terminal_outcome_rate":    "tasks reaching their frozen expected outcome, including correct refusal/clarification/rejection, divided by all 50 tasks",
			"purchase_task_delivery_rate":      "the 40 approved-and-purchasable tasks reaching FULFILLED with valid delivery evidence divided by 40",
			"post_payment_valid_delivery_rate": "confirmed mock PaymentIntents with matching valid Runtime delivery evidence divided by confirmed mock PaymentIntents",
			"task_e2e_latency":                 "elapsed wall time from rule-baseline intent parsing through Runtime catalog search and task's expected terminal outcome; excludes human think time",
			"payment_to_delivery_latency":      "harness wall time from deterministic mock payment adapter returning CONFIRMED to Runtime.RunEpisode returning FULFILLED with valid delivery evidence; excludes buyer think time",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("M1_FIXED_50_MATRIX run_id=%s suite=%s correct_outcomes=%d/%d purchased_delivered=%d/%d post_payment_valid=%d/%d quote_changes_reconfirmed=%d/10 budget_refusals=%d clarifications=%d buyer_rejections=%d task_p50_ms=%.3f task_p95_ms=%.3f payment_to_delivery_p95_ms=%.3f duplicate_intents=%d duplicate_settlements=%d duplicate_tx_ids=%d persistence=%s evidence=%s",
		metrics.RunID, suiteHash, metrics.CorrectTerminalOutcomes, metrics.TaskCount, metrics.PurchaseTasksFulfilled, metrics.PurchaseAuthorizedTasks,
		metrics.PostPaymentValidDeliveries, metrics.ConfirmedPayments, metrics.PriceChangeReconfirmed, metrics.BudgetRefusals, metrics.Clarifications,
		metrics.BuyerRejections, metrics.TaskE2EP50Millis, metrics.TaskE2EP95Millis, metrics.PaymentToDeliveryP95Millis,
		metrics.DuplicatePaymentIntentRows, metrics.DuplicatePaymentSettledLedgerRows, metrics.DuplicateTransactionIDs, storeName, artifactDir)
}

func m1MatrixReadTasks(raw []byte) ([]m1MatrixTask, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	tasks := make([]m1MatrixTask, 0, 50)
	seen := make(map[string]struct{})
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var task m1MatrixTask
		if err := json.Unmarshal([]byte(line), &task); err != nil {
			return nil, err
		}
		if task.TaskID == "" || task.Prompt == "" || task.Scenario == "" || task.ExpectedOutcome == "" {
			return nil, fmt.Errorf("fixed M1 task is missing required fields")
		}
		if _, exists := seen[task.TaskID]; exists {
			return nil, fmt.Errorf("fixed M1 task ID is duplicated: %s", task.TaskID)
		}
		seen[task.TaskID] = struct{}{}
		tasks = append(tasks, task)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func m1MatrixParseNaturalLanguage(prompt string) m1MatrixParsedIntent {
	value := strings.ToLower(strings.TrimSpace(prompt))
	category := "category_unmapped"
	switch {
	case containsAnyM1(value, "ebook", "e-book", "book"):
		category = "category_books"
	case containsAnyM1(value, "developer", "coding", "code", "sdk", "programming"):
		category = "category_developer-tools"
	case containsAnyM1(value, "cloud", "hosting", "backup", "storage"):
		category = "category_cloud-tools"
	}
	budget := int64(200)
	if match := m1MatrixMoneyPattern.FindStringSubmatch(value); len(match) == 2 {
		amount, err := strconv.ParseFloat(match[1], 64)
		if err == nil && amount >= 0 {
			budget = int64(math.Round(amount * 100))
		}
	}
	return m1MatrixParsedIntent{CategoryTag: category, BudgetMinor: budget}
}

func containsAnyM1(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}

func m1MatrixHasTag(tags []string, expected string) bool {
	for _, tag := range tags {
		if tag == expected {
			return true
		}
	}
	return false
}

func absM1Int(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func m1MatrixAcquireRequest(task m1MatrixTask, requesterDID string, parsed m1MatrixParsedIntent, now time.Time) contract.AcquireCapabilityRequest {
	return contract.AcquireCapabilityRequest{RequestID: task.TaskID, ParentSessionID: "m1-fixed-50-matrix", RequesterDID: requesterDID,
		AcquisitionGoal: contract.AcquisitionGoal{TaskType: "product_purchase", Description: task.Prompt,
			SemanticConstraints: []contract.KeyValue{{Key: parsed.CategoryTag, Value: ""}}},
		Input: contract.Input{Ref: "fixture://m1/" + task.TaskID, ContentType: "application/json"},
		Constraints: contract.Constraints{BudgetLimitMinor: parsed.BudgetMinor, Currency: "USDC", DeadlineAt: now.Add(10 * time.Minute),
			SupportedProtocolVersions: []string{"x402-v2"}, MaxTotalAttempts: 5, MaxPaymentAttempts: 2, MaxDeliveryAttempts: 2, RequireBuyerConfirmation: true},
		ExpectedOutput: contract.ExpectedOutput{Schema: "digital-service-delivery-v1", ContentType: "application/json"},
		Validator:      contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"}}
}

func m1MatrixGetBuyerOffer(t *testing.T, baseURL, episodeID string) (application.BuyerOffer, error) {
	t.Helper()
	response := doJSON(t, baseURL+"/v1/episodes/"+episodeID, http.MethodGet, m1MatrixAuthToken, "", nil)
	if response.Code != http.StatusOK {
		return application.BuyerOffer{}, fmt.Errorf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		BuyerOffer *application.BuyerOffer `json:"buyer_offer"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		return application.BuyerOffer{}, err
	}
	if payload.BuyerOffer == nil {
		return application.BuyerOffer{}, fmt.Errorf("Runtime status omitted BuyerOffer")
	}
	return *payload.BuyerOffer, nil
}

func m1MatrixPostBuyerConfirmation(t *testing.T, baseURL, episodeID string, offer application.BuyerOffer, privateKey ed25519.PrivateKey, accepted bool, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()
	message := offer.AcceptMessage
	if !accepted {
		message = offer.RejectMessage
	}
	acceptMessage, err := base64.StdEncoding.DecodeString(message)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, acceptMessage))
	body, err := json.Marshal(map[string]any{"offer_ref": offer.OfferRef, "quote_hash": offer.QuoteHash, "amount_minor": offer.AmountMinor,
		"currency": offer.Currency, "accepted": accepted, "signature_base64": signature})
	if err != nil {
		t.Fatal(err)
	}
	return doJSON(t, baseURL+"/v1/episodes/"+episodeID+"/buyer-confirmations", http.MethodPost, m1MatrixAuthToken, idempotencyKey, body)
}

func countM1Settlements(entries []*ledger.LedgerEntry) int {
	count := 0
	for _, entry := range entries {
		if entry != nil && entry.Type == ledger.EntryPaymentSettled {
			count++
		}
	}
	return count
}

func m1MatrixWriteArtifacts(root string, metrics m1MatrixMetrics, tasks []byte, trials []m1MatrixTrial, manifest map[string]any) (string, error) {
	outputDir := filepath.Join(root, ".local-run", "commerce-runtime", "m1-fixed-50", metrics.RunID)
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "suite.jsonl"), tasks, 0o600); err != nil {
		return "", err
	}
	if err := m1MatrixWriteJSONLines(filepath.Join(outputDir, "trials.jsonl"), trials); err != nil {
		return "", err
	}
	if err := m1MatrixWriteJSON(filepath.Join(outputDir, "metrics.json"), metrics); err != nil {
		return "", err
	}
	if err := m1MatrixWriteJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		return "", err
	}
	return outputDir, nil
}

func m1MatrixWriteJSONLines(path string, values []m1MatrixTrial) error {
	var output strings.Builder
	for _, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		output.Write(encoded)
		output.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(output.String()), 0o600)
}

func m1MatrixWriteJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o600)
}

func m1MatrixNearestRank(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := int(math.Ceil(float64(len(sorted))*percentile)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}
