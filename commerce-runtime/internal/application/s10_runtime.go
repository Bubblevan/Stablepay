package application

import (
	"context"
	"errors"
	"strings"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/repository"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

// ExpireEpisode is the runtime-owned deterministic deadline transition. It is
// intentionally separate from operations status: the CommerceEpisode remains
// the authority for business terminal state.
func (s *Service) ExpireEpisode(ctx context.Context, episodeID, traceID string) (CommitResult, error) {
	if s == nil || s.store == nil {
		return CommitResult{}, repository.ErrRepositoryUnavailable
	}
	current, err := s.store.Get(ctx, episodeID)
	if err != nil {
		return CommitResult{}, err
	}
	if episode.IsTerminal(current.State) {
		return CommitResult{Episode: current}, nil
	}
	now := s.clock().UTC()
	if now.Before(current.DeadlineAt) {
		return CommitResult{}, episode.ErrEpisodeExpired
	}
	key := "runtime:expire:" + current.EpisodeID
	if existing, findErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, key); findErr == nil {
		latest, getErr := s.store.Get(ctx, current.EpisodeID)
		if getErr != nil {
			return CommitResult{}, getErr
		}
		return CommitResult{Episode: latest, Event: existing, Replayed: true}, nil
	} else if !errors.Is(findErr, repository.ErrNotFound) {
		return CommitResult{}, findErr
	}
	next := current.Clone()
	next.ActionCount++
	if err := next.ApplyCommittedState(episode.StateExpired, now, "EPISODE_DEADLINE_EXCEEDED"); err != nil {
		return CommitResult{}, err
	}
	if strings.TrimSpace(traceID) == "" {
		traceID = current.EpisodeID + ":deadline"
	}
	event, err := episode.NewEvent(s.idGenerator("evt"), current.EpisodeID, current.Version, now, current.State,
		trace.Action{Type: trace.ActionStop, IdempotencyKey: key},
		trace.Observation{Type: trace.ObservationDeadlineExceeded, Code: "EPISODE_DEADLINE_EXCEEDED"},
		trace.Decision{ProposedAction: trace.ActionStop, ProposalID: "runtime:deadline:" + current.EpisodeID, Reason: "episode deadline exceeded"},
		trace.RuntimeVerdict{Allowed: true, Checks: []trace.RuntimeCheck{trace.Check("episode_deadline", true, "runtime expiry transition")}},
		next.State, "runtime", traceID, s.runtimeVersion)
	if err != nil {
		return CommitResult{}, err
	}
	if err := s.store.CommitTransition(ctx, current.EpisodeID, current.Version, next, event); err != nil {
		if replay, findErr := s.store.FindByIdempotencyKey(ctx, current.EpisodeID, key); findErr == nil {
			latest, getErr := s.store.Get(ctx, current.EpisodeID)
			if getErr != nil {
				return CommitResult{}, getErr
			}
			return CommitResult{Episode: latest, Event: replay, Replayed: true}, nil
		}
		return CommitResult{}, err
	}
	result := CommitResult{Episode: next, Event: event}
	if err := s.projectTerminalMemory(ctx, next); err != nil {
		return result, err
	}
	return result, nil
}
