package episode

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/internal/trace"
)

var (
	ErrInvalidEvent  = errors.New("invalid episode event")
	ErrEventSequence = errors.New("episode event sequence must be positive")
)

// EpisodeEvent is append-only. Its fields describe one complete structured
// trace step; historical semantic fields are not exposed through an update API.
type EpisodeEvent struct {
	EventID        string               `json:"event_id"`
	EpisodeID      string               `json:"episode_id"`
	Sequence       uint64               `json:"sequence"`
	OccurredAt     time.Time            `json:"occurred_at"`
	StateBefore    State                `json:"state_before"`
	Action         trace.Action         `json:"action"`
	Observation    trace.Observation    `json:"observation"`
	Decision       trace.Decision       `json:"decision"`
	RuntimeVerdict trace.RuntimeVerdict `json:"runtime_verdict"`
	StateAfter     State                `json:"state_after"`
	Actor          string               `json:"actor"`
	TraceID        string               `json:"trace_id"`
	RuntimeVersion string               `json:"runtime_version"`
}

func NewEvent(eventID, episodeID string, sequence uint64, occurredAt time.Time, before State, action trace.Action, observation trace.Observation, decision trace.Decision, verdict trace.RuntimeVerdict, after State, actor, traceID, runtimeVersion string) (*EpisodeEvent, error) {
	event := &EpisodeEvent{
		EventID: eventID, EpisodeID: episodeID, Sequence: sequence, OccurredAt: occurredAt.UTC(),
		StateBefore: before, Action: action, Observation: observation, Decision: decision,
		RuntimeVerdict: verdict, StateAfter: after, Actor: actor, TraceID: traceID,
		RuntimeVersion: runtimeVersion,
	}
	if err := event.Validate(); err != nil {
		return nil, err
	}
	return event, nil
}

func (e *EpisodeEvent) Validate() error {
	if e == nil || strings.TrimSpace(e.EventID) == "" || strings.TrimSpace(e.EpisodeID) == "" {
		return ErrInvalidEvent
	}
	if e.Sequence == 0 {
		return ErrEventSequence
	}
	if e.OccurredAt.IsZero() || !KnownState(e.StateBefore) || !KnownState(e.StateAfter) {
		return ErrInvalidEvent
	}
	if err := e.Action.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEvent, err)
	}
	if err := e.Observation.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEvent, err)
	}
	if err := e.RuntimeVerdict.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEvent, err)
	}
	if !e.RuntimeVerdict.Allowed {
		return fmt.Errorf("%w: committed event must have an allowed runtime verdict", ErrInvalidEvent)
	}
	if e.StateBefore == e.StateAfter {
		if IsTerminal(e.StateBefore) {
			return fmt.Errorf("%w: terminal state cannot accept same-state events", ErrInvalidEvent)
		}
	} else if !CanTransition(e.StateBefore, e.StateAfter) {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidEvent, e.StateBefore, e.StateAfter)
	}
	if strings.TrimSpace(e.Decision.ProposalID) == "" || e.Decision.ProposedAction != e.Action.Type {
		return fmt.Errorf("%w: decision does not describe action", ErrInvalidEvent)
	}
	return nil
}

func (e *EpisodeEvent) Clone() *EpisodeEvent {
	if e == nil {
		return nil
	}
	copy := *e
	copy.RuntimeVerdict.Checks = append([]trace.RuntimeCheck(nil), e.RuntimeVerdict.Checks...)
	copy.Decision.EvidenceRefs = append([]string(nil), e.Decision.EvidenceRefs...)
	copy.Decision.MemoryRefs = append([]string(nil), e.Decision.MemoryRefs...)
	if e.Decision.Target != nil {
		target := *e.Decision.Target
		copy.Decision.Target = &target
	}
	return &copy
}
