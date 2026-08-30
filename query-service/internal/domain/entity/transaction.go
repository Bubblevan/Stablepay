package entity

import "time"

type TransactionType int32

const (
	TransactionPurchase TransactionType = 1
	TransactionRevenue  TransactionType = 2
)

type Transaction struct {
	TxID        string
	SourceTxID  string
	AgentDID    string
	SkillDID    string
	AmountMinor int64
	Currency    int32
	Type        TransactionType
	CreatedAt   time.Time
}

type TransactionQuery struct {
	DID    string
	Type   TransactionType
	Limit  int
	Offset int
}

type Sale struct {
	Transaction
}

type RevenueSummary struct {
	TotalRevenueMinor int64
	TotalSales        int64
	Trend             []RevenueTrend
}

type RevenueTrend struct {
	Date        string
	AmountMinor int64
}
