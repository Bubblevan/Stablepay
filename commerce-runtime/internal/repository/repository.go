// Package repository defines persistence ports for the runtime aggregates.
package repository

import (
	"context"
	"errors"

	"github.com/stablepay/commerce-runtime/internal/episode"
)

var (
	ErrNotFound              = errors.New("commerce runtime record not found")
	ErrVersionConflict       = errors.New("episode version conflict")
	ErrRequestIDConflict     = errors.New("request_id already belongs to another contract")
	ErrIdempotencyConflict   = errors.New("idempotency key conflicts with an existing transition")
	ErrEventSequenceConflict = errors.New("episode event sequence conflict")
	ErrIdempotentReplay      = errors.New("transition was already committed")
	ErrRepositoryUnavailable = errors.New("repository does not support atomic transition commit")
)

type EpisodeRepository interface {
	Create(ctx context.Context, value *episode.CommerceEpisode) error
	Get(ctx context.Context, episodeID string) (*episode.CommerceEpisode, error)
	FindByRequestID(ctx context.Context, requestID string) (*episode.CommerceEpisode, error)
	UpdateOptimistic(ctx context.Context, episodeID string, expectedVersion uint64, next *episode.CommerceEpisode) error
}

type EpisodeEventRepository interface {
	Append(ctx context.Context, value *episode.EpisodeEvent) error
	ListByEpisode(ctx context.Context, episodeID string) ([]*episode.EpisodeEvent, error)
	FindByIdempotencyKey(ctx context.Context, episodeID, key string) (*episode.EpisodeEvent, error)
}

// TransitionStore is the stronger port used by the application service. Its
// CommitTransition method must update the projection and append the event in one
// local database transaction.
type TransitionStore interface {
	EpisodeRepository
	EpisodeEventRepository
	CommitTransition(ctx context.Context, episodeID string, expectedVersion uint64, next *episode.CommerceEpisode, event *episode.EpisodeEvent) error
}
