package repository

import (
	"context"
	"errors"
	"strings"
	"time"
)

// EpisodeExecutionStatus is an operations-plane projection. It records how
// the runner is doing without becoming an authority over CommerceEpisode,
// payment, budget, entitlement, or delivery state.
type EpisodeExecutionStatus struct {
	EpisodeID        string     `json:"episode_id"`
	Status           string     `json:"status"`
	AttemptCount     int        `json:"attempt_count"`
	LastStartedAt    time.Time  `json:"last_started_at"`
	LastFinishedAt   *time.Time `json:"last_finished_at,omitempty"`
	LastErrorCode    string     `json:"last_error_code,omitempty"`
	LastErrorMessage string     `json:"last_error_message,omitempty"`
	LastErrorAt      *time.Time `json:"last_error_at,omitempty"`
	NextRetryAt      *time.Time `json:"next_retry_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

const (
	ExecutionIdle      = "IDLE"
	ExecutionRunning   = "RUNNING"
	ExecutionRetryWait = "RETRY_WAIT"
	ExecutionPaused    = "PAUSED"
	ExecutionCompleted = "COMPLETED"
	ExecutionError     = "ERROR"
)

var ErrInvalidExecutionStatus = errors.New("invalid episode execution status")

func (s EpisodeExecutionStatus) Clone() *EpisodeExecutionStatus {
	copy := s
	if s.LastFinishedAt != nil {
		value := *s.LastFinishedAt
		copy.LastFinishedAt = &value
	}
	if s.LastErrorAt != nil {
		value := *s.LastErrorAt
		copy.LastErrorAt = &value
	}
	if s.NextRetryAt != nil {
		value := *s.NextRetryAt
		copy.NextRetryAt = &value
	}
	return &copy
}

func (s EpisodeExecutionStatus) Validate() error {
	if strings.TrimSpace(s.EpisodeID) == "" || !KnownExecutionStatus(s.Status) || s.AttemptCount < 0 || s.UpdatedAt.IsZero() {
		return ErrInvalidExecutionStatus
	}
	if len(s.LastErrorCode) > 128 || len(s.LastErrorMessage) > 512 {
		return ErrInvalidExecutionStatus
	}
	return nil
}

func KnownExecutionStatus(value string) bool {
	switch value {
	case ExecutionIdle, ExecutionRunning, ExecutionRetryWait, ExecutionPaused, ExecutionCompleted, ExecutionError:
		return true
	default:
		return false
	}
}

type ExecutionStatusRepository interface {
	GetEpisodeExecutionStatus(context.Context, string) (*EpisodeExecutionStatus, error)
	UpsertEpisodeExecutionStatus(context.Context, *EpisodeExecutionStatus) error
}
