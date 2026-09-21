// Package contract contains the immutable, canonical acquisition contract.
package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const DefaultProtocolVersion = "x402-v1"

var sha256Pattern = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

var (
	ErrInvalidRequest        = errors.New("invalid acquire capability request")
	ErrInvalidMoney          = errors.New("budget limit must be a non-negative minor-unit amount")
	ErrInvalidDeadline       = errors.New("deadline must be in the future")
	ErrInvalidAttempts       = errors.New("attempt limits must be positive and bounded")
	ErrInvalidValidator      = errors.New("validator must be a versioned builtin reference")
	ErrInvalidProtocol       = errors.New("at least one supported protocol version is required")
	ErrInvalidInputChecksum  = errors.New("input sha256 must be a 64-character hexadecimal digest")
	ErrInvalidValidationTime = errors.New("validation time must be provided for deadline validation")
)

// KeyValue is used instead of map[string]any in the contract so its canonical
// representation cannot depend on map iteration order or contain executable code.
type KeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type AcquisitionGoal struct {
	TaskType            string     `json:"task_type"`
	Description         string     `json:"description"`
	SemanticConstraints []KeyValue `json:"semantic_constraints,omitempty"`
}

type Input struct {
	URI            string `json:"uri,omitempty"`
	Ref            string `json:"ref,omitempty"`
	ContentType    string `json:"content_type"`
	SHA256         string `json:"sha256,omitempty"`
	AccessTokenRef string `json:"access_token_ref,omitempty"`
}

// Constraints contains business limits in integer minor units. It deliberately
// has no floating-point amount fields.
type Constraints struct {
	BudgetLimitMinor                    int64     `json:"budget_limit_minor"`
	Currency                            string    `json:"currency"`
	DeadlineAt                          time.Time `json:"deadline_at"`
	SupportedProtocolVersions           []string  `json:"supported_protocol_versions"`
	MaxTotalAttempts                    int       `json:"max_total_attempts"`
	MaxPaymentAttempts                  int       `json:"max_payment_attempts"`
	MaxDeliveryAttempts                 int       `json:"max_delivery_attempts"`
	AllowCrossMerchantSwitch            bool      `json:"allow_cross_merchant_switch"`
	RequireParentConfirmationAboveMinor int64     `json:"require_parent_confirmation_above_minor"`
}

type ExpectedOutput struct {
	Schema              string     `json:"schema"`
	ContentType         string     `json:"content_type"`
	SemanticConstraints []KeyValue `json:"semantic_constraints,omitempty"`
}

// ValidatorRef is only a versioned reference plus inert configuration. The
// runtime resolves Kind/Name/Version from its allowlist; it never executes code
// supplied by a request.
type ValidatorRef struct {
	Kind    string     `json:"kind"`
	Name    string     `json:"name"`
	Version string     `json:"version"`
	Config  []KeyValue `json:"config,omitempty"`
}

type AcquireCapabilityRequest struct {
	RequestID       string          `json:"request_id"`
	ParentSessionID string          `json:"parent_session_id,omitempty"`
	ParentEpisodeID string          `json:"parent_episode_id,omitempty"`
	WorkflowID      string          `json:"workflow_id,omitempty"`
	WorkflowVersion string          `json:"workflow_version,omitempty"`
	WorkflowRunID   string          `json:"workflow_run_id,omitempty"`
	WorkflowStepID  string          `json:"workflow_step_id,omitempty"`
	RequesterDID    string          `json:"requester_did"`
	AcquisitionGoal AcquisitionGoal `json:"acquisition_goal"`
	Input           Input           `json:"input"`
	Constraints     Constraints     `json:"constraints"`
	ExpectedOutput  ExpectedOutput  `json:"expected_output"`
	Validator       ValidatorRef    `json:"validator"`
}

func (r AcquireCapabilityRequest) Validate() error {
	return r.validateStructure()
}

// ValidateAt performs structural validation and evaluates deadline semantics
// against the caller-provided clock. It never reads time.Now internally.
func (r AcquireCapabilityRequest) ValidateAt(now time.Time) error {
	if err := r.validateStructure(); err != nil {
		return err
	}
	if now.IsZero() {
		return ErrInvalidValidationTime
	}
	if !r.Constraints.DeadlineAt.After(now.UTC()) {
		return ErrInvalidDeadline
	}
	return nil
}

func (r AcquireCapabilityRequest) validateStructure() error {
	if strings.TrimSpace(r.RequestID) == "" || strings.TrimSpace(r.RequesterDID) == "" {
		return fmt.Errorf("%w: request_id and requester_did are required", ErrInvalidRequest)
	}
	workflowFields := []string{r.WorkflowID, r.WorkflowVersion, r.WorkflowRunID, r.WorkflowStepID}
	workflowPresent := false
	for _, value := range workflowFields {
		if strings.TrimSpace(value) != "" {
			workflowPresent = true
			break
		}
	}
	if workflowPresent {
		for _, value := range workflowFields {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("%w: workflow metadata must be complete", ErrInvalidRequest)
			}
		}
	}
	if strings.TrimSpace(r.AcquisitionGoal.TaskType) == "" || strings.TrimSpace(r.AcquisitionGoal.Description) == "" {
		return fmt.Errorf("%w: acquisition goal task_type and description are required", ErrInvalidRequest)
	}
	if strings.TrimSpace(r.Input.URI) == "" && strings.TrimSpace(r.Input.Ref) == "" {
		return fmt.Errorf("%w: input uri or ref is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(r.Input.ContentType) == "" {
		return fmt.Errorf("%w: input content_type is required", ErrInvalidRequest)
	}
	if r.Input.SHA256 != "" && !sha256Pattern.MatchString(r.Input.SHA256) {
		return ErrInvalidInputChecksum
	}
	if r.Constraints.BudgetLimitMinor < 0 || r.Constraints.RequireParentConfirmationAboveMinor < 0 {
		return ErrInvalidMoney
	}
	if r.Constraints.DeadlineAt.IsZero() {
		return ErrInvalidDeadline
	}
	if r.Constraints.MaxTotalAttempts <= 0 || r.Constraints.MaxTotalAttempts > 1000 ||
		r.Constraints.MaxPaymentAttempts <= 0 || r.Constraints.MaxPaymentAttempts > r.Constraints.MaxTotalAttempts ||
		r.Constraints.MaxDeliveryAttempts <= 0 || r.Constraints.MaxDeliveryAttempts > r.Constraints.MaxTotalAttempts {
		return ErrInvalidAttempts
	}
	if strings.TrimSpace(r.Constraints.Currency) == "" {
		return fmt.Errorf("%w: currency is required", ErrInvalidRequest)
	}
	if len(r.effectiveProtocolVersions()) == 0 {
		return ErrInvalidProtocol
	}
	if err := r.Validator.Validate(); err != nil {
		return err
	}
	return nil
}

func (v ValidatorRef) Validate() error {
	if strings.ToLower(strings.TrimSpace(v.Kind)) != "builtin" ||
		strings.TrimSpace(v.Name) == "" || strings.TrimSpace(v.Version) == "" {
		return ErrInvalidValidator
	}
	for _, item := range v.Config {
		if strings.TrimSpace(item.Key) == "" {
			return fmt.Errorf("%w: validator config key is empty", ErrInvalidValidator)
		}
	}
	return nil
}

func (r AcquireCapabilityRequest) effectiveProtocolVersions() []string {
	versions := append([]string(nil), r.Constraints.SupportedProtocolVersions...)
	if len(versions) == 0 {
		return []string{DefaultProtocolVersion}
	}
	return versions
}

// Normalize returns a new request with whitespace/case normalization and
// deterministic ordering for set-like fields. The receiver is never mutated.
func (r AcquireCapabilityRequest) Normalize() (AcquireCapabilityRequest, error) {
	normalized := r.normalizeFields()
	if err := normalized.Validate(); err != nil {
		return AcquireCapabilityRequest{}, err
	}
	return normalized, nil
}

// NormalizeAt is the runtime entry point when deadline semantics matter.
func (r AcquireCapabilityRequest) NormalizeAt(now time.Time) (AcquireCapabilityRequest, error) {
	normalized := r.normalizeFields()
	if err := normalized.ValidateAt(now); err != nil {
		return AcquireCapabilityRequest{}, err
	}
	return normalized, nil
}

func (r AcquireCapabilityRequest) normalizeFields() AcquireCapabilityRequest {
	r.RequestID = strings.TrimSpace(r.RequestID)
	r.ParentSessionID = strings.TrimSpace(r.ParentSessionID)
	r.ParentEpisodeID = strings.TrimSpace(r.ParentEpisodeID)
	r.WorkflowID = strings.TrimSpace(r.WorkflowID)
	r.WorkflowVersion = strings.TrimSpace(r.WorkflowVersion)
	r.WorkflowRunID = strings.TrimSpace(r.WorkflowRunID)
	r.WorkflowStepID = strings.TrimSpace(r.WorkflowStepID)
	r.RequesterDID = strings.TrimSpace(r.RequesterDID)
	r.AcquisitionGoal.TaskType = strings.TrimSpace(r.AcquisitionGoal.TaskType)
	r.AcquisitionGoal.Description = strings.TrimSpace(r.AcquisitionGoal.Description)
	r.AcquisitionGoal.SemanticConstraints = normalizeKeyValues(r.AcquisitionGoal.SemanticConstraints)
	r.Input.URI = strings.TrimSpace(r.Input.URI)
	r.Input.Ref = strings.TrimSpace(r.Input.Ref)
	r.Input.ContentType = strings.ToLower(strings.TrimSpace(r.Input.ContentType))
	r.Input.SHA256 = strings.ToLower(strings.TrimSpace(r.Input.SHA256))
	r.Input.AccessTokenRef = strings.TrimSpace(r.Input.AccessTokenRef)
	r.Constraints.Currency = strings.ToUpper(strings.TrimSpace(r.Constraints.Currency))
	r.Constraints.DeadlineAt = r.Constraints.DeadlineAt.UTC().Truncate(time.Nanosecond)
	r.Constraints.SupportedProtocolVersions = normalizeStrings(r.effectiveProtocolVersions())
	r.ExpectedOutput.Schema = strings.TrimSpace(r.ExpectedOutput.Schema)
	r.ExpectedOutput.ContentType = strings.ToLower(strings.TrimSpace(r.ExpectedOutput.ContentType))
	r.ExpectedOutput.SemanticConstraints = normalizeKeyValues(r.ExpectedOutput.SemanticConstraints)
	r.Validator.Kind = strings.ToLower(strings.TrimSpace(r.Validator.Kind))
	r.Validator.Name = strings.TrimSpace(r.Validator.Name)
	r.Validator.Version = strings.TrimSpace(r.Validator.Version)
	r.Validator.Config = normalizeKeyValues(r.Validator.Config)
	return r
}

func normalizeStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeKeyValues(values []KeyValue) []KeyValue {
	result := make([]KeyValue, 0, len(values))
	for _, value := range values {
		result = append(result, KeyValue{Key: strings.TrimSpace(value.Key), Value: strings.TrimSpace(value.Value)})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Key == result[j].Key {
			return result[i].Value < result[j].Value
		}
		return result[i].Key < result[j].Key
	})
	return result
}

// CanonicalSnapshot is JSON for the normalized value. It uses a struct with
// ordered slices, never a map, so equal contracts produce byte-identical data.
func (r AcquireCapabilityRequest) CanonicalSnapshot() ([]byte, error) {
	normalized, err := r.Normalize()
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func (r AcquireCapabilityRequest) CanonicalSnapshotAt(now time.Time) ([]byte, error) {
	normalized, err := r.NormalizeAt(now)
	if err != nil {
		return nil, err
	}
	return json.Marshal(normalized)
}

func (r AcquireCapabilityRequest) SnapshotHash() (string, error) {
	snapshot, err := r.CanonicalSnapshot()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(snapshot)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (r AcquireCapabilityRequest) SnapshotHashAt(now time.Time) (string, error) {
	snapshot, err := r.CanonicalSnapshotAt(now)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(snapshot)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
