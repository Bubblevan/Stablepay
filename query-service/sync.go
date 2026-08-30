package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

type TransactionSyncRequest struct {
	TxID        string `json:"tx_id"`
	AgentDID    string `json:"agent_did"`
	SkillDID    string `json:"skill_did"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Type        string `json:"type"`
	CreatedAt   string `json:"created_at"`
}

func syncTransactionRecord(ctx context.Context, req *TransactionSyncRequest) error {
	if strings.TrimSpace(req.TxID) == "" {
		return fmt.Errorf("tx_id is required")
	}
	if req.AmountMinor < 0 {
		return fmt.Errorf("amount_minor must be non-negative")
	}

	txType, err := parseTransactionType(req.Type)
	if err != nil {
		return err
	}

	createdAt := req.CreatedAt
	if strings.TrimSpace(createdAt) == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339)
	}

	record := &TransactionRecord{
		TxId:        buildStorageTxID(req.TxID, txType),
		SourceTxId:  req.TxID,
		AgentDid:    req.AgentDID,
		SkillDid:    req.SkillDID,
		AmountMinor: req.AmountMinor,
		Currency:    parseCurrency(req.Currency),
		TxType:      txType,
		CreatedAt:   createdAt,
	}

	return DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "tx_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"source_tx_id", "agent_did", "skill_did", "amount_minor", "currency", "tx_type", "created_at"}),
	}).Create(record).Error
}

func buildStorageTxID(txID string, txType int32) string {
	switch txType {
	case 1:
		return txID + "#purchase"
	case 2:
		return txID + "#revenue"
	default:
		return txID
	}
}

func parseTransactionType(raw string) (int32, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "purchase":
		return 1, nil
	case "2", "revenue", "reward":
		return 2, nil
	default:
		return 0, fmt.Errorf("unsupported transaction type: %q", raw)
	}
}

func parseCurrency(raw string) int32 {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "USDT", "2":
		return 2
	default:
		return 1
	}
}
