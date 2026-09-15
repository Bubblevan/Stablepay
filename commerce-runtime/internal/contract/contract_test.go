package contract

import (
	"errors"
	"testing"
	"time"
)

func validRequest() AcquireCapabilityRequest {
	deadline := time.Date(2099, 9, 15, 13, 0, 0, 0, time.UTC)
	return AcquireCapabilityRequest{
		RequestID: "acr_01", ParentSessionID: "session_01", RequesterDID: "did:stablepay:agent",
		AcquisitionGoal: AcquisitionGoal{
			TaskType: "transcription", Description: "transcribe the audio",
			SemanticConstraints: []KeyValue{{Key: "language", Value: "zh"}, {Key: "coverage", Value: "0.95"}},
		},
		Input: Input{URI: " object://audio/01.mp3 ", ContentType: "Audio/MPEG", SHA256: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
		Constraints: Constraints{
			BudgetLimitMinor: 1000, Currency: "usdc", DeadlineAt: deadline,
			MaxTotalAttempts: 4, MaxPaymentAttempts: 2, MaxDeliveryAttempts: 2,
		},
		ExpectedOutput: ExpectedOutput{Schema: "transcript", ContentType: "text/plain", SemanticConstraints: []KeyValue{{Key: "language", Value: "zh"}}},
		Validator:      ValidatorRef{Kind: "BUILTIN", Name: "transcript_validator", Version: "v1", Config: []KeyValue{{Key: "require_non_empty", Value: "true"}}},
	}
}

func TestValidateRejectsInvalidMoney(t *testing.T) {
	request := validRequest()
	request.Constraints.BudgetLimitMinor = -1
	if !errors.Is(request.Validate(), ErrInvalidMoney) {
		t.Fatalf("expected invalid money, got %v", request.Validate())
	}
}

func TestValidateRejectsExpiredDeadline(t *testing.T) {
	request := validRequest()
	now := time.Date(2099, 9, 15, 13, 0, 1, 0, time.UTC)
	if !errors.Is(request.ValidateAt(now), ErrInvalidDeadline) {
		t.Fatalf("expected invalid deadline, got %v", request.ValidateAt(now))
	}
}

func TestValidateIsStructuralOnly(t *testing.T) {
	request := validRequest()
	request.Constraints.DeadlineAt = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := request.Validate(); err != nil {
		t.Fatalf("structurally valid request rejected because of wall-clock time: %v", err)
	}
}

func TestValidateAtRequiresExplicitTime(t *testing.T) {
	if err := validRequest().ValidateAt(time.Time{}); !errors.Is(err, ErrInvalidValidationTime) {
		t.Fatalf("expected explicit validation time error, got %v", err)
	}
}

func TestValidateRejectsInvalidAttempts(t *testing.T) {
	request := validRequest()
	request.Constraints.MaxPaymentAttempts = request.Constraints.MaxTotalAttempts + 1
	if !errors.Is(request.Validate(), ErrInvalidAttempts) {
		t.Fatalf("expected invalid attempts, got %v", request.Validate())
	}
}

func TestCanonicalNormalizationIsDeterministic(t *testing.T) {
	first := validRequest()
	second := validRequest()
	deadline := time.Date(2099, 9, 15, 13, 0, 0, 0, time.UTC)
	first.Constraints.DeadlineAt = deadline
	second.Constraints.DeadlineAt = deadline
	second.RequestID = " acr_01 "
	second.Input.URI = "object://audio/01.mp3"
	second.Constraints.Currency = "USDC"
	second.AcquisitionGoal.SemanticConstraints = []KeyValue{{Key: "coverage", Value: "0.95"}, {Key: "language", Value: "zh"}}
	second.Validator.Config = []KeyValue{{Key: "require_non_empty", Value: "true"}}
	firstSnapshot, err := first.CanonicalSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	secondSnapshot, err := second.CanonicalSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if string(firstSnapshot) != string(secondSnapshot) {
		t.Fatalf("normalized snapshots differ:\n%s\n%s", firstSnapshot, secondSnapshot)
	}
	firstHash, err := first.SnapshotHash()
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := second.SnapshotHash()
	if err != nil {
		t.Fatal(err)
	}
	if firstHash != secondHash {
		t.Fatalf("same contract produced different hash: %s != %s", firstHash, secondHash)
	}
	second.AcquisitionGoal.Description = "a meaningfully different task"
	differentHash, err := second.SnapshotHash()
	if err != nil {
		t.Fatal(err)
	}
	if firstHash == differentHash {
		t.Fatal("meaningfully different contract produced the same hash")
	}
}

func TestNormalizeUsesExplicitDefaultProtocol(t *testing.T) {
	request := validRequest()
	request.Constraints.SupportedProtocolVersions = nil
	normalized, err := request.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if len(normalized.Constraints.SupportedProtocolVersions) != 1 || normalized.Constraints.SupportedProtocolVersions[0] != DefaultProtocolVersion {
		t.Fatalf("unexpected default protocols: %#v", normalized.Constraints.SupportedProtocolVersions)
	}
}

func TestValidatorOnlyAcceptsBuiltinReference(t *testing.T) {
	request := validRequest()
	request.Validator.Kind = "code"
	if !errors.Is(request.Validate(), ErrInvalidValidator) {
		t.Fatalf("expected invalid validator, got %v", request.Validate())
	}
}
