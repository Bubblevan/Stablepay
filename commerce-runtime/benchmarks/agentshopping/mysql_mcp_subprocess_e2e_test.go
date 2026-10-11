package agentshopping

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

// TestMySQLMCPSubprocessPurchaseLifecycle crosses the compiled MCP process,
// its allowlisted HTTP client, the authenticated simulation API, and a unique
// MySQL tenant. The confirmation call is made by a test-only synthetic buyer
// actor with a separate credential; it is not represented as human approval.
func TestMySQLMCPSubprocessPurchaseLifecycle(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_INTEGRATION") != "1" || os.Getenv("SHOP_SIM_MCP_MYSQL_INTEGRATION") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_INTEGRATION=1 and SHOP_SIM_MCP_MYSQL_INTEGRATION=1 for the isolated MCP/MySQL E2E")
	}

	binary := strings.TrimSpace(os.Getenv("SHOP_SIM_MCP_BINARY"))
	if binary == "" {
		t.Fatal("SHOP_SIM_MCP_BINARY must point to a compiled local agentshopping-mcp executable")
	}
	moduleRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	localRoot := filepath.Join(moduleRoot, ".local-run", "agent-shopping")
	binary, err = filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	relativeBinary, err := filepath.Rel(localRoot, binary)
	if err != nil || relativeBinary == ".." || strings.HasPrefix(relativeBinary, ".."+string(filepath.Separator)) {
		t.Fatalf("MCP executable must be inside ignored local-run storage: %s", binary)
	}
	if info, statErr := os.Stat(binary); statErr != nil || !info.Mode().IsRegular() {
		t.Fatalf("MCP executable is unavailable: %s", binary)
	}

	password := os.Getenv("SHOP_SIM_DB_PASSWORD")
	if strings.TrimSpace(password) == "" {
		t.Fatal("SHOP_SIM_DB_PASSWORD is required for isolated MCP/MySQL integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated shopping MySQL: %v", err)
		}
	})
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("isolated shopping MySQL ping failed: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate isolated shopping schema: %v", err)
	}

	suffix := mysqlIntegrationSuffix(t)
	tenantID := "shopping-mcp-it-" + suffix
	store.tenant = tenantID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if err := cleanupMySQLIntegrationTenant(cleanupCtx, store, tenantID); err != nil {
			t.Errorf("clean isolated MCP integration tenant %q: %v", tenantID, err)
			return
		}
		counts, err := store.Counts(cleanupCtx)
		if err != nil {
			t.Errorf("verify MCP tenant cleanup: %v", err)
			return
		}
		for table, count := range counts {
			if count != 0 {
				t.Errorf("MCP integration tenant retained %d rows in %s", count, table)
			}
		}
	})

	api, err := NewHTTPAPI(store, mysqlIntegrationParser{}, mysqlIntegrationAPIKey, mysqlIntegrationBuyerApprovalKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client := &mysqlIntegrationAPI{client: server.Client(), server: server, key: mysqlIntegrationAPIKey}
	if ready := client.do(t, http.MethodGet, "/readyz", nil); ready.status != http.StatusOK {
		t.Fatalf("simulation API is not ready: status=%d", ready.status)
	}
	fixture := createMySQLIntegrationFixture(t, ctx, store, client, "mcp-"+suffix)

	mcp, err := startMySQLIntegrationMCP(ctx, binary, server.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mcp.close(t) })

	var discovery struct {
		SupportedVersions []string `json:"supportedVersions"`
	}
	if err := mcp.request("server/discover", map[string]any{}, &discovery); err != nil {
		t.Fatal(err)
	}
	if len(discovery.SupportedVersions) != 1 || discovery.SupportedVersions[0] != "2026-07-28" {
		t.Fatalf("unexpected MCP protocol discovery: %+v", discovery)
	}
	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := mcp.request("tools/list", map[string]any{}, &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 7 {
		t.Fatalf("MCP exposed %d tools, want 7", len(listed.Tools))
	}
	for _, tool := range listed.Tools {
		if tool.Name == "stablepay.shopping.confirm_quote" {
			t.Fatal("MCP must not expose buyer confirmation to the agent process")
		}
	}

	var search struct {
		TaskID     string      `json:"task_id"`
		TaskStatus string      `json:"task_status"`
		Intent     Intent      `json:"intent"`
		Candidates []Candidate `json:"candidates"`
	}
	if err := mcp.callTool("stablepay.shopping.search", map[string]any{
		"buyer_id": fixture.buyerID, "prompt": "Buy one floral whole-bean coffee within budget",
		"idempotency_key": "mcp-search-" + suffix,
	}, &search); err != nil {
		t.Fatal(err)
	}
	if search.TaskID == "" || search.TaskStatus != TaskStatusDiscovered || len(search.Candidates) != 1 ||
		search.Candidates[0].Product.SKUID != fixture.skuID {
		t.Fatalf("MCP search did not discover the seeded product: task=%q status=%q candidates=%+v structured=%s", search.TaskID, search.TaskStatus, search.Candidates, mcp.lastStructured)
	}

	var quoteResponse struct {
		Quote                     Quote `json:"quote"`
		BuyerConfirmationRequired bool  `json:"buyer_confirmation_required"`
	}
	quoteIdempotency := "mcp-quote-" + suffix
	if err := mcp.callTool("stablepay.shopping.quote", map[string]any{
		"task_id": search.TaskID, "buyer_id": fixture.buyerID,
		"merchant_id": fixture.merchantID, "sku_id": fixture.skuID,
		"region": search.Intent.Region, "max_price_minor": search.Intent.MaxPriceMinor,
		"currency": "USDC", "idempotency_key": quoteIdempotency,
	}, &quoteResponse); err != nil {
		t.Fatal(err)
	}
	quote := quoteResponse.Quote
	if quote.ID == "" || quote.AmountMinor <= 0 || !quoteResponse.BuyerConfirmationRequired {
		t.Fatalf("MCP quote omitted immutable price or fresh-consent boundary: %+v", quoteResponse)
	}

	// This test-only actor is explicitly synthetic. Its independent credential
	// is not passed to the MCP child and does not represent a human's consent.
	confirmationHTTP, err := client.doRawWithHeaders(http.MethodPost,
		"/v1/quotes/"+quote.ID+"/confirm", map[string]any{
			"buyer_id": fixture.buyerID, "accepted": true, "amount_minor": quote.AmountMinor,
			"idempotency_key": "synthetic-test-approval-" + suffix,
		}, map[string]string{"X-Simulation-Buyer-Approval-Key": mysqlIntegrationBuyerApprovalKey})
	if err != nil {
		t.Fatal(err)
	}
	var confirmationResponse struct {
		Confirmation   Confirmation `json:"confirmation"`
		PaymentCreated bool         `json:"payment_created"`
	}
	if confirmationHTTP.status != http.StatusCreated || json.Unmarshal(confirmationHTTP.body, &confirmationResponse) != nil ||
		confirmationResponse.Confirmation.ID == "" || !confirmationResponse.Confirmation.Accepted || confirmationResponse.PaymentCreated {
		t.Fatalf("synthetic buyer approval failed or created payment early: status=%d body=%s", confirmationHTTP.status, confirmationHTTP.body)
	}

	checkoutKey := settledCheckoutKey("mcp-checkout-" + suffix)
	checkoutArguments := map[string]any{"confirmation_id": confirmationResponse.Confirmation.ID, "idempotency_key": checkoutKey}
	var checkout mysqlIntegrationCheckoutResponse
	if err := mcp.callTool("stablepay.shopping.checkout", checkoutArguments, &checkout); err != nil {
		t.Fatal(err)
	}
	if checkout.Order.ID == "" || checkout.Order.TaskID != search.TaskID || checkout.Order.Status != OrderPaid ||
		checkout.Payment.ID == "" || checkout.Payment.Status != PaymentSettled || !checkout.Payment.SimulationOnly || !checkout.SimulationOnly ||
		checkout.RealSettlement || checkout.ChainTransaction != nil {
		t.Fatalf("checkout escaped deterministic mock-payment boundary or returned invalid state: %+v", checkout)
	}
	orderID, paymentID := checkout.Order.ID, checkout.Payment.ID

	var paidStatus struct {
		Order          Order       `json:"order"`
		Payment        MockPayment `json:"payment"`
		RealSettlement bool        `json:"real_settlement"`
	}
	if err := mcp.callTool("stablepay.shopping.order_status", map[string]any{"order_id": orderID}, &paidStatus); err != nil {
		t.Fatal(err)
	}
	if paidStatus.Order.Status != OrderPaid || paidStatus.Payment.ID != paymentID || paidStatus.RealSettlement {
		t.Fatalf("MCP order-status read did not preserve paid mock state: %+v", paidStatus)
	}

	store.mockFulfillmentFaults = map[string]MockFulfillmentFaultPlan{
		search.TaskID: {MaxAttempts: 1},
	}
	fulfillmentKey := "mcp-fulfill-" + suffix
	var fulfillment FulfillmentResult
	if err := mcp.callTool("stablepay.shopping.fulfill", map[string]any{
		"order_id": orderID, "idempotency_key": fulfillmentKey,
	}, &fulfillment); err != nil {
		t.Fatal(err)
	}
	if fulfillment.Order.Status != OrderFulfilled || fulfillment.Payment.ID != paymentID ||
		fulfillment.Payment.Status != PaymentSettled || fulfillment.Fulfillment.Status != FulfillmentDelivered ||
		!fulfillment.Fulfillment.SimulationOnly || fulfillment.Refund != nil {
		t.Fatalf("MCP fulfillment did not reach simulation-only delivered state: %+v", fulfillment)
	}
	var checkoutReplay mysqlIntegrationCheckoutResponse
	if err := mcp.callTool("stablepay.shopping.checkout", checkoutArguments, &checkoutReplay); err != nil {
		t.Fatal(err)
	}
	if checkoutReplay.Order.ID != orderID || checkoutReplay.Payment.ID != paymentID {
		t.Fatalf("idempotent MCP checkout replay changed economic identities: first=(%s,%s) replay=(%s,%s)",
			orderID, paymentID, checkoutReplay.Order.ID, checkoutReplay.Payment.ID)
	}
	var fulfillmentReplay FulfillmentResult
	if err := mcp.callTool("stablepay.shopping.fulfill", map[string]any{
		"order_id": orderID, "idempotency_key": fulfillmentKey,
	}, &fulfillmentReplay); err != nil {
		t.Fatal(err)
	}
	if fulfillmentReplay.Fulfillment.ID != fulfillment.Fulfillment.ID || fulfillmentReplay.Order.Status != OrderFulfilled {
		t.Fatalf("idempotent fulfillment replay changed terminal state: first=%+v replay=%+v", fulfillment.Fulfillment, fulfillmentReplay.Fulfillment)
	}

	var persistedOrder Order
	var persistedPayment MockPayment
	if persistedOrder, persistedPayment, err = store.GetOrder(ctx, orderID); err != nil {
		t.Fatal(err)
	}
	if persistedOrder.Status != OrderFulfilled || persistedPayment.Status != PaymentSettled || !persistedPayment.SimulationOnly {
		t.Fatalf("persisted order/payment state mismatch: order=%+v payment=%+v", persistedOrder, persistedPayment)
	}
	persistedFulfillment, err := store.GetFulfillment(ctx, orderID)
	if err != nil || persistedFulfillment.Fulfillment.Status != FulfillmentDelivered {
		t.Fatalf("persisted fulfillment mismatch: result=%+v err=%v", persistedFulfillment, err)
	}

	counts := map[string]int64{}
	count := func(name string, query *gorm.DB) {
		t.Helper()
		var value int64
		if err := query.Count(&value).Error; err != nil {
			t.Fatalf("count %s rows: %v", name, err)
		}
		counts[name] = value
	}
	count("orders", store.db.WithContext(ctx).Model(&OrderRecord{}).
		Where("tenant_id = ? AND task_id = ?", tenantID, search.TaskID))
	count("payments", store.db.WithContext(ctx).Model(&MockPaymentRecord{}).
		Joins("JOIN sim_orders ON sim_orders.order_id = simulated_payments.order_id AND sim_orders.tenant_id = simulated_payments.tenant_id").
		Where("simulated_payments.tenant_id = ? AND sim_orders.task_id = ?", tenantID, search.TaskID))
	count("confirmations", store.db.WithContext(ctx).Model(&BuyerConfirmationRecord{}).
		Where("tenant_id = ? AND quote_id = ?", tenantID, quote.ID))
	count("reservations", store.db.WithContext(ctx).Model(&InventoryReservationRecord{}).
		Joins("JOIN sim_orders ON sim_orders.order_id = sim_inventory_reservations.order_id AND sim_orders.tenant_id = sim_inventory_reservations.tenant_id").
		Where("sim_inventory_reservations.tenant_id = ? AND sim_orders.task_id = ?", tenantID, search.TaskID))
	count("fulfillments", store.db.WithContext(ctx).Model(&MockFulfillmentRecord{}).
		Where("tenant_id = ? AND order_id = ?", tenantID, orderID))
	count("fulfillment_attempts", store.db.WithContext(ctx).Model(&MockFulfillmentAttemptRecord{}).
		Where("tenant_id = ? AND order_id = ?", tenantID, orderID))
	for name, got := range counts {
		if got != 1 {
			t.Errorf("SQL %s rows=%d, want exactly 1", name, got)
		}
	}
	var inventory InventoryRecord
	if err := store.db.WithContext(ctx).Where("tenant_id = ? AND merchant_id = ? AND sku_id = ? AND region = ?",
		tenantID, fixture.merchantID, fixture.skuID, "CN-EAST").First(&inventory).Error; err != nil {
		t.Fatal(err)
	}
	if inventory.OnHand != 3 || inventory.Reserved != 0 {
		t.Fatalf("inventory reservation/consumption mismatch: on_hand=%d reserved=%d", inventory.OnHand, inventory.Reserved)
	}
	binaryBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	binaryHash := sha256.Sum256(binaryBytes)
	artifactDirectory, runID, err := lifecycleEvidenceDirectory(moduleRoot, "mcp-subprocess")
	if err != nil {
		t.Fatalf("create MCP E2E evidence directory: %v", err)
	}
	manifest := map[string]any{
		"schema_version": "stablepay.agent-shopping-mcp-e2e.v1", "run_id": runID,
		"created_at_utc": time.Now().UTC().Format(time.RFC3339Nano),
		"database":       SimulationDatabaseName, "tenant_id": tenantID,
		"mcp_protocol_version": "2026-07-28", "mcp_binary_sha256": hex.EncodeToString(binaryHash[:]),
		"api_server": "in-process httptest", "intent_parser": "deterministic integration-test stub",
		"approver": "synthetic test actor with separate credential", "payment_adapter": "deterministic mock",
		"real_settlement": false, "model_or_external_api_calls": false,
	}
	metrics := map[string]any{
		"status": "PASS", "tools_listed": len(listed.Tools), "task_id": search.TaskID,
		"order_id": orderID, "payment_id": paymentID, "fulfillment_id": fulfillment.Fulfillment.ID,
		"order_status": persistedOrder.Status, "payment_status": persistedPayment.Status,
		"fulfillment_status":         persistedFulfillment.Fulfillment.Status,
		"idempotent_checkout_replay": true, "idempotent_fulfillment_replay": true,
		"real_settlement": false, "synthetic_approver": true,
		"sql_rows_by_table": counts, "inventory_on_hand": inventory.OnHand,
		"inventory_reserved": inventory.Reserved,
	}
	trial := map[string]any{
		"trial_id": runID + "-trial-001", "task_id": search.TaskID, "order_id": orderID,
		"payment_id": paymentID, "fulfillment_id": fulfillment.Fulfillment.ID,
		"task_status": search.TaskStatus, "order_status": persistedOrder.Status,
		"payment_status": persistedPayment.Status, "fulfillment_status": persistedFulfillment.Fulfillment.Status,
		"sql_rows_by_table": counts, "inventory_on_hand": inventory.OnHand,
		"inventory_reserved": inventory.Reserved, "real_settlement": false,
	}
	for name, value := range map[string]any{"manifest.json": manifest, "metrics.json": metrics} {
		if err := writeLifecycleJSON(filepath.Join(artifactDirectory, name), value); err != nil {
			t.Fatalf("write MCP E2E %s: %v", name, err)
		}
	}
	if err := writeLifecycleLines(filepath.Join(artifactDirectory, "trials.jsonl"), []map[string]any{trial}); err != nil {
		t.Fatalf("write MCP E2E trials: %v", err)
	}
	t.Logf("MCP_MYSQL_E2E PASS protocol=2026-07-28 task=%s order=%s payment=%s fulfillment=%s SQL=%v real_settlement=false synthetic_approver=true evidence=%s",
		search.TaskID, orderID, paymentID, fulfillment.Fulfillment.ID, counts, artifactDirectory)
}

type mysqlMCPSubprocess struct {
	command        *exec.Cmd
	stdin          io.WriteCloser
	stdout         *bufio.Reader
	stderr         strings.Builder
	cancel         context.CancelFunc
	lastStructured []byte
	nextID         int
	meta           map[string]any
}

func startMySQLIntegrationMCP(ctx context.Context, binary, baseURL string) (*mysqlMCPSubprocess, error) {
	childContext, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	command := exec.CommandContext(childContext, binary)
	allowed := []string{"PATH", "SystemRoot", "WINDIR"}
	for _, name := range allowed {
		if value := os.Getenv(name); value != "" {
			command.Env = append(command.Env, name+"="+value)
		}
	}
	command.Env = append(command.Env,
		"SHOP_SIM_BASE_URL="+baseURL,
		"SHOP_SIM_API_KEY="+mysqlIntegrationAPIKey,
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		return nil, err
	}
	var stderr strings.Builder
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		return nil, err
	}
	return &mysqlMCPSubprocess{
		command: command, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: stderr, cancel: cancel,
		meta: map[string]any{
			"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
			"io.modelcontextprotocol/clientInfo":         map[string]string{"name": "stablepay-mysql-e2e", "version": "1.0"},
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		},
	}, nil
}

func (m *mysqlMCPSubprocess) request(method string, params map[string]any, destination any) error {
	m.nextID++
	params["_meta"] = m.meta
	encoded, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.nextID, "method": method, "params": params})
	if err != nil {
		return err
	}
	if _, err := m.stdin.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write MCP request: %w", err)
	}
	line, err := m.stdout.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read MCP response: %w", err)
	}
	var response struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &response); err != nil {
		return fmt.Errorf("decode MCP response: %w", err)
	}
	if response.Error != nil {
		return fmt.Errorf("MCP JSON-RPC error %d: %s", response.Error.Code, response.Error.Message)
	}
	return json.Unmarshal(response.Result, destination)
}

func (m *mysqlMCPSubprocess) callTool(name string, arguments map[string]any, destination any) error {
	var result struct {
		IsError           bool            `json:"isError"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		Content           []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := m.request("tools/call", map[string]any{"name": name, "arguments": arguments}, &result); err != nil {
		return err
	}
	if result.IsError {
		return fmt.Errorf("MCP tool %s returned an error: %v", name, result.Content)
	}
	if len(result.StructuredContent) == 0 {
		return fmt.Errorf("MCP tool %s omitted structured content", name)
	}
	m.lastStructured = append(m.lastStructured[:0], result.StructuredContent...)
	var upstream struct {
		HTTPStatus int             `json:"http_status"`
		Response   json.RawMessage `json:"response"`
	}
	if err := json.Unmarshal(result.StructuredContent, &upstream); err != nil {
		return fmt.Errorf("decode MCP tool %s HTTP envelope: %w", name, err)
	}
	if upstream.HTTPStatus < http.StatusOK || upstream.HTTPStatus >= http.StatusMultipleChoices {
		return fmt.Errorf("MCP tool %s returned HTTP %d: %s", name, upstream.HTTPStatus, result.StructuredContent)
	}
	if len(upstream.Response) == 0 || string(upstream.Response) == "null" {
		return fmt.Errorf("MCP tool %s returned an empty HTTP response: %s", name, result.StructuredContent)
	}
	return json.Unmarshal(upstream.Response, destination)
}

func (m *mysqlMCPSubprocess) close(t *testing.T) {
	t.Helper()
	if err := m.stdin.Close(); err != nil {
		t.Errorf("close MCP stdin: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- m.command.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("MCP subprocess exit: %v; stderr=%s", err, m.stderr.String())
		}
	case <-time.After(5 * time.Second):
		m.cancel()
		if err := <-done; err != nil {
			t.Errorf("MCP subprocess required forced cancellation: %v; stderr=%s", err, m.stderr.String())
		}
	}
	m.cancel()
	if stderr := strings.TrimSpace(m.stderr.String()); stderr != "" {
		t.Errorf("MCP subprocess emitted stderr: %s", stderr)
	}
}
