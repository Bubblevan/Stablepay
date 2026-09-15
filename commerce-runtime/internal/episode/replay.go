package episode

import (
	"fmt"

	"github.com/stablepay/commerce-runtime/internal/trace"
)

// Reconstruct deterministically applies ordered events to an initial
// projection. It is intentionally small: the persisted episode remains the
// authoritative projection while this function provides a replay check.
func Reconstruct(initial *CommerceEpisode, events []*EpisodeEvent) (*CommerceEpisode, error) {
	if initial == nil {
		return nil, ErrInvalidEpisode
	}
	result := initial.Clone()
	for index, event := range events {
		if event == nil || event.EpisodeID != result.EpisodeID || event.Sequence != uint64(index+1) {
			return nil, fmt.Errorf("invalid replay event at index %d", index)
		}
		if event.StateBefore != result.State {
			return nil, fmt.Errorf("replay state mismatch at sequence %d", event.Sequence)
		}
		if err := event.Validate(); err != nil {
			return nil, err
		}
		reason := event.Observation.Code
		if reason == "" && IsTerminal(event.StateAfter) {
			reason = string(event.Observation.Type)
		}
		if err := result.ApplyCommittedState(event.StateAfter, event.OccurredAt, reason); err != nil {
			return nil, err
		}
		result.ActionCount++
		switch event.Action.Type {
		case trace.ActionCreatePayment:
			result.PaymentAttemptCount++
		case trace.ActionRetrySameMerchant:
			result.RetryCount++
		case trace.ActionInvoke:
			if event.StateBefore == StateInvokingDelivery {
				result.DeliveryAttemptCount++
			}
		}
	}
	return result, nil
}
