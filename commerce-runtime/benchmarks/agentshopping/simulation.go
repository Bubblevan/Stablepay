package agentshopping

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
)

var (
	ErrNotFound              = errors.New("simulation record not found")
	ErrUnavailable           = errors.New("item is not available for the requested region")
	ErrBudgetExceeded        = errors.New("quoted price exceeds buyer budget")
	ErrStaleQuote            = errors.New("quote is stale; issue a new quote and obtain confirmation again")
	ErrQuoteAmountMismatch   = errors.New("confirmation amount does not match the immutable quote")
	ErrConfirmationRequired  = errors.New("explicit buyer confirmation is required")
	ErrIdempotencyConflict   = errors.New("idempotency key was already used for a different confirmation")
	ErrClarificationNotOpen  = errors.New("shopping task is not awaiting clarification from this buyer")
	ErrCandidateNotAllowed   = errors.New("selected product is not an exact match or an explicitly allowed substitute")
	ErrOrderNotReady         = errors.New("order is not eligible for simulated fulfillment")
	ErrInvalidSimulationData = errors.New("invalid benchmark simulation data")
)

const (
	OrderPaid                          = "PAID"
	OrderPaymentError                  = "PAYMENT_FAILED"
	OrderFulfilled                     = "FULFILLED"
	OrderRefunded                      = "REFUNDED"
	PaymentSettled                     = "SETTLED"
	PaymentDeclined                    = "DECLINED"
	FulfillmentDelivered               = "DELIVERED"
	FulfillmentCompensated             = "FAILED_COMPENSATED"
	FulfillmentRetryPending            = "RETRY_PENDING"
	FulfillmentAttemptRetryableFailure = "RETRYABLE_FAILURE"
	FulfillmentAttemptDelivered        = "DELIVERED"
	FulfillmentAttemptPermanentFailure = "PERMANENT_FAILURE"
	RefundCompleted                    = "REFUNDED"
)

type Inventory struct {
	TenantID   string
	MerchantID string
	SKUID      string
	Region     string
	OnHand     int
	Reserved   int
	Eligible   bool
	PriceMinor int64
	Revision   int64
}

type QuoteRequest struct {
	TenantID   string
	TaskID     string
	MerchantID string
	SKUID      string
	Region     string
	MaxMinor   int64
	Currency   string
}

// Quote is an immutable snapshot. Repricing never edits an issued quote; it
// changes inventory revision and requires a new quote plus new consent.
type Quote struct {
	ID                 string    `json:"quote_id"`
	TenantID           string    `json:"tenant_id"`
	TaskID             string    `json:"task_id"`
	BuyerID            string    `json:"buyer_id"`
	Version            int64     `json:"version"`
	MerchantID         string    `json:"merchant_id"`
	SKUID              string    `json:"sku_id"`
	RequestedItem      string    `json:"requested_item"`
	ProductItem        string    `json:"product_item"`
	ProductTitle       string    `json:"product_title"`
	MatchType          string    `json:"match_type"`
	SubstitutionNotice string    `json:"substitution_notice,omitempty"`
	Region             string    `json:"region"`
	AmountMinor        int64     `json:"amount_minor"`
	Currency           string    `json:"currency"`
	StockRevision      int64     `json:"stock_revision"`
	SnapshotHash       string    `json:"snapshot_hash"`
	CreatedAt          time.Time `json:"created_at"`
	ExpiresAt          time.Time `json:"expires_at"`
}

type Confirmation struct {
	ID             string    `json:"confirmation_id"`
	TenantID       string    `json:"tenant_id"`
	BuyerID        string    `json:"buyer_id"`
	QuoteID        string    `json:"quote_id"`
	QuoteVersion   int64     `json:"quote_version"`
	QuoteHash      string    `json:"quote_hash"`
	AmountMinor    int64     `json:"amount_minor"`
	Accepted       bool      `json:"accepted"`
	IdempotencyKey string    `json:"idempotency_key,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type Order struct {
	ID             string    `json:"order_id"`
	TenantID       string    `json:"tenant_id"`
	TaskID         string    `json:"task_id"`
	BuyerID        string    `json:"buyer_id"`
	QuoteID        string    `json:"quote_id"`
	ConfirmationID string    `json:"confirmation_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	AmountMinor    int64     `json:"amount_minor"`
	Currency       string    `json:"currency"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	CompletedAt    time.Time `json:"completed_at,omitempty"`
}

type MockPayment struct {
	ID                  string    `json:"payment_id"`
	TenantID            string    `json:"tenant_id"`
	OrderID             string    `json:"order_id"`
	AmountMinor         int64     `json:"amount_minor"`
	Currency            string    `json:"currency"`
	Provider            string    `json:"provider"`
	Status              string    `json:"status"`
	SimulationOnly      bool      `json:"simulation_only"`
	SettlementReference string    `json:"settlement_reference,omitempty"`
	FailureCode         string    `json:"failure_code,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

type AuditRecord struct {
	Sequence int64     `json:"sequence"`
	TenantID string    `json:"tenant_id"`
	TaskID   string    `json:"task_id"`
	EntityID string    `json:"entity_id"`
	Action   string    `json:"action"`
	At       time.Time `json:"at"`
}

type CheckoutResult struct {
	Order   Order       `json:"order"`
	Payment MockPayment `json:"payment"`
}

type Fulfillment struct {
	ID                string     `json:"fulfillment_id"`
	TenantID          string     `json:"tenant_id"`
	OrderID           string     `json:"order_id"`
	IdempotencyKey    string     `json:"idempotency_key"`
	Status            string     `json:"status"`
	Provider          string     `json:"provider"`
	DeliveryReference string     `json:"delivery_reference,omitempty"`
	FailureCode       string     `json:"failure_code,omitempty"`
	AttemptCount      int        `json:"attempt_count"`
	MaxAttempts       int        `json:"max_attempts"`
	Retryable         bool       `json:"retryable"`
	RetryAfterMillis  int64      `json:"retry_after_ms,omitempty"`
	SimulationOnly    bool       `json:"simulation_only"`
	CreatedAt         time.Time  `json:"created_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

type MockRefund struct {
	ID              string    `json:"refund_id"`
	TenantID        string    `json:"tenant_id"`
	OrderID         string    `json:"order_id"`
	PaymentID       string    `json:"payment_id"`
	AmountMinor     int64     `json:"amount_minor"`
	Currency        string    `json:"currency"`
	Provider        string    `json:"provider"`
	Status          string    `json:"status"`
	IdempotencyKey  string    `json:"idempotency_key"`
	RefundReference string    `json:"refund_reference"`
	SimulationOnly  bool      `json:"simulation_only"`
	CreatedAt       time.Time `json:"created_at"`
}

type FulfillmentResult struct {
	Order       Order       `json:"order"`
	Payment     MockPayment `json:"payment"`
	Fulfillment Fulfillment `json:"fulfillment"`
	Refund      *MockRefund `json:"refund,omitempty"`
}

// Simulator is an in-process benchmark-only commerce boundary. It has no
// production database, payment, merchant, or settlement adapter.
type Simulator struct {
	mu                 sync.Mutex
	inventory          map[string]Inventory
	quotes             map[string]Quote
	confirmations      map[string]Confirmation
	orders             map[string]Order
	orderByIdempotency map[string]string
	payments           map[string]MockPayment
	quoteVersions      map[string]int64
	audit              []AuditRecord
	paymentOutcome     string
	now                func() time.Time
}

func NewSimulator(inventory []Inventory) (*Simulator, error) {
	s := &Simulator{
		inventory:          make(map[string]Inventory, len(inventory)),
		quotes:             make(map[string]Quote),
		confirmations:      make(map[string]Confirmation),
		orders:             make(map[string]Order),
		orderByIdempotency: make(map[string]string),
		payments:           make(map[string]MockPayment),
		quoteVersions:      make(map[string]int64),
		paymentOutcome:     PaymentSettled,
		now:                func() time.Time { return time.Now().UTC() },
	}
	for _, row := range inventory {
		if row.TenantID == "" || row.MerchantID == "" || row.SKUID == "" || row.Region == "" ||
			row.OnHand < 0 || row.Reserved < 0 || row.Reserved > row.OnHand || row.PriceMinor < 0 || row.Revision < 1 {
			return nil, fmt.Errorf("%w: invalid inventory row for sku %q", ErrInvalidSimulationData, row.SKUID)
		}
		key := inventoryKey(row.TenantID, row.MerchantID, row.SKUID, row.Region)
		if _, exists := s.inventory[key]; exists {
			return nil, fmt.Errorf("%w: duplicate inventory row for sku %q", ErrInvalidSimulationData, row.SKUID)
		}
		s.inventory[key] = row
	}
	return s, nil
}

func inventoryKey(tenant, merchant, sku, region string) string {
	return tenant + "\x00" + merchant + "\x00" + sku + "\x00" + region
}

func idempotencyIndex(tenant, key string) string { return tenant + "\x00" + key }

func (s *Simulator) SetMockPaymentOutcome(outcome string) error {
	if outcome != PaymentSettled && outcome != PaymentDeclined {
		return fmt.Errorf("%w: unsupported mock payment outcome %q", ErrInvalidSimulationData, outcome)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paymentOutcome = outcome
	return nil
}

func (s *Simulator) IssueQuote(request QuoteRequest) (Quote, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if request.TenantID == "" || request.TaskID == "" || request.Currency != "USDC" {
		return Quote{}, fmt.Errorf("%w: tenant, task, and USDC currency are required", ErrInvalidSimulationData)
	}
	key := inventoryKey(request.TenantID, request.MerchantID, request.SKUID, request.Region)
	row, ok := s.inventory[key]
	if !ok {
		return Quote{}, ErrNotFound
	}
	if !row.Eligible || row.OnHand-row.Reserved < 1 {
		return Quote{}, ErrUnavailable
	}
	if row.PriceMinor > request.MaxMinor {
		return Quote{}, ErrBudgetExceeded
	}
	s.quoteVersions[key]++
	version := s.quoteVersions[key]
	quote := Quote{
		ID:       StableID("quote", request.TenantID, request.TaskID, request.MerchantID, request.SKUID, request.Region, strconv.FormatInt(version, 10)),
		TenantID: request.TenantID, TaskID: request.TaskID, Version: version,
		MerchantID: request.MerchantID, SKUID: request.SKUID, Region: request.Region,
		AmountMinor: row.PriceMinor, Currency: request.Currency, StockRevision: row.Revision, CreatedAt: s.now(),
	}
	s.quotes[quote.ID] = quote
	s.record(request.TenantID, request.TaskID, quote.ID, "QUOTE_ISSUED")
	return quote, nil
}

// ChangeInventory is the benchmark's controlled pre-confirmation mutation
// hook. It represents a merchant price/stock update, not a production adapter.
func (s *Simulator) ChangeInventory(tenant, merchant, sku, region string, priceMinor *int64, onHand *int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := inventoryKey(tenant, merchant, sku, region)
	row, ok := s.inventory[key]
	if !ok {
		return ErrNotFound
	}
	changed := false
	if priceMinor != nil {
		if *priceMinor < 0 {
			return fmt.Errorf("%w: negative price", ErrInvalidSimulationData)
		}
		row.PriceMinor = *priceMinor
		changed = true
	}
	if onHand != nil {
		if *onHand < row.Reserved || *onHand < 0 {
			return fmt.Errorf("%w: stock cannot be lower than already reserved quantity", ErrInvalidSimulationData)
		}
		row.OnHand = *onHand
		changed = true
	}
	if changed {
		row.Revision++
		s.inventory[key] = row
	}
	return nil
}

func (s *Simulator) Confirm(tenant, quoteID string, accepted bool, amountMinor int64) (Confirmation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	quote, ok := s.quotes[quoteID]
	if !ok || quote.TenantID != tenant {
		return Confirmation{}, ErrNotFound
	}
	if amountMinor != quote.AmountMinor {
		return Confirmation{}, ErrQuoteAmountMismatch
	}
	if accepted {
		row := s.inventory[inventoryKey(tenant, quote.MerchantID, quote.SKUID, quote.Region)]
		if row.Revision != quote.StockRevision || !row.Eligible || row.OnHand-row.Reserved < 1 {
			s.record(tenant, quote.TaskID, quote.ID, "QUOTE_STALE")
			return Confirmation{}, ErrStaleQuote
		}
	}
	confirmation := Confirmation{
		ID:       StableID("confirmation", tenant, quote.ID, strconv.Itoa(len(s.confirmations)+1)),
		TenantID: tenant, QuoteID: quote.ID, QuoteVersion: quote.Version,
		AmountMinor: amountMinor, Accepted: accepted, CreatedAt: s.now(),
	}
	s.confirmations[confirmation.ID] = confirmation
	action := "BUYER_REJECTED"
	if accepted {
		action = "BUYER_CONFIRMED"
	}
	s.record(tenant, quote.TaskID, confirmation.ID, action)
	return confirmation, nil
}

// Checkout atomically reserves one unit, invokes only the local mock payment
// outcome, and either consumes stock or releases the reservation. The
// confirmation is bound to quote ID/version/amount; a stale quote cannot pay.
func (s *Simulator) Checkout(tenant, confirmationID, idempotencyKey string) (CheckoutResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idempotencyKey == "" {
		return CheckoutResult{}, fmt.Errorf("%w: idempotency key is required", ErrInvalidSimulationData)
	}
	confirmation, ok := s.confirmations[confirmationID]
	if !ok || confirmation.TenantID != tenant {
		return CheckoutResult{}, ErrNotFound
	}
	index := idempotencyIndex(tenant, idempotencyKey)
	if existingID, exists := s.orderByIdempotency[index]; exists {
		order := s.orders[existingID]
		if order.ConfirmationID != confirmationID {
			return CheckoutResult{}, ErrIdempotencyConflict
		}
		return CheckoutResult{Order: order, Payment: s.payments[order.ID]}, nil
	}
	if !confirmation.Accepted {
		return CheckoutResult{}, ErrConfirmationRequired
	}
	quote, ok := s.quotes[confirmation.QuoteID]
	if !ok || quote.TenantID != tenant || quote.Version != confirmation.QuoteVersion || quote.AmountMinor != confirmation.AmountMinor {
		return CheckoutResult{}, ErrStaleQuote
	}
	key := inventoryKey(tenant, quote.MerchantID, quote.SKUID, quote.Region)
	row, ok := s.inventory[key]
	if !ok {
		return CheckoutResult{}, ErrNotFound
	}
	if row.Revision != quote.StockRevision || !row.Eligible || row.OnHand-row.Reserved < 1 {
		s.record(tenant, quote.TaskID, quote.ID, "QUOTE_STALE")
		return CheckoutResult{}, ErrStaleQuote
	}
	row.Reserved++
	s.inventory[key] = row
	order := Order{
		ID: StableID("order", tenant, idempotencyKey), TenantID: tenant,
		TaskID: quote.TaskID, QuoteID: quote.ID, ConfirmationID: confirmation.ID,
		IdempotencyKey: idempotencyKey, Status: "PAYMENT_PENDING", CreatedAt: s.now(),
	}
	s.orders[order.ID] = order
	s.orderByIdempotency[index] = order.ID
	s.record(tenant, quote.TaskID, order.ID, "ORDER_RESERVED")

	payment := MockPayment{
		ID: StableID("mock-payment", tenant, order.ID), TenantID: tenant,
		OrderID: order.ID, AmountMinor: quote.AmountMinor, Currency: quote.Currency,
		Provider: "benchmark-mock", Status: s.paymentOutcome, CreatedAt: s.now(),
	}
	s.payments[order.ID] = payment
	row.Reserved--
	if payment.Status == PaymentSettled {
		row.OnHand--
		row.Revision++
		order.Status = OrderPaid
		s.record(tenant, quote.TaskID, payment.ID, "MOCK_PAYMENT_SETTLED")
		s.record(tenant, quote.TaskID, order.ID, "ORDER_PAID")
	} else {
		order.Status = OrderPaymentError
		s.record(tenant, quote.TaskID, payment.ID, "MOCK_PAYMENT_DECLINED")
		s.record(tenant, quote.TaskID, order.ID, "ORDER_PAYMENT_FAILED")
	}
	order.CompletedAt = s.now()
	s.inventory[key] = row
	s.orders[order.ID] = order
	return CheckoutResult{Order: order, Payment: payment}, nil
}

func (s *Simulator) record(tenant, task, entity, action string) {
	s.audit = append(s.audit, AuditRecord{
		Sequence: int64(len(s.audit) + 1), TenantID: tenant, TaskID: task,
		EntityID: entity, Action: action, At: s.now(),
	})
}

func (s *Simulator) Snapshot() (inventory []Inventory, quotes []Quote, confirmations []Confirmation, orders []Order, payments []MockPayment, audit []AuditRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range s.inventory {
		inventory = append(inventory, row)
	}
	for _, row := range s.quotes {
		quotes = append(quotes, row)
	}
	for _, row := range s.confirmations {
		confirmations = append(confirmations, row)
	}
	for _, row := range s.orders {
		orders = append(orders, row)
	}
	for _, row := range s.payments {
		payments = append(payments, row)
	}
	audit = append(audit, s.audit...)
	return
}
