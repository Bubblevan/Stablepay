package workflow

import (
	"errors"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/contract"
)

func validDefinition() WorkflowDefinition {
	now := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	validator := contract.ValidatorRef{Kind: "builtin", Name: "text", Version: "v1"}
	return WorkflowDefinition{
		WorkflowID: "two-step-commerce", Version: "v1", Name: "Two step commerce", Description: "deterministic two-step workflow",
		Input: WorkflowInputSpec{ContentType: "text/plain", Schema: "schema://input"}, Output: WorkflowOutputSpec{ContentType: "text/plain", Schema: "schema://output"}, Currency: "USDC", MaxBudgetMinor: 500, CreatedAt: now, FactsRef: "workflow-definition://two-step-commerce/v1",
		Steps: []WorkflowStepDefinition{
			{StepID: "step-b", StepType: StepAcquireCapability, Capability: CapabilityRequirement{TaskType: "s8-transform", RequiredInputContentType: "text/plain", RequiredOutputContentType: "text/plain", RequiredProtocolVersions: []string{"x402-v1"}}, DependsOn: []string{"step-a"}, InputBinding: WorkflowInputBinding{Source: WorkflowStepOutput, SourceStepID: "step-a", ContentType: "text/plain"}, ExpectedOutput: contract.ExpectedOutput{Schema: "schema://output", ContentType: "text/plain"}, Validator: validator, MaxBudgetMinor: 200, MaxTotalAttempts: 10, MaxPaymentAttempts: 3, MaxDeliveryAttempts: 3, TimeoutSeconds: 60, RequiredInputSchemaRef: "schema://middle"},
			{StepID: "step-a", StepType: StepAcquireCapability, Capability: CapabilityRequirement{TaskType: "s8-source", RequiredInputContentType: "text/plain", RequiredOutputContentType: "text/plain", RequiredProtocolVersions: []string{"x402-v1"}}, InputBinding: WorkflowInputBinding{Source: WorkflowRootInput, ContentType: "text/plain"}, ExpectedOutput: contract.ExpectedOutput{Schema: "schema://middle", ContentType: "text/plain"}, Validator: validator, MaxBudgetMinor: 200, MaxTotalAttempts: 10, MaxPaymentAttempts: 3, MaxDeliveryAttempts: 3, TimeoutSeconds: 60},
		},
	}
}

func TestDefinitionCanonicalHashSortsSteps(t *testing.T) {
	first := validDefinition()
	second := validDefinition()
	second.Steps[0], second.Steps[1] = second.Steps[1], second.Steps[0]
	hashA, err := first.ComputedDefinitionHash()
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := second.ComputedDefinitionHash()
	if err != nil {
		t.Fatal(err)
	}
	if hashA != hashB {
		t.Fatalf("canonical hash changed with step order: %s != %s", hashA, hashB)
	}
}

func TestDefinitionRejectsCycle(t *testing.T) {
	definition := validDefinition()
	definition.Steps[0].DependsOn = []string{"step-a"}
	definition.Steps[1].DependsOn = []string{"step-b"}
	if err := definition.Validate(); err == nil {
		t.Fatal("expected cycle rejection")
	}
}

func TestDefinitionRejectsContentMismatch(t *testing.T) {
	definition := validDefinition()
	definition.Steps[1].Capability.RequiredInputContentType = "application/json"
	if err := definition.Validate(); err == nil {
		t.Fatal("expected content mismatch rejection")
	}
}

func TestDefinitionHasExactlyOneTypedSinkAndOutputStep(t *testing.T) {
	definition := validDefinition()
	output, err := OutputStep(definition)
	if err != nil || output.StepID != "step-b" {
		t.Fatalf("expected step-b as the sole output step, got=%#v err=%v", output, err)
	}

	definition.Steps[0].DependsOn = nil
	definition.Steps[0].InputBinding = WorkflowInputBinding{Source: WorkflowRootInput, ContentType: "text/plain"}
	definition.Steps[0].Capability.TaskType = "independent"
	definition.Steps[0].ExpectedOutput = contract.ExpectedOutput{Schema: "schema://output", ContentType: "text/plain"}
	if err := definition.Validate(); err == nil {
		t.Fatal("expected multiple sinks to be rejected")
	}

	definition = validDefinition()
	definition.Steps[0].ExpectedOutput.Schema = "schema://wrong"
	if err := definition.Validate(); err == nil {
		t.Fatal("expected sink output schema mismatch to be rejected")
	}
}

func TestStableIdentifiersAndArtifactURI(t *testing.T) {
	for _, value := range []string{"step-a", "Step.A", "step:a", "v1:blue"} {
		if err := ValidateIdentifier(value); err != nil {
			t.Fatalf("valid identifier %q rejected: %v", value, err)
		}
	}
	for _, value := range []string{" step-a", "step-a ", "step/a", "step?x", "step#x", "step..x", "", "a\n"} {
		if err := ValidateIdentifier(value); !errors.Is(err, ErrInvalidIdentifier) {
			t.Fatalf("unsafe identifier %q was accepted: %v", value, err)
		}
	}
	ref := ArtifactRef{WorkflowRunID: "wr:Case.1", StepID: "step:a"}
	if ref.URI() != "workflow-artifact://wr:Case.1/step:a" {
		t.Fatalf("artifact URI changed identity: %s", ref.URI())
	}
}

func TestStepSafetyFieldsAndFulfilledProjectionRequirements(t *testing.T) {
	definition := validDefinition()
	definition.Steps[1].AllowCrossMerchantSwitch = true
	definition.Steps[1].RequireParentConfirmationAboveMinor = definition.MaxBudgetMinor + 1
	if err := definition.Validate(); err != nil {
		t.Fatalf("threshold may exceed workflow budget: %v", err)
	}
	definition.Steps[1].RequireParentConfirmationAboveMinor = -1
	if err := definition.Validate(); err == nil {
		t.Fatal("negative parent threshold was accepted")
	}
	step := WorkflowStepRun{WorkflowRunID: "wr:test", StepID: "step-b", State: StepFulfilled, Version: 1}
	if err := step.Validate(); err == nil {
		t.Fatal("fulfilled step without durable output bindings was accepted")
	}
}
