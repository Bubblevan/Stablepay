package main

import (
	"query-service/kitex_gen/stablepay/common"
	query_service "query-service/kitex_gen/stablepay/query_service"
)

type SaleItem struct {
	TxID        string `json:"tx_id"`
	SourceTxID  string `json:"source_tx_id,omitempty"`
	AgentDID    string `json:"agent_did"`
	SkillDID    string `json:"skill_did"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
	CreatedAt   string `json:"created_at"`
}

// listSalesData 与 HTTP /internal、sales 及 RPC ListSales 共用查询逻辑。
func listSalesData(skillDID string, limit, offset int) (items []SaleItem, total int64, lim, off int, err error) {
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}
	lim, off = limit, offset

	query := DB.Model(&TransactionRecord{}).
		Where("skill_did = ? AND tx_type = ?", skillDID, int32(query_service.TransactionType_REVENUE))

	if err = query.Count(&total).Error; err != nil {
		return nil, 0, lim, off, err
	}

	var records []TransactionRecord
	if err = query.Order("created_at DESC").Limit(limit).Offset(offset).Find(&records).Error; err != nil {
		return nil, 0, lim, off, err
	}

	items = make([]SaleItem, 0, len(records))
	for _, r := range records {
		txID := r.SourceTxId
		if txID == "" {
			txID = r.TxId
		}
		items = append(items, SaleItem{
			TxID:        txID,
			SourceTxID:  r.SourceTxId,
			AgentDID:    r.AgentDid,
			SkillDID:    r.SkillDid,
			AmountMinor: r.AmountMinor,
			Currency:    currencyLabel(r.Currency),
			CreatedAt:   r.CreatedAt,
		})
	}
	return items, total, lim, off, nil
}

func listSalesRecords(skillDID string, limit, offset int) (map[string]any, error) {
	items, total, lim, off, err := listSalesData(skillDID, limit, offset)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"base": map[string]any{
			"code":    0,
			"message": "success",
		},
		"skill_did": skillDID,
		"items":     items,
		"total":     total,
		"limit":     lim,
		"offset":    off,
	}, nil
}

func currencyLabel(code int32) string {
	switch common.Currency(code) {
	case common.Currency_USDT:
		return "USDT"
	default:
		return "USDC"
	}
}
