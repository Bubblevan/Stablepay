// Package artifact resolves immutable Workflow step output references into
// bounded, already-validated delivery payloads for the existing merchant
// adapter boundary.
package artifact

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/workflow"
)

const MaxPayloadBytes = invocation.MaxStoredPayloadBytes

var (
	ErrInvalidRef          = errors.New("invalid workflow artifact reference")
	ErrUnknownScheme       = errors.New("unknown workflow artifact reference scheme")
	ErrRunMismatch         = errors.New("workflow artifact is outside the requested workflow run")
	ErrStepNotFulfilled    = errors.New("workflow artifact step is not fulfilled")
	ErrHashMismatch        = errors.New("workflow artifact payload hash mismatch")
	ErrContentTypeMismatch = errors.New("workflow artifact content type mismatch")
	ErrPayloadTooLarge     = errors.New("workflow artifact payload exceeds the bounded limit")
	ErrArtifactNotFound    = errors.New("workflow delivery artifact was not found")
)

type DeliveryArtifactStore interface {
	GetDeliveryArtifact(context.Context, string) (*invocation.DeliveryArtifact, error)
}

// WorkflowArtifactResolver is the data-plane boundary between a Workflow
// step's internal output reference and a merchant invocation payload.
type WorkflowArtifactResolver interface {
	Resolve(context.Context, workflow.ArtifactRef, []byte, string) (*ResolvedArtifact, error)
}

type ResolvedArtifact struct {
	DeliveryID  string
	PayloadHash []byte
	ContentType string
	Body        []byte
	Size        int64
}

type Resolver struct {
	Workflows workflow.Repository
	Artifacts DeliveryArtifactStore
	MaxBytes  int64
}

func NewResolver(workflows workflow.Repository, artifacts DeliveryArtifactStore) *Resolver {
	return &Resolver{Workflows: workflows, Artifacts: artifacts, MaxBytes: MaxPayloadBytes}
}

func ParseRef(value string) (workflow.ArtifactRef, error) {
	value = strings.TrimSpace(value)
	const prefix = "workflow-artifact://"
	if !strings.HasPrefix(value, prefix) {
		if strings.Contains(value, "://") {
			return workflow.ArtifactRef{}, ErrUnknownScheme
		}
		return workflow.ArtifactRef{}, ErrInvalidRef
	}
	parts := strings.Split(strings.TrimPrefix(value, prefix), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" || strings.Contains(parts[0], "?") || strings.Contains(parts[1], "?") {
		return workflow.ArtifactRef{}, ErrInvalidRef
	}
	return workflow.ArtifactRef{WorkflowRunID: parts[0], StepID: parts[1]}, nil
}

func (r *Resolver) Resolve(ctx context.Context, ref workflow.ArtifactRef, expectedHash []byte, expectedContentType string) (*ResolvedArtifact, error) {
	if r == nil || r.Workflows == nil || r.Artifacts == nil || strings.TrimSpace(ref.WorkflowRunID) == "" || strings.TrimSpace(ref.StepID) == "" {
		return nil, ErrInvalidRef
	}
	expectedDigest, err := normalizeExpectedHash(expectedHash)
	if err != nil {
		return nil, err
	}
	expectedType := strings.ToLower(strings.TrimSpace(expectedContentType))
	if expectedType == "" {
		return nil, ErrContentTypeMismatch
	}
	run, err := r.Workflows.GetRun(ctx, ref.WorkflowRunID)
	if err != nil || run == nil {
		return nil, ErrRunMismatch
	}
	step, err := r.Workflows.GetStepRun(ctx, ref.WorkflowRunID, ref.StepID)
	if err != nil || step == nil {
		return nil, ErrArtifactNotFound
	}
	if step.WorkflowRunID != run.WorkflowRunID || step.OutputRef != ref.URI() {
		return nil, ErrRunMismatch
	}
	if step.State != workflow.StepFulfilled || step.DeliveryID == "" {
		return nil, ErrStepNotFulfilled
	}
	artifact, err := r.Artifacts.GetDeliveryArtifact(ctx, step.DeliveryID)
	if err != nil || artifact == nil {
		return nil, ErrArtifactNotFound
	}
	if artifact.EpisodeID == "" || int64(len(artifact.Body)) > r.maxBytes() {
		if int64(len(artifact.Body)) > r.maxBytes() {
			return nil, ErrPayloadTooLarge
		}
		return nil, ErrArtifactNotFound
	}
	actualHash, err := digestBytes(artifact.PayloadHash)
	if err != nil || !equalBytes(actualHash, expectedDigest) || !equalBytes(actualHash, digestForBody(artifact.Body)) || step.OutputHash != artifact.PayloadHash {
		return nil, ErrHashMismatch
	}
	if strings.ToLower(strings.TrimSpace(artifact.ContentType)) != expectedType || strings.ToLower(strings.TrimSpace(step.ContentType)) != expectedType {
		return nil, ErrContentTypeMismatch
	}
	body := append([]byte(nil), artifact.Body...)
	return &ResolvedArtifact{DeliveryID: artifact.DeliveryID, PayloadHash: actualHash, ContentType: artifact.ContentType, Body: body, Size: int64(len(body))}, nil
}

// ResolveInput adapts the typed workflow resolver to the application layer.
// Non-workflow input references deliberately return an empty payload so old
// merchant flows remain unchanged.
func (r *Resolver) ResolveInput(ctx context.Context, input contract.Input) ([]byte, string, []byte, error) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(input.Ref)), "workflow-artifact://") {
		return nil, "", nil, nil
	}
	ref, err := ParseRef(input.Ref)
	if err != nil {
		return nil, "", nil, err
	}
	resolved, err := r.Resolve(ctx, ref, []byte(input.SHA256), input.ContentType)
	if err != nil {
		return nil, "", nil, err
	}
	return resolved.Body, resolved.ContentType, resolved.PayloadHash, nil
}

func (r *Resolver) maxBytes() int64 {
	if r.MaxBytes <= 0 {
		return MaxPayloadBytes
	}
	return r.MaxBytes
}

func normalizeExpectedHash(value []byte) ([]byte, error) {
	if len(value) == 0 {
		return nil, ErrHashMismatch
	}
	if len(value) == 32 {
		return append([]byte(nil), value...), nil
	}
	return digestBytes(string(value))
}

func digestBytes(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(strings.ToLower(value), "sha256:")
	if len(value) != 64 {
		return nil, fmt.Errorf("%w: expected sha256 digest", ErrHashMismatch)
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return nil, fmt.Errorf("%w: invalid sha256 digest", ErrHashMismatch)
	}
	return decoded, nil
}

func digestForBody(body []byte) []byte {
	digest, err := digestBytes(invocation.PayloadHash(body))
	if err != nil {
		return nil
	}
	return digest
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
