package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/invocation"
	"github.com/stablepay/commerce-runtime/internal/payment"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
	"github.com/stablepay/commerce-runtime/internal/validator"
	"github.com/stablepay/commerce-runtime/internal/x402"
)

type MerchantAdapter = adapters.MerchantAdapter
type MerchantInvokeRequest = adapters.MerchantInvokeRequest
type MerchantInvokeResult = adapters.MerchantInvokeResult
type MerchantInvocation = invocation.MerchantInvocation
type PaymentRequirementFact = invocation.PaymentRequirementFact
type DeliveryArtifact = invocation.DeliveryArtifact
type ValidationEvidence = invocation.ValidationEvidence
type DeliveryValidator = validator.DeliveryValidator

type InvokeSelectedMerchantRequest struct {
	EpisodeID        string
	TraceID          string
	PaymentSignature string
}

type MerchantInvocationResult struct {
	Episode    *episode.CommerceEpisode
	Invocation *invocation.MerchantInvocation
	Response   adapters.MerchantInvokeResult
	Event      *episode.EpisodeEvent
	Replayed   bool
}

type ParsePaymentRequirementRequest struct {
	EpisodeID    string
	InvocationID string
	TraceID      string
}

type PaymentRequirementResult struct {
	Episode     *episode.CommerceEpisode
	Invocation  *invocation.MerchantInvocation
	Requirement *invocation.PaymentRequirementFact
	Quote       TrustedPaymentQuote
	Event       *episode.EpisodeEvent
	Replayed    bool
}

type InvokeDeliveryRequest struct {
	EpisodeID        string
	Attempt          int
	TraceID          string
	PaymentSignature string
}

type DeliveryInvocationResult struct {
	Episode    *episode.CommerceEpisode
	Invocation *invocation.MerchantInvocation
	Artifact   *invocation.DeliveryArtifact
	Response   adapters.MerchantInvokeResult
	Event      *episode.EpisodeEvent
	Replayed   bool
}

type ValidateDeliveryRequest struct {
	EpisodeID  string
	DeliveryID string
	TraceID    string
}

type DeliveryValidationResult struct {
	Episode  *episode.CommerceEpisode
	Artifact *invocation.DeliveryArtifact
	Evidence *invocation.ValidationEvidence
	Valid    bool
	Event    *episode.EpisodeEvent
	Replayed bool
}

type RetrySameMerchantRequest struct {
	EpisodeID   string
	Proposal    decision.DecisionProposal
	Action      trace.Action
	Observation trace.Observation
	Actor       string
	TraceID     string
}

func (s *Service) s4Store() (repository.S4Store, error) {
	if s == nil || s.store == nil {
		return nil, repository.ErrRepositoryUnavailable
	}
	store, ok := s.store.(repository.S4Store)
	if !ok {
		return nil, repository.ErrRepositoryUnavailable
	}
	return store, nil
}

func (s *Service) InvokeSelectedMerchant(ctx context.Context, request InvokeSelectedMerchantRequest) (MerchantInvocationResult, error) {
	store, err := s.s4Store()
	if err != nil {
		return MerchantInvocationResult{}, err
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return MerchantInvocationResult{}, err
	}
	capability, input, err := s.selectedCapabilityAndInput(ctx, current)
	if err != nil {
		return MerchantInvocationResult{}, err
	}
	traceID := strings.TrimSpace(request.TraceID)
	if traceID == "" {
		traceID = current.EpisodeID + ":initial-invoke"
	}
	operation := adapters.MerchantInvokeRequest{EpisodeID: current.EpisodeID, RequesterDID: current.RequesterDID, MerchantDID: current.SelectedMerchantDID, CapabilityID: current.SelectedCapabilityID, CatalogVersion: current.SelectedCatalogVersion, CatalogSnapshotHash: current.SelectedCatalogSnapshotHash, CatalogSnapshotRef: current.SelectedCatalogSnapshotRef, InputRef: inputRef(input), InputHash: input.SHA256, Attempt: 1, Phase: invocation.PhaseInitial, TraceID: traceID, IdempotencyKey: "invoke:initial:" + current.EpisodeID + ":" + current.SelectedMerchantDID, Endpoint: capability.InvokeEndpoint, PaymentSignature: request.PaymentSignature}
	if _, findErr := store.FindMerchantInvocationByIdempotencyKey(ctx, current.EpisodeID, operation.IdempotencyKey); errors.Is(findErr, repository.ErrFactNotFound) && current.State != episode.StateInvoking {
		return MerchantInvocationResult{}, decision.ErrActionNotAllowed
	} else if findErr != nil && !errors.Is(findErr, repository.ErrFactNotFound) {
		return MerchantInvocationResult{}, findErr
	}
	fact, response, adapterErr, replayed, err := s.executeMerchantInvocation(ctx, store, operation)
	if err != nil {
		return MerchantInvocationResult{}, err
	}
	key := operation.IdempotencyKey
	if existing, findErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, key); findErr == nil {
		latest, getErr := s.store.Get(ctx, current.EpisodeID)
		if getErr != nil {
			return MerchantInvocationResult{}, getErr
		}
		return MerchantInvocationResult{Episode: latest, Invocation: fact, Response: response, Event: existing, Replayed: true}, adapterErr
	}
	if current.State != episode.StateInvoking {
		return MerchantInvocationResult{}, decision.ErrActionNotAllowed
	}
	observationType := trace.ObservationMerchantResponse
	if response.HTTPStatus == 402 {
		observationType = trace.ObservationHTTP402
	}
	next := current.Clone()
	next.AttemptedMerchants = appendUnique(next.AttemptedMerchants, current.SelectedMerchantDID)
	next.ActionCount++
	if err := next.ApplyCommittedState(episode.StateInvoking, response.OccurredAt, ""); err != nil {
		return MerchantInvocationResult{}, err
	}
	event, replay, err := s.commitS4Event(ctx, current, next, trace.Action{Type: trace.ActionInvoke, IdempotencyKey: key, InputRef: operation.InputRef, InputHash: operation.InputHash}, trace.Observation{Type: observationType, Code: fmt.Sprintf("HTTP_%d", response.HTTPStatus), FactsRef: "invocation://" + fact.InvocationID, PayloadHash: response.PayloadHash}, nil, traceID)
	if err != nil {
		return MerchantInvocationResult{}, err
	}
	return MerchantInvocationResult{Episode: eventEpisodeOr(next, replay, s, ctx), Invocation: fact, Response: response, Event: event, Replayed: replayed || replay}, adapterErr
}

func (s *Service) ParsePaymentRequirement(ctx context.Context, request ParsePaymentRequirementRequest) (PaymentRequirementResult, error) {
	store, err := s.s4Store()
	if err != nil {
		return PaymentRequirementResult{}, err
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return PaymentRequirementResult{}, err
	}
	fact, err := store.GetMerchantInvocation(ctx, request.InvocationID)
	if err != nil {
		return PaymentRequirementResult{}, err
	}
	if fact.EpisodeID != current.EpisodeID || fact.Phase != invocation.PhaseInitial || fact.ResponseStatus != 402 {
		return PaymentRequirementResult{}, fmt.Errorf("%w: invocation is not an HTTP 402", x402.ErrInvalidChallenge)
	}
	if existing, findErr := store.FindPaymentRequirementByInvocation(ctx, current.EpisodeID, fact.InvocationID); findErr == nil {
		quote := trustedQuoteFromRequirement(*existing)
		key := "parse-402:" + fact.InvocationID
		event, eventErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, key)
		if errors.Is(eventErr, repository.ErrNotFound) {
			traceID := strings.TrimSpace(request.TraceID)
			if traceID == "" {
				traceID = current.EpisodeID + ":parse-402"
			}
			next := current.Clone()
			next.CurrentQuoteHash = existing.CanonicalQuoteHash
			next.ActionCount++
			if current.State == episode.StateNegotiating {
				if err := next.ApplyCommittedState(episode.StateNegotiating, existing.ObservedAt, ""); err != nil {
					return PaymentRequirementResult{}, err
				}
			} else {
				if err := next.ApplyCommittedState(episode.StateNegotiating, existing.ObservedAt, ""); err != nil {
					return PaymentRequirementResult{}, err
				}
			}
			var commitErr error
			event, _, commitErr = s.commitS4Transition(ctx, current, next, trace.Action{Type: trace.ActionParse402, IdempotencyKey: key, InputRef: fact.ResponseRef, InputHash: fact.ResponsePayloadHash}, trace.Observation{Type: trace.ObservationQuoteValid, Code: existing.CanonicalQuoteHash, FactsRef: existing.FactsRef, PayloadHash: existing.CanonicalQuoteHash}, nil, traceID, nil, nil, nil)
			if commitErr != nil {
				return PaymentRequirementResult{}, commitErr
			}
		} else if eventErr != nil {
			return PaymentRequirementResult{}, eventErr
		}
		latest, getErr := s.store.Get(ctx, current.EpisodeID)
		if getErr != nil {
			return PaymentRequirementResult{}, getErr
		}
		quote.RequesterDID = current.RequesterDID
		return PaymentRequirementResult{Episode: latest, Invocation: fact, Requirement: existing, Quote: quote, Event: event, Replayed: eventErr == nil}, nil
	} else if !errors.Is(findErr, repository.ErrFactNotFound) {
		return PaymentRequirementResult{}, findErr
	}
	capability, _, err := s.selectedCapabilityAndInput(ctx, current)
	if err != nil {
		return PaymentRequirementResult{}, err
	}
	parsed, err := x402.ParseRequired(fact.SelectedHeaders, fact.ResponseBody)
	if err != nil {
		return PaymentRequirementResult{}, err
	}
	if err := s.bindPaymentRequirement(current, capability, parsed); err != nil {
		return PaymentRequirementResult{}, err
	}
	expiresAt := fact.CompletedAt.Add(time.Duration(parsed.MaxTimeoutSeconds) * time.Second)
	if expiresAt.After(current.DeadlineAt) {
		expiresAt = current.DeadlineAt.Add(-time.Nanosecond)
	}
	if !expiresAt.After(fact.CompletedAt) {
		return PaymentRequirementResult{}, payment.ErrIntentExpired
	}
	requirement := &invocation.PaymentRequirementFact{PaymentRequirementID: "prf:" + fact.InvocationID, EpisodeID: current.EpisodeID, InvocationID: fact.InvocationID, MerchantDID: current.SelectedMerchantDID, CapabilityID: current.SelectedCapabilityID, CatalogVersion: current.SelectedCatalogVersion, CatalogSnapshotHash: current.SelectedCatalogSnapshotHash, ProtocolVersion: parsed.ProtocolVersion, Scheme: parsed.Scheme, Network: parsed.Network, Asset: parsed.Asset, AtomicAmount: parsed.AtomicAmount, AtomicDecimals: parsed.AtomicDecimals, BusinessAmountMinor: parsed.BusinessAmountMinor, Currency: parsed.Currency, PayTo: parsed.PayTo, PayeeDID: capability.PayeeDID, ResourceURL: parsed.ResourceURL, ProductID: parsed.ProductID, SkillDID: parsed.SkillDID, MaxTimeoutSeconds: parsed.MaxTimeoutSeconds, ObservedAt: fact.CompletedAt, ExpiresAt: expiresAt, RawPayloadHash: parsed.RawPayloadHash, CanonicalQuoteHash: parsed.CanonicalHash, FactsRef: "payment-requirement://" + fact.InvocationID}
	fact.PaymentRequiredRef = requirement.FactsRef
	if err := store.UpdateMerchantInvocation(ctx, fact); err != nil {
		return PaymentRequirementResult{}, err
	}
	traceID := strings.TrimSpace(request.TraceID)
	if traceID == "" {
		traceID = current.EpisodeID + ":parse-402"
	}
	next := current.Clone()
	next.CurrentQuoteHash = requirement.CanonicalQuoteHash
	next.ActionCount++
	if err := next.ApplyCommittedState(episode.StateNegotiating, fact.CompletedAt, ""); err != nil {
		return PaymentRequirementResult{}, err
	}
	event, replay, err := s.commitS4Transition(ctx, current, next, trace.Action{Type: trace.ActionParse402, IdempotencyKey: "parse-402:" + fact.InvocationID, InputRef: fact.ResponseRef, InputHash: fact.ResponsePayloadHash}, trace.Observation{Type: trace.ObservationQuoteValid, Code: requirement.CanonicalQuoteHash, FactsRef: requirement.FactsRef, PayloadHash: requirement.CanonicalQuoteHash}, nil, traceID, requirement, nil, nil)
	if err != nil {
		return PaymentRequirementResult{}, err
	}
	latest := next
	if replay {
		latest, _ = s.store.Get(ctx, current.EpisodeID)
	}
	quote := trustedQuoteFromRequirement(*requirement)
	quote.RequesterDID = current.RequesterDID
	return PaymentRequirementResult{Episode: latest, Invocation: fact, Requirement: requirement, Quote: quote, Event: event, Replayed: replay}, nil
}

func (s *Service) InvokeDelivery(ctx context.Context, request InvokeDeliveryRequest) (DeliveryInvocationResult, error) {
	store, err := s.s4Store()
	if err != nil {
		return DeliveryInvocationResult{}, err
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return DeliveryInvocationResult{}, err
	}
	intentStore, err := s.financeStore()
	if err != nil {
		return DeliveryInvocationResult{}, err
	}
	capability, _, err := s.selectedCapabilityAndInput(ctx, current)
	if err != nil {
		return DeliveryInvocationResult{}, err
	}
	intent, err := findPaymentIntentForSelection(ctx, intentStore, current, capability.PayeeDID)
	if err != nil {
		return DeliveryInvocationResult{}, err
	}
	if intent.Status != payment.IntentConfirmed {
		return DeliveryInvocationResult{}, payment.ErrIntentStateConflict
	}
	attempt := request.Attempt
	if attempt <= 0 {
		attempt = current.DeliveryAttemptCount + 1
	}
	traceID := strings.TrimSpace(request.TraceID)
	if traceID == "" {
		traceID = fmt.Sprintf("%s:delivery:%d", current.EpisodeID, attempt)
	}
	entitlementRef := ""
	if len(current.EntitlementRefs) > 0 {
		entitlementRef = current.EntitlementRefs[len(current.EntitlementRefs)-1]
	}
	operation := adapters.MerchantInvokeRequest{EpisodeID: current.EpisodeID, RequesterDID: current.RequesterDID, MerchantDID: current.SelectedMerchantDID, CapabilityID: current.SelectedCapabilityID, CatalogVersion: current.SelectedCatalogVersion, CatalogSnapshotHash: current.SelectedCatalogSnapshotHash, CatalogSnapshotRef: current.SelectedCatalogSnapshotRef, Attempt: attempt, Phase: invocation.PhaseDelivery, TraceID: traceID, IdempotencyKey: fmt.Sprintf("invoke:delivery:%s:%d", current.EpisodeID, attempt), Endpoint: capability.InvokeEndpoint, EntitlementRef: entitlementRef, PaymentIntentID: intent.IntentID, PaymentSignature: request.PaymentSignature}
	existingInvocation, findInvocationErr := store.FindMerchantInvocationByIdempotencyKey(ctx, current.EpisodeID, operation.IdempotencyKey)
	completedInvocation := findInvocationErr == nil && !existingInvocation.CompletedAt.IsZero() && existingInvocation.ResponseStatus != 0
	if findInvocationErr != nil && !errors.Is(findInvocationErr, repository.ErrFactNotFound) {
		return DeliveryInvocationResult{}, findInvocationErr
	}
	if !completedInvocation {
		if current.State != episode.StateInvokingDelivery {
			return DeliveryInvocationResult{}, decision.ErrActionNotAllowed
		}
		if attempt > current.MaxDeliveryAttempts {
			return DeliveryInvocationResult{}, decision.ErrAttemptLimit
		}
		if !s.clock().UTC().Before(current.DeadlineAt) {
			return DeliveryInvocationResult{}, payment.ErrIntentExpired
		}
		if strings.TrimSpace(entitlementRef) == "" {
			return DeliveryInvocationResult{}, decision.ErrPaymentBindingMismatch
		}
		if intent.MerchantDID != current.SelectedMerchantDID || intent.CapabilityID != current.SelectedCapabilityID || intent.PayeeDID != capability.PayeeDID {
			return DeliveryInvocationResult{}, fmt.Errorf("%w: intent merchant=%s capability=%s payee=%s; selected merchant=%s capability=%s payee=%s", decision.ErrPaymentBindingMismatch, intent.MerchantDID, intent.CapabilityID, intent.PayeeDID, current.SelectedMerchantDID, current.SelectedCapabilityID, capability.PayeeDID)
		}
	}
	fact, response, adapterErr, replayed, err := s.executeMerchantInvocation(ctx, store, operation)
	if err != nil {
		return DeliveryInvocationResult{}, err
	}
	if existing, findErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, operation.IdempotencyKey); findErr == nil {
		latest, getErr := s.store.Get(ctx, current.EpisodeID)
		if getErr != nil {
			return DeliveryInvocationResult{}, getErr
		}
		artifact, _ := store.GetDeliveryArtifact(ctx, "delivery:"+fact.InvocationID)
		return DeliveryInvocationResult{Episode: latest, Invocation: fact, Artifact: artifact, Response: response, Event: existing, Replayed: true}, adapterErr
	}
	next := current.Clone()
	next.DeliveryAttemptCount++
	next.ActionCount++
	after := episode.StateValidatingDelivery
	observationType := trace.ObservationMerchantResponse
	if response.HTTPStatus == 402 {
		after = episode.StateRecovering
		observationType = trace.ObservationHTTP402
	}
	if err := next.ApplyCommittedState(after, response.OccurredAt, ""); err != nil {
		return DeliveryInvocationResult{}, err
	}
	var delivery *invocation.DeliveryArtifact
	if response.HTTPStatus != 402 {
		delivery = &invocation.DeliveryArtifact{DeliveryID: "delivery:" + fact.InvocationID, EpisodeID: current.EpisodeID, InvocationID: fact.InvocationID, MerchantDID: current.SelectedMerchantDID, CapabilityID: current.SelectedCapabilityID, ContentType: response.ContentType, PayloadRef: response.PayloadRef, PayloadHash: response.PayloadHash, Body: append([]byte(nil), response.Body...), PaymentIntentID: intent.IntentID, EntitlementRef: entitlementRef, Attempt: attempt, HTTPStatus: response.HTTPStatus, ReceivedAt: response.OccurredAt}
		next.DeliveryRefs = appendUnique(next.DeliveryRefs, delivery.DeliveryID)
	}
	var recoveryContext *recovery.RecoveryContext
	if response.HTTPStatus == 402 {
		recoveryContext, err = s.currentRecoveryContext(ctx, current, recovery.ReasonMerchantAccessRejected, current.SelectedCandidateSetID, operation.IdempotencyKey)
		if err != nil {
			return DeliveryInvocationResult{}, err
		}
	}
	event, replay, err := s.commitS4Transition(ctx, current, next, trace.Action{Type: trace.ActionInvoke, IdempotencyKey: operation.IdempotencyKey, InputRef: operation.EntitlementRef, InputHash: operation.InputHash}, trace.Observation{Type: observationType, Code: fmt.Sprintf("HTTP_%d", response.HTTPStatus), FactsRef: "invocation://" + fact.InvocationID, PayloadHash: response.PayloadHash}, nil, traceID, nil, delivery, nil, recoveryContext)
	if err != nil {
		return DeliveryInvocationResult{}, err
	}
	latest := next
	if replay {
		latest, _ = s.store.Get(ctx, current.EpisodeID)
	}
	return DeliveryInvocationResult{Episode: latest, Invocation: fact, Artifact: delivery, Response: response, Event: event, Replayed: replayed || replay}, adapterErr
}

func (s *Service) ValidateDelivery(ctx context.Context, request ValidateDeliveryRequest) (DeliveryValidationResult, error) {
	store, err := s.s4Store()
	if err != nil {
		return DeliveryValidationResult{}, err
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return DeliveryValidationResult{}, err
	}
	artifact, err := store.GetDeliveryArtifact(ctx, request.DeliveryID)
	if err != nil {
		return DeliveryValidationResult{}, err
	}
	if artifact.EpisodeID != current.EpisodeID {
		return DeliveryValidationResult{}, invocation.ErrInvalidFact
	}
	var acquire contract.AcquireCapabilityRequest
	if err := json.Unmarshal(current.ContractSnapshot, &acquire); err != nil {
		return DeliveryValidationResult{}, err
	}
	if existing, findErr := store.FindValidationEvidenceByDelivery(ctx, artifact.DeliveryID, acquire.Validator.Name, acquire.Validator.Version); findErr == nil {
		key := "validate:" + artifact.DeliveryID + ":" + acquire.Validator.Name + ":" + acquire.Validator.Version
		event, eventErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, key)
		if errors.Is(eventErr, repository.ErrNotFound) {
			now := s.clock().UTC()
			next := current.Clone()
			next.ValidationEvidenceRefs = appendUnique(next.ValidationEvidenceRefs, existing.ValidationID)
			next.ActionCount++
			after := episode.StateRecovering
			observation := trace.ObservationDeliveryInvalid
			if existing.Valid {
				after = episode.StateFulfilled
				observation = trace.ObservationDeliveryValid
			}
			if err := next.ApplyCommittedState(after, now, existing.ReasonCode); err != nil {
				return DeliveryValidationResult{}, err
			}
			var commitErr error
			var recoveryContext *recovery.RecoveryContext
			if after == episode.StateRecovering {
				recoveryContext, commitErr = s.currentRecoveryContext(ctx, current, recovery.ReasonDeliveryInvalid, current.SelectedCandidateSetID, key)
				if commitErr != nil {
					return DeliveryValidationResult{}, commitErr
				}
			}
			event, _, commitErr = s.commitS4Transition(ctx, current, next, trace.Action{Type: trace.ActionValidateDelivery, IdempotencyKey: key}, trace.Observation{Type: observation, Code: existing.ReasonCode, FactsRef: "validation://" + existing.ValidationID, PayloadHash: existing.PayloadHash}, nil, current.EpisodeID+":validate:"+artifact.DeliveryID, nil, nil, nil, recoveryContext)
			if commitErr != nil {
				return DeliveryValidationResult{}, commitErr
			}
		} else if eventErr != nil {
			return DeliveryValidationResult{}, eventErr
		}
		latest, getErr := s.store.Get(ctx, current.EpisodeID)
		if getErr != nil {
			return DeliveryValidationResult{}, getErr
		}
		return DeliveryValidationResult{Episode: latest, Artifact: artifact, Evidence: existing, Valid: existing.Valid, Event: event, Replayed: eventErr == nil}, nil
	} else if !errors.Is(findErr, repository.ErrFactNotFound) {
		return DeliveryValidationResult{}, findErr
	}
	result, err := s.validatorRegistry.Validate(ctx, acquire.Validator, acquire, *artifact)
	if err != nil {
		return DeliveryValidationResult{}, err
	}
	now := s.clock().UTC()
	evidence := &invocation.ValidationEvidence{ValidationID: "validation:" + artifact.DeliveryID, EpisodeID: current.EpisodeID, DeliveryID: artifact.DeliveryID, ValidatorName: acquire.Validator.Name, ValidatorVersion: acquire.Validator.Version, Valid: result.Valid, ReasonCode: result.ReasonCode, EvidenceRefs: result.EvidenceRefs, PayloadHash: artifact.PayloadHash, CreatedAt: now}
	if current.State != episode.StateValidatingDelivery {
		return DeliveryValidationResult{}, decision.ErrActionNotAllowed
	}
	next := current.Clone()
	next.ValidationEvidenceRefs = appendUnique(next.ValidationEvidenceRefs, evidence.ValidationID)
	next.ActionCount++
	after := episode.StateRecovering
	observation := trace.ObservationDeliveryInvalid
	if result.Valid {
		after = episode.StateFulfilled
		observation = trace.ObservationDeliveryValid
	}
	if err := next.ApplyCommittedState(after, now, result.ReasonCode); err != nil {
		return DeliveryValidationResult{}, err
	}
	traceID := strings.TrimSpace(request.TraceID)
	if traceID == "" {
		traceID = current.EpisodeID + ":validate:" + artifact.DeliveryID
	}
	key := "validate:" + artifact.DeliveryID + ":" + acquire.Validator.Name + ":" + acquire.Validator.Version
	var recoveryContext *recovery.RecoveryContext
	if after == episode.StateRecovering {
		recoveryContext, err = s.currentRecoveryContext(ctx, current, recovery.ReasonDeliveryInvalid, current.SelectedCandidateSetID, key)
		if err != nil {
			return DeliveryValidationResult{}, err
		}
	}
	event, replay, err := s.commitS4Transition(ctx, current, next, trace.Action{Type: trace.ActionValidateDelivery, IdempotencyKey: key}, trace.Observation{Type: observation, Code: result.ReasonCode, FactsRef: "validation://" + evidence.ValidationID, PayloadHash: evidence.PayloadHash}, nil, traceID, nil, nil, evidence, recoveryContext)
	if err != nil {
		return DeliveryValidationResult{}, err
	}
	latest := next
	if replay {
		latest, _ = s.store.Get(ctx, current.EpisodeID)
	}
	return DeliveryValidationResult{Episode: latest, Artifact: artifact, Evidence: evidence, Valid: result.Valid, Event: event, Replayed: replay}, nil
}

// RetrySameMerchant accepts only a constrained proposal. It does not call the
// payment runtime and therefore cannot create a second economic purchase.
func (s *Service) RetrySameMerchant(ctx context.Context, request RetrySameMerchantRequest) (CommitResult, error) {
	store, err := s.financeStore()
	if err != nil {
		return CommitResult{}, err
	}
	current, err := s.store.Get(ctx, request.EpisodeID)
	if err != nil {
		return CommitResult{}, err
	}
	if strings.TrimSpace(request.Action.IdempotencyKey) != "" {
		if existing, replayErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, request.Action.IdempotencyKey); replayErr == nil {
			return CommitResult{Episode: current, Event: existing, Replayed: true}, nil
		} else if !errors.Is(replayErr, repository.ErrNotFound) {
			return CommitResult{}, replayErr
		}
	}
	if current.State != episode.StateRecovering {
		return CommitResult{}, decision.ErrActionNotAllowed
	}
	if current.RetryCount >= 1 {
		return CommitResult{}, decision.ErrAttemptLimit
	}
	capability, _, err := s.selectedCapabilityAndInput(ctx, current)
	if err != nil {
		return CommitResult{}, err
	}
	intent, err := findPaymentIntentForSelection(ctx, store, current, capability.PayeeDID)
	if err != nil {
		return CommitResult{}, err
	}
	if intent.Status != payment.IntentConfirmed || intent.MerchantDID != current.SelectedMerchantDID || intent.CapabilityID != current.SelectedCapabilityID {
		return CommitResult{}, decision.ErrPaymentBindingMismatch
	}
	if request.Proposal.ProposedAction == "" {
		request.Proposal.ProposedAction = trace.ActionRetrySameMerchant
	}
	if request.Proposal.ProposalID == "" {
		request.Proposal.ProposalID = "retry-" + current.EpisodeID
	}
	if request.Proposal.EpisodeID == "" {
		request.Proposal.EpisodeID = current.EpisodeID
	}
	if request.Proposal.BasedOnEventSequence == 0 {
		request.Proposal.BasedOnEventSequence = current.Version - 1
	}
	if request.Proposal.CreatedAt.IsZero() {
		request.Proposal.CreatedAt = s.clock().UTC().Add(-time.Nanosecond)
	}
	if request.Proposal.ExpiresAt.IsZero() {
		request.Proposal.ExpiresAt = s.clock().UTC().Add(time.Minute)
	}
	if request.Action.Type == "" {
		request.Action.Type = trace.ActionRetrySameMerchant
	}
	if request.Action.IdempotencyKey == "" {
		request.Action.IdempotencyKey = fmt.Sprintf("retry:%s:%d", current.EpisodeID, current.RetryCount+1)
	}
	if request.Observation.Type == "" {
		request.Observation.Type = trace.ObservationDeliveryInvalid
	}
	if request.Actor == "" {
		request.Actor = "runtime"
	}
	if request.TraceID == "" {
		request.TraceID = current.EpisodeID + ":retry"
	}
	if request.Proposal.Target != nil && (request.Proposal.Target.MerchantDID != current.SelectedMerchantDID || request.Proposal.Target.CapabilityID != current.SelectedCapabilityID) {
		return CommitResult{}, decision.ErrPaymentBindingMismatch
	}
	return s.CommitProposal(ctx, CommitRequest{Proposal: request.Proposal, Action: request.Action, Observation: request.Observation, Actor: request.Actor, TraceID: request.TraceID, runtimeRecoveryValidated: true})
}

// findPaymentIntentForSelection disambiguates repeated quote hashes across
// merchants. An x402 quote can be byte-identical for two merchants while the
// payment intents remain merchant-bound economic facts.
func findPaymentIntentForSelection(ctx context.Context, store repository.S2Store, current *episode.CommerceEpisode, payeeDID string) (*payment.PaymentIntent, error) {
	intents, err := store.ListPaymentIntents(ctx, current.EpisodeID)
	if err != nil {
		return nil, err
	}
	for index := len(intents) - 1; index >= 0; index-- {
		intent := intents[index]
		if intent == nil || intent.QuoteHash != current.CurrentQuoteHash {
			continue
		}
		if intent.MerchantDID == current.SelectedMerchantDID && intent.CapabilityID == current.SelectedCapabilityID && intent.PayeeDID == payeeDID {
			return intent, nil
		}
	}
	return nil, repository.ErrNotFound
}

func (s *Service) executeMerchantInvocation(ctx context.Context, store repository.S4Store, request adapters.MerchantInvokeRequest) (*invocation.MerchantInvocation, adapters.MerchantInvokeResult, error, bool, error) {
	requestHash, err := adapters.RequestHash(request)
	if err != nil {
		return nil, adapters.MerchantInvokeResult{}, nil, false, err
	}
	if existing, findErr := store.FindMerchantInvocationByIdempotencyKey(ctx, request.EpisodeID, request.IdempotencyKey); findErr == nil {
		if existing.RequestHash != requestHash {
			return nil, adapters.MerchantInvokeResult{}, nil, false, repository.ErrFactConflict
		}
		if existing.CompletedAt.IsZero() || existing.ResponseStatus == 0 {
			if s.clock().UTC().Before(existing.StartedAt.Add(s.invocationStaleAfter)) {
				return nil, adapters.MerchantInvokeResult{}, nil, false, invocation.ErrInvocationInFlight
			}
			if s.merchantAdapter == nil {
				return nil, adapters.MerchantInvokeResult{}, nil, false, adapters.ErrMerchantAdapterNotConfigured
			}
			response, callErr := s.merchantAdapter.Invoke(ctx, request)
			response, completeErr := s.completeMerchantInvocation(ctx, store, existing, response, callErr)
			if completeErr != nil {
				return nil, adapters.MerchantInvokeResult{}, callErr, false, completeErr
			}
			return existing, response, callErr, false, nil
		}
		return existing, merchantResponseFromFact(*existing), nil, true, nil
	} else if !errors.Is(findErr, repository.ErrFactNotFound) {
		return nil, adapters.MerchantInvokeResult{}, nil, false, findErr
	}
	if s.merchantAdapter == nil {
		return nil, adapters.MerchantInvokeResult{}, nil, false, adapters.ErrMerchantAdapterNotConfigured
	}
	now := s.clock().UTC()
	fact := &invocation.MerchantInvocation{InvocationID: s.idGenerator("inv"), EpisodeID: request.EpisodeID, MerchantDID: request.MerchantDID, CapabilityID: request.CapabilityID, CatalogVersion: request.CatalogVersion, CatalogSnapshotHash: request.CatalogSnapshotHash, Phase: request.Phase, Attempt: request.Attempt, RequestHash: requestHash, StartedAt: now, TraceID: request.TraceID, IdempotencyKey: request.IdempotencyKey}
	if err := store.SaveMerchantInvocation(ctx, fact); err != nil {
		return nil, adapters.MerchantInvokeResult{}, nil, false, err
	}
	response, callErr := s.merchantAdapter.Invoke(ctx, request)
	response, completeErr := s.completeMerchantInvocation(ctx, store, fact, response, callErr)
	if completeErr != nil {
		return nil, adapters.MerchantInvokeResult{}, callErr, false, completeErr
	}
	return fact, response, callErr, false, nil
}

func (s *Service) completeMerchantInvocation(ctx context.Context, store repository.S4Store, fact *invocation.MerchantInvocation, response adapters.MerchantInvokeResult, callErr error) (adapters.MerchantInvokeResult, error) {
	if callErr != nil || response.HTTPStatus == 0 {
		response = adapters.MerchantInvokeResult{HTTPStatus: 599, ContentType: "application/problem+json", Body: nil, OccurredAt: s.clock().UTC(), PayloadHash: invocation.PayloadHash(nil), PayloadRef: "merchant-response://" + strings.TrimPrefix(invocation.PayloadHash(nil), "sha256:")}
	}
	if response.OccurredAt.IsZero() {
		response.OccurredAt = s.clock().UTC()
	}
	if response.OccurredAt.Before(fact.StartedAt) {
		response.OccurredAt = fact.StartedAt
	}
	if response.PayloadHash == "" {
		response.PayloadHash = invocation.PayloadHash(response.Body)
	}
	if response.PayloadRef == "" {
		response.PayloadRef = "merchant-response://" + strings.TrimPrefix(response.PayloadHash, "sha256:")
	}
	fact.ResponseStatus = response.HTTPStatus
	fact.ResponseContentType = response.ContentType
	fact.ResponsePayloadHash = response.PayloadHash
	fact.ResponseRef = response.PayloadRef
	fact.SelectedHeaders = copyHeaders(response.Headers)
	fact.ResponseBody = append([]byte(nil), response.Body...)
	fact.CompletedAt = response.OccurredAt
	if err := store.UpdateMerchantInvocation(ctx, fact); err != nil {
		return adapters.MerchantInvokeResult{}, err
	}
	return response, nil
}

func (s *Service) selectedCapabilityAndInput(ctx context.Context, current *episode.CommerceEpisode) (*catalog.MerchantCapability, contract.Input, error) {
	if current == nil || current.SelectedCandidateSetID == "" || current.SelectedMerchantDID == "" || current.SelectedCapabilityID == "" {
		return nil, contract.Input{}, decision.ErrPaymentBindingMismatch
	}
	store, err := s.discoveryStore()
	if err != nil {
		return nil, contract.Input{}, err
	}
	set, err := store.GetCandidateSet(ctx, current.SelectedCandidateSetID)
	if err != nil {
		return nil, contract.Input{}, err
	}
	if err := set.ValidateAt(s.clock().UTC()); err != nil {
		return nil, contract.Input{}, err
	}
	candidate, ok := set.FindCandidate(current.SelectedMerchantDID, current.SelectedCapabilityID)
	if !ok || candidate.CatalogVersion != current.SelectedCatalogVersion || candidate.CatalogSnapshotHash != current.SelectedCatalogSnapshotHash || candidate.CatalogSnapshotRef != current.SelectedCatalogSnapshotRef {
		return nil, contract.Input{}, decision.ErrPaymentBindingMismatch
	}
	capability, err := store.GetCapabilityVersion(ctx, candidate.MerchantDID, candidate.CapabilityID, candidate.CatalogVersion)
	if err != nil {
		return nil, contract.Input{}, err
	}
	hash, err := capability.SnapshotHash()
	if err != nil || hash != candidate.CatalogSnapshotHash {
		return nil, contract.Input{}, decision.ErrPaymentBindingMismatch
	}
	var acquire contract.AcquireCapabilityRequest
	if err := json.Unmarshal(current.ContractSnapshot, &acquire); err != nil {
		return nil, contract.Input{}, err
	}
	return capability, acquire.Input, nil
}

func (s *Service) bindPaymentRequirement(current *episode.CommerceEpisode, capability *catalog.MerchantCapability, parsed x402.ParsedRequirement) error {
	if current == nil || capability == nil {
		return decision.ErrPaymentBindingMismatch
	}
	if parsed.Scheme != "exact" || !protocolSupported(capability.SupportedProtocolVersions, parsed.ProtocolVersion) || !strings.EqualFold(parsed.Currency, current.Budget.Currency) || parsed.BusinessAmountMinor > current.Budget.AvailableBudget {
		return decision.ErrPaymentBindingMismatch
	}
	if err := s.settlementPolicy.Validate(parsed); err != nil {
		return decision.ErrPaymentBindingMismatch
	}
	if !x402.EqualResourceURL(parsed.ResourceURL, capability.InvokeEndpoint.Endpoint) {
		return decision.ErrPaymentBindingMismatch
	}
	if strings.HasPrefix(strings.ToLower(capability.PayeeDID), "did:solana:") {
		if !solanaPayeeMatches(capability.PayeeDID, parsed.PayTo) {
			return decision.ErrPaymentBindingMismatch
		}
	} else if capability.PayeeDID != parsed.PayTo {
		return decision.ErrPaymentBindingMismatch
	}
	if parsed.SkillDID != "" && !strings.EqualFold(parsed.SkillDID, capability.PayeeDID) {
		return decision.ErrPaymentBindingMismatch
	}
	return nil
}

// Validate applies the settlement policy to an x402 payment challenge. It is
// public so an integration/black-box composition root can prove that the
// exact production network, asset and currency configuration is in force.
func (policy SettlementPolicy) Validate(parsed x402.ParsedRequirement) error {
	if strings.TrimSpace(policy.Network) == "" && !policy.AllowUnconstrainedForTests {
		return decision.ErrPaymentBindingMismatch
	}
	if policy.Network != "" && !strings.EqualFold(policy.Network, parsed.Network) {
		return decision.ErrPaymentBindingMismatch
	}
	expectedAsset, configuredAsset := policyAsset(policy, parsed.Currency)
	if !configuredAsset && !policy.AllowUnconstrainedForTests {
		return decision.ErrPaymentBindingMismatch
	}
	if configuredAsset && (expectedAsset == "" || !strings.EqualFold(expectedAsset, parsed.Asset)) {
		return decision.ErrPaymentBindingMismatch
	}
	return nil
}

func protocolSupported(supported []string, actual string) bool {
	for _, value := range supported {
		value = strings.ToLower(strings.TrimSpace(value))
		actual = strings.ToLower(strings.TrimSpace(actual))
		if value == actual {
			return true
		}
	}
	return false
}

func solanaPayeeMatches(did, payTo string) bool {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	value := strings.TrimPrefix(did, "did:solana:")
	if value == "" || payTo == "" {
		return false
	}
	return base58Equal(value, payTo, alphabet)
}

// base58Equal compares decoded Solana public keys without accepting a textual
// alias. It intentionally requires exactly 32 bytes.
func base58Equal(left, right, alphabet string) bool {
	decode := func(value string) ([]byte, bool) {
		number := new(big.Int)
		for _, char := range value {
			index := strings.IndexRune(alphabet, char)
			if index < 0 {
				return nil, false
			}
			number.Mul(number, big.NewInt(58))
			number.Add(number, big.NewInt(int64(index)))
		}
		raw := number.Bytes()
		leading := 0
		for leading < len(value) && value[leading] == alphabet[0] {
			leading++
		}
		decoded := append(make([]byte, leading), raw...)
		return decoded, len(decoded) == 32
	}
	a, ok := decode(left)
	if !ok {
		return false
	}
	b, ok := decode(right)
	if !ok {
		return false
	}
	return string(a) == string(b)
}

func (s *Service) commitS4Event(ctx context.Context, current, next *episode.CommerceEpisode, action trace.Action, observation trace.Observation, target *trace.Target, traceID string) (*episode.EpisodeEvent, bool, error) {
	return s.commitS4Transition(ctx, current, next, action, observation, target, traceID, nil, nil, nil)
}

func (s *Service) commitS4Transition(ctx context.Context, current, next *episode.CommerceEpisode, action trace.Action, observation trace.Observation, target *trace.Target, traceID string, requirement *invocation.PaymentRequirementFact, delivery *invocation.DeliveryArtifact, evidence *invocation.ValidationEvidence, recoveryContext ...*recovery.RecoveryContext) (*episode.EpisodeEvent, bool, error) {
	store, err := s.s4Store()
	if err != nil {
		return nil, false, err
	}
	if existing, err := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, action.IdempotencyKey); err == nil {
		return existing, true, nil
	} else if !errors.Is(err, repository.ErrNotFound) {
		return nil, false, err
	}
	if target == nil && action.Type == trace.ActionInvoke && current.SelectedMerchantDID != "" {
		target = &trace.Target{MerchantDID: current.SelectedMerchantDID, CapabilityID: current.SelectedCapabilityID, CatalogVersion: current.SelectedCatalogVersion, CatalogSnapshotHash: current.SelectedCatalogSnapshotHash, CatalogSnapshotRef: current.SelectedCatalogSnapshotRef}
	}
	event, err := episode.NewEvent(s.idGenerator("evt"), current.EpisodeID, current.Version, next.UpdatedAt, current.State, action, observation, trace.Decision{ProposedAction: action.Type, ProposalID: "runtime:" + action.IdempotencyKey, Reason: "runtime-owned merchant fact", Target: target}, trace.RuntimeVerdict{Allowed: true, Checks: []trace.RuntimeCheck{trace.Check("runtime_fact", true, "environment fact committed by runtime")}}, next.State, "runtime", traceID, s.runtimeVersion)
	if err != nil {
		return nil, false, err
	}
	var durableRecovery *recovery.RecoveryContext
	if len(recoveryContext) > 0 {
		durableRecovery = recoveryContext[0]
		if durableRecovery != nil {
			next.RecoveryID = durableRecovery.RecoveryID
		}
	}
	if err := store.CommitS4Transition(ctx, repository.S4Transition{EpisodeID: current.EpisodeID, ExpectedEpisodeVersion: current.Version, NextEpisode: next, Event: event, PaymentRequirement: requirement, DeliveryArtifact: delivery, ValidationEvidence: evidence, RecoveryContext: durableRecovery}); err != nil {
		if errors.Is(err, repository.ErrIdempotentReplay) || errors.Is(err, repository.ErrVersionConflict) {
			if replay, findErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, action.IdempotencyKey); findErr == nil {
				return replay, true, nil
			}
		}
		return nil, false, err
	}
	if err := s.projectTerminalMemory(ctx, next); err != nil {
		return nil, false, err
	}
	return event, false, nil
}

func inputRef(input contract.Input) string {
	if strings.TrimSpace(input.Ref) != "" {
		return input.Ref
	}
	return input.URI
}
func appendUnique(values []string, value string) []string {
	for _, item := range values {
		if item == value {
			return values
		}
	}
	return append(values, value)
}
func copyHeaders(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}
func merchantResponseFromFact(value invocation.MerchantInvocation) adapters.MerchantInvokeResult {
	return adapters.MerchantInvokeResult{HTTPStatus: value.ResponseStatus, ContentType: value.ResponseContentType, Headers: copyHeaders(value.SelectedHeaders), PayloadRef: value.ResponseRef, PayloadHash: value.ResponsePayloadHash, Body: append([]byte(nil), value.ResponseBody...), OccurredAt: value.CompletedAt}
}
func trustedQuoteFromRequirement(value invocation.PaymentRequirementFact) TrustedPaymentQuote {
	return TrustedPaymentQuote{MerchantDID: value.MerchantDID, CapabilityID: value.CapabilityID, PayeeDID: value.PayeeDID, QuoteHash: value.CanonicalQuoteHash, AmountMinor: value.BusinessAmountMinor, Currency: value.Currency, RequesterDID: "", ExpiresAt: value.ExpiresAt, ProtocolVersion: value.ProtocolVersion, Scheme: value.Scheme, Network: value.Network, Asset: value.Asset, ResourceURL: value.ResourceURL, ProductID: value.ProductID, SkillDID: value.SkillDID, PaymentRequirementRef: value.FactsRef}
}

func policyAsset(policy SettlementPolicy, currency string) (string, bool) {
	if policy.Assets == nil {
		return "", false
	}
	for key, value := range policy.Assets {
		if strings.EqualFold(strings.TrimSpace(key), strings.TrimSpace(currency)) {
			return strings.TrimSpace(value), true
		}
	}
	return "", true
}

func copyStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copy := make(map[string]string, len(values))
	for key, value := range values {
		copy[key] = value
	}
	return copy
}
func eventEpisodeOr(next *episode.CommerceEpisode, replay bool, s *Service, ctx context.Context) *episode.CommerceEpisode {
	if replay {
		if current, err := s.store.Get(ctx, next.EpisodeID); err == nil {
			return current
		}
	}
	return next
}
