// Package runtime owns the durable episode execution loop. It is deliberately
// a thin orchestration layer: all state transitions and external side effects
// remain in application.Service and its adapters.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

const (
	defaultMaxSteps           = 64
	initialCandidateSetPrefix = "cs:"
)

var ErrRunnerStepLimit = errors.New("episode runner step limit reached")

type Runner struct {
	service     *application.Service
	store       repository.TransitionStore
	credentials adapters.CredentialProvider
	maxSteps    int
	locks       sync.Map
}

type Option func(*Runner)

func WithMaxSteps(value int) Option {
	return func(r *Runner) {
		if value > 0 {
			r.maxSteps = value
		}
	}
}

func WithCredentialProvider(value adapters.CredentialProvider) Option {
	return func(r *Runner) { r.credentials = value }
}

func NewRunner(service *application.Service, store repository.TransitionStore, options ...Option) *Runner {
	r := &Runner{service: service, store: store, maxSteps: defaultMaxSteps}
	for _, option := range options {
		option(r)
	}
	return r
}

type Result struct {
	Episode *episode.CommerceEpisode
	Steps   int
	Paused  bool
}

// RunEpisode and ResumeEpisode intentionally share one implementation. A
// restart does not restore in-memory workflow state; it loads the persisted
// projection and facts and continues from the authoritative state.
func (r *Runner) RunEpisode(ctx context.Context, episodeID string) (Result, error) {
	return r.run(ctx, episodeID)
}

func (r *Runner) ResumeEpisode(ctx context.Context, episodeID string) (Result, error) {
	return r.run(ctx, episodeID)
}

// ResumePersisted is called during server startup. It is the crash/restart
// bridge: every non-terminal projection is loaded from the repository and
// handed back to the same state-driven loop.
func (r *Runner) ResumePersisted(ctx context.Context) error {
	lister, ok := r.store.(repository.RunnableEpisodeLister)
	if !ok {
		return repository.ErrRepositoryUnavailable
	}
	values, err := lister.ListRunnableEpisodes(ctx)
	if err != nil {
		return err
	}
	for _, value := range values {
		if value != nil {
			r.Enqueue(ctx, value.EpisodeID)
		}
	}
	return nil
}

func (r *Runner) run(ctx context.Context, episodeID string) (Result, error) {
	if r == nil || r.service == nil || r.store == nil {
		return Result{}, repository.ErrRepositoryUnavailable
	}
	episodeID = strings.TrimSpace(episodeID)
	if episodeID == "" {
		return Result{}, repository.ErrNotFound
	}
	value, _ := r.locks.LoadOrStore(episodeID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	for step := 0; step < r.maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		current, err := r.service.GetEpisode(ctx, episodeID)
		if err != nil {
			return Result{}, err
		}
		if episode.IsTerminal(current.State) {
			return Result{Episode: current, Steps: step}, nil
		}
		if current.State == episode.StateAwaitingParent {
			return Result{Episode: current, Steps: step, Paused: true}, nil
		}
		if err := r.step(ctx, current); err != nil {
			return Result{}, err
		}
	}
	current, err := r.service.GetEpisode(ctx, episodeID)
	if err != nil {
		return Result{}, err
	}
	return Result{Episode: current, Steps: r.maxSteps}, ErrRunnerStepLimit
}

// Enqueue is used by the HTTP/MCP surface after a successful ingress commit.
// Work is persisted before this function is called, so a process restart can
// safely enqueue the same idempotent episode again.
func (r *Runner) Enqueue(ctx context.Context, episodeID string) {
	go func() {
		background := context.Background()
		if deadline, ok := ctx.Deadline(); ok {
			var cancel context.CancelFunc
			background, cancel = context.WithDeadline(background, deadline)
			defer cancel()
		}
		for attempt := 0; attempt < 8; attempt++ {
			result, err := r.ResumeEpisode(background, episodeID)
			if err == nil || result.Paused || (result.Episode != nil && episode.IsTerminal(result.Episode.State)) {
				return
			}
			backoff := time.Duration(attempt+1) * 250 * time.Millisecond
			timer := time.NewTimer(backoff)
			select {
			case <-background.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (r *Runner) step(ctx context.Context, current *episode.CommerceEpisode) error {
	traceID := fmt.Sprintf("runtime:%s:%d", current.EpisodeID, current.Version)
	switch current.State {
	case episode.StateAccepted:
		_, err := r.service.DiscoverCapabilities(ctx, application.DiscoverCapabilitiesRequest{EpisodeID: current.EpisodeID, CandidateSetID: initialCandidateSetPrefix + current.EpisodeID})
		return err
	case episode.StateDiscovering:
		return r.selectMerchant(ctx, current)
	case episode.StateInvoking:
		invoked, err := r.service.InvokeSelectedMerchant(ctx, application.InvokeSelectedMerchantRequest{EpisodeID: current.EpisodeID, TraceID: traceID})
		if err != nil {
			return err
		}
		if invoked.Response.HTTPStatus != 402 {
			return fmt.Errorf("merchant did not return x402 challenge: HTTP %d", invoked.Response.HTTPStatus)
		}
		_, err = r.service.ParsePaymentRequirement(ctx, application.ParsePaymentRequirementRequest{EpisodeID: current.EpisodeID, InvocationID: invoked.Invocation.InvocationID, TraceID: traceID + ":parse"})
		return err
	case episode.StateNegotiating:
		quote, err := r.currentQuote(ctx, current)
		if err != nil {
			return err
		}
		_, err = r.service.ReservePaymentIntentForEpisode(ctx, application.ReservePaymentIntentForEpisodeRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "payment:" + current.EpisodeID + ":" + quote.QuoteHash, Actor: "runtime", TraceID: traceID + ":reserve"})
		return err
	case episode.StatePaying:
		intent, err := r.currentPaymentIntent(ctx, current)
		if err != nil {
			return err
		}
		_, err = r.service.AuthorizeAndSubmitPayment(ctx, intent.IntentID, traceID+":submit")
		return err
	case episode.StateClaiming:
		intent, err := r.currentPaymentIntent(ctx, current)
		if err != nil {
			return err
		}
		_, err = r.service.VerifyPaymentEntitlement(ctx, intent.IntentID, traceID+":entitlement")
		return err
	case episode.StateInvokingDelivery:
		signature, err := r.paymentSignature(ctx, current)
		if err != nil {
			return err
		}
		_, err = r.service.InvokeDelivery(ctx, application.InvokeDeliveryRequest{EpisodeID: current.EpisodeID, Attempt: current.DeliveryAttemptCount + 1, TraceID: traceID + ":delivery", PaymentSignature: signature})
		return err
	case episode.StateValidatingDelivery:
		if len(current.DeliveryRefs) == 0 {
			return errors.New("validating episode has no delivery artifact reference")
		}
		_, err := r.service.ValidateDelivery(ctx, application.ValidateDeliveryRequest{EpisodeID: current.EpisodeID, DeliveryID: current.DeliveryRefs[len(current.DeliveryRefs)-1], TraceID: traceID + ":validate"})
		return err
	case episode.StateRecovering:
		_, _, err := r.service.ExecuteRecoveryDecision(ctx, application.S6DecisionRequest{EpisodeID: current.EpisodeID, Query: "continue the persisted commerce episode toward a valid delivery; choose only an allowed recovery action"})
		return err
	default:
		return fmt.Errorf("unsupported non-terminal episode state %s", current.State)
	}
}

func (r *Runner) paymentSignature(ctx context.Context, current *episode.CommerceEpisode) (string, error) {
	if r.credentials == nil {
		return "", nil
	}
	intent, err := r.currentPaymentIntent(ctx, current)
	if err != nil {
		return "", err
	}
	credentials, err := r.credentials.Credentials(ctx, *intent)
	if err != nil {
		return "", err
	}
	return credentials.Signature, nil
}

func (r *Runner) selectMerchant(ctx context.Context, current *episode.CommerceEpisode) error {
	discovery, err := r.discoveryStore()
	if err != nil {
		return err
	}
	setID := initialCandidateSetPrefix + current.EpisodeID
	if current.DiscoveryGeneration > 0 {
		setID = fmt.Sprintf("%s%d", setID+":", current.DiscoveryGeneration)
	}
	set, err := discovery.GetCandidateSet(ctx, setID)
	if err != nil {
		return err
	}
	if len(set.Candidates) == 0 {
		return fmt.Errorf("candidate set %s contains no eligible merchant", setID)
	}
	candidate := set.Candidates[0]
	now := time.Now().UTC()
	// Durable catalog facts are normalized to millisecond precision. A wall
	// clock read immediately after the commit can therefore still be a few
	// hundred microseconds before GeneratedAt after rounding. Wait at this
	// orchestration boundary instead of changing the frozen catalog semantics.
	if now.Before(set.GeneratedAt) {
		timer := time.NewTimer(time.Until(set.GeneratedAt) + time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		now = time.Now().UTC()
	}
	expires := now.Add(5 * time.Minute)
	if set.ExpiresAt.Before(expires) {
		expires = set.ExpiresAt
	}
	if current.DeadlineAt.Before(expires) {
		expires = current.DeadlineAt
	}
	if !expires.After(now) {
		return episode.ErrEpisodeExpired
	}
	proposal := decision.DecisionProposal{
		ProposalID: "runtime-select:" + current.EpisodeID + ":" + fmt.Sprint(current.DiscoveryGeneration),
		EpisodeID:  current.EpisodeID, BasedOnEventSequence: current.Version - 1,
		ProposedAction: trace.ActionSelectMerchant, CandidateSetID: set.CandidateSetID,
		Target:    &decision.ProposalTarget{MerchantDID: candidate.MerchantDID, CapabilityID: candidate.CapabilityID, CatalogVersion: candidate.CatalogVersion, CatalogSnapshotHash: candidate.CatalogSnapshotHash, CatalogSnapshotRef: candidate.CatalogSnapshotRef},
		Rationale: "runtime deterministic candidate selection", EvidenceRefs: []string{set.FactsRef, set.PayloadHash}, Confidence: 1, CreatedAt: now.Add(-time.Nanosecond), ExpiresAt: expires,
	}
	_, err = r.service.CommitMerchantSelection(ctx, application.SelectMerchantRequest{
		Proposal:    proposal,
		Action:      trace.Action{Type: trace.ActionSelectMerchant, IdempotencyKey: "select:" + current.EpisodeID + ":" + fmt.Sprint(current.DiscoveryGeneration)},
		Observation: trace.Observation{Type: trace.ObservationCandidatesFound, FactsRef: set.FactsRef, PayloadHash: set.PayloadHash},
		Actor:       "runtime", TraceID: current.EpisodeID + ":select",
	})
	return err
}

func (r *Runner) currentQuote(ctx context.Context, current *episode.CommerceEpisode) (application.TrustedPaymentQuote, error) {
	store, err := r.s4Store()
	if err != nil {
		return application.TrustedPaymentQuote{}, err
	}
	key := "invoke:initial:" + current.EpisodeID + ":" + current.SelectedMerchantDID
	invocationFact, err := store.FindMerchantInvocationByIdempotencyKey(ctx, current.EpisodeID, key)
	if err != nil {
		return application.TrustedPaymentQuote{}, err
	}
	requirement, err := store.FindPaymentRequirementByInvocation(ctx, current.EpisodeID, invocationFact.InvocationID)
	if err != nil {
		return application.TrustedPaymentQuote{}, err
	}
	return application.TrustedQuoteFromPaymentRequirement(*requirement, current.RequesterDID), nil
}

func (r *Runner) currentPaymentIntent(ctx context.Context, current *episode.CommerceEpisode) (*payment.PaymentIntent, error) {
	store, err := r.financeStore()
	if err != nil {
		return nil, err
	}
	intents, err := store.ListPaymentIntents(ctx, current.EpisodeID)
	if err != nil {
		return nil, err
	}
	discovery, err := r.discoveryStore()
	if err != nil {
		return nil, err
	}
	capability, err := discovery.GetCapabilityVersion(ctx, current.SelectedMerchantDID, current.SelectedCapabilityID, current.SelectedCatalogVersion)
	if err != nil {
		return nil, err
	}
	for i := len(intents) - 1; i >= 0; i-- {
		value := intents[i]
		if value != nil && value.QuoteHash == current.CurrentQuoteHash && value.MerchantDID == current.SelectedMerchantDID && value.CapabilityID == current.SelectedCapabilityID && value.PayeeDID == capability.PayeeDID {
			return value, nil
		}
	}
	return nil, repository.ErrPaymentIntentNotFound
}

func (r *Runner) discoveryStore() (repository.DiscoveryRepository, error) {
	value, ok := r.store.(repository.DiscoveryRepository)
	if !ok {
		return nil, repository.ErrRepositoryUnavailable
	}
	return value, nil
}

func (r *Runner) s4Store() (repository.S4Store, error) {
	value, ok := r.store.(repository.S4Store)
	if !ok {
		return nil, repository.ErrRepositoryUnavailable
	}
	return value, nil
}

func (r *Runner) financeStore() (repository.S2Store, error) {
	value, ok := r.store.(repository.S2Store)
	if !ok {
		return nil, repository.ErrRepositoryUnavailable
	}
	return value, nil
}

// Keep the imports/contract boundary explicit for production composition and
// for compile-time detection if these runtime facts change shape.
var _ catalog.Candidate
var _ invocation.MerchantInvocation
var _ recovery.Decision
