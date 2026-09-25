// Package runtime owns the durable episode execution loop. It is deliberately
// a thin orchestration layer: all state transitions and external side effects
// remain in application.Service and its adapters.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/application"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/contract"
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
	defaultScanInterval       = 2 * time.Second
	defaultRetryBase          = 250 * time.Millisecond
	defaultRetryMax           = 30 * time.Second
	initialCandidateSetPrefix = "cs:"
)

type workerContextKey struct{}

func isWorkerContext(ctx context.Context) bool {
	value, _ := ctx.Value(workerContextKey{}).(bool)
	return value
}

type Runner struct {
	service     *application.Service
	store       repository.TransitionStore
	credentials adapters.CredentialProvider
	maxSteps    int
	locks       sync.Map
	jobs        sync.Map

	rootCtx   context.Context
	workerMu  sync.RWMutex
	workerCtx context.Context
	stopMu    sync.Mutex
	stop      context.CancelFunc
	started   atomic.Bool
	wg        sync.WaitGroup

	scanInterval time.Duration
	retryBase    time.Duration
	retryMax     time.Duration
	clock        func() time.Time
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

// WithRootContext binds worker lifetime to the server/supervisor root context.
// HTTP request cancellation is intentionally not used as the worker lifetime.
func WithRootContext(value context.Context) Option {
	return func(r *Runner) {
		if value != nil {
			r.rootCtx = value
		}
	}
}

func WithScanInterval(value time.Duration) Option {
	return func(r *Runner) {
		if value > 0 {
			r.scanInterval = value
		}
	}
}

func WithRetryBackoff(base, maximum time.Duration) Option {
	return func(r *Runner) {
		if base > 0 {
			r.retryBase = base
		}
		if maximum > 0 {
			r.retryMax = maximum
		}
	}
}

func WithClock(clock func() time.Time) Option {
	return func(r *Runner) {
		if clock != nil {
			r.clock = clock
		}
	}
}

func NewRunner(service *application.Service, store repository.TransitionStore, options ...Option) *Runner {
	r := &Runner{
		service: service, store: store, maxSteps: defaultMaxSteps,
		rootCtx: context.Background(), scanInterval: defaultScanInterval,
		retryBase: defaultRetryBase, retryMax: defaultRetryMax,
		clock: func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		option(r)
	}
	r.workerCtx = r.rootCtx
	return r
}

type Result struct {
	Episode *episode.CommerceEpisode
	Steps   int
	Paused  bool
}

// StartSupervisor starts the persisted runnable sweep. It is safe to call
// more than once; only one ticker is ever active for a Runner.
func (r *Runner) StartSupervisor(ctx context.Context) error {
	if r == nil || r.store == nil {
		return repository.ErrRepositoryUnavailable
	}
	if _, ok := r.store.(repository.RunnableEpisodeLister); !ok {
		return repository.ErrRepositoryUnavailable
	}
	if !r.started.CompareAndSwap(false, true) {
		return nil
	}
	if ctx == nil {
		ctx = r.rootCtx
	}
	workerCtx, cancel := context.WithCancel(context.WithValue(ctx, workerContextKey{}, true))
	r.stopMu.Lock()
	r.stop = cancel
	r.stopMu.Unlock()
	r.workerMu.Lock()
	r.workerCtx = workerCtx
	r.workerMu.Unlock()
	r.wg.Add(1)
	go r.supervisorLoop(workerCtx)
	return nil
}

func (r *Runner) supervisorLoop(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(r.scanInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.sweep(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("commerce-runtime supervisor sweep failed: %v", err)
			}
		}
	}
}

// StopSupervisor cancels the supervisor-owned worker context and waits for
// the sweep loop to exit. Current adapter calls receive that same context.
func (r *Runner) StopSupervisor() {
	if r == nil {
		return
	}
	r.stopMu.Lock()
	stop := r.stop
	r.stop = nil
	r.stopMu.Unlock()
	if stop != nil {
		stop()
	}
	r.wg.Wait()
	r.started.Store(false)
}

func (r *Runner) Close() { r.StopSupervisor() }

func (r *Runner) SupervisorStarted() bool { return r != nil && r.started.Load() }

// ResumePersisted is the startup scan. The periodic supervisor repeats the
// same persisted-state scan, so a transient outage after startup is recoverable.
func (r *Runner) ResumePersisted(ctx context.Context) error { return r.sweep(ctx) }

func (r *Runner) sweep(ctx context.Context) error {
	lister, ok := r.store.(repository.RunnableEpisodeLister)
	if !ok {
		return repository.ErrRepositoryUnavailable
	}
	values, err := lister.ListRunnableEpisodes(ctx)
	if err != nil {
		return err
	}
	now := r.now()
	for _, value := range values {
		if value == nil || episode.IsTerminal(value.State) || value.State == episode.StateAwaitingParent {
			continue
		}
		status, statusErr := r.getExecutionStatus(ctx, value.EpisodeID)
		if statusErr != nil && !errors.Is(statusErr, repository.ErrNotFound) {
			return statusErr
		}
		if status != nil {
			if status.Status == repository.ExecutionError {
				continue
			}
			if status.NextRetryAt != nil && status.NextRetryAt.After(now) {
				continue
			}
		}
		r.Enqueue(nil, value.EpisodeID)
	}
	return nil
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

func (r *Runner) run(ctx context.Context, episodeID string) (Result, error) {
	if r == nil || r.service == nil || r.store == nil {
		return Result{}, repository.ErrRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	episodeID = strings.TrimSpace(episodeID)
	if episodeID == "" {
		return Result{}, repository.ErrNotFound
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	value, _ := r.locks.LoadOrStore(episodeID, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	current, err := r.service.GetEpisode(ctx, episodeID)
	if err != nil {
		return Result{}, err
	}
	if episode.IsTerminal(current.State) {
		_ = r.markCompleted(ctx, episodeID)
		return Result{Episode: current}, nil
	}
	if isWorkerContext(ctx) {
		if persisted, statusErr := r.getExecutionStatus(ctx, episodeID); statusErr == nil && persisted != nil && persisted.NextRetryAt != nil && persisted.NextRetryAt.After(r.now()) {
			return Result{Episode: current}, nil
		}
	}
	if !r.now().Before(current.DeadlineAt) {
		return r.expire(ctx, current)
	}
	status, err := r.beginExecution(ctx, episodeID)
	if err != nil {
		return Result{}, err
	}

	for step := 0; step < r.maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			// Leave RUNNING as a durable indication that the operation was
			// interrupted by shutdown; the next startup sweep resumes it.
			return Result{}, err
		}
		current, err = r.service.GetEpisode(ctx, episodeID)
		if err != nil {
			return Result{}, r.recordExecutionError(ctx, episodeID, status, err)
		}
		if episode.IsTerminal(current.State) {
			_ = r.markCompleted(ctx, episodeID)
			return Result{Episode: current, Steps: step}, nil
		}
		if current.State == episode.StateAwaitingParent {
			_ = r.markPaused(ctx, episodeID)
			return Result{Episode: current, Steps: step, Paused: true}, nil
		}
		if !r.now().Before(current.DeadlineAt) {
			return r.expire(ctx, current)
		}
		if err := r.step(ctx, current); err != nil {
			latest, getErr := r.service.GetEpisode(ctx, episodeID)
			if getErr == nil {
				if episode.IsTerminal(latest.State) {
					_ = r.markCompleted(ctx, episodeID)
					return Result{Episode: latest, Steps: step + 1}, nil
				}
				if latest.State == episode.StateAwaitingParent {
					_ = r.markPaused(ctx, episodeID)
					return Result{Episode: latest, Steps: step + 1, Paused: true}, nil
				}
				if !r.now().Before(latest.DeadlineAt) {
					return r.expire(ctx, latest)
				}
			}
			failureEpisode := current
			if getErr == nil && latest != nil {
				failureEpisode = latest
			}
			return Result{Episode: failureEpisode, Steps: step + 1}, r.recordExecutionError(ctx, episodeID, status, err)
		}
	}
	current, err = r.service.GetEpisode(ctx, episodeID)
	if err != nil {
		return Result{}, r.recordExecutionError(ctx, episodeID, status, err)
	}
	return Result{Episode: current, Steps: r.maxSteps}, r.recordExecutionError(ctx, episodeID, status, ErrRunnerStepLimit)
}

// Enqueue only prompts execution. The worker uses the supervisor-owned root
// context, never the HTTP request context and never an unbounded Background
// context in production. jobs is a dedupe guard, not an authoritative queue.
func (r *Runner) Enqueue(_ context.Context, episodeID string) {
	if r == nil || strings.TrimSpace(episodeID) == "" {
		return
	}
	episodeID = strings.TrimSpace(episodeID)
	if _, loaded := r.jobs.LoadOrStore(episodeID, struct{}{}); loaded {
		return
	}
	go func() {
		defer r.jobs.Delete(episodeID)
		_, _ = r.ResumeEpisode(r.workerContextValue(), episodeID)
	}()
}

func (r *Runner) workerContextValue() context.Context {
	r.workerMu.RLock()
	ctx := r.workerCtx
	r.workerMu.RUnlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (r *Runner) beginExecution(ctx context.Context, episodeID string) (*repository.EpisodeExecutionStatus, error) {
	store, ok := r.store.(repository.ExecutionStatusRepository)
	if !ok {
		return nil, nil
	}
	now := r.now()
	status, err := store.GetEpisodeExecutionStatus(ctx, episodeID)
	if errors.Is(err, repository.ErrNotFound) {
		status = &repository.EpisodeExecutionStatus{EpisodeID: episodeID, Status: repository.ExecutionIdle}
	} else if err != nil {
		return nil, err
	}
	status.Status = repository.ExecutionRunning
	status.AttemptCount++
	status.LastStartedAt = now
	status.NextRetryAt = nil
	status.UpdatedAt = now
	if err := store.UpsertEpisodeExecutionStatus(ctx, status); err != nil {
		return nil, err
	}
	return status, nil
}

func (r *Runner) getExecutionStatus(ctx context.Context, episodeID string) (*repository.EpisodeExecutionStatus, error) {
	store, ok := r.store.(repository.ExecutionStatusRepository)
	if !ok {
		return nil, nil
	}
	return store.GetEpisodeExecutionStatus(ctx, episodeID)
}

func (r *Runner) markCompleted(ctx context.Context, episodeID string) error {
	return r.finishExecution(ctx, episodeID, repository.ExecutionCompleted, "", "", nil)
}

func (r *Runner) markPaused(ctx context.Context, episodeID string) error {
	return r.finishExecution(ctx, episodeID, repository.ExecutionPaused, "", "", nil)
}

func (r *Runner) recordExecutionError(ctx context.Context, episodeID string, status *repository.EpisodeExecutionStatus, err error) error {
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	class := ClassifyError(err)
	state := repository.ExecutionError
	if class == Retryable {
		state = repository.ExecutionRetryWait
	}
	code := ErrorCode(err)
	message := SanitizeError(err)
	now := r.now()
	if status == nil {
		status, _ = r.getExecutionStatus(ctx, episodeID)
	}
	if status == nil {
		status = &repository.EpisodeExecutionStatus{EpisodeID: episodeID, AttemptCount: 1}
	}
	status.Status = state
	status.LastErrorCode = code
	status.LastErrorMessage = message
	status.LastErrorAt = &now
	status.UpdatedAt = now
	status.LastFinishedAt = &now
	if state == repository.ExecutionRetryWait {
		delay := r.retryDelay(status.AttemptCount)
		next := now.Add(delay)
		status.NextRetryAt = &next
	} else {
		status.NextRetryAt = nil
	}
	store, ok := r.store.(repository.ExecutionStatusRepository)
	if !ok {
		return err
	}
	if saveErr := store.UpsertEpisodeExecutionStatus(ctx, status); saveErr != nil {
		return saveErr
	}
	return err
}

func (r *Runner) finishExecution(ctx context.Context, episodeID string, state, code, message string, finishedAt *time.Time) error {
	store, ok := r.store.(repository.ExecutionStatusRepository)
	if !ok {
		return nil
	}
	status, err := store.GetEpisodeExecutionStatus(ctx, episodeID)
	if errors.Is(err, repository.ErrNotFound) {
		status = &repository.EpisodeExecutionStatus{EpisodeID: episodeID, AttemptCount: 0}
	} else if err != nil {
		return err
	}
	now := r.now()
	if finishedAt == nil {
		finishedAt = &now
	}
	if status.LastStartedAt.IsZero() {
		status.LastStartedAt = now
	}
	status.Status = state
	status.LastFinishedAt = finishedAt
	status.NextRetryAt = nil
	status.LastErrorCode = code
	status.LastErrorMessage = message
	status.LastErrorAt = nil
	status.UpdatedAt = now
	return store.UpsertEpisodeExecutionStatus(ctx, status)
}

func (r *Runner) retryDelay(attempt int) time.Duration {
	delay := r.retryBase
	if attempt < 1 {
		attempt = 1
	}
	for index := 1; index < attempt; index++ {
		if delay >= r.retryMax || delay > r.retryMax/2 {
			return r.retryMax
		}
		delay *= 2
	}
	if delay > r.retryMax {
		return r.retryMax
	}
	return delay
}

func (r *Runner) expire(ctx context.Context, current *episode.CommerceEpisode) (Result, error) {
	result, err := r.service.ExpireEpisode(ctx, current.EpisodeID, current.EpisodeID+":deadline")
	if err != nil {
		return Result{Episode: current}, r.recordExecutionError(ctx, current.EpisodeID, nil, err)
	}
	_ = r.markCompleted(ctx, current.EpisodeID)
	return Result{Episode: result.Episode}, nil
}

func (r *Runner) now() time.Time {
	if r.clock == nil {
		return time.Now().UTC()
	}
	return r.clock().UTC()
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
		var acquire contract.AcquireCapabilityRequest
		if err := json.Unmarshal(current.ContractSnapshot, &acquire); err != nil {
			return fmt.Errorf("decode episode contract snapshot: %w", err)
		}
		if threshold := acquire.Constraints.RequireParentConfirmationAboveMinor; threshold > 0 && quote.AmountMinor > threshold && current.RecoveryID == "" {
			_, err = r.service.RequestParentConfirmation(ctx, application.ParentConfirmationRequest{EpisodeID: current.EpisodeID, TraceID: traceID + ":parent-threshold"})
			return err
		}
		_, err = r.service.ReservePaymentIntentForEpisode(ctx, application.ReservePaymentIntentForEpisodeRequest{EpisodeID: current.EpisodeID, Quote: quote, IdempotencyKey: "payment:" + current.EpisodeID + ":" + quote.QuoteHash, Actor: "runtime", TraceID: traceID + ":reserve"})
		return err
	case episode.StatePaying:
		intent, err := r.currentPaymentIntent(ctx, current)
		if err != nil {
			return err
		}
		if intent.Status == payment.IntentPending || intent.Status == payment.IntentSubmitting || intent.Status == payment.IntentUnknown {
			_, err = r.service.ReconcilePayment(ctx, intent.IntentID, traceID+":reconcile")
			return err
		}
		_, err = r.service.AuthorizeAndSubmitPayment(ctx, intent.IntentID, traceID+":submit")
		return err
	case episode.StateClaiming:
		intent, err := r.currentPaymentIntent(ctx, current)
		if err != nil {
			return err
		}
		result, err := r.service.VerifyPaymentEntitlement(ctx, intent.IntentID, traceID+":entitlement")
		if err != nil {
			return err
		}
		if result.Event != nil && result.Event.Observation.Type == trace.ObservationEntitlementUnknown {
			// The payment success event and the verification-plane projection are
			// delivered asynchronously. Persist the pending observation, then let
			// the supervisor apply its bounded retry backoff instead of hot-looping
			// against the Gateway until its rate limit fires.
			return ErrEntitlementPending
		}
		return nil
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
	now := r.now()
	if now.Before(set.GeneratedAt) {
		timer := time.NewTimer(time.Until(set.GeneratedAt) + time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		now = r.now()
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
