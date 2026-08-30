package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stablepay/query-service/internal/domain"
	"github.com/stablepay/query-service/internal/domain/entity"
)

type Config struct {
	MonthlyLimitMinor int64
}

type QueryService struct {
	repository   domain.TransactionRepository
	balance      domain.BalanceProvider
	monthlyLimit int64
}

func NewQueryService(repository domain.TransactionRepository, balance domain.BalanceProvider, cfg Config) *QueryService {
	if cfg.MonthlyLimitMinor <= 0 {
		cfg.MonthlyLimitMinor = 50_000 * 1_000_000
	}
	return &QueryService{repository: repository, balance: balance, monthlyLimit: cfg.MonthlyLimitMinor}
}

type BalanceSummary struct {
	BalanceMinor      int64
	Currency          int32
	MonthlySpentMinor int64
	MonthlyLimitMinor int64
}

func (s *QueryService) GetBalanceSummary(ctx context.Context, agentDID string) (BalanceSummary, error) {
	if strings.TrimSpace(agentDID) == "" {
		return BalanceSummary{}, fmt.Errorf("agent_did is required")
	}
	spent, err := s.repository.SumPurchaseByAgent(ctx, agentDID)
	if err != nil {
		return BalanceSummary{}, fmt.Errorf("calculate monthly spent: %w", err)
	}
	balance, err := s.balance.USDCBalanceMinor(ctx, agentDID)
	if err != nil {
		return BalanceSummary{}, fmt.Errorf("query onchain balance: %w", err)
	}
	return BalanceSummary{
		BalanceMinor:      balance,
		Currency:          1,
		MonthlySpentMinor: spent,
		MonthlyLimitMinor: s.monthlyLimit,
	}, nil
}

type TransactionPage struct {
	Items []entity.Transaction
	Total int64
}

func (s *QueryService) ListTransactions(ctx context.Context, query entity.TransactionQuery) (TransactionPage, error) {
	if strings.TrimSpace(query.DID) == "" {
		return TransactionPage{}, fmt.Errorf("did is required")
	}
	if query.Type != entity.TransactionPurchase && query.Type != entity.TransactionRevenue {
		return TransactionPage{}, fmt.Errorf("unsupported transaction type: %d", query.Type)
	}
	if query.Limit <= 0 {
		query.Limit = 10
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	items, total, err := s.repository.List(ctx, query)
	if err != nil {
		return TransactionPage{}, fmt.Errorf("list transactions: %w", err)
	}
	return TransactionPage{Items: items, Total: total}, nil
}

func (s *QueryService) GetRevenueSummary(ctx context.Context, skillDID string) (entity.RevenueSummary, error) {
	if strings.TrimSpace(skillDID) == "" {
		return entity.RevenueSummary{}, fmt.Errorf("skill_did is required")
	}
	result, err := s.repository.RevenueSummary(ctx, skillDID)
	if err != nil {
		return entity.RevenueSummary{}, fmt.Errorf("get revenue summary: %w", err)
	}
	return result, nil
}

func (s *QueryService) ListSales(ctx context.Context, skillDID string, limit, offset int) ([]entity.Sale, int64, int, int, error) {
	if strings.TrimSpace(skillDID) == "" {
		return nil, 0, 0, 0, fmt.Errorf("skill_did is required")
	}
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}
	items, total, err := s.repository.ListSales(ctx, skillDID, limit, offset)
	if err != nil {
		return nil, 0, limit, offset, fmt.Errorf("list sales: %w", err)
	}
	return items, total, limit, offset, nil
}

type SyncTransactionCommand struct {
	TxID        string
	AgentDID    string
	SkillDID    string
	AmountMinor int64
	Currency    string
	Type        string
	CreatedAt   time.Time
}

func (s *QueryService) SyncTransaction(ctx context.Context, cmd SyncTransactionCommand) error {
	if strings.TrimSpace(cmd.TxID) == "" {
		return fmt.Errorf("tx_id is required")
	}
	if cmd.AmountMinor < 0 {
		return fmt.Errorf("amount_minor must be non-negative")
	}
	var txType entity.TransactionType
	switch strings.ToLower(strings.TrimSpace(cmd.Type)) {
	case "1", "purchase":
		txType = entity.TransactionPurchase
	case "2", "revenue", "reward":
		txType = entity.TransactionRevenue
	default:
		return fmt.Errorf("unsupported transaction type: %q", cmd.Type)
	}
	if cmd.CreatedAt.IsZero() {
		cmd.CreatedAt = time.Now().UTC()
	}
	currency := int32(1)
	if strings.EqualFold(strings.TrimSpace(cmd.Currency), "USDT") || strings.TrimSpace(cmd.Currency) == "2" {
		currency = 2
	}
	return s.repository.Upsert(ctx, entity.Transaction{
		TxID: cmd.TxID, SourceTxID: cmd.TxID, AgentDID: cmd.AgentDID, SkillDID: cmd.SkillDID,
		AmountMinor: cmd.AmountMinor, Currency: currency, Type: txType, CreatedAt: cmd.CreatedAt,
	})
}
