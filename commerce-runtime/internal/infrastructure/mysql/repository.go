// Package mysql provides the GORM/MySQL adapter for the commerce runtime.
package mysql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/evidence"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EpisodeModel struct {
	EpisodeID                   string    `gorm:"column:episode_id;type:varchar(128);primaryKey"`
	RequestID                   string    `gorm:"column:request_id;type:varchar(128);not null;uniqueIndex:uk_commerce_episode_request_id"`
	RequesterDID                string    `gorm:"column:requester_did;type:varchar(128);not null"`
	SessionID                   string    `gorm:"column:session_id;type:varchar(128)"`
	State                       string    `gorm:"column:state;type:varchar(32);not null"`
	TerminalReason              string    `gorm:"column:terminal_reason;type:varchar(128)"`
	ContractSnapshotHash        string    `gorm:"column:contract_snapshot_hash;type:char(71);not null"`
	ContractSnapshot            []byte    `gorm:"column:contract_snapshot;type:longtext;not null"`
	SelectedMerchantDID         string    `gorm:"column:selected_merchant_did;type:varchar(128)"`
	SelectedCapabilityID        string    `gorm:"column:selected_capability_id;type:varchar(128)"`
	SelectedCandidateSetID      string    `gorm:"column:selected_candidate_set_id;type:varchar(128)"`
	SelectedCatalogVersion      string    `gorm:"column:selected_catalog_version;type:varchar(64)"`
	SelectedCatalogSnapshotHash string    `gorm:"column:selected_catalog_snapshot_hash;type:char(71)"`
	SelectedCatalogSnapshotRef  string    `gorm:"column:selected_catalog_snapshot_ref;type:varchar(255)"`
	SelectedWorkflowVersion     string    `gorm:"column:selected_workflow_version;type:varchar(64)"`
	CurrentQuoteHash            string    `gorm:"column:current_quote_hash;type:char(71)"`
	EntitlementRefs             []byte    `gorm:"column:entitlement_refs;type:json"`
	DeliveryRefs                []byte    `gorm:"column:delivery_refs;type:json"`
	ValidationEvidenceRefs      []byte    `gorm:"column:validation_evidence_refs;type:json"`
	AttemptedMerchants          []byte    `gorm:"column:attempted_merchants;type:json"`
	ActionCount                 int       `gorm:"column:action_count;not null"`
	PaymentAttemptCount         int       `gorm:"column:payment_attempt_count;not null"`
	DeliveryAttemptCount        int       `gorm:"column:delivery_attempt_count;not null"`
	RetryCount                  int       `gorm:"column:retry_count;not null"`
	DiscoveryGeneration         int       `gorm:"column:discovery_generation;not null;default:0"`
	RecoveryID                  string    `gorm:"column:recovery_id;type:varchar(128)"`
	MaxTotalAttempts            int       `gorm:"column:max_total_attempts;not null"`
	MaxPaymentAttempts          int       `gorm:"column:max_payment_attempts;not null"`
	MaxDeliveryAttempts         int       `gorm:"column:max_delivery_attempts;not null"`
	BudgetCurrency              string    `gorm:"column:budget_currency;type:varchar(16);not null"`
	BudgetLimitMinor            int64     `gorm:"column:budget_limit_minor;not null"`
	ReservedAmount              int64     `gorm:"column:reserved_amount;not null"`
	SettledAmount               int64     `gorm:"column:settled_amount;not null"`
	RefundedAmount              int64     `gorm:"column:refunded_amount;not null"`
	ConsumedAmount              int64     `gorm:"column:consumed_amount;not null"`
	AvailableBudget             int64     `gorm:"column:available_budget;not null"`
	SunkCost                    int64     `gorm:"column:sunk_cost;not null"`
	RefundReusable              bool      `gorm:"column:refund_reusable;not null;default:true"`
	DeadlineAt                  time.Time `gorm:"column:deadline_at;not null"`
	Version                     uint64    `gorm:"column:version;not null"`
	CreatedAt                   time.Time `gorm:"column:created_at;not null"`
	UpdatedAt                   time.Time `gorm:"column:updated_at;not null"`
}

func (EpisodeModel) TableName() string { return "commerce_episodes" }

type EventModel struct {
	EventID            string    `gorm:"column:event_id;type:varchar(128);primaryKey"`
	EpisodeID          string    `gorm:"column:episode_id;type:varchar(128);not null;uniqueIndex:uk_episode_sequence;index:idx_episode_events_episode"`
	Sequence           uint64    `gorm:"column:sequence;not null;uniqueIndex:uk_episode_sequence"`
	OccurredAt         time.Time `gorm:"column:occurred_at;not null"`
	StateBefore        string    `gorm:"column:state_before;type:varchar(32);not null"`
	ActionPayload      []byte    `gorm:"column:action_payload;type:json;not null"`
	ObservationPayload []byte    `gorm:"column:observation_payload;type:json;not null"`
	DecisionPayload    []byte    `gorm:"column:decision_payload;type:json;not null"`
	VerdictPayload     []byte    `gorm:"column:runtime_verdict_payload;type:json;not null"`
	StateAfter         string    `gorm:"column:state_after;type:varchar(32);not null"`
	Actor              string    `gorm:"column:actor;type:varchar(32);not null"`
	TraceID            string    `gorm:"column:trace_id;type:varchar(128)"`
	RuntimeVersion     string    `gorm:"column:runtime_version;type:varchar(64);not null"`
	IdempotencyKey     string    `gorm:"column:idempotency_key;type:varchar(128);not null;uniqueIndex:uk_episode_idempotency"`
}

func (EventModel) TableName() string { return "episode_events" }

type LedgerEntryModel struct {
	EntryID         string    `gorm:"column:entry_id;type:varchar(128);primaryKey"`
	EpisodeID       string    `gorm:"column:episode_id;type:varchar(128);not null;uniqueIndex:uk_ledger_sequence;index:idx_ledger_episode"`
	Sequence        uint64    `gorm:"column:sequence;not null;uniqueIndex:uk_ledger_sequence"`
	Type            string    `gorm:"column:type;type:varchar(40);not null"`
	Currency        string    `gorm:"column:currency;type:varchar(16);not null"`
	AmountMinor     int64     `gorm:"column:amount_minor;not null"`
	PaymentIntentID string    `gorm:"column:payment_intent_id;type:varchar(128);index:idx_ledger_intent"`
	TxID            string    `gorm:"column:tx_id;type:varchar(128);index:idx_ledger_tx"`
	RefundID        string    `gorm:"column:refund_id;type:varchar(128)"`
	IdempotencyKey  string    `gorm:"column:idempotency_key;type:varchar(128);not null;uniqueIndex:uk_ledger_idempotency"`
	OccurredAt      time.Time `gorm:"column:occurred_at;not null"`
	TraceID         string    `gorm:"column:trace_id;type:varchar(128)"`
	ReferenceHash   string    `gorm:"column:reference_hash;type:char(71);not null"`
	MetadataHash    string    `gorm:"column:metadata_hash;type:char(71)"`
}

func (LedgerEntryModel) TableName() string { return "ledger_entries" }

type PaymentIntentModel struct {
	IntentID           string    `gorm:"column:intent_id;type:varchar(128);primaryKey"`
	EpisodeID          string    `gorm:"column:episode_id;type:varchar(128);not null;index:idx_intent_episode"`
	MerchantDID        string    `gorm:"column:merchant_did;type:varchar(128);not null"`
	CapabilityID       string    `gorm:"column:capability_id;type:varchar(128);not null"`
	PayeeDID           string    `gorm:"column:payee_did;type:varchar(128);not null"`
	QuoteHash          string    `gorm:"column:quote_hash;type:char(71);not null"`
	AmountMinor        int64     `gorm:"column:amount_minor;not null"`
	Currency           string    `gorm:"column:currency;type:varchar(16);not null"`
	RequesterDID       string    `gorm:"column:requester_did;type:varchar(128);not null"`
	EpisodeVersion     uint64    `gorm:"column:episode_version;not null"`
	BudgetReservation  int64     `gorm:"column:budget_reservation;not null"`
	IdempotencyKey     string    `gorm:"column:idempotency_key;type:varchar(128);not null;uniqueIndex:uk_intent_idempotency"`
	EconomicKey        string    `gorm:"column:economic_key;type:char(71);not null;uniqueIndex:uk_intent_economic"`
	ExpiresAt          time.Time `gorm:"column:expires_at;not null"`
	Status             string    `gorm:"column:status;type:varchar(24);not null"`
	TxID               string    `gorm:"column:tx_id;type:varchar(128)"`
	TxHash             string    `gorm:"column:tx_hash;type:varchar(128)"`
	AuthorizationRef   string    `gorm:"column:authorization_ref;type:varchar(128)"`
	CredentialRef      string    `gorm:"column:credential_ref;type:varchar(128);not null"`
	RequestFingerprint string    `gorm:"column:request_fingerprint;type:char(71);not null"`
	FailureCode        string    `gorm:"column:failure_code;type:varchar(128)"`
	CreatedAt          time.Time `gorm:"column:created_at;not null"`
	UpdatedAt          time.Time `gorm:"column:updated_at;not null"`
}

func (PaymentIntentModel) TableName() string { return "payment_intents" }

// MerchantCapabilityModel stores one immutable catalog version. The JSON
// columns hold only structured arrays/endpoint records, not prompt text.
type MerchantCapabilityModel struct {
	MerchantDID               string    `gorm:"column:merchant_did;type:varchar(128);primaryKey"`
	CapabilityID              string    `gorm:"column:capability_id;type:varchar(128);primaryKey"`
	CatalogVersion            string    `gorm:"column:catalog_version;type:varchar(64);primaryKey"`
	PayeeDID                  string    `gorm:"column:payee_did;type:varchar(128);not null"`
	Name                      string    `gorm:"column:name;type:varchar(255);not null"`
	Description               string    `gorm:"column:description;type:text;not null"`
	TaskTypes                 []byte    `gorm:"column:task_types;type:json;not null"`
	SemanticTags              []byte    `gorm:"column:semantic_tags;type:json;not null"`
	InvokeEndpoint            []byte    `gorm:"column:invoke_endpoint;type:json;not null"`
	QuoteEndpoint             []byte    `gorm:"column:quote_endpoint;type:json"`
	InputSchemaRef            string    `gorm:"column:input_schema_ref;type:varchar(255);not null"`
	OutputSchemaRef           string    `gorm:"column:output_schema_ref;type:varchar(255);not null"`
	InputContentTypes         []byte    `gorm:"column:input_content_types;type:json;not null"`
	OutputContentTypes        []byte    `gorm:"column:output_content_types;type:json;not null"`
	SupportedProtocolVersions []byte    `gorm:"column:supported_protocol_versions;type:json;not null"`
	SupportedCurrencies       []byte    `gorm:"column:supported_currencies;type:json;not null"`
	PricingModel              string    `gorm:"column:pricing_model;type:varchar(64);not null"`
	PriceHintMinor            *int64    `gorm:"column:price_hint_minor"`
	PriceHintCurrency         string    `gorm:"column:price_hint_currency;type:varchar(16)"`
	Status                    string    `gorm:"column:status;type:varchar(24);not null"`
	Availability              string    `gorm:"column:availability;type:varchar(24);not null"`
	Source                    string    `gorm:"column:source;type:varchar(128);not null"`
	SourceRef                 string    `gorm:"column:source_ref;type:varchar(255)"`
	SourceHash                string    `gorm:"column:source_hash;type:char(71)"`
	ValidFrom                 time.Time `gorm:"column:valid_from;not null"`
	ValidUntil                time.Time `gorm:"column:valid_until;not null"`
	CreatedAt                 time.Time `gorm:"column:created_at;not null"`
	UpdatedAt                 time.Time `gorm:"column:updated_at;not null"`
}

func (MerchantCapabilityModel) TableName() string { return "merchant_capabilities" }

type MerchantCapabilityCurrentModel struct {
	MerchantDID    string    `gorm:"column:merchant_did;type:varchar(128);primaryKey"`
	CapabilityID   string    `gorm:"column:capability_id;type:varchar(128);primaryKey"`
	CatalogVersion string    `gorm:"column:catalog_version;type:varchar(64);not null"`
	UpdatedAt      time.Time `gorm:"column:updated_at;not null"`
}

func (MerchantCapabilityCurrentModel) TableName() string { return "merchant_capability_current" }

type CandidateSetModel struct {
	CandidateSetID      string    `gorm:"column:candidate_set_id;type:varchar(128);primaryKey"`
	EpisodeID           string    `gorm:"column:episode_id;type:varchar(128);not null;index:idx_candidate_set_episode"`
	RequestID           string    `gorm:"column:request_id;type:varchar(128);not null"`
	Generation          int       `gorm:"column:generation;not null;default:0"`
	QueryHash           string    `gorm:"column:query_hash;type:char(71);not null"`
	CatalogSnapshotRefs []byte    `gorm:"column:catalog_snapshot_refs;type:json;not null"`
	Candidates          []byte    `gorm:"column:candidates;type:json;not null"`
	GeneratedAt         time.Time `gorm:"column:generated_at;not null"`
	ExpiresAt           time.Time `gorm:"column:expires_at;not null;index:idx_candidate_set_expiry"`
	FactsRef            string    `gorm:"column:facts_ref;type:varchar(255);not null"`
	PayloadHash         string    `gorm:"column:payload_hash;type:char(71);not null"`
}

func (CandidateSetModel) TableName() string { return "candidate_sets" }

type MerchantInvocationModel struct {
	InvocationID        string     `gorm:"column:invocation_id;type:varchar(128);primaryKey"`
	EpisodeID           string     `gorm:"column:episode_id;type:varchar(128);not null;index:idx_invocation_episode"`
	MerchantDID         string     `gorm:"column:merchant_did;type:varchar(128);not null"`
	CapabilityID        string     `gorm:"column:capability_id;type:varchar(128);not null"`
	CatalogVersion      string     `gorm:"column:catalog_version;type:varchar(64);not null"`
	CatalogSnapshotHash string     `gorm:"column:catalog_snapshot_hash;type:char(71);not null"`
	Phase               string     `gorm:"column:phase;type:varchar(16);not null"`
	Attempt             int        `gorm:"column:attempt;not null"`
	RequestHash         string     `gorm:"column:request_hash;type:char(71);not null"`
	ResponseStatus      int        `gorm:"column:response_status;not null"`
	ResponseContentType string     `gorm:"column:response_content_type;type:varchar(255)"`
	ResponsePayloadHash string     `gorm:"column:response_payload_hash;type:char(71)"`
	ResponseRef         string     `gorm:"column:response_ref;type:varchar(255)"`
	SelectedHeaders     []byte     `gorm:"column:selected_headers;type:json"`
	ResponseBody        []byte     `gorm:"column:response_body;type:longblob"`
	PaymentRequiredRef  string     `gorm:"column:payment_required_ref;type:varchar(255)"`
	EntitlementRef      string     `gorm:"column:entitlement_ref;type:varchar(255)"`
	StartedAt           time.Time  `gorm:"column:started_at;not null"`
	CompletedAt         *time.Time `gorm:"column:completed_at"`
	TraceID             string     `gorm:"column:trace_id;type:varchar(128)"`
	IdempotencyKey      string     `gorm:"column:idempotency_key;type:varchar(255);not null;uniqueIndex:uk_invocation_idempotency"`
}

func (MerchantInvocationModel) TableName() string { return "merchant_invocations" }

type PaymentRequirementFactModel struct {
	PaymentRequirementID string    `gorm:"column:payment_requirement_id;type:varchar(128);primaryKey"`
	EpisodeID            string    `gorm:"column:episode_id;type:varchar(128);not null;index:idx_requirement_episode"`
	InvocationID         string    `gorm:"column:invocation_id;type:varchar(128);not null;uniqueIndex:uk_requirement_invocation"`
	MerchantDID          string    `gorm:"column:merchant_did;type:varchar(128);not null"`
	CapabilityID         string    `gorm:"column:capability_id;type:varchar(128);not null"`
	CatalogVersion       string    `gorm:"column:catalog_version;type:varchar(64);not null"`
	CatalogSnapshotHash  string    `gorm:"column:catalog_snapshot_hash;type:char(71);not null"`
	ProtocolVersion      string    `gorm:"column:protocol_version;type:varchar(32);not null"`
	Scheme               string    `gorm:"column:scheme;type:varchar(32);not null"`
	Network              string    `gorm:"column:network;type:varchar(128);not null"`
	Asset                string    `gorm:"column:asset;type:varchar(128);not null"`
	AtomicAmount         int64     `gorm:"column:atomic_amount;not null"`
	AtomicDecimals       int       `gorm:"column:atomic_decimals;not null"`
	BusinessAmountMinor  int64     `gorm:"column:business_amount_minor;not null"`
	Currency             string    `gorm:"column:currency;type:varchar(16);not null"`
	PayTo                string    `gorm:"column:pay_to;type:varchar(128);not null"`
	PayeeDID             string    `gorm:"column:payee_did;type:varchar(128);not null"`
	ResourceURL          string    `gorm:"column:resource_url;type:varchar(1024);not null"`
	ProductID            string    `gorm:"column:product_id;type:varchar(255)"`
	SkillDID             string    `gorm:"column:skill_did;type:varchar(255)"`
	MaxTimeoutSeconds    int       `gorm:"column:max_timeout_seconds;not null"`
	ObservedAt           time.Time `gorm:"column:observed_at;not null"`
	ExpiresAt            time.Time `gorm:"column:expires_at;not null"`
	RawPayloadHash       string    `gorm:"column:raw_payload_hash;type:char(71);not null"`
	CanonicalQuoteHash   string    `gorm:"column:canonical_quote_hash;type:char(71);not null"`
	FactsRef             string    `gorm:"column:facts_ref;type:varchar(255);not null"`
}

func (PaymentRequirementFactModel) TableName() string { return "payment_requirement_facts" }

type DeliveryArtifactModel struct {
	DeliveryID      string    `gorm:"column:delivery_id;type:varchar(128);primaryKey"`
	EpisodeID       string    `gorm:"column:episode_id;type:varchar(128);not null;index:idx_delivery_episode"`
	InvocationID    string    `gorm:"column:invocation_id;type:varchar(128);not null"`
	MerchantDID     string    `gorm:"column:merchant_did;type:varchar(128);not null"`
	CapabilityID    string    `gorm:"column:capability_id;type:varchar(128);not null"`
	ContentType     string    `gorm:"column:content_type;type:varchar(255)"`
	PayloadRef      string    `gorm:"column:payload_ref;type:varchar(255);not null"`
	PayloadHash     string    `gorm:"column:payload_hash;type:char(71);not null"`
	Body            []byte    `gorm:"column:body;type:longblob"`
	PaymentIntentID string    `gorm:"column:payment_intent_id;type:varchar(128)"`
	EntitlementRef  string    `gorm:"column:entitlement_ref;type:varchar(255)"`
	Attempt         int       `gorm:"column:attempt;not null"`
	HTTPStatus      int       `gorm:"column:http_status;not null"`
	ReceivedAt      time.Time `gorm:"column:received_at;not null"`
}

func (DeliveryArtifactModel) TableName() string { return "delivery_artifacts" }

type ValidationEvidenceModel struct {
	ValidationID     string    `gorm:"column:validation_id;type:varchar(128);primaryKey"`
	EpisodeID        string    `gorm:"column:episode_id;type:varchar(128);not null;index:idx_validation_episode"`
	DeliveryID       string    `gorm:"column:delivery_id;type:varchar(128);not null;uniqueIndex:uk_validation_delivery"`
	ValidatorName    string    `gorm:"column:validator_name;type:varchar(255);not null;uniqueIndex:uk_validation_delivery"`
	ValidatorVersion string    `gorm:"column:validator_version;type:varchar(64);not null;uniqueIndex:uk_validation_delivery"`
	Valid            bool      `gorm:"column:valid;not null"`
	ReasonCode       string    `gorm:"column:reason_code;type:varchar(128);not null"`
	EvidenceRefs     []byte    `gorm:"column:evidence_refs;type:json"`
	PayloadHash      string    `gorm:"column:payload_hash;type:char(71);not null"`
	CreatedAt        time.Time `gorm:"column:created_at;not null"`
}

func (ValidationEvidenceModel) TableName() string { return "validation_evidence" }

type EvidenceRecordModel struct {
	EvidenceID    string     `gorm:"column:evidence_id;type:varchar(128);primaryKey"`
	EvidenceRef   string     `gorm:"column:evidence_ref;type:varchar(255);not null;uniqueIndex:uk_evidence_ref"`
	SourceType    string     `gorm:"column:source_type;type:varchar(64);not null"`
	SourceRef     string     `gorm:"column:source_ref;type:varchar(255);not null"`
	SourceVersion string     `gorm:"column:source_version;type:varchar(128);not null"`
	SourceHash    string     `gorm:"column:source_hash;type:char(71);not null"`
	ContentType   string     `gorm:"column:content_type;type:varchar(128);not null"`
	PayloadHash   string     `gorm:"column:payload_hash;type:char(71);not null"`
	ChunkHash     string     `gorm:"column:chunk_hash;type:char(71);not null"`
	TrustClass    string     `gorm:"column:trust_class;type:varchar(32);not null"`
	Content       string     `gorm:"column:content;type:longtext;not null"`
	CreatedAt     time.Time  `gorm:"column:created_at;not null"`
	ValidUntil    *time.Time `gorm:"column:valid_until"`
}

func (EvidenceRecordModel) TableName() string { return "evidence_records" }

type ModelDecisionTraceModel struct {
	TraceID            string    `gorm:"column:trace_id;type:varchar(255);primaryKey"`
	EpisodeID          string    `gorm:"column:episode_id;type:varchar(128);not null;index:idx_model_trace_episode"`
	Provider           string    `gorm:"column:provider;type:varchar(64);not null"`
	ModelRef           string    `gorm:"column:model_ref;type:varchar(128);not null"`
	ContextHash        string    `gorm:"column:context_hash;type:char(71);not null"`
	EvidenceRefs       []byte    `gorm:"column:evidence_refs;type:json"`
	MemoryRefs         []byte    `gorm:"column:memory_refs;type:json"`
	RequestStartedAt   time.Time `gorm:"column:request_started_at;not null"`
	ResponseReceivedAt time.Time `gorm:"column:response_received_at;not null"`
	RawResponseHash    string    `gorm:"column:raw_response_hash;type:char(71)"`
	ParsedProposalHash string    `gorm:"column:parsed_proposal_hash;type:char(71)"`
	Status             string    `gorm:"column:status;type:varchar(32);not null"`
	ErrorCode          string    `gorm:"column:error_code;type:varchar(64)"`
	InputTokens        int       `gorm:"column:input_tokens;not null;default:0"`
	OutputTokens       int       `gorm:"column:output_tokens;not null;default:0"`
	FallbackReason     string    `gorm:"column:fallback_reason;type:varchar(255)"`
}

func (ModelDecisionTraceModel) TableName() string { return "model_decision_traces" }

type Store struct{ db *gorm.DB }

func Open(dsn string) (*gorm.DB, error) {
	if dsn == "" {
		return nil, errors.New("mysql dsn is required")
	}
	return gorm.Open(mysql.Open(dsn), &gorm.Config{})
}

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

func AutoMigrate(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return errors.New("mysql db is required")
	}
	if err := db.WithContext(ctx).AutoMigrate(&EpisodeModel{}, &EventModel{}, &LedgerEntryModel{}, &PaymentIntentModel{}, &MerchantCapabilityModel{}, &MerchantCapabilityCurrentModel{}, &CandidateSetModel{}, &MerchantInvocationModel{}, &PaymentRequirementFactModel{}, &DeliveryArtifactModel{}, &ValidationEvidenceModel{}, &RecoveryContextModel{}, &ParentApprovalRequestModel{}, &ParentDecisionFactModel{}, &BudgetAmendmentModel{}, &EvidenceRecordModel{}, &ModelDecisionTraceModel{}, &MemoryRecordModel{}, &MemoryObservationModel{}); err != nil {
		return err
	}
	// GORM's generic MySQL time mapping may retain an older DATETIME(3)
	// column created before S7. Memory payload hashes include timestamps, so
	// the durable schema must preserve microseconds for round-trip integrity.
	if err := db.WithContext(ctx).Exec(`
ALTER TABLE memory_records
    MODIFY COLUMN first_observed_at DATETIME(6) NOT NULL,
    MODIFY COLUMN last_observed_at DATETIME(6) NOT NULL,
    MODIFY COLUMN valid_from DATETIME(6) NOT NULL,
    MODIFY COLUMN valid_until DATETIME(6) NULL,
    MODIFY COLUMN created_at DATETIME(6) NOT NULL,
    MODIFY COLUMN updated_at DATETIME(6) NOT NULL`).Error; err != nil {
		return err
	}
	return db.WithContext(ctx).Exec(`ALTER TABLE memory_observations MODIFY COLUMN observed_at DATETIME(6) NOT NULL`).Error
}

func evidenceRecordToModel(value *evidence.EvidenceRecord) (*EvidenceRecordModel, error) {
	if value == nil || value.Validate() != nil {
		return nil, evidence.ErrInvalidRecord
	}
	return &EvidenceRecordModel{EvidenceID: value.EvidenceID, EvidenceRef: value.EvidenceRef, SourceType: string(value.SourceType), SourceRef: value.SourceRef, SourceVersion: value.SourceVersion, SourceHash: value.SourceHash, ContentType: value.ContentType, PayloadHash: value.PayloadHash, ChunkHash: value.ChunkHash, TrustClass: string(value.TrustClass), Content: value.Content, CreatedAt: value.CreatedAt, ValidUntil: value.ValidUntil}, nil
}

func modelToEvidenceRecord(value EvidenceRecordModel) (*evidence.EvidenceRecord, error) {
	record := &evidence.EvidenceRecord{EvidenceID: value.EvidenceID, EvidenceRef: value.EvidenceRef, SourceType: evidence.SourceType(value.SourceType), SourceRef: value.SourceRef, SourceVersion: value.SourceVersion, SourceHash: value.SourceHash, ContentType: value.ContentType, PayloadHash: value.PayloadHash, ChunkHash: value.ChunkHash, TrustClass: evidence.TrustClass(value.TrustClass), Content: value.Content, CreatedAt: value.CreatedAt, ValidUntil: value.ValidUntil}
	if err := record.Validate(); err != nil {
		return nil, err
	}
	return record, nil
}

func modelDecisionTraceToModel(value *llm.ModelDecisionTrace) (*ModelDecisionTraceModel, error) {
	if value == nil || value.Validate() != nil {
		return nil, llm.ErrInvalidModelOutput
	}
	refs, err := json.Marshal(value.EvidenceRefs)
	if err != nil {
		return nil, err
	}
	memoryRefs, err := json.Marshal(value.MemoryRefs)
	if err != nil {
		return nil, err
	}
	return &ModelDecisionTraceModel{TraceID: value.TraceID, EpisodeID: value.EpisodeID, Provider: value.Provider, ModelRef: value.ModelRef, ContextHash: value.ContextHash, EvidenceRefs: refs, MemoryRefs: memoryRefs, RequestStartedAt: value.RequestStartedAt, ResponseReceivedAt: value.ResponseReceivedAt, RawResponseHash: value.RawResponseHash, ParsedProposalHash: value.ParsedProposalHash, Status: string(value.Status), ErrorCode: value.ErrorCode, InputTokens: value.InputTokens, OutputTokens: value.OutputTokens, FallbackReason: value.FallbackReason}, nil
}

func modelToModelDecisionTrace(value ModelDecisionTraceModel) (*llm.ModelDecisionTrace, error) {
	traceValue := &llm.ModelDecisionTrace{TraceID: value.TraceID, EpisodeID: value.EpisodeID, Provider: value.Provider, ModelRef: value.ModelRef, ContextHash: value.ContextHash, RequestStartedAt: value.RequestStartedAt, ResponseReceivedAt: value.ResponseReceivedAt, RawResponseHash: value.RawResponseHash, ParsedProposalHash: value.ParsedProposalHash, Status: llm.TraceStatus(value.Status), ErrorCode: value.ErrorCode, InputTokens: value.InputTokens, OutputTokens: value.OutputTokens, FallbackReason: value.FallbackReason}
	if len(value.EvidenceRefs) > 0 {
		if err := json.Unmarshal(value.EvidenceRefs, &traceValue.EvidenceRefs); err != nil {
			return nil, err
		}
	}
	if len(value.MemoryRefs) > 0 {
		if err := json.Unmarshal(value.MemoryRefs, &traceValue.MemoryRefs); err != nil {
			return nil, err
		}
	}
	if err := traceValue.Validate(); err != nil {
		return nil, err
	}
	return traceValue, nil
}

func (s *Store) SaveEvidenceRecord(ctx context.Context, value *evidence.EvidenceRecord) error {
	model, err := evidenceRecordToModel(value)
	if err != nil {
		return err
	}
	var existing EvidenceRecordModel
	lookup := s.db.WithContext(ctx).Where("evidence_ref = ?", model.EvidenceRef).First(&existing).Error
	if lookup == nil {
		if existing.PayloadHash == model.PayloadHash && existing.ChunkHash == model.ChunkHash {
			return nil
		}
		return repository.ErrFactConflict
	}
	if !errors.Is(lookup, gorm.ErrRecordNotFound) {
		return lookup
	}
	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKey(err) {
			return repository.ErrFactConflict
		}
		return err
	}
	return nil
}

func (s *Store) GetEvidenceRecord(ctx context.Context, ref string) (*evidence.EvidenceRecord, error) {
	var model EvidenceRecordModel
	if err := s.db.WithContext(ctx).Where("evidence_ref = ?", strings.TrimSpace(ref)).First(&model).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, evidence.ErrEvidenceNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToEvidenceRecord(model)
}

func (s *Store) ListEvidenceRecords(ctx context.Context) ([]*evidence.EvidenceRecord, error) {
	var models []EvidenceRecordModel
	if err := s.db.WithContext(ctx).Order("evidence_ref ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	result := make([]*evidence.EvidenceRecord, 0, len(models))
	for _, model := range models {
		value, err := modelToEvidenceRecord(model)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (s *Store) SaveModelDecisionTrace(ctx context.Context, value *llm.ModelDecisionTrace) error {
	model, err := modelDecisionTraceToModel(value)
	if err != nil {
		return err
	}
	var existing ModelDecisionTraceModel
	lookup := s.db.WithContext(ctx).Where("trace_id = ?", model.TraceID).First(&existing).Error
	if lookup == nil {
		previous, decodeErr := modelToModelDecisionTrace(existing)
		if decodeErr == nil && reflect.DeepEqual(previous, value) {
			return nil
		}
		return repository.ErrModelTraceConflict
	}
	if !errors.Is(lookup, gorm.ErrRecordNotFound) {
		return lookup
	}
	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKey(err) {
			return repository.ErrModelTraceConflict
		}
		return err
	}
	return nil
}

func (s *Store) GetModelDecisionTrace(ctx context.Context, id string) (*llm.ModelDecisionTrace, error) {
	var model ModelDecisionTraceModel
	if err := s.db.WithContext(ctx).Where("trace_id = ?", strings.TrimSpace(id)).First(&model).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToModelDecisionTrace(model)
}

func (s *Store) SaveMerchantInvocation(ctx context.Context, value *invocation.MerchantInvocation) error {
	model, err := merchantInvocationToModel(value)
	if err != nil {
		return err
	}
	var existing MerchantInvocationModel
	lookup := s.db.WithContext(ctx).Where("invocation_id = ?", model.InvocationID).First(&existing).Error
	if lookup == nil {
		previous, decodeErr := modelToMerchantInvocation(existing)
		if decodeErr == nil && reflect.DeepEqual(previous, value) {
			return nil
		}
		return repository.ErrFactConflict
	}
	if !errors.Is(lookup, gorm.ErrRecordNotFound) {
		return lookup
	}
	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKey(err) {
			return repository.ErrFactConflict
		}
		return err
	}
	return nil
}

func (s *Store) GetMerchantInvocation(ctx context.Context, id string) (*invocation.MerchantInvocation, error) {
	var row MerchantInvocationModel
	if err := s.db.WithContext(ctx).Where("invocation_id = ?", id).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrFactNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToMerchantInvocation(row)
}

func (s *Store) FindMerchantInvocationByIdempotencyKey(ctx context.Context, episodeID, key string) (*invocation.MerchantInvocation, error) {
	var row MerchantInvocationModel
	if err := s.db.WithContext(ctx).Where("episode_id = ? AND idempotency_key = ?", episodeID, key).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrFactNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToMerchantInvocation(row)
}

func (s *Store) UpdateMerchantInvocation(ctx context.Context, value *invocation.MerchantInvocation) error {
	model, err := merchantInvocationToModel(value)
	if err != nil {
		return err
	}
	var current MerchantInvocationModel
	if err := s.db.WithContext(ctx).Where("invocation_id = ?", model.InvocationID).First(&current).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return repository.ErrFactNotFound
	} else if err != nil {
		return err
	}
	if current.EpisodeID != model.EpisodeID || current.IdempotencyKey != model.IdempotencyKey || current.RequestHash != model.RequestHash {
		return repository.ErrFactConflict
	}
	if err := s.db.WithContext(ctx).Model(&MerchantInvocationModel{}).Where("invocation_id = ?", model.InvocationID).Updates(map[string]any{"response_status": model.ResponseStatus, "response_content_type": model.ResponseContentType, "response_payload_hash": model.ResponsePayloadHash, "response_ref": model.ResponseRef, "selected_headers": model.SelectedHeaders, "response_body": model.ResponseBody, "payment_required_ref": model.PaymentRequiredRef, "entitlement_ref": model.EntitlementRef, "completed_at": model.CompletedAt, "trace_id": model.TraceID}).Error; err != nil {
		return err
	}
	return nil
}

func (s *Store) SavePaymentRequirementFact(ctx context.Context, value *invocation.PaymentRequirementFact) error {
	model, err := paymentRequirementToModel(value)
	if err != nil {
		return err
	}
	var existing PaymentRequirementFactModel
	lookup := s.db.WithContext(ctx).Where("payment_requirement_id = ?", model.PaymentRequirementID).First(&existing).Error
	if lookup == nil {
		previous, decodeErr := modelToPaymentRequirement(existing)
		if decodeErr == nil && reflect.DeepEqual(previous, value) {
			return nil
		}
		return repository.ErrFactConflict
	}
	if !errors.Is(lookup, gorm.ErrRecordNotFound) {
		return lookup
	}
	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKey(err) {
			return repository.ErrFactConflict
		}
		return err
	}
	return nil
}

func (s *Store) GetPaymentRequirementFact(ctx context.Context, id string) (*invocation.PaymentRequirementFact, error) {
	var row PaymentRequirementFactModel
	if err := s.db.WithContext(ctx).Where("payment_requirement_id = ?", id).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrFactNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToPaymentRequirement(row)
}
func (s *Store) FindPaymentRequirementByInvocation(ctx context.Context, episodeID, invocationID string) (*invocation.PaymentRequirementFact, error) {
	var row PaymentRequirementFactModel
	if err := s.db.WithContext(ctx).Where("episode_id = ? AND invocation_id = ?", episodeID, invocationID).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrFactNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToPaymentRequirement(row)
}

func (s *Store) SaveDeliveryArtifact(ctx context.Context, value *invocation.DeliveryArtifact) error {
	model, err := deliveryArtifactToModel(value)
	if err != nil {
		return err
	}
	var existing DeliveryArtifactModel
	lookup := s.db.WithContext(ctx).Where("delivery_id = ?", model.DeliveryID).First(&existing).Error
	if lookup == nil {
		previous, decodeErr := modelToDeliveryArtifact(existing)
		if decodeErr == nil && reflect.DeepEqual(previous, value) {
			return nil
		}
		return repository.ErrFactConflict
	}
	if !errors.Is(lookup, gorm.ErrRecordNotFound) {
		return lookup
	}
	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKey(err) {
			return repository.ErrFactConflict
		}
		return err
	}
	return nil
}
func (s *Store) GetDeliveryArtifact(ctx context.Context, id string) (*invocation.DeliveryArtifact, error) {
	var row DeliveryArtifactModel
	if err := s.db.WithContext(ctx).Where("delivery_id = ?", id).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrFactNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToDeliveryArtifact(row)
}

func (s *Store) SaveValidationEvidence(ctx context.Context, value *invocation.ValidationEvidence) error {
	model, err := validationEvidenceToModel(value)
	if err != nil {
		return err
	}
	var existing ValidationEvidenceModel
	lookup := s.db.WithContext(ctx).Where("validation_id = ?", model.ValidationID).First(&existing).Error
	if lookup == nil {
		previous, decodeErr := modelToValidationEvidence(existing)
		if decodeErr == nil && reflect.DeepEqual(previous, value) {
			return nil
		}
		return repository.ErrFactConflict
	}
	if !errors.Is(lookup, gorm.ErrRecordNotFound) {
		return lookup
	}
	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKey(err) {
			return repository.ErrFactConflict
		}
		return err
	}
	return nil
}
func (s *Store) GetValidationEvidence(ctx context.Context, id string) (*invocation.ValidationEvidence, error) {
	var row ValidationEvidenceModel
	if err := s.db.WithContext(ctx).Where("validation_id = ?", id).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrFactNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToValidationEvidence(row)
}
func (s *Store) FindValidationEvidenceByDelivery(ctx context.Context, deliveryID, name, version string) (*invocation.ValidationEvidence, error) {
	var row ValidationEvidenceModel
	if err := s.db.WithContext(ctx).Where("delivery_id = ? AND validator_name = ? AND validator_version = ?", deliveryID, name, version).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrFactNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToValidationEvidence(row)
}

func (s *Store) Create(ctx context.Context, value *episode.CommerceEpisode) error {
	model, err := episodeToModel(value)
	if err != nil {
		return err
	}
	err = s.db.WithContext(ctx).Create(model).Error
	if isDuplicateKey(err) {
		return repository.ErrRequestIDConflict
	}
	return err
}

func (s *Store) Get(ctx context.Context, episodeID string) (*episode.CommerceEpisode, error) {
	var row EpisodeModel
	err := s.db.WithContext(ctx).Where("episode_id = ?", episodeID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToEpisode(row)
}

func (s *Store) FindByRequestID(ctx context.Context, requestID string) (*episode.CommerceEpisode, error) {
	var row EpisodeModel
	err := s.db.WithContext(ctx).Where("request_id = ?", requestID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToEpisode(row)
}

func (s *Store) SaveCapabilityVersion(ctx context.Context, value *catalog.MerchantCapability) error {
	model, err := merchantCapabilityToModel(value)
	if err != nil {
		return err
	}
	normalized := value.Normalize()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing MerchantCapabilityModel
		lookup := tx.Where("merchant_did = ? AND capability_id = ? AND catalog_version = ?", model.MerchantDID, model.CapabilityID, model.CatalogVersion).First(&existing).Error
		if lookup == nil {
			previous, decodeErr := modelToMerchantCapability(existing)
			if decodeErr == nil {
				previousHash, hashErr := previous.SnapshotHash()
				currentHash, currentErr := normalized.SnapshotHash()
				if hashErr == nil && currentErr == nil && previousHash == currentHash {
					return nil
				}
			}
			return repository.ErrCatalogVersionConflict
		}
		if !errors.Is(lookup, gorm.ErrRecordNotFound) {
			return lookup
		}
		if err := tx.Create(model).Error; err != nil {
			if isDuplicateKey(err) {
				return repository.ErrCatalogVersionConflict
			}
			return err
		}
		var pointer MerchantCapabilityCurrentModel
		pointerErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("merchant_did = ? AND capability_id = ?", model.MerchantDID, model.CapabilityID).First(&pointer).Error
		if errors.Is(pointerErr, gorm.ErrRecordNotFound) {
			return tx.Create(&MerchantCapabilityCurrentModel{MerchantDID: model.MerchantDID, CapabilityID: model.CapabilityID, CatalogVersion: model.CatalogVersion, UpdatedAt: model.UpdatedAt}).Error
		}
		if pointerErr != nil {
			return pointerErr
		}
		if catalog.CompareVersions(model.CatalogVersion, pointer.CatalogVersion) <= 0 {
			return nil
		}
		return tx.Model(&MerchantCapabilityCurrentModel{}).Where("merchant_did = ? AND capability_id = ?", model.MerchantDID, model.CapabilityID).Updates(map[string]any{"catalog_version": model.CatalogVersion, "updated_at": model.UpdatedAt}).Error
	})
}

func (s *Store) GetCapabilityVersion(ctx context.Context, merchantDID, capabilityID, catalogVersion string) (*catalog.MerchantCapability, error) {
	var row MerchantCapabilityModel
	err := s.db.WithContext(ctx).Where("merchant_did = ? AND capability_id = ? AND catalog_version = ?", strings.TrimSpace(merchantDID), strings.ToLower(strings.TrimSpace(capabilityID)), strings.TrimSpace(catalogVersion)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToMerchantCapability(row)
}

func (s *Store) GetCurrentActiveCapability(ctx context.Context, merchantDID, capabilityID string) (*catalog.MerchantCapability, error) {
	var pointer MerchantCapabilityCurrentModel
	err := s.db.WithContext(ctx).Where("merchant_did = ? AND capability_id = ?", strings.TrimSpace(merchantDID), strings.ToLower(strings.TrimSpace(capabilityID))).First(&pointer).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.GetCapabilityVersion(ctx, pointer.MerchantDID, pointer.CapabilityID, pointer.CatalogVersion)
}

func (s *Store) ListActiveCapabilities(ctx context.Context) ([]*catalog.MerchantCapability, error) {
	var pointers []MerchantCapabilityCurrentModel
	if err := s.db.WithContext(ctx).Find(&pointers).Error; err != nil {
		return nil, err
	}
	result := make([]*catalog.MerchantCapability, 0, len(pointers))
	for _, pointer := range pointers {
		var row MerchantCapabilityModel
		err := s.db.WithContext(ctx).Where("merchant_did = ? AND capability_id = ? AND catalog_version = ?", pointer.MerchantDID, pointer.CapabilityID, pointer.CatalogVersion).First(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, repository.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		value, err := modelToMerchantCapability(row)
		if err != nil {
			return nil, err
		}
		if value.Status == catalog.StatusActive && value.Availability == catalog.AvailabilityAvailable {
			result = append(result, value)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].MerchantDID != result[j].MerchantDID {
			return result[i].MerchantDID < result[j].MerchantDID
		}
		return result[i].CapabilityID < result[j].CapabilityID
	})
	return result, nil
}

func (s *Store) SaveCandidateSet(ctx context.Context, value *catalog.CandidateSet) error {
	model, err := candidateSetToModel(value)
	if err != nil {
		return err
	}
	var episodeRow EpisodeModel
	if err := s.db.WithContext(ctx).Where("episode_id = ?", model.EpisodeID).First(&episodeRow).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return repository.ErrNotFound
	} else if err != nil {
		return err
	}
	var existing CandidateSetModel
	lookupErr := s.db.WithContext(ctx).Where("candidate_set_id = ?", model.CandidateSetID).First(&existing).Error
	if lookupErr == nil {
		previous, previousErr := modelToCandidateSet(existing)
		currentHash, currentErr := value.SnapshotHash()
		if previousErr == nil && currentErr == nil {
			previousHash, hashErr := previous.SnapshotHash()
			if hashErr == nil && previousHash == currentHash {
				return nil
			}
		}
		return repository.ErrCandidateSetConflict
	}
	if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return lookupErr
	}
	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKey(err) {
			return repository.ErrCandidateSetConflict
		}
		return err
	}
	return nil
}

func (s *Store) GetCandidateSet(ctx context.Context, candidateSetID string) (*catalog.CandidateSet, error) {
	var row CandidateSetModel
	err := s.db.WithContext(ctx).Where("candidate_set_id = ?", strings.TrimSpace(candidateSetID)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToCandidateSet(row)
}

func (s *Store) UpdateOptimistic(ctx context.Context, episodeID string, expectedVersion uint64, next *episode.CommerceEpisode) error {
	if next == nil {
		return errors.New("episode is required")
	}
	var current EpisodeModel
	err := s.db.WithContext(ctx).Where("episode_id = ?", episodeID).First(&current).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return repository.ErrNotFound
	}
	if err != nil {
		return err
	}
	if current.Version != expectedVersion {
		return repository.ErrVersionConflict
	}
	if current.ContractSnapshotHash != next.ContractSnapshotHash || !bytes.Equal(current.ContractSnapshot, next.ContractSnapshot) {
		return episode.ErrImmutableContract
	}
	updates, err := episodeUpdates(next)
	if err != nil {
		return err
	}
	result := s.db.WithContext(ctx).Model(&EpisodeModel{}).
		Where("episode_id = ? AND version = ?", episodeID, expectedVersion).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return repository.ErrVersionConflict
	}
	return nil
}

func (s *Store) Append(ctx context.Context, value *episode.EpisodeEvent) error {
	model, err := eventToModel(value)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Create(model).Error
}

func (s *Store) ListByEpisode(ctx context.Context, episodeID string) ([]*episode.EpisodeEvent, error) {
	var rows []EventModel
	if err := s.db.WithContext(ctx).Where("episode_id = ?", episodeID).Order("sequence ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*episode.EpisodeEvent, 0, len(rows))
	for _, row := range rows {
		value, err := modelToEvent(row)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (s *Store) FindByIdempotencyKey(ctx context.Context, episodeID, key string) (*episode.EpisodeEvent, error) {
	var row EventModel
	err := s.db.WithContext(ctx).Where("episode_id = ? AND idempotency_key = ?", episodeID, key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToEvent(row)
}

func (s *Store) AppendLedgerEntry(ctx context.Context, value *ledger.LedgerEntry) error {
	model, err := ledgerToModel(value)
	if err != nil {
		return err
	}
	var episodeRow EpisodeModel
	if err := s.db.WithContext(ctx).Where("episode_id = ?", value.EpisodeID).First(&episodeRow).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return repository.ErrNotFound
	} else if err != nil {
		return err
	}
	var existing LedgerEntryModel
	lookupErr := s.db.WithContext(ctx).Where("episode_id = ? AND idempotency_key = ?", value.EpisodeID, value.IdempotencyKey).First(&existing).Error
	if lookupErr == nil {
		previous, decodeErr := modelToLedger(existing)
		if decodeErr == nil && *previous == *value {
			return repository.ErrLedgerIdempotentReplay
		}
		return repository.ErrLedgerIdempotencyConflict
	}
	if !errors.Is(lookupErr, gorm.ErrRecordNotFound) {
		return lookupErr
	}
	var latest LedgerEntryModel
	if err := s.db.WithContext(ctx).Where("episode_id = ?", value.EpisodeID).Order("sequence DESC").First(&latest).Error; err == nil {
		if value.Sequence != latest.Sequence+1 {
			return repository.ErrEventSequenceConflict
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	} else if value.Sequence != 1 {
		return repository.ErrEventSequenceConflict
	}
	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKey(err) {
			return repository.ErrLedgerIdempotencyConflict
		}
		return err
	}
	return nil
}

func (s *Store) ListLedgerEntries(ctx context.Context, episodeID string) ([]*ledger.LedgerEntry, error) {
	var rows []LedgerEntryModel
	if err := s.db.WithContext(ctx).Where("episode_id = ?", episodeID).Order("sequence ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*ledger.LedgerEntry, 0, len(rows))
	for _, row := range rows {
		value, err := modelToLedger(row)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (s *Store) FindLedgerByIdempotencyKey(ctx context.Context, episodeID, key string) (*ledger.LedgerEntry, error) {
	var row LedgerEntryModel
	if err := s.db.WithContext(ctx).Where("episode_id = ? AND idempotency_key = ?", episodeID, key).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToLedger(row)
}

func (s *Store) CreatePaymentIntent(ctx context.Context, value *payment.PaymentIntent) error {
	model, err := paymentIntentToModel(value)
	if err != nil {
		return err
	}
	var episodeRow EpisodeModel
	if err := s.db.WithContext(ctx).Where("episode_id = ?", value.EpisodeID).First(&episodeRow).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return repository.ErrNotFound
	} else if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(model).Error; err != nil {
		if isDuplicateKey(err) {
			return repository.ErrPaymentIntentConflict
		}
		return err
	}
	return nil
}

func (s *Store) GetPaymentIntent(ctx context.Context, intentID string) (*payment.PaymentIntent, error) {
	var row PaymentIntentModel
	if err := s.db.WithContext(ctx).Where("intent_id = ?", intentID).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrPaymentIntentNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToPaymentIntent(row)
}

func (s *Store) FindPaymentIntentByIdempotencyKey(ctx context.Context, episodeID, key string) (*payment.PaymentIntent, error) {
	var row PaymentIntentModel
	if err := s.db.WithContext(ctx).Where("episode_id = ? AND idempotency_key = ?", episodeID, key).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrPaymentIntentNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToPaymentIntent(row)
}

func (s *Store) FindPaymentIntentByEconomicKey(ctx context.Context, episodeID, key string) (*payment.PaymentIntent, error) {
	var row PaymentIntentModel
	if err := s.db.WithContext(ctx).Where("episode_id = ? AND economic_key = ?", episodeID, key).First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrPaymentIntentNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToPaymentIntent(row)
}

func (s *Store) FindPaymentIntentByQuoteHash(ctx context.Context, episodeID, quoteHash string) (*payment.PaymentIntent, error) {
	var row PaymentIntentModel
	if err := s.db.WithContext(ctx).Where("episode_id = ? AND quote_hash = ?", episodeID, quoteHash).Order("created_at ASC").First(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrPaymentIntentNotFound
	} else if err != nil {
		return nil, err
	}
	return modelToPaymentIntent(row)
}

func (s *Store) ListPaymentIntents(ctx context.Context, episodeID string) ([]*payment.PaymentIntent, error) {
	var rows []PaymentIntentModel
	if err := s.db.WithContext(ctx).Where("episode_id = ?", episodeID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*payment.PaymentIntent, 0, len(rows))
	for _, row := range rows {
		value, err := modelToPaymentIntent(row)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (s *Store) UpdatePaymentIntent(ctx context.Context, intentID string, expectedStatus payment.IntentStatus, next *payment.PaymentIntent) error {
	if next == nil || next.IntentID != intentID {
		return payment.ErrInvalidIntent
	}
	if err := next.Validate(); err != nil {
		return err
	}
	var current PaymentIntentModel
	if err := s.db.WithContext(ctx).Where("intent_id = ?", intentID).First(&current).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return repository.ErrPaymentIntentNotFound
	} else if err != nil {
		return err
	}
	previous, err := modelToPaymentIntent(current)
	if err != nil {
		return err
	}
	if expectedStatus != "" && previous.Status != expectedStatus {
		return repository.ErrPaymentIntentStateConflict
	}
	if !sameIntentIdentity(previous, next) {
		return payment.ErrIntentConflict
	}
	query := s.db.WithContext(ctx).Model(&PaymentIntentModel{}).Where("intent_id = ?", intentID)
	if expectedStatus != "" {
		query = query.Where("status = ?", string(expectedStatus))
	}
	result := query.Updates(paymentIntentUpdates(next))
	if result.Error != nil {
		return result.Error
	}
	if expectedStatus != "" && result.RowsAffected != 1 {
		return repository.ErrPaymentIntentStateConflict
	}
	return nil
}

func (s *Store) CommitTransition(ctx context.Context, episodeID string, expectedVersion uint64, next *episode.CommerceEpisode, event *episode.EpisodeEvent) error {
	if next == nil || event == nil || next.EpisodeID != episodeID || event.EpisodeID != episodeID {
		return errors.New("transition records do not refer to the same episode")
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if event.Sequence != expectedVersion {
		return repository.ErrEventSequenceConflict
	}
	updates, err := episodeUpdates(next)
	if err != nil {
		return err
	}
	eventModel, err := eventToModel(event)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing EventModel
		if err := tx.Where("episode_id = ? AND idempotency_key = ?", episodeID, event.Action.IdempotencyKey).First(&existing).Error; err == nil {
			return repository.ErrIdempotentReplay
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var current EpisodeModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("episode_id = ?", episodeID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return repository.ErrNotFound
			}
			return err
		}
		if current.Version != expectedVersion || next.Version != expectedVersion+1 {
			return repository.ErrVersionConflict
		}
		if event.StateBefore != episode.State(current.State) || event.StateAfter != next.State {
			return errors.New("transition event does not match episode projection")
		}
		if current.ContractSnapshotHash != next.ContractSnapshotHash || !bytes.Equal(current.ContractSnapshot, next.ContractSnapshot) {
			return episode.ErrImmutableContract
		}
		result := tx.Model(&EpisodeModel{}).
			Where("episode_id = ? AND version = ?", episodeID, expectedVersion).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return repository.ErrVersionConflict
		}
		if err := tx.Create(eventModel).Error; err != nil {
			return err
		}
		return nil
	})
}

func (s *Store) CommitS4Transition(ctx context.Context, transition repository.S4Transition) error {
	if transition.NextEpisode == nil || transition.Event == nil || transition.EpisodeID == "" || transition.NextEpisode.EpisodeID != transition.EpisodeID || transition.Event.EpisodeID != transition.EpisodeID {
		return errors.New("s4 transition records do not refer to the same episode")
	}
	if err := transition.NextEpisode.Validate(); err != nil {
		return err
	}
	if err := transition.Event.Validate(); err != nil {
		return err
	}
	if transition.ExpectedEpisodeVersion == 0 || transition.Event.Sequence != transition.ExpectedEpisodeVersion || transition.NextEpisode.Version != transition.ExpectedEpisodeVersion+1 {
		return repository.ErrVersionConflict
	}
	if transition.Event.StateAfter != transition.NextEpisode.State {
		return errors.New("s4 event does not match episode projection")
	}
	updates, err := episodeUpdates(transition.NextEpisode)
	if err != nil {
		return err
	}
	eventModel, err := eventToModel(transition.Event)
	if err != nil {
		return err
	}
	var requirementModel *PaymentRequirementFactModel
	if transition.PaymentRequirement != nil {
		if transition.PaymentRequirement.EpisodeID != transition.EpisodeID {
			return invocation.ErrInvalidFact
		}
		requirementModel, err = paymentRequirementToModel(transition.PaymentRequirement)
		if err != nil {
			return err
		}
	}
	var deliveryModel *DeliveryArtifactModel
	if transition.DeliveryArtifact != nil {
		if transition.DeliveryArtifact.EpisodeID != transition.EpisodeID {
			return invocation.ErrInvalidFact
		}
		deliveryModel, err = deliveryArtifactToModel(transition.DeliveryArtifact)
		if err != nil {
			return err
		}
	}
	var validationModel *ValidationEvidenceModel
	if transition.ValidationEvidence != nil {
		if transition.ValidationEvidence.EpisodeID != transition.EpisodeID {
			return invocation.ErrInvalidFact
		}
		validationModel, err = validationEvidenceToModel(transition.ValidationEvidence)
		if err != nil {
			return err
		}
	}
	var recoveryModel *RecoveryContextModel
	if transition.RecoveryContext != nil {
		if transition.RecoveryContext.EpisodeID != transition.EpisodeID {
			return recovery.ErrInvalidRecoveryContext
		}
		if err := transition.RecoveryContext.Validate(); err != nil {
			return err
		}
		recoveryModel, err = recoveryContextToModel(transition.RecoveryContext)
		if err != nil {
			return err
		}
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existingEvent EventModel
		if err := tx.Where("episode_id = ? AND idempotency_key = ?", transition.EpisodeID, transition.Event.Action.IdempotencyKey).First(&existingEvent).Error; err == nil {
			return repository.ErrIdempotentReplay
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var current EpisodeModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("episode_id = ?", transition.EpisodeID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return repository.ErrNotFound
			}
			return err
		}
		if current.Version != transition.ExpectedEpisodeVersion {
			return repository.ErrVersionConflict
		}
		if episode.State(current.State) != transition.Event.StateBefore {
			return errors.New("s4 event state does not match current episode")
		}
		if current.ContractSnapshotHash != transition.NextEpisode.ContractSnapshotHash || !bytes.Equal(current.ContractSnapshot, transition.NextEpisode.ContractSnapshot) {
			return episode.ErrImmutableContract
		}
		if err := s.checkS4Requirement(tx, requirementModel, transition.PaymentRequirement); err != nil {
			return err
		}
		if err := s.checkS4Delivery(tx, deliveryModel, transition.DeliveryArtifact); err != nil {
			return err
		}
		if err := s.checkS4Validation(tx, validationModel, transition.ValidationEvidence); err != nil {
			return err
		}
		result := tx.Model(&EpisodeModel{}).Where("episode_id = ? AND version = ?", transition.EpisodeID, transition.ExpectedEpisodeVersion).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return repository.ErrVersionConflict
		}
		if requirementModel != nil {
			var existing PaymentRequirementFactModel
			if err := tx.Where("payment_requirement_id = ?", requirementModel.PaymentRequirementID).First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				if err := tx.Create(requirementModel).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		if deliveryModel != nil {
			var existing DeliveryArtifactModel
			if err := tx.Where("delivery_id = ?", deliveryModel.DeliveryID).First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				if err := tx.Create(deliveryModel).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		if validationModel != nil {
			var existing ValidationEvidenceModel
			if err := tx.Where("validation_id = ?", validationModel.ValidationID).First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				if err := tx.Create(validationModel).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		if recoveryModel != nil {
			var existing RecoveryContextModel
			if err := tx.Where("recovery_id = ?", recoveryModel.RecoveryID).First(&existing).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				if err := tx.Create(recoveryModel).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if existing.EpisodeID != recoveryModel.EpisodeID {
				return repository.ErrRecoveryConflict
			} else if err := tx.Model(&RecoveryContextModel{}).Where("recovery_id = ?", recoveryModel.RecoveryID).Updates(recoveryModel).Error; err != nil {
				return err
			}
		}
		return tx.Create(eventModel).Error
	})
}

func (s *Store) checkS4Requirement(tx *gorm.DB, model *PaymentRequirementFactModel, value *invocation.PaymentRequirementFact) error {
	if model == nil {
		return nil
	}
	var existing PaymentRequirementFactModel
	err := tx.Where("payment_requirement_id = ? OR (episode_id = ? AND invocation_id = ?)", model.PaymentRequirementID, model.EpisodeID, model.InvocationID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	previous, decodeErr := modelToPaymentRequirement(existing)
	if decodeErr == nil && reflect.DeepEqual(previous, value) {
		return nil
	}
	return repository.ErrFactConflict
}

func (s *Store) checkS4Delivery(tx *gorm.DB, model *DeliveryArtifactModel, value *invocation.DeliveryArtifact) error {
	if model == nil {
		return nil
	}
	var existing DeliveryArtifactModel
	err := tx.Where("delivery_id = ?", model.DeliveryID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	previous, decodeErr := modelToDeliveryArtifact(existing)
	if decodeErr == nil && reflect.DeepEqual(previous, value) {
		return nil
	}
	return repository.ErrFactConflict
}

func (s *Store) checkS4Validation(tx *gorm.DB, model *ValidationEvidenceModel, value *invocation.ValidationEvidence) error {
	if model == nil {
		return nil
	}
	var existing ValidationEvidenceModel
	err := tx.Where("validation_id = ? OR (delivery_id = ? AND validator_name = ? AND validator_version = ?)", model.ValidationID, model.DeliveryID, model.ValidatorName, model.ValidatorVersion).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	previous, decodeErr := modelToValidationEvidence(existing)
	if decodeErr == nil && reflect.DeepEqual(previous, value) {
		return nil
	}
	return repository.ErrFactConflict
}

func (s *Store) CommitFinanceTransition(ctx context.Context, transition repository.FinanceTransition) error {
	if transition.NextEpisode == nil || transition.Event == nil || transition.NextEpisode.EpisodeID != transition.EpisodeID || transition.Event.EpisodeID != transition.EpisodeID {
		return errors.New("finance transition records do not refer to the same episode")
	}
	if err := transition.NextEpisode.Validate(); err != nil {
		return err
	}
	if err := transition.Event.Validate(); err != nil {
		return err
	}
	if transition.ExpectedEpisodeVersion == 0 || transition.Event.Sequence != transition.ExpectedEpisodeVersion || transition.NextEpisode.Version != transition.ExpectedEpisodeVersion+1 {
		return repository.ErrVersionConflict
	}
	for _, entry := range transition.LedgerEntries {
		if entry == nil {
			return ledger.ErrInvalidEntry
		}
		if entry.EpisodeID != transition.EpisodeID {
			return ledger.ErrInvalidEntry
		}
		if err := entry.Validate(); err != nil {
			return err
		}
	}
	if transition.IntentCreate != nil && transition.IntentUpdate != nil {
		return errors.New("finance transition cannot create and update an intent together")
	}
	if transition.IntentCreate != nil && transition.IntentCreate.EpisodeID != transition.EpisodeID {
		return payment.ErrIntentConflict
	}
	eventModel, err := eventToModel(transition.Event)
	if err != nil {
		return err
	}
	ledgerModels := make([]*LedgerEntryModel, 0, len(transition.LedgerEntries))
	for _, entry := range transition.LedgerEntries {
		model, err := ledgerToModel(entry)
		if err != nil {
			return err
		}
		ledgerModels = append(ledgerModels, model)
	}
	var intentCreateModel *PaymentIntentModel
	if transition.IntentCreate != nil {
		intentCreateModel, err = paymentIntentToModel(transition.IntentCreate)
		if err != nil {
			return err
		}
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current EpisodeModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("episode_id = ?", transition.EpisodeID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return repository.ErrNotFound
			}
			return err
		}
		if current.Version != transition.ExpectedEpisodeVersion {
			return repository.ErrVersionConflict
		}
		if episode.State(current.State) != transition.Event.StateBefore || transition.NextEpisode.State != transition.Event.StateAfter {
			return errors.New("finance event does not match episode projection")
		}
		if current.ContractSnapshotHash != transition.NextEpisode.ContractSnapshotHash || !bytes.Equal(current.ContractSnapshot, transition.NextEpisode.ContractSnapshot) {
			return episode.ErrImmutableContract
		}
		var existingEvent EventModel
		if err := tx.Where("episode_id = ? AND idempotency_key = ?", transition.EpisodeID, transition.Event.Action.IdempotencyKey).First(&existingEvent).Error; err == nil {
			return repository.ErrIdempotentReplay
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var latest LedgerEntryModel
		ledgerSequence := uint64(1)
		if err := tx.Where("episode_id = ?", transition.EpisodeID).Order("sequence DESC").First(&latest).Error; err == nil {
			ledgerSequence = latest.Sequence + 1
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		for index, model := range ledgerModels {
			if model.Sequence != ledgerSequence+uint64(index) {
				return repository.ErrEventSequenceConflict
			}
			var existingLedger LedgerEntryModel
			if err := tx.Where("episode_id = ? AND (idempotency_key = ? OR entry_id = ?)", transition.EpisodeID, model.IdempotencyKey, model.EntryID).First(&existingLedger).Error; err == nil {
				previous, decodeErr := modelToLedger(existingLedger)
				candidate, candidateErr := modelToLedger(*model)
				if decodeErr == nil && candidateErr == nil && *previous == *candidate {
					return repository.ErrLedgerIdempotentReplay
				}
				return repository.ErrLedgerIdempotencyConflict
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		var existingLedgerRows []LedgerEntryModel
		if err := tx.Where("episode_id = ?", transition.EpisodeID).Order("sequence ASC").Find(&existingLedgerRows).Error; err != nil {
			return err
		}
		allLedgerEntries := make([]*ledger.LedgerEntry, 0, len(existingLedgerRows)+len(ledgerModels))
		for _, row := range existingLedgerRows {
			entry, err := modelToLedger(row)
			if err != nil {
				return err
			}
			allLedgerEntries = append(allLedgerEntries, entry)
		}
		for _, model := range ledgerModels {
			entry, err := modelToLedger(*model)
			if err != nil {
				return err
			}
			allLedgerEntries = append(allLedgerEntries, entry)
		}
		projection, err := ledger.BuildProjection(current.BudgetCurrency, current.BudgetLimitMinor, current.RefundReusable, allLedgerEntries)
		if err != nil {
			return err
		}
		if projection.ToEpisodeBudget() != transition.NextEpisode.Budget {
			return ledger.ErrProjectionInvariant
		}
		if transition.IntentCreate != nil {
			if err := tx.Where("intent_id = ? OR (episode_id = ? AND idempotency_key = ?) OR (episode_id = ? AND economic_key = ?)", intentCreateModel.IntentID, transition.EpisodeID, intentCreateModel.IdempotencyKey, transition.EpisodeID, intentCreateModel.EconomicKey).First(&PaymentIntentModel{}).Error; err == nil {
				return repository.ErrPaymentIntentConflict
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if transition.IntentUpdate != nil {
			var currentIntent PaymentIntentModel
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("intent_id = ?", transition.IntentUpdate.IntentID).First(&currentIntent).Error; errors.Is(err, gorm.ErrRecordNotFound) {
				return repository.ErrPaymentIntentNotFound
			} else if err != nil {
				return err
			}
			previous, err := modelToPaymentIntent(currentIntent)
			if err != nil {
				return err
			}
			if transition.ExpectedIntentStatus != "" && previous.Status != transition.ExpectedIntentStatus {
				return repository.ErrPaymentIntentStateConflict
			}
			if !sameIntentIdentity(previous, transition.IntentUpdate) {
				return payment.ErrIntentConflict
			}
		}
		updates, err := episodeUpdates(transition.NextEpisode)
		if err != nil {
			return err
		}
		if result := tx.Model(&EpisodeModel{}).Where("episode_id = ? AND version = ?", transition.EpisodeID, transition.ExpectedEpisodeVersion).Updates(updates); result.Error != nil {
			return result.Error
		} else if result.RowsAffected != 1 {
			return repository.ErrVersionConflict
		}
		if err := tx.Create(eventModel).Error; err != nil {
			return err
		}
		for _, model := range ledgerModels {
			if err := tx.Create(model).Error; err != nil {
				if isDuplicateKey(err) {
					return repository.ErrLedgerIdempotencyConflict
				}
				return err
			}
		}
		if intentCreateModel != nil {
			if err := tx.Create(intentCreateModel).Error; err != nil {
				if isDuplicateKey(err) {
					return repository.ErrPaymentIntentConflict
				}
				return err
			}
		}
		if transition.IntentUpdate != nil {
			query := tx.Model(&PaymentIntentModel{}).Where("intent_id = ?", transition.IntentUpdate.IntentID)
			if transition.ExpectedIntentStatus != "" {
				query = query.Where("status = ?", string(transition.ExpectedIntentStatus))
			}
			if result := query.Updates(paymentIntentUpdates(transition.IntentUpdate)); result.Error != nil {
				return result.Error
			} else if result.RowsAffected != 1 {
				return repository.ErrPaymentIntentStateConflict
			}
		}
		return nil
	})
}

func merchantCapabilityToModel(value *catalog.MerchantCapability) (*MerchantCapabilityModel, error) {
	if value == nil {
		return nil, catalog.ErrInvalidCapability
	}
	normalized := value.Normalize()
	if err := normalized.Validate(); err != nil {
		return nil, err
	}
	encode := func(input any) ([]byte, error) { return json.Marshal(input) }
	taskTypes, err := encode(normalized.TaskTypes)
	if err != nil {
		return nil, err
	}
	semanticTags, err := encode(normalized.SemanticTags)
	if err != nil {
		return nil, err
	}
	invokeEndpoint, err := encode(normalized.InvokeEndpoint)
	if err != nil {
		return nil, err
	}
	quoteEndpoint, err := encode(normalized.QuoteEndpoint)
	if err != nil {
		return nil, err
	}
	inputTypes, err := encode(normalized.InputContentTypes)
	if err != nil {
		return nil, err
	}
	outputTypes, err := encode(normalized.OutputContentTypes)
	if err != nil {
		return nil, err
	}
	protocols, err := encode(normalized.SupportedProtocolVersions)
	if err != nil {
		return nil, err
	}
	currencies, err := encode(normalized.SupportedCurrencies)
	if err != nil {
		return nil, err
	}
	return &MerchantCapabilityModel{MerchantDID: normalized.MerchantDID, CapabilityID: normalized.CapabilityID, CatalogVersion: normalized.CatalogVersion,
		PayeeDID: normalized.PayeeDID, Name: normalized.Name, Description: normalized.Description, TaskTypes: taskTypes, SemanticTags: semanticTags,
		InvokeEndpoint: invokeEndpoint, QuoteEndpoint: quoteEndpoint, InputSchemaRef: normalized.InputSchemaRef, OutputSchemaRef: normalized.OutputSchemaRef,
		InputContentTypes: inputTypes, OutputContentTypes: outputTypes, SupportedProtocolVersions: protocols, SupportedCurrencies: currencies,
		PricingModel: normalized.PricingModel, PriceHintMinor: normalized.PriceHintMinor, PriceHintCurrency: normalized.PriceHintCurrency,
		Status: string(normalized.Status), Availability: string(normalized.Availability), Source: normalized.Source, SourceRef: normalized.SourceRef, SourceHash: normalized.SourceHash,
		ValidFrom: normalized.ValidFrom, ValidUntil: normalized.ValidUntil, CreatedAt: normalized.CreatedAt, UpdatedAt: normalized.UpdatedAt}, nil
}

func modelToMerchantCapability(row MerchantCapabilityModel) (*catalog.MerchantCapability, error) {
	decode := func(input []byte, target any) error {
		if len(input) == 0 {
			return nil
		}
		return json.Unmarshal(input, target)
	}
	value := &catalog.MerchantCapability{MerchantDID: row.MerchantDID, CapabilityID: row.CapabilityID, CatalogVersion: row.CatalogVersion, PayeeDID: row.PayeeDID,
		Name: row.Name, Description: row.Description, InputSchemaRef: row.InputSchemaRef, OutputSchemaRef: row.OutputSchemaRef, PricingModel: row.PricingModel,
		PriceHintMinor: row.PriceHintMinor, PriceHintCurrency: row.PriceHintCurrency, Status: catalog.CapabilityStatus(row.Status), Availability: catalog.Availability(row.Availability),
		Source: row.Source, SourceRef: row.SourceRef, SourceHash: row.SourceHash, ValidFrom: row.ValidFrom, ValidUntil: row.ValidUntil, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	if err := decode(row.TaskTypes, &value.TaskTypes); err != nil {
		return nil, err
	}
	if err := decode(row.SemanticTags, &value.SemanticTags); err != nil {
		return nil, err
	}
	if err := decode(row.InvokeEndpoint, &value.InvokeEndpoint); err != nil {
		return nil, err
	}
	if err := decode(row.QuoteEndpoint, &value.QuoteEndpoint); err != nil {
		return nil, err
	}
	if err := decode(row.InputContentTypes, &value.InputContentTypes); err != nil {
		return nil, err
	}
	if err := decode(row.OutputContentTypes, &value.OutputContentTypes); err != nil {
		return nil, err
	}
	if err := decode(row.SupportedProtocolVersions, &value.SupportedProtocolVersions); err != nil {
		return nil, err
	}
	if err := decode(row.SupportedCurrencies, &value.SupportedCurrencies); err != nil {
		return nil, err
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func candidateSetToModel(value *catalog.CandidateSet) (*CandidateSetModel, error) {
	if value == nil {
		return nil, catalog.ErrInvalidCandidateSet
	}
	normalized := value.Normalize()
	if err := normalized.Validate(); err != nil {
		return nil, err
	}
	refs, err := json.Marshal(normalized.CatalogSnapshotRefs)
	if err != nil {
		return nil, err
	}
	candidates, err := json.Marshal(normalized.Candidates)
	if err != nil {
		return nil, err
	}
	return &CandidateSetModel{CandidateSetID: normalized.CandidateSetID, EpisodeID: normalized.EpisodeID, RequestID: normalized.RequestID, Generation: normalized.Generation, QueryHash: normalized.QueryHash,
		CatalogSnapshotRefs: refs, Candidates: candidates, GeneratedAt: normalized.GeneratedAt, ExpiresAt: normalized.ExpiresAt, FactsRef: normalized.FactsRef, PayloadHash: normalized.PayloadHash}, nil
}

func modelToCandidateSet(row CandidateSetModel) (*catalog.CandidateSet, error) {
	value := &catalog.CandidateSet{CandidateSetID: row.CandidateSetID, EpisodeID: row.EpisodeID, RequestID: row.RequestID, Generation: row.Generation, QueryHash: row.QueryHash,
		GeneratedAt: row.GeneratedAt, ExpiresAt: row.ExpiresAt, FactsRef: row.FactsRef, PayloadHash: row.PayloadHash}
	if len(row.CatalogSnapshotRefs) > 0 {
		if err := json.Unmarshal(row.CatalogSnapshotRefs, &value.CatalogSnapshotRefs); err != nil {
			return nil, err
		}
	}
	if len(row.Candidates) > 0 {
		if err := json.Unmarshal(row.Candidates, &value.Candidates); err != nil {
			return nil, err
		}
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func merchantInvocationToModel(value *invocation.MerchantInvocation) (*MerchantInvocationModel, error) {
	if value == nil {
		return nil, invocation.ErrInvalidFact
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	headers, err := json.Marshal(value.SelectedHeaders)
	if err != nil {
		return nil, err
	}
	var completed *time.Time
	if !value.CompletedAt.IsZero() {
		timestamp := value.CompletedAt
		completed = &timestamp
	}
	return &MerchantInvocationModel{InvocationID: value.InvocationID, EpisodeID: value.EpisodeID, MerchantDID: value.MerchantDID, CapabilityID: value.CapabilityID, CatalogVersion: value.CatalogVersion, CatalogSnapshotHash: value.CatalogSnapshotHash, Phase: string(value.Phase), Attempt: value.Attempt, RequestHash: value.RequestHash, ResponseStatus: value.ResponseStatus, ResponseContentType: value.ResponseContentType, ResponsePayloadHash: value.ResponsePayloadHash, ResponseRef: value.ResponseRef, SelectedHeaders: headers, ResponseBody: append([]byte(nil), value.ResponseBody...), PaymentRequiredRef: value.PaymentRequiredRef, EntitlementRef: value.EntitlementRef, StartedAt: value.StartedAt, CompletedAt: completed, TraceID: value.TraceID, IdempotencyKey: value.IdempotencyKey}, nil
}

func modelToMerchantInvocation(row MerchantInvocationModel) (*invocation.MerchantInvocation, error) {
	value := &invocation.MerchantInvocation{InvocationID: row.InvocationID, EpisodeID: row.EpisodeID, MerchantDID: row.MerchantDID, CapabilityID: row.CapabilityID, CatalogVersion: row.CatalogVersion, CatalogSnapshotHash: row.CatalogSnapshotHash, Phase: invocation.Phase(row.Phase), Attempt: row.Attempt, RequestHash: row.RequestHash, ResponseStatus: row.ResponseStatus, ResponseContentType: row.ResponseContentType, ResponsePayloadHash: row.ResponsePayloadHash, ResponseRef: row.ResponseRef, ResponseBody: append([]byte(nil), row.ResponseBody...), PaymentRequiredRef: row.PaymentRequiredRef, EntitlementRef: row.EntitlementRef, StartedAt: row.StartedAt, TraceID: row.TraceID, IdempotencyKey: row.IdempotencyKey}
	if row.CompletedAt != nil {
		value.CompletedAt = *row.CompletedAt
	}
	if len(row.SelectedHeaders) > 0 {
		if err := json.Unmarshal(row.SelectedHeaders, &value.SelectedHeaders); err != nil {
			return nil, err
		}
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func paymentRequirementToModel(value *invocation.PaymentRequirementFact) (*PaymentRequirementFactModel, error) {
	if value == nil {
		return nil, invocation.ErrInvalidFact
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return &PaymentRequirementFactModel{PaymentRequirementID: value.PaymentRequirementID, EpisodeID: value.EpisodeID, InvocationID: value.InvocationID, MerchantDID: value.MerchantDID, CapabilityID: value.CapabilityID, CatalogVersion: value.CatalogVersion, CatalogSnapshotHash: value.CatalogSnapshotHash, ProtocolVersion: value.ProtocolVersion, Scheme: value.Scheme, Network: value.Network, Asset: value.Asset, AtomicAmount: value.AtomicAmount, AtomicDecimals: value.AtomicDecimals, BusinessAmountMinor: value.BusinessAmountMinor, Currency: value.Currency, PayTo: value.PayTo, PayeeDID: value.PayeeDID, ResourceURL: value.ResourceURL, ProductID: value.ProductID, SkillDID: value.SkillDID, MaxTimeoutSeconds: value.MaxTimeoutSeconds, ObservedAt: value.ObservedAt, ExpiresAt: value.ExpiresAt, RawPayloadHash: value.RawPayloadHash, CanonicalQuoteHash: value.CanonicalQuoteHash, FactsRef: value.FactsRef}, nil
}

func modelToPaymentRequirement(row PaymentRequirementFactModel) (*invocation.PaymentRequirementFact, error) {
	value := &invocation.PaymentRequirementFact{PaymentRequirementID: row.PaymentRequirementID, EpisodeID: row.EpisodeID, InvocationID: row.InvocationID, MerchantDID: row.MerchantDID, CapabilityID: row.CapabilityID, CatalogVersion: row.CatalogVersion, CatalogSnapshotHash: row.CatalogSnapshotHash, ProtocolVersion: row.ProtocolVersion, Scheme: row.Scheme, Network: row.Network, Asset: row.Asset, AtomicAmount: row.AtomicAmount, AtomicDecimals: row.AtomicDecimals, BusinessAmountMinor: row.BusinessAmountMinor, Currency: row.Currency, PayTo: row.PayTo, PayeeDID: row.PayeeDID, ResourceURL: row.ResourceURL, ProductID: row.ProductID, SkillDID: row.SkillDID, MaxTimeoutSeconds: row.MaxTimeoutSeconds, ObservedAt: row.ObservedAt, ExpiresAt: row.ExpiresAt, RawPayloadHash: row.RawPayloadHash, CanonicalQuoteHash: row.CanonicalQuoteHash, FactsRef: row.FactsRef}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func deliveryArtifactToModel(value *invocation.DeliveryArtifact) (*DeliveryArtifactModel, error) {
	if value == nil {
		return nil, invocation.ErrInvalidFact
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return &DeliveryArtifactModel{DeliveryID: value.DeliveryID, EpisodeID: value.EpisodeID, InvocationID: value.InvocationID, MerchantDID: value.MerchantDID, CapabilityID: value.CapabilityID, ContentType: value.ContentType, PayloadRef: value.PayloadRef, PayloadHash: value.PayloadHash, Body: append([]byte(nil), value.Body...), PaymentIntentID: value.PaymentIntentID, EntitlementRef: value.EntitlementRef, Attempt: value.Attempt, HTTPStatus: value.HTTPStatus, ReceivedAt: value.ReceivedAt}, nil
}
func modelToDeliveryArtifact(row DeliveryArtifactModel) (*invocation.DeliveryArtifact, error) {
	value := &invocation.DeliveryArtifact{DeliveryID: row.DeliveryID, EpisodeID: row.EpisodeID, InvocationID: row.InvocationID, MerchantDID: row.MerchantDID, CapabilityID: row.CapabilityID, ContentType: row.ContentType, PayloadRef: row.PayloadRef, PayloadHash: row.PayloadHash, Body: append([]byte(nil), row.Body...), PaymentIntentID: row.PaymentIntentID, EntitlementRef: row.EntitlementRef, Attempt: row.Attempt, HTTPStatus: row.HTTPStatus, ReceivedAt: row.ReceivedAt}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func validationEvidenceToModel(value *invocation.ValidationEvidence) (*ValidationEvidenceModel, error) {
	if value == nil {
		return nil, invocation.ErrInvalidFact
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	refs, err := json.Marshal(value.EvidenceRefs)
	if err != nil {
		return nil, err
	}
	return &ValidationEvidenceModel{ValidationID: value.ValidationID, EpisodeID: value.EpisodeID, DeliveryID: value.DeliveryID, ValidatorName: value.ValidatorName, ValidatorVersion: value.ValidatorVersion, Valid: value.Valid, ReasonCode: value.ReasonCode, EvidenceRefs: refs, PayloadHash: value.PayloadHash, CreatedAt: value.CreatedAt}, nil
}
func modelToValidationEvidence(row ValidationEvidenceModel) (*invocation.ValidationEvidence, error) {
	value := &invocation.ValidationEvidence{ValidationID: row.ValidationID, EpisodeID: row.EpisodeID, DeliveryID: row.DeliveryID, ValidatorName: row.ValidatorName, ValidatorVersion: row.ValidatorVersion, Valid: row.Valid, ReasonCode: row.ReasonCode, PayloadHash: row.PayloadHash, CreatedAt: row.CreatedAt}
	if len(row.EvidenceRefs) > 0 {
		if err := json.Unmarshal(row.EvidenceRefs, &value.EvidenceRefs); err != nil {
			return nil, err
		}
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func episodeToModel(value *episode.CommerceEpisode) (*EpisodeModel, error) {
	if value == nil {
		return nil, errors.New("episode is required")
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	encode := func(values []string) ([]byte, error) {
		if values == nil {
			values = []string{}
		}
		return json.Marshal(values)
	}
	entitlement, err := encode(value.EntitlementRefs)
	if err != nil {
		return nil, err
	}
	delivery, err := encode(value.DeliveryRefs)
	if err != nil {
		return nil, err
	}
	evidence, err := encode(value.ValidationEvidenceRefs)
	if err != nil {
		return nil, err
	}
	attempted, err := encode(value.AttemptedMerchants)
	if err != nil {
		return nil, err
	}
	return &EpisodeModel{
		EpisodeID: value.EpisodeID, RequestID: value.RequestID, RequesterDID: value.RequesterDID, SessionID: value.SessionID,
		State: string(value.State), TerminalReason: value.TerminalReason,
		ContractSnapshotHash: value.ContractSnapshotHash, ContractSnapshot: append([]byte(nil), value.ContractSnapshot...),
		SelectedMerchantDID: value.SelectedMerchantDID, SelectedCapabilityID: value.SelectedCapabilityID, SelectedCandidateSetID: value.SelectedCandidateSetID,
		SelectedCatalogVersion: value.SelectedCatalogVersion, SelectedCatalogSnapshotHash: value.SelectedCatalogSnapshotHash, SelectedCatalogSnapshotRef: value.SelectedCatalogSnapshotRef,
		SelectedWorkflowVersion: value.SelectedWorkflowVersion, CurrentQuoteHash: value.CurrentQuoteHash,
		EntitlementRefs: entitlement, DeliveryRefs: delivery, ValidationEvidenceRefs: evidence, AttemptedMerchants: attempted,
		ActionCount: value.ActionCount, PaymentAttemptCount: value.PaymentAttemptCount, DeliveryAttemptCount: value.DeliveryAttemptCount,
		RetryCount: value.RetryCount, DiscoveryGeneration: value.DiscoveryGeneration, RecoveryID: value.RecoveryID, MaxTotalAttempts: value.MaxTotalAttempts, MaxPaymentAttempts: value.MaxPaymentAttempts,
		MaxDeliveryAttempts: value.MaxDeliveryAttempts, BudgetCurrency: value.Budget.Currency,
		BudgetLimitMinor: value.Budget.BudgetLimitMinor, ReservedAmount: value.Budget.ReservedAmount,
		SettledAmount: value.Budget.SettledAmount, RefundedAmount: value.Budget.RefundedAmount,
		ConsumedAmount: value.Budget.ConsumedAmount, AvailableBudget: value.Budget.AvailableBudget, SunkCost: value.Budget.SunkCost,
		RefundReusable: value.Budget.RefundReusable,
		DeadlineAt:     value.DeadlineAt, Version: value.Version, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}, nil
}

func modelToEpisode(row EpisodeModel) (*episode.CommerceEpisode, error) {
	decode := func(value []byte) ([]string, error) {
		if len(value) == 0 {
			return nil, nil
		}
		var result []string
		if err := json.Unmarshal(value, &result); err != nil {
			return nil, err
		}
		return result, nil
	}
	entitlement, err := decode(row.EntitlementRefs)
	if err != nil {
		return nil, err
	}
	delivery, err := decode(row.DeliveryRefs)
	if err != nil {
		return nil, err
	}
	evidence, err := decode(row.ValidationEvidenceRefs)
	if err != nil {
		return nil, err
	}
	attempted, err := decode(row.AttemptedMerchants)
	if err != nil {
		return nil, err
	}
	value := &episode.CommerceEpisode{
		EpisodeID: row.EpisodeID, RequestID: row.RequestID, RequesterDID: row.RequesterDID, SessionID: row.SessionID,
		State: episode.State(row.State), TerminalReason: row.TerminalReason,
		ContractSnapshotHash: row.ContractSnapshotHash, ContractSnapshot: append([]byte(nil), row.ContractSnapshot...),
		SelectedMerchantDID: row.SelectedMerchantDID, SelectedCapabilityID: row.SelectedCapabilityID, SelectedCandidateSetID: row.SelectedCandidateSetID,
		SelectedCatalogVersion: row.SelectedCatalogVersion, SelectedCatalogSnapshotHash: row.SelectedCatalogSnapshotHash, SelectedCatalogSnapshotRef: row.SelectedCatalogSnapshotRef,
		SelectedWorkflowVersion: row.SelectedWorkflowVersion, CurrentQuoteHash: row.CurrentQuoteHash,
		EntitlementRefs: entitlement, DeliveryRefs: delivery, ValidationEvidenceRefs: evidence, AttemptedMerchants: attempted,
		ActionCount: row.ActionCount, PaymentAttemptCount: row.PaymentAttemptCount, DeliveryAttemptCount: row.DeliveryAttemptCount,
		RetryCount: row.RetryCount, DiscoveryGeneration: row.DiscoveryGeneration, RecoveryID: row.RecoveryID, MaxTotalAttempts: row.MaxTotalAttempts, MaxPaymentAttempts: row.MaxPaymentAttempts,
		MaxDeliveryAttempts: row.MaxDeliveryAttempts, Budget: episode.BudgetSnapshot{
			Currency: row.BudgetCurrency, BudgetLimitMinor: row.BudgetLimitMinor, ReservedAmount: row.ReservedAmount,
			SettledAmount: row.SettledAmount, RefundedAmount: row.RefundedAmount, AvailableBudget: row.AvailableBudget, SunkCost: row.SunkCost,
			ConsumedAmount: row.ConsumedAmount, RefundReusable: row.RefundReusable,
		}, DeadlineAt: row.DeadlineAt, Version: row.Version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func episodeUpdates(value *episode.CommerceEpisode) (map[string]any, error) {
	model, err := episodeToModel(value)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"session_id": model.SessionID, "state": model.State, "terminal_reason": model.TerminalReason,
		"selected_merchant_did": model.SelectedMerchantDID, "selected_capability_id": model.SelectedCapabilityID, "selected_candidate_set_id": model.SelectedCandidateSetID,
		"selected_catalog_version": model.SelectedCatalogVersion, "selected_catalog_snapshot_hash": model.SelectedCatalogSnapshotHash, "selected_catalog_snapshot_ref": model.SelectedCatalogSnapshotRef,
		"selected_workflow_version": model.SelectedWorkflowVersion, "current_quote_hash": model.CurrentQuoteHash,
		"entitlement_refs": model.EntitlementRefs, "delivery_refs": model.DeliveryRefs,
		"validation_evidence_refs": model.ValidationEvidenceRefs, "attempted_merchants": model.AttemptedMerchants,
		"action_count": model.ActionCount, "payment_attempt_count": model.PaymentAttemptCount,
		"delivery_attempt_count": model.DeliveryAttemptCount, "retry_count": model.RetryCount, "discovery_generation": model.DiscoveryGeneration, "recovery_id": model.RecoveryID,
		"max_total_attempts": model.MaxTotalAttempts, "max_payment_attempts": model.MaxPaymentAttempts,
		"max_delivery_attempts": model.MaxDeliveryAttempts, "budget_currency": model.BudgetCurrency,
		"budget_limit_minor": model.BudgetLimitMinor, "reserved_amount": model.ReservedAmount,
		"settled_amount": model.SettledAmount, "refunded_amount": model.RefundedAmount,
		"consumed_amount": model.ConsumedAmount, "available_budget": model.AvailableBudget, "sunk_cost": model.SunkCost,
		"refund_reusable": model.RefundReusable, "deadline_at": model.DeadlineAt,
		"version": model.Version, "updated_at": model.UpdatedAt,
	}, nil
}

func eventToModel(value *episode.EpisodeEvent) (*EventModel, error) {
	if value == nil {
		return nil, errors.New("event is required")
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	action, err := json.Marshal(value.Action)
	if err != nil {
		return nil, err
	}
	observation, err := json.Marshal(value.Observation)
	if err != nil {
		return nil, err
	}
	decision, err := json.Marshal(value.Decision)
	if err != nil {
		return nil, err
	}
	verdict, err := json.Marshal(value.RuntimeVerdict)
	if err != nil {
		return nil, err
	}
	return &EventModel{EventID: value.EventID, EpisodeID: value.EpisodeID, Sequence: value.Sequence,
		OccurredAt: value.OccurredAt, StateBefore: string(value.StateBefore), ActionPayload: action,
		ObservationPayload: observation, DecisionPayload: decision, VerdictPayload: verdict,
		StateAfter: string(value.StateAfter), Actor: value.Actor, TraceID: value.TraceID,
		RuntimeVersion: value.RuntimeVersion, IdempotencyKey: value.Action.IdempotencyKey}, nil
}

func modelToEvent(row EventModel) (*episode.EpisodeEvent, error) {
	var action trace.Action
	var observation trace.Observation
	var decision trace.Decision
	var verdict trace.RuntimeVerdict
	if err := json.Unmarshal(row.ActionPayload, &action); err != nil {
		return nil, fmt.Errorf("decode action: %w", err)
	}
	if err := json.Unmarshal(row.ObservationPayload, &observation); err != nil {
		return nil, fmt.Errorf("decode observation: %w", err)
	}
	if err := json.Unmarshal(row.DecisionPayload, &decision); err != nil {
		return nil, fmt.Errorf("decode decision: %w", err)
	}
	if err := json.Unmarshal(row.VerdictPayload, &verdict); err != nil {
		return nil, fmt.Errorf("decode verdict: %w", err)
	}
	value := &episode.EpisodeEvent{EventID: row.EventID, EpisodeID: row.EpisodeID, Sequence: row.Sequence,
		OccurredAt: row.OccurredAt, StateBefore: episode.State(row.StateBefore), Action: action,
		Observation: observation, Decision: decision, RuntimeVerdict: verdict, StateAfter: episode.State(row.StateAfter),
		Actor: row.Actor, TraceID: row.TraceID, RuntimeVersion: row.RuntimeVersion}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func ledgerToModel(value *ledger.LedgerEntry) (*LedgerEntryModel, error) {
	if value == nil {
		return nil, ledger.ErrInvalidEntry
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return &LedgerEntryModel{
		EntryID: value.EntryID, EpisodeID: value.EpisodeID, Sequence: value.Sequence, Type: string(value.Type), Currency: value.Currency,
		AmountMinor: value.AmountMinor, PaymentIntentID: value.PaymentIntentID, TxID: value.TxID, RefundID: value.RefundID,
		IdempotencyKey: value.IdempotencyKey, OccurredAt: value.OccurredAt, TraceID: value.TraceID,
		ReferenceHash: value.ReferenceHash, MetadataHash: value.MetadataHash,
	}, nil
}

func modelToLedger(row LedgerEntryModel) (*ledger.LedgerEntry, error) {
	value := &ledger.LedgerEntry{
		EntryID: row.EntryID, EpisodeID: row.EpisodeID, Sequence: row.Sequence, Type: ledger.EntryType(row.Type), Currency: row.Currency,
		AmountMinor: row.AmountMinor, PaymentIntentID: row.PaymentIntentID, TxID: row.TxID, RefundID: row.RefundID,
		IdempotencyKey: row.IdempotencyKey, OccurredAt: row.OccurredAt, TraceID: row.TraceID,
		ReferenceHash: row.ReferenceHash, MetadataHash: row.MetadataHash,
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func paymentIntentToModel(value *payment.PaymentIntent) (*PaymentIntentModel, error) {
	if value == nil {
		return nil, payment.ErrInvalidIntent
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return &PaymentIntentModel{
		IntentID: value.IntentID, EpisodeID: value.EpisodeID, MerchantDID: value.MerchantDID, CapabilityID: value.CapabilityID, PayeeDID: value.PayeeDID,
		QuoteHash: value.QuoteHash, AmountMinor: value.AmountMinor, Currency: value.Currency, RequesterDID: value.RequesterDID,
		EpisodeVersion: value.EpisodeVersion, BudgetReservation: value.BudgetReservation, IdempotencyKey: value.IdempotencyKey,
		EconomicKey: value.EconomicKey, ExpiresAt: value.ExpiresAt, Status: string(value.Status), TxID: value.TxID, TxHash: value.TxHash,
		AuthorizationRef: value.AuthorizationRef, CredentialRef: value.CredentialRef, RequestFingerprint: value.RequestFingerprint, FailureCode: value.FailureCode, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
	}, nil
}

func modelToPaymentIntent(row PaymentIntentModel) (*payment.PaymentIntent, error) {
	value := &payment.PaymentIntent{
		IntentID: row.IntentID, EpisodeID: row.EpisodeID, MerchantDID: row.MerchantDID, CapabilityID: row.CapabilityID, PayeeDID: row.PayeeDID,
		QuoteHash: row.QuoteHash, AmountMinor: row.AmountMinor, Currency: row.Currency, RequesterDID: row.RequesterDID,
		EpisodeVersion: row.EpisodeVersion, BudgetReservation: row.BudgetReservation, IdempotencyKey: row.IdempotencyKey,
		EconomicKey: row.EconomicKey, ExpiresAt: row.ExpiresAt, Status: payment.IntentStatus(row.Status), TxID: row.TxID, TxHash: row.TxHash,
		AuthorizationRef: row.AuthorizationRef, CredentialRef: row.CredentialRef, RequestFingerprint: row.RequestFingerprint, FailureCode: row.FailureCode, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	return value, nil
}

func paymentIntentUpdates(value *payment.PaymentIntent) map[string]any {
	return map[string]any{
		"episode_version": value.EpisodeVersion, "status": string(value.Status), "tx_id": value.TxID, "tx_hash": value.TxHash,
		"authorization_ref": value.AuthorizationRef, "credential_ref": value.CredentialRef, "request_fingerprint": value.RequestFingerprint, "failure_code": value.FailureCode, "updated_at": value.UpdatedAt,
	}
}

func sameIntentIdentity(left, right *payment.PaymentIntent) bool {
	if left == nil || right == nil {
		return false
	}
	return left.IntentID == right.IntentID && left.EpisodeID == right.EpisodeID && left.MerchantDID == right.MerchantDID &&
		left.CapabilityID == right.CapabilityID && left.PayeeDID == right.PayeeDID && left.QuoteHash == right.QuoteHash && left.AmountMinor == right.AmountMinor &&
		left.Currency == right.Currency && left.RequesterDID == right.RequesterDID &&
		left.BudgetReservation == right.BudgetReservation && left.IdempotencyKey == right.IdempotencyKey && left.EconomicKey == right.EconomicKey && left.CredentialRef == right.CredentialRef &&
		left.ExpiresAt.Equal(right.ExpiresAt) && left.CreatedAt.Equal(right.CreatedAt) && left.RequestFingerprint == right.RequestFingerprint
}

func isDuplicateKey(err error) bool {
	var mysqlErr *mysqlDriver.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
