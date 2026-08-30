package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"query-service/kitex_gen/stablepay/common"
	query_service "query-service/kitex_gen/stablepay/query_service"
)

// startHTTPServer starts the HTTP adapter used by api-gateway and local debugging.
func startHTTPServer(addr string) {
	impl := &QueryServiceImpl{}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("GET /internal/balance", func(w http.ResponseWriter, r *http.Request) {
		resp, err := impl.GetBalanceSummary(context.Background(), &query_service.GetBalanceSummaryRequest{
			Base:     &common.BaseReq{},
			AgentDid: r.URL.Query().Get("agent_did"),
		})
		writeHTTPJSON(w, resp, err)
	})

	mux.HandleFunc("GET /internal/transactions", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		txType, _ := strconv.Atoi(q.Get("type"))
		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		if limit == 0 {
			limit = 10
		}
		resp, err := impl.ListTransactions(context.Background(), &query_service.ListTransactionsRequest{
			Base: &common.BaseReq{},
			Did:  q.Get("did"),
			Type: query_service.TransactionType(txType),
			Page: &common.PageRequest{Limit: int32(limit), Offset: int32(offset)},
		})
		writeHTTPJSON(w, resp, err)
	})

	mux.HandleFunc("GET /internal/revenue", func(w http.ResponseWriter, r *http.Request) {
		resp, err := impl.GetRevenueSummary(context.Background(), &query_service.GetRevenueSummaryRequest{
			Base:     &common.BaseReq{},
			SkillDid: r.URL.Query().Get("skill_did"),
		})
		writeHTTPJSON(w, resp, err)
	})

	mux.HandleFunc("GET /internal/sales", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		offset, _ := strconv.Atoi(q.Get("offset"))
		resp, err := listSalesRecords(q.Get("skill_did"), limit, offset)
		writeHTTPJSON(w, resp, err)
	})

	mux.HandleFunc("POST /internal/transactions/sync", func(w http.ResponseWriter, r *http.Request) {
		var req TransactionSyncRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeHTTPJSON(w, map[string]string{"error": "invalid json body"}, err)
			return
		}
		if err := syncTransactionRecord(r.Context(), &req); err != nil {
			writeHTTPJSON(w, map[string]string{"error": err.Error()}, err)
			return
		}
		writeHTTPJSON(w, map[string]any{
			"ok":     true,
			"tx_id":  req.TxID,
			"type":   req.Type,
			"status": "synced",
		}, nil)
	})

	log.Printf("Query Service HTTP adapter starting on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Query HTTP adapter error: %v", err)
	}
}

func writeHTTPJSON(w http.ResponseWriter, v interface{}, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}
