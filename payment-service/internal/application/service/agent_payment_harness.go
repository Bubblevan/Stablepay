package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stablepay/payment-service/internal/application/dto"
	"github.com/stablepay/payment-service/internal/domain/vo"
	"github.com/stablepay/payment-service/pkg/constants"
	"github.com/stablepay/payment-service/pkg/errors"
	"github.com/stablepay/payment-service/pkg/utils"
)

const (
	intentPending  = "PENDING_USER_CONFIRMATION"
	intentApproved = "APPROVED"
	intentConsumed = "CONSUMED"
)

// IntentStore is intentionally narrow. Redis is an execution-state store, not
// a source of financial truth; a consumed intent remains auditable in payment
// records and event traces.
type IntentStore interface {
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	GetDel(ctx context.Context, key string) (string, error)
}

// AgentHarnessConfig defines the deterministic boundary around an agent. No
// LLM output is trusted as a payment decision: it may populate Purpose, while
// this policy decides whether a transfer can be executed.
type AgentHarnessConfig struct {
	Enabled                 bool
	RequireIntentForPayment bool
	AutoApproveMaxMinor     int64
	MaxIntentAmountMinor    int64
	IntentTTL               time.Duration
	PolicyVersion           string
	AllowedSkillDIDs        []string
}

type storedPaymentIntent struct {
	ID            string             `json:"id"`
	AgentDID      string             `json:"agent_did"`
	SkillDID      string             `json:"skill_did"`
	AmountMinor   int64              `json:"amount_minor"`
	Currency      constants.Currency `json:"currency"`
	Purpose       string             `json:"purpose"`
	ResourceURI   string             `json:"resource_uri,omitempty"`
	Status        string             `json:"status"`
	Decision      string             `json:"decision"`
	ReasonCodes   []string           `json:"reason_codes"`
	PolicyVersion string             `json:"policy_version"`
	ExpiresAt     time.Time          `json:"expires_at"`
}

// AgentPaymentHarness is a fixed DAG: Normalize -> Policy -> Approval ->
// Execute. It makes the Agent behavior inspectable and prevents a model/tool
// from jumping directly to the blockchain executor.
type AgentPaymentHarness struct {
	store   IntentStore
	config  AgentHarnessConfig
	allowed map[string]struct{}
	now     func() time.Time
}

func NewAgentPaymentHarness(store IntentStore, cfg AgentHarnessConfig) *AgentPaymentHarness {
	allowed := make(map[string]struct{}, len(cfg.AllowedSkillDIDs))
	for _, did := range cfg.AllowedSkillDIDs {
		if did = strings.TrimSpace(did); did != "" {
			allowed[did] = struct{}{}
		}
	}
	if cfg.IntentTTL <= 0 {
		cfg.IntentTTL = 5 * time.Minute
	}
	if cfg.PolicyVersion == "" {
		cfg.PolicyVersion = "agent-payment-v1"
	}
	return &AgentPaymentHarness{store: store, config: cfg, allowed: allowed, now: time.Now}
}

func (h *AgentPaymentHarness) Enabled() bool { return h != nil && h.config.Enabled }
func (h *AgentPaymentHarness) RequiresIntent() bool {
	return h.Enabled() && h.config.RequireIntentForPayment
}

func (h *AgentPaymentHarness) Create(ctx context.Context, req *dto.CreatePaymentIntentRequest) (*dto.CreatePaymentIntentResponse, error) {
	if !h.Enabled() {
		return nil, errors.New(errors.PERMISSION_DENIED, "agent payment harness is disabled")
	}
	amountMinor, err := utils.StringToMinorUnit(req.AmountStr)
	if err != nil || amountMinor <= 0 {
		return nil, errors.New(errors.INVALID_PARAMETERS, "invalid intent amount")
	}
	currency := vo.StringToCommonCurrency(strings.ToUpper(req.Currency))
	if currency != constants.CurrencyUSDC && currency != constants.CurrencyUSDT {
		return nil, errors.New(errors.INVALID_PARAMETERS, "unsupported intent currency")
	}

	intent := storedPaymentIntent{
		ID: uuid.NewString(), AgentDID: req.AgentDID, SkillDID: req.SkillDID,
		AmountMinor: amountMinor, Currency: currency, Purpose: strings.TrimSpace(req.Purpose),
		ResourceURI: strings.TrimSpace(req.ResourceURI), PolicyVersion: h.config.PolicyVersion,
		ExpiresAt: h.now().UTC().Add(h.config.IntentTTL),
	}
	if intent.Purpose == "" {
		return nil, errors.New(errors.INVALID_PARAMETERS, "intent purpose is required")
	}

	trace := []string{"normalize", "policy"}
	switch {
	case h.config.MaxIntentAmountMinor > 0 && amountMinor > h.config.MaxIntentAmountMinor:
		intent.Status, intent.Decision = "REJECTED", "DENY"
		intent.ReasonCodes = []string{"AMOUNT_EXCEEDS_AGENT_POLICY"}
	case len(h.allowed) > 0 && !h.isAllowed(req.SkillDID):
		intent.Status, intent.Decision = "REJECTED", "DENY"
		intent.ReasonCodes = []string{"MERCHANT_NOT_ALLOWLISTED"}
	case h.config.AutoApproveMaxMinor > 0 && amountMinor <= h.config.AutoApproveMaxMinor:
		intent.Status, intent.Decision = intentApproved, "ALLOW"
		intent.ReasonCodes = []string{"WITHIN_AUTO_APPROVAL_LIMIT"}
		trace = append(trace, "approval:auto")
	default:
		intent.Status, intent.Decision = intentPending, "REQUIRE_USER_CONFIRMATION"
		intent.ReasonCodes = []string{"ABOVE_AUTO_APPROVAL_LIMIT"}
		trace = append(trace, "approval:await_did_signature")
	}

	if intent.Status != "REJECTED" {
		if err := h.save(ctx, &intent); err != nil {
			return nil, errors.Wrap(errors.INTERNAL_SERVER_ERROR, err, "store payment intent")
		}
	}
	resp := &dto.CreatePaymentIntentResponse{
		IntentID: intent.ID, Status: intent.Status, Decision: intent.Decision,
		ReasonCodes: intent.ReasonCodes, PolicyVersion: intent.PolicyVersion,
		ExpiresAt: intent.ExpiresAt.Format(time.RFC3339), Trace: trace,
	}
	if intent.Status == intentPending {
		resp.ApprovalPayload = approvalPayload(&intent)
	}
	return resp, nil
}

func (h *AgentPaymentHarness) Approve(ctx context.Context, req *dto.ApprovePaymentIntentRequest, validate func(string) error) (*dto.ApprovePaymentIntentResponse, error) {
	if !h.Enabled() {
		return nil, errors.New(errors.PERMISSION_DENIED, "agent payment harness is disabled")
	}
	intent, err := h.load(ctx, req.IntentID)
	if err != nil {
		return nil, err
	}
	if intent.AgentDID != req.AgentDID {
		return nil, errors.New(errors.PERMISSION_DENIED, "approval DID does not own intent")
	}
	if intent.Status != intentPending {
		return nil, errors.New(errors.INVALID_PARAMETERS, "intent is not awaiting confirmation")
	}
	if err := validate(approvalPayload(intent)); err != nil {
		return nil, err
	}
	intent.Status, intent.Decision = intentApproved, "ALLOW"
	intent.ReasonCodes = append(intent.ReasonCodes, "DID_SIGNED_USER_CONFIRMATION")
	if err := h.save(ctx, intent); err != nil {
		return nil, errors.Wrap(errors.INTERNAL_SERVER_ERROR, err, "save approved payment intent")
	}
	return &dto.ApprovePaymentIntentResponse{IntentID: intent.ID, Status: intent.Status, Decision: intent.Decision, PolicyVersion: intent.PolicyVersion, ExpiresAt: intent.ExpiresAt.Format(time.RFC3339)}, nil
}

// Consume is deliberately one-shot. It closes the time-of-check/time-of-use
// gap between a policy decision and chain execution; retries use the existing
// payment idempotency response instead of replaying the intent.
func (h *AgentPaymentHarness) Consume(ctx context.Context, intentID, agentDID, skillDID string, amountMinor int64, currency constants.Currency) error {
	if !h.RequiresIntent() {
		return nil
	}
	if strings.TrimSpace(intentID) == "" {
		return errors.New(errors.PERMISSION_DENIED, "payment intent_id is required")
	}
	raw, err := h.store.GetDel(ctx, h.key(intentID))
	if err != nil {
		return errors.Wrap(errors.PERMISSION_DENIED, err, "payment intent missing, expired, or already consumed")
	}
	var intent storedPaymentIntent
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		return errors.Wrap(errors.INTERNAL_SERVER_ERROR, err, "decode payment intent")
	}
	if intent.Status != intentApproved || h.now().After(intent.ExpiresAt) {
		return errors.New(errors.PERMISSION_DENIED, "payment intent is not executable")
	}
	if intent.AgentDID != agentDID || intent.SkillDID != skillDID || intent.AmountMinor != amountMinor || intent.Currency != currency {
		return errors.New(errors.PERMISSION_DENIED, "payment does not match approved intent")
	}
	return nil
}

func (h *AgentPaymentHarness) load(ctx context.Context, id string) (*storedPaymentIntent, error) {
	raw, err := h.store.Get(ctx, h.key(id))
	if err != nil {
		return nil, errors.Wrap(errors.RESOURCE_NOT_FOUND, err, "payment intent not found or expired")
	}
	var intent storedPaymentIntent
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		return nil, errors.Wrap(errors.INTERNAL_SERVER_ERROR, err, "decode payment intent")
	}
	if h.now().After(intent.ExpiresAt) {
		return nil, errors.New(errors.RESOURCE_NOT_FOUND, "payment intent expired")
	}
	return &intent, nil
}

func (h *AgentPaymentHarness) save(ctx context.Context, intent *storedPaymentIntent) error {
	b, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	ttl := time.Until(intent.ExpiresAt)
	if ttl <= 0 {
		return fmt.Errorf("intent expired")
	}
	return h.store.Set(ctx, h.key(intent.ID), string(b), ttl)
}
func (h *AgentPaymentHarness) key(id string) string      { return "agent-payment:intent:" + id }
func (h *AgentPaymentHarness) isAllowed(did string) bool { _, ok := h.allowed[did]; return ok }

func approvalPayload(intent *storedPaymentIntent) string {
	return fmt.Sprintf("stablepay:agent-payment-approval:v1|%s|%s|%s|%d|%s|%s", intent.ID, intent.AgentDID, intent.SkillDID, intent.AmountMinor, intent.Currency, intent.PolicyVersion)
}
