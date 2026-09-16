package payment

import "errors"

type OutcomeStatus string

const (
	OutcomePending   OutcomeStatus = "PENDING"
	OutcomeConfirmed OutcomeStatus = "CONFIRMED"
	OutcomeFailed    OutcomeStatus = "FAILED"
	OutcomeUnknown   OutcomeStatus = "UNKNOWN"
)

var ErrInvalidOutcome = errors.New("invalid payment outcome")

type PaymentOutcome struct {
	Status      OutcomeStatus `json:"status"`
	IntentID    string        `json:"intent_id,omitempty"`
	TxID        string        `json:"tx_id,omitempty"`
	TxHash      string        `json:"tx_hash,omitempty"`
	AmountMinor int64         `json:"amount_minor,omitempty"`
	Currency    string        `json:"currency,omitempty"`
	Reason      string        `json:"reason,omitempty"`
	OccurredAt  string        `json:"occurred_at,omitempty"`
}

func (o PaymentOutcome) Validate() error {
	switch o.Status {
	case OutcomePending, OutcomeConfirmed, OutcomeFailed, OutcomeUnknown:
	default:
		return ErrInvalidOutcome
	}
	if o.AmountMinor < 0 {
		return ErrInvalidOutcome
	}
	return nil
}
