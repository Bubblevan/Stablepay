package artifact_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/workflow"
	artifactresolver "github.com/stablepay/commerce-runtime/internal/workflow/artifact"
)

type artifactStoreFunc func(context.Context, string) (*invocation.DeliveryArtifact, error)

func (f artifactStoreFunc) GetDeliveryArtifact(ctx context.Context, id string) (*invocation.DeliveryArtifact, error) {
	return f(ctx, id)
}

func TestResolverValidatesRunScopedArtifactAndBoundsPayload(t *testing.T) {
	ctx := context.Background()
	store := repository.NewInMemoryStore()
	now := time.Now().UTC().Truncate(time.Millisecond)
	definition, err := testDefinition(now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDefinition(ctx, &definition); err != nil {
		t.Fatal(err)
	}
	runID := "wr:resolver"
	stepID := "source"
	body := []byte("hello")
	hash := invocation.PayloadHash(body)
	run := &workflow.WorkflowRun{WorkflowRunID: runID, RequestID: "resolver-request", WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version, DefinitionHash: definition.DefinitionHash, RequesterDID: "did:agent:resolver", State: workflow.WorkflowFulfilled, Input: contract.Input{URI: "object://root", ContentType: "text/plain"}, Budget: workflow.WorkflowBudgetSnapshot{Currency: "USDC", BudgetLimitMinor: 100, AvailableMinor: 100}, DeadlineAt: now.Add(time.Hour), Version: 1, CreatedAt: now, UpdatedAt: now}
	ref := (workflow.ArtifactRef{WorkflowRunID: runID, StepID: stepID}).URI()
	step := &workflow.WorkflowStepRun{WorkflowRunID: runID, StepID: stepID, State: workflow.StepFulfilled, ChildEpisodeID: "episode:resolver", OutputRef: ref, OutputHash: hash, ContentType: "text/plain", DeliveryID: "delivery:resolver", Version: 1, CompletedAt: &now}
	event := &workflow.WorkflowEvent{EventID: "event:resolver", WorkflowRunID: runID, Sequence: 1, Type: workflow.EventWorkflowCreated, WorkflowVersion: definition.Version, DefinitionHash: definition.DefinitionHash, OccurredAt: now, FactsRef: "workflow://resolver", PayloadHash: hash, IdempotencyKey: "event:resolver"}
	if err := store.CreateAggregate(ctx, run, []*workflow.WorkflowStepRun{step}, event); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDeliveryArtifact(ctx, &invocation.DeliveryArtifact{DeliveryID: step.DeliveryID, EpisodeID: "episode:resolver", InvocationID: "invocation:resolver", MerchantDID: "did:merchant:resolver", CapabilityID: "source", ContentType: "text/plain", PayloadRef: "merchant-response://resolver", PayloadHash: hash, Body: body, Attempt: 1, HTTPStatus: 200, ReceivedAt: now}); err != nil {
		t.Fatal(err)
	}
	resolver := artifactresolver.NewResolver(store, store)
	resolvedBody, resolvedContentType, resolvedHash, err := resolver.ResolveInput(ctx, contract.Input{Ref: ref, SHA256: strings.TrimPrefix(hash, "sha256:"), ContentType: "text/plain"})
	if err != nil || string(resolvedBody) != "hello" || resolvedContentType != "text/plain" || invocation.PayloadHash(resolvedBody) != hash || len(resolvedHash) != 32 {
		t.Fatalf("valid workflow artifact was not materialized: body=%q content_type=%q hash=%x err=%v", resolvedBody, resolvedContentType, resolvedHash, err)
	}

	badBody, contentType, payloadHash, err := resolver.ResolveInput(ctx, contract.Input{Ref: ref, SHA256: strings.Repeat("0", 64), ContentType: "text/plain"})
	if !errors.Is(err, artifactresolver.ErrHashMismatch) || badBody != nil || contentType != "" || payloadHash != nil {
		t.Fatalf("hash mismatch returned unsafe or untyped result: body=%q content_type=%q hash=%x err=%v", badBody, contentType, payloadHash, err)
	}
	foreign, foreignType, foreignHash, err := resolver.ResolveInput(ctx, contract.Input{Ref: "workflow-artifact://wr:other/source", SHA256: strings.TrimPrefix(hash, "sha256:"), ContentType: "text/plain"})
	if !errors.Is(err, artifactresolver.ErrRunMismatch) || foreign != nil || foreignType != "" || foreignHash != nil {
		t.Fatalf("foreign run reference was not rejected: body=%q content_type=%q hash=%x err=%v", foreign, foreignType, foreignHash, err)
	}
	wrongType, wrongContentType, wrongHash, err := resolver.ResolveInput(ctx, contract.Input{Ref: ref, SHA256: strings.TrimPrefix(hash, "sha256:"), ContentType: "application/json"})
	if !errors.Is(err, artifactresolver.ErrContentTypeMismatch) || wrongType != nil || wrongContentType != "" || wrongHash != nil {
		t.Fatalf("content type mismatch was not rejected: body=%q content_type=%q hash=%x err=%v", wrongType, wrongContentType, wrongHash, err)
	}

	overflow := append([]byte(nil), make([]byte, artifactresolver.MaxPayloadBytes+1)...)
	overflowResolver := artifactresolver.NewResolver(store, artifactStoreFunc(func(context.Context, string) (*invocation.DeliveryArtifact, error) {
		return &invocation.DeliveryArtifact{DeliveryID: step.DeliveryID, EpisodeID: "episode:resolver", PayloadHash: hash, Body: overflow, ContentType: "text/plain"}, nil
	}))
	oversized, err := overflowResolver.Resolve(ctx, workflow.ArtifactRef{WorkflowRunID: runID, StepID: stepID}, []byte(hash), "text/plain")
	if !errors.Is(err, artifactresolver.ErrPayloadTooLarge) || oversized != nil {
		t.Fatalf("oversized payload was not rejected without body: artifact=%#v err=%v", oversized, err)
	}
}

func TestResolverRejectsUnsafeRefsAndCrossEpisodeOrDeliveryArtifacts(t *testing.T) {
	for _, value := range []string{
		"workflow-artifact://wr:resolver/step/a",
		"workflow-artifact://wr:resolver/step?x",
		"workflow-artifact://wr:resolver/step#x",
		"workflow-artifact://wr:resolver/..",
		"workflow-artifact://wr:resolver/ step",
	} {
		if _, err := artifactresolver.ParseRef(value); !errors.Is(err, artifactresolver.ErrInvalidRef) {
			t.Fatalf("unsafe artifact reference %q was accepted: %v", value, err)
		}
	}

	ctx := context.Background()
	store := repository.NewInMemoryStore()
	now := time.Now().UTC().Truncate(time.Millisecond)
	definition, err := testDefinition(now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDefinition(ctx, &definition); err != nil {
		t.Fatal(err)
	}
	runID, stepID := "wr:provenance", "source"
	body := []byte("hello")
	hash := invocation.PayloadHash(body)
	ref := (workflow.ArtifactRef{WorkflowRunID: runID, StepID: stepID}).URI()
	run := &workflow.WorkflowRun{WorkflowRunID: runID, RequestID: "provenance-request", WorkflowID: definition.WorkflowID, WorkflowVersion: definition.Version, DefinitionHash: definition.DefinitionHash, RequesterDID: "did:agent:resolver", State: workflow.WorkflowFulfilled, Input: contract.Input{URI: "object://root", ContentType: "text/plain"}, Budget: workflow.WorkflowBudgetSnapshot{Currency: "USDC", BudgetLimitMinor: 100, AvailableMinor: 100}, DeadlineAt: now.Add(time.Hour), Version: 1, CreatedAt: now, UpdatedAt: now}
	step := &workflow.WorkflowStepRun{WorkflowRunID: runID, StepID: stepID, State: workflow.StepFulfilled, ChildEpisodeID: "episode:expected", OutputRef: ref, OutputHash: hash, ContentType: "text/plain", DeliveryID: "delivery:expected", Version: 1, CompletedAt: &now}
	event := &workflow.WorkflowEvent{EventID: "event:provenance", WorkflowRunID: runID, Sequence: 1, Type: workflow.EventWorkflowCreated, WorkflowVersion: definition.Version, DefinitionHash: definition.DefinitionHash, OccurredAt: now, FactsRef: "workflow://provenance", PayloadHash: hash, IdempotencyKey: "event:provenance"}
	if err := store.CreateAggregate(ctx, run, []*workflow.WorkflowStepRun{step}, event); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		delivery string
		episode  string
	}{
		{name: "wrong delivery", delivery: "delivery:other", episode: "episode:expected"},
		{name: "wrong episode", delivery: "delivery:expected", episode: "episode:other"},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := artifactresolver.NewResolver(store, artifactStoreFunc(func(context.Context, string) (*invocation.DeliveryArtifact, error) {
				return &invocation.DeliveryArtifact{DeliveryID: test.delivery, EpisodeID: test.episode, PayloadHash: hash, Body: body, ContentType: "text/plain"}, nil
			}))
			resolved, err := resolver.Resolve(ctx, workflow.ArtifactRef{WorkflowRunID: runID, StepID: stepID}, []byte(hash), "text/plain")
			if !errors.Is(err, artifactresolver.ErrArtifactProvenanceMismatch) || resolved != nil {
				t.Fatalf("provenance mismatch returned body or wrong error: resolved=%#v err=%v", resolved, err)
			}
		})
	}
}

func testDefinition(now time.Time) (workflow.WorkflowDefinition, error) {
	definition := workflow.WorkflowDefinition{WorkflowID: "resolver-workflow", Version: "v1", Name: "resolver", Input: workflow.WorkflowInputSpec{ContentType: "text/plain", Schema: "schema://input"}, Output: workflow.WorkflowOutputSpec{ContentType: "text/plain", Schema: "schema://output"}, Currency: "USDC", MaxBudgetMinor: 100, CreatedAt: now, FactsRef: "workflow-definition://resolver-workflow/v1", Steps: []workflow.WorkflowStepDefinition{{StepID: "source", StepType: workflow.StepAcquireCapability, Capability: workflow.CapabilityRequirement{TaskType: "source", RequiredInputContentType: "text/plain", RequiredOutputContentType: "text/plain", RequiredProtocolVersions: []string{contract.DefaultProtocolVersion}}, InputBinding: workflow.WorkflowInputBinding{Source: workflow.WorkflowRootInput, ContentType: "text/plain"}, ExpectedOutput: contract.ExpectedOutput{Schema: "schema://output", ContentType: "text/plain"}, Validator: contract.ValidatorRef{Kind: "builtin", Name: "non-empty", Version: "v1"}, MaxBudgetMinor: 100, MaxTotalAttempts: 1, MaxPaymentAttempts: 1, MaxDeliveryAttempts: 1, TimeoutSeconds: 60}}}
	return definition.WithComputedHash()
}
