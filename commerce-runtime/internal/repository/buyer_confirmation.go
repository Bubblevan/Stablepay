package repository

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/buyer"
	"github.com/stablepay/commerce-runtime/internal/episode"
)

func buyerConfirmationKey(episodeID, value string) string {
	return strings.TrimSpace(episodeID) + "\x00" + strings.TrimSpace(value)
}

func (s *InMemoryStore) GetBuyerConfirmationByQuoteHash(ctx context.Context, episodeID, quoteHash string) (*buyer.Confirmation, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.buyerConfirmationByQuote[buyerConfirmationKey(episodeID, quoteHash)]
	if !ok {
		return nil, ErrNotFound
	}
	return s.buyerConfirmations[id].Clone(), nil
}

func (s *InMemoryStore) FindBuyerConfirmationByIdempotencyKey(ctx context.Context, episodeID, key string) (*buyer.Confirmation, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.buyerConfirmationByKey[buyerConfirmationKey(episodeID, key)]
	if !ok {
		return nil, ErrNotFound
	}
	return s.buyerConfirmations[id].Clone(), nil
}

func (s *InMemoryStore) CommitBuyerConfirmationTransition(ctx context.Context, transition BuyerConfirmationTransition) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if transition.NextEpisode == nil || transition.Event == nil || transition.Confirmation == nil ||
		transition.EpisodeID == "" || transition.NextEpisode.EpisodeID != transition.EpisodeID ||
		transition.Event.EpisodeID != transition.EpisodeID || transition.Confirmation.EpisodeID != transition.EpisodeID {
		return errors.New("buyer confirmation transition records do not refer to the same episode")
	}
	if err := transition.NextEpisode.Validate(); err != nil {
		return err
	}
	if err := transition.Event.Validate(); err != nil {
		return err
	}
	if err := transition.Confirmation.Validate(); err != nil {
		return err
	}
	if transition.Event.Sequence != transition.ExpectedEpisodeVersion || transition.NextEpisode.Version != transition.ExpectedEpisodeVersion+1 {
		return ErrVersionConflict
	}
	if transition.Event.StateAfter != transition.NextEpisode.State ||
		(transition.Confirmation.Accepted && transition.NextEpisode.State != episode.StateNegotiating) ||
		(!transition.Confirmation.Accepted && transition.NextEpisode.State != episode.StateAborted) {
		return errors.New("buyer confirmation outcome does not match episode transition")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	byKey := buyerConfirmationKey(transition.EpisodeID, transition.Confirmation.IdempotencyKey)
	if existingID, ok := s.buyerConfirmationByKey[byKey]; ok {
		if reflect.DeepEqual(s.buyerConfirmations[existingID], transition.Confirmation) {
			return ErrIdempotentReplay
		}
		return ErrBuyerConfirmationConflict
	}
	byQuote := buyerConfirmationKey(transition.EpisodeID, transition.Confirmation.QuoteHash)
	if existingID, ok := s.buyerConfirmationByQuote[byQuote]; ok {
		if reflect.DeepEqual(s.buyerConfirmations[existingID], transition.Confirmation) {
			return ErrIdempotentReplay
		}
		return ErrBuyerConfirmationConflict
	}
	current := s.episodes[transition.EpisodeID]
	if current == nil {
		return ErrNotFound
	}
	if current.Version != transition.ExpectedEpisodeVersion || current.State != episode.StateAwaitingBuyer ||
		current.RequestID != transition.Confirmation.RequestID || current.RequesterDID != transition.Confirmation.RequesterDID ||
		current.SelectedMerchantDID != transition.Confirmation.MerchantDID || current.SelectedCapabilityID != transition.Confirmation.CapabilityID ||
		current.CurrentQuoteHash != transition.Confirmation.QuoteHash {
		return ErrVersionConflict
	}
	for _, existing := range s.events[transition.EpisodeID] {
		if existing.Action.IdempotencyKey == transition.Event.Action.IdempotencyKey {
			return ErrIdempotentReplay
		}
	}
	if transition.Event.StateBefore != current.State {
		return ErrVersionConflict
	}
	if err := s.updateLocked(transition.EpisodeID, transition.ExpectedEpisodeVersion, transition.NextEpisode); err != nil {
		return err
	}
	if err := s.appendLocked(transition.Event); err != nil {
		s.episodes[transition.EpisodeID] = current
		return err
	}
	s.buyerConfirmations[transition.Confirmation.ConfirmationID] = transition.Confirmation.Clone()
	s.buyerConfirmationByKey[byKey] = transition.Confirmation.ConfirmationID
	s.buyerConfirmationByQuote[byQuote] = transition.Confirmation.ConfirmationID
	return nil
}
