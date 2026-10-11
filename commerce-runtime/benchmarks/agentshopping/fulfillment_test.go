package agentshopping

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

type mysqlIntegrationFulfillmentResponse struct {
	Order       Order       `json:"order"`
	Payment     MockPayment `json:"payment"`
	Fulfillment Fulfillment `json:"fulfillment"`
	Refund      *MockRefund `json:"refund"`
	HTTPStatus  int         `json:"-"`
}

func TestSimulatedFulfillmentFailureIsDeterministicAndPredeclared(t *testing.T) {
	failures := 0
	for index := 0; index < 10000; index++ {
		taskID := fmt.Sprintf("predeclared-fulfillment-trial-%05d", index)
		first := simulatedFulfillmentFailure(taskID)
		if first != simulatedFulfillmentFailure(taskID) {
			t.Fatalf("fulfillment result changed for stable task ID %q", taskID)
		}
		if first {
			failures++
		}
	}
	if failures < 900 || failures > 1100 {
		t.Fatalf("deterministic fault fixture generated %d/10000 failures, outside its declared 10%% band", failures)
	}
}

func TestMockFulfillmentFaultPlanIsBoundedAndDistinguishesTransientFromPermanent(t *testing.T) {
	transient, err := normalizeMockFulfillmentFaultPlan(MockFulfillmentFaultPlan{TransientFailures: 1, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("normalize transient plan: %v", err)
	}
	if got := mockFulfillmentAttemptOutcome(transient, 1); got != FulfillmentAttemptRetryableFailure {
		t.Fatalf("first transient attempt status=%q, want %q", got, FulfillmentAttemptRetryableFailure)
	}
	if got := mockFulfillmentAttemptOutcome(transient, 2); got != FulfillmentAttemptDelivered {
		t.Fatalf("retry attempt status=%q, want %q", got, FulfillmentAttemptDelivered)
	}
	exhausted, err := normalizeMockFulfillmentFaultPlan(MockFulfillmentFaultPlan{TransientFailures: 2, MaxAttempts: 2})
	if err != nil {
		t.Fatalf("normalize retry-exhaustion plan: %v", err)
	}
	if got := mockFulfillmentAttemptOutcome(exhausted, 1); got != FulfillmentAttemptRetryableFailure {
		t.Fatalf("pre-exhaustion attempt status=%q, want %q", got, FulfillmentAttemptRetryableFailure)
	}
	if got := mockFulfillmentAttemptOutcome(exhausted, 2); got != FulfillmentAttemptPermanentFailure {
		t.Fatalf("exhausted attempt status=%q, want %q", got, FulfillmentAttemptPermanentFailure)
	}

	permanent, err := normalizeMockFulfillmentFaultPlan(MockFulfillmentFaultPlan{PermanentFailure: true, MaxAttempts: 3})
	if err != nil {
		t.Fatalf("normalize permanent plan: %v", err)
	}
	if got := mockFulfillmentAttemptOutcome(permanent, 1); got != FulfillmentAttemptPermanentFailure {
		t.Fatalf("permanent attempt status=%q, want %q", got, FulfillmentAttemptPermanentFailure)
	}

	for _, invalid := range []MockFulfillmentFaultPlan{
		{TransientFailures: -1, MaxAttempts: 3},
		{MaxAttempts: 11},
		{TransientFailures: 1, PermanentFailure: true, MaxAttempts: 3},
	} {
		if _, err := normalizeMockFulfillmentFaultPlan(invalid); err == nil {
			t.Errorf("invalid fault plan was accepted: %+v", invalid)
		}
	}
	defaultPlan, err := normalizeMockFulfillmentFaultPlan(MockFulfillmentFaultPlan{})
	if err != nil || defaultPlan.MaxAttempts != defaultMockFulfillmentMaxAttempts {
		t.Fatalf("default max-attempt policy=%+v err=%v", defaultPlan, err)
	}
}

func TestFulfillmentRetryBackoffIsBoundedAndRoundsUp(t *testing.T) {
	if got := mockFulfillmentRetryDelay(1); got != 5*time.Millisecond {
		t.Fatalf("first retry delay=%s, want 5ms", got)
	}
	if got := mockFulfillmentRetryDelay(3); got != 20*time.Millisecond {
		t.Fatalf("third-attempt retry delay=%s, want 20ms", got)
	}
	if got := mockFulfillmentRetryDelay(10); got > time.Second {
		t.Fatalf("retry delay exceeded cap: %s", got)
	}
	now := time.Now().UTC()
	if got := retryAfterMillis(now.Add(1500*time.Microsecond), now); got != 2 {
		t.Fatalf("retry-after rounded down: got %dms, want 2ms", got)
	}
}

func TestMySQLHTTPFulfillmentIsIdempotentAndCompensatesDeliveryFailure(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_INTEGRATION") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_INTEGRATION=1 to run fulfillment and compensation against isolated simulation MySQL")
	}
	password := strings.TrimSpace(os.Getenv("SHOP_SIM_DB_PASSWORD"))
	if password == "" {
		t.Fatal("SHOP_SIM_DB_PASSWORD is required when SHOP_SIM_MYSQL_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated simulation MySQL: %v", err)
		}
	})
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("isolated MySQL ping failed: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("isolated MySQL schema migration failed: %v", err)
	}
	suffix := mysqlIntegrationSuffix(t)
	tenantID := "shopping-it-" + suffix
	store.tenant = tenantID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := cleanupMySQLIntegrationTenant(cleanupCtx, store, tenantID); err != nil {
			t.Errorf("clean fulfillment integration tenant %q: %v", tenantID, err)
		}
	})
	api, err := NewHTTPAPI(store, mysqlIntegrationParser{}, mysqlIntegrationAPIKey, mysqlIntegrationBuyerApprovalKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client := &mysqlIntegrationAPI{client: server.Client(), server: server, key: mysqlIntegrationAPIKey}

	for _, testCase := range []struct {
		name      string
		fixture   string
		mustFail  bool
		transient bool
		plan      MockFulfillmentFaultPlan
	}{
		{name: "delivered", fixture: "delivered", plan: MockFulfillmentFaultPlan{MaxAttempts: 3}},
		{name: "permanent delivery failure is refunded and restocked", fixture: "delivery-failure", mustFail: true,
			plan: MockFulfillmentFaultPlan{PermanentFailure: true, MaxAttempts: 1}},
		{name: "transient delivery failure retries to delivery", fixture: "transient-delivery-failure", transient: true,
			plan: MockFulfillmentFaultPlan{TransientFailures: 1, MaxAttempts: 3}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := createMySQLIntegrationFixture(t, ctx, store, client, testCase.fixture+"-"+suffix)
			quote := client.issueAndCheckQuote(t, fixture)
			confirmation := client.confirm(t, quote, true, "confirm-"+fixture.taskID, http.StatusCreated).confirmation
			checkoutKey := settledCheckoutKey("settled-" + fixture.taskID)
			checkout := client.checkout(t, confirmation.ID, checkoutKey, http.StatusCreated)
			if checkout.Order.Status != OrderPaid || checkout.Payment.Status != PaymentSettled || !checkout.Payment.SimulationOnly {
				t.Fatalf("fixture did not begin fulfillment from a simulation-only paid order: %+v", checkout)
			}
			var inventoryBefore InventoryRecord
			if err := store.db.WithContext(ctx).Where("tenant_id = ? AND merchant_id = ? AND sku_id = ? AND region = ?",
				tenantID, fixture.merchantID, fixture.skuID, "CN-EAST").First(&inventoryBefore).Error; err != nil {
				t.Fatal(err)
			}
			var outstandingReservation *InventoryReservationRecord
			if testCase.mustFail {
				inventoryBefore.Reserved++
				if err := store.db.WithContext(ctx).Save(&inventoryBefore).Error; err != nil {
					t.Fatalf("record a concurrent outstanding inventory reservation: %v", err)
				}
				reservation := InventoryReservationRecord{ReservationID: StableID("fixture-reservation", tenantID, fixture.taskID),
					TenantID: tenantID, OrderID: StableID("fixture-pending-order", tenantID, fixture.taskID),
					MerchantID: fixture.merchantID, SKUID: fixture.skuID, Region: "CN-EAST", Quantity: 1,
					Status: "RESERVED", CreatedAt: time.Now().UTC(), CompletedAt: time.Now().UTC(), DataOrigin: syntheticOrigin}
				if err := store.db.WithContext(ctx).Create(&reservation).Error; err != nil {
					t.Fatalf("create a concurrent outstanding inventory reservation: %v", err)
				}
				outstandingReservation = &reservation
			}

			fulfillmentKey := "fulfillment-" + fixture.taskID
			store.mockFulfillmentFaults = map[string]MockFulfillmentFaultPlan{fixture.taskID: testCase.plan}
			first := client.fulfill(t, checkout.Order.ID, fulfillmentKey)
			if testCase.transient {
				if first.HTTPStatus != http.StatusAccepted || first.Fulfillment.Status != FulfillmentRetryPending ||
					first.Order.Status != OrderPaid || first.Refund != nil || !first.Fulfillment.Retryable || first.Fulfillment.AttemptCount != 1 {
					t.Fatalf("transient delivery failure was not durably scheduled for retry: %+v", first)
				}
				if first.Fulfillment.RetryAfterMillis > 0 {
					time.Sleep(time.Duration(first.Fulfillment.RetryAfterMillis) * time.Millisecond)
				}
				first = client.fulfill(t, checkout.Order.ID, fulfillmentKey)
				if first.HTTPStatus != http.StatusOK || first.Fulfillment.Status != FulfillmentDelivered ||
					first.Fulfillment.AttemptCount != 2 || first.Fulfillment.Retryable {
					t.Fatalf("bounded retry did not resume and deliver: %+v", first)
				}
			}
			if first.Order.Status == OrderFulfilled {
				if testCase.mustFail || first.Fulfillment.Status != FulfillmentDelivered || first.Refund != nil || first.Fulfillment.DeliveryReference == "" {
					t.Fatalf("unexpected successful fulfillment: %+v", first)
				}
			} else {
				if !testCase.mustFail || first.Order.Status != OrderRefunded || first.Fulfillment.Status != FulfillmentCompensated ||
					first.Fulfillment.FailureCode != "SIMULATED_DELIVERY_FAILURE" || first.Refund == nil ||
					first.Refund.Status != RefundCompleted || first.Refund.AmountMinor != checkout.Order.AmountMinor ||
					first.Refund.Currency != checkout.Order.Currency || !first.Refund.SimulationOnly {
					t.Fatalf("delivery failure did not produce an exact simulation-only refund: %+v", first)
				}
				var inventoryAfter InventoryRecord
				if err := store.db.WithContext(ctx).Where("tenant_id = ? AND merchant_id = ? AND sku_id = ? AND region = ?",
					tenantID, fixture.merchantID, fixture.skuID, "CN-EAST").First(&inventoryAfter).Error; err != nil {
					t.Fatal(err)
				}
				if inventoryAfter.OnHand != inventoryBefore.OnHand+1 || inventoryAfter.Reserved != inventoryBefore.Reserved {
					t.Fatalf("compensation did not restock without disturbing the concurrent reservation: before=%+v after=%+v",
						inventoryBefore, inventoryAfter)
				}
				if outstandingReservation != nil {
					var reservationAfter InventoryReservationRecord
					if err := store.db.WithContext(ctx).Where("tenant_id = ? AND reservation_id = ?", tenantID,
						outstandingReservation.ReservationID).First(&reservationAfter).Error; err != nil {
						t.Fatal(err)
					}
					if reservationAfter.Status != "RESERVED" || reservationAfter.Quantity != 1 {
						t.Fatalf("compensation modified the unrelated outstanding reservation: %+v", reservationAfter)
					}
				}
			}
			if first.Payment.Status != PaymentSettled || !first.Payment.SimulationOnly {
				t.Fatalf("fulfillment changed the immutable original payment fact: %+v", first.Payment)
			}

			replay := client.fulfill(t, checkout.Order.ID, fulfillmentKey)
			if replay.Order.ID != first.Order.ID || replay.Fulfillment.ID != first.Fulfillment.ID ||
				replay.Fulfillment.Status != first.Fulfillment.Status || (replay.Refund == nil) != (first.Refund == nil) {
				t.Fatalf("fulfillment retry changed its durable result: first=%+v replay=%+v", first, replay)
			}
			conflict := client.fulfillError(t, checkout.Order.ID, fulfillmentKey+"-different", http.StatusConflict)
			if conflict != "IDEMPOTENCY_CONFLICT" {
				t.Fatalf("second fulfillment key error=%q, want IDEMPOTENCY_CONFLICT", conflict)
			}
			readBack := client.do(t, http.MethodGet, "/v1/orders/"+checkout.Order.ID+"/fulfillment", nil)
			if readBack.status != http.StatusOK {
				t.Fatalf("fulfillment read-back status=%d body=%s", readBack.status, readBack.body)
			}
			var readResult mysqlIntegrationFulfillmentResponse
			if err := json.Unmarshal(readBack.body, &readResult); err != nil {
				t.Fatal(err)
			}
			if readResult.Fulfillment.ID != first.Fulfillment.ID || readResult.Order.Status != first.Order.Status {
				t.Fatalf("fulfillment read-back differs from committed result: got=%+v want=%+v", readResult, first)
			}

			var fulfillmentCount, refundCount int64
			if err := store.db.WithContext(ctx).Model(&MockFulfillmentRecord{}).Where("tenant_id = ? AND order_id = ?", tenantID, checkout.Order.ID).Count(&fulfillmentCount).Error; err != nil {
				t.Fatal(err)
			}
			if err := store.db.WithContext(ctx).Model(&MockRefundRecord{}).Where("tenant_id = ? AND order_id = ?", tenantID, checkout.Order.ID).Count(&refundCount).Error; err != nil {
				t.Fatal(err)
			}
			if fulfillmentCount != 1 || refundCount != int64(boolToInt(testCase.mustFail)) {
				t.Fatalf("idempotent fulfillment effects duplicated: fulfillments=%d refunds=%d", fulfillmentCount, refundCount)
			}
			var inventory InventoryRecord
			if err := store.db.WithContext(ctx).Where("tenant_id = ? AND merchant_id = ? AND sku_id = ? AND region = ?",
				tenantID, fixture.merchantID, fixture.skuID, "CN-EAST").First(&inventory).Error; err != nil {
				t.Fatal(err)
			}
			if testCase.mustFail {
				wantReserved := 0
				if outstandingReservation != nil {
					wantReserved = outstandingReservation.Quantity
				}
				if inventory.OnHand != 4 || inventory.Reserved != wantReserved {
					t.Fatalf("compensated delivery did not restore inventory while preserving reservations: on_hand=%d reserved=%d want_on_hand=4 want_reserved=%d",
						inventory.OnHand, inventory.Reserved, wantReserved)
				}
				client.assertTaskStatus(t, fixture.taskID, TaskStatusRefunded)
			} else {
				if inventory.OnHand != 3 || inventory.Reserved != 0 {
					t.Fatalf("delivered order inventory state mismatch: on_hand=%d reserved=%d", inventory.OnHand, inventory.Reserved)
				}
				client.assertTaskStatus(t, fixture.taskID, TaskStatusFulfilled)
			}
		})
	}

	declinedFixture := createMySQLIntegrationFixture(t, ctx, store, client, "declined-"+suffix)
	declinedQuote := client.issueAndCheckQuote(t, declinedFixture)
	declinedConfirmation := client.confirm(t, declinedQuote, true, "declined-confirm-"+suffix, http.StatusCreated).confirmation
	declinedCheckout := client.checkout(t, declinedConfirmation.ID, declinedCheckoutKey("declined-"+suffix), http.StatusCreated)
	if declinedCheckout.Payment.Status != PaymentDeclined {
		t.Fatalf("expected deterministic declined payment, got %s", declinedCheckout.Payment.Status)
	}
	blocked := client.fulfillError(t, declinedCheckout.Order.ID, "must-not-fulfill-declined-"+suffix, http.StatusConflict)
	if blocked != "ORDER_NOT_READY" {
		t.Fatalf("declined order fulfillment error=%q, want ORDER_NOT_READY", blocked)
	}
	var declinedFulfillments, declinedRefunds int64
	if err := store.db.WithContext(ctx).Model(&MockFulfillmentRecord{}).Where("tenant_id = ? AND order_id = ?", tenantID, declinedCheckout.Order.ID).Count(&declinedFulfillments).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.db.WithContext(ctx).Model(&MockRefundRecord{}).Where("tenant_id = ? AND order_id = ?", tenantID, declinedCheckout.Order.ID).Count(&declinedRefunds).Error; err != nil {
		t.Fatal(err)
	}
	if declinedFulfillments != 0 || declinedRefunds != 0 {
		t.Fatalf("declined payment created fulfillment/refund rows: %d/%d", declinedFulfillments, declinedRefunds)
	}
	fulfillmentAudit := auditPurchaseReplayFulfillment(ctx, store, tenantID)
	if !fulfillmentAudit.Passed || fulfillmentAudit.ObservedFulfilledOrders != 2 ||
		fulfillmentAudit.ObservedCompensatedOrders != 1 || fulfillmentAudit.ObservedRefunds != 1 ||
		fulfillmentAudit.ObservedProviderAttempts != 4 || fulfillmentAudit.RetryableFailureAttempts != 1 ||
		fulfillmentAudit.DeclinedPaymentsWithFulfillmentOrRefund != 0 {
		t.Fatalf("isolated fulfillment SQL audit failed: %+v", fulfillmentAudit)
	}
}

func (api *mysqlIntegrationAPI) fulfill(t *testing.T, orderID, key string) mysqlIntegrationFulfillmentResponse {
	t.Helper()
	response := api.do(t, http.MethodPost, "/v1/orders/"+orderID+"/fulfillment", map[string]any{"idempotency_key": key})
	if response.status != http.StatusOK && response.status != http.StatusAccepted {
		t.Fatalf("fulfillment status=%d body=%s", response.status, response.body)
	}
	var result mysqlIntegrationFulfillmentResponse
	if err := json.Unmarshal(response.body, &result); err != nil {
		t.Fatal(err)
	}
	result.HTTPStatus = response.status
	return result
}

func (api *mysqlIntegrationAPI) fulfillError(t *testing.T, orderID, key string, expectedStatus int) string {
	t.Helper()
	response := api.do(t, http.MethodPost, "/v1/orders/"+orderID+"/fulfillment", map[string]any{"idempotency_key": key})
	if response.status != expectedStatus {
		t.Fatalf("fulfillment error status=%d want=%d body=%s", response.status, expectedStatus, response.body)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Error.Code
}

func TestMySQLHTTPFulfillmentRetryResumesAfterProcessRestart(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_HTTP_FULFILLMENT_RESTART") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_HTTP_FULFILLMENT_RESTART=1 to verify durable fulfillment retry after an OS-process restart")
	}
	password := strings.TrimSpace(os.Getenv("SHOP_SIM_DB_PASSWORD"))
	if password == "" {
		t.Fatal("SHOP_SIM_DB_PASSWORD is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated fulfillment retry store: %v", err)
		}
	})
	if err := store.Ping(ctx); err != nil {
		t.Fatal("isolated simulation MySQL is not ready")
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal("isolated simulation schema migration failed")
	}
	tenantID := "shopping-purchase-replay-retry-" + mysqlIntegrationSuffix(t)
	store.tenant = tenantID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := cleanupMySQLIntegrationTenant(cleanupCtx, store, tenantID); err != nil {
			t.Errorf("clean fulfillment retry tenant %q: %v", tenantID, err)
		}
	})
	api, err := NewHTTPAPI(store, mysqlIntegrationParser{}, mysqlIntegrationAPIKey, mysqlIntegrationBuyerApprovalKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client := &mysqlIntegrationAPI{client: server.Client(), server: server, key: mysqlIntegrationAPIKey}
	fixture := createMySQLIntegrationFixture(t, ctx, store, client, "transient-retry-process-restart")
	quote := client.issueAndCheckQuote(t, fixture)
	confirmation := client.confirm(t, quote, true, "retry-confirm-"+fixture.taskID, http.StatusCreated).confirmation
	checkout := client.checkout(t, confirmation.ID, settledCheckoutKey("retry-order-"+fixture.taskID), http.StatusCreated)
	store.mockFulfillmentFaults = map[string]MockFulfillmentFaultPlan{
		fixture.taskID: {TransientFailures: 1, MaxAttempts: defaultMockFulfillmentMaxAttempts},
	}
	fulfillmentKey := "retry-fulfillment-" + fixture.taskID
	pending := client.fulfill(t, checkout.Order.ID, fulfillmentKey)
	if pending.HTTPStatus != http.StatusAccepted || pending.Order.Status != OrderPaid || pending.Fulfillment.Status != FulfillmentRetryPending ||
		pending.Fulfillment.AttemptCount != 1 || pending.Refund != nil {
		t.Fatalf("first transient delivery attempt did not persist retry-pending state: %+v", pending)
	}
	server.Close()
	processAddr := unusedLoopbackAddress(t)
	process := startPurchaseReplayProcessServer(t, processAddr, tenantID)
	defer stopPurchaseReplayProcessServer(process)
	if pending.Fulfillment.RetryAfterMillis > 0 {
		time.Sleep(time.Duration(pending.Fulfillment.RetryAfterMillis) * time.Millisecond)
	}
	processClient := &http.Client{Timeout: 10 * time.Second}
	response, err := doPurchaseReplayProcessFulfillment(processClient, processAddr, checkout.Order.ID, fulfillmentKey)
	if err != nil || response.status != http.StatusOK {
		t.Fatalf("durable fulfillment retry after API process restart failed: status=%d err=%v body=%s", response.status, err, response.body)
	}
	var recovered mysqlIntegrationFulfillmentResponse
	if err := json.Unmarshal(response.body, &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered.Order.Status != OrderFulfilled || recovered.Payment.ID != checkout.Payment.ID || recovered.Payment.Status != PaymentSettled ||
		recovered.Fulfillment.Status != FulfillmentDelivered || recovered.Fulfillment.AttemptCount != 2 || recovered.Refund != nil {
		t.Fatalf("post-restart retry did not recover the same paid order: %+v", recovered)
	}
	replay, err := doPurchaseReplayProcessFulfillment(processClient, processAddr, checkout.Order.ID, fulfillmentKey)
	if err != nil || replay.status != http.StatusOK {
		t.Fatalf("terminal same-key fulfillment replay failed: status=%d err=%v", replay.status, err)
	}
	var replayed mysqlIntegrationFulfillmentResponse
	if err := json.Unmarshal(replay.body, &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.Order.ID != recovered.Order.ID || replayed.Payment.ID != recovered.Payment.ID || replayed.Fulfillment.ID != recovered.Fulfillment.ID {
		t.Fatalf("same-key replay changed terminal identities: first=%+v replay=%+v", recovered, replayed)
	}
	conflict, err := doPurchaseReplayProcessFulfillment(processClient, processAddr, checkout.Order.ID, fulfillmentKey+"-different")
	if err != nil || conflict.status != http.StatusConflict {
		t.Fatalf("conflicting fulfillment idempotency key status=%d err=%v body=%s", conflict.status, err, conflict.body)
	}
	stopPurchaseReplayProcessServer(process)

	var attemptCount, fulfillmentCount, refundCount int64
	for _, check := range []struct {
		model any
		count *int64
	}{
		{&MockFulfillmentAttemptRecord{}, &attemptCount},
		{&MockFulfillmentRecord{}, &fulfillmentCount},
		{&MockRefundRecord{}, &refundCount},
	} {
		if err := store.db.WithContext(ctx).Model(check.model).Where("tenant_id=? AND order_id=?", tenantID, checkout.Order.ID).Count(check.count).Error; err != nil {
			t.Fatal(err)
		}
	}
	if attemptCount != 2 || fulfillmentCount != 1 || refundCount != 0 {
		t.Fatalf("retry recovery side effects mismatch: attempts=%d fulfillments=%d refunds=%d", attemptCount, fulfillmentCount, refundCount)
	}
	audit := auditPurchaseReplayFulfillment(ctx, store, tenantID)
	if !audit.Passed || audit.ObservedProviderAttempts != 2 || audit.RetryableFailureAttempts != 1 ||
		audit.ObservedFulfilledOrders != 1 || audit.ObservedCompensatedOrders != 0 || audit.RetryableFailuresWithoutRetry != 0 {
		t.Fatalf("durable fulfillment retry SQL audit failed: %+v", audit)
	}
}

func declinedCheckoutKey(prefix string) string {
	for index := 0; index < 10000; index++ {
		key := fmt.Sprintf("%s-%d", prefix, index)
		if simulatedPaymentStatus(key) == PaymentDeclined {
			return key
		}
	}
	return prefix + "-fallback"
}
