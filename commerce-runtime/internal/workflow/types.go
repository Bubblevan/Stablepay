// Package workflow contains the declarative, durable orchestration plane.
// Workflow code coordinates Commerce Episodes; it never implements payment,
// merchant, memory, validation, or RuntimeGuard behavior itself.
package workflow

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

	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/ledger"
)

type WorkflowID string
type WorkflowVersion string

const (
	StepAcquireCapability = "ACQUIRE_CAPABILITY"

	WorkflowAccepted       = "ACCEPTED"
	WorkflowRunning        = "RUNNING"
	WorkflowAwaitingParent = "AWAITING_PARENT"
	WorkflowFulfilled      = "FULFILLED"
	WorkflowFailed         = "FAILED"
	WorkflowAborted        = "ABORTED"
	WorkflowExpired        = "EXPIRED"

	StepPending   = "PENDING"
	StepReady     = "READY"
	StepRunning   = "RUNNING"
	StepFulfilled = "FULFILLED"
	StepFailed    = "FAILED"

	EventWorkflowCreated        = "WORKFLOW_CREATED"
	EventStepReady              = "STEP_READY"
	EventStepEpisodeCreated     = "STEP_EPISODE_CREATED"
	EventStepFulfilled          = "STEP_FULFILLED"
	EventStepFailed             = "STEP_FAILED"
	EventWorkflowAwaitingParent = "WORKFLOW_AWAITING_PARENT"
	EventWorkflowFulfilled      = "WORKFLOW_FULFILLED"
	EventWorkflowFailed         = "WORKFLOW_FAILED"
	EventWorkflowExpired        = "WORKFLOW_EXPIRED"
)

var (
	ErrInvalidDefinition                   = errors.New("invalid workflow definition")
	ErrDefinitionConflict                  = errors.New("workflow definition conflicts with an immutable version")
	ErrInvalidWorkflowRun                  = errors.New("invalid workflow run")
	ErrWorkflowRequestConflict             = errors.New("workflow request_id conflicts with an existing run")
	ErrWorkflowVersionConflict             = errors.New("workflow run version conflict")
	ErrWorkflowEventConflict               = errors.New("workflow event conflict")
	ErrWorkflowTerminal                    = errors.New("workflow run is terminal")
	ErrWorkflowBudget                      = errors.New("workflow budget is insufficient")
	ErrWorkflowArtifact                    = errors.New("workflow artifact binding is invalid")
	ErrWorkflowArtifactNotFound            = errors.New("workflow artifact is not available")
	ErrWorkflowArtifactNotFulfilled        = errors.New("workflow artifact requires a fulfilled workflow")
	ErrWorkflowArtifactHashMismatch        = errors.New("workflow artifact hash mismatch")
	ErrWorkflowArtifactContentTypeMismatch = errors.New("workflow artifact content type mismatch")
	ErrWorkflowArtifactTooLarge            = errors.New("workflow artifact exceeds the bounded payload limit")
	ErrWorkflowArtifactProvenanceMismatch  = errors.New("workflow artifact provenance mismatch")
	ErrInvalidIdentifier                   = errors.New("invalid workflow identifier")
	ErrWorkflowDeadline                    = errors.New("workflow deadline has expired")
	ErrWorkflowNotReady                    = errors.New("workflow step is not ready")
)

type WorkflowInputSpec struct {
	ContentType string `json:"content_type"`
	Schema      string `json:"schema,omitempty"`
}

type WorkflowOutputSpec struct {
	ContentType string `json:"content_type"`
	Schema      string `json:"schema,omitempty"`
}

type CapabilityRequirement struct {
	TaskType                  string              `json:"task_type"`
	SemanticConstraints       []contract.KeyValue `json:"semantic_constraints,omitempty"`
	RequiredInputContentType  string              `json:"required_input_content_type"`
	RequiredOutputContentType string              `json:"required_output_content_type"`
	RequiredProtocolVersions  []string            `json:"required_protocol_versions,omitempty"`
}

type WorkflowInputSource string

const (
	WorkflowRootInput  WorkflowInputSource = "WORKFLOW_INPUT"
	WorkflowStepOutput WorkflowInputSource = "STEP_OUTPUT"
)

type WorkflowInputBinding struct {
	Source       WorkflowInputSource `json:"source"`
	SourceStepID string              `json:"source_step_id,omitempty"`
	ContentType  string              `json:"content_type"`
}

type WorkflowStepDefinition struct {
	StepID         string                  `json:"step_id"`
	StepType       string                  `json:"step_type"`
	Capability     CapabilityRequirement   `json:"capability"`
	DependsOn      []string                `json:"depends_on,omitempty"`
	InputBinding   WorkflowInputBinding    `json:"input_binding"`
	ExpectedOutput contract.ExpectedOutput `json:"expected_output"`
	Validator      contract.ValidatorRef   `json:"validator"`

	MaxBudgetMinor                      int64  `json:"max_budget_minor"`
	MaxTotalAttempts                    int    `json:"max_total_attempts"`
	MaxPaymentAttempts                  int    `json:"max_payment_attempts"`
	MaxDeliveryAttempts                 int    `json:"max_delivery_attempts"`
	TimeoutSeconds                      int64  `json:"timeout_seconds"`
	RequiredInputSchemaRef              string `json:"required_input_schema_ref,omitempty"`
	AllowCrossMerchantSwitch            bool   `json:"allow_cross_merchant_switch"`
	RequireParentConfirmationAboveMinor int64  `json:"require_parent_confirmation_above_minor"`
}

type WorkflowDefinition struct {
	WorkflowID     string                   `json:"workflow_id"`
	Version        string                   `json:"version"`
	Name           string                   `json:"name"`
	Description    string                   `json:"description"`
	Input          WorkflowInputSpec        `json:"input"`
	Output         WorkflowOutputSpec       `json:"output"`
	Steps          []WorkflowStepDefinition `json:"steps"`
	Currency       string                   `json:"currency"`
	MaxBudgetMinor int64                    `json:"max_budget_minor,omitempty"`
	CreatedAt      time.Time                `json:"created_at"`
	FactsRef       string                   `json:"facts_ref"`
	DefinitionHash string                   `json:"definition_hash"`
}

func (d WorkflowDefinition) Clone() *WorkflowDefinition {
	copy := d.Normalize()
	copy.Steps = append([]WorkflowStepDefinition(nil), d.Steps...)
	for index := range copy.Steps {
		copy.Steps[index].DependsOn = append([]string(nil), d.Steps[index].DependsOn...)
		copy.Steps[index].Capability.RequiredProtocolVersions = append([]string(nil), d.Steps[index].Capability.RequiredProtocolVersions...)
		copy.Steps[index].Capability.SemanticConstraints = append([]contract.KeyValue(nil), d.Steps[index].Capability.SemanticConstraints...)
		copy.Steps[index].ExpectedOutput.SemanticConstraints = append([]contract.KeyValue(nil), d.Steps[index].ExpectedOutput.SemanticConstraints...)
		copy.Steps[index].Validator.Config = append([]contract.KeyValue(nil), d.Steps[index].Validator.Config...)
	}
	return &copy
}

func (d WorkflowDefinition) Normalize() WorkflowDefinition {
	d.WorkflowID = strings.TrimSpace(d.WorkflowID)
	d.Version = strings.TrimSpace(d.Version)
	d.Name = strings.TrimSpace(d.Name)
	d.Description = strings.TrimSpace(d.Description)
	d.Currency = strings.ToUpper(strings.TrimSpace(d.Currency))
	d.Input.ContentType = normalizeContentType(d.Input.ContentType)
	d.Output.ContentType = normalizeContentType(d.Output.ContentType)
	d.Input.Schema = strings.TrimSpace(d.Input.Schema)
	d.Output.Schema = strings.TrimSpace(d.Output.Schema)
	d.FactsRef = strings.TrimSpace(d.FactsRef)
	d.DefinitionHash = strings.TrimSpace(d.DefinitionHash)
	if !d.CreatedAt.IsZero() {
		d.CreatedAt = d.CreatedAt.UTC().Truncate(time.Nanosecond)
	}
	for i := range d.Steps {
		step := &d.Steps[i]
		step.StepID = strings.TrimSpace(step.StepID)
		step.StepType = strings.ToUpper(strings.TrimSpace(step.StepType))
		if step.StepType == "" {
			step.StepType = StepAcquireCapability
		}
		step.DependsOn = normalizeStrings(step.DependsOn)
		step.Capability.TaskType = strings.TrimSpace(step.Capability.TaskType)
		step.Capability.RequiredInputContentType = normalizeContentType(step.Capability.RequiredInputContentType)
		step.Capability.RequiredOutputContentType = normalizeContentType(step.Capability.RequiredOutputContentType)
		step.Capability.RequiredProtocolVersions = normalizeStrings(step.Capability.RequiredProtocolVersions)
		step.Capability.SemanticConstraints = normalizeKeyValues(step.Capability.SemanticConstraints)
		step.InputBinding.Source = WorkflowInputSource(strings.ToUpper(strings.TrimSpace(string(step.InputBinding.Source))))
		step.InputBinding.SourceStepID = strings.TrimSpace(step.InputBinding.SourceStepID)
		step.InputBinding.ContentType = normalizeContentType(step.InputBinding.ContentType)
		step.ExpectedOutput.Schema = strings.TrimSpace(step.ExpectedOutput.Schema)
		step.ExpectedOutput.ContentType = normalizeContentType(step.ExpectedOutput.ContentType)
		step.ExpectedOutput.SemanticConstraints = normalizeKeyValues(step.ExpectedOutput.SemanticConstraints)
		step.Validator.Kind = strings.ToLower(strings.TrimSpace(step.Validator.Kind))
		step.Validator.Name = strings.TrimSpace(step.Validator.Name)
		step.Validator.Version = strings.TrimSpace(step.Validator.Version)
		step.Validator.Config = normalizeKeyValues(step.Validator.Config)
		step.RequiredInputSchemaRef = strings.TrimSpace(step.RequiredInputSchemaRef)
	}
	sort.Slice(d.Steps, func(i, j int) bool { return d.Steps[i].StepID < d.Steps[j].StepID })
	return d
}

func (d WorkflowDefinition) Validate() error {
	if err := validateDefinitionIdentifiers(d); err != nil {
		return err
	}
	d = d.Normalize()
	if d.WorkflowID == "" || d.Version == "" || d.Name == "" || d.Currency == "" || d.Input.ContentType == "" || d.Output.ContentType == "" || len(d.Steps) == 0 {
		return fmt.Errorf("%w: identity, currency, input/output content types and steps are required", ErrInvalidDefinition)
	}
	if d.MaxBudgetMinor < 0 || d.CreatedAt.IsZero() || d.FactsRef == "" {
		return fmt.Errorf("%w: created_at, facts_ref and non-negative max budget are required", ErrInvalidDefinition)
	}
	steps := make(map[string]WorkflowStepDefinition, len(d.Steps))
	for _, step := range d.Steps {
		if step.StepID == "" || step.StepType != StepAcquireCapability || step.Capability.TaskType == "" || step.Capability.RequiredInputContentType == "" || step.Capability.RequiredOutputContentType == "" || step.ExpectedOutput.ContentType == "" || step.MaxBudgetMinor <= 0 || step.MaxTotalAttempts <= 0 || step.MaxPaymentAttempts <= 0 || step.MaxDeliveryAttempts <= 0 || step.MaxPaymentAttempts > step.MaxTotalAttempts || step.MaxDeliveryAttempts > step.MaxTotalAttempts || step.TimeoutSeconds < 0 || step.RequireParentConfirmationAboveMinor < 0 {
			return fmt.Errorf("%w: invalid step %q", ErrInvalidDefinition, step.StepID)
		}
		if err := step.Validator.Validate(); err != nil {
			return fmt.Errorf("%w: step %s validator: %v", ErrInvalidDefinition, step.StepID, err)
		}
		if _, exists := steps[step.StepID]; exists {
			return fmt.Errorf("%w: duplicate step id %s", ErrInvalidDefinition, step.StepID)
		}
		steps[step.StepID] = step
	}
	indegree := make(map[string]int, len(steps))
	children := make(map[string][]string, len(steps))
	for _, step := range d.Steps {
		for _, dependency := range step.DependsOn {
			if dependency == step.StepID {
				return fmt.Errorf("%w: step %s depends on itself", ErrInvalidDefinition, step.StepID)
			}
			if _, exists := steps[dependency]; !exists {
				return fmt.Errorf("%w: step %s depends on missing step %s", ErrInvalidDefinition, step.StepID, dependency)
			}
			indegree[step.StepID]++
			children[dependency] = append(children[dependency], step.StepID)
		}
		if step.InputBinding.Source == WorkflowInputSource("") {
			return fmt.Errorf("%w: step %s input binding source is required", ErrInvalidDefinition, step.StepID)
		}
		switch step.InputBinding.Source {
		case WorkflowRootInput:
			if step.InputBinding.SourceStepID != "" {
				return fmt.Errorf("%w: root step %s cannot name a source step", ErrInvalidDefinition, step.StepID)
			}
			if step.InputBinding.ContentType != "" && step.InputBinding.ContentType != d.Input.ContentType {
				return fmt.Errorf("%w: root input content type mismatch at step %s", ErrInvalidDefinition, step.StepID)
			}
			if step.Capability.RequiredInputContentType != d.Input.ContentType {
				return fmt.Errorf("%w: root capability input content type mismatch at step %s", ErrInvalidDefinition, step.StepID)
			}
		case WorkflowStepOutput:
			upstream, exists := steps[step.InputBinding.SourceStepID]
			if !exists {
				return fmt.Errorf("%w: step %s source step is missing", ErrInvalidDefinition, step.StepID)
			}
			if !contains(step.DependsOn, step.InputBinding.SourceStepID) {
				return fmt.Errorf("%w: step %s source step must be a dependency", ErrInvalidDefinition, step.StepID)
			}
			if upstream.ExpectedOutput.ContentType != step.Capability.RequiredInputContentType {
				return fmt.Errorf("%w: content type mismatch %s -> %s", ErrInvalidDefinition, upstream.StepID, step.StepID)
			}
			if step.InputBinding.ContentType != "" && step.InputBinding.ContentType != upstream.ExpectedOutput.ContentType {
				return fmt.Errorf("%w: input binding content type mismatch %s -> %s", ErrInvalidDefinition, upstream.StepID, step.StepID)
			}
			if step.RequiredInputSchemaRef != "" && step.RequiredInputSchemaRef != upstream.ExpectedOutput.Schema {
				return fmt.Errorf("%w: schema ref mismatch %s -> %s", ErrInvalidDefinition, upstream.StepID, step.StepID)
			}
		default:
			return fmt.Errorf("%w: unsupported input binding source %q", ErrInvalidDefinition, step.InputBinding.Source)
		}
		if step.Capability.RequiredOutputContentType != step.ExpectedOutput.ContentType {
			return fmt.Errorf("%w: step %s capability/output content type mismatch", ErrInvalidDefinition, step.StepID)
		}
	}
	order, err := topologicalOrder(d.Steps, indegree, children)
	if err != nil {
		return err
	}
	if len(order) != len(d.Steps) {
		return fmt.Errorf("%w: workflow contains a cycle", ErrInvalidDefinition)
	}
	if d.MaxBudgetMinor > 0 {
		for _, step := range d.Steps {
			if step.MaxBudgetMinor > d.MaxBudgetMinor {
				return fmt.Errorf("%w: step %s max budget exceeds workflow max", ErrInvalidDefinition, step.StepID)
			}
		}
	}
	sinkCount := 0
	for _, step := range d.Steps {
		if len(children[step.StepID]) != 0 {
			continue
		}
		sinkCount++
		if step.ExpectedOutput.ContentType != d.Output.ContentType || (d.Output.Schema != "" && step.ExpectedOutput.Schema != d.Output.Schema) {
			return fmt.Errorf("%w: sink step %s output content type does not match workflow output", ErrInvalidDefinition, step.StepID)
		}
	}
	if sinkCount != 1 {
		return fmt.Errorf("%w: workflow must contain exactly one sink step, found %d", ErrInvalidDefinition, sinkCount)
	}
	if hash := d.DefinitionHash; hash != "" {
		computed, hashErr := d.ComputedDefinitionHash()
		if hashErr != nil || hash != computed {
			return fmt.Errorf("%w: definition hash mismatch", ErrInvalidDefinition)
		}
	}
	return nil
}

func (d WorkflowDefinition) CanonicalSnapshot() ([]byte, error) {
	if err := validateDefinitionIdentifiers(d); err != nil {
		return nil, err
	}
	d = d.Normalize()
	d.DefinitionHash = ""
	if err := d.ValidateWithoutHash(); err != nil {
		return nil, err
	}
	return json.Marshal(d)
}

func (d WorkflowDefinition) ValidateWithoutHash() error {
	hash := d.DefinitionHash
	d.DefinitionHash = ""
	err := d.Validate()
	d.DefinitionHash = hash
	return err
}

func (d WorkflowDefinition) ComputedDefinitionHash() (string, error) {
	snapshot, err := d.CanonicalSnapshot()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(snapshot)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (d WorkflowDefinition) WithComputedHash() (WorkflowDefinition, error) {
	if err := validateDefinitionIdentifiers(d); err != nil {
		return WorkflowDefinition{}, err
	}
	d = d.Normalize()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now().UTC().Truncate(time.Nanosecond)
	}
	hash, err := d.ComputedDefinitionHash()
	if err != nil {
		return WorkflowDefinition{}, err
	}
	d.DefinitionHash = hash
	return d, d.Validate()
}

var stableIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// ValidateIdentifier protects identifiers embedded in HTTP paths,
// deterministic IDs, and workflow-artifact URIs. Case is preserved because
// identity canonicalization must not silently change names.
func ValidateIdentifier(value string) error {
	if strings.TrimSpace(value) != value || !stableIdentifierPattern.MatchString(value) || strings.Contains(value, "..") {
		return fmt.Errorf("%w: %q", ErrInvalidIdentifier, value)
	}
	return nil
}

func validateDefinitionIdentifiers(definition WorkflowDefinition) error {
	if err := ValidateIdentifier(definition.WorkflowID); err != nil {
		return fmt.Errorf("%w: workflow_id: %v", ErrInvalidDefinition, err)
	}
	if err := ValidateIdentifier(definition.Version); err != nil {
		return fmt.Errorf("%w: version: %v", ErrInvalidDefinition, err)
	}
	for _, step := range definition.Steps {
		if err := ValidateIdentifier(step.StepID); err != nil {
			return fmt.Errorf("%w: step_id: %v", ErrInvalidDefinition, err)
		}
	}
	return nil
}

// OutputStep is the sole semantic source of a workflow's final output.
func OutputStep(definition WorkflowDefinition) (WorkflowStepDefinition, error) {
	if err := definition.Validate(); err != nil {
		return WorkflowStepDefinition{}, err
	}
	definition = definition.Normalize()
	children := make(map[string]int, len(definition.Steps))
	for _, step := range definition.Steps {
		for _, dependency := range step.DependsOn {
			children[dependency]++
		}
	}
	for _, step := range definition.Steps {
		if children[step.StepID] == 0 {
			return step, nil
		}
	}
	return WorkflowStepDefinition{}, fmt.Errorf("%w: workflow has no output step", ErrInvalidDefinition)
}

func TopologicalOrder(def WorkflowDefinition) ([]string, error) {
	d := def.Normalize()
	if err := d.Validate(); err != nil {
		return nil, err
	}
	indegree := make(map[string]int, len(d.Steps))
	children := make(map[string][]string, len(d.Steps))
	for _, step := range d.Steps {
		for _, dependency := range step.DependsOn {
			indegree[step.StepID]++
			children[dependency] = append(children[dependency], step.StepID)
		}
	}
	return topologicalOrder(d.Steps, indegree, children)
}

func topologicalOrder(steps []WorkflowStepDefinition, indegree map[string]int, children map[string][]string) ([]string, error) {
	ready := make([]string, 0)
	for _, step := range steps {
		if indegree[step.StepID] == 0 {
			ready = append(ready, step.StepID)
		}
	}
	sort.Strings(ready)
	order := make([]string, 0, len(steps))
	for len(ready) > 0 {
		stepID := ready[0]
		ready = ready[1:]
		order = append(order, stepID)
		for _, child := range children[stepID] {
			indegree[child]--
			if indegree[child] == 0 {
				ready = append(ready, child)
			}
		}
		sort.Strings(ready)
	}
	if len(order) != len(steps) {
		return nil, fmt.Errorf("%w: workflow contains a cycle", ErrInvalidDefinition)
	}
	return order, nil
}

type WorkflowRunRequest struct {
	RequestID        string         `json:"request_id"`
	WorkflowID       string         `json:"workflow_id"`
	WorkflowVersion  string         `json:"workflow_version,omitempty"`
	RequesterDID     string         `json:"requester_did"`
	ParentSessionID  string         `json:"parent_session_id,omitempty"`
	Input            contract.Input `json:"input"`
	BudgetLimitMinor int64          `json:"budget_limit_minor"`
	Currency         string         `json:"currency"`
	DeadlineAt       time.Time      `json:"deadline_at"`
}

type WorkflowBudgetSnapshot struct {
	Currency         string `json:"currency"`
	BudgetLimitMinor int64  `json:"budget_limit_minor"`
	SettledMinor     int64  `json:"settled_minor"`
	RefundedMinor    int64  `json:"refunded_minor"`
	ConsumedMinor    int64  `json:"consumed_minor"`
	SunkCostMinor    int64  `json:"sunk_cost_minor"`
	AvailableMinor   int64  `json:"available_minor"`
}

func newBudget(currency string, limit int64) WorkflowBudgetSnapshot {
	return WorkflowBudgetSnapshot{Currency: strings.ToUpper(strings.TrimSpace(currency)), BudgetLimitMinor: limit, AvailableMinor: limit}
}

func projectBudget(currency string, limit int64, entries []*ledger.LedgerEntry) (WorkflowBudgetSnapshot, error) {
	projection, err := ledger.BuildProjection(currency, limit, true, entries)
	if err != nil {
		return WorkflowBudgetSnapshot{}, err
	}
	return WorkflowBudgetSnapshot{Currency: projection.Currency, BudgetLimitMinor: projection.BudgetLimitMinor, SettledMinor: projection.SettledAmount, RefundedMinor: projection.RefundedAmount, ConsumedMinor: projection.ConsumedAmount, SunkCostMinor: projection.SunkCost, AvailableMinor: projection.AvailableBudget}, nil
}

type WorkflowRun struct {
	WorkflowRunID       string                 `json:"workflow_run_id"`
	RequestID           string                 `json:"request_id"`
	RequestSnapshotHash string                 `json:"request_snapshot_hash"`
	WorkflowID          string                 `json:"workflow_id"`
	WorkflowVersion     string                 `json:"workflow_version"`
	DefinitionHash      string                 `json:"definition_hash"`
	RequesterDID        string                 `json:"requester_did"`
	ParentSessionID     string                 `json:"parent_session_id,omitempty"`
	State               string                 `json:"state"`
	Input               contract.Input         `json:"input"`
	Budget              WorkflowBudgetSnapshot `json:"budget"`
	DeadlineAt          time.Time              `json:"deadline_at"`
	Version             uint64                 `json:"version"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
}

// ArtifactRef identifies a validated output inside one WorkflowRun. The URI
// is an internal reference; merchants receive materialized payload bytes
// through the existing adapter boundary instead.
type ArtifactRef struct {
	WorkflowRunID string
	StepID        string
}

func (r ArtifactRef) URI() string {
	return "workflow-artifact://" + r.WorkflowRunID + "/" + r.StepID
}

func (r WorkflowRun) Clone() *WorkflowRun { copy := r; return &copy }

func IsTerminalRun(state string) bool {
	switch state {
	case WorkflowFulfilled, WorkflowFailed, WorkflowAborted, WorkflowExpired:
		return true
	default:
		return false
	}
}

func (r WorkflowRun) Validate() error {
	if strings.TrimSpace(r.WorkflowRunID) == "" || strings.TrimSpace(r.RequestID) == "" || strings.TrimSpace(r.WorkflowID) == "" || strings.TrimSpace(r.WorkflowVersion) == "" || strings.TrimSpace(r.DefinitionHash) == "" || strings.TrimSpace(r.RequesterDID) == "" || r.State == "" || r.Version == 0 || r.DeadlineAt.IsZero() || r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
		return ErrInvalidWorkflowRun
	}
	if ValidateIdentifier(r.WorkflowRunID) != nil || ValidateIdentifier(r.WorkflowID) != nil || ValidateIdentifier(r.WorkflowVersion) != nil {
		return ErrInvalidWorkflowRun
	}
	if r.Budget.Currency == "" || r.Budget.BudgetLimitMinor < 0 || r.Budget.SettledMinor < 0 || r.Budget.RefundedMinor < 0 || r.Budget.ConsumedMinor < 0 || r.Budget.AvailableMinor < 0 || r.Budget.SunkCostMinor < 0 {
		return ErrInvalidWorkflowRun
	}
	if !IsKnownRunState(r.State) {
		return ErrInvalidWorkflowRun
	}
	return nil
}

func IsKnownRunState(value string) bool {
	switch value {
	case WorkflowAccepted, WorkflowRunning, WorkflowAwaitingParent, WorkflowFulfilled, WorkflowFailed, WorkflowAborted, WorkflowExpired:
		return true
	default:
		return false
	}
}

type WorkflowStepRun struct {
	WorkflowRunID  string     `json:"workflow_run_id"`
	StepID         string     `json:"step_id"`
	State          string     `json:"state"`
	ChildEpisodeID string     `json:"child_episode_id,omitempty"`
	ChildRequestID string     `json:"child_request_id,omitempty"`
	InputRef       string     `json:"input_ref,omitempty"`
	InputHash      string     `json:"input_hash,omitempty"`
	OutputRef      string     `json:"output_ref,omitempty"`
	OutputHash     string     `json:"output_hash,omitempty"`
	ContentType    string     `json:"content_type,omitempty"`
	DeliveryID     string     `json:"delivery_id,omitempty"`
	Attempt        int        `json:"attempt"`
	Version        uint64     `json:"version"`
	StartedAt      time.Time  `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
}

func (s WorkflowStepRun) Clone() *WorkflowStepRun {
	copy := s
	if s.CompletedAt != nil {
		value := *s.CompletedAt
		copy.CompletedAt = &value
	}
	return &copy
}

func (s WorkflowStepRun) Validate() error {
	if strings.TrimSpace(s.WorkflowRunID) == "" || strings.TrimSpace(s.StepID) == "" || !IsKnownStepState(s.State) || s.Attempt < 0 || s.Version == 0 {
		return ErrInvalidWorkflowRun
	}
	if ValidateIdentifier(s.WorkflowRunID) != nil || ValidateIdentifier(s.StepID) != nil {
		return ErrInvalidWorkflowRun
	}
	if s.State == StepFulfilled && (strings.TrimSpace(s.ChildEpisodeID) == "" || strings.TrimSpace(s.DeliveryID) == "" || strings.TrimSpace(s.OutputRef) == "" || strings.TrimSpace(s.OutputHash) == "" || strings.TrimSpace(s.ContentType) == "") {
		return ErrInvalidWorkflowRun
	}
	return nil
}

func IsKnownStepState(value string) bool {
	switch value {
	case StepPending, StepReady, StepRunning, StepFulfilled, StepFailed:
		return true
	default:
		return false
	}
}

type WorkflowEvent struct {
	EventID         string    `json:"event_id"`
	WorkflowRunID   string    `json:"workflow_run_id"`
	Sequence        uint64    `json:"sequence"`
	Type            string    `json:"type"`
	StepID          string    `json:"step_id,omitempty"`
	ChildEpisodeID  string    `json:"child_episode_id,omitempty"`
	WorkflowVersion string    `json:"workflow_version"`
	DefinitionHash  string    `json:"definition_hash"`
	OccurredAt      time.Time `json:"occurred_at"`
	FactsRef        string    `json:"facts_ref"`
	PayloadHash     string    `json:"payload_hash"`
	IdempotencyKey  string    `json:"idempotency_key"`
}

func (e WorkflowEvent) Validate() error {
	if strings.TrimSpace(e.EventID) == "" || strings.TrimSpace(e.WorkflowRunID) == "" || e.Sequence == 0 || strings.TrimSpace(e.Type) == "" || strings.TrimSpace(e.WorkflowVersion) == "" || strings.TrimSpace(e.DefinitionHash) == "" || e.OccurredAt.IsZero() || strings.TrimSpace(e.FactsRef) == "" || strings.TrimSpace(e.PayloadHash) == "" || strings.TrimSpace(e.IdempotencyKey) == "" {
		return ErrWorkflowEventConflict
	}
	return nil
}

type WorkflowStatus struct {
	WorkflowRun     *WorkflowRun                   `json:"workflow_run"`
	Definition      *WorkflowDefinition            `json:"definition"`
	Budget          WorkflowBudgetSnapshot         `json:"budget"`
	Steps           []*WorkflowStepRun             `json:"steps"`
	FinalArtifact   *WorkflowFinalArtifactRef      `json:"final_artifact,omitempty"`
	FinalValidation *invocation.ValidationEvidence `json:"validation,omitempty"`
}

// WorkflowFinalArtifactRef is safe for the default public status projection:
// it contains metadata and integrity material, never the artifact body.
type WorkflowFinalArtifactRef struct {
	DeliveryID      string `json:"delivery_id"`
	PayloadHash     string `json:"payload_hash"`
	ContentType     string `json:"content_type"`
	PaymentIntentID string `json:"payment_intent_id,omitempty"`
	EntitlementRef  string `json:"entitlement_ref,omitempty"`
	Size            int64  `json:"size"`
}

type WorkflowObservability struct {
	WorkflowRunID   string                 `json:"workflow_run_id"`
	State           string                 `json:"state"`
	DefinitionHash  string                 `json:"definition_hash"`
	Budget          WorkflowBudgetSnapshot `json:"budget"`
	Steps           []*WorkflowStepRun     `json:"steps"`
	ChildEpisodeIDs []string               `json:"child_episode_ids"`
}

func normalizeContentType(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

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

func normalizeKeyValues(values []contract.KeyValue) []contract.KeyValue {
	result := make([]contract.KeyValue, 0, len(values))
	for _, value := range values {
		result = append(result, contract.KeyValue{Key: strings.TrimSpace(value.Key), Value: strings.TrimSpace(value.Value)})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Key == result[j].Key {
			return result[i].Value < result[j].Value
		}
		return result[i].Key < result[j].Key
	})
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
