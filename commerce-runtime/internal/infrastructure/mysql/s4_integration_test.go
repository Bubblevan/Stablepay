package mysql

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type mysqlS4Merchant struct{ calls int }

func (m *mysqlS4Merchant) Invoke(_ context.Context, request adapters.MerchantInvokeRequest) (adapters.MerchantInvokeResult, error) {
	m.calls++
	if request.Phase == "INITIAL" {
		payload := map[string]any{"x402Version": 2, "resource": map[string]any{"url": request.Endpoint.Endpoint}, "accepts": []any{map[string]any{
			"scheme": "exact", "network": "devnet", "amount": "3000000", "asset": "USDC", "payTo": "11111111111111111111111111111111", "maxTimeoutSeconds": 300,
			"extra": map[string]any{"currency": "USDC", "productId": "mysql-s4-product", "skillDid": "did:solana:11111111111111111111111111111111"},
		}}}
		body, _ := json.Marshal(payload)
		return adapters.MerchantInvokeResult{HTTPStatus: 402, Headers: map[string]string{"PAYMENT-REQUIRED": base64.StdEncoding.EncodeToString(body)}, Body: body, ContentType: "application/json", OccurredAt: time.Now().UTC()}, nil
	}
	body := []byte("mysql transcript")
	if request.Attempt == 1 {
		body = nil
	}
	return adapters.MerchantInvokeResult{HTTPStatus: 200, Body: body, ContentType: "text/plain", OccurredAt: time.Now().UTC()}, nil
}

type mysqlS4DIDAdapter struct{}

func (mysqlS4DIDAdapter) AuthorizePayment(_ context.Context, request adapters.AuthorizationRequest) (adapters.AuthorizationResult, error) {
	return adapters.AuthorizationResult{Allowed: true, RequesterDID: request.Intent.RequesterDID, MerchantDID: request.Intent.MerchantDID, CapabilityID: request.Intent.CapabilityID, PayeeDID: request.Intent.PayeeDID, QuoteHash: request.Intent.QuoteHash, AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency, AuthorizationRef: "mysql-s4-auth"}, nil
}

type mysqlS4PaymentAdapter struct{ calls int }

func (p *mysqlS4PaymentAdapter) Submit(_ context.Context, request adapters.PaymentSubmitRequest) (payment.PaymentOutcome, error) {
	p.calls++
	return payment.PaymentOutcome{Status: payment.OutcomePending, TxID: "mysql-s4-tx", TxHash: "mysql-s4-hash", AmountMinor: request.Intent.AmountMinor, Currency: request.Intent.Currency}, nil
}

type mysqlS4StatusAdapter struct{}

func (mysqlS4StatusAdapter) Query(context.Context, adapters.PaymentQuery) (payment.PaymentOutcome, error) {
	return payment.PaymentOutcome{Status: payment.OutcomeConfirmed, TxID: "mysql-s4-tx", TxHash: "mysql-s4-hash", AmountMinor: 300, Currency: "USDC"}, nil
}

type mysqlS4EntitlementAdapter struct{}

func (mysqlS4EntitlementAdapter) Verify(_ context.Context, query adapters.EntitlementQuery) (adapters.EntitlementResult, error) {
	return adapters.EntitlementResult{Status: adapters.EntitlementValid, PaymentIntentID: query.IntentID, EvidenceRef: "mysql-s4-entitlement"}, nil
}

func TestMySQLS4CanonicalInnerLoopPersistsFactsAndAvoidsSecondPayment(t *testing.T) {
	dsn := os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COMMERCE_RUNTIME_MYSQL_DSN is not set")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := sqlDB.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	request := integrationRequest(now, integrationRequestID(t))
	request.AcquisitionGoal.TaskType = "integration"
	request.Input.ContentType = "application/octet-stream"
	request.ExpectedOutput.ContentType = "text/plain"
	request.Validator.Name = "transcript_validator"
	request.Constraints.SupportedProtocolVersions = []string{"x402-v2"}
	merchant := &mysqlS4Merchant{}
	paymentAdapter := &mysqlS4PaymentAdapter{}
	service := application.NewService(NewStore(db), application.WithClock(func() time.Time { return now }), application.WithMerchantAdapter(merchant), application.WithPaymentAdapters(application.PaymentDependencies{
		DID: mysqlS4DIDAdapter{}, Payment: paymentAdapter, Status: mysqlS4StatusAdapter{}, Entitlement: mysqlS4EntitlementAdapter{},
	}))
	created, err := service.CreateEpisode(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	episodeID := created.Episode.EpisodeID
	merchantDID := "did:merchant:mysql-s4-" + fmt.Sprintf("%d", time.Now().UnixNano())
	capability := mysqlS3Capability(merchantDID, "integration", "did:solana:11111111111111111111111111111111", "v1", now)
	capability.InvokeEndpoint = catalog.EndpointRef{Endpoint: "https://merchant.example/api/v1/products/integration/execute", Method: "GET"}
	capability.TaskTypes = []string{"integration"}
	capability.InputContentTypes = []string{"application/octet-stream"}
	capability.OutputContentTypes = []string{"text/plain"}
	capability.SupportedProtocolVersions = []string{"x402-v2"}
	if err := service.RegisterCapabilityVersion(ctx, capability); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, table := range []string{"validation_evidence", "delivery_artifacts", "payment_requirement_facts", "merchant_invocations", "payment_intents", "ledger_entries", "episode_events", "candidate_sets", "commerce_episodes"} {
			db.Exec("DELETE FROM "+table+" WHERE episode_id = ?", episodeID)
		}
		db.Exec("DELETE FROM merchant_capability_current WHERE merchant_did = ? AND capability_id = ?", merchantDID, capability.CapabilityID)
		db.Exec("DELETE FROM merchant_capabilities WHERE merchant_did = ? AND capability_id = ?", merchantDID, capability.CapabilityID)
	})

	discovered, err := service.DiscoverCapabilities(ctx, application.DiscoverCapabilitiesRequest{EpisodeID: episodeID, CandidateSetID: "mysql-s4-candidates"})
	if err != nil {
		t.Fatal(err)
	}
	candidate := discovered.CandidateSet.Candidates[0]
	_, err = service.CommitMerchantSelection(ctx, application.SelectMerchantRequest{
		Proposal: decision.DecisionProposal{ProposalID: "mysql-s4-select-proposal", EpisodeID: episodeID, BasedOnEventSequence: discovered.Episode.Version - 1, ProposedAction: trace.ActionSelectMerchant, CandidateSetID: discovered.CandidateSet.CandidateSetID, Target: &decision.ProposalTarget{MerchantDID: candidate.MerchantDID, CapabilityID: candidate.CapabilityID, CatalogVersion: candidate.CatalogVersion, CatalogSnapshotHash: candidate.CatalogSnapshotHash, CatalogSnapshotRef: candidate.CatalogSnapshotRef}, Confidence: 1, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)},
		Action:   trace.Action{Type: trace.ActionSelectMerchant, IdempotencyKey: "mysql-s4-select"}, Observation: trace.Observation{Type: trace.ObservationCandidatesFound}, Actor: "runtime", TraceID: "mysql-s4-select-trace",
	})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := service.InvokeSelectedMerchant(ctx, application.InvokeSelectedMerchantRequest{EpisodeID: episodeID, TraceID: "mysql-s4-initial"})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := service.ParsePaymentRequirement(ctx, application.ParsePaymentRequirementRequest{EpisodeID: episodeID, InvocationID: initial.Invocation.InvocationID, TraceID: "mysql-s4-parse"})
	if err != nil {
		t.Fatal(err)
	}
	reserved, err := service.ReservePaymentIntent(ctx, application.ReservePaymentIntentRequest{EpisodeID: episodeID, Quote: parsed.Quote, IdempotencyKey: "mysql-s4-payment"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthorizeAndSubmitPayment(ctx, reserved.Intent.IntentID, "mysql-s4-submit"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReconcilePayment(ctx, reserved.Intent.IntentID, "mysql-s4-reconcile"); err != nil {
		t.Fatal(err)
	}
	claimed, err := service.VerifyPaymentEntitlement(ctx, reserved.Intent.IntentID, "mysql-s4-entitlement")
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.InvokeDelivery(ctx, application.InvokeDeliveryRequest{EpisodeID: episodeID, TraceID: "mysql-s4-delivery-1"})
	if err != nil {
		t.Fatal(err)
	}
	invalid, err := service.ValidateDelivery(ctx, application.ValidateDeliveryRequest{EpisodeID: episodeID, DeliveryID: first.Artifact.DeliveryID, TraceID: "mysql-s4-validate-1"})
	if err != nil || invalid.Valid || invalid.Episode.State != episode.StateRecovering {
		t.Fatalf("expected invalid first delivery: result=%#v err=%v", invalid, err)
	}
	retry, err := service.RetrySameMerchant(ctx, application.RetrySameMerchantRequest{EpisodeID: episodeID, Proposal: decision.DecisionProposal{ProposalID: "mysql-s4-retry-proposal", EpisodeID: episodeID, BasedOnEventSequence: invalid.Episode.Version - 1, ProposedAction: trace.ActionRetrySameMerchant, Confidence: 1, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}, Action: trace.Action{Type: trace.ActionRetrySameMerchant, IdempotencyKey: "mysql-s4-retry"}, Observation: trace.Observation{Type: trace.ObservationDeliveryInvalid}, Actor: "runtime", TraceID: "mysql-s4-retry-trace"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.InvokeDelivery(ctx, application.InvokeDeliveryRequest{EpisodeID: retry.Episode.EpisodeID, Attempt: 2, TraceID: "mysql-s4-delivery-2"})
	if err != nil {
		t.Fatal(err)
	}
	final, err := service.ValidateDelivery(ctx, application.ValidateDeliveryRequest{EpisodeID: episodeID, DeliveryID: second.Artifact.DeliveryID, TraceID: "mysql-s4-validate-2"})
	if err != nil {
		t.Fatal(err)
	}
	if !final.Valid || final.Episode.State != episode.StateFulfilled || final.Episode.PaymentAttemptCount != 1 || final.Episode.DeliveryAttemptCount != 2 || merchant.calls != 3 || paymentAdapter.calls != 1 || claimed.Episode.State != episode.StateInvokingDelivery {
		t.Fatalf("MySQL S4 invariants failed: final=%#v merchant_calls=%d payment_calls=%d claimed=%s", final.Episode, merchant.calls, paymentAdapter.calls, claimed.Episode.State)
	}
	store := NewStore(db)
	if _, err := store.GetMerchantInvocation(ctx, initial.Invocation.InvocationID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetPaymentRequirementFact(ctx, parsed.Requirement.PaymentRequirementID); err != nil {
		t.Fatal(err)
	}
}
