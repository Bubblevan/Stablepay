package main

import (
	"context"
	"log"
	"os"
	"strconv"

	"query-service/kitex_gen/stablepay/common"
	query_service "query-service/kitex_gen/stablepay/query_service"
)

// QueryServiceImpl implements the last service interface defined in the IDL.
type QueryServiceImpl struct{}

// GetBalanceSummary implements the QueryServiceImpl interface.
func (s *QueryServiceImpl) GetBalanceSummary(ctx context.Context, req *query_service.GetBalanceSummaryRequest) (resp *query_service.GetBalanceSummaryResponse, err error) {
	resp = query_service.NewGetBalanceSummaryResponse()
	resp.Base = &common.BaseResp{Code: 0, Message: "success"}

	var spentMinor int64
	if err := DB.Model(&TransactionRecord{}).
		Where("agent_did = ? AND tx_type = ?", req.AgentDid, int32(query_service.TransactionType_PURCHASE)).
		Select("COALESCE(SUM(amount_minor), 0)").
		Scan(&spentMinor).Error; err != nil {
		resp.Base.Code = 30003
		resp.Base.Message = "failed to calculate monthly spent"
		return resp, nil
	}

	// PRD 语义：用户侧余额应返回链上钱包余额，而非账本 inflow-spent 净额
	onchainMinor, err := queryOnchainUSDCBalanceMinor(ctx, req.AgentDid)
	if err != nil {
		resp.Base.Code = 30003
		resp.Base.Message = "failed to query onchain balance"
		return resp, nil
	}

	resp.MonthlySpentMinor = spentMinor
	resp.Currency = common.Currency_USDC
	resp.BalanceMinor = onchainMinor
	resp.MonthlyLimitMinor = getenvInt64Default("QUERY_MONTHLY_LIMIT_MINOR", 50000*1000000)

	return resp, nil
}

// ListTransactions implements the QueryServiceImpl interface.
func (s *QueryServiceImpl) ListTransactions(ctx context.Context, req *query_service.ListTransactionsRequest) (resp *query_service.ListTransactionsResponse, err error) {
	resp = query_service.NewListTransactionsResponse()
	resp.Base = &common.BaseResp{Code: 0, Message: "success"}

	if req.Page == nil {
		req.Page = &common.PageRequest{Limit: 10, Offset: 0}
	}

	query := DB.Model(&TransactionRecord{})
	if req.Type == query_service.TransactionType_PURCHASE {
		query = query.Where("agent_did = ? AND tx_type = ?", req.Did, int32(req.Type))
	} else if req.Type == query_service.TransactionType_REVENUE {
		query = query.Where("skill_did = ? AND tx_type = ?", req.Did, int32(req.Type))
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		log.Printf("count transactions error: %v", err)
		resp.Base.Code = 30003
		resp.Base.Message = "failed to count records"
		return resp, nil
	}

	var records []TransactionRecord
	if err := query.Order("created_at DESC").
		Limit(int(req.Page.Limit)).
		Offset(int(req.Page.Offset)).
		Find(&records).Error; err != nil {
		log.Printf("find transactions error: %v", err)
		resp.Base.Code = 30003
		resp.Base.Message = "failed to fetch records"
		return resp, nil
	}

	var items []*query_service.TransactionItem
	for _, r := range records {
		txID := r.SourceTxId
		if txID == "" {
			txID = r.TxId
		}
		items = append(items, &query_service.TransactionItem{
			TxId:        txID,
			SkillDid:    r.SkillDid,
			AmountMinor: r.AmountMinor,
			Currency:    common.Currency(r.Currency),
			Type:        query_service.TransactionType(r.TxType),
			CreatedAt:   r.CreatedAt,
		})
	}

	resp.Items = items
	resp.Page = common.NewPageResult_()
	resp.Page.Total = int32(total)

	return resp, nil
}

// GetRevenueSummary implements the QueryServiceImpl interface.
func (s *QueryServiceImpl) GetRevenueSummary(ctx context.Context, req *query_service.GetRevenueSummaryRequest) (resp *query_service.GetRevenueSummaryResponse, err error) {
	resp = query_service.NewGetRevenueSummaryResponse()
	resp.Base = &common.BaseResp{Code: 0, Message: "success"}

	var totalSales int64
	var totalRevenue int64

	DB.Model(&TransactionRecord{}).
		Where("skill_did = ? AND tx_type = ?", req.SkillDid, int32(query_service.TransactionType_REVENUE)).
		Count(&totalSales)

	DB.Model(&TransactionRecord{}).
		Where("skill_did = ? AND tx_type = ?", req.SkillDid, int32(query_service.TransactionType_REVENUE)).
		Select("COALESCE(SUM(amount_minor), 0)").
		Scan(&totalRevenue)

	resp.TotalSales = int32(totalSales)
	resp.TotalRevenueMinor = totalRevenue
	resp.Currency = common.Currency_USDC

	type TrendResult struct {
		Date   string
		Amount int64
	}
	var trends []TrendResult

	DB.Model(&TransactionRecord{}).
		Select("SUBSTR(created_at, 1, 10) as date, SUM(amount_minor) as amount").
		Where("skill_did = ? AND tx_type = ?", req.SkillDid, int32(query_service.TransactionType_REVENUE)).
		Group("SUBSTR(created_at, 1, 10)").
		Order("date ASC").
		Scan(&trends)

	var trendItems []*query_service.SalesTrendItem
	for _, t := range trends {
		trendItems = append(trendItems, &query_service.SalesTrendItem{
			Date:        t.Date,
			AmountMinor: t.Amount,
		})
	}
	resp.SalesTrend = trendItems

	return resp, nil
}

func getenvInt64Default(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return i
}

// ListSales implements the QueryServiceImpl interface（与 HTTP /internal/sales 数据一致）。
func (s *QueryServiceImpl) ListSales(ctx context.Context, req *query_service.ListSalesRequest) (resp *query_service.ListSalesResponse, err error) {
	_ = ctx
	resp = query_service.NewListSalesResponse()
	items, total, lim, off, err := listSalesData(string(req.SkillDid), int(req.Limit), int(req.Offset))
	if err != nil {
		log.Printf("list sales error: %v", err)
		resp.Base = &common.BaseResp{Code: 30003, Message: "failed to fetch sales records"}
		return resp, nil
	}
	resp.Base = &common.BaseResp{Code: 0, Message: "success"}
	resp.SkillDid = req.SkillDid
	resp.Total = total
	resp.Limit = int32(lim)
	resp.Offset = int32(off)
	out := make([]*query_service.SaleRecordItem, 0, len(items))
	for _, it := range items {
		row := &query_service.SaleRecordItem{
			TxId:        common.TxId(it.TxID),
			AgentDid:    common.DID(it.AgentDID),
			SkillDid:    common.DID(it.SkillDID),
			AmountMinor: it.AmountMinor,
			Currency:    it.Currency,
			CreatedAt:   it.CreatedAt,
		}
		if it.SourceTxID != "" {
			st := common.TxId(it.SourceTxID)
			row.SourceTxId = &st
		}
		out = append(out, row)
	}
	resp.Items = out
	return resp, nil
}
