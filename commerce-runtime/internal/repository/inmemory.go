package repository

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/payment"
)

// InMemoryStore is a deterministic repository for unit tests and local
// demonstrations. The mutex models the atomic boundary of the SQL transaction.
type InMemoryStore struct {
	mu                  sync.RWMutex
	episodes            map[string]*episode.CommerceEpisode
	byRequest           map[string]string
	events              map[string][]*episode.EpisodeEvent
	ledger              map[string][]*ledger.LedgerEntry
	intents             map[string]*payment.PaymentIntent
	intentByIdempotency map[string]string
	intentByEconomicKey map[string]string
	capabilities        map[string]*catalog.MerchantCapability
	currentCapabilities map[string]string
	candidateSets       map[string]*catalog.CandidateSet
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		episodes:            make(map[string]*episode.CommerceEpisode),
		byRequest:           make(map[string]string),
		events:              make(map[string][]*episode.EpisodeEvent),
		ledger:              make(map[string][]*ledger.LedgerEntry),
		intents:             make(map[string]*payment.PaymentIntent),
		intentByIdempotency: make(map[string]string),
		intentByEconomicKey: make(map[string]string),
		capabilities:        make(map[string]*catalog.MerchantCapability),
		currentCapabilities: make(map[string]string),
		candidateSets:       make(map[string]*catalog.CandidateSet),
	}
}

func capabilityKey(merchantDID, capabilityID string) string {
	return merchantDID + "\x00" + capabilityID
}

func capabilityVersionKey(merchantDID, capabilityID, version string) string {
	return capabilityKey(merchantDID, capabilityID) + "\x00" + version
}

func (s *InMemoryStore) SaveCapabilityVersion(ctx context.Context, value *catalog.MerchantCapability) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil {
		return catalog.ErrInvalidCapability
	}
	normalized := value.Normalize()
	if err := normalized.Validate(); err != nil {
		return err
	}
	hash, err := normalized.SnapshotHash()
	if err != nil {
		return err
	}
	key := capabilityVersionKey(normalized.MerchantDID, normalized.CapabilityID, normalized.CatalogVersion)
	group := capabilityKey(normalized.MerchantDID, normalized.CapabilityID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.capabilities[key]; ok {
		existingHash, hashErr := existing.SnapshotHash()
		if hashErr == nil && existingHash == hash {
			return nil
		}
		return ErrCatalogVersionConflict
	}
	s.capabilities[key] = normalized.Clone()
	currentVersion, ok := s.currentCapabilities[group]
	if !ok || catalog.CompareVersions(normalized.CatalogVersion, currentVersion) > 0 {
		s.currentCapabilities[group] = normalized.CatalogVersion
	}
	return nil
}

func (s *InMemoryStore) GetCapabilityVersion(ctx context.Context, merchantDID, capabilityID, version string) (*catalog.MerchantCapability, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.capabilities[capabilityVersionKey(strings.TrimSpace(merchantDID), strings.ToLower(strings.TrimSpace(capabilityID)), strings.TrimSpace(version))]
	if !ok {
		return nil, ErrNotFound
	}
	return value.Clone(), nil
}

func (s *InMemoryStore) GetCurrentActiveCapability(ctx context.Context, merchantDID, capabilityID string) (*catalog.MerchantCapability, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	group := capabilityKey(strings.TrimSpace(merchantDID), strings.ToLower(strings.TrimSpace(capabilityID)))
	version, ok := s.currentCapabilities[group]
	if !ok {
		return nil, ErrNotFound
	}
	value, ok := s.capabilities[capabilityVersionKey(strings.TrimSpace(merchantDID), strings.ToLower(strings.TrimSpace(capabilityID)), version)]
	if !ok {
		return nil, ErrNotFound
	}
	return value.Clone(), nil
}

func (s *InMemoryStore) ListActiveCapabilities(ctx context.Context) ([]*catalog.MerchantCapability, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*catalog.MerchantCapability, 0, len(s.currentCapabilities))
	for group, version := range s.currentCapabilities {
		value, ok := s.capabilities[group+"\x00"+version]
		if !ok || value.Status != catalog.StatusActive || value.Availability != catalog.AvailabilityAvailable {
			continue
		}
		result = append(result, value.Clone())
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].MerchantDID != result[j].MerchantDID {
			return result[i].MerchantDID < result[j].MerchantDID
		}
		return result[i].CapabilityID < result[j].CapabilityID
	})
	return result, nil
}

func (s *InMemoryStore) SaveCandidateSet(ctx context.Context, value *catalog.CandidateSet) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil {
		return catalog.ErrInvalidCandidateSet
	}
	normalized := value.Normalize()
	if err := normalized.Validate(); err != nil {
		return err
	}
	hash, err := normalized.SnapshotHash()
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.episodes[normalized.EpisodeID]; !ok {
		return ErrNotFound
	}
	if existing, ok := s.candidateSets[normalized.CandidateSetID]; ok {
		existingHash, hashErr := existing.SnapshotHash()
		if hashErr == nil && existingHash == hash {
			return nil
		}
		return ErrCandidateSetConflict
	}
	s.candidateSets[normalized.CandidateSetID] = normalized.Clone()
	return nil
}

func (s *InMemoryStore) GetCandidateSet(ctx context.Context, candidateSetID string) (*catalog.CandidateSet, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.candidateSets[strings.TrimSpace(candidateSetID)]
	if !ok {
		return nil, ErrNotFound
	}
	return value.Clone(), nil
}

func (s *InMemoryStore) Create(ctx context.Context, value *episode.CommerceEpisode) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.episodes[value.EpisodeID]; ok {
		return ErrRequestIDConflict
	}
	if existingID, ok := s.byRequest[value.RequestID]; ok && existingID != value.EpisodeID {
		return ErrRequestIDConflict
	}
	s.episodes[value.EpisodeID] = value.Clone()
	s.byRequest[value.RequestID] = value.EpisodeID
	return nil
}

func (s *InMemoryStore) Get(ctx context.Context, episodeID string) (*episode.CommerceEpisode, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.episodes[episodeID]
	if !ok {
		return nil, ErrNotFound
	}
	return value.Clone(), nil
}

func (s *InMemoryStore) FindByRequestID(ctx context.Context, requestID string) (*episode.CommerceEpisode, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	episodeID, ok := s.byRequest[requestID]
	if !ok {
		return nil, ErrNotFound
	}
	return s.episodes[episodeID].Clone(), nil
}

func (s *InMemoryStore) UpdateOptimistic(ctx context.Context, episodeID string, expectedVersion uint64, next *episode.CommerceEpisode) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateLocked(episodeID, expectedVersion, next)
}

func (s *InMemoryStore) Append(ctx context.Context, value *episode.EpisodeEvent) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.episodes[value.EpisodeID]; !ok {
		return ErrNotFound
	}
	return s.appendLocked(value)
}

func (s *InMemoryStore) ListByEpisode(ctx context.Context, episodeID string) ([]*episode.EpisodeEvent, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := s.events[episodeID]
	result := make([]*episode.EpisodeEvent, 0, len(values))
	for _, value := range values {
		result = append(result, value.Clone())
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Sequence < result[j].Sequence })
	return result, nil
}

func (s *InMemoryStore) FindByIdempotencyKey(ctx context.Context, episodeID, key string) (*episode.EpisodeEvent, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, ErrNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, value := range s.events[episodeID] {
		if value.Action.IdempotencyKey == key {
			return value.Clone(), nil
		}
	}
	return nil, ErrNotFound
}

func (s *InMemoryStore) CommitTransition(ctx context.Context, episodeID string, expectedVersion uint64, next *episode.CommerceEpisode, event *episode.EpisodeEvent) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if next == nil || event == nil || event.EpisodeID != episodeID || next.EpisodeID != episodeID {
		return errors.New("transition records do not refer to the same episode")
	}
	if err := next.Validate(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	if event.Sequence != expectedVersion {
		return ErrEventSequenceConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.events[episodeID] {
		if existing.Action.IdempotencyKey == event.Action.IdempotencyKey {
			return ErrIdempotentReplay
		}
	}
	previous := s.episodes[episodeID].Clone()
	if err := s.updateLocked(episodeID, expectedVersion, next); err != nil {
		return err
	}
	if err := s.appendLocked(event); err != nil {
		// Both maps are only changed under this lock. Restore the projection if
		// the append hits a uniqueness conflict after the optimistic update.
		s.episodes[episodeID] = previous
		return err
	}
	return nil
}

func (s *InMemoryStore) AppendLedgerEntry(ctx context.Context, value *ledger.LedgerEntry) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil {
		return ledger.ErrInvalidEntry
	}
	if err := value.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.episodes[value.EpisodeID]; !ok {
		return ErrNotFound
	}
	return s.appendLedgerLocked(value)
}

func (s *InMemoryStore) ListLedgerEntries(ctx context.Context, episodeID string) ([]*ledger.LedgerEntry, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := s.ledger[episodeID]
	result := make([]*ledger.LedgerEntry, 0, len(values))
	for _, value := range values {
		copy := *value
		result = append(result, &copy)
	}
	return result, nil
}

func (s *InMemoryStore) FindLedgerByIdempotencyKey(ctx context.Context, episodeID, key string) (*ledger.LedgerEntry, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, value := range s.ledger[episodeID] {
		if value.IdempotencyKey == key {
			copy := *value
			return &copy, nil
		}
	}
	return nil, ErrNotFound
}

func (s *InMemoryStore) CreatePaymentIntent(ctx context.Context, value *payment.PaymentIntent) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if value == nil {
		return payment.ErrInvalidIntent
	}
	if err := value.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.episodes[value.EpisodeID]; !ok {
		return ErrNotFound
	}
	if existingID, ok := s.intentByIdempotency[value.EpisodeID+"\x00"+value.IdempotencyKey]; ok && existingID != value.IntentID {
		return ErrPaymentIntentConflict
	}
	if existingID, ok := s.intentByEconomicKey[value.EpisodeID+"\x00"+value.EconomicKey]; ok && existingID != value.IntentID {
		return ErrPaymentIntentConflict
	}
	if _, ok := s.intents[value.IntentID]; ok {
		return ErrPaymentIntentConflict
	}
	copy := *value
	s.intents[value.IntentID] = &copy
	s.intentByIdempotency[value.EpisodeID+"\x00"+value.IdempotencyKey] = value.IntentID
	s.intentByEconomicKey[value.EpisodeID+"\x00"+value.EconomicKey] = value.IntentID
	return nil
}

func (s *InMemoryStore) GetPaymentIntent(ctx context.Context, intentID string) (*payment.PaymentIntent, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.intents[intentID]
	if !ok {
		return nil, ErrPaymentIntentNotFound
	}
	copy := *value
	return &copy, nil
}

func (s *InMemoryStore) FindPaymentIntentByIdempotencyKey(ctx context.Context, episodeID, key string) (*payment.PaymentIntent, error) {
	return s.findPaymentIntent(ctx, s.intentByIdempotency, episodeID+"\x00"+key)
}

func (s *InMemoryStore) FindPaymentIntentByEconomicKey(ctx context.Context, episodeID, key string) (*payment.PaymentIntent, error) {
	return s.findPaymentIntent(ctx, s.intentByEconomicKey, episodeID+"\x00"+key)
}

func (s *InMemoryStore) findPaymentIntent(ctx context.Context, index map[string]string, key string) (*payment.PaymentIntent, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	intentID, ok := index[key]
	if !ok {
		return nil, ErrPaymentIntentNotFound
	}
	copy := *s.intents[intentID]
	return &copy, nil
}

func (s *InMemoryStore) UpdatePaymentIntent(ctx context.Context, intentID string, expectedStatus payment.IntentStatus, next *payment.PaymentIntent) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if next == nil || next.IntentID != intentID {
		return payment.ErrInvalidIntent
	}
	if err := next.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.intents[intentID]
	if !ok {
		return ErrPaymentIntentNotFound
	}
	if expectedStatus != "" && current.Status != expectedStatus {
		return ErrPaymentIntentStateConflict
	}
	if !samePaymentIntentIdentity(current, next) {
		return payment.ErrIntentConflict
	}
	copy := *next
	s.intents[intentID] = &copy
	return nil
}

func (s *InMemoryStore) CommitFinanceTransition(ctx context.Context, transition FinanceTransition) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if transition.NextEpisode == nil || transition.Event == nil || transition.NextEpisode.EpisodeID != transition.EpisodeID || transition.Event.EpisodeID != transition.EpisodeID {
		return errors.New("finance transition records do not refer to the same episode")
	}
	if err := transition.NextEpisode.Validate(); err != nil {
		return err
	}
	if err := transition.Event.Validate(); err != nil {
		return err
	}
	if transition.ExpectedEpisodeVersion == 0 || transition.Event.Sequence != transition.ExpectedEpisodeVersion || transition.NextEpisode.Version != transition.ExpectedEpisodeVersion+1 {
		return ErrVersionConflict
	}
	for _, entry := range transition.LedgerEntries {
		if entry == nil {
			return ledger.ErrInvalidEntry
		}
		if entry.EpisodeID != transition.EpisodeID {
			return ledger.ErrInvalidEntry
		}
		if err := entry.Validate(); err != nil {
			return err
		}
	}
	if transition.IntentCreate != nil && transition.IntentUpdate != nil {
		return errors.New("finance transition cannot create and update an intent together")
	}
	if transition.IntentCreate != nil && transition.IntentCreate.EpisodeID != transition.EpisodeID {
		return payment.ErrIntentConflict
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.episodes[transition.EpisodeID]
	if !ok {
		return ErrNotFound
	}
	if current.Version != transition.ExpectedEpisodeVersion {
		return ErrVersionConflict
	}
	if current.State != transition.Event.StateBefore || transition.NextEpisode.State != transition.Event.StateAfter {
		return errors.New("finance event does not match episode projection")
	}
	for _, existing := range s.events[transition.EpisodeID] {
		if existing.Action.IdempotencyKey == transition.Event.Action.IdempotencyKey {
			return ErrIdempotentReplay
		}
	}
	if err := s.validateLedgerBatchLocked(transition.EpisodeID, transition.LedgerEntries); err != nil {
		return err
	}
	if err := s.validateBudgetProjectionLocked(current, transition.NextEpisode, transition.LedgerEntries); err != nil {
		return err
	}
	if transition.IntentCreate != nil {
		if err := s.validateIntentCreateLocked(transition.IntentCreate); err != nil {
			return err
		}
	}
	if transition.IntentUpdate != nil {
		currentIntent, ok := s.intents[transition.IntentUpdate.IntentID]
		if !ok {
			return ErrPaymentIntentNotFound
		}
		if transition.ExpectedIntentStatus != "" && currentIntent.Status != transition.ExpectedIntentStatus {
			return ErrPaymentIntentStateConflict
		}
		if !samePaymentIntentIdentity(currentIntent, transition.IntentUpdate) {
			return payment.ErrIntentConflict
		}
	}
	previous := current.Clone()
	s.episodes[transition.EpisodeID] = transition.NextEpisode.Clone()
	if err := s.appendLocked(transition.Event); err != nil {
		s.episodes[transition.EpisodeID] = previous
		return err
	}
	for _, entry := range transition.LedgerEntries {
		copy := *entry
		s.ledger[transition.EpisodeID] = append(s.ledger[transition.EpisodeID], &copy)
	}
	if transition.IntentCreate != nil {
		copy := *transition.IntentCreate
		s.intents[copy.IntentID] = &copy
		s.intentByIdempotency[copy.EpisodeID+"\x00"+copy.IdempotencyKey] = copy.IntentID
		s.intentByEconomicKey[copy.EpisodeID+"\x00"+copy.EconomicKey] = copy.IntentID
	}
	if transition.IntentUpdate != nil {
		copy := *transition.IntentUpdate
		s.intents[copy.IntentID] = &copy
	}
	return nil
}

func (s *InMemoryStore) appendLedgerLocked(value *ledger.LedgerEntry) error {
	for _, existing := range s.ledger[value.EpisodeID] {
		if existing.EntryID == value.EntryID {
			if *existing == *value {
				return ErrLedgerIdempotentReplay
			}
			return ErrLedgerIdempotencyConflict
		}
		if existing.IdempotencyKey == value.IdempotencyKey {
			if *existing == *value {
				return ErrLedgerIdempotentReplay
			}
			return ErrLedgerIdempotencyConflict
		}
	}
	if value.Sequence != uint64(len(s.ledger[value.EpisodeID])+1) {
		return ErrEventSequenceConflict
	}
	copy := *value
	s.ledger[value.EpisodeID] = append(s.ledger[value.EpisodeID], &copy)
	return nil
}

func (s *InMemoryStore) validateLedgerBatchLocked(episodeID string, entries []*ledger.LedgerEntry) error {
	for index, entry := range entries {
		if entry.Sequence != uint64(len(s.ledger[episodeID])+index+1) {
			return ErrEventSequenceConflict
		}
		for _, existing := range s.ledger[episodeID] {
			if existing.EntryID == entry.EntryID || existing.IdempotencyKey == entry.IdempotencyKey {
				return ErrLedgerIdempotencyConflict
			}
		}
		for previous := 0; previous < index; previous++ {
			if entries[previous].EntryID == entry.EntryID || entries[previous].IdempotencyKey == entry.IdempotencyKey {
				return ErrLedgerIdempotencyConflict
			}
		}
	}
	return nil
}

func (s *InMemoryStore) validateBudgetProjectionLocked(current, next *episode.CommerceEpisode, entries []*ledger.LedgerEntry) error {
	existing := s.ledger[current.EpisodeID]
	all := make([]*ledger.LedgerEntry, 0, len(existing)+len(entries))
	for _, entry := range existing {
		copy := *entry
		all = append(all, &copy)
	}
	all = append(all, entries...)
	projection, err := ledger.BuildProjection(current.Budget.Currency, current.Budget.BudgetLimitMinor, current.Budget.RefundReusable, all)
	if err != nil {
		return err
	}
	if projection.ToEpisodeBudget() != next.Budget {
		return ledger.ErrProjectionInvariant
	}
	return nil
}

func (s *InMemoryStore) validateIntentCreateLocked(value *payment.PaymentIntent) error {
	if _, ok := s.intents[value.IntentID]; ok {
		return ErrPaymentIntentConflict
	}
	if existingID, ok := s.intentByIdempotency[value.EpisodeID+"\x00"+value.IdempotencyKey]; ok && existingID != value.IntentID {
		return ErrPaymentIntentConflict
	}
	if existingID, ok := s.intentByEconomicKey[value.EpisodeID+"\x00"+value.EconomicKey]; ok && existingID != value.IntentID {
		return ErrPaymentIntentConflict
	}
	return nil
}

func samePaymentIntentIdentity(left, right *payment.PaymentIntent) bool {
	if left == nil || right == nil {
		return false
	}
	return left.IntentID == right.IntentID && left.EpisodeID == right.EpisodeID && left.MerchantDID == right.MerchantDID &&
		left.CapabilityID == right.CapabilityID && left.PayeeDID == right.PayeeDID && left.QuoteHash == right.QuoteHash && left.AmountMinor == right.AmountMinor &&
		left.Currency == right.Currency && left.RequesterDID == right.RequesterDID && left.BudgetReservation == right.BudgetReservation &&
		left.IdempotencyKey == right.IdempotencyKey && left.EconomicKey == right.EconomicKey && left.CredentialRef == right.CredentialRef && left.ExpiresAt.Equal(right.ExpiresAt) &&
		left.CreatedAt.Equal(right.CreatedAt) && left.RequestFingerprint == right.RequestFingerprint
}

func (s *InMemoryStore) updateLocked(episodeID string, expectedVersion uint64, next *episode.CommerceEpisode) error {
	current, ok := s.episodes[episodeID]
	if !ok {
		return ErrNotFound
	}
	if current.Version != expectedVersion {
		return ErrVersionConflict
	}
	if current.ContractSnapshotHash != next.ContractSnapshotHash || !bytes.Equal(current.ContractSnapshot, next.ContractSnapshot) {
		return episode.ErrImmutableContract
	}
	if next.Version != expectedVersion+1 {
		return ErrVersionConflict
	}
	s.episodes[episodeID] = next.Clone()
	return nil
}

func (s *InMemoryStore) appendLocked(value *episode.EpisodeEvent) error {
	if value.Sequence != uint64(len(s.events[value.EpisodeID])+1) {
		return ErrEventSequenceConflict
	}
	for _, existing := range s.events[value.EpisodeID] {
		if existing.Sequence == value.Sequence {
			return ErrEventSequenceConflict
		}
		if existing.Action.IdempotencyKey == value.Action.IdempotencyKey {
			return ErrIdempotentReplay
		}
	}
	s.events[value.EpisodeID] = append(s.events[value.EpisodeID], value.Clone())
	return nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
