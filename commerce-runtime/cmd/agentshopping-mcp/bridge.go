package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	legacyProtocolVersion  = "2025-11-25"
	currentProtocolVersion = "2026-07-28"
	maxStdioMessageBytes   = 1 << 20
	maxAPIResponseBytes    = 2 << 20
	bridgeServerName       = "stablepay-agent-shopping-simulation"
)

type bridge struct {
	baseURL string
	apiKey  string
	client  *http.Client
	era     protocolEra
}

type protocolEra uint8

const (
	protocolEraUnknown protocolEra = iota
	protocolEraLegacy
	protocolEraModern
)

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type toolRoute struct {
	method   string
	path     func(map[string]json.RawMessage) (string, error)
	bodyKeys []string
}

func newBridge(baseURL, apiKey string, client *http.Client) (*bridge, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || parsed == nil || parsed.Scheme != "http" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" && parsed.Path != "/" ||
		!validatePort(parsed.Port()) || !isLoopbackHost(parsed.Hostname()) {
		return nil, errors.New("simulation API URL must be an HTTP loopback origin")
	}
	if len(apiKey) < 32 {
		return nil, errors.New("simulation API key must contain at least 32 characters")
	}
	if client == nil {
		client = &http.Client{
			Timeout: 65 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &bridge{baseURL: strings.TrimRight(parsed.String(), "/"), apiKey: apiKey, client: client}, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (b *bridge) serve(ctx context.Context, input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), maxStdioMessageBytes)
	writer := bufio.NewWriter(output)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		response, respond := b.handleLine(ctx, line)
		if !respond {
			continue
		}
		if err := json.NewEncoder(writer).Encode(response); err != nil {
			return errors.New("could not write MCP response")
		}
		if err := writer.Flush(); err != nil {
			return errors.New("could not flush MCP response")
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read MCP stdio: %w", err)
	}
	return nil
}

func (b *bridge) handleLine(ctx context.Context, line []byte) (jsonRPCResponse, bool) {
	var request jsonRPCRequest
	if err := json.Unmarshal(line, &request); err != nil {
		return jsonRPCResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &jsonRPCError{Code: -32700, Message: "parse error"}}, true
	}
	if request.JSONRPC != "2.0" || request.Method == "" {
		return jsonRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: &jsonRPCError{Code: -32600, Message: "invalid request"}}, true
	}
	if strings.HasPrefix(request.Method, "notifications/") || len(request.ID) == 0 {
		return jsonRPCResponse{}, false
	}
	modern, version, metadataErr := requestProtocolMetadata(request.Params)
	if metadataErr != nil {
		return jsonRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: &jsonRPCError{Code: -32602, Message: "invalid MCP request metadata"}}, true
	}
	if b.era == protocolEraUnknown {
		if request.Method == "initialize" && !modern {
			b.era = protocolEraLegacy
		} else if modern {
			b.era = protocolEraModern
		} else {
			return jsonRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: &jsonRPCError{Code: -32600, Message: "expected initialize or per-request MCP metadata"}}, true
		}
	} else if (b.era == protocolEraLegacy && modern) || (b.era == protocolEraModern && !modern) {
		return jsonRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: &jsonRPCError{Code: -32602, Message: "MCP protocol era changed within stdio connection"}}, true
	}
	if modern {
		if version != currentProtocolVersion {
			return jsonRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: &jsonRPCError{
				Code: -32022, Message: "unsupported protocol version",
				Data: map[string]any{"supported": []string{currentProtocolVersion}, "requested": version},
			}}, len(request.ID) != 0
		}
		if err := validateModernRequestMetadata(request.Params); err != nil {
			return jsonRPCResponse{JSONRPC: "2.0", ID: request.ID, Error: &jsonRPCError{Code: -32602, Message: "invalid MCP request metadata"}}, len(request.ID) != 0
		}
	}
	response := jsonRPCResponse{JSONRPC: "2.0", ID: request.ID}
	switch request.Method {
	case "initialize":
		if modern {
			response.Error = &jsonRPCError{Code: -32601, Message: "method not found"}
			break
		}
		response.Result = map[string]any{
			"protocolVersion": legacyProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": bridgeServerName, "version": "0.1.0"},
			"instructions":    "Simulation-only shopping tools. Payment and fulfillment are deterministic local mocks; no real settlement occurs. Obtain explicit buyer approval before recording confirmation.",
		}
	case "server/discover":
		if !modern {
			response.Error = &jsonRPCError{Code: -32601, Message: "method not found"}
			break
		}
		response.Result = map[string]any{
			"resultType":        "complete",
			"supportedVersions": []string{currentProtocolVersion},
			"capabilities":      map[string]any{"tools": map[string]any{}},
			"_meta":             serverInfoMetadata(),
			"instructions":      "Simulation-only shopping tools. Payment and fulfillment are deterministic local mocks; no real settlement occurs. Obtain explicit buyer approval before recording confirmation.",
			"ttlMs":             3_600_000,
			"cacheScope":        "public",
		}
	case "ping":
		if modern {
			response.Error = &jsonRPCError{Code: -32601, Message: "method not found"}
		} else {
			response.Result = map[string]any{}
		}
	case "tools/list":
		response.Result = map[string]any{"tools": shoppingTools()}
		if modern {
			response.Result.(map[string]any)["ttlMs"] = 3_600_000
			response.Result.(map[string]any)["cacheScope"] = "public"
		}
	case "tools/call":
		var params toolCallParams
		if err := json.Unmarshal(request.Params, &params); err != nil || strings.TrimSpace(params.Name) == "" {
			response.Error = &jsonRPCError{Code: -32602, Message: "invalid tools/call params"}
			break
		}
		response.Result = b.callTool(ctx, params)
	default:
		response.Error = &jsonRPCError{Code: -32601, Message: "method not found"}
	}
	if modern && response.Error == nil && request.Method != "server/discover" {
		decorateModernResult(response.Result)
	}
	return response, true
}

func requestProtocolMetadata(rawParams json.RawMessage) (modern bool, version string, err error) {
	if len(rawParams) == 0 || string(rawParams) == "null" {
		return false, "", nil
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(rawParams, &params); err != nil || params == nil {
		return false, "", nil
	}
	rawMeta, ok := params["_meta"]
	if !ok {
		return false, "", nil
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(rawMeta, &meta); err != nil || meta == nil {
		return true, "", errors.New("metadata must be an object")
	}
	rawVersion, ok := meta["io.modelcontextprotocol/protocolVersion"]
	if !ok {
		return false, "", nil
	}
	if err := json.Unmarshal(rawVersion, &version); err != nil || version == "" {
		return true, "", errors.New("protocol version must be a string")
	}
	return true, version, nil
}

func validateModernRequestMetadata(rawParams json.RawMessage) error {
	var params map[string]json.RawMessage
	if err := json.Unmarshal(rawParams, &params); err != nil || params == nil {
		return errors.New("params must be an object")
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(params["_meta"], &meta); err != nil || meta == nil {
		return errors.New("metadata must be an object")
	}
	var capabilities map[string]json.RawMessage
	if err := json.Unmarshal(meta["io.modelcontextprotocol/clientCapabilities"], &capabilities); err != nil || capabilities == nil {
		return errors.New("client capabilities must be an object")
	}
	if rawClientInfo, ok := meta["io.modelcontextprotocol/clientInfo"]; ok {
		var clientInfo struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		}
		if err := json.Unmarshal(rawClientInfo, &clientInfo); err != nil || strings.TrimSpace(clientInfo.Name) == "" || strings.TrimSpace(clientInfo.Version) == "" {
			return errors.New("client identity is invalid")
		}
	}
	return nil
}

func serverInfoMetadata() map[string]any {
	return map[string]any{
		"io.modelcontextprotocol/serverInfo": map[string]string{"name": bridgeServerName, "version": "0.1.0"},
	}
}

func decorateModernResult(result any) {
	object, ok := result.(map[string]any)
	if !ok {
		return
	}
	object["resultType"] = "complete"
	object["_meta"] = serverInfoMetadata()
}

func shoppingTools() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "stablepay.shopping.search",
			Description: "Parse a buyer's request and discover current matching products, region availability, and prices in the isolated shopping simulation. Creates a shopping task but no quote or payment.",
			InputSchema: objectSchema([]string{"buyer_id", "prompt", "idempotency_key"}, map[string]any{
				"buyer_id":          stringProperty("Synthetic buyer ID"),
				"prompt":            stringProperty("Natural-language shopping request"),
				"idempotency_key":   stringProperty("Stable key for replaying this request"),
				"clarifies_task_id": stringProperty("Optional task ID being clarified"),
			}),
		},
		{
			Name:        "stablepay.shopping.task_status",
			Description: "Read a persisted shopping task and its current status from the simulation.",
			InputSchema: objectSchema([]string{"task_id"}, map[string]any{"task_id": stringProperty("Shopping task ID")}),
		},
		{
			Name:        "stablepay.shopping.quote",
			Description: "Request an immutable quote for one discovered candidate. The simulator rechecks buyer budget, live regional stock, price, and allowed substitutions; creating a quote is not buyer approval and does not create payment.",
			InputSchema: objectSchema([]string{"task_id", "buyer_id", "merchant_id", "sku_id", "region", "max_price_minor", "currency", "idempotency_key"}, map[string]any{
				"task_id":         stringProperty("Discovered task ID"),
				"buyer_id":        stringProperty("Synthetic buyer ID"),
				"merchant_id":     stringProperty("Merchant ID from a candidate"),
				"sku_id":          stringProperty("SKU ID from a candidate"),
				"region":          stringProperty("Delivery region from the parsed intent"),
				"max_price_minor": map[string]any{"type": "integer", "minimum": 1},
				"currency":        map[string]any{"type": "string", "enum": []string{"USDC"}},
				"idempotency_key": stringProperty("Stable key for replaying this quote request"),
			}),
		},
		{
			Name:        "stablepay.shopping.quote_status",
			Description: "Read the immutable quote version and exact amount; this does not record buyer approval.",
			InputSchema: objectSchema([]string{"quote_id"}, map[string]any{"quote_id": stringProperty("Quote ID")}),
		},
		{
			Name:        "stablepay.shopping.checkout",
			Description: "Submit a previously accepted confirmation to the isolated simulation. This creates only a deterministic mock payment; real_settlement=false and no chain transaction is submitted. Never use this tool for a real purchase.",
			InputSchema: objectSchema([]string{"confirmation_id", "idempotency_key"}, map[string]any{
				"confirmation_id": stringProperty("Accepted buyer confirmation ID"),
				"idempotency_key": stringProperty("Stable key for replaying this checkout"),
			}),
		},
		{
			Name:        "stablepay.shopping.order_status",
			Description: "Read a persisted simulation order and its mock payment status.",
			InputSchema: objectSchema([]string{"order_id"}, map[string]any{"order_id": stringProperty("Order ID")}),
		},
		{
			Name:        "stablepay.shopping.fulfill",
			Description: "Run idempotent benchmark-only fulfillment for a settled simulation order. Failure compensation/refunds are mock records; no external delivery or real refund occurs.",
			InputSchema: objectSchema([]string{"order_id", "idempotency_key"}, map[string]any{
				"order_id":        stringProperty("Settled simulation order ID"),
				"idempotency_key": stringProperty("Stable key for replaying fulfillment"),
			}),
		},
	}
}

func objectSchema(required []string, properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "required": required, "properties": properties, "additionalProperties": false}
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "description": description}
}

func (b *bridge) callTool(ctx context.Context, params toolCallParams) map[string]any {
	var arguments map[string]json.RawMessage
	if len(params.Arguments) == 0 || json.Unmarshal(params.Arguments, &arguments) != nil || arguments == nil {
		return toolError("tool arguments must be a JSON object")
	}
	route, ok := shoppingToolRoutes()[params.Name]
	if !ok {
		return toolError("unknown shopping tool")
	}
	path, err := route.path(arguments)
	if err != nil {
		return toolError(err.Error())
	}
	body := []byte("{}")
	if route.method == http.MethodPost {
		bodyArguments := make(map[string]json.RawMessage, len(route.bodyKeys))
		for _, key := range route.bodyKeys {
			if value, exists := arguments[key]; exists {
				bodyArguments[key] = value
			}
		}
		body, err = json.Marshal(bodyArguments)
		if err != nil {
			return toolError("tool arguments are not valid JSON")
		}
	}
	request, err := http.NewRequestWithContext(ctx, route.method, b.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return toolError("could not create simulation API request")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Simulation-API-Key", b.apiKey)
	if route.method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := b.client.Do(request)
	if err != nil {
		return toolError("shopping simulation API is unavailable")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxAPIResponseBytes+1))
	if err != nil || len(data) > maxAPIResponseBytes {
		return toolError("shopping simulation API response is invalid or too large")
	}
	var payload any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return toolError("shopping simulation API returned invalid JSON")
	}
	payload = redactCredential(payload, b.apiKey)
	structured := map[string]any{"http_status": response.StatusCode, "response": payload}
	encoded, err := json.Marshal(structured)
	if err != nil {
		return toolError("could not encode simulation API response")
	}
	return map[string]any{
		"isError":           response.StatusCode < 200 || response.StatusCode >= 300,
		"content":           []map[string]string{{"type": "text", "text": string(encoded)}},
		"structuredContent": structured,
	}
}

func toolError(message string) map[string]any {
	return map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": message}}}
}

func shoppingToolRoutes() map[string]toolRoute {
	return map[string]toolRoute{
		"stablepay.shopping.search": {
			method:   http.MethodPost,
			path:     func(map[string]json.RawMessage) (string, error) { return "/v1/intents/parse", nil },
			bodyKeys: []string{"buyer_id", "prompt", "idempotency_key"},
		},
		"stablepay.shopping.task_status": {
			method: http.MethodGet,
			path: func(arguments map[string]json.RawMessage) (string, error) {
				return pathForArgument(arguments, "task_id", "/v1/tasks/")
			},
		},
		"stablepay.shopping.quote": {
			method:   http.MethodPost,
			path:     func(map[string]json.RawMessage) (string, error) { return "/v1/quotes", nil },
			bodyKeys: []string{"task_id", "buyer_id", "merchant_id", "sku_id", "region", "max_price_minor", "currency", "idempotency_key"},
		},
		"stablepay.shopping.quote_status": {
			method: http.MethodGet,
			path: func(arguments map[string]json.RawMessage) (string, error) {
				return pathForArgument(arguments, "quote_id", "/v1/quotes/")
			},
		},
		"stablepay.shopping.checkout": {
			method:   http.MethodPost,
			path:     func(map[string]json.RawMessage) (string, error) { return "/v1/orders", nil },
			bodyKeys: []string{"confirmation_id", "idempotency_key"},
		},
		"stablepay.shopping.order_status": {
			method: http.MethodGet,
			path: func(arguments map[string]json.RawMessage) (string, error) {
				return pathForArgument(arguments, "order_id", "/v1/orders/")
			},
		},
		"stablepay.shopping.fulfill": {
			method:   http.MethodPost,
			bodyKeys: []string{"idempotency_key"},
			path: func(arguments map[string]json.RawMessage) (string, error) {
				path, err := pathForArgument(arguments, "order_id", "/v1/orders/")
				return path + "/fulfillment", err
			},
		},
	}
}

func pathForArgument(arguments map[string]json.RawMessage, key, prefix string) (string, error) {
	var value string
	if err := json.Unmarshal(arguments[key], &value); err != nil || !validResourceID(value) {
		return "", fmt.Errorf("%s is required", key)
	}
	return prefix + url.PathEscape(strings.TrimSpace(value)), nil
}

func validResourceID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 200 || value[0] == '.' {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("_.:-", character) {
			continue
		}
		return false
	}
	return true
}

func redactCredential(value any, credential string) any {
	if credential == "" {
		return value
	}
	switch typed := value.(type) {
	case string:
		return strings.ReplaceAll(typed, credential, "[REDACTED]")
	case []any:
		for index := range typed {
			typed[index] = redactCredential(typed[index], credential)
		}
		return typed
	case map[string]any:
		for key, nested := range typed {
			typed[key] = redactCredential(nested, credential)
		}
		return typed
	default:
		return value
	}
}

func validatePort(raw string) bool {
	if raw == "" {
		return true
	}
	port, err := strconv.Atoi(raw)
	return err == nil && port > 0 && port <= 65535
}
