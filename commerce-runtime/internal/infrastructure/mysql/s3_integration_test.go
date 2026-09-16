package mysql

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func mysqlS3Capability(merchant, capability, payee, version string, now time.Time) *catalog.MerchantCapability {
	price := int64(8)
	return &catalog.MerchantCapability{MerchantDID: merchant, CapabilityID: capability, PayeeDID: payee, Name: capability, Description: "mysql catalog fact",
		TaskTypes: []string{"transcription"}, SemanticTags: []string{"language=en"}, InvokeEndpoint: catalog.EndpointRef{Ref: "merchant://" + merchant + "/" + capability},
		QuoteEndpoint: catalog.EndpointRef{Ref: "quote://" + merchant + "/" + capability}, InputSchemaRef: "schema:input", OutputSchemaRef: "schema:output",
		InputContentTypes: []string{"audio/mpeg"}, OutputContentTypes: []string{"text/plain"}, SupportedProtocolVersions: []string{"x402-v1"},
		SupportedCurrencies: []string{"USDC"}, PricingModel: "fixed", PriceHintMinor: &price, PriceHintCurrency: "USDC", Status: catalog.StatusActive,
		Availability: catalog.AvailabilityAvailable, CatalogVersion: version, Source: "mysql-test", SourceRef: "mysql:" + merchant,
		ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(10 * time.Minute), CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)}
}

func TestMySQLS3CurrentPointerHonorsDeactivation(t *testing.T) {
	dsn := os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COMMERCE_RUNTIME_MYSQL_DSN is not set")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	merchant := "did:merchant:mysql-current-" + suffix
	capabilityIDs := []string{"inactive-capability", "deprecated-capability", "reactivated-capability"}
	for _, capabilityID := range capabilityIDs {
		defer func(capabilityID string) {
			db.Exec("DELETE FROM merchant_capability_current WHERE merchant_did = ? AND capability_id = ?", merchant, capabilityID)
			db.Exec("DELETE FROM merchant_capabilities WHERE merchant_did = ? AND capability_id = ?", merchant, capabilityID)
		}(capabilityID)
	}
	versions := []*catalog.MerchantCapability{
		mysqlS3Capability(merchant, capabilityIDs[0], "did:solana:mysql-inactive-"+suffix, "v1", now),
		mysqlS3Capability(merchant, capabilityIDs[0], "did:solana:mysql-inactive-"+suffix, "v2", now),
		mysqlS3Capability(merchant, capabilityIDs[1], "did:solana:mysql-deprecated-"+suffix, "v1", now),
		mysqlS3Capability(merchant, capabilityIDs[1], "did:solana:mysql-deprecated-"+suffix, "v2", now),
		mysqlS3Capability(merchant, capabilityIDs[2], "did:solana:mysql-reactivated-"+suffix, "v1", now),
		mysqlS3Capability(merchant, capabilityIDs[2], "did:solana:mysql-reactivated-"+suffix, "v2", now),
		mysqlS3Capability(merchant, capabilityIDs[2], "did:solana:mysql-reactivated-"+suffix, "v3", now),
	}
	versions[1].Status = catalog.StatusInactive
	versions[3].Status = catalog.StatusDeprecated
	versions[5].Status = catalog.StatusInactive
	versions[6].UpdatedAt = now.Add(2 * time.Second)
	for index, value := range versions {
		if index > 0 {
			value.UpdatedAt = now.Add(time.Duration(index) * time.Second)
		}
		if err := NewStore(db).SaveCapabilityVersion(ctx, value); err != nil {
			t.Fatal(err)
		}
	}
	store := NewStore(db)
	for _, expected := range []struct {
		capabilityID string
		version      string
		status       catalog.CapabilityStatus
	}{
		{capabilityIDs[0], "v2", catalog.StatusInactive},
		{capabilityIDs[1], "v2", catalog.StatusDeprecated},
		{capabilityIDs[2], "v3", catalog.StatusActive},
	} {
		current, err := store.GetCurrentActiveCapability(ctx, merchant, expected.capabilityID)
		if err != nil || current.CatalogVersion != expected.version || current.Status != expected.status {
			t.Fatalf("unexpected current pointer for %s: %#v, %v", expected.capabilityID, current, err)
		}
	}
	active, err := store.ListActiveCapabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range active {
		if value.MerchantDID != merchant {
			continue
		}
		if value.CapabilityID == capabilityIDs[0] || value.CapabilityID == capabilityIDs[1] {
			t.Fatalf("discovery fell back to a historical active version: %#v", value)
		}
		if value.CapabilityID == capabilityIDs[2] && value.CatalogVersion != "v3" {
			t.Fatalf("discovery returned a non-current version after reactivation: %#v", value)
		}
	}
}

// TestMySQLS3CatalogAndCandidateSetPersistence exercises the real MySQL path
// for immutable versions, current lookup, snapshot hashes, expiry and facts.
func TestMySQLS3CatalogAndCandidateSetPersistence(t *testing.T) {
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
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	merchant, capabilityID := "did:merchant:mysql-s3-"+suffix, "transcription-premium"
	request := integrationRequest(now, "acr-mysql-s3-"+suffix)
	service := application.NewService(NewStore(db), application.WithClock(func() time.Time { return now }))
	created, err := service.CreateEpisode(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	v1 := mysqlS3Capability(merchant, capabilityID, "did:solana:mysql-old-"+suffix, "v1", now)
	v2 := mysqlS3Capability(merchant, capabilityID, "did:solana:mysql-new-"+suffix, "v2", now)
	for _, value := range []*catalog.MerchantCapability{v1, v2} {
		value.TaskTypes = []string{"integration"}
		value.InputContentTypes = []string{"application/octet-stream"}
		value.OutputContentTypes = []string{"application/json"}
	}
	v2.UpdatedAt = now.Add(time.Second)
	if err := store.SaveCapabilityVersion(ctx, v1); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCapabilityVersion(ctx, v2); err != nil {
		t.Fatal(err)
	}
	old, err := store.GetCapabilityVersion(ctx, merchant, capabilityID, "v1")
	if err != nil || old.PayeeDID != v1.PayeeDID {
		t.Fatalf("v1 snapshot changed: %#v, %v", old, err)
	}
	current, err := store.GetCurrentActiveCapability(ctx, merchant, capabilityID)
	if err != nil || current.CatalogVersion != "v2" || current.PayeeDID != v2.PayeeDID {
		t.Fatalf("unexpected current capability: %#v, %v", current, err)
	}
	active, err := store.ListActiveCapabilities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) == 0 || active[0].CatalogVersion != "v2" {
		t.Fatalf("active lookup did not select v2: %#v", active)
	}
	oldHash, err := v1.SnapshotHash()
	if err != nil {
		t.Fatal(err)
	}
	readHash, err := old.SnapshotHash()
	if err != nil {
		t.Fatal(err)
	}
	if oldHash != readHash {
		t.Fatalf("catalog snapshot hash was not persisted: %s != %s", oldHash, readHash)
	}
	query, err := catalog.FromAcquireCapabilityRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	set, err := catalog.BuildCandidateSet("cs-mysql-s3-"+suffix, created.Episode.EpisodeID, created.Episode.RequestID, query, active, now, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCandidateSet(ctx, set); err != nil {
		t.Fatal(err)
	}
	readSet, err := store.GetCandidateSet(ctx, set.CandidateSetID)
	if err != nil {
		t.Fatal(err)
	}
	if readSet.PayloadHash != set.PayloadHash || len(readSet.Candidates) != 1 {
		t.Fatalf("candidate set fact did not persist: %#v", readSet)
	}
	if err := readSet.ValidateAt(now.Add(3 * time.Minute)); !errors.Is(err, catalog.ErrCandidateSetExpired) {
		t.Fatalf("candidate set expiry was not enforced: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM candidate_sets WHERE candidate_set_id = ?", set.CandidateSetID)
		db.Exec("DELETE FROM merchant_capability_current WHERE merchant_did = ? AND capability_id = ?", merchant, capabilityID)
		db.Exec("DELETE FROM merchant_capabilities WHERE merchant_did = ? AND capability_id = ?", merchant, capabilityID)
		db.Exec("DELETE FROM payment_intents WHERE episode_id = ?", created.Episode.EpisodeID)
		db.Exec("DELETE FROM ledger_entries WHERE episode_id = ?", created.Episode.EpisodeID)
		db.Exec("DELETE FROM episode_events WHERE episode_id = ?", created.Episode.EpisodeID)
		db.Exec("DELETE FROM commerce_episodes WHERE episode_id = ?", created.Episode.EpisodeID)
	})
}

func TestMySQLS3ConcurrentSameVersionConflict(t *testing.T) {
	dsn := os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COMMERCE_RUNTIME_MYSQL_DSN is not set")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	merchant, capabilityID := "did:merchant:mysql-race-"+suffix, "transcription"
	first := mysqlS3Capability(merchant, capabilityID, "did:solana:race", "v1", now)
	second := first.Clone()
	second.Description = "different immutable payload"
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, value := range []*catalog.MerchantCapability{first, second} {
		value := value
		wait.Add(1)
		go func() { defer wait.Done(); results <- NewStore(db).SaveCapabilityVersion(ctx, value) }()
	}
	wait.Wait()
	close(results)
	var success, conflict int
	for result := range results {
		if result == nil {
			success++
		} else if errors.Is(result, repository.ErrCatalogVersionConflict) {
			conflict++
		} else {
			t.Fatalf("unexpected concurrent catalog result: %v", result)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("expected one immutable version winner, got success=%d conflict=%d", success, conflict)
	}
	db.Exec("DELETE FROM merchant_capability_current WHERE merchant_did = ? AND capability_id = ?", merchant, capabilityID)
	db.Exec("DELETE FROM merchant_capabilities WHERE merchant_did = ? AND capability_id = ?", merchant, capabilityID)
}

func TestMySQLS3CandidateSetToSelectMerchant(t *testing.T) {
	dsn := os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COMMERCE_RUNTIME_MYSQL_DSN is not set")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	merchant, capabilityID := "did:merchant:mysql-select-"+suffix, "integration-capability"
	store := NewStore(db)
	service := application.NewService(store, application.WithClock(func() time.Time { return now }))
	request := integrationRequest(now, "acr-mysql-select-"+suffix)
	created, err := service.CreateEpisode(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	capability := mysqlS3Capability(merchant, capabilityID, "did:solana:mysql-selected-"+suffix, "v1", now)
	capability.TaskTypes = []string{"integration"}
	capability.InputContentTypes = []string{"application/octet-stream"}
	capability.OutputContentTypes = []string{"application/json"}
	if err := store.SaveCapabilityVersion(ctx, capability); err != nil {
		t.Fatal(err)
	}
	discovered, err := service.DiscoverCapabilities(ctx, application.DiscoverCapabilitiesRequest{EpisodeID: created.Episode.EpisodeID, CandidateSetID: "cs-mysql-select-" + suffix})
	if err != nil {
		t.Fatal(err)
	}
	if len(discovered.CandidateSet.Candidates) != 1 {
		t.Fatalf("unexpected MySQL CandidateSet: %#v", discovered.CandidateSet)
	}
	candidate := discovered.CandidateSet.Candidates[0]
	proposal := decision.DecisionProposal{ProposalID: "mysql-select-" + suffix, EpisodeID: created.Episode.EpisodeID, BasedOnEventSequence: discovered.Episode.Version - 1,
		ProposedAction: trace.ActionSelectMerchant, CandidateSetID: discovered.CandidateSet.CandidateSetID,
		Target: &decision.ProposalTarget{MerchantDID: candidate.MerchantDID, CapabilityID: candidate.CapabilityID}, Confidence: 1,
		EvidenceRefs: []string{discovered.CandidateSet.FactsRef, discovered.CandidateSet.PayloadHash}, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute)}
	selected, err := service.CommitMerchantSelection(ctx, application.SelectMerchantRequest{Proposal: proposal,
		Action:      trace.Action{Type: trace.ActionSelectMerchant, IdempotencyKey: "mysql-select-" + suffix},
		Observation: trace.Observation{Type: trace.ObservationCandidatesFound, FactsRef: discovered.CandidateSet.FactsRef, PayloadHash: discovered.CandidateSet.PayloadHash}, Actor: "runtime", TraceID: "mysql-select-trace"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Episode.SelectedMerchantDID != candidate.MerchantDID || selected.Episode.SelectedCapabilityID != candidate.CapabilityID || selected.Episode.SelectedCatalogVersion != candidate.CatalogVersion || selected.Episode.SelectedCatalogSnapshotHash != candidate.CatalogSnapshotHash {
		t.Fatalf("MySQL selection did not bind candidate snapshot: %#v", selected.Episode)
	}
	payee, err := service.ResolveSelectedPayeeDID(ctx, created.Episode.EpisodeID)
	if err != nil || payee != candidate.PayeeDID {
		t.Fatalf("MySQL selection resolved the wrong payee: %s, %v", payee, err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM candidate_sets WHERE candidate_set_id = ?", discovered.CandidateSet.CandidateSetID)
		db.Exec("DELETE FROM merchant_capability_current WHERE merchant_did = ? AND capability_id = ?", merchant, capabilityID)
		db.Exec("DELETE FROM merchant_capabilities WHERE merchant_did = ? AND capability_id = ?", merchant, capabilityID)
		db.Exec("DELETE FROM episode_events WHERE episode_id = ?", created.Episode.EpisodeID)
		db.Exec("DELETE FROM commerce_episodes WHERE episode_id = ?", created.Episode.EpisodeID)
	})
}

func TestMySQLS3DiscoveryReplayUsesSameCandidateSetFact(t *testing.T) {
	dsn := os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COMMERCE_RUNTIME_MYSQL_DSN is not set")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2099, 9, 15, 12, 0, 0, 0, time.UTC)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	merchant := "did:merchant:mysql-discovery-replay-" + suffix
	capabilityID := "integration-capability"
	store := NewStore(db)
	service := application.NewService(store, application.WithClock(func() time.Time { return now }))
	request := integrationRequest(now, "acr-mysql-discovery-replay-"+suffix)
	created, err := service.CreateEpisode(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	v1 := mysqlS3Capability(merchant, capabilityID, "did:solana:mysql-replay-old-"+suffix, "v1", now)
	v1.TaskTypes = []string{"integration"}
	v1.InputContentTypes = []string{"application/octet-stream"}
	v1.OutputContentTypes = []string{"application/json"}
	if err := store.SaveCapabilityVersion(ctx, v1); err != nil {
		t.Fatal(err)
	}
	first, err := service.DiscoverCapabilities(ctx, application.DiscoverCapabilitiesRequest{EpisodeID: created.Episode.EpisodeID})
	if err != nil {
		t.Fatal(err)
	}
	if first.Replayed || len(first.CandidateSet.Candidates) != 1 {
		t.Fatalf("unexpected first MySQL discovery: %#v", first)
	}
	v2 := v1.Clone()
	v2.CatalogVersion = "v2"
	v2.Status = catalog.StatusInactive
	v2.UpdatedAt = now.Add(time.Second)
	if err := store.SaveCapabilityVersion(ctx, v2); err != nil {
		t.Fatal(err)
	}
	second, err := service.DiscoverCapabilities(ctx, application.DiscoverCapabilitiesRequest{EpisodeID: created.Episode.EpisodeID})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Replayed || second.CandidateSet.CandidateSetID != first.CandidateSet.CandidateSetID || second.CandidateSet.PayloadHash != first.CandidateSet.PayloadHash || second.Event.EventID != first.Event.EventID || second.CandidateSet.Candidates[0].CatalogVersion != "v1" {
		t.Fatalf("MySQL discovery replay changed the committed fact: first=%#v second=%#v", first, second)
	}
	if second.Episode.ActionCount != first.Episode.ActionCount {
		t.Fatalf("MySQL discovery replay changed ActionCount: %d -> %d", first.Episode.ActionCount, second.Episode.ActionCount)
	}
	events, err := service.ListEvents(ctx, created.Episode.EpisodeID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("MySQL discovery replay appended another event: %#v", events)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM candidate_sets WHERE candidate_set_id = ?", first.CandidateSet.CandidateSetID)
		db.Exec("DELETE FROM merchant_capability_current WHERE merchant_did = ? AND capability_id = ?", merchant, capabilityID)
		db.Exec("DELETE FROM merchant_capabilities WHERE merchant_did = ? AND capability_id = ?", merchant, capabilityID)
		db.Exec("DELETE FROM episode_events WHERE episode_id = ?", created.Episode.EpisodeID)
		db.Exec("DELETE FROM commerce_episodes WHERE episode_id = ?", created.Episode.EpisodeID)
	})
}
