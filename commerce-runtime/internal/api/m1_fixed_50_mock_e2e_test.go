package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/repository"
	runtime "github.com/stablepay/commerce-runtime/internal/runtime"
)

const m1FixedTaskCount = 50

type m1FixedTask struct {
	TaskID      string `json:"task_id"`
	Prompt      string `json:"prompt"`
	TaskType    string `json:"task_type"`
	InputRef    string `json:"input_ref"`
	Quality     string `json:"quality,omitempty"`
	BudgetMinor int64  `json:"budget_limit_minor"`
}

type m1FixedTrial struct {
	TaskID                    string               `json:"task_id"`
	Prompt                    string               `json:"prompt"`
	RequestID                 string               `json:"request_id"`
	EpisodeID                 string               `json:"episode_id"`
	IntentSource              string               `json:"intent_source"`
	QueryHash                 string               `json:"query_hash"`
	Selected                  catalog.Candidate    `json:"selected_candidate"`
	FinalState                episode.State        `json:"final_state"`
	PaymentIntentCount        int                  `json:"payment_intent_count"`
	PaymentIntentStatus       payment.IntentStatus `json:"payment_intent_status,omitempty"`
	PaymentSettledLedgerCount int                  `json:"payment_settled_ledger_count"`
	DeliveryHTTPStatus        int                  `json:"delivery_http_status"`
	ValidationValid           bool                 `json:"validation_valid"`
	ReplayReturnedExisting    bool                 `json:"replay_returned_existing_episode"`
	PaymentSubmitsAfterReplay int32                `json:"payment_submits_after_replay"`
	TaskE2EMillis             float64              `json:"task_e2e_ms"`
	PaymentToDeliveryMillis   float64              `json:"payment_to_delivery_ms"`
}

type m1FixedMetrics struct {
	SchemaVersion                     string  `json:"schema_version"`
	RunID                             string  `json:"run_id"`
	EvidenceBoundary                  string  `json:"evidence_boundary"`
	TaskCount                         int     `json:"task_count"`
	TasksCreated                      int     `json:"tasks_created"`
	TasksFulfilled                    int     `json:"tasks_fulfilled"`
	TaskCompletionRate                float64 `json:"task_completion_rate"`
	TasksWithConfirmedPayment         int     `json:"tasks_with_confirmed_payment"`
	PostPaymentValidDeliveries        int     `json:"post_payment_valid_deliveries"`
	PostPaymentValidDeliveryRate      float64 `json:"post_payment_valid_delivery_rate"`
	PaymentIntentRows                 int     `json:"payment_intent_rows"`
	PaymentSettledLedgerRows          int     `json:"payment_settled_ledger_rows"`
	DuplicatePaymentIntentRows        int     `json:"duplicate_payment_intent_rows"`
	DuplicatePaymentSettledLedgerRows int     `json:"duplicate_payment_settled_ledger_rows"`
	DuplicateTransactionIDs           int     `json:"duplicate_transaction_ids"`
	PaymentAdapterSubmitCalls         int32   `json:"payment_adapter_submit_calls"`
	EntitlementVerificationCalls      int32   `json:"entitlement_verification_calls"`
	MerchantDeliveryCalls             int32   `json:"merchant_delivery_calls"`
	TaskE2EP50Millis                  float64 `json:"task_e2e_p50_ms"`
	TaskE2EP95Millis                  float64 `json:"task_e2e_p95_ms"`
	TaskE2EMaxMillis                  float64 `json:"task_e2e_max_ms"`
	PaymentToDeliveryP50Millis        float64 `json:"payment_to_delivery_p50_ms"`
	PaymentToDeliveryP95Millis        float64 `json:"payment_to_delivery_p95_ms"`
	PaymentToDeliveryMaxMillis        float64 `json:"payment_to_delivery_max_ms"`
	RealModelCalls                    bool    `json:"real_model_calls"`
	RealPaymentCalls                  bool    `json:"real_payment_calls"`
	PersistenceBackend                string  `json:"persistence_backend"`
	MySQLAudit                        string  `json:"mysql_audit"`
}

type m1MockMerchant struct {
	deliveries atomic.Int32
}

func (m *m1MockMerchant) Invoke(_ context.Context, request adapters.MerchantInvokeRequest) (adapters.MerchantInvokeResult, error) {
	now := time.Now().UTC()
	if request.Phase == invocation.PhaseInitial || request.Phase == invocation.PhaseQuoteRefresh {
		parsed, err := url.Parse(request.Endpoint.Endpoint)
		if err != nil {
			return adapters.MerchantInvokeResult{}, err
		}
		parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
		if len(parts) < 2 || parts[len(parts)-1] != "execute" {
			return adapters.MerchantInvokeResult{}, fmt.Errorf("mock Merchant received an unexpected resource path")
		}
		productID := parts[len(parts)-2]
		businessMinor, ok := m1MockPrices()[productID]
		if !ok {
			return adapters.MerchantInvokeResult{}, fmt.Errorf("mock Merchant does not recognize product %q", productID)
		}
		challenge, err := json.Marshal(map[string]any{
			"x402Version": 2,
			"resource":    map[string]any{"url": request.Endpoint.Endpoint},
			"accepts": []map[string]any{{"scheme": "exact", "network": "devnet", "amount": fmt.Sprintf("%d", businessMinor*10_000),
				"asset": "USDC", "payTo": "did:payee:m1-mock", "maxTimeoutSeconds": 300,
				"extra": map[string]any{"currency": "USDC", "productId": productID}}},
		})
		if err != nil {
			return adapters.MerchantInvokeResult{}, err
		}
		return adapters.MerchantInvokeResult{HTTPStatus: http.StatusPaymentRequired, ContentType: "application/json",
			Headers:    map[string]string{"PAYMENT-REQUIRED": base64.StdEncoding.EncodeToString(challenge)},
			PayloadRef: "m1-mock://quote/" + productID, PayloadHash: invocation.PayloadHash(challenge), Body: challenge, OccurredAt: now}, nil
	}
	if request.Phase != invocation.PhaseDelivery || request.PaymentIntentID == "" {
		return adapters.MerchantInvokeResult{}, fmt.Errorf("mock Merchant delivery was not bound to a PaymentIntent")
	}
	m.deliveries.Add(1)
	body, err := json.Marshal(map[string]string{"episode_id": request.EpisodeID, "capability_id": request.CapabilityID, "result": "deterministic mock service output"})
	if err != nil {
		return adapters.MerchantInvokeResult{}, err
	}
	return adapters.MerchantInvokeResult{HTTPStatus: http.StatusOK, ContentType: "application/json", PayloadRef: "m1-mock://delivery/" + request.EpisodeID,
		PayloadHash: invocation.PayloadHash(body), Body: body, OccurredAt: now}, nil
}

type m1MockEntitlement struct {
	verifications atomic.Int32
}

func (v *m1MockEntitlement) Verify(_ context.Context, query adapters.EntitlementQuery) (adapters.EntitlementResult, error) {
	v.verifications.Add(1)
	if query.IntentID == "" || query.TxID == "" || query.EpisodeID == "" {
		return adapters.EntitlementResult{Status: adapters.EntitlementInvalid, Reason: "missing transaction identity"}, nil
	}
	return adapters.EntitlementResult{Status: adapters.EntitlementValid, PaymentIntentID: query.IntentID, TxID: query.TxID,
		Reference: "m1-mock-entitlement:" + query.IntentID, EvidenceRef: "m1-mock-entitlement://" + query.IntentID}, nil
}

func TestM1Fixed50MockPurchaseE2E(t *testing.T) {
	if os.Getenv("STABLEPAY_M1_50_E2E") != "1" {
		t.Skip("set STABLEPAY_M1_50_E2E=1 to run the fixed 50-task Runtime mock E2E")
	}

	tasks := m1FixedTasks()
	store := repository.NewInMemoryStore()
	merchant := &m1MockMerchant{}
	paymentAdapter := &apiPayment{}
	entitlement := &m1MockEntitlement{}
	now := time.Now().UTC()
	service := application.NewService(store,
		application.WithClock(func() time.Time { return time.Now().UTC() }),
		application.WithUnconstrainedSettlementPolicyForTests(),
		application.WithMerchantAdapter(merchant),
		application.WithPaymentAdapters(application.PaymentDependencies{DID: apiDID{}, Payment: paymentAdapter, Entitlement: entitlement}),
		application.WithBuyerQuoteValidator(&currentBuyerQuote{}),
	)
	catalogHashes := m1RegisterCatalog(t, service, now)
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	requesterDID := "did:solana:" + solana.PublicKeyFromBytes(publicKey).String()
	runner := runtime.NewRunner(service, store)
	server := httptest.NewServer(NewServer(service, store, nil, AuthConfig{Token: "m1-fixed-mock-test"}, nil))
	defer server.Close()
	var paymentIntents repository.PaymentIntentRepository = store
	var ledgerStore repository.LedgerRepository = store

	trials := make([]m1FixedTrial, 0, len(tasks))
	taskE2EDurations := make([]float64, 0, len(tasks))
	paymentToDeliveryDurations := make([]float64, 0, len(tasks))
	txIDs := make(map[string]int)
	metrics := m1FixedMetrics{SchemaVersion: "stablepay.m1-fixed-mock-e2e.v1", EvidenceBoundary: "50 templated natural-language fixtures; pre-normalized deterministic intent; authenticated Runtime HTTP catalog/search/acquire/offer/confirmation/status; in-memory Runtime store; deterministic mock Merchant, payment, and entitlement adapters; no live services, model calls, MySQL, or chain settlement.",
		TaskCount: len(tasks), RealModelCalls: false, RealPaymentCalls: false, PersistenceBackend: "repository.InMemoryStore", MySQLAudit: "NOT_RUN_IN_MEMORY_BACKEND"}

	for _, task := range tasks {
		started := time.Now()
		request := m1FixedAcquireRequest(task, requesterDID, time.Now().UTC())
		query, err := catalog.FromAcquireCapabilityRequest(request)
		if err != nil {
			t.Fatalf("task %s normalize fixture intent: %v", task.TaskID, err)
		}
		searchBody, err := json.Marshal(catalogSearchPayload{Query: query, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		search := doJSON(t, server.URL+"/v1/catalog/search", http.MethodPost, "m1-fixed-mock-test", "", searchBody)
		if search.Code != http.StatusOK {
			t.Fatalf("task %s catalog search status=%d body=%s", task.TaskID, search.Code, search.Body.String())
		}
		var searchResult application.CatalogSearchResult
		if err := json.Unmarshal(search.Body.Bytes(), &searchResult); err != nil || len(searchResult.Candidates) == 0 {
			t.Fatalf("task %s has no Runtime catalog candidate: count=%d err=%v body=%s", task.TaskID, len(searchResult.Candidates), err, search.Body.String())
		}
		selected := searchResult.Candidates[0]
		if selected.ProductID == "" {
			t.Fatalf("task %s candidate has no stable SKU: %+v", task.TaskID, selected)
		}
		request.PurchaseSelection = &contract.PurchaseSelection{MerchantDID: selected.MerchantDID, CapabilityID: selected.CapabilityID, ProductID: selected.ProductID}
		requestBody, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		createdResponse := doJSON(t, server.URL+"/v1/episodes", http.MethodPost, "m1-fixed-mock-test", request.RequestID, requestBody)
		if createdResponse.Code != http.StatusAccepted {
			t.Fatalf("task %s create status=%d body=%s", task.TaskID, createdResponse.Code, createdResponse.Body.String())
		}
		var created struct {
			Episode *episode.CommerceEpisode `json:"episode"`
		}
		if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil || created.Episode == nil {
			t.Fatalf("task %s decode created Episode: err=%v body=%s", task.TaskID, err, createdResponse.Body.String())
		}
		episodeID := created.Episode.EpisodeID
		paused, err := runner.RunEpisode(context.Background(), episodeID)
		if err != nil || paused.Episode.State != episode.StateAwaitingBuyer || !paused.Paused {
			t.Fatalf("task %s did not stop at exact buyer-approval gate: state=%v paused=%v err=%v", task.TaskID, paused.Episode.State, paused.Paused, err)
		}
		beforeApproval, err := paymentIntents.ListPaymentIntents(context.Background(), episodeID)
		if err != nil || len(beforeApproval) != 0 || paymentAdapter.submits.Load() != int32(metrics.TasksFulfilled) {
			t.Fatalf("task %s created economic effects before approval: intents=%d submits=%d err=%v", task.TaskID, len(beforeApproval), paymentAdapter.submits.Load(), err)
		}
		offerResponse := doJSON(t, server.URL+"/v1/episodes/"+episodeID, http.MethodGet, "m1-fixed-mock-test", "", nil)
		if offerResponse.Code != http.StatusOK {
			t.Fatalf("task %s status/offer HTTP=%d body=%s", task.TaskID, offerResponse.Code, offerResponse.Body.String())
		}
		var offerPayload struct {
			BuyerOffer *application.BuyerOffer `json:"buyer_offer"`
		}
		if err := json.Unmarshal(offerResponse.Body.Bytes(), &offerPayload); err != nil || offerPayload.BuyerOffer == nil {
			t.Fatalf("task %s has no persisted buyer offer: err=%v body=%s", task.TaskID, err, offerResponse.Body.String())
		}
		acceptMessage, err := base64.StdEncoding.DecodeString(offerPayload.BuyerOffer.AcceptMessage)
		if err != nil {
			t.Fatal(err)
		}
		signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, acceptMessage))
		confirmationBody, err := json.Marshal(map[string]any{"offer_ref": offerPayload.BuyerOffer.OfferRef, "quote_hash": offerPayload.BuyerOffer.QuoteHash,
			"amount_minor": offerPayload.BuyerOffer.AmountMinor, "currency": offerPayload.BuyerOffer.Currency, "accepted": true, "signature_base64": signature})
		if err != nil {
			t.Fatal(err)
		}
		confirmation := doJSON(t, server.URL+"/v1/episodes/"+episodeID+"/buyer-confirmations", http.MethodPost,
			"m1-fixed-mock-test", "buyer-"+request.RequestID, confirmationBody)
		if confirmation.Code != http.StatusAccepted {
			t.Fatalf("task %s buyer confirmation status=%d body=%s", task.TaskID, confirmation.Code, confirmation.Body.String())
		}
		completed, err := runner.RunEpisode(context.Background(), episodeID)
		if err != nil || completed.Episode.State != episode.StateFulfilled {
			t.Fatalf("task %s did not complete through Runtime: state=%v err=%v", task.TaskID, completed.Episode.State, err)
		}

		finalResponse := doJSON(t, server.URL+"/v1/episodes/"+episodeID, http.MethodGet, "m1-fixed-mock-test", "", nil)
		var final struct {
			Episode    *episode.CommerceEpisode       `json:"episode"`
			Artifact   *invocation.DeliveryArtifact   `json:"artifact"`
			Validation *invocation.ValidationEvidence `json:"validation"`
		}
		if finalResponse.Code != http.StatusOK || json.Unmarshal(finalResponse.Body.Bytes(), &final) != nil || final.Episode == nil {
			t.Fatalf("task %s final status failed: HTTP=%d body=%s", task.TaskID, finalResponse.Code, finalResponse.Body.String())
		}
		intents, err := paymentIntents.ListPaymentIntents(context.Background(), episodeID)
		if err != nil || len(intents) != 1 || intents[0].Status != payment.IntentConfirmed {
			t.Fatalf("task %s expected one confirmed PaymentIntent; count=%d err=%v", task.TaskID, len(intents), err)
		}
		entries, err := ledgerStore.ListLedgerEntries(context.Background(), episodeID)
		if err != nil {
			t.Fatal(err)
		}
		settlements := 0
		var settledAt time.Time
		for _, entry := range entries {
			if entry.Type == ledger.EntryPaymentSettled {
				settlements++
				settledAt = entry.OccurredAt
			}
		}
		validDelivery := final.Validation != nil && final.Validation.Valid && final.Artifact != nil &&
			final.Artifact.PaymentIntentID == intents[0].IntentID && final.Artifact.HTTPStatus == http.StatusOK
		if settlements != 1 || settledAt.IsZero() || !validDelivery || final.Episode.State != episode.StateFulfilled {
			t.Fatalf("task %s failed economic/delivery audit: settled=%d valid=%v state=%s", task.TaskID, settlements, validDelivery, final.Episode.State)
		}
		paymentToDeliveryMS := float64(final.Artifact.ReceivedAt.Sub(settledAt).Microseconds()) / 1000
		if paymentToDeliveryMS < 0 {
			t.Fatalf("task %s delivery timestamp precedes PAYMENT_SETTLED: settled=%s delivered=%s", task.TaskID, settledAt, final.Artifact.ReceivedAt)
		}

		// Replaying the identical acquire and resuming the terminal Episode must
		// not create another PaymentIntent, settlement row, or payment submit.
		replay := doJSON(t, server.URL+"/v1/episodes", http.MethodPost, "m1-fixed-mock-test", request.RequestID, requestBody)
		var replayPayload struct {
			Replayed bool `json:"replayed"`
		}
		if replay.Code != http.StatusOK || json.Unmarshal(replay.Body.Bytes(), &replayPayload) != nil || !replayPayload.Replayed {
			t.Fatalf("task %s acquire replay was not idempotent: HTTP=%d body=%s", task.TaskID, replay.Code, replay.Body.String())
		}
		if _, err := runner.RunEpisode(context.Background(), episodeID); err != nil {
			t.Fatalf("task %s terminal resume: %v", task.TaskID, err)
		}
		if paymentAdapter.submits.Load() != int32(metrics.TasksFulfilled+1) {
			t.Fatalf("task %s replay repeated payment submit: submits=%d completed_before=%d", task.TaskID, paymentAdapter.submits.Load(), metrics.TasksFulfilled)
		}

		taskE2EMS := float64(time.Since(started).Microseconds()) / 1000
		taskE2EDurations = append(taskE2EDurations, taskE2EMS)
		paymentToDeliveryDurations = append(paymentToDeliveryDurations, paymentToDeliveryMS)
		txIDs[intents[0].TxID]++
		trial := m1FixedTrial{TaskID: task.TaskID, Prompt: task.Prompt, RequestID: request.RequestID, EpisodeID: episodeID,
			IntentSource: "FIXTURE_NORMALIZED_NO_LLM", QueryHash: searchResult.QueryHash, Selected: selected, FinalState: final.Episode.State,
			PaymentIntentCount: len(intents), PaymentIntentStatus: intents[0].Status, PaymentSettledLedgerCount: settlements,
			DeliveryHTTPStatus: final.Artifact.HTTPStatus, ValidationValid: validDelivery,
			ReplayReturnedExisting: replayPayload.Replayed, PaymentSubmitsAfterReplay: paymentAdapter.submits.Load(), TaskE2EMillis: taskE2EMS,
			PaymentToDeliveryMillis: paymentToDeliveryMS}
		trials = append(trials, trial)
		metrics.TasksCreated++
		metrics.TasksFulfilled++
		metrics.TasksWithConfirmedPayment++
		metrics.PostPaymentValidDeliveries++
		metrics.PaymentIntentRows += len(intents)
		metrics.PaymentSettledLedgerRows += settlements
	}

	for _, appearances := range txIDs {
		if appearances > 1 {
			metrics.DuplicateTransactionIDs += appearances - 1
		}
	}
	metrics.RunID = "m1-runtime-fixed-50-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	metrics.TaskCompletionRate = float64(metrics.TasksFulfilled) / float64(metrics.TaskCount)
	metrics.PostPaymentValidDeliveryRate = float64(metrics.PostPaymentValidDeliveries) / float64(metrics.TasksWithConfirmedPayment)
	metrics.PaymentAdapterSubmitCalls = paymentAdapter.submits.Load()
	metrics.EntitlementVerificationCalls = entitlement.verifications.Load()
	metrics.MerchantDeliveryCalls = merchant.deliveries.Load()
	metrics.TaskE2EP50Millis = m1NearestRankPercentile(taskE2EDurations, 0.50)
	metrics.TaskE2EP95Millis = m1NearestRankPercentile(taskE2EDurations, 0.95)
	metrics.TaskE2EMaxMillis = m1NearestRankPercentile(taskE2EDurations, 1)
	metrics.PaymentToDeliveryP50Millis = m1NearestRankPercentile(paymentToDeliveryDurations, 0.50)
	metrics.PaymentToDeliveryP95Millis = m1NearestRankPercentile(paymentToDeliveryDurations, 0.95)
	metrics.PaymentToDeliveryMaxMillis = m1NearestRankPercentile(paymentToDeliveryDurations, 1)
	metrics.DuplicatePaymentIntentRows = metrics.PaymentIntentRows - metrics.TasksWithConfirmedPayment
	metrics.DuplicatePaymentSettledLedgerRows = metrics.PaymentSettledLedgerRows - metrics.TasksWithConfirmedPayment
	if metrics.TasksCreated != len(tasks) || metrics.TasksFulfilled != len(tasks) || metrics.PaymentAdapterSubmitCalls != int32(len(tasks)) ||
		metrics.EntitlementVerificationCalls != int32(len(tasks)) || metrics.MerchantDeliveryCalls != int32(len(tasks)) || metrics.DuplicatePaymentIntentRows != 0 ||
		metrics.DuplicatePaymentSettledLedgerRows != 0 || metrics.DuplicateTransactionIDs != 0 {
		t.Fatalf("M1 50-task invariant failed: %+v", metrics)
	}

	taskHash, err := m1WriteFixedArtifacts(t, metrics, tasks, trials, catalogHashes)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("M1_FIXED_50_MOCK_E2E run_id=%s tasks_sha256=%s completed=%d/%d post_payment_valid_delivery=%d/%d task_e2e_p50_ms=%.3f task_e2e_p95_ms=%.3f payment_to_delivery_p50_ms=%.3f payment_to_delivery_p95_ms=%.3f duplicate_payment_intents=%d duplicate_settlements=%d evidence=%s",
		metrics.RunID, taskHash, metrics.TasksFulfilled, metrics.TaskCount, metrics.PostPaymentValidDeliveries, metrics.TasksWithConfirmedPayment,
		metrics.TaskE2EP50Millis, metrics.TaskE2EP95Millis, metrics.PaymentToDeliveryP50Millis, metrics.PaymentToDeliveryP95Millis,
		metrics.DuplicatePaymentIntentRows, metrics.DuplicatePaymentSettledLedgerRows,
		"local-mock/in-memory-not-MySQL")
}

func m1FixedTasks() []m1FixedTask {
	types := []struct{ taskType, label string }{{"transcription", "transcribe"}, {"translation", "translate"}, {"summarization", "summarize"}}
	profiles := []string{"standard", "high", "", "high", "standard"}
	tasks := make([]m1FixedTask, 0, m1FixedTaskCount)
	for index := 1; index <= m1FixedTaskCount; index++ {
		service := types[(index-1)%len(types)]
		quality := profiles[(index-1)%len(profiles)]
		asset := fmt.Sprintf("digital-asset-%03d", index)
		prompt := fmt.Sprintf("Please %s the digital asset %s and return a structured JSON result; spend no more than 10.00 USDC and wait for my approval of the exact quote before paying.", service.label, asset)
		if quality != "" {
			prompt = fmt.Sprintf("Please %s the digital asset %s with %s quality and return a structured JSON result; spend no more than 10.00 USDC and wait for my approval of the exact quote before paying.", service.label, asset, quality)
		}
		tasks = append(tasks, m1FixedTask{TaskID: fmt.Sprintf("m1-task-%03d", index), Prompt: prompt, TaskType: service.taskType,
			InputRef: "fixture://digital-service/" + asset, Quality: quality, BudgetMinor: 1000})
	}
	return tasks
}

func m1FixedAcquireRequest(task m1FixedTask, requesterDID string, now time.Time) contract.AcquireCapabilityRequest {
	semantic := []contract.KeyValue(nil)
	if task.Quality != "" {
		semantic = append(semantic, contract.KeyValue{Key: "quality", Value: task.Quality})
	}
	return contract.AcquireCapabilityRequest{RequestID: task.TaskID, ParentSessionID: "m1-fixed-50", RequesterDID: requesterDID,
		AcquisitionGoal: contract.AcquisitionGoal{TaskType: task.TaskType, Description: task.Prompt, SemanticConstraints: semantic},
		Input:           contract.Input{Ref: task.InputRef, ContentType: "application/json"},
		Constraints:     contract.Constraints{BudgetLimitMinor: task.BudgetMinor, Currency: "USDC", DeadlineAt: now.Add(10 * time.Minute), SupportedProtocolVersions: []string{"x402-v2"}, MaxTotalAttempts: 5, MaxPaymentAttempts: 2, MaxDeliveryAttempts: 2, RequireBuyerConfirmation: true},
		ExpectedOutput:  contract.ExpectedOutput{Schema: "digital-service-result-v1", ContentType: "application/json"},
		Validator:       contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"}}
}

func m1MockPrices() map[string]int64 {
	return map[string]int64{
		"transcription-standard-v1": 20, "transcription-high-v1": 40,
		"translation-standard-v1": 25, "translation-high-v1": 45,
		"summarization-standard-v1": 15, "summarization-high-v1": 35,
	}
}

func m1RegisterCatalog(t *testing.T, service *application.Service, now time.Time) []map[string]string {
	t.Helper()
	prices := m1MockPrices()
	keys := make([]string, 0, len(prices))
	for sku := range prices {
		keys = append(keys, sku)
	}
	sort.Strings(keys)
	hashes := make([]map[string]string, 0, len(keys))
	for _, sku := range keys {
		parts := strings.Split(sku, "-")
		taskType := parts[0]
		quality := parts[1]
		price := prices[sku]
		capability := &catalog.MerchantCapability{MerchantDID: "did:merchant:m1-mock", CapabilityID: strings.ReplaceAll(sku, "-v1", ""), ProductID: sku,
			PayeeDID: "did:payee:m1-mock", Name: taskType + " " + quality + " service", Description: "deterministic local digital-service fixture",
			TaskTypes: []string{taskType}, SemanticTags: []string{"quality=" + quality},
			InvokeEndpoint: catalog.EndpointRef{Endpoint: "https://mock.merchant.test/api/v1/products/" + sku + "/execute", Method: http.MethodGet},
			InputSchemaRef: "schema:input:json", OutputSchemaRef: "schema:output:json", InputContentTypes: []string{"application/json"}, OutputContentTypes: []string{"application/json"},
			SupportedProtocolVersions: []string{"x402-v2"}, SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed", PriceHintMinor: &price, PriceHintCurrency: "USDC",
			Status: catalog.StatusActive, Availability: catalog.AvailabilityAvailable, CatalogVersion: "m1-v1", Source: "m1-mock-fixture", SourceRef: "fixture://m1/" + sku,
			ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), CreatedAt: now, UpdatedAt: now}
		if err := service.RegisterCapabilityVersion(context.Background(), capability); err != nil {
			t.Fatalf("register M1 mock catalog SKU %s: %v", sku, err)
		}
		hash, err := capability.SnapshotHash()
		if err != nil {
			t.Fatal(err)
		}
		hashes = append(hashes, map[string]string{"task_type": taskType, "sku": sku, "catalog_snapshot_hash": hash})
	}
	return hashes
}

func m1NearestRankPercentile(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	index := int(float64(len(sorted))*percentile+0.999999999) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func m1WriteFixedArtifacts(t *testing.T, metrics m1FixedMetrics, tasks []m1FixedTask, trials []m1FixedTrial, catalogHashes []map[string]string) (string, error) {
	t.Helper()
	root, err := m1FindWorkspaceRoot()
	if err != nil {
		return "", err
	}
	outputDir := filepath.Join(root, ".local-run", "commerce-runtime", "m1-fixed-50", metrics.RunID)
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return "", err
	}
	var suite strings.Builder
	for _, task := range tasks {
		encoded, err := json.Marshal(task)
		if err != nil {
			return "", err
		}
		suite.Write(encoded)
		suite.WriteByte('\n')
	}
	suiteDigest := sha256.Sum256([]byte(suite.String()))
	taskHash := "sha256:" + hex.EncodeToString(suiteDigest[:])
	if err := os.WriteFile(filepath.Join(outputDir, "suite.jsonl"), []byte(suite.String()), 0o600); err != nil {
		return "", err
	}
	if err := m1WriteJSONLines(filepath.Join(outputDir, "trials.jsonl"), trials); err != nil {
		return "", err
	}
	if err := m1WriteJSON(filepath.Join(outputDir, "metrics.json"), metrics); err != nil {
		return "", err
	}
	catalogBytes, err := json.Marshal(catalogHashes)
	if err != nil {
		return "", err
	}
	catalogDigest := sha256.Sum256(catalogBytes)
	manifest := map[string]any{"schema_version": "stablepay.m1-fixed-mock-e2e-manifest.v1", "run_id": metrics.RunID, "created_at_utc": time.Now().UTC(),
		"git_sha": os.Getenv("STABLEPAY_M1_GIT_SHA"), "source_modified": os.Getenv("STABLEPAY_SOURCE_MODIFIED") == "true", "task_count": len(tasks), "task_suite_sha256": taskHash,
		"catalog_snapshot_set_sha256": "sha256:" + hex.EncodeToString(catalogDigest[:]), "catalog_snapshot_hashes": catalogHashes,
		"intent_source": "FIXTURE_NORMALIZED_NO_LLM", "payment_adapter": "deterministic mock", "merchant_adapter": "deterministic mock",
		"entitlement_adapter": "deterministic mock", "persistence_backend": "repository.InMemoryStore", "real_model_calls": false,
		"real_payment_calls": false, "mysql_audit": "NOT_RUN_IN_MEMORY_BACKEND", "chain_settlement": false,
		"metric_definition": map[string]string{"task_completion_rate": "FULFILLED Episodes / fixed tasks", "post_payment_valid_delivery_rate": "confirmed PaymentIntents with matching valid delivery evidence / confirmed PaymentIntents", "task_e2e_latency": "elapsed wall time from before HTTP catalog search through exact buyer confirmation, Runtime execution, and final valid FULFILLED status; intent is fixture-normalized and no LLM or think time is included", "payment_to_delivery_latency": "delivery artifact received_at minus the unique PAYMENT_SETTLED ledger occurred_at for the same episode; deterministic in-process mock adapters"}}
	if err := m1WriteJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		return "", err
	}
	t.Logf("M1 artifact directory: %s", outputDir)
	return taskHash, nil
}

func m1WriteJSONLines(path string, values []m1FixedTrial) error {
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

func m1WriteJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(path, encoded, 0o600)
}

func m1FindWorkspaceRoot() (string, error) {
	current, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(current, ".local-run")); err == nil {
			if _, err := os.Stat(filepath.Join(current, "commerce-runtime")); err == nil {
				return current, nil
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("could not locate StablePay workspace root for local evidence")
		}
		current = parent
	}
}
