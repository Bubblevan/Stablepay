package domain

import (
	"context"

	"github.com/stablepay/verification-service/internal/domain/entity"
)

type PurchaseRepository interface {
	Find(ctx context.Context, agentDID, skillDID string) (*entity.PurchaseRecord, error)
	FindByEventID(ctx context.Context, eventID string) (*entity.PurchaseRecord, error)
	Create(ctx context.Context, record *entity.PurchaseRecord) error
}
