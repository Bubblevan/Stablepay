// Package livelocal provides a test-only composition that reuses the real
// application service, Runtime runner, HTTP/MCP API, memory projection and
// observability surface with deterministic local adapters.
package livelocal

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/api"
	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/eval"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/memory"
	"github.com/stablepay/commerce-runtime/internal/observability"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/repository"
	runtime "github.com/stablepay/commerce-runtime/internal/runtime"
	"github.com/stablepay/commerce-runtime/internal/validator"
	workflowartifact "github.com/stablepay/commerce-runtime/internal/workflow/artifact"
	"github.com/stablepay/commerce-runtime/internal/workflowruntime"
)

type Config struct {
	MemoryMode string
	LLMMode    string
}

type Runtime struct {
	Store    *repository.InMemoryStore
	Service  *application.Service
	Runner   *runtime.Runner
	Workflow *workflowruntime.Manager
	Handler  http.Handler
	Variant  observability.RuntimeVariant
	Fault    *faultController
}

func New(ctx context.Context, config Config) (*Runtime, error) {
	memoryMode := strings.ToLower(strings.TrimSpace(config.MemoryMode))
	if memoryMode == "" {
		memoryMode = "on"
	}
	if memoryMode != "on" && memoryMode != "off" {
		return nil, errors.New("live-local memory mode must be on or off")
	}
	llmMode := strings.ToLower(strings.TrimSpace(config.LLMMode))
	if llmMode == "" {
		llmMode = "rule"
	}
	if llmMode != "rule" && llmMode != "local" {
		return nil, errors.New("live-local LLM mode must be rule or local")
	}
	store := repository.NewInMemoryStore()
	fault := &faultController{}
	merchant := &localMerchant{calls: map[string]int{}, fault: fault}
	serviceOptions := []application.Option{
		application.WithRuntimeVersion("s11-live-local"),
		application.WithIDGenerator(localIDGenerator()),
		application.WithMerchantAdapter(merchant),
		application.WithSettlementPolicy(application.SettlementPolicy{Network: "devnet", Assets: map[string]string{"USDC": "local-usdc-mint"}}),
		application.WithValidatorRegistry(validator.NewBuiltinRegistry()),
		application.WithPaymentAdapters(application.PaymentDependencies{DID: localDID{}, Payment: localPayment{}, Status: localPayment{}, Entitlement: localEntitlement{}}),
		application.WithRuleRecoveryFallback(true),
		application.WithInputPayloadResolver(workflowartifact.NewResolver(store, store)),
	}
	if memoryMode == "on" {
		serviceOptions = append(serviceOptions, application.WithMemoryStore(store))
	}
	if llmMode == "local" {
		provider := llm.NewLLMDecisionProvider(&localDecisionClient{calls: map[string]int{}, fault: fault}, llm.WithProviderName("local"), llm.WithModelRef("s11-local-adversarial"), llm.WithProviderMaxAttempts(1), llm.WithProviderTTL(time.Minute))
		serviceOptions = append(serviceOptions, application.WithLLMDecisionProvider(provider))
	}
	service := application.NewService(store, serviceOptions...)
	if err := seedCatalog(ctx, store); err != nil {
		return nil, err
	}
	if memoryMode == "on" {
		if err := seedMemory(ctx, store); err != nil {
			return nil, err
		}
	}
	runner := runtime.NewRunner(service, store, runtime.WithMaxSteps(80), runtime.WithRootContext(ctx), runtime.WithScanInterval(25*time.Millisecond), runtime.WithRetryBackoff(25*time.Millisecond, 200*time.Millisecond))
	if err := runner.StartSupervisor(ctx); err != nil {
		return nil, err
	}
	workflowRunner := workflowruntime.NewManager(store, service, store, runner, workflowruntime.Config{RootContext: ctx, ScanInterval: 25 * time.Millisecond})
	if err := workflowRunner.StartSupervisor(ctx); err != nil {
		runner.StopSupervisor()
		return nil, err
	}
	providerName, modelRef, recoveryProvider := "none", "rule-recovery", "rule"
	if llmMode == "local" {
		providerName, modelRef, recoveryProvider = "local", "s11-local-adversarial", "llm"
	}
	variant := observability.RuntimeVariant{RuntimeVersion: "s11-live-local", MemoryMode: memoryMode, RecoveryProvider: recoveryProvider, LLMProvider: providerName, ModelRef: modelRef}.WithHash()
	return &Runtime{Store: store, Service: service, Runner: runner, Workflow: workflowRunner, Handler: apiServer(service, store, runner, workflowRunner, variant, fault), Variant: variant, Fault: fault}, nil
}

func (r *Runtime) Close() {
	if r != nil && r.Workflow != nil {
		r.Workflow.StopSupervisor()
	}
	if r != nil && r.Runner != nil {
		r.Runner.StopSupervisor()
	}
}

func apiServer(service *application.Service, store *repository.InMemoryStore, runner *runtime.Runner, workflowRunner *workflowruntime.Manager, variant observability.RuntimeVariant, fault *faultController) http.Handler {
	return fault.Wrap(api.NewServerWithWorkflow(service, store, runner, workflowRunner, api.AuthConfig{AllowInsecure: true}, func(context.Context) error { return nil }, variant))
}

func localIDGenerator() func(string) string {
	var sequence uint64
	return func(prefix string) string { return fmt.Sprintf("%s-live-%d", prefix, atomic.AddUint64(&sequence, 1)) }
}

func seedCatalog(ctx context.Context, store *repository.InMemoryStore) error {
	now := time.Now().UTC().Truncate(time.Millisecond)
	fixtures := []struct {
		merchant, capability, taskType, name string
	}{
		{"did:merchant:local", "local-capability", "local-eval", "StablePay local capability"},
		{"did:merchant:local-hit", "local-capability-hit", "local-eval-memory-hit", "StablePay memory-hit capability"},
		{"did:merchant:local-miss", "local-capability-miss", "local-eval-memory-miss", "StablePay memory-miss capability"},
		{"did:merchant:s8-source", "s8-source", "s8-source", "StablePay S8 source capability"},
		{"did:merchant:s8-transform", "s8-transform", "s8-transform", "StablePay S8 transform capability"},
	}
	for _, fixture := range fixtures {
		price := int64(1000000)
		value := catalog.MerchantCapability{MerchantDID: fixture.merchant, CapabilityID: fixture.capability, PayeeDID: "did:payee:local", Name: fixture.name, Description: "Deterministic live-local capability", TaskTypes: []string{fixture.taskType}, SemanticTags: []string{"local", "eval"}, InvokeEndpoint: catalog.EndpointRef{Endpoint: "http://local.invalid/commerce/" + fixture.capability, Method: "GET"}, InputSchemaRef: "schema://local/input", OutputSchemaRef: "schema://local/output", InputContentTypes: []string{"text/plain"}, OutputContentTypes: []string{"text/plain"}, SupportedProtocolVersions: []string{"x402-v1"}, SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed", PriceHintMinor: &price, PriceHintCurrency: "USDC", Status: catalog.StatusActive, Availability: catalog.AvailabilityAvailable, CatalogVersion: "v1", Source: "s11-live-local", SourceRef: "s11-live-local:" + fixture.capability, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(24 * time.Hour), CreatedAt: now, UpdatedAt: now}
		hash, err := value.SnapshotHash()
		if err != nil {
			return err
		}
		value.SourceHash = hash
		if err := store.SaveCapabilityVersion(ctx, &value); err != nil {
			return err
		}
	}
	return nil
}

func seedMemory(ctx context.Context, store *repository.InMemoryStore) error {
	now := time.Now().UTC().Truncate(time.Millisecond)
	expires := now.Add(24 * time.Hour)
	value, err := store.GetCurrentActiveCapability(ctx, "did:merchant:local-hit", "local-capability-hit")
	if err != nil {
		return err
	}
	snapshotHash, err := value.SnapshotHash()
	if err != nil {
		return err
	}
	record := &memory.MemoryRecord{MemoryID: "s11-live-local-memory-hit", Type: memory.MemoryCapabilityOutcome, Scope: memory.ScopeMerchantCapability, MerchantDID: "did:merchant:local-hit", CapabilityID: "local-capability-hit", CatalogVersion: "v1", CatalogSnapshotHash: snapshotHash, CatalogSnapshotRef: value.SnapshotRef(), Summary: "local seeded recovery history", StructuredFacts: memory.OutcomeFacts{AttemptCount: 2, FulfilledCount: 2, DeliveryValidCount: 2, RecoverySuccessCount: 2, LastOutcome: "DELIVERY_VALID", LastOutcomeAt: now}, SourceEpisodeIDs: []string{"seeded-history"}, SourceEventRefs: []string{"seeded-history:event"}, ObservationCount: 1, Confidence: 0.8, FirstObservedAt: now, LastObservedAt: now, ValidFrom: now, ValidUntil: &expires, CreatedAt: now, UpdatedAt: now, FactsRef: "memory://s11-live-local-memory-hit"}
	if err := record.RefreshPayloadHash(); err != nil {
		return err
	}
	observation := &memory.MemoryObservation{ObservationID: memory.ObservationIDFor(record.MemoryID, "seeded-history", "CAPABILITY_OUTCOME"), MemoryID: record.MemoryID, SourceEpisodeID: "seeded-history", SourceEventRef: "seeded-history:event", ObservationKind: string(record.Type), Outcome: "DELIVERY_VALID", ObservedAt: now, CatalogVersion: record.CatalogVersion, CatalogSnapshotHash: record.CatalogSnapshotHash, CatalogSnapshotRef: record.CatalogSnapshotRef}
	if err := observation.RefreshPayloadHash(); err != nil {
		return err
	}
	return store.SaveObservationAndUpdateAggregate(ctx, record, observation, record.StructuredFacts)
}

type localMerchant struct {
	mu    sync.Mutex
	calls map[string]int
	fault *faultController
}

type faultController struct {
	mu             sync.Mutex
	CaseID         string    `json:"case_id"`
	Seed           int64     `json:"seed"`
	Kind           string    `json:"kind"`
	Trigger        string    `json:"trigger,omitempty"`
	RatePercent    int       `json:"rate_percent"`
	Configured     bool      `json:"configured"`
	RequestCount   int       `json:"request_count"`
	EligibleCount  int       `json:"eligible_count"`
	InjectionCount int       `json:"injection_count"`
	LastInjectedAt time.Time `json:"last_injected_at"`
	Evidence       string    `json:"evidence"`
	Repeat         int       `json:"-"`
}

type faultStatus struct {
	CaseID         string    `json:"case_id"`
	Seed           int64     `json:"seed"`
	Kind           string    `json:"kind"`
	Trigger        string    `json:"trigger,omitempty"`
	RatePercent    int       `json:"rate_percent"`
	Configured     bool      `json:"configured"`
	RequestCount   int       `json:"request_count"`
	EligibleCount  int       `json:"eligible_count"`
	InjectionCount int       `json:"injection_count"`
	LastInjectedAt time.Time `json:"last_injected_at"`
	Evidence       string    `json:"evidence"`
}

func (f *faultController) status() faultStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return faultStatus{CaseID: f.CaseID, Seed: f.Seed, Kind: f.Kind, Trigger: f.Trigger, RatePercent: f.RatePercent, Configured: f.Configured, RequestCount: f.RequestCount, EligibleCount: f.EligibleCount, InjectionCount: f.InjectionCount, LastInjectedAt: f.LastInjectedAt, Evidence: f.Evidence}
}

func (f *faultController) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/injections" && request.Method == http.MethodPost {
			var payload struct {
				CaseID  string `json:"case_id"`
				Seed    int64  `json:"seed"`
				Failure struct {
					Kind        string `json:"kind"`
					RatePercent int    `json:"rate_percent"`
					Repeat      int    `json:"repeat"`
					Trigger     string `json:"trigger"`
				} `json:"failure"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				http.Error(writer, err.Error(), http.StatusBadRequest)
				return
			}
			f.mu.Lock()
			f.CaseID, f.Seed, f.Kind, f.Trigger, f.RatePercent, f.Repeat = payload.CaseID, payload.Seed, payload.Failure.Kind, payload.Failure.Trigger, payload.Failure.RatePercent, payload.Failure.Repeat
			f.Configured, f.RequestCount, f.EligibleCount, f.InjectionCount = true, 0, 0, 0
			f.LastInjectedAt = time.Time{}
			f.Evidence = "live-local deterministic fault controller"
			f.mu.Unlock()
			writeLocalJSON(writer, http.StatusOK, map[string]any{"configured": true, "evidence": "live-local deterministic fault controller"})
			return
		}
		if request.URL.Path == "/v1/injections/current" && request.Method == http.MethodGet {
			writeLocalJSON(writer, http.StatusOK, f.status())
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (f *faultController) Observe(kind string, eligible bool) bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.Configured || (f.Kind != kind && f.Trigger != kind) {
		return false
	}
	f.RequestCount++
	if eligible {
		f.EligibleCount++
		limit := f.Repeat
		if limit <= 0 {
			limit = 1
		}
		if f.InjectionCount < limit {
			if f.RatePercent <= 0 || !eval.InjectAt(f.Seed, f.CaseID, kind, f.EligibleCount, f.RatePercent) {
				return false
			}
			f.InjectionCount++
			f.LastInjectedAt = time.Now().UTC()
			return true
		}
	}
	return false
}

func writeLocalJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func (m *localMerchant) Invoke(_ context.Context, request adapters.MerchantInvokeRequest) (adapters.MerchantInvokeResult, error) {
	m.mu.Lock()
	m.calls[request.EpisodeID]++
	call := m.calls[request.EpisodeID]
	m.mu.Unlock()
	now := time.Now().UTC()
	if request.Phase == "INITIAL" && strings.Contains(request.InputRef, "merchant-transient") && call <= 2 && m.fault.Observe("merchant_transient", true) {
		return adapters.MerchantInvokeResult{HTTPStatus: 599, ContentType: "application/problem+json", Body: []byte(`{"error":"transient local merchant"}`), OccurredAt: now, PayloadHash: "sha256:" + hashHex([]byte("transient")), PayloadRef: "merchant-response://transient"}, errors.New("local merchant temporarily unavailable")
	}
	if request.Phase == "INITIAL" {
		return localChallenge(request, now), nil
	}
	if request.Phase == "DELIVERY" && request.Attempt == 1 && m.fault.Observe("delivery_invalid", true) {
		return localDelivery(request, true, now), nil
	}
	return localDelivery(request, false, now), nil
}

func localChallenge(request adapters.MerchantInvokeRequest, now time.Time) adapters.MerchantInvokeResult {
	body := []byte(fmt.Sprintf(`{"x402Version":1,"accepts":[{"scheme":"exact","network":"devnet","maxAmountRequired":"1000000","asset":"local-usdc-mint","payTo":"did:payee:local","resource":"%s","maxTimeoutSeconds":300,"extra":{"currency":"USDC","productId":"local-product"}}]}`, request.Endpoint.Endpoint))
	encoded := base64.StdEncoding.EncodeToString(body)
	return adapters.MerchantInvokeResult{HTTPStatus: http.StatusPaymentRequired, Headers: map[string]string{"PAYMENT-REQUIRED": encoded}, ContentType: "application/json", Body: body, OccurredAt: now, PayloadHash: "sha256:" + hashHex(body), PayloadRef: "merchant-response://" + hashHex(body)}
}

func localDelivery(request adapters.MerchantInvokeRequest, invalid bool, now time.Time) adapters.MerchantInvokeResult {
	body := []byte("stablepay live-local artifact")
	switch request.CapabilityID {
	case "s8-source":
		body = []byte("hello")
	case "s8-transform":
		body = []byte(strings.ToUpper(string(request.InputPayload)))
	}
	if invalid {
		body = nil
	}
	return adapters.MerchantInvokeResult{HTTPStatus: http.StatusOK, ContentType: "text/plain", Body: body, OccurredAt: now, PayloadHash: "sha256:" + hashHex(body), PayloadRef: "merchant-response://" + hashHex(body)}
}

type localDID struct{}

func (localDID) AuthorizePayment(_ context.Context, request adapters.AuthorizationRequest) (adapters.AuthorizationResult, error) {
	return adapters.AuthorizationResult{Allowed: true, RequesterDID: request.Intent.RequesterDID, MerchantDID: request.Intent.MerchantDID, CapabilityID: request.Intent.CapabilityID, PayeeDID: request.PayeeDID, QuoteHash: request.Intent.QuoteHash, AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency, AuthorizationRef: "local-authorization:" + request.Intent.IntentID}, nil
}

type localPayment struct{}

func (localPayment) Submit(_ context.Context, request adapters.PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	tx := "local-tx:" + request.Intent.IntentID
	return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, IntentID: request.Intent.IntentID, TxID: tx, TxHash: tx, AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency}, nil
}

func (localPayment) Query(_ context.Context, request adapters.PaymentQuery) (payment.PaymentOutcome, error) {
	tx := "local-tx:" + request.Intent.IntentID
	return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, IntentID: request.Intent.IntentID, TxID: tx, TxHash: tx, AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency}, nil
}

type localEntitlement struct{}

func (localEntitlement) Verify(_ context.Context, query adapters.EntitlementQuery) (adapters.EntitlementResult, error) {
	return adapters.EntitlementResult{Status: adapters.EntitlementValid, PaymentIntentID: query.IntentID, TxID: query.TxID, TxHash: query.TxID, EvidenceRef: "verification:" + query.TxID}, nil
}

type localDecisionClient struct {
	mu    sync.Mutex
	calls map[string]int
	fault *faultController
}

func (c *localDecisionClient) GenerateDecision(_ context.Context, request llm.LLMDecisionRequest) (llm.LLMDecisionResponse, error) {
	episodeID := firstCapture(request.Prompt.User, `"episode_id":"([^"]+)"`)
	c.mu.Lock()
	c.calls[episodeID]++
	count := c.calls[episodeID]
	c.mu.Unlock()
	injectedGuardCase := c.fault != nil && c.fault.Observe("adversarial_guard", true)
	injectedMalformed := c.fault != nil && c.fault.Observe("llm_malformed", true)
	if injectedGuardCase && count == 1 {
		candidateSet := firstCapture(request.Prompt.User, `"candidate_set_id":"([^"]+)"`)
		version := firstCapture(request.Prompt.User, `"catalog_version":"([^"]+)"`)
		hash := firstCapture(request.Prompt.User, `"catalog_snapshot_hash":"(sha256:[a-fA-F0-9]+)"`)
		ref := firstCapture(request.Prompt.User, `"catalog_snapshot_ref":"([^"]+)"`)
		capability := firstCapture(request.Prompt.User, `"capability_id":"([^"]+)"`)
		evidence := firstCapture(request.Prompt.User, `(recovery://[^"[:space:]]+)`)
		body := fmt.Sprintf(`{"proposed_action":"SWITCH_MERCHANT","candidate_set_id":%q,"target":{"merchant_did":"did:merchant:evil","capability_id":%q,"catalog_version":%q,"catalog_snapshot_hash":%q,"catalog_snapshot_ref":%q},"evidence_refs":[%q],"rationale":"adversarial local proposal","confidence":0.9}`, candidateSet, capability, version, hash, ref, evidence)
		return llm.LLMDecisionResponse{Provider: "local", ModelRef: "s11-local-adversarial", RawJSON: []byte(body), ResponseReceivedAt: time.Now().UTC()}, nil
	}
	if injectedMalformed && count == 1 {
		return llm.LLMDecisionResponse{Provider: "local", ModelRef: "s11-local-adversarial", RawJSON: []byte(`{"malformed":true}`), ResponseReceivedAt: time.Now().UTC()}, nil
	}
	evidence := firstCapture(request.Prompt.User, `(recovery://[^"[:space:]]+)`)
	body := fmt.Sprintf(`{"proposed_action":"RETRY_SAME_MERCHANT","evidence_refs":[%q],"rationale":"deterministic local retry","confidence":0.8}`, evidence)
	return llm.LLMDecisionResponse{Provider: "local", ModelRef: "s11-local-adversarial", RawJSON: []byte(body), ResponseReceivedAt: time.Now().UTC()}, nil
}

var capturePatternCache sync.Map

func firstCapture(value, expression string) string {
	var re *regexp.Regexp
	if cached, ok := capturePatternCache.Load(expression); ok {
		re = cached.(*regexp.Regexp)
	} else {
		re = regexp.MustCompile(expression)
		capturePatternCache.Store(expression, re)
	}
	match := re.FindStringSubmatch(value)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func hashHex(value []byte) string {
	digest := sha256.Sum256(value)
	return fmt.Sprintf("%x", digest[:])
}

var _ adapters.MerchantAdapter = (*localMerchant)(nil)
var _ adapters.DIDAdapter = localDID{}
var _ adapters.PaymentAdapter = localPayment{}
var _ adapters.PaymentStatusAdapter = localPayment{}
var _ adapters.EntitlementAdapter = localEntitlement{}
var _ llm.LLMClient = (*localDecisionClient)(nil)
