package memory

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
)

const DefaultTTL = 90 * 24 * time.Hour

type MemoryMutation struct {
	Record      *MemoryRecord
	Observation *MemoryObservation
	Delta       OutcomeFacts
}

// MemoryProjector derives advisory memory only from persisted authoritative
// episode facts. It must be safe to replay after a terminal commit or crash.
type MemoryProjector interface {
	ProjectEpisode(context.Context, string) ([]MemoryMutation, error)
}

// Projector derives historical memory only after an episode is terminal. It
// reads authoritative persisted facts and writes observation plus aggregate
// through one MemoryStore operation. Re-running it is safe because the
// observation identity is deterministic.
type Projector struct {
	source      EpisodeSource
	store       MemoryStore
	clock       func() time.Time
	ttl         time.Duration
	writePolicy MemoryWritePolicy
}

var _ MemoryProjector = (*Projector)(nil)

type ProjectorOption func(*Projector)

func WithProjectorClock(clock func() time.Time) ProjectorOption {
	return func(p *Projector) {
		if clock != nil {
			p.clock = clock
		}
	}
}

func WithMemoryTTL(ttl time.Duration) ProjectorOption {
	return func(p *Projector) {
		if ttl > 0 {
			p.ttl = ttl
		}
	}
}

func WithMemoryWritePolicy(policy MemoryWritePolicy) ProjectorOption {
	return func(p *Projector) {
		if policy != nil {
			p.writePolicy = policy
		}
	}
}

func NewProjector(source EpisodeSource, store MemoryStore, options ...ProjectorOption) *Projector {
	p := &Projector{source: source, store: store, clock: func() time.Time { return time.Now().UTC() }, ttl: DefaultTTL, writePolicy: DeterministicWritePolicy{}}
	for _, option := range options {
		option(p)
	}
	return p
}

func (p *Projector) ProjectEpisode(ctx context.Context, episodeID string) ([]MemoryMutation, error) {
	if p == nil || p.source == nil || p.store == nil {
		return nil, errors.New("memory projector is not configured")
	}
	episodeID = strings.TrimSpace(episodeID)
	if episodeID == "" {
		return nil, errors.New("episode id is required")
	}
	ep, err := p.source.GetMemoryEpisode(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	if !episode.IsTerminal(episode.State(ep.State)) {
		return nil, errors.New("memory projection requires a terminal episode")
	}
	events, err := p.source.ListMemoryEpisodeEvents(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
	payments, err := p.source.ListMemoryPaymentIntents(ctx, episodeID)
	if err != nil {
		return nil, err
	}
	validations := make(map[string]ValidationView, len(ep.ValidationRefs))
	for _, ref := range ep.ValidationRefs {
		value, getErr := p.source.GetMemoryValidationEvidence(ctx, ref)
		if getErr != nil {
			return nil, getErr
		}
		validations[value.DeliveryID] = value
	}
	deliveries := make([]DeliveryView, 0, len(ep.DeliveryRefs))
	for _, ref := range ep.DeliveryRefs {
		value, getErr := p.source.GetMemoryDeliveryArtifact(ctx, ref)
		if getErr != nil {
			return nil, getErr
		}
		deliveries = append(deliveries, value)
	}
	now := p.clock().UTC()
	if now.IsZero() {
		now = ep.UpdatedAt.UTC()
	}
	observedAt := ep.UpdatedAt.UTC()
	if observedAt.IsZero() {
		observedAt = now
	}
	if observedAt.After(now) {
		now = observedAt
	}

	merchantStats := make(map[string]*projectionStats)
	capabilityStats := make(map[string]*projectionStats)
	for _, delivery := range deliveries {
		merchant := strings.TrimSpace(delivery.MerchantDID)
		capability := strings.ToLower(strings.TrimSpace(delivery.CapabilityID))
		if merchant == "" {
			continue
		}
		merchantValue := ensureStats(merchantStats, merchant, merchant, "")
		version, hash, ref := delivery.CatalogVersion, delivery.CatalogSnapshotHash, delivery.CatalogSnapshotRef
		if version == "" && hash == "" {
			version, hash, ref = catalogForAttempt(events, merchant, capability, delivery.ReceivedAt)
			if version == "" && hash == "" {
				version, ref = ep.SelectedCatalogVersion, ep.SelectedCatalogRef
			}
		}
		capabilityValue := ensureStats(capabilityStats, capabilityKey(merchant, capability, version, hash), merchant, capability)
		setCatalog(capabilityValue, version, hash, ref)
		validation, ok := validations[delivery.DeliveryID]
		applyDeliveryOutcome(merchantValue, capabilityValue, validation, ok, observedAt)
	}
	for _, payment := range payments {
		merchant := strings.TrimSpace(payment.MerchantDID)
		capability := strings.ToLower(strings.TrimSpace(payment.CapabilityID))
		if merchant == "" {
			continue
		}
		merchantValue := ensureStats(merchantStats, merchant, merchant, "")
		version, hash, ref := payment.CatalogVersion, payment.CatalogSnapshotHash, payment.CatalogSnapshotRef
		if version == "" && hash == "" {
			version, hash, ref = catalogForAttempt(events, merchant, capability, payment.UpdatedAt)
			if version == "" && hash == "" {
				version, ref = ep.SelectedCatalogVersion, ep.SelectedCatalogRef
			}
		}
		capabilityValue := ensureStats(capabilityStats, capabilityKey(merchant, capability, version, hash), merchant, capability)
		setCatalog(capabilityValue, version, hash, ref)
		if payment.Status == "FAILED" || payment.Status == "EXPIRED" {
			merchantValue.delta.PaymentFailedCount++
			capabilityValue.delta.PaymentFailedCount++
		}
		// A payment intent is one economic attempt. Delivery artifacts are
		// preferred for attempt counting because a single intent can be replayed.
		merchantValue.paymentAttempts++
		capabilityValue.paymentAttempts++
	}
	for _, event := range events {
		if event.Observation == "TOOL_ERROR" {
			merchant := strings.TrimSpace(event.MerchantDID)
			if merchant == "" {
				merchant = strings.TrimSpace(event.TargetMerchantDID)
			}
			if merchant != "" {
				ensureStats(merchantStats, merchant, merchant, "").delta.MerchantErrorCount++
				capability := strings.ToLower(strings.TrimSpace(event.CapabilityID))
				value := ensureStats(capabilityStats, capabilityKey(merchant, capability, event.TargetCatalogVersion, event.TargetCatalogSnapshotHash), merchant, capability)
				setCatalog(value, event.TargetCatalogVersion, event.TargetCatalogSnapshotHash, event.TargetCatalogSnapshotRef)
				value.delta.MerchantErrorCount++
			}
		}
	}
	applySwitchAway(events, merchantStats, capabilityStats)
	for _, value := range merchantStats {
		value.finalizeAttempts()
	}
	for _, value := range capabilityStats {
		value.finalizeAttempts()
	}

	mutations := make([]MemoryMutation, 0, len(merchantStats)+len(capabilityStats)+1)
	for _, value := range sortedStats(merchantStats) {
		mutations = append(mutations, p.newMutation(ep, events, value, MemoryMerchantOutcome, ScopeMerchant, observedAt, now))
	}
	for _, value := range sortedStats(capabilityStats) {
		mutations = append(mutations, p.newMutation(ep, events, value, MemoryCapabilityOutcome, ScopeMerchantCapability, observedAt, now))
	}
	if recoveryMutation := p.recoveryMutation(ep, events, observedAt, now); recoveryMutation != nil {
		mutations = append(mutations, *recoveryMutation)
	}
	for index := range mutations {
		mutation := &mutations[index]
		existing, getErr := p.store.GetMemory(ctx, mutation.Record.MemoryID)
		if getErr != nil && !errors.Is(getErr, ErrMemoryNotFound) {
			return mutations[:index], getErr
		}
		if getErr == nil {
			observations, resolveErr := p.store.Resolve(ctx, mutation.Record.MemoryID, 10000)
			if resolveErr != nil {
				return mutations[:index], resolveErr
			}
			found := false
			for _, observation := range observations {
				if observation != nil && observation.ObservationID == mutation.Observation.ObservationID {
					found = true
					break
				}
			}
			if found {
				continue
			}
		}
		decision, decideErr := p.writePolicy.Decide(ctx, *mutation, existing)
		if decideErr != nil {
			return mutations[:index], decideErr
		}
		if decision == MemoryWriteNoop {
			continue
		}
		if decision == MemoryWriteDelete {
			return mutations[:index], errors.New("deterministic memory write policy does not auto-delete")
		}
		if err := p.store.SaveObservationAndUpdateAggregate(ctx, mutation.Record, mutation.Observation, mutation.Delta); err != nil {
			return mutations[:index], err
		}
	}
	return mutations, nil
}

type projectionStats struct {
	merchant        string
	capability      string
	catalogVersion  string
	catalogHash     string
	catalogRef      string
	deliveryCount   int
	paymentAttempts int
	delta           OutcomeFacts
}

func capabilityKey(merchant, capability, version, hash string) string {
	return strings.Join([]string{merchant, capability, version, hash}, "\x00")
}

func setCatalog(value *projectionStats, version, hash, ref string) {
	if value == nil {
		return
	}
	version, hash, ref = strings.TrimSpace(version), strings.TrimSpace(hash), strings.TrimSpace(ref)
	if version == "" && hash == "" && ref == "" {
		return
	}
	value.catalogVersion, value.catalogHash, value.catalogRef = version, hash, ref
}

func catalogForAttempt(events []EventView, merchant, capability string, at time.Time) (string, string, string) {
	version, hash, ref := "", "", ""
	var selectedAt time.Time
	for _, event := range events {
		if !at.IsZero() && !event.OccurredAt.IsZero() && event.OccurredAt.After(at) {
			continue
		}
		if strings.TrimSpace(event.TargetMerchantDID) == merchant && strings.ToLower(strings.TrimSpace(event.CapabilityID)) == capability && (event.TargetCatalogVersion != "" || event.TargetCatalogSnapshotHash != "") && (selectedAt.IsZero() || event.OccurredAt.IsZero() || !event.OccurredAt.Before(selectedAt)) {
			version, hash, ref = event.TargetCatalogVersion, event.TargetCatalogSnapshotHash, event.TargetCatalogSnapshotRef
			selectedAt = event.OccurredAt
		}
	}
	return version, hash, ref
}

func ensureStats(values map[string]*projectionStats, key, merchant, capability string) *projectionStats {
	if value, ok := values[key]; ok {
		return value
	}
	value := &projectionStats{merchant: merchant, capability: capability}
	values[key] = value
	return value
}

func applyDeliveryOutcome(merchant, capability *projectionStats, validation ValidationView, ok bool, observedAt time.Time) {
	merchant.deliveryCount++
	capability.deliveryCount++
	if !ok {
		merchant.delta.LastOutcome = "DELIVERY_RECEIVED"
		capability.delta.LastOutcome = "DELIVERY_RECEIVED"
	} else if validation.Valid {
		merchant.delta.DeliveryValidCount++
		merchant.delta.FulfilledCount++
		merchant.delta.LastOutcome = "DELIVERY_VALID"
		capability.delta.DeliveryValidCount++
		capability.delta.FulfilledCount++
		capability.delta.LastOutcome = "DELIVERY_VALID"
	} else {
		merchant.delta.DeliveryInvalidCount++
		merchant.delta.RecentFailureStreak++
		merchant.delta.LastOutcome = "DELIVERY_INVALID"
		capability.delta.DeliveryInvalidCount++
		capability.delta.RecentFailureStreak++
		capability.delta.LastOutcome = "DELIVERY_INVALID"
	}
	merchant.delta.LastOutcomeAt = observedAt
	capability.delta.LastOutcomeAt = observedAt
}

func (s *projectionStats) finalizeAttempts() {
	s.delta.AttemptCount = s.deliveryCount
	if s.delta.AttemptCount == 0 && s.paymentAttempts > 0 {
		s.delta.AttemptCount = s.paymentAttempts
	}
}

func applySwitchAway(events []EventView, merchants map[string]*projectionStats, capabilities map[string]*projectionStats) {
	currentMerchant := ""
	currentCapability := ""
	currentVersion, currentHash, currentRef := "", "", ""
	for _, event := range events {
		if event.TargetMerchantDID != "" && event.Action != "SWITCH_MERCHANT" {
			currentMerchant = event.TargetMerchantDID
			currentCapability = strings.ToLower(strings.TrimSpace(event.CapabilityID))
			if event.TargetCatalogVersion != "" || event.TargetCatalogSnapshotHash != "" || event.TargetCatalogSnapshotRef != "" {
				currentVersion, currentHash, currentRef = event.TargetCatalogVersion, event.TargetCatalogSnapshotHash, event.TargetCatalogSnapshotRef
			}
		}
		if event.Action != "SWITCH_MERCHANT" {
			continue
		}
		if currentMerchant != "" {
			ensureStats(merchants, currentMerchant, currentMerchant, "").delta.SwitchAwayCount++
			key := capabilityKey(currentMerchant, currentCapability, currentVersion, currentHash)
			value, ok := capabilities[key]
			if !ok && currentVersion == "" && currentHash == "" {
				prefix := currentMerchant + "\x00" + currentCapability + "\x00"
				for candidateKey, candidate := range capabilities {
					if strings.HasPrefix(candidateKey, prefix) {
						value, ok = candidate, true
						break
					}
				}
			}
			if !ok {
				value = ensureStats(capabilities, key, currentMerchant, currentCapability)
			}
			setCatalog(value, currentVersion, currentHash, currentRef)
			value.delta.SwitchAwayCount++
		}
		if event.TargetMerchantDID != "" {
			currentMerchant = event.TargetMerchantDID
			currentCapability = strings.ToLower(strings.TrimSpace(event.CapabilityID))
			if event.TargetCatalogVersion != "" || event.TargetCatalogSnapshotHash != "" || event.TargetCatalogSnapshotRef != "" {
				currentVersion, currentHash, currentRef = event.TargetCatalogVersion, event.TargetCatalogSnapshotHash, event.TargetCatalogSnapshotRef
			}
		}
	}
}

func sortedStats(values map[string]*projectionStats) []*projectionStats {
	result := make([]*projectionStats, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].merchant != result[j].merchant {
			return result[i].merchant < result[j].merchant
		}
		if result[i].capability != result[j].capability {
			return result[i].capability < result[j].capability
		}
		if result[i].catalogVersion != result[j].catalogVersion {
			return result[i].catalogVersion < result[j].catalogVersion
		}
		return result[i].catalogHash < result[j].catalogHash
	})
	return result
}

func (p *Projector) newMutation(ep EpisodeView, events []EventView, stats *projectionStats, typ MemoryType, scope MemoryScope, observedAt, now time.Time) MemoryMutation {
	requester := ""
	parentSession := ""
	merchant := stats.merchant
	capability := stats.capability
	if scope == ScopeMerchant {
		capability = ""
	}
	catalogVersion := ""
	catalogHash := ""
	catalogRef := ""
	if scope == ScopeMerchantCapability {
		catalogVersion = stats.catalogVersion
		catalogHash = stats.catalogHash
		catalogRef = stats.catalogRef
	}
	memoryID := MemoryIDForSnapshot(typ, scope, requester, parentSession, merchant, capability, catalogVersion, catalogHash)
	if typ == MemoryMerchantOutcome || typ == MemoryCapabilityOutcome {
		requester = ""
		parentSession = ""
	}
	validUntil := observedAt.Add(p.ttl)
	record := &MemoryRecord{MemoryID: memoryID, Type: typ, Scope: scope, RequesterDID: requester, ParentSessionID: parentSession, MerchantDID: merchant, CapabilityID: capability, CatalogVersion: catalogVersion, CatalogSnapshotHash: catalogHash, CatalogSnapshotRef: catalogRef, StructuredFacts: stats.delta, ObservationCount: 1, Confidence: 0.5, FirstObservedAt: observedAt, LastObservedAt: observedAt, ValidFrom: observedAt, ValidUntil: &validUntil, CreatedAt: observedAt, UpdatedAt: now, FactsRef: "memory://" + memoryID, SourceEpisodeIDs: []string{ep.EpisodeID}, SourceEventRefs: eventIDs(events), SourceEvidenceRefs: evidenceRefs(events)}
	record.Summary = Summarize(*record)
	_ = record.RefreshPayloadHash()
	eventRef := firstEventRef(events, ep.EpisodeID)
	observation := &MemoryObservation{ObservationID: ObservationIDFor(memoryID, ep.EpisodeID, string(typ)), MemoryID: memoryID, SourceEpisodeID: ep.EpisodeID, SourceEventRef: eventRef, ObservationKind: string(typ), Outcome: stats.delta.LastOutcome, ObservedAt: observedAt, CatalogVersion: catalogVersion, CatalogSnapshotHash: catalogHash, CatalogSnapshotRef: catalogRef}
	observation.SourceEvidenceRefs = evidenceRefs(events)
	_ = observation.RefreshPayloadHash()
	return MemoryMutation{Record: record, Observation: observation, Delta: stats.delta}
}

func (p *Projector) recoveryMutation(ep EpisodeView, events []EventView, observedAt, now time.Time) *MemoryMutation {
	var delta OutcomeFacts
	var sourceRefs []string
	lastAction := ""
	triggerRef, actionRef, terminalRef := "", "", ""
	triggerReason := ""
	for _, event := range events {
		if triggerRef == "" && (event.Observation == "DELIVERY_INVALID" || event.ObservationCode == "DELIVERY_INVALID") {
			triggerRef, triggerReason = event.EventID, "DELIVERY_INVALID"
		}
		switch event.Action {
		case "RETRY_SAME_MERCHANT", "SWITCH_MERCHANT", "REDISCOVER", "ASK_PARENT":
			delta.RecoveryAttemptCount++
			lastAction = event.Action
			if actionRef == "" {
				actionRef = event.EventID
			}
			switch event.Action {
			case "RETRY_SAME_MERCHANT":
				delta.RetryAfterDeliveryInvalidCount++
			case "SWITCH_MERCHANT":
				delta.SwitchAfterDeliveryInvalidCount++
			case "REDISCOVER":
				delta.RediscoverCount++
			case "ASK_PARENT":
				delta.AskParentCount++
			}
			sourceRefs = append(sourceRefs, event.EventID)
		case "PARENT_DECISION":
			if event.Observation == "PARENT_APPROVED" {
				delta.ParentApprovalCount++
			}
			if event.Observation == "PARENT_DENIED" {
				delta.ParentDenialCount++
			}
			sourceRefs = append(sourceRefs, event.EventID)
		}
		if episode.IsTerminal(episode.State(event.StateAfter)) {
			terminalRef = event.EventID
		}
	}
	if delta.RecoveryAttemptCount == 0 && delta.ParentApprovalCount == 0 && delta.ParentDenialCount == 0 {
		return nil
	}
	delta.RecoveryAction = lastAction
	if episode.IsTerminal(episode.State(ep.State)) && ep.State == string(episode.StateFulfilled) {
		delta.RecoverySuccessCount = 1
		delta.RecoveryResult = "FULFILLED"
		if lastAction == "RETRY_SAME_MERCHANT" {
			delta.RetryAfterDeliveryInvalidSuccessCount = 1
		}
		if lastAction == "SWITCH_MERCHANT" {
			delta.SwitchAfterDeliveryInvalidSuccessCount = 1
		}
	} else {
		delta.RecoveryResult = ep.State
	}
	delta.RecoveryTriggerReason = triggerReason
	scope := ScopeRequester
	parentSession := ""
	if strings.TrimSpace(ep.ParentSessionID) != "" {
		scope = ScopeParentSession
		parentSession = strings.TrimSpace(ep.ParentSessionID)
	}
	memoryID := MemoryIDFor(MemoryRecoveryOutcome, scope, ep.RequesterDID, parentSession, "", "", "")
	validUntil := observedAt.Add(p.ttl)
	record := &MemoryRecord{MemoryID: memoryID, Type: MemoryRecoveryOutcome, Scope: scope, RequesterDID: ep.RequesterDID, ParentSessionID: parentSession, Summary: "", StructuredFacts: delta, ObservationCount: 1, Confidence: 0.5, FirstObservedAt: observedAt, LastObservedAt: observedAt, ValidFrom: observedAt, ValidUntil: &validUntil, CreatedAt: observedAt, UpdatedAt: now, FactsRef: "memory://" + memoryID, SourceEpisodeIDs: []string{ep.EpisodeID}, SourceEventRefs: sourceRefs, SourceEvidenceRefs: evidenceRefs(events)}
	record.Summary = Summarize(*record)
	_ = record.RefreshPayloadHash()
	related := []string{}
	for _, ref := range []string{triggerRef, actionRef, terminalRef} {
		if strings.TrimSpace(ref) == "" {
			continue
		}
		seen := false
		for _, existing := range related {
			if existing == ref {
				seen = true
				break
			}
		}
		if !seen {
			related = append(related, ref)
		}
	}
	obs := &MemoryObservation{ObservationID: ObservationIDFor(memoryID, ep.EpisodeID, string(MemoryRecoveryOutcome)), MemoryID: memoryID, SourceEpisodeID: ep.EpisodeID, SourceEventRef: firstEventRef(events, ep.EpisodeID), SourceEvidenceRefs: evidenceRefs(events), ObservationKind: string(MemoryRecoveryOutcome), Outcome: lastAction, ObservedAt: observedAt, TriggerEventRef: triggerRef, RecoveryActionEventRef: actionRef, TerminalEventRef: terminalRef, RelatedEventRefs: related}
	_ = obs.RefreshPayloadHash()
	return &MemoryMutation{Record: record, Observation: obs, Delta: delta}
}

func eventIDs(events []EventView) []string {
	result := make([]string, 0, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.EventID) != "" {
			result = append(result, event.EventID)
		}
	}
	return result
}

func firstEventRef(events []EventView, episodeID string) string {
	if len(events) > 0 && strings.TrimSpace(events[0].EventID) != "" {
		return events[0].EventID
	}
	return "episode-event://" + episodeID
}

func evidenceRefs(events []EventView) []string {
	result := make([]string, 0, len(events)*2)
	for _, event := range events {
		if strings.TrimSpace(event.FactsRef) != "" {
			result = append(result, event.FactsRef)
		}
		if strings.TrimSpace(event.PayloadHash) != "" {
			result = append(result, event.PayloadHash)
		}
	}
	return result
}
