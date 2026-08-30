package application

import (
	"context"
	"strings"
	"time"

	"github.com/stablepay/verification-service/internal/domain"
	"github.com/stablepay/verification-service/internal/domain/entity"
)

const (
	CodeOK                int32 = 0
	CodeInternalError     int32 = 10000
	CodeNotFound          int32 = 10001
	CodeInvalidParameters int32 = 10002
)

type Service struct {
	purchases domain.PurchaseRepository
}

func NewService(purchases domain.PurchaseRepository) *Service {
	return &Service{purchases: purchases}
}

type PurchaseResult struct {
	Purchased    bool
	PurchaseTime *time.Time
	TxID         string
	AmountMinor  *int64
	Currency     *int32
	TxHash       string
	Code         int32
	Message      string
}

func (s *Service) VerifyPurchase(ctx context.Context, agentDID, skillDID string) (PurchaseResult, error) {
	if strings.TrimSpace(agentDID) == "" || strings.TrimSpace(skillDID) == "" {
		return PurchaseResult{Code: CodeInvalidParameters, Message: "agent_did and skill_did are required"}, nil
	}
	record, err := s.purchases.Find(ctx, agentDID, skillDID)
	if err != nil {
		return PurchaseResult{Purchased: false, Code: CodeNotFound, Message: "record not found"}, nil
	}
	return purchaseResult(record, CodeOK, "success"), nil
}

func (s *Service) BatchVerifyPurchase(ctx context.Context, agentDID string, skillDIDs []string) ([]PurchaseResult, int32, string, error) {
	if strings.TrimSpace(agentDID) == "" || len(skillDIDs) == 0 {
		return nil, CodeInvalidParameters, "agent_did and skill_dids are required", nil
	}
	items := make([]PurchaseResult, 0, len(skillDIDs))
	for _, skillDID := range skillDIDs {
		item, err := s.VerifyPurchase(ctx, agentDID, skillDID)
		if err != nil {
			return nil, CodeInternalError, err.Error(), err
		}
		items = append(items, item)
	}
	return items, CodeOK, "success", nil
}

func (s *Service) GetPurchaseProof(ctx context.Context, agentDID, skillDID string) (PurchaseResult, error) {
	result, err := s.VerifyPurchase(ctx, agentDID, skillDID)
	if err != nil || !result.Purchased {
		return result, err
	}
	result.Code, result.Message = CodeOK, "success"
	return result, nil
}

func purchaseResult(record *entity.PurchaseRecord, code int32, message string) PurchaseResult {
	amount, currency := record.AmountMinor, record.Currency
	return PurchaseResult{
		Purchased:    true,
		PurchaseTime: &record.CreatedAt,
		TxID:         record.TxID,
		AmountMinor:  &amount,
		Currency:     &currency,
		TxHash:       record.TxHash,
		Code:         code,
		Message:      message,
	}
}
