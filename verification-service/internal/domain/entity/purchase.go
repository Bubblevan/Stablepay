package entity

import "time"

type PurchaseRecord struct {
	AgentDID    string
	SkillDID    string
	TxID        string
	AmountMinor int64
	Currency    int32
	TxHash      string
	CreatedAt   time.Time
}
