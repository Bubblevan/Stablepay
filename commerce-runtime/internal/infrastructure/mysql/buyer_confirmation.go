package mysql

import (
	"context"
	"errors"

	"github.com/stablepay/commerce-runtime/internal/buyer"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Store) GetBuyerConfirmationByQuoteHash(ctx context.Context, episodeID, quoteHash string) (*buyer.Confirmation, error) {
	var row BuyerConfirmationModel
	err := s.db.WithContext(ctx).Where("episode_id = ? AND quote_hash = ?", episodeID, quoteHash).
		Order("created_at DESC, offer_ref DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToBuyerConfirmation(row)
}

func (s *Store) FindBuyerConfirmationByOfferRef(ctx context.Context, episodeID, offerRef string) (*buyer.Confirmation, error) {
	var row BuyerConfirmationModel
	err := s.db.WithContext(ctx).Where("episode_id = ? AND offer_ref = ?", episodeID, offerRef).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToBuyerConfirmation(row)
}

func (s *Store) FindBuyerConfirmationByIdempotencyKey(ctx context.Context, episodeID, key string) (*buyer.Confirmation, error) {
	var row BuyerConfirmationModel
	err := s.db.WithContext(ctx).Where("episode_id = ? AND idempotency_key = ?", episodeID, key).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelToBuyerConfirmation(row)
}

func (s *Store) CommitBuyerConfirmationTransition(ctx context.Context, t repository.BuyerConfirmationTransition) error {
	if t.NextEpisode == nil || t.Event == nil || t.Confirmation == nil || t.EpisodeID == "" ||
		t.NextEpisode.EpisodeID != t.EpisodeID || t.Event.EpisodeID != t.EpisodeID || t.Confirmation.EpisodeID != t.EpisodeID {
		return errors.New("buyer confirmation transition records do not refer to the same episode")
	}
	if err := t.NextEpisode.Validate(); err != nil {
		return err
	}
	if err := t.Event.Validate(); err != nil {
		return err
	}
	if err := t.Confirmation.Validate(); err != nil {
		return err
	}
	if t.ExpectedEpisodeVersion == 0 || t.Event.Sequence != t.ExpectedEpisodeVersion || t.NextEpisode.Version != t.ExpectedEpisodeVersion+1 {
		return repository.ErrVersionConflict
	}
	if t.Event.StateAfter != t.NextEpisode.State ||
		t.NextEpisode.CurrentPaymentRequirementID != t.Confirmation.OfferRef || t.NextEpisode.CurrentQuoteHash != t.Confirmation.QuoteHash ||
		(t.Confirmation.Accepted && t.NextEpisode.State != episode.StateNegotiating) ||
		(!t.Confirmation.Accepted && t.NextEpisode.State != episode.StateAborted) {
		return errors.New("buyer confirmation outcome does not match episode transition")
	}
	updates, err := episodeUpdates(t.NextEpisode)
	if err != nil {
		return err
	}
	eventModel, err := eventToModel(t.Event)
	if err != nil {
		return err
	}
	confirmationModel, err := buyerConfirmationToModel(t.Confirmation)
	if err != nil {
		return err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var byKey BuyerConfirmationModel
		lookup := tx.Where("episode_id = ? AND idempotency_key = ?", t.EpisodeID, t.Confirmation.IdempotencyKey).First(&byKey).Error
		if lookup == nil {
			previous, decodeErr := modelToBuyerConfirmation(byKey)
			if decodeErr == nil && buyer.SameIdempotentRequest(previous, t.Confirmation) {
				return repository.ErrIdempotentReplay
			}
			return repository.ErrBuyerConfirmationConflict
		}
		if !errors.Is(lookup, gorm.ErrRecordNotFound) {
			return lookup
		}
		var byQuote BuyerConfirmationModel
		lookup = tx.Where("episode_id = ? AND offer_ref = ?", t.EpisodeID, t.Confirmation.OfferRef).First(&byQuote).Error
		if lookup == nil {
			previous, decodeErr := modelToBuyerConfirmation(byQuote)
			if decodeErr == nil && buyer.SameIdempotentRequest(previous, t.Confirmation) {
				return repository.ErrIdempotentReplay
			}
			return repository.ErrBuyerConfirmationConflict
		}
		if !errors.Is(lookup, gorm.ErrRecordNotFound) {
			return lookup
		}
		var existingEvent EventModel
		if lookup := tx.Where("episode_id = ? AND idempotency_key = ?", t.EpisodeID, t.Event.Action.IdempotencyKey).First(&existingEvent).Error; lookup == nil {
			return repository.ErrIdempotentReplay
		} else if !errors.Is(lookup, gorm.ErrRecordNotFound) {
			return lookup
		}
		var current EpisodeModel
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("episode_id = ?", t.EpisodeID).First(&current).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return repository.ErrNotFound
			}
			return err
		}
		if current.Version != t.ExpectedEpisodeVersion || episode.State(current.State) != episode.StateAwaitingBuyer ||
			current.RequestID != t.Confirmation.RequestID || current.RequesterDID != t.Confirmation.RequesterDID ||
			current.SelectedMerchantDID != t.Confirmation.MerchantDID || current.SelectedCapabilityID != t.Confirmation.CapabilityID ||
			current.CurrentQuoteHash != t.Confirmation.QuoteHash || current.CurrentPaymentRequirementID != t.Confirmation.OfferRef || episode.State(current.State) != t.Event.StateBefore {
			return repository.ErrVersionConflict
		}
		if err := tx.Model(&EpisodeModel{}).Where("episode_id = ? AND version = ?", t.EpisodeID, t.ExpectedEpisodeVersion).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Create(confirmationModel).Error; err != nil {
			return err
		}
		return tx.Create(eventModel).Error
	})
	if isDuplicateKey(err) {
		if previous, lookupErr := s.FindBuyerConfirmationByIdempotencyKey(ctx, t.EpisodeID, t.Confirmation.IdempotencyKey); lookupErr == nil && buyer.SameIdempotentRequest(previous, t.Confirmation) {
			return repository.ErrIdempotentReplay
		}
		return repository.ErrBuyerConfirmationConflict
	}
	return err
}
