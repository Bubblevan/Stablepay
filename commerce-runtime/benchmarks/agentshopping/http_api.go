package agentshopping

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type HTTPAPI struct {
	store  *MySQLStore
	parser IntentParser
	apiKey string
	mux    *http.ServeMux
}

func NewHTTPAPI(store *MySQLStore, parser IntentParser, apiKey string) (*HTTPAPI, error) {
	if store == nil || parser == nil || len(apiKey) < 32 {
		return nil, errors.New("simulation API requires MySQL store, intent parser, and an API key of at least 32 characters")
	}
	api := &HTTPAPI{store: store, parser: parser, apiKey: apiKey, mux: http.NewServeMux()}
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
	a.mux.Handle("GET /v1/buyers", a.auth(http.HandlerFunc(a.buyers)))
	a.mux.Handle("POST /v1/intents/parse", a.auth(http.HandlerFunc(a.parseIntent)))
	a.mux.Handle("POST /v1/quotes", a.auth(http.HandlerFunc(a.issueQuote)))
	a.mux.Handle("GET /v1/quotes/{quoteID}", a.auth(http.HandlerFunc(a.getQuote)))
	a.mux.Handle("POST /v1/quotes/{quoteID}/confirm", a.auth(http.HandlerFunc(a.confirmQuote)))
	a.mux.Handle("POST /v1/orders", a.auth(http.HandlerFunc(a.checkout)))
	a.mux.Handle("GET /v1/orders/{orderID}", a.auth(http.HandlerFunc(a.getOrder)))
	a.mux.Handle("POST /v1/admin/inventory-adjustments", a.auth(http.HandlerFunc(a.adjustInventory)))
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
	writeJSON(w, http.StatusOK, map[string]any{"tenant_id": SimulationTenantID, "counts": counts,
		"data_origin": syntheticOrigin, "payment_provider": "simulation-only", "real_settlement": false})
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
	BuyerID        string `json:"buyer_id"`
	Prompt         string `json:"prompt"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (a *HTTPAPI) parseIntent(w http.ResponseWriter, r *http.Request) {
	var request parseIntentRequest
	if !readJSON(w, r, &request) {
		return
	}
	buyer, err := a.store.GetBuyer(r.Context(), strings.TrimSpace(request.BuyerID))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	task, err := a.store.CreateTask(r.Context(), buyer.BuyerID, request.Prompt, request.IdempotencyKey)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if task.Status == "DISCOVERED" {
		var intent Intent
		if err := json.Unmarshal(task.IntentJSON, &intent); err != nil {
			writeError(w, http.StatusInternalServerError, "TASK_STATE_INVALID", "persisted shopping intent could not be read")
			return
		}
		candidates, err := a.store.FindCandidates(r.Context(), intent, 20)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "CATALOG_READ_FAILED", "could not search the simulated catalog")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task_id": task.TaskID, "intent": intent,
			"candidates": candidates, "provider": task.Provider, "model": task.Model, "idempotent_replay": true})
		return
	}
	taxonomy, err := a.store.ReadTaxonomy(r.Context())
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
	latency := time.Since(started)
	if err := a.store.SaveParsedIntent(r.Context(), task.TaskID, a.parser.Provider(), a.parser.Model(), intent, latency, parseErr, usage); err != nil {
		writeError(w, http.StatusServiceUnavailable, "INTENT_TRACE_WRITE_FAILED", "could not persist the simulation intent trace")
		return
	}
	if parseErr != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]string{"code": "INTENT_PARSE_FAILED", "message": "Hermes could not produce a valid shopping intent"},
			"task_id": task.TaskID, "provider": a.parser.Provider(), "model": a.parser.Model()})
		return
	}
	candidates, err := a.store.FindCandidates(r.Context(), intent, 20)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "CATALOG_SEARCH_FAILED", "could not search the simulated catalog")
		return
	}
	if len(candidates) == 0 {
		_ = a.store.SetTaskStatus(r.Context(), task.TaskID, "NO_MATCHING_CANDIDATES")
		writeJSON(w, http.StatusOK, map[string]any{"task_id": task.TaskID, "intent": intent,
			"candidates": []Candidate{}, "provider": a.parser.Provider(), "model": a.parser.Model(),
			"message": "No in-stock catalog item matches this region and budget; no quote or payment was created."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task_id": task.TaskID, "intent": intent, "candidates": candidates,
		"provider": a.parser.Provider(), "model": a.parser.Model(), "latency_ms": latency.Milliseconds(), "buyer_confirmation_required": true})
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
	confirmation, err := a.store.ConfirmBuyer(r.Context(), PersistentConfirmRequest{BuyerID: request.BuyerID,
		QuoteID: r.PathValue("quoteID"), Accepted: request.Accepted, AmountMinor: request.AmountMinor,
		IdempotencyKey: request.IdempotencyKey})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"confirmation": confirmation, "payment_created": false,
		"message": "Buyer confirmation was recorded. Submit a separate order request to run the simulation-only payment."})
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
	result, err := a.store.Checkout(r.Context(), SimulationTenantID, request.ConfirmationID, request.IdempotencyKey)
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
	case errors.Is(err, ErrStaleQuote):
		writeError(w, http.StatusConflict, "STALE_QUOTE", "price, stock, or quote validity changed; obtain a new quote and ask the buyer again")
	case errors.Is(err, ErrQuoteAmountMismatch):
		writeError(w, http.StatusConflict, "QUOTE_AMOUNT_MISMATCH", "confirmation amount must equal the immutable quote amount")
	case errors.Is(err, ErrConfirmationRequired):
		writeError(w, http.StatusConflict, "BUYER_CONFIRMATION_REQUIRED", "an accepted buyer confirmation is required before checkout")
	case errors.Is(err, ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT", "idempotency key was already used for a different request")
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

func contextWithParseTimeout(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 60*time.Second)
}
