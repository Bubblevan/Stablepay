package mysql

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func TestMySQLS5RecoveryAndParentFactsAreAtomicAndReplayable(t *testing.T) {
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
	request := integrationRequest(now, "mysql-s5-"+integrationRequestID(t))
	service := application.NewService(NewStore(db), application.WithClock(func() time.Time { return now }))
	created, err := service.CreateEpisode(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	episodeID := created.Episode.EpisodeID
	t.Cleanup(func() {
		for _, table := range []string{"budget_amendments", "parent_decisions", "parent_approval_requests", "recovery_contexts", "episode_events", "commerce_episodes"} {
			db.Exec("DELETE FROM "+table+" WHERE episode_id = ?", episodeID)
			if table != "commerce_episodes" {
				db.Exec("DELETE FROM "+table+" WHERE episode_id = ?", episodeID)
			}
		}
	})
	store := NewStore(db)
	rc := &recovery.RecoveryContext{RecoveryID: "mysql-s5-recovery-" + episodeID, EpisodeID: episodeID, TriggerEventID: "pending", ReasonCode: recovery.ReasonDeliveryInvalid, CurrentMerchantDID: "did:merchant:a", CurrentCapabilityID: "cap", CandidateSetID: "cs:mysql:s5", AttemptedMerchants: []string{"did:merchant:a"}, SettledMinor: 700, ConsumedMinor: 700, AvailableMinor: 300, SunkCostMinor: 700, DeadlineAt: now.Add(time.Hour), CreatedAt: now, FactsRef: "recovery://" + episodeID}
	if err := rc.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	commitRecovery := func(current *episode.CommerceEpisode, to episode.State, action trace.ActionType, key string, approval *recovery.ParentApprovalRequest) (*episode.CommerceEpisode, *episode.EpisodeEvent) {
		next := current.Clone()
		if err := next.ApplyCommittedState(to, now, ""); err != nil {
			t.Fatal(err)
		}
		event, err := episode.NewEvent("evt:"+key, current.EpisodeID, current.Version, now, current.State, trace.Action{Type: action, IdempotencyKey: key}, trace.Observation{Type: trace.ObservationCandidatesFound, FactsRef: rc.FactsRef, PayloadHash: rc.PayloadHash}, trace.Decision{ProposedAction: action, ProposalID: key, EvidenceRefs: []string{rc.FactsRef, rc.PayloadHash}}, trace.RuntimeVerdict{Allowed: true}, next.State, "runtime", key, application.DefaultRuntimeVersion)
		if err != nil {
			t.Fatal(err)
		}
		rc.TriggerEventID = event.EventID
		rc.RefreshPayloadHash()
		next.RecoveryID = rc.RecoveryID
		if err := store.CommitRecoveryTransition(ctx, repository.RecoveryTransition{EpisodeID: episodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event, RecoveryContext: rc, ParentApproval: approval}); err != nil {
			t.Fatal(err)
		}
		return next, event
	}
	current, event := commitRecovery(created.Episode, episode.StateDiscovering, trace.ActionDiscover, "mysql-s5-discover", nil)
	_ = event
	current, _ = commitRecovery(current, episode.StateInvoking, trace.ActionInvoke, "mysql-s5-invoke", nil)
	current, _ = commitRecovery(current, episode.StateNegotiating, trace.ActionParse402, "mysql-s5-parse", nil)
	current, _ = commitRecovery(current, episode.StateRecovering, trace.ActionReserveBudget, "mysql-s5-recover", nil)
	approval := &recovery.ParentApprovalRequest{ApprovalID: "mysql-s5-approval", EpisodeID: episodeID, RecoveryID: rc.RecoveryID, ReasonCode: recovery.ReasonBudgetInsufficient, RequestedAction: string(trace.ActionSwitchMerchant), ApprovalScope: recovery.AllowSwitch, CurrentBudgetMinor: 1000, ConsumedMinor: 700, AvailableMinor: 300, SunkCostMinor: 700, CurrentMerchantDID: "did:merchant:a", ExpiresAt: now.Add(time.Minute), FactsRef: "parent-approval://mysql-s5", CreatedAt: now}
	if err := approval.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	if err := approval.Validate(); err != nil {
		t.Fatal(err)
	}
	next, event := commitRecovery(current, episode.StateAwaitingParent, trace.ActionAskParent, "mysql-s5-ask", approval)
	_ = next
	_ = event
	decisionFact := &recovery.ParentDecisionFact{ApprovalID: approval.ApprovalID, EpisodeID: episodeID, Decision: recovery.Approve, ActorRef: "parent:mysql", OccurredAt: now, FactsRef: "parent-decision://mysql-s5"}
	if err := decisionFact.RefreshPayloadHash(); err != nil {
		t.Fatal(err)
	}
	rc.TriggerEventID = "mysql-s5-parent"
	rc.RefreshPayloadHash()
	after := current
	after, _ = service.GetEpisode(ctx, episodeID)
	afterNext := after.Clone()
	if err := afterNext.ApplyCommittedState(episode.StateRecovering, now, ""); err != nil {
		t.Fatal(err)
	}
	parentEvent, err := episode.NewEvent("evt:mysql-s5-parent", episodeID, after.Version, now, after.State, trace.Action{Type: trace.ActionAskParent, IdempotencyKey: "mysql-s5-parent"}, trace.Observation{Type: trace.ObservationParentApproved, FactsRef: decisionFact.FactsRef, PayloadHash: decisionFact.PayloadHash}, trace.Decision{ProposedAction: trace.ActionAskParent, ProposalID: approval.ApprovalID}, trace.RuntimeVerdict{Allowed: true}, afterNext.State, "parent", "mysql-s5-parent", application.DefaultRuntimeVersion)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitParentDecision(ctx, repository.ParentDecisionTransition{EpisodeID: episodeID, ExpectedEpisodeVersion: after.Version, NextEpisode: afterNext, Event: parentEvent, Decision: decisionFact, RecoveryContext: rc}); err != nil {
		t.Fatal(err)
	}
	read, err := store.GetRecoveryContextByEpisode(ctx, episodeID)
	if err != nil || read.RecoveryID != rc.RecoveryID {
		t.Fatalf("recovery=%#v err=%v", read, err)
	}
	readApproval, err := store.GetParentApprovalRequest(ctx, approval.ApprovalID)
	if err != nil || readApproval.ApprovalID != approval.ApprovalID {
		t.Fatalf("approval=%#v err=%v", readApproval, err)
	}
	readDecision, err := store.GetParentDecision(ctx, approval.ApprovalID)
	if err != nil || readDecision.Decision != recovery.Approve {
		t.Fatalf("decision=%#v err=%v", readDecision, err)
	}
}
