// Command e4-local-runtime is a benchmark-only durable Runtime composition.
// It exercises the production application/runner/MySQL persistence path while
// replacing merchant, payment, authorization and verification adapters with
// deterministic local mocks. It never constructs a chain or Payment Service
// client.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/api"
	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/infrastructure/mysql"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/observability"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	runtime "github.com/stablepay/commerce-runtime/internal/runtime"
	"github.com/stablepay/commerce-runtime/internal/validator"
	workflowartifact "github.com/stablepay/commerce-runtime/internal/workflow/artifact"
	"github.com/stablepay/commerce-runtime/internal/workflowruntime"
)

const (
	localNetwork      = "e4-local"
	localAsset        = "e4-local-usdc"
	localMerchantDID  = "did:merchant:e4-local"
	localPayeeDID     = "did:payee:e4-local"
	localCapabilityID = "e4-local-eval"
	localPriceMinor   = int64(1000)
	localUSDCAtomic   = "10000000" // 1,000 cents is 10 USDC at six atomic decimals.
)

type mockPaymentModel struct {
	IntentID    string    `gorm:"column:intent_id;type:varchar(128);primaryKey"`
	TxID        string    `gorm:"column:tx_id;type:varchar(160);not null;uniqueIndex"`
	TxHash      string    `gorm:"column:tx_hash;type:varchar(160);not null"`
	AmountMinor int64     `gorm:"column:amount_minor;not null"`
	Currency    string    `gorm:"column:currency;type:varchar(16);not null"`
	Status      string    `gorm:"column:status;type:varchar(24);not null"`
	CreatedAt   time.Time `gorm:"column:created_at;not null"`
}

func (mockPaymentModel) TableName() string { return "e4_mock_payments" }

type localAdapters struct {
	db    *gorm.DB
	delay time.Duration
}

// e4BenchmarkStore adds controlled, benchmark-only dwell time to otherwise
// very short state windows. It embeds the production MySQL store and changes
// no production package or persistence semantics.
type e4BenchmarkStore struct {
	*mysql.Store
	adapters *localAdapters
}

func (s *e4BenchmarkStore) waitAtState(ctx context.Context, episodeID string, target episode.State) error {
	current, err := s.Store.Get(ctx, episodeID)
	if err != nil {
		return err
	}
	if current.State == target {
		return s.adapters.wait(ctx)
	}
	return nil
}

func (s *e4BenchmarkStore) GetCandidateSet(ctx context.Context, candidateSetID string) (*catalog.CandidateSet, error) {
	if strings.HasPrefix(candidateSetID, "cs:") {
		episodeID := strings.TrimPrefix(candidateSetID, "cs:")
		if separator := strings.IndexByte(episodeID, ':'); separator >= 0 {
			episodeID = episodeID[:separator]
		}
		if episodeID != "" {
			if err := s.waitAtState(ctx, episodeID, episode.StateDiscovering); err != nil {
				return nil, err
			}
		}
	}
	return s.Store.GetCandidateSet(ctx, candidateSetID)
}

func (s *e4BenchmarkStore) FindPaymentRequirementByInvocation(ctx context.Context, episodeID, invocationID string) (*invocation.PaymentRequirementFact, error) {
	current, err := s.Store.Get(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	if current.State == episode.StateNegotiating {
		if err := s.adapters.wait(ctx); err != nil {
			return nil, err
		}
	} else if current.State == episode.StateInvoking {
		fact, findErr := s.Store.GetMerchantInvocation(ctx, invocationID)
		if findErr == nil && fact.Phase == invocation.PhaseInitial && fact.ResponseStatus == 402 && !fact.CompletedAt.IsZero() {
			if err := s.adapters.wait(ctx); err != nil {
				return nil, err
			}
		}
	}
	return s.Store.FindPaymentRequirementByInvocation(ctx, episodeID, invocationID)
}

func (s *e4BenchmarkStore) GetDeliveryArtifact(ctx context.Context, deliveryID string) (*invocation.DeliveryArtifact, error) {
	artifact, err := s.Store.GetDeliveryArtifact(ctx, deliveryID)
	if err != nil {
		return nil, err
	}
	if err := s.waitAtState(ctx, artifact.EpisodeID, episode.StateValidatingDelivery); err != nil {
		return nil, err
	}
	return artifact, nil
}

func (s *e4BenchmarkStore) GetRecoveryContextByEpisode(ctx context.Context, episodeID string) (*recovery.RecoveryContext, error) {
	if err := s.waitAtState(ctx, episodeID, episode.StateRecovering); err != nil {
		return nil, err
	}
	return s.Store.GetRecoveryContextByEpisode(ctx, episodeID)
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dsn := strings.TrimSpace(os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"))
	if dsn == "" {
		return errors.New("COMMERCE_RUNTIME_MYSQL_DSN is required")
	}
	token := strings.TrimSpace(os.Getenv("COMMERCE_RUNTIME_API_TOKEN"))
	if token == "" {
		return errors.New("COMMERCE_RUNTIME_API_TOKEN is required")
	}
	addr := strings.TrimSpace(os.Getenv("COMMERCE_RUNTIME_HTTP_ADDR"))
	if addr == "" {
		return errors.New("COMMERCE_RUNTIME_HTTP_ADDR must be set to an isolated local port")
	}
	delay := 150 * time.Millisecond
	if value := strings.TrimSpace(os.Getenv("E4_MOCK_DELAY_MS")); value != "" {
		var millis int
		if _, err := fmt.Sscanf(value, "%d", &millis); err != nil || millis < 0 || millis > 5000 {
			return errors.New("E4_MOCK_DELAY_MS must be between 0 and 5000")
		}
		delay = time.Duration(millis) * time.Millisecond
	}

	db, err := mysql.Open(dsn)
	if err != nil {
		return fmt.Errorf("open isolated E4 MySQL schema: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := mysql.AutoMigrate(ctx, db); err != nil {
		return fmt.Errorf("migrate isolated E4 schema: %w", err)
	}
	if err := db.WithContext(ctx).AutoMigrate(&mockPaymentModel{}); err != nil {
		return fmt.Errorf("migrate E4 mock payment ledger: %w", err)
	}
	baseStore := mysql.NewStore(db)
	if err := seedCatalog(ctx, baseStore); err != nil {
		return fmt.Errorf("seed deterministic E4 capability: %w", err)
	}
	mocks := &localAdapters{db: db, delay: delay}
	store := &e4BenchmarkStore{Store: baseStore, adapters: mocks}
	service := application.NewService(store,
		application.WithRuntimeVersion("e4-local-mysql"),
		application.WithMerchantAdapter(mocks),
		application.WithSettlementPolicy(application.SettlementPolicy{Network: localNetwork, Assets: map[string]string{"USDC": localAsset}}),
		application.WithPaymentAdapters(application.PaymentDependencies{DID: mocks, Payment: mocks, Status: mocks, Entitlement: mocks}),
		application.WithRuleRecoveryFallback(true),
		application.WithValidatorRegistry(validator.NewBuiltinRegistry()),
		application.WithInputPayloadResolver(workflowartifact.NewResolver(store, store)),
		application.WithPersistentEvidenceStore(store),
	)
	runner := runtime.NewRunner(service, store,
		runtime.WithRootContext(ctx), runtime.WithMaxSteps(80),
		runtime.WithScanInterval(25*time.Millisecond), runtime.WithRetryBackoff(25*time.Millisecond, 250*time.Millisecond),
	)
	workflowRunner := workflowruntime.NewManager(store, service, store, runner, workflowruntime.Config{RootContext: ctx, ScanInterval: 25 * time.Millisecond})
	if err := runner.ResumePersisted(ctx); err != nil {
		return fmt.Errorf("resume existing E4 episodes: %w", err)
	}
	if err := runner.StartSupervisor(ctx); err != nil {
		return err
	}
	if err := workflowRunner.StartSupervisor(ctx); err != nil {
		runner.StopSupervisor()
		return err
	}
	defer workflowRunner.StopSupervisor()
	defer runner.StopSupervisor()
	variant := observability.RuntimeVariant{RuntimeVersion: "e4-local-mysql", MemoryMode: "off", RecoveryProvider: "rule", LLMProvider: "none", ModelRef: "benchmark-mock"}.WithHash()
	apiHandler := api.NewServerWithWorkflow(service, store, runner, workflowRunner, api.AuthConfig{Token: token}, func(check context.Context) error {
		return sqlDB.PingContext(check)
	}, variant)
	handler := withDurableInitialInvocationProbe(apiHandler, store, token)
	server := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("E4 benchmark-only Runtime listening on %s; network=%s; mock adapters enabled", addr, localNetwork)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// withDurableInitialInvocationProbe is deliberately wired only into the E4
// benchmark executable. It lets the harness distinguish the beginning of
// INVOKING from the point after the initial merchant 402 fact is committed.
func withDurableInitialInvocationProbe(next http.Handler, store *e4BenchmarkStore, token string) http.Handler {
	const prefix = "/__e4/durable-initial-invocation/"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, prefix) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		episodeID := strings.TrimPrefix(r.URL.Path, prefix)
		if episodeID == "" || strings.Contains(episodeID, "/") {
			http.Error(w, "episode id is required", http.StatusBadRequest)
			return
		}
		current, err := store.Get(r.Context(), episodeID)
		if err != nil {
			http.Error(w, "episode lookup failed", http.StatusInternalServerError)
			return
		}
		ready := false
		if current.State == episode.StateInvoking && current.SelectedMerchantDID != "" {
			key := "invoke:initial:" + episodeID + ":" + current.SelectedMerchantDID
			fact, findErr := store.FindMerchantInvocationByIdempotencyKey(r.Context(), episodeID, key)
			ready = findErr == nil && fact.Phase == invocation.PhaseInitial && fact.ResponseStatus == http.StatusPaymentRequired && !fact.CompletedAt.IsZero()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ready": ready})
	})
}

func seedCatalog(ctx context.Context, store *mysql.Store) error {
	price := localPriceMinor
	from := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	value := catalog.MerchantCapability{
		MerchantDID: localMerchantDID, CapabilityID: localCapabilityID, PayeeDID: localPayeeDID,
		Name: "E4 deterministic local capability", Description: "Benchmark-only local merchant and payment mocks",
		TaskTypes: []string{"local-eval"}, SemanticTags: []string{"e4", "local", "benchmark"},
		InvokeEndpoint: catalog.EndpointRef{Endpoint: "http://e4.local/commerce/e4-local-eval", Method: "GET"},
		InputSchemaRef: "schema://e4-local/input", OutputSchemaRef: "schema://e4-local/output",
		InputContentTypes: []string{"text/plain"}, OutputContentTypes: []string{"text/plain"},
		SupportedProtocolVersions: []string{"x402-v1"}, SupportedCurrencies: []string{"USDC"},
		PricingModel: "fixed", PriceHintMinor: &price, PriceHintCurrency: "USDC",
		Status: catalog.StatusActive, Availability: catalog.AvailabilityAvailable, CatalogVersion: "v1",
		Source: "e4-local-benchmark", SourceRef: "e4-local://capability/e4-local-eval",
		ValidFrom: from, ValidUntil: until, CreatedAt: from, UpdatedAt: from,
	}
	hash, err := value.SnapshotHash()
	if err != nil {
		return err
	}
	value.SourceHash = hash
	return store.SaveCapabilityVersion(ctx, &value)
}

func (m *localAdapters) wait(ctx context.Context) error {
	if m.delay <= 0 {
		return nil
	}
	timer := time.NewTimer(m.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (m *localAdapters) Invoke(ctx context.Context, request adapters.MerchantInvokeRequest) (adapters.MerchantInvokeResult, error) {
	initialInvocationWindow := request.Phase == invocation.PhaseInitial && strings.EqualFold(strings.TrimSpace(os.Getenv("E4_CRASH_WINDOW")), string(episode.StateInvoking))
	if !initialInvocationWindow {
		if err := m.wait(ctx); err != nil {
			return adapters.MerchantInvokeResult{}, err
		}
	}
	now := time.Now().UTC()
	if request.Phase == "INITIAL" {
		payload := map[string]any{"x402Version": 1, "accepts": []any{map[string]any{
			"scheme": "exact", "network": localNetwork, "maxAmountRequired": localUSDCAtomic, "asset": localAsset,
			"payTo": localPayeeDID, "resource": request.Endpoint.Endpoint, "maxTimeoutSeconds": 300,
			"extra": map[string]any{"currency": "USDC", "productId": "e4-local-product"},
		}}}
		body, err := json.Marshal(payload)
		if err != nil {
			return adapters.MerchantInvokeResult{}, err
		}
		return adapters.MerchantInvokeResult{HTTPStatus: http.StatusPaymentRequired, Headers: map[string]string{"PAYMENT-REQUIRED": base64.StdEncoding.EncodeToString(body)}, ContentType: "application/json", Body: body, OccurredAt: now, PayloadHash: hash(body), PayloadRef: "e4-local://merchant/quote/" + hash(body)}, nil
	}
	body := []byte("E4 deterministic local artifact")
	if request.Attempt == 1 && strings.EqualFold(strings.TrimSpace(os.Getenv("E4_CRASH_WINDOW")), string(episode.StateRecovering)) {
		body = nil // deterministic delivery fault exercises RECOVERING and retry
	}
	return adapters.MerchantInvokeResult{HTTPStatus: http.StatusOK, ContentType: "text/plain", Body: body, OccurredAt: now, PayloadHash: hash(body), PayloadRef: "e4-local://merchant/delivery/" + hash(body)}, nil
}

func (m *localAdapters) AuthorizePayment(_ context.Context, request adapters.AuthorizationRequest) (adapters.AuthorizationResult, error) {
	return adapters.AuthorizationResult{Allowed: true, RequesterDID: request.Intent.RequesterDID, MerchantDID: request.Intent.MerchantDID, CapabilityID: request.Intent.CapabilityID, PayeeDID: request.PayeeDID, QuoteHash: request.Intent.QuoteHash, AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency, AuthorizationRef: "e4-local-auth:" + request.Intent.IntentID}, nil
}

func (m *localAdapters) Submit(ctx context.Context, request adapters.PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	intent := request.Intent
	txID := "e4-local-tx:" + shortHash(intent.IntentID)
	row := mockPaymentModel{IntentID: intent.IntentID, TxID: txID, TxHash: "sha256:" + shortHash(intent.IntentID+":"+intent.RequestFingerprint), AmountMinor: intent.AmountMinor, Currency: strings.ToUpper(intent.Currency), Status: string(payment.OutcomeConfirmed), CreatedAt: time.Now().UTC()}
	if err := m.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "intent_id"}}, DoNothing: true}).Create(&row).Error; err != nil {
		return payment.PaymentOutcome{}, err
	}
	var persisted mockPaymentModel
	if err := m.db.WithContext(ctx).Where("intent_id = ?", intent.IntentID).First(&persisted).Error; err != nil {
		return payment.PaymentOutcome{}, err
	}
	if persisted.AmountMinor != intent.AmountMinor || !strings.EqualFold(persisted.Currency, intent.Currency) {
		return payment.PaymentOutcome{}, errors.New("E4 mock payment identity conflict")
	}
	if err := m.wait(ctx); err != nil {
		return payment.PaymentOutcome{}, err
	}
	return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, IntentID: intent.IntentID, TxID: persisted.TxID, TxHash: persisted.TxHash, AmountMinor: persisted.AmountMinor, Currency: persisted.Currency, OccurredAt: persisted.CreatedAt.UTC().Format(time.RFC3339Nano)}, nil
}

func (m *localAdapters) Query(ctx context.Context, request adapters.PaymentQuery) (payment.PaymentOutcome, error) {
	var row mockPaymentModel
	err := m.db.WithContext(ctx).Where("intent_id = ?", request.Intent.IntentID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return payment.PaymentOutcome{Status: payment.OutcomeUnknown, IntentID: request.Intent.IntentID, AmountMinor: request.Intent.AmountMinor, Currency: strings.ToUpper(request.Intent.Currency)}, nil
	}
	if err != nil {
		return payment.PaymentOutcome{}, err
	}
	if err := m.wait(ctx); err != nil {
		return payment.PaymentOutcome{}, err
	}
	return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, IntentID: row.IntentID, TxID: row.TxID, TxHash: row.TxHash, AmountMinor: row.AmountMinor, Currency: row.Currency, OccurredAt: row.CreatedAt.UTC().Format(time.RFC3339Nano)}, nil
}

func (m *localAdapters) Verify(ctx context.Context, query adapters.EntitlementQuery) (adapters.EntitlementResult, error) {
	if err := m.wait(ctx); err != nil {
		return adapters.EntitlementResult{}, err
	}
	return adapters.EntitlementResult{Status: adapters.EntitlementValid, PaymentIntentID: query.IntentID, TxID: query.TxID, EvidenceRef: "e4-local://verification/" + query.IntentID}, nil
}

func hash(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func shortHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:16])
}

var _ adapters.MerchantAdapter = (*localAdapters)(nil)
var _ adapters.DIDAdapter = (*localAdapters)(nil)
var _ adapters.PaymentAdapter = (*localAdapters)(nil)
var _ adapters.PaymentStatusAdapter = (*localAdapters)(nil)
var _ adapters.EntitlementAdapter = (*localAdapters)(nil)
