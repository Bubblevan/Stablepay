// Package repository defines persistence ports for the runtime aggregates.
package repository

import (
	"context"
	"errors"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/ledger"
	"github.com/stablepay/commerce-runtime/internal/payment"
)

var (
	ErrNotFound                   = errors.New("commerce runtime record not found")
	ErrVersionConflict            = errors.New("episode version conflict")
	ErrRequestIDConflict          = errors.New("request_id already belongs to another contract")
	ErrIdempotencyConflict        = errors.New("idempotency key conflicts with an existing transition")
	ErrEventSequenceConflict      = errors.New("episode event sequence conflict")
	ErrIdempotentReplay           = errors.New("transition was already committed")
	ErrRepositoryUnavailable      = errors.New("repository does not support atomic transition commit")
	ErrLedgerIdempotentReplay     = errors.New("ledger entry was already committed")
	ErrLedgerIdempotencyConflict  = errors.New("ledger idempotency key conflicts with an existing entry")
	ErrPaymentIntentConflict      = errors.New("payment intent conflicts with an existing identity")
	ErrPaymentIntentNotFound      = errors.New("payment intent not found")
	ErrPaymentIntentStateConflict = errors.New("payment intent state changed concurrently")
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

type LedgerRepository interface {
	AppendLedgerEntry(ctx context.Context, value *ledger.LedgerEntry) error
	ListLedgerEntries(ctx context.Context, episodeID string) ([]*ledger.LedgerEntry, error)
	FindLedgerByIdempotencyKey(ctx context.Context, episodeID, key string) (*ledger.LedgerEntry, error)
}

type PaymentIntentRepository interface {
	CreatePaymentIntent(ctx context.Context, value *payment.PaymentIntent) error
	GetPaymentIntent(ctx context.Context, intentID string) (*payment.PaymentIntent, error)
	FindPaymentIntentByIdempotencyKey(ctx context.Context, episodeID, key string) (*payment.PaymentIntent, error)
	FindPaymentIntentByEconomicKey(ctx context.Context, episodeID, key string) (*payment.PaymentIntent, error)
	UpdatePaymentIntent(ctx context.Context, intentID string, expectedStatus payment.IntentStatus, next *payment.PaymentIntent) error
}

// FinanceTransition is the local atomic boundary for a projection/event plus
// ledger facts and an intent mutation. It does not pretend to include the
// external payment-service transaction; the intent and reconciliation flow
// provide the cross-service recovery boundary.
type FinanceTransition struct {
	EpisodeID              string
	ExpectedEpisodeVersion uint64
	NextEpisode            *episode.CommerceEpisode
	Event                  *episode.EpisodeEvent
	LedgerEntries          []*ledger.LedgerEntry
	IntentCreate           *payment.PaymentIntent
	IntentUpdate           *payment.PaymentIntent
	ExpectedIntentStatus   payment.IntentStatus
}

type S2Store interface {
	TransitionStore
	LedgerRepository
	PaymentIntentRepository
	CommitFinanceTransition(ctx context.Context, transition FinanceTransition) error
}
