package mysql

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/buyer"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func TestMySQLBuyerConfirmationBindsDistinctOfferRefsWithSameQuoteHash(t *testing.T) {
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
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx := context.Background()
	if err := sqlDB.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2099, 10, 11, 12, 0, 0, 0, time.UTC)
	request := integrationRequest(now, integrationRequestID(t))
	request.Constraints.RequireBuyerConfirmation = true
	service := application.NewService(NewStore(db), application.WithClock(func() time.Time { return now }))
	created, err := service.CreateEpisode(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	episodeID := created.Episode.EpisodeID
	store := NewStore(db)
	t.Cleanup(func() {
		for _, table := range []string{"buyer_confirmations", "payment_requirement_facts", "episode_events", "commerce_episodes"} {
			_ = db.Exec("DELETE FROM "+table+" WHERE episode_id = ?", episodeID).Error
		}
	})

	const quoteHash = "sha256:stable-offer-same-quote"
	makeRequirement := func(id string) *invocation.PaymentRequirementFact {
		return &invocation.PaymentRequirementFact{
			PaymentRequirementID: id, EpisodeID: episodeID, InvocationID: "invoke:" + id,
			MerchantDID: "did:merchant:mysql-offer", CapabilityID: "capability:mysql-offer",
			CatalogVersion: "catalog-v1", CatalogSnapshotHash: "sha256:catalog", ProtocolVersion: "2.0",
			Scheme: "exact", Network: "solana-devnet", Asset: "USDC", AtomicAmount: 1000, AtomicDecimals: 6,
			BusinessAmountMinor: 1, Currency: "USDC", PayTo: "PayToAddress", PayeeDID: "did:merchant:mysql-offer",
			ResourceURL: "https://merchant.invalid/service", ProductID: "sku-1", MaxTimeoutSeconds: 300,
			ObservedAt: now, ExpiresAt: now.Add(5 * time.Minute), RawPayloadHash: "sha256:raw", CanonicalQuoteHash: quoteHash,
			FactsRef: "payment-requirement://" + id,
		}
	}
	firstRequirement := makeRequirement("prf:mysql-offer-1")
	secondRequirement := makeRequirement("prf:mysql-offer-2")
	for _, requirement := range []*invocation.PaymentRequirementFact{firstRequirement, secondRequirement} {
		if err := store.SavePaymentRequirementFact(ctx, requirement); err != nil {
			t.Fatal(err)
		}
	}

	current := created.Episode
	commitState := func(to episode.State, key string, action trace.ActionType, observation trace.ObservationType, selectOffer *invocation.PaymentRequirementFact) *episode.CommerceEpisode {
		t.Helper()
		next := current.Clone()
		if selectOffer != nil {
			next.SelectedMerchantDID = selectOffer.MerchantDID
			next.SelectedCapabilityID = selectOffer.CapabilityID
			next.CurrentQuoteHash = selectOffer.CanonicalQuoteHash
			next.CurrentPaymentRequirementID = selectOffer.PaymentRequirementID
		}
		next.ActionCount++
		if err := next.ApplyCommittedState(to, now, ""); err != nil {
			t.Fatal(err)
		}
		event, err := episode.NewEvent("evt:"+key, episodeID, current.Version, now, current.State,
			trace.Action{Type: action, IdempotencyKey: key}, trace.Observation{Type: observation}, trace.Decision{ProposedAction: action, ProposalID: key},
			trace.RuntimeVerdict{Allowed: true}, next.State, "runtime", key, application.DefaultRuntimeVersion)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.CommitTransition(ctx, episodeID, current.Version, next, event); err != nil {
			t.Fatal(err)
		}
		current = next
		return current
	}
	commitState(episode.StateDiscovering, "mysql-offer-discover", trace.ActionDiscover, trace.ObservationCandidatesFound, nil)
	commitState(episode.StateInvoking, "mysql-offer-invoke", trace.ActionInvoke, trace.ObservationMerchantResponse, nil)
	commitState(episode.StateNegotiating, "mysql-offer-negotiate", trace.ActionParse402, trace.ObservationQuoteValid, nil)
	commitState(episode.StateAwaitingBuyer, "mysql-offer-await-1", trace.ActionRequestBuyerConfirmation, trace.ObservationBuyerConfirmationRequired, firstRequirement)

	confirm := func(requirement *invocation.PaymentRequirementFact, suffix string) {
		t.Helper()
		confirmation := &buyer.Confirmation{
			ConfirmationID: "confirmation:" + suffix, EpisodeID: episodeID, RequestID: current.RequestID,
			RequesterDID: current.RequesterDID, MerchantDID: requirement.MerchantDID, CapabilityID: requirement.CapabilityID,
			ProductID: requirement.ProductID, OfferRef: requirement.PaymentRequirementID, QuoteHash: requirement.CanonicalQuoteHash,
			AmountMinor: requirement.BusinessAmountMinor, Currency: requirement.Currency, Accepted: true,
			IdempotencyKey: "mysql-buyer-confirm:" + suffix, PayloadHash: "sha256:buyer-confirmation", Signature: "test-only-signature",
			ExpiresAt: requirement.ExpiresAt, CreatedAt: now,
		}
		next := current.Clone()
		next.ActionCount++
		if err := next.ApplyCommittedState(episode.StateNegotiating, now, ""); err != nil {
			t.Fatal(err)
		}
		event, err := episode.NewEvent("evt:mysql-confirm:"+suffix, episodeID, current.Version, now, current.State,
			trace.Action{Type: trace.ActionRecordBuyerConfirmation, IdempotencyKey: "event:mysql-confirm:" + suffix},
			trace.Observation{Type: trace.ObservationBuyerApproved, Code: quoteHash}, trace.Decision{ProposedAction: trace.ActionRecordBuyerConfirmation, ProposalID: suffix},
			trace.RuntimeVerdict{Allowed: true}, next.State, "buyer", suffix, application.DefaultRuntimeVersion)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.CommitBuyerConfirmationTransition(ctx, repository.BuyerConfirmationTransition{
			EpisodeID: episodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event, Confirmation: confirmation,
		}); err != nil {
			t.Fatal(err)
		}
		current = next
		persisted, err := store.FindBuyerConfirmationByOfferRef(ctx, episodeID, requirement.PaymentRequirementID)
		if err != nil || persisted.OfferRef != requirement.PaymentRequirementID || persisted.QuoteHash != quoteHash {
			t.Fatalf("confirmation did not round-trip by exact offer ref: value=%#v err=%v", persisted, err)
		}
	}
	confirm(firstRequirement, "first")
	commitState(episode.StateAwaitingBuyer, "mysql-offer-refresh-2", trace.ActionRefreshBuyerOffer, trace.ObservationBuyerQuoteRefreshed, secondRequirement)
	confirm(secondRequirement, "second")

	byQuote, err := store.GetBuyerConfirmationByQuoteHash(ctx, episodeID, quoteHash)
	if err != nil || (byQuote.OfferRef != firstRequirement.PaymentRequirementID && byQuote.OfferRef != secondRequirement.PaymentRequirementID) {
		t.Fatalf("quote-hash compatibility lookup failed after two offer refs: value=%#v err=%v", byQuote, err)
	}
}
