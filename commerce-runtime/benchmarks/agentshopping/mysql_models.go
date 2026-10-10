package agentshopping

import "time"

const SimulationTenantID = "stablepay-shopping-sim"

const (
	TaskStatusIntentPending          = "INTENT_PENDING"
	TaskStatusIntentParseFailed      = "INTENT_PARSE_FAILED"
	TaskStatusDiscovered             = "DISCOVERED"
	TaskStatusNoMatchingCandidates   = "NO_MATCHING_CANDIDATES"
	TaskStatusQuoted                 = "QUOTED"
	TaskStatusReconfirmationRequired = "RECONFIRMATION_REQUIRED"
	TaskStatusBuyerConfirmed         = "BUYER_CONFIRMED"
	TaskStatusBuyerRejected          = "BUYER_REJECTED"
	TaskStatusPaid                   = "PAID"
	TaskStatusPaymentFailed          = "PAYMENT_FAILED"
)

// These records live only in the dedicated stablepay_shopping_sim database.
// Every business record is explicitly marked synthetic; none maps to the
// production Commerce Runtime ledger or payment_intents tables.
type MerchantRecord struct {
	TenantID   string    `gorm:"primaryKey;size:64"`
	MerchantID string    `gorm:"primaryKey;size:64"`
	Name       string    `gorm:"size:255;not null"`
	Category   string    `gorm:"size:64;not null;index"`
	DID        string    `gorm:"size:128;not null"`
	DataOrigin string    `gorm:"size:32;not null;default:SYNTHETIC"`
	CreatedAt  time.Time `gorm:"not null"`
}

func (MerchantRecord) TableName() string { return "sim_merchants" }

type ProductRecord struct {
	TenantID       string    `gorm:"primaryKey;size:64"`
	SKUID          string    `gorm:"column:sku_id;primaryKey;size:96"`
	MerchantID     string    `gorm:"size:64;not null;index:idx_sim_product_discovery,priority:1"`
	Category       string    `gorm:"size:64;not null;index:idx_sim_product_discovery,priority:2"`
	Item           string    `gorm:"size:160;not null;index:idx_sim_product_discovery,priority:3"`
	Format         string    `gorm:"size:160;not null;index:idx_sim_product_discovery,priority:4"`
	Style          string    `gorm:"size:160;not null;index:idx_sim_product_discovery,priority:5"`
	Title          string    `gorm:"size:512;not null"`
	Description    string    `gorm:"type:text;not null"`
	ImageURL       string    `gorm:"size:1024"`
	BasePrice      int64     `gorm:"not null"`
	Currency       string    `gorm:"size:16;not null"`
	Status         string    `gorm:"size:24;not null;index"`
	TagsJSON       []byte    `gorm:"type:json;not null"`
	AttributesJSON []byte    `gorm:"type:json;not null"`
	DataOrigin     string    `gorm:"size:32;not null;default:SYNTHETIC"`
	CreatedAt      time.Time `gorm:"not null"`
	UpdatedAt      time.Time `gorm:"not null"`
}

func (ProductRecord) TableName() string { return "sim_products" }

type InventoryRecord struct {
	TenantID   string    `gorm:"primaryKey;size:64"`
	MerchantID string    `gorm:"primaryKey;size:64"`
	SKUID      string    `gorm:"column:sku_id;primaryKey;size:96"`
	Region     string    `gorm:"primaryKey;size:16"`
	OnHand     int       `gorm:"not null"`
	Reserved   int       `gorm:"not null;default:0"`
	Eligible   bool      `gorm:"not null;index:idx_sim_inventory_search,priority:2"`
	PriceMinor int64     `gorm:"not null"`
	Currency   string    `gorm:"size:16;not null"`
	Revision   int64     `gorm:"not null"`
	UpdatedAt  time.Time `gorm:"not null"`
	DataOrigin string    `gorm:"size:32;not null;default:SYNTHETIC"`
}

func (InventoryRecord) TableName() string { return "sim_inventory" }

type BuyerRecord struct {
	TenantID        string    `gorm:"primaryKey;size:64"`
	BuyerID         string    `gorm:"primaryKey;size:96"`
	DisplayName     string    `gorm:"size:96;not null"`
	HomeRegion      string    `gorm:"size:16;not null;index"`
	Segment         string    `gorm:"size:32;not null;index"`
	BudgetMinor     int64     `gorm:"not null"`
	PreferencesJSON []byte    `gorm:"type:json;not null"`
	DataOrigin      string    `gorm:"size:32;not null;default:SYNTHETIC"`
	CreatedAt       time.Time `gorm:"not null"`
}

func (BuyerRecord) TableName() string { return "sim_buyers" }

type ShoppingTaskRecord struct {
	TaskID         string    `gorm:"primaryKey;size:96"`
	TenantID       string    `gorm:"size:64;not null;uniqueIndex:uk_sim_task_idem,priority:1;index:idx_sim_task_buyer,priority:1"`
	BuyerID        string    `gorm:"size:96;not null;index:idx_sim_task_buyer,priority:2"`
	IdempotencyKey string    `gorm:"size:128;not null;uniqueIndex:uk_sim_task_idem,priority:2"`
	Prompt         string    `gorm:"type:text;not null"`
	IntentJSON     []byte    `gorm:"type:json;not null"`
	Provider       string    `gorm:"size:64;not null"`
	Model          string    `gorm:"size:128;not null"`
	ParseSource    string    `gorm:"size:32;not null"`
	Status         string    `gorm:"size:32;not null;index"`
	CreatedAt      time.Time `gorm:"not null"`
	DataOrigin     string    `gorm:"size:32;not null;default:SYNTHETIC"`
}

func (ShoppingTaskRecord) TableName() string { return "sim_shopping_tasks" }

type QuoteRecord struct {
	QuoteID        string    `gorm:"primaryKey;size:96"`
	TenantID       string    `gorm:"size:64;not null;uniqueIndex:uk_sim_quote_version,priority:1;uniqueIndex:uk_sim_quote_idem,priority:1;index:idx_sim_quote_task"`
	TaskID         string    `gorm:"size:96;not null;index:idx_sim_quote_task,priority:2"`
	BuyerID        string    `gorm:"size:96;not null;index"`
	MerchantID     string    `gorm:"size:64;not null;uniqueIndex:uk_sim_quote_version,priority:2"`
	SKUID          string    `gorm:"column:sku_id;size:96;not null;uniqueIndex:uk_sim_quote_version,priority:3"`
	Region         string    `gorm:"size:16;not null;uniqueIndex:uk_sim_quote_version,priority:4"`
	Version        int64     `gorm:"not null;uniqueIndex:uk_sim_quote_version,priority:5"`
	AmountMinor    int64     `gorm:"not null"`
	Currency       string    `gorm:"size:16;not null"`
	StockRevision  int64     `gorm:"not null"`
	SnapshotHash   string    `gorm:"size:71;not null"`
	IdempotencyKey string    `gorm:"size:128;not null;uniqueIndex:uk_sim_quote_idem,priority:2"`
	RequestHash    string    `gorm:"size:71;not null"`
	ExpiresAt      time.Time `gorm:"not null;index"`
	CreatedAt      time.Time `gorm:"not null"`
	DataOrigin     string    `gorm:"size:32;not null;default:SYNTHETIC"`
}

func (QuoteRecord) TableName() string { return "sim_quotes" }

type BuyerConfirmationRecord struct {
	ConfirmationID string    `gorm:"primaryKey;size:96"`
	TenantID       string    `gorm:"size:64;not null;uniqueIndex:uk_sim_confirmation_idem,priority:1"`
	BuyerID        string    `gorm:"size:96;not null;index"`
	QuoteID        string    `gorm:"size:96;not null;index"`
	QuoteVersion   int64     `gorm:"not null"`
	QuoteHash      string    `gorm:"size:71;not null"`
	AmountMinor    int64     `gorm:"not null"`
	Accepted       bool      `gorm:"not null"`
	IdempotencyKey string    `gorm:"size:128;not null;uniqueIndex:uk_sim_confirmation_idem,priority:2"`
	RequestHash    string    `gorm:"size:71;not null"`
	ConfirmedAt    time.Time `gorm:"not null"`
	DataOrigin     string    `gorm:"size:32;not null;default:SYNTHETIC"`
}

func (BuyerConfirmationRecord) TableName() string { return "sim_buyer_confirmations" }

type BuyerConfirmationAttemptRecord struct {
	AttemptID      string    `gorm:"primaryKey;size:96"`
	TenantID       string    `gorm:"size:64;not null;uniqueIndex:uk_sim_confirmation_attempt_idem,priority:1"`
	IdempotencyKey string    `gorm:"size:128;not null;uniqueIndex:uk_sim_confirmation_attempt_idem,priority:2"`
	RequestHash    string    `gorm:"size:71;not null"`
	BuyerID        string    `gorm:"size:96;not null;index"`
	QuoteID        string    `gorm:"size:96;not null;index"`
	Status         string    `gorm:"size:32;not null;index"`
	CreatedAt      time.Time `gorm:"not null"`
	DataOrigin     string    `gorm:"size:32;not null;default:SYNTHETIC"`
}

func (BuyerConfirmationAttemptRecord) TableName() string { return "sim_buyer_confirmation_attempts" }

type OrderRecord struct {
	OrderID        string    `gorm:"primaryKey;size:96"`
	TenantID       string    `gorm:"size:64;not null;uniqueIndex:uk_sim_order_idem,priority:1;index:idx_sim_order_buyer"`
	TaskID         string    `gorm:"size:96;not null;index"`
	BuyerID        string    `gorm:"size:96;not null;index:idx_sim_order_buyer,priority:2"`
	QuoteID        string    `gorm:"size:96;not null"`
	ConfirmationID string    `gorm:"size:96;not null;uniqueIndex"`
	IdempotencyKey string    `gorm:"size:128;not null;uniqueIndex:uk_sim_order_idem,priority:2"`
	AmountMinor    int64     `gorm:"not null"`
	Currency       string    `gorm:"size:16;not null"`
	Status         string    `gorm:"size:32;not null;index"`
	CreatedAt      time.Time `gorm:"not null"`
	CompletedAt    time.Time `gorm:"not null"`
	DataOrigin     string    `gorm:"size:32;not null;default:SYNTHETIC"`
}

func (OrderRecord) TableName() string { return "sim_orders" }

type InventoryReservationRecord struct {
	ReservationID string    `gorm:"primaryKey;size:96"`
	TenantID      string    `gorm:"size:64;not null;index"`
	OrderID       string    `gorm:"size:96;not null;uniqueIndex"`
	MerchantID    string    `gorm:"size:64;not null"`
	SKUID         string    `gorm:"column:sku_id;size:96;not null"`
	Region        string    `gorm:"size:16;not null"`
	Quantity      int       `gorm:"not null"`
	Status        string    `gorm:"size:24;not null;index"`
	CreatedAt     time.Time `gorm:"not null"`
	CompletedAt   time.Time `gorm:"not null"`
	DataOrigin    string    `gorm:"size:32;not null;default:SYNTHETIC"`
}

func (InventoryReservationRecord) TableName() string { return "sim_inventory_reservations" }

type InventoryAdjustmentRecord struct {
	AdjustmentID   string    `gorm:"primaryKey;size:96"`
	TenantID       string    `gorm:"size:64;not null;uniqueIndex:uk_sim_inventory_adjustment_idem,priority:1"`
	IdempotencyKey string    `gorm:"size:128;not null;uniqueIndex:uk_sim_inventory_adjustment_idem,priority:2"`
	RequestHash    string    `gorm:"size:71;not null"`
	TaskID         string    `gorm:"size:96;not null;index"`
	MerchantID     string    `gorm:"size:64;not null"`
	SKUID          string    `gorm:"column:sku_id;size:96;not null"`
	Region         string    `gorm:"size:16;not null"`
	Reason         string    `gorm:"size:120;not null"`
	PreviousOnHand int       `gorm:"not null"`
	NewOnHand      int       `gorm:"not null"`
	PreviousPrice  int64     `gorm:"not null"`
	NewPrice       int64     `gorm:"not null"`
	Revision       int64     `gorm:"not null"`
	CreatedAt      time.Time `gorm:"not null"`
	DataOrigin     string    `gorm:"size:32;not null;default:SYNTHETIC"`
}

func (InventoryAdjustmentRecord) TableName() string { return "sim_inventory_adjustments" }

type MockPaymentRecord struct {
	PaymentID           string    `gorm:"primaryKey;size:96"`
	TenantID            string    `gorm:"size:64;not null;index"`
	OrderID             string    `gorm:"size:96;not null;uniqueIndex"`
	AmountMinor         int64     `gorm:"not null"`
	Currency            string    `gorm:"size:16;not null"`
	Provider            string    `gorm:"size:64;not null;default:simulation-only"`
	Status              string    `gorm:"size:24;not null;index"`
	SettlementReference string    `gorm:"size:128"`
	FailureCode         string    `gorm:"size:48"`
	IdempotencyKey      string    `gorm:"size:128;not null;uniqueIndex"`
	CreatedAt           time.Time `gorm:"not null"`
	DataOrigin          string    `gorm:"size:32;not null;default:SYNTHETIC"`
}

func (MockPaymentRecord) TableName() string { return "simulated_payments" }

type SimulationAuditRecord struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement"`
	TenantID   string    `gorm:"size:64;not null;index:idx_sim_audit_task,priority:1"`
	TaskID     string    `gorm:"size:96;not null;index:idx_sim_audit_task,priority:2"`
	EntityID   string    `gorm:"size:96;not null;index"`
	Action     string    `gorm:"size:48;not null"`
	OccurredAt time.Time `gorm:"not null;index"`
	DataOrigin string    `gorm:"size:32;not null;default:SYNTHETIC"`
}

func (SimulationAuditRecord) TableName() string { return "sim_audit_events" }

type IntentParseTraceRecord struct {
	TraceID         string    `gorm:"primaryKey;size:96"`
	TenantID        string    `gorm:"size:64;not null;index"`
	TaskID          string    `gorm:"size:96;not null;index"`
	InputHash       string    `gorm:"size:71;not null"`
	TemplateHash    string    `gorm:"size:71;not null"`
	Provider        string    `gorm:"size:64;not null"`
	Model           string    `gorm:"size:128;not null"`
	Reasoning       string    `gorm:"size:24;not null"`
	UsageStatus     string    `gorm:"size:48;not null;index"`
	UsageFile       string    `gorm:"size:255"`
	InputTokens     int64     `gorm:"not null;default:0"`
	OutputTokens    int64     `gorm:"not null;default:0"`
	AuxInputTokens  int64     `gorm:"not null;default:0"`
	AuxOutputTokens int64     `gorm:"not null;default:0"`
	APICalls        int64     `gorm:"not null;default:0"`
	ModelConfigJSON []byte    `gorm:"type:json;not null"`
	OutputJSON      []byte    `gorm:"type:json;not null"`
	LatencyMS       int64     `gorm:"not null"`
	Succeeded       bool      `gorm:"not null;index"`
	ErrorCode       string    `gorm:"size:48"`
	CreatedAt       time.Time `gorm:"not null"`
	DataOrigin      string    `gorm:"size:32;not null;default:SYNTHETIC"`
}

func (IntentParseTraceRecord) TableName() string { return "sim_intent_parse_traces" }

type DatasetRunRecord struct {
	DatasetID      string    `gorm:"primaryKey;size:96"`
	TenantID       string    `gorm:"size:64;not null;index"`
	Seed           int64     `gorm:"not null"`
	MerchantCount  int       `gorm:"not null"`
	ProductCount   int       `gorm:"not null"`
	InventoryCount int       `gorm:"not null"`
	BuyerCount     int       `gorm:"not null"`
	TaskCount      int       `gorm:"not null"`
	OrderCount     int       `gorm:"not null"`
	PaymentCount   int       `gorm:"not null"`
	SourceManifest string    `gorm:"size:1024;not null"`
	SeededAt       time.Time `gorm:"not null"`
	DataOrigin     string    `gorm:"size:32;not null;default:SYNTHETIC"`
}

func (DatasetRunRecord) TableName() string { return "sim_dataset_runs" }
