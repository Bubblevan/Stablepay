package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testAPIKey = "test-simulation-api-key-32-characters-long"

func TestBridgeMCPStdioCallsOnlyAllowlistedSimulationRoutes(t *testing.T) {
	type observedRequest struct {
		method  string
		path    string
		apiKey  string
		body    map[string]any
		bodyErr error
	}
	var observed []observedRequest
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		row := observedRequest{method: r.Method, path: r.URL.Path, apiKey: r.Header.Get("X-Simulation-API-Key")}
		if r.Body != nil {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				row.bodyErr = err
			} else if len(data) != 0 {
				row.bodyErr = json.Unmarshal(data, &row.body)
			}
		}
		observed = append(observed, row)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"simulation_only":true,"real_settlement":false,"ok":true}`)
	}))
	defer api.Close()

	bridge, err := newBridge(api.URL, testAPIKey, nil)
	if err != nil {
		t.Fatalf("newBridge: %v", err)
	}
	requests := []any{
		map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"},
		map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"},
		map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"},
		toolCallRequest(3, "stablepay.shopping.search", map[string]any{"buyer_id": "sim-buyer-000001", "prompt": "找咖啡", "idempotency_key": "search-1"}),
		toolCallRequest(4, "stablepay.shopping.task_status", map[string]any{"task_id": "task-1"}),
		toolCallRequest(5, "stablepay.shopping.quote", map[string]any{"task_id": "task-1", "buyer_id": "sim-buyer-000001", "merchant_id": "sim-m01", "sku_id": "sku-1", "region": "CN-EAST", "max_price_minor": 5000, "currency": "USDC", "idempotency_key": "quote-1"}),
		toolCallRequest(6, "stablepay.shopping.quote_status", map[string]any{"quote_id": "quote-1"}),
		toolCallRequest(8, "stablepay.shopping.checkout", map[string]any{"confirmation_id": "confirm-1", "idempotency_key": "checkout-1"}),
		toolCallRequest(9, "stablepay.shopping.order_status", map[string]any{"order_id": "order-1"}),
		toolCallRequest(10, "stablepay.shopping.fulfill", map[string]any{"order_id": "order-1", "idempotency_key": "fulfill-1"}),
	}
	var input strings.Builder
	for _, request := range requests {
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		input.Write(encoded)
		input.WriteByte('\n')
	}
	var output bytes.Buffer
	if err := bridge.serve(context.Background(), strings.NewReader(input.String()), &output); err != nil {
		t.Fatalf("serve: %v", err)
	}

	responses := make([]jsonRPCResponse, 0, 10)
	scanner := bufio.NewScanner(&output)
	for scanner.Scan() {
		var response jsonRPCResponse
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			t.Fatalf("decode MCP response %q: %v", scanner.Text(), err)
		}
		responses = append(responses, response)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(responses) != 9 {
		t.Fatalf("responses=%d, want initialize + tools/list + 7 calls (notification is silent)", len(responses))
	}
	var listed struct {
		Tools []toolDefinition `json:"tools"`
	}
	if err := json.Unmarshal(mustJSON(t, responses[1].Result), &listed); err != nil {
		t.Fatalf("decode tools/list: %v", err)
	}
	if len(listed.Tools) != 7 {
		t.Fatalf("registered tools=%d, want 7", len(listed.Tools))
	}
	for _, response := range responses[2:] {
		if response.Error != nil {
			t.Fatalf("unexpected JSON-RPC error: %+v", response.Error)
		}
		var result struct {
			IsError bool `json:"isError"`
		}
		if err := json.Unmarshal(mustJSON(t, response.Result), &result); err != nil || result.IsError {
			t.Fatalf("tool call failed: result=%s err=%v", mustJSON(t, response.Result), err)
		}
	}

	wantRoutes := []struct{ method, path string }{
		{http.MethodPost, "/v1/intents/parse"},
		{http.MethodGet, "/v1/tasks/task-1"},
		{http.MethodPost, "/v1/quotes"},
		{http.MethodGet, "/v1/quotes/quote-1"},
		{http.MethodPost, "/v1/orders"},
		{http.MethodGet, "/v1/orders/order-1"},
		{http.MethodPost, "/v1/orders/order-1/fulfillment"},
	}
	if len(observed) != len(wantRoutes) {
		t.Fatalf("upstream calls=%d, want %d", len(observed), len(wantRoutes))
	}
	for i, want := range wantRoutes {
		got := observed[i]
		if got.method != want.method || got.path != want.path || got.apiKey != testAPIKey || got.bodyErr != nil {
			t.Fatalf("upstream request %d = %+v, want method=%s path=%s and API key", i, got, want.method, want.path)
		}
	}
	if observed[4].body["confirmation_id"] != "confirm-1" {
		t.Fatalf("checkout payload was not preserved: checkout=%v", observed[4].body)
	}
	if observed[6].body["idempotency_key"] != "fulfill-1" {
		t.Fatalf("fulfillment idempotency key was not preserved: fulfill=%v", observed[6].body)
	}
	if _, exists := observed[6].body["order_id"]; exists {
		t.Fatalf("path-only order_id leaked into fulfillment request body: fulfill=%v", observed[6].body)
	}
	if _, exists := shoppingToolRoutes()["stablepay.shopping.confirm_quote"]; exists {
		t.Fatal("MCP must not expose an agent-callable buyer confirmation route")
	}
}

func TestBridgeSupportsCurrentStatelessMCPProtocol(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"simulation_only":true,"real_settlement":false}`)
	}))
	defer api.Close()
	bridge, err := newBridge(api.URL, testAPIKey, nil)
	if err != nil {
		t.Fatal(err)
	}

	requests := []any{
		modernRequest(1, "server/discover", map[string]any{}),
		modernRequest(2, "tools/list", map[string]any{}),
		modernToolCallRequest(3, "stablepay.shopping.search", map[string]any{
			"buyer_id": "sim-buyer-000001", "prompt": "找咖啡", "idempotency_key": "search-1",
		}),
	}
	var input strings.Builder
	for _, request := range requests {
		input.Write(mustJSON(t, request))
		input.WriteByte('\n')
	}
	var output bytes.Buffer
	if err := bridge.serve(context.Background(), strings.NewReader(input.String()), &output); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var responses []jsonRPCResponse
	scanner := bufio.NewScanner(&output)
	for scanner.Scan() {
		var response jsonRPCResponse
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(responses) != 3 {
		t.Fatalf("responses=%d, want discover + tools/list + tool call", len(responses))
	}
	for i, response := range responses {
		if response.Error != nil {
			t.Fatalf("response %d: %+v", i, response.Error)
		}
		var result map[string]any
		if err := json.Unmarshal(mustJSON(t, response.Result), &result); err != nil {
			t.Fatal(err)
		}
		if result["resultType"] != "complete" {
			t.Fatalf("response %d omitted 2026 resultType: %v", i, result)
		}
		meta, ok := result["_meta"].(map[string]any)
		if !ok || meta["io.modelcontextprotocol/serverInfo"] == nil {
			t.Fatalf("response %d omitted modern server identity metadata: %v", i, result)
		}
	}
	var discover map[string]any
	if err := json.Unmarshal(mustJSON(t, responses[0].Result), &discover); err != nil {
		t.Fatal(err)
	}
	versions, ok := discover["supportedVersions"].([]any)
	if !ok || len(versions) != 1 || versions[0] != currentProtocolVersion {
		t.Fatalf("discovery versions=%v", discover["supportedVersions"])
	}
	if bridge.era != protocolEraModern {
		t.Fatalf("protocol era=%v, want modern", bridge.era)
	}

	missingMeta, ok := bridge.handleLine(context.Background(), mustJSON(t, map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "tools/list", "params": map[string]any{},
	}))
	if !ok || missingMeta.Error == nil || missingMeta.Error.Code != -32602 {
		t.Fatalf("modern connection should reject requests missing per-request metadata: %+v", missingMeta)
	}
}

func TestNewBridgeRejectsNonLoopbackOriginsAndWeakKeys(t *testing.T) {
	for _, test := range []struct {
		url string
		key string
	}{
		{"https://127.0.0.1:18090", testAPIKey},
		{"http://example.com:18090", testAPIKey},
		{"http://user:pass@127.0.0.1:18090", testAPIKey},
		{"http://127.0.0.1:18090/base", testAPIKey},
		{"http://127.0.0.1:99999", testAPIKey},
		{"http://127.0.0.1:18090", "short"},
	} {
		if _, err := newBridge(test.url, test.key, nil); err == nil {
			t.Errorf("newBridge(%q, ...) unexpectedly succeeded", test.url)
		}
	}
}

func TestBridgeMCPListsOnlySimulationToolsAndRejectsUnknownTool(t *testing.T) {
	bridge, err := newBridge("http://127.0.0.1:18090", testAPIKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	initializeLegacyBridge(t, bridge)
	response, ok := bridge.handleLine(context.Background(), mustJSON(t, toolCallRequest(1, "stablepay.shopping.admin_adjust_inventory", map[string]any{"sku_id": "sku-1"})))
	if !ok || response.Error != nil {
		t.Fatalf("unknown MCP tool should return a tool-level error, got %+v", response)
	}
	var result struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(mustJSON(t, response.Result), &result); err != nil || !result.IsError {
		t.Fatalf("unknown tool result=%s err=%v", mustJSON(t, response.Result), err)
	}
	for _, tool := range shoppingTools() {
		if strings.Contains(strings.ToLower(tool.Name+" "+tool.Description), "admin") || strings.Contains(strings.ToLower(tool.Name), "payment") {
			t.Fatalf("unexpected admin/payment tool exposed: %+v", tool)
		}
	}
}

func TestBridgeReportsUpstreamErrorsWithoutLeakingAPIKey(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"error":{"code":"UNAUTHORIZED","message":"invalid %s"}}`, testAPIKey)
	}))
	defer api.Close()
	bridge, err := newBridge(api.URL, testAPIKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	initializeLegacyBridge(t, bridge)
	response, _ := bridge.handleLine(context.Background(), mustJSON(t, toolCallRequest(1, "stablepay.shopping.task_status", map[string]any{"task_id": "task-1"})))
	encoded := string(mustJSON(t, response.Result))
	if !strings.Contains(encoded, `"isError":true`) || strings.Contains(encoded, testAPIKey) {
		t.Fatalf("upstream error result must be marked and never contain credential: %s", encoded)
	}
}

func TestBridgeRequiresJSONObjectArgumentsAndDoesNotCallUnknownPaths(t *testing.T) {
	var calls int
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++ }))
	defer api.Close()
	bridge, err := newBridge(api.URL, testAPIKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	initializeLegacyBridge(t, bridge)
	for _, line := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"stablepay.shopping.search","arguments":[]}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"stablepay.shopping.task_status","arguments":{"task_id":""}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"stablepay.shopping.task_status","arguments":{"task_id":"../admin"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"unknown","arguments":{}}}`,
	} {
		response, ok := bridge.handleLine(context.Background(), []byte(line))
		if !ok || response.Result == nil {
			t.Fatalf("invalid tool call did not return a tool error: %+v", response)
		}
		var result struct {
			IsError bool `json:"isError"`
		}
		if err := json.Unmarshal(mustJSON(t, response.Result), &result); err != nil || !result.IsError {
			t.Fatalf("invalid tool result=%s err=%v", mustJSON(t, response.Result), err)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid/unknown tool calls reached upstream %d times", calls)
	}
}

func toolCallRequest(id int, name string, arguments any) map[string]any {
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}}
}

func modernRequest(id int, method string, params map[string]any) map[string]any {
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    currentProtocolVersion,
		"io.modelcontextprotocol/clientInfo":         map[string]string{"name": "agentshopping-mcp-test", "version": "1.0.0"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	return map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
}

func modernToolCallRequest(id int, name string, arguments any) map[string]any {
	params := map[string]any{"name": name, "arguments": arguments}
	return modernRequest(id, "tools/call", params)
}

func initializeLegacyBridge(t *testing.T, bridge *bridge) {
	t.Helper()
	response, ok := bridge.handleLine(context.Background(), mustJSON(t, map[string]any{
		"jsonrpc": "2.0", "id": 99, "method": "initialize",
		"params": map[string]any{"protocolVersion": legacyProtocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "test", "version": "1.0.0"}},
	}))
	if !ok || response.Error != nil {
		t.Fatalf("legacy initialize failed: %+v", response.Error)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
