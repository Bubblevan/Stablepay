// Package llm defines the provider-neutral S6 model boundary. Implementations
// return proposals only. They do not receive repositories, payment adapters,
// merchant adapters, ledgers, validators, or parent-decision APIs.
package llm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

var (
	ErrLLMUnavailable     = errors.New("LLM provider is unavailable")
	ErrMalformedResponse  = errors.New("LLM response is not valid strict decision JSON")
	ErrInvalidModelOutput = errors.New("LLM output violates the decision proposal schema")
)

type LLMDecisionRequest struct {
	ModelRef    string
	ContextHash string
	Prompt      Prompt
}

type LLMUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

type LLMDecisionResponse struct {
	Provider           string
	ModelRef           string
	RawJSON            []byte
	ResponseReceivedAt time.Time
	Usage              LLMUsage
}

type LLMClient interface {
	GenerateDecision(ctx context.Context, request LLMDecisionRequest) (LLMDecisionResponse, error)
}

type TraceStatus string

const (
	TraceSuccess        TraceStatus = "SUCCESS"
	TraceTransportError TraceStatus = "TRANSPORT_ERROR"
	TraceParseError     TraceStatus = "PARSE_ERROR"
	TraceGuardRejected  TraceStatus = "GUARD_REJECTED"
	TraceFallback       TraceStatus = "FALLBACK_RULE"
)

// ModelDecisionTrace is redacted by construction: raw prompt/response bodies
// are represented by hashes, never persisted here.
type ModelDecisionTrace struct {
	TraceID            string      `json:"trace_id"`
	EpisodeID          string      `json:"episode_id"`
	Provider           string      `json:"provider"`
	ModelRef           string      `json:"model_ref"`
	ContextHash        string      `json:"context_hash"`
	EvidenceRefs       []string    `json:"evidence_refs,omitempty"`
	RequestStartedAt   time.Time   `json:"request_started_at"`
	ResponseReceivedAt time.Time   `json:"response_received_at"`
	RawResponseHash    string      `json:"raw_response_hash,omitempty"`
	ParsedProposalHash string      `json:"parsed_proposal_hash,omitempty"`
	Status             TraceStatus `json:"status"`
	ErrorCode          string      `json:"error_code,omitempty"`
	InputTokens        int         `json:"input_tokens,omitempty"`
	OutputTokens       int         `json:"output_tokens,omitempty"`
	FallbackReason     string      `json:"fallback_reason,omitempty"`
}

func (t ModelDecisionTrace) Clone() *ModelDecisionTrace {
	t.EvidenceRefs = append([]string(nil), t.EvidenceRefs...)
	return &t
}

func (t ModelDecisionTrace) Validate() error {
	if strings.TrimSpace(t.TraceID) == "" || strings.TrimSpace(t.EpisodeID) == "" || strings.TrimSpace(t.Provider) == "" || strings.TrimSpace(t.ModelRef) == "" || strings.TrimSpace(t.ContextHash) == "" || t.RequestStartedAt.IsZero() || t.ResponseReceivedAt.IsZero() || t.ResponseReceivedAt.Before(t.RequestStartedAt) {
		return ErrInvalidModelOutput
	}
	switch t.Status {
	case TraceSuccess, TraceTransportError, TraceParseError, TraceGuardRejected, TraceFallback:
	default:
		return ErrInvalidModelOutput
	}
	return nil
}

type DecisionResult struct {
	Proposal decision.DecisionProposal
	Trace    ModelDecisionTrace
}

type ProviderOption func(*LLMDecisionProvider)

type LLMDecisionProvider struct {
	client       LLMClient
	providerName string
	modelRef     string
	clock        func() time.Time
	ttl          time.Duration
	maxAttempts  int
	idGenerator  func(string) string
}

func NewLLMDecisionProvider(client LLMClient, options ...ProviderOption) *LLMDecisionProvider {
	provider := &LLMDecisionProvider{client: client, providerName: "llm", modelRef: "configured", clock: func() time.Time { return time.Now().UTC() }, ttl: time.Minute, maxAttempts: 2, idGenerator: randomID}
	for _, option := range options {
		option(provider)
	}
	if provider.maxAttempts < 1 {
		provider.maxAttempts = 1
	}
	return provider
}

func WithProviderName(name string) ProviderOption {
	return func(p *LLMDecisionProvider) {
		if strings.TrimSpace(name) != "" {
			p.providerName = strings.TrimSpace(name)
		}
	}
}
func WithModelRef(model string) ProviderOption {
	return func(p *LLMDecisionProvider) {
		if strings.TrimSpace(model) != "" {
			p.modelRef = strings.TrimSpace(model)
		}
	}
}
func WithProviderClock(clock func() time.Time) ProviderOption {
	return func(p *LLMDecisionProvider) {
		if clock != nil {
			p.clock = clock
		}
	}
}
func WithProviderTTL(ttl time.Duration) ProviderOption {
	return func(p *LLMDecisionProvider) {
		if ttl > 0 {
			p.ttl = ttl
		}
	}
}
func WithProviderMaxAttempts(attempts int) ProviderOption {
	return func(p *LLMDecisionProvider) {
		if attempts > 0 {
			p.maxAttempts = attempts
		}
	}
}
func WithProposalIDGenerator(generator func(string) string) ProviderOption {
	return func(p *LLMDecisionProvider) {
		if generator != nil {
			p.idGenerator = generator
		}
	}
}

func (p *LLMDecisionProvider) Propose(ctx context.Context, input DecisionContext) (decision.DecisionProposal, error) {
	result, err := p.ProposeWithTrace(ctx, input)
	return result.Proposal, err
}

func (p *LLMDecisionProvider) ProposeWithTrace(ctx context.Context, input DecisionContext) (DecisionResult, error) {
	if p == nil || p.client == nil {
		return DecisionResult{}, ErrLLMUnavailable
	}
	if err := input.Validate(); err != nil {
		return DecisionResult{}, err
	}
	contextHash, err := input.Hash()
	if err != nil {
		return DecisionResult{}, err
	}
	prompt, err := BuildPrompt(input)
	if err != nil {
		return DecisionResult{}, err
	}
	started := p.clock().UTC()
	traceValue := ModelDecisionTrace{TraceID: p.idGenerator("llm-trace"), EpisodeID: input.Episode.EpisodeID, Provider: p.providerName, ModelRef: p.modelRef, ContextHash: contextHash, EvidenceRefs: contextEvidenceRefs(input), RequestStartedAt: started, Status: TraceTransportError}
	var lastErr error
	var raw []byte
	for attempt := 0; attempt < p.maxAttempts; attempt++ {
		if err := contextErr(ctx); err != nil {
			lastErr = err
			break
		}
		response, callErr := p.client.GenerateDecision(ctx, LLMDecisionRequest{ModelRef: p.modelRef, ContextHash: contextHash, Prompt: prompt})
		raw = append([]byte(nil), response.RawJSON...)
		if len(raw) > 0 {
			traceValue.RawResponseHash = hashBytes(raw)
		}
		if !response.ResponseReceivedAt.IsZero() {
			traceValue.ResponseReceivedAt = response.ResponseReceivedAt.UTC()
		} else {
			traceValue.ResponseReceivedAt = p.clock().UTC()
		}
		traceValue.InputTokens = response.Usage.InputTokens
		traceValue.OutputTokens = response.Usage.OutputTokens
		if response.Provider != "" {
			traceValue.Provider = response.Provider
		}
		if response.ModelRef != "" {
			traceValue.ModelRef = response.ModelRef
		}
		if callErr != nil {
			lastErr = callErr
			traceValue.Status = TraceTransportError
			traceValue.ErrorCode = errorCode(callErr)
			continue
		}
		wire, parseErr := decodeStrictProposal(raw)
		if parseErr != nil {
			lastErr = parseErr
			traceValue.Status = TraceParseError
			traceValue.ErrorCode = errorCode(parseErr)
			continue
		}
		proposal, buildErr := p.authoritativeProposal(input, wire, started)
		if buildErr != nil {
			lastErr = buildErr
			traceValue.Status = TraceParseError
			traceValue.ErrorCode = errorCode(buildErr)
			continue
		}
		traceValue.Status = TraceSuccess
		traceValue.ErrorCode = ""
		traceValue.ParsedProposalHash = hashProposal(proposal)
		return DecisionResult{Proposal: proposal, Trace: traceValue}, nil
	}
	if traceValue.ResponseReceivedAt.IsZero() {
		traceValue.ResponseReceivedAt = p.clock().UTC()
	}
	if lastErr == nil {
		lastErr = ErrLLMUnavailable
	}
	return DecisionResult{Trace: traceValue}, lastErr
}

type wireProposal struct {
	ProposedAction trace.ActionType         `json:"proposed_action"`
	CandidateSetID string                   `json:"candidate_set_id,omitempty"`
	Target         *decision.ProposalTarget `json:"target,omitempty"`
	EvidenceRefs   []string                 `json:"evidence_refs"`
	Rationale      string                   `json:"rationale"`
	Confidence     float64                  `json:"confidence"`
}

func decodeStrictProposal(raw []byte) (wireProposal, error) {
	if len(raw) == 0 || len(raw) > 16<<10 {
		return wireProposal{}, ErrMalformedResponse
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var wire wireProposal
	if err := decoder.Decode(&wire); err != nil {
		return wireProposal{}, fmt.Errorf("%w: %v", ErrMalformedResponse, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return wireProposal{}, ErrMalformedResponse
	}
	if err := validateWireProposal(wire); err != nil {
		return wireProposal{}, err
	}
	return wire, nil
}

func validateWireProposal(w wireProposal) error {
	if !decision.ProviderAllowedAction(w.ProposedAction) && w.ProposedAction != trace.ActionSelectMerchant {
		return fmt.Errorf("%w: action %s", ErrInvalidModelOutput, w.ProposedAction)
	}
	if len(w.EvidenceRefs) == 0 || len(w.EvidenceRefs) > 16 {
		return fmt.Errorf("%w: evidence_refs", ErrInvalidModelOutput)
	}
	seen := make(map[string]struct{}, len(w.EvidenceRefs))
	for _, ref := range w.EvidenceRefs {
		if !evidenceRefSyntax(ref) || len(ref) > 512 {
			return fmt.Errorf("%w: evidence_ref", ErrInvalidModelOutput)
		}
		if _, ok := seen[ref]; ok {
			return fmt.Errorf("%w: duplicate evidence_ref", ErrInvalidModelOutput)
		}
		seen[ref] = struct{}{}
	}
	if len(w.CandidateSetID) > 256 || len(w.Rationale) > 4096 || math.IsNaN(w.Confidence) || math.IsInf(w.Confidence, 0) || w.Confidence < 0 || w.Confidence > 1 {
		return ErrInvalidModelOutput
	}
	requiresTarget := w.ProposedAction == trace.ActionSelectMerchant || w.ProposedAction == trace.ActionSwitchMerchant
	if requiresTarget {
		if strings.TrimSpace(w.CandidateSetID) == "" || w.Target == nil || strings.TrimSpace(w.Target.MerchantDID) == "" || strings.TrimSpace(w.Target.CapabilityID) == "" {
			return ErrInvalidModelOutput
		}
	} else if w.Target != nil {
		return ErrInvalidModelOutput
	}
	if w.Target != nil {
		for _, value := range []string{w.Target.MerchantDID, w.Target.CapabilityID, w.Target.CatalogVersion, w.Target.CatalogSnapshotHash, w.Target.CatalogSnapshotRef} {
			if len(value) > 512 || strings.ContainsAny(value, "\r\n") {
				return ErrInvalidModelOutput
			}
		}
	}
	return nil
}

func (p *LLMDecisionProvider) authoritativeProposal(input DecisionContext, wire wireProposal, started time.Time) (decision.DecisionProposal, error) {
	now := p.clock().UTC()
	if now.Before(started) {
		now = started
	}
	// TTL starts when the model request starts. A slow or delayed response must
	// not receive a fresh lifetime merely because parsing completed later.
	expires := started.Add(p.ttl)
	if expires.After(input.Episode.DeadlineAt) {
		expires = input.Episode.DeadlineAt
	}
	if !expires.After(now) {
		return decision.DecisionProposal{}, decision.ErrProposalExpired
	}
	proposal := decision.DecisionProposal{ProposalID: p.idGenerator("llm-proposal"), EpisodeID: input.Episode.EpisodeID, BasedOnEventSequence: input.CurrentEventSequence, ProposedAction: wire.ProposedAction, CandidateSetID: strings.TrimSpace(wire.CandidateSetID), Target: wire.Target, Rationale: strings.TrimSpace(wire.Rationale), EvidenceRefs: append([]string(nil), wire.EvidenceRefs...), Confidence: wire.Confidence, ModelRef: p.modelRef, CreatedAt: started, ExpiresAt: expires}
	if err := proposal.Validate(); err != nil {
		return decision.DecisionProposal{}, err
	}
	return proposal, nil
}

func contextEvidenceRefs(input DecisionContext) []string {
	refs := make([]string, 0, len(input.RetrievedEvidence)+2)
	for _, record := range input.RetrievedEvidence {
		refs = append(refs, record.EvidenceRef)
	}
	if input.Recovery != nil {
		refs = append(refs, input.Recovery.FactsRef, input.Recovery.PayloadHash)
	}
	if input.CandidateSet.FactsRef != "" {
		refs = append(refs, input.CandidateSet.FactsRef, input.CandidateSet.PayloadHash)
	}
	return uniqueStrings(refs)
}

func evidenceRefSyntax(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.ContainsAny(ref, "\r\n\t ") || strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return false
	}
	if len(ref) == len("sha256:")+64 && strings.HasPrefix(ref, "sha256:") {
		for _, char := range ref[len("sha256:"):] {
			if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
				return false
			}
		}
		return true
	}
	for _, prefix := range []string{"evidence://", "recovery://", "candidate-set://", "invocation://", "validation://", "parent-approval://", "parent-decision://", "budget-amendment://"} {
		if strings.HasPrefix(ref, prefix) && len(ref) > len(prefix) {
			return true
		}
	}
	return false
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func hashBytes(value []byte) string {
	if len(value) == 0 {
		return ""
	}
	return evidenceHash(value)
}

func evidenceHash(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func hashProposal(proposal decision.DecisionProposal) string {
	encoded, _ := json.Marshal(proposal)
	return hashBytes(encoded)
}

func randomID(prefix string) string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return prefix + ":fallback"
	}
	return prefix + ":" + hex.EncodeToString(value[:])
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "TIMEOUT"
	case errors.Is(err, context.Canceled):
		return "CANCELED"
	case errors.Is(err, ErrMalformedResponse):
		return "MALFORMED_JSON"
	case errors.Is(err, ErrInvalidModelOutput):
		return "SCHEMA_INVALID"
	default:
		return "TRANSPORT_ERROR"
	}
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
