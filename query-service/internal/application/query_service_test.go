package application

import (
	"context"
	"testing"

	"github.com/stablepay/query-service/internal/domain/entity"
)

type fakeRepository struct {
	spent int64
	items []entity.Transaction
}

func (f *fakeRepository) SumPurchaseByAgent(context.Context, string) (int64, error) {
	return f.spent, nil
}
func (f *fakeRepository) List(context.Context, entity.TransactionQuery) ([]entity.Transaction, int64, error) {
	return f.items, int64(len(f.items)), nil
}
func (f *fakeRepository) RevenueSummary(context.Context, string) (entity.RevenueSummary, error) {
	return entity.RevenueSummary{TotalSales: 1, TotalRevenueMinor: 100}, nil
}
func (f *fakeRepository) ListSales(context.Context, string, int, int) ([]entity.Sale, int64, error) {
	return []entity.Sale{{Transaction: entity.Transaction{TxID: "tx-1"}}}, 1, nil
}
func (f *fakeRepository) Upsert(context.Context, entity.Transaction) error { return nil }

type fakeBalanceProvider struct{ balance int64 }

func (f fakeBalanceProvider) USDCBalanceMinor(context.Context, string) (int64, error) {
	return f.balance, nil
}

func TestQueryServiceBalanceSummaryUsesInjectedPorts(t *testing.T) {
	service := NewQueryService(&fakeRepository{spent: 25}, fakeBalanceProvider{balance: 1000}, Config{MonthlyLimitMinor: 500})
	result, err := service.GetBalanceSummary(context.Background(), "did:solana:agent")
	if err != nil {
		t.Fatalf("GetBalanceSummary() error = %v", err)
	}
	if result.BalanceMinor != 1000 || result.MonthlySpentMinor != 25 || result.MonthlyLimitMinor != 500 {
		t.Fatalf("unexpected balance summary: %+v", result)
	}
}

func TestQueryServiceSyncTransactionNormalizesInput(t *testing.T) {
	service := NewQueryService(&fakeRepository{}, fakeBalanceProvider{}, Config{})
	if err := service.SyncTransaction(context.Background(), SyncTransactionCommand{TxID: "tx-1", Type: "revenue", Currency: "USDT", AmountMinor: 10}); err != nil {
		t.Fatalf("SyncTransaction() error = %v", err)
	}
	if err := service.SyncTransaction(context.Background(), SyncTransactionCommand{TxID: "tx-1", Type: "refund"}); err == nil {
		t.Fatal("SyncTransaction() should reject unsupported transaction types")
	}
}

func TestQueryServiceValidatesListQuery(t *testing.T) {
	service := NewQueryService(&fakeRepository{}, fakeBalanceProvider{}, Config{})
	if _, err := service.ListTransactions(context.Background(), entity.TransactionQuery{DID: "agent", Type: 9}); err == nil {
		t.Fatal("ListTransactions() should reject unsupported transaction types")
	}
}
