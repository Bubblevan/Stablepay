// Package validator contains the runtime-owned, allowlisted delivery
// validators. It never executes code supplied by an acquisition request.
package validator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/invocation"
)

var (
	ErrValidatorNotAllowed = errors.New("delivery validator is not in the runtime allowlist")
	ErrValidationFailed    = errors.New("delivery validation failed")
)

type ValidationResult struct {
	Valid        bool
	ReasonCode   string
	EvidenceRefs []string
}

type DeliveryValidator interface {
	Validate(context.Context, contract.AcquireCapabilityRequest, invocation.DeliveryArtifact) ValidationResult
}

type Registry struct {
	validators map[string]DeliveryValidator
}

func NewBuiltinRegistry() *Registry {
	return &Registry{validators: map[string]DeliveryValidator{
		"non-empty@v1":               nonEmptyValidator{},
		"content-type@v1":            contentTypeValidator{},
		"json-schema-lite@v1":        expectedFieldsValidator{},
		"expected-fields@v1":         expectedFieldsValidator{},
		"transcript_validator@v1":    transcriptValidator{},
		"transcript_validator.v1@v1": transcriptValidator{},
		"transcript-validator@v1":    transcriptValidator{},
	}}
}

func (r *Registry) Resolve(ref contract.ValidatorRef) (DeliveryValidator, error) {
	if r == nil || strings.ToLower(strings.TrimSpace(ref.Kind)) != "builtin" {
		return nil, ErrValidatorNotAllowed
	}
	key := strings.TrimSpace(ref.Name) + "@" + strings.TrimSpace(ref.Version)
	value, ok := r.validators[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrValidatorNotAllowed, key)
	}
	return value, nil
}

func (r *Registry) Validate(ctx context.Context, ref contract.ValidatorRef, request contract.AcquireCapabilityRequest, artifact invocation.DeliveryArtifact) (ValidationResult, error) {
	value, err := r.Resolve(ref)
	if err != nil {
		return ValidationResult{}, err
	}
	result := value.Validate(ctx, request, artifact)
	if strings.TrimSpace(result.ReasonCode) == "" {
		if result.Valid {
			result.ReasonCode = "VALID"
		} else {
			result.ReasonCode = "INVALID"
		}
	}
	return result, nil
}

type nonEmptyValidator struct{}

func (nonEmptyValidator) Validate(_ context.Context, _ contract.AcquireCapabilityRequest, artifact invocation.DeliveryArtifact) ValidationResult {
	if len(strings.TrimSpace(string(artifact.Body))) == 0 {
		return invalid("EMPTY_PAYLOAD", artifact)
	}
	return valid("NON_EMPTY", artifact)
}

type contentTypeValidator struct{}

func (contentTypeValidator) Validate(_ context.Context, request contract.AcquireCapabilityRequest, artifact invocation.DeliveryArtifact) ValidationResult {
	want := strings.ToLower(strings.TrimSpace(request.ExpectedOutput.ContentType))
	got := strings.ToLower(strings.TrimSpace(artifact.ContentType))
	if want == "" || want == "*/*" || contentTypeMatches(got, want) {
		return valid("CONTENT_TYPE_MATCH", artifact)
	}
	return invalid("CONTENT_TYPE_MISMATCH", artifact)
}

type transcriptValidator struct{}

func (transcriptValidator) Validate(_ context.Context, request contract.AcquireCapabilityRequest, artifact invocation.DeliveryArtifact) ValidationResult {
	if len(strings.TrimSpace(string(artifact.Body))) == 0 {
		return invalid("EMPTY_TRANSCRIPT", artifact)
	}
	want := strings.ToLower(strings.TrimSpace(request.ExpectedOutput.ContentType))
	if want != "" && want != "*/*" && !contentTypeMatches(strings.ToLower(artifact.ContentType), want) {
		return invalid("CONTENT_TYPE_MISMATCH", artifact)
	}
	return valid("TRANSCRIPT_PRESENT", artifact)
}

type expectedFieldsValidator struct{}

func (expectedFieldsValidator) Validate(_ context.Context, request contract.AcquireCapabilityRequest, artifact invocation.DeliveryArtifact) ValidationResult {
	if len(strings.TrimSpace(string(artifact.Body))) == 0 {
		return invalid("EMPTY_JSON", artifact)
	}
	var value map[string]any
	if err := json.Unmarshal(artifact.Body, &value); err != nil {
		return invalid("INVALID_JSON", artifact)
	}
	for _, field := range request.Validator.Config {
		if strings.EqualFold(strings.TrimSpace(field.Key), "required_field") || strings.EqualFold(strings.TrimSpace(field.Key), "expected_field") {
			if _, ok := value[strings.TrimSpace(field.Value)]; !ok {
				return invalid("MISSING_EXPECTED_FIELD", artifact)
			}
		}
	}
	return valid("EXPECTED_FIELDS_PRESENT", artifact)
}

func valid(code string, artifact invocation.DeliveryArtifact) ValidationResult {
	return ValidationResult{Valid: true, ReasonCode: code, EvidenceRefs: []string{artifact.PayloadRef, artifact.PayloadHash}}
}

func invalid(code string, artifact invocation.DeliveryArtifact) ValidationResult {
	return ValidationResult{Valid: false, ReasonCode: code, EvidenceRefs: []string{artifact.PayloadRef, artifact.PayloadHash}}
}

func contentTypeMatches(got, want string) bool {
	got = strings.TrimSpace(strings.Split(got, ";")[0])
	want = strings.TrimSpace(strings.Split(want, ";")[0])
	if got == want || got == "*/*" || want == "*/*" {
		return true
	}
	if strings.HasSuffix(want, "/*") {
		return strings.HasPrefix(got, strings.TrimSuffix(want, "*"))
	}
	return false
}
