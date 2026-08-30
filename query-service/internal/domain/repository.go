package domain

import (
	"context"

	"github.com/stablepay/query-service/internal/domain/entity"
)

type TransactionRepository interface {
	SumPurchaseByAgent(ctx context.Context, agentDID string) (int64, error)
	List(ctx context.Context, query entity.TransactionQuery) ([]entity.Transaction, int64, error)
	RevenueSummary(ctx context.Context, skillDID string) (entity.RevenueSummary, error)
	ListSales(ctx context.Context, skillDID string, limit, offset int) ([]entity.Sale, int64, error)
	Upsert(ctx context.Context, transaction entity.Transaction) error
}

type BalanceProvider interface {
	USDCBalanceMinor(ctx context.Context, agentDID string) (int64, error)
}
