package catalog

import (
	"reflect"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/contract"
)

func testCapability(merchant, capability, payee, task string, price int64, version string, now time.Time) *MerchantCapability {
	return &MerchantCapability{MerchantDID: merchant, CapabilityID: capability, PayeeDID: payee,
		Name: capability, Description: "trusted capability fact", TaskTypes: []string{task}, SemanticTags: []string{"language=en"},
		InvokeEndpoint: EndpointRef{Ref: "merchant://" + merchant + "/" + capability}, QuoteEndpoint: EndpointRef{Ref: "quote://" + merchant + "/" + capability},
		InputSchemaRef: "schema:input", OutputSchemaRef: "schema:output", InputContentTypes: []string{"audio/mpeg"}, OutputContentTypes: []string{"text/plain"},
		SupportedProtocolVersions: []string{"x402-v1"}, SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed",
		PriceHintMinor: &price, PriceHintCurrency: "USDC", Status: StatusActive, Availability: AvailabilityAvailable,
		CatalogVersion: version, Source: "test", SourceRef: "fixture:" + merchant, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)}
}

func testQuery(now time.Time) DiscoveryQuery {
	return DiscoveryQuery{TaskType: "transcription", InputContentType: "audio/mpeg", ExpectedOutputContentType: "text/plain",
		SupportedProtocolVersions: []string{"x402-v1"}, Currency: "USDC", BudgetLimitMinor: 10, Deadline: now.Add(time.Hour),
		SemanticConstraints: []contract.KeyValue{{Key: "language", Value: "en"}}}
}

func TestBuildCandidateSetFiltersHardConstraintsAndRanksDeterministically(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	a := testCapability("did:merchant:a", "transcription", "did:solana:payee-a", "transcription", 8, "v1", now)
	b := testCapability("did:merchant:b", "transcription", "did:solana:payee-b", "transcription", 12, "v1", now)
	c := testCapability("did:merchant:c", "image", "did:solana:payee-c", "image-generation", 5, "v1", now)
	set, err := BuildCandidateSet("cs-1", "episode-1", "request-1", testQuery(now), []*MerchantCapability{c, b, a}, now, now.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Candidates) != 1 || set.Candidates[0].MerchantDID != a.MerchantDID {
		t.Fatalf("unexpected hard-filtered candidates: %#v", set.Candidates)
	}
	if set.Candidates[0].PayeeDID != a.PayeeDID || set.Candidates[0].CatalogVersion != "v1" || set.PayloadHash == "" || set.FactsRef == "" {
		t.Fatalf("candidate did not retain authoritative catalog facts: %#v", set.Candidates[0])
	}
	if err := set.ValidateAt(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	second, err := BuildCandidateSet("cs-1", "episode-1", "request-1", testQuery(now), []*MerchantCapability{a, b, c}, now, now.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(set.Candidates, second.Candidates) || set.PayloadHash != second.PayloadHash {
		t.Fatalf("same contract/catalog snapshot produced a different CandidateSet: %#v vs %#v", set, second)
	}
}

func TestCapabilityCanonicalSnapshotSeparatesCapabilityAndPayee(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	capability := testCapability("did:merchant:acme", "transcription-premium", "did:solana:payee-wallet", "transcription", 8, "v1", now)
	if err := capability.Validate(); err != nil {
		t.Fatal(err)
	}
	hash, err := capability.SnapshotHash()
	if err != nil {
		t.Fatal(err)
	}
	if capability.CapabilityID == capability.PayeeDID || hash == "" || capability.SnapshotRef() == "" {
		t.Fatalf("identity boundary was not retained: %#v", capability)
	}
	changed := capability.Clone()
	changed.PayeeDID = "did:solana:new-payee"
	changedHash, err := changed.SnapshotHash()
	if err != nil {
		t.Fatal(err)
	}
	if changedHash == hash {
		t.Fatal("payee mutation did not change the immutable snapshot hash")
	}
}

func TestCandidateSetPayloadHashDetectsFactMutation(t *testing.T) {
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	capability := testCapability("did:merchant:a", "transcription", "did:solana:payee-a", "transcription", 8, "v1", now)
	set, err := BuildCandidateSet("cs-2", "episode-2", "request-2", testQuery(now), []*MerchantCapability{capability}, now, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	set.Candidates[0].PayeeDID = "did:solana:attacker"
	if err := set.Validate(); err != ErrInvalidCandidateSet {
		t.Fatalf("expected payload hash guard, got %v", err)
	}
}
