package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/repository"
)

func TestCatalogSearchHTTPAndMCPShareReadOnlyRuntimeSearch(t *testing.T) {
	ctx := context.Background()
	store := repository.NewInMemoryStore()
	now := time.Now().UTC().Truncate(time.Millisecond)
	service := application.NewService(store, application.WithClock(func() time.Time { return now }))
	cheap := testCapability(now)
	cheap.ProductID = "sku-cheap"
	if err := service.RegisterCapabilityVersion(ctx, cheap); err != nil {
		t.Fatal(err)
	}
	expensive := testCapability(now)
	expensive.MerchantDID = "did:merchant:expensive"
	expensive.ProductID = "sku-expensive"
	price := int64(900)
	expensive.PriceHintMinor = &price
	if err := service.RegisterCapabilityVersion(ctx, expensive); err != nil {
		t.Fatal(err)
	}
	handler := NewServer(service, store, nil, AuthConfig{Token: "catalog-search-token"}, nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	unauthorized := doJSON(t, server.URL+"/v1/catalog/search", http.MethodPost, "", "", []byte(`{"query":{}}`))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized search status=%d body=%s", unauthorized.Code, unauthorized.Body.String())
	}
	method := doJSON(t, server.URL+"/v1/catalog/search", http.MethodGet, "catalog-search-token", "", nil)
	if method.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET search status=%d body=%s", method.Code, method.Body.String())
	}

	query := catalog.DiscoveryQuery{TaskType: "api-test", InputContentType: "application/json", ExpectedOutputContentType: "application/json",
		SupportedProtocolVersions: []string{"x402-v2"}, Currency: "USDC", BudgetLimitMinor: 500, Deadline: now.Add(10 * time.Minute)}
	queryJSON, err := json.Marshal(query)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"query":` + string(queryJSON) + `}`)
	response := doJSON(t, server.URL+"/v1/catalog/search", http.MethodPost, "catalog-search-token", "", body)
	if response.Code != http.StatusOK {
		t.Fatalf("HTTP search status=%d body=%s", response.Code, response.Body.String())
	}
	var httpResult application.CatalogSearchResult
	if err := json.Unmarshal(response.Body.Bytes(), &httpResult); err != nil {
		t.Fatal(err)
	}
	if httpResult.TotalCandidates != 1 || len(httpResult.Candidates) != 1 {
		t.Fatalf("HTTP search candidates=%+v total=%d", httpResult.Candidates, httpResult.TotalCandidates)
	}
	candidate := httpResult.Candidates[0]
	if candidate.ProductID != "sku-cheap" || candidate.MerchantDID != "did:merchant:test" || candidate.CatalogVersion != "v1" || candidate.CatalogSnapshotHash == "" {
		t.Fatalf("HTTP candidate lost stable selection facts: %+v", candidate)
	}
	if httpResult.InventoryAuthoritative || httpResult.RegionAuthoritative {
		t.Fatalf("catalog snapshot overstated authority: %+v", httpResult)
	}

	mcpBody := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"stablepay.search_catalog","arguments":{"query":` + string(queryJSON) + `}}}`
	mcpResponse := doJSON(t, server.URL+"/mcp", http.MethodPost, "catalog-search-token", "", []byte(mcpBody))
	if mcpResponse.Code != http.StatusOK {
		t.Fatalf("MCP search status=%d body=%s", mcpResponse.Code, mcpResponse.Body.String())
	}
	var mcpResult struct {
		Result struct {
			StructuredContent application.CatalogSearchResult `json:"structuredContent"`
			IsError           bool                            `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(mcpResponse.Body.Bytes(), &mcpResult); err != nil {
		t.Fatal(err)
	}
	if mcpResult.Result.IsError || len(mcpResult.Result.StructuredContent.Candidates) != 1 ||
		mcpResult.Result.StructuredContent.Candidates[0].ProductID != candidate.ProductID ||
		mcpResult.Result.StructuredContent.QueryHash != httpResult.QueryHash {
		t.Fatalf("MCP did not share HTTP discovery behavior: %+v", mcpResult)
	}
	if _, err := store.FindByRequestID(ctx, "search-only"); err != repository.ErrNotFound {
		t.Fatalf("read-only search unexpectedly created a purchase record: err=%v", err)
	}
}

func TestCatalogSearchRejectsInvalidLimitAndAdvertisesMCPTool(t *testing.T) {
	store := repository.NewInMemoryStore()
	service := application.NewService(store)
	handler := NewServer(service, store, nil, AuthConfig{Token: "catalog-search-token"}, nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	badLimit := doJSON(t, server.URL+"/v1/catalog/search", http.MethodPost, "catalog-search-token", "", []byte(`{"limit":51,"query":{}}`))
	if badLimit.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit status=%d body=%s", badLimit.Code, badLimit.Body.String())
	}
	list := doJSON(t, server.URL+"/mcp", http.MethodPost, "catalog-search-token", "", []byte(`{"jsonrpc":"2.0","id":8,"method":"tools/list"}`))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"stablepay.search_catalog"`) {
		t.Fatalf("catalog search tool missing from MCP list: status=%d body=%s", list.Code, list.Body.String())
	}
}
