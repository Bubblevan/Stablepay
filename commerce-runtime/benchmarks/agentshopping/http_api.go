package agentshopping

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	simulationTaskSetupMicrosHeader         = "X-Simulation-Task-Setup-Micros"
	simulationIntentParseMicrosHeader       = "X-Simulation-Intent-Parse-Micros"
	simulationIntentPersistenceMicrosHeader = "X-Simulation-Intent-Persistence-Micros"
	simulationCatalogSearchMicrosHeader     = "X-Simulation-Catalog-Search-Micros"
)

type HTTPAPI struct {
	store                *MySQLStore
	parser               IntentParser
	apiKey               string
	buyerApprovalKey     string
	mux                  *http.ServeMux
	catalogSource        string
	catalogProducts      []Product
	catalogDiscovery     merchantCatalogDiscoveryIndex
	catalogProductsByKey map[string]struct{}
	catalogMerchantCount int
	catalogLoadedAt      time.Time
	buyerProfiles        map[string]BuyerRecord
	buyerSnapshotTenant  string
	buyerSnapshotLoaded  bool
}

func NewHTTPAPI(store *MySQLStore, parser IntentParser, apiKey string, buyerApprovalKey ...string) (*HTTPAPI, error) {
	return newHTTPAPI(store, parser, apiKey, buyerApprovalKey, nil, "mysql_simulation_snapshot", 0)
}

// NewHTTPAPIWithMerchantCatalog serves product facts and taxonomy from a
// startup snapshot fetched through the local Merchant HTTP listing APIs. The
// isolated MySQL store remains authoritative for regional inventory and all
// transactional state.
func NewHTTPAPIWithMerchantCatalog(store *MySQLStore, parser IntentParser, apiKey string, catalog ImportedCatalog, buyerApprovalKey ...string) (*HTTPAPI, error) {
	if len(catalog.Products) == 0 || len(catalog.Merchants) == 0 {
		return nil, errors.New("Merchant HTTP catalog snapshot must include merchants and products")
	}
	api, err := newHTTPAPI(store, parser, apiKey, buyerApprovalKey, catalog.Products, "merchant_http_snapshot", len(catalog.Merchants))
	if err != nil {
		return nil, err
	}
	if store.db != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		profiles, err := store.SnapshotBuyerProfiles(ctx)
		if err != nil {
			return nil, fmt.Errorf("load startup buyer-profile snapshot: %w", err)
		}
		api.buyerProfiles = profiles
		api.buyerSnapshotTenant = store.tenant
		api.buyerSnapshotLoaded = true
	}
	return api, nil
}

func newHTTPAPI(store *MySQLStore, parser IntentParser, apiKey string, buyerApprovalKeys []string, products []Product, source string, merchantCount int) (*HTTPAPI, error) {
	if store == nil || parser == nil || len(apiKey) < 32 {
		return nil, errors.New("simulation API requires MySQL store, intent parser, and an API key of at least 32 characters")
	}
	if len(buyerApprovalKeys) > 1 {
		return nil, errors.New("simulation API accepts at most one buyer-approval key")
	}
	buyerApprovalKey := ""
	if len(buyerApprovalKeys) == 1 {
		buyerApprovalKey = buyerApprovalKeys[0]
		if buyerApprovalKey != "" && len(buyerApprovalKey) < 32 {
			return nil, errors.New("buyer-approval key must contain at least 32 characters")
		}
	}
	api := &HTTPAPI{store: store, parser: parser, apiKey: apiKey, buyerApprovalKey: buyerApprovalKey,
		mux: http.NewServeMux(), catalogSource: source}
	if len(products) > 0 {
		api.catalogProducts = make([]Product, 0, len(products))
		api.catalogProductsByKey = make(map[string]struct{}, len(products))
		merchants := make(map[string]struct{})
		for _, product := range products {
			if strings.TrimSpace(product.MerchantID) == "" || strings.TrimSpace(product.SKUID) == "" ||
				product.Status != "active" || product.Currency != "USDC" || product.PriceMinor <= 0 ||
				strings.TrimSpace(product.Category) == "" || strings.TrimSpace(product.Item) == "" ||
				strings.TrimSpace(product.Format) == "" {
				return nil, fmt.Errorf("invalid product in Merchant HTTP catalog snapshot: merchant=%q sku=%q", product.MerchantID, product.SKUID)
			}
			key := catalogProductKey(product.MerchantID, product.SKUID)
			if _, exists := api.catalogProductsByKey[key]; exists {
				return nil, fmt.Errorf("duplicate product in Merchant HTTP catalog snapshot: merchant=%q sku=%q", product.MerchantID, product.SKUID)
			}
			product.Tags = append([]string(nil), product.Tags...)
			api.catalogProducts = append(api.catalogProducts, product)
			api.catalogProductsByKey[key] = struct{}{}
			merchants[product.MerchantID] = struct{}{}
		}
		if merchantCount == 0 {
			merchantCount = len(merchants)
		}
		api.catalogDiscovery = newMerchantCatalogDiscoveryIndex(api.catalogProducts)
		api.catalogMerchantCount = merchantCount
		api.catalogLoadedAt = time.Now().UTC()
	}
	api.routes()
	return api, nil
}

func (a *HTTPAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.mux.ServeHTTP(w, r) }

func (a *HTTPAPI) routes() {
	a.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "agentshopping-simulation", "simulation_only": true})
	})
	a.mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := a.store.Ping(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, "MYSQL_NOT_READY", "isolated simulation MySQL is not ready")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready", "database": SimulationDatabaseName, "simulation_only": true})
	})
	a.mux.Handle("GET /v1/dataset", a.auth(http.HandlerFunc(a.dataset)))
	a.mux.Handle("GET /v1/catalog/source", a.auth(http.HandlerFunc(a.catalogSourceStatus)))
	a.mux.Handle("GET /v1/buyers", a.auth(http.HandlerFunc(a.buyers)))
	a.mux.Handle("GET /v1/tasks/{taskID}", a.auth(http.HandlerFunc(a.getTask)))
	a.mux.Handle("POST /v1/intents/parse", a.auth(http.HandlerFunc(a.parseIntent)))
	a.mux.Handle("POST /v1/quotes", a.auth(http.HandlerFunc(a.issueQuote)))
	a.mux.Handle("GET /v1/quotes/{quoteID}", a.auth(http.HandlerFunc(a.getQuote)))
	a.mux.Handle("POST /v1/quotes/{quoteID}/confirm", a.auth(a.buyerApprovalAuth(http.HandlerFunc(a.confirmQuote))))
	a.mux.Handle("POST /v1/orders", a.auth(http.HandlerFunc(a.checkout)))
	a.mux.Handle("GET /v1/orders/{orderID}", a.auth(http.HandlerFunc(a.getOrder)))
	a.mux.Handle("POST /v1/orders/{orderID}/fulfillment", a.auth(http.HandlerFunc(a.fulfillOrder)))
	a.mux.Handle("GET /v1/orders/{orderID}/fulfillment", a.auth(http.HandlerFunc(a.getFulfillment)))
	a.mux.Handle("POST /v1/admin/inventory-adjustments", a.auth(http.HandlerFunc(a.adjustInventory)))
}

func (a *HTTPAPI) buyerApprovalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.buyerApprovalKey == "" {
			writeError(w, http.StatusServiceUnavailable, "BUYER_APPROVAL_NOT_CONFIGURED", "simulation buyer approval is not configured")
			return
		}
		provided := r.Header.Get("X-Simulation-Buyer-Approval-Key")
		if len(provided) != len(a.buyerApprovalKey) || subtle.ConstantTimeCompare([]byte(provided), []byte(a.buyerApprovalKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "BUYER_APPROVAL_REQUIRED", "an independent buyer-approval credential is required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *HTTPAPI) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		provided := r.Header.Get("X-Simulation-API-Key")
		if len(provided) != len(a.apiKey) || subtle.ConstantTimeCompare([]byte(provided), []byte(a.apiKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "simulation API key is missing or invalid")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *HTTPAPI) dataset(w http.ResponseWriter, r *http.Request) {
	counts, err := a.store.Counts(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "DATASET_READ_FAILED", "could not read isolated simulation dataset")
		return
	}
	productCount, merchantCount := int64(len(a.catalogProducts)), int64(a.catalogMerchantCount)
	if len(a.catalogProducts) == 0 {
		productCount, merchantCount = counts["products"], counts["merchants"]
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenant_id": a.store.tenant, "counts": counts,
		"data_origin": syntheticOrigin, "catalog_source": a.catalogSource,
		"catalog_products": productCount, "catalog_merchants": merchantCount,
		"payment_provider": "simulation-only", "real_settlement": false})
}

func (a *HTTPAPI) catalogSourceStatus(w http.ResponseWriter, r *http.Request) {
	productCount, merchantCount := int64(len(a.catalogProducts)), int64(a.catalogMerchantCount)
	if len(a.catalogProducts) == 0 {
		counts, err := a.store.Counts(r.Context())
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "CATALOG_STATUS_UNAVAILABLE", "could not inspect the isolated MySQL catalog")
			return
		}
		productCount, merchantCount = counts["products"], counts["merchants"]
	}
	status := map[string]any{"source": a.catalogSource, "product_count": productCount,
		"merchant_count": merchantCount, "simulation_only": true}
	if !a.catalogLoadedAt.IsZero() {
		status["snapshot_loaded_at_utc"] = a.catalogLoadedAt.Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, status)
}

func (a *HTTPAPI) readTaxonomy(ctx context.Context) (Taxonomy, error) {
	if len(a.catalogProducts) > 0 {
		return TaxonomyFromProducts(a.catalogProducts), nil
	}
	return a.store.ReadTaxonomy(ctx)
}

func (a *HTTPAPI) findCandidates(ctx context.Context, intent Intent, limit int) ([]Candidate, error) {
	if len(a.catalogProducts) > 0 {
		catalog := a.catalogProducts
		if a.catalogDiscovery != nil {
			intent.discoveryDocs = a.catalogDiscovery.documentsForIntent(intent)
			catalog = a.catalogDiscovery.productsForIntent(intent)
		}
		return a.store.FindCandidatesFromMerchantCatalog(ctx, catalog, intent, limit)
	}
	return a.store.FindCandidates(ctx, intent, limit)
}

func (a *HTTPAPI) buyers(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := a.store.ListBuyers(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "BUYER_LIST_FAILED", "could not read synthetic buyer profiles")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"buyers": rows, "count": len(rows), "data_origin": syntheticOrigin})
}

type parseIntentRequest struct {
	BuyerID         string `json:"buyer_id"`
	Prompt          string `json:"prompt"`
	IdempotencyKey  string `json:"idempotency_key"`
	ClarifiesTaskID string `json:"clarifies_task_id,omitempty"`
}

func (a *HTTPAPI) parseIntent(w http.ResponseWriter, r *http.Request) {
	var request parseIntentRequest
	if !readJSON(w, r, &request) {
		return
	}
	taskSetupStarted := time.Now()
	buyer, err := a.getBuyer(r.Context(), strings.TrimSpace(request.BuyerID))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	task, err := a.store.createTaskForKnownBuyer(r.Context(), buyer, request.Prompt, request.IdempotencyKey, request.ClarifiesTaskID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	setSimulationStageDuration(w, simulationTaskSetupMicrosHeader, time.Since(taskSetupStarted))
	if task.Status != TaskStatusIntentPending {
		if task.Status == TaskStatusIntentParseFailed {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]string{"code": "INTENT_PARSE_FAILED", "message": "the original intent parse failed"},
				"task_id": task.TaskID, "task_status": task.Status, "provider": task.Provider, "model": task.Model, "idempotent_replay": true})
			return
		}
		var intent Intent
		if err := json.Unmarshal(task.IntentJSON, &intent); err != nil {
			writeError(w, http.StatusInternalServerError, "TASK_STATE_INVALID", "persisted shopping intent could not be read")
			return
		}
		intent.searchText = request.Prompt
		if task.Status == TaskStatusClarificationRequired || task.Status == TaskStatusClarificationResolved {
			writeIntentClarification(w, task.TaskID, task.Status, intent, task.Provider, task.Model, task.ResolvedByTaskID, true)
			return
		}
		searchStarted := time.Now()
		candidates, err := a.findCandidates(r.Context(), intent, 20)
		setSimulationCatalogSearchDuration(w, time.Since(searchStarted))
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "CATALOG_READ_FAILED", "could not search the simulated catalog")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task_id": task.TaskID, "task_status": task.Status, "intent": intent,
			"candidates": candidates, "provider": task.Provider, "model": task.Model,
			"buyer_confirmation_required": task.Status == TaskStatusDiscovered || task.Status == TaskStatusQuoted,
			"idempotent_replay":           true})
		return
	}
	taxonomy, err := a.readTaxonomy(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "CATALOG_TAXONOMY_UNAVAILABLE", "could not load the simulated catalog taxonomy")
		return
	}
	parseCtx, cancel := contextWithParseTimeout(r)
	defer cancel()
	started := time.Now()
	var intent Intent
	var usage ParseUsage
	var parseErr error
	if observable, ok := a.parser.(ObservableIntentParser); ok {
		intent, usage, parseErr = observable.ParseWithUsage(parseCtx, request.Prompt, buyer.HomeRegion, buyer.BudgetMinor, taxonomy)
	} else {
		intent, parseErr = a.parser.Parse(parseCtx, request.Prompt, buyer.HomeRegion, buyer.BudgetMinor, taxonomy)
		usage.UsageStatus = "TOKEN_METRIC_UNAVAILABLE"
	}
	if parseErr == nil {
		intent, _ = canonicalizeIntentFromTaxonomy(intent, taxonomy)
		intent = normalizeIntentClarification(intent)
		intent.searchText = request.Prompt
	}
	latency := time.Since(started)
	setSimulationStageDuration(w, simulationIntentParseMicrosHeader, latency)
	intentPersistenceStarted := time.Now()
	persistErr := a.store.saveParsedIntentForKnownTask(r.Context(), task, a.parser.Provider(), a.parser.Model(), intent, latency, parseErr, usage)
	setSimulationStageDuration(w, simulationIntentPersistenceMicrosHeader, time.Since(intentPersistenceStarted))
	if persistErr != nil {
		writeError(w, http.StatusServiceUnavailable, "INTENT_TRACE_WRITE_FAILED", "could not persist the simulation intent trace")
		return
	}
	if parseErr != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]string{"code": "INTENT_PARSE_FAILED", "message": "Hermes could not produce a valid shopping intent"},
			"task_id": task.TaskID, "provider": a.parser.Provider(), "model": a.parser.Model()})
		return
	}
	if intent.ClarificationRequired {
		writeIntentClarification(w, task.TaskID, TaskStatusClarificationRequired, intent, a.parser.Provider(), a.parser.Model(), "", false)
		return
	}
	searchStarted := time.Now()
	candidates, err := a.findCandidates(r.Context(), intent, 20)
	setSimulationCatalogSearchDuration(w, time.Since(searchStarted))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "CATALOG_SEARCH_FAILED", "could not search the simulated catalog")
		return
	}
	if len(candidates) == 0 {
		if err := a.store.SetTaskStatus(r.Context(), task.TaskID, TaskStatusNoMatchingCandidates); err != nil {
			writeError(w, http.StatusServiceUnavailable, "TASK_STATE_WRITE_FAILED", "could not persist the no-candidate task status")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task_id": task.TaskID, "task_status": TaskStatusNoMatchingCandidates,
			"clarifies_task_id": task.ClarifiesTaskID, "intent": intent,
			"candidates": []Candidate{}, "provider": a.parser.Provider(), "model": a.parser.Model(),
			"message": "No in-stock catalog item matches this region and budget; no quote or payment was created."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task_id": task.TaskID, "task_status": TaskStatusDiscovered,
		"clarifies_task_id": task.ClarifiesTaskID, "intent": intent, "candidates": candidates,
		"provider": a.parser.Provider(), "model": a.parser.Model(), "latency_ms": latency.Milliseconds(), "buyer_confirmation_required": true})
}

func (a *HTTPAPI) getBuyer(ctx context.Context, buyerID string) (BuyerRecord, error) {
	if a.buyerSnapshotLoaded && a.buyerSnapshotTenant == a.store.tenant {
		buyer, ok := a.buyerProfiles[buyerID]
		if !ok {
			return BuyerRecord{}, ErrNotFound
		}
		return buyer, nil
	}
	return a.store.GetBuyer(ctx, buyerID)
}

func writeIntentClarification(w http.ResponseWriter, taskID, status string, intent Intent, provider, model, resolvedByTaskID string, replay bool) {
	payload := map[string]any{"task_id": taskID, "task_status": status,
		"intent": intent, "candidates": []Candidate{}, "clarification_question": intent.ClarificationQuestion,
		"clarification_fields": missingIntentClarificationFields(intent), "provider": provider, "model": model,
		"buyer_confirmation_required": false, "payment_created": false, "real_settlement": false,
		"idempotent_replay": replay,
		"message":           "请补充缺失的购物条件后再搜索；当前没有创建报价或支付。"}
	if resolvedByTaskID != "" {
		payload["resolved_by_task_id"] = resolvedByTaskID
	}
	writeJSON(w, http.StatusOK, payload)
}

type issueQuoteRequest struct {
	TaskID         string `json:"task_id"`
	BuyerID        string `json:"buyer_id"`
	MerchantID     string `json:"merchant_id"`
	SKUID          string `json:"sku_id"`
	Region         string `json:"region"`
	MaxMinor       int64  `json:"max_price_minor"`
	Currency       string `json:"currency"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (a *HTTPAPI) issueQuote(w http.ResponseWriter, r *http.Request) {
	var request issueQuoteRequest
	if !readJSON(w, r, &request) {
		return
	}
	if len(a.catalogProductsByKey) > 0 {
		if _, exists := a.catalogProductsByKey[catalogProductKey(request.MerchantID, request.SKUID)]; !exists {
			writeError(w, http.StatusConflict, "CATALOG_PRODUCT_UNAVAILABLE", "product is not present in the active Merchant HTTP catalog snapshot")
			return
		}
	}
	quote, err := a.store.IssueQuote(r.Context(), PersistentQuoteRequest{TaskID: request.TaskID,
		BuyerID: request.BuyerID, MerchantID: request.MerchantID, SKUID: request.SKUID,
		Region: request.Region, MaxMinor: request.MaxMinor, Currency: request.Currency,
		IdempotencyKey: request.IdempotencyKey})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"quote": quote, "immutable": true, "buyer_confirmation_required": true})
}

func (a *HTTPAPI) getQuote(w http.ResponseWriter, r *http.Request) {
	quote, err := a.store.GetQuote(r.Context(), r.PathValue("quoteID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"quote": quote, "immutable": true})
}

type confirmQuoteRequest struct {
	BuyerID        string `json:"buyer_id"`
	Accepted       bool   `json:"accepted"`
	AmountMinor    int64  `json:"amount_minor"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (a *HTTPAPI) confirmQuote(w http.ResponseWriter, r *http.Request) {
	var request confirmQuoteRequest
	if !readJSON(w, r, &request) {
		return
	}
	confirmationID, err := newBuyerConfirmationID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CONFIRMATION_ID_UNAVAILABLE", "could not create a protected buyer confirmation")
		return
	}
	confirmation, err := a.store.ConfirmBuyer(r.Context(), PersistentConfirmRequest{BuyerID: request.BuyerID,
		QuoteID: r.PathValue("quoteID"), Accepted: request.Accepted, AmountMinor: request.AmountMinor,
		IdempotencyKey: request.IdempotencyKey, ConfirmationID: confirmationID})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"confirmation": confirmation, "payment_created": false,
		"message": "Buyer confirmation was recorded. Submit a separate order request to run the simulation-only payment."})
}

func newBuyerConfirmationID() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return "confirmation_" + hex.EncodeToString(token[:]), nil
}

type checkoutRequest struct {
	ConfirmationID string `json:"confirmation_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (a *HTTPAPI) checkout(w http.ResponseWriter, r *http.Request) {
	var request checkoutRequest
	if !readJSON(w, r, &request) {
		return
	}
	result, err := a.store.Checkout(r.Context(), a.store.tenant, request.ConfirmationID, request.IdempotencyKey)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"order": result.Order, "payment": result.Payment,
		"simulation_only": true, "real_settlement": false, "chain_transaction": nil})
}

func (a *HTTPAPI) getOrder(w http.ResponseWriter, r *http.Request) {
	order, payment, err := a.store.GetOrder(r.Context(), r.PathValue("orderID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"order": order, "payment": payment, "simulation_only": true,
		"real_settlement": false, "chain_transaction": nil})
}

type fulfillmentRequest struct {
	IdempotencyKey string `json:"idempotency_key"`
}

func (a *HTTPAPI) fulfillOrder(w http.ResponseWriter, r *http.Request) {
	var request fulfillmentRequest
	if !readJSON(w, r, &request) {
		return
	}
	result, err := a.store.FulfillOrder(r.Context(), r.PathValue("orderID"), request.IdempotencyKey)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	status := http.StatusOK
	if result.Fulfillment.Status == FulfillmentRetryPending {
		status = http.StatusAccepted
	}
	writeFulfillmentResult(w, status, result)
}

func (a *HTTPAPI) getFulfillment(w http.ResponseWriter, r *http.Request) {
	result, err := a.store.GetFulfillment(r.Context(), r.PathValue("orderID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeFulfillmentResult(w, http.StatusOK, result)
}

func writeFulfillmentResult(w http.ResponseWriter, status int, result FulfillmentResult) {
	writeJSON(w, status, map[string]any{"order": result.Order, "payment": result.Payment,
		"fulfillment": result.Fulfillment, "refund": result.Refund, "simulation_only": true,
		"real_settlement": false, "real_refund": false})
}

func (a *HTTPAPI) getTask(w http.ResponseWriter, r *http.Request) {
	task, err := a.store.GetTask(r.Context(), r.PathValue("taskID"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task_id": task.TaskID, "task_status": task.Status,
		"clarifies_task_id": task.ClarifiesTaskID, "resolved_by_task_id": task.ResolvedByTaskID,
		"provider": task.Provider, "model": task.Model, "data_origin": task.DataOrigin})
}

type inventoryAdjustmentRequest struct {
	TaskID         string `json:"task_id"`
	MerchantID     string `json:"merchant_id"`
	SKUID          string `json:"sku_id"`
	Region         string `json:"region"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
	PriceMinor     *int64 `json:"price_minor,omitempty"`
	OnHand         *int   `json:"on_hand,omitempty"`
}

func (a *HTTPAPI) adjustInventory(w http.ResponseWriter, r *http.Request) {
	var request inventoryAdjustmentRequest
	if !readJSON(w, r, &request) {
		return
	}
	if err := a.store.ChangeInventory(r.Context(), request.TaskID, request.MerchantID, request.SKUID,
		request.Region, request.Reason, request.IdempotencyKey, request.PriceMinor, request.OnHand); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "updated", "quote_revision_incremented": true, "simulation_only": true})
}

func readJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must be a valid JSON object")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_JSON", "request body must contain exactly one JSON object")
		return false
	}
	return true
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "simulation record was not found")
	case errors.Is(err, ErrUnavailable):
		writeError(w, http.StatusConflict, "NOT_AVAILABLE", "item is unavailable in this region")
	case errors.Is(err, ErrBudgetExceeded):
		writeError(w, http.StatusUnprocessableEntity, "BUDGET_EXCEEDED", "quote exceeds the buyer's budget")
	case errors.Is(err, ErrCandidateNotAllowed):
		writeError(w, http.StatusConflict, "CANDIDATE_NOT_ALLOWED", "selected product does not match the request or an explicitly allowed substitute")
	case errors.Is(err, ErrStaleQuote):
		writeError(w, http.StatusConflict, "STALE_QUOTE", "price, stock, or quote validity changed; obtain a new quote and ask the buyer again")
	case errors.Is(err, ErrQuoteAmountMismatch):
		writeError(w, http.StatusConflict, "QUOTE_AMOUNT_MISMATCH", "confirmation amount must equal the immutable quote amount")
	case errors.Is(err, ErrConfirmationRequired):
		writeError(w, http.StatusConflict, "BUYER_CONFIRMATION_REQUIRED", "an accepted buyer confirmation is required before checkout")
	case errors.Is(err, ErrOrderNotReady):
		writeError(w, http.StatusConflict, "ORDER_NOT_READY", "only a settled paid order can enter simulated fulfillment")
	case errors.Is(err, ErrClarificationNotOpen):
		writeError(w, http.StatusConflict, "CLARIFICATION_NOT_OPEN", "the referenced shopping task is not awaiting clarification from this buyer")
	case errors.Is(err, ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "idempotency key was already used for a different request")
	case errors.Is(err, ErrIntentAlreadyParsed):
		writeError(w, http.StatusConflict, "INTENT_ALREADY_PARSED", "the shopping task intent is no longer pending")
	case errors.Is(err, ErrInvalidSimulationData):
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "request violates the shopping simulation contract")
	default:
		writeError(w, http.StatusInternalServerError, "SIMULATION_OPERATION_FAILED", "simulation operation failed")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		return
	}
}

func setSimulationCatalogSearchDuration(w http.ResponseWriter, duration time.Duration) {
	setSimulationStageDuration(w, simulationCatalogSearchMicrosHeader, duration)
}

func setSimulationStageDuration(w http.ResponseWriter, headerName string, duration time.Duration) {
	w.Header().Set(headerName, strconv.FormatInt(duration.Microseconds(), 10))
}

func parseSimulationCatalogSearchMillis(header http.Header) (float64, error) {
	return parseSimulationStageMillis(header, simulationCatalogSearchMicrosHeader)
}

func parseSimulationStageMillis(header http.Header, headerName string) (float64, error) {
	raw := strings.TrimSpace(header.Get(headerName))
	micros, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || micros < 0 {
		return 0, fmt.Errorf("invalid %s header", headerName)
	}
	return float64(micros) / 1000, nil
}

func contextWithParseTimeout(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 60*time.Second)
}
