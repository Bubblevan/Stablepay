package entity

import "time"

type PurchaseRecord struct {
	EventID     string
	AgentDID    string
	SkillDID    string
	TxID        string
	AmountMinor int64
	Currency    int32
	TxHash      string
	CreatedAt   time.Time
}
