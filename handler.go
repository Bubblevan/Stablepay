package main

import (
	"context"
	query_service "query-service/kitex_gen/stablepay/query_service"
)

// QueryServiceImpl implements the last service interface defined in the IDL.
type QueryServiceImpl struct{}

// GetBalanceSummary implements the QueryServiceImpl interface.
func (s *QueryServiceImpl) GetBalanceSummary(ctx context.Context, req *query_service.GetBalanceSummaryRequest) (resp *query_service.GetBalanceSummaryResponse, err error) {
	resp = query_service.NewGetBalanceSummaryResponse()
	resp.Base = &common.BaseResp{Code: 0, Message: "success"}

	var spentMinor int64
	// 利用 COALESCE 防止查不到数据时 SUM 返回 NULL 导致报错
	if err := DB.Model(&TransactionRecord{}).
		Where("agent_did = ? AND tx_type = ?", req.AgentDid, int32(query_service.TransactionType_PURCHASE)).
		Select("COALESCE(SUM(amount_minor), 0)").
		Scan(&spentMinor).Error; err != nil {
		resp.Base.Code = 30003
		resp.Base.Message = "failed to calculate monthly spent"
		return resp, nil
	}

	resp.MonthlySpentMinor = spentMinor
	resp.Currency = common.Currency_USDC 
	
	// Demo
	resp.BalanceMinor = 10000 * 1000000 
	resp.MonthlyLimitMinor = 50000 * 1000000 

	return resp, nil
}

// ListTransactions implements the QueryServiceImpl interface.
func (s *QueryServiceImpl) ListTransactions(ctx context.Context, req *query_service.ListTransactionsRequest) (resp *query_service.ListTransactionsResponse, err error) {
	// 初始化统一的返回结构
	resp = query_service.NewListTransactionsResponse()
	resp.Base = &common.BaseResp{Code: 0, Message: "success"}

	
	if req.Page == nil {
		req.Page = &common.PageRequest{Limit: 10, Offset: 0}
	}// 容错：如果前端没传分页参数，给个默认值

	
	query := DB.Model(&TransactionRecord{})// 构造 GORM 查询器

	// 根据查询类型来区分（如果是查支出，就匹配 agent_did；如果是查收入，就匹配 skill_did）
	if req.Type == query_service.TransactionType_PURCHASE {
		query = query.Where("agent_did = ? AND tx_type = ?", req.Did, int32(req.Type))
	} else if req.Type == query_service.TransactionType_REVENUE {
		query = query.Where("skill_did = ? AND tx_type = ?", req.Did, int32(req.Type))
	}

	// 3. 第一步查询：获取符合条件的总条数 (Total)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		log.Printf("count transactions error: %v", err)
		resp.Base.Code = 30003 // 对应你们 common 里定义的 DATABASE_CONNECTION_ERROR
		resp.Base.Message = "failed to count records"
		return resp, nil
	}

	// 4. 第二步查询：获取当前页的具体数据 (Limit + Offset)
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

	// 5. 将数据库里的数据组装成 Thrift 接口定义的返回格式
	var items []*query_service.TransactionItem
	for _, r := range records {
		items = append(items, &query_service.TransactionItem{
			TxId:        r.TxId,
			SkillDid:    r.SkillDid,
			AmountMinor: r.AmountMinor,
			Currency:    common.Currency(r.Currency),
			Type:        query_service.TransactionType(r.TxType),
			CreatedAt:   r.CreatedAt,
		})
	}

	
	resp.Items = items
	resp.Page = &common.PageResult{Total: int32(total)}// 赋值给最终的 response

	return resp, nil
}

// GetRevenueSummary implements the QueryServiceImpl interface.
func (s *QueryServiceImpl) GetRevenueSummary(ctx context.Context, req *query_service.GetRevenueSummaryRequest) (resp *query_service.GetRevenueSummaryResponse, err error) {
	resp = query_service.NewGetRevenueSummaryResponse()
	resp.Base = &common.BaseResp{Code: 0, Message: "success"}

	var totalSales int64
	var totalRevenue int64

	// 1. 查总单量
	DB.Model(&TransactionRecord{}).
		Where("skill_did = ? AND tx_type = ?", req.SkillDid, int32(query_service.TransactionType_REVENUE)).
		Count(&totalSales)

	// 2. 查总收入
	DB.Model(&TransactionRecord{}).
		Where("skill_did = ? AND tx_type = ?", req.SkillDid, int32(query_service.TransactionType_REVENUE)).
		Select("COALESCE(SUM(amount_minor), 0)").
		Scan(&totalRevenue)

	resp.TotalSales = int32(totalSales)
	resp.TotalRevenueMinor = totalRevenue
	resp.Currency = common.Currency_USDC

	// 3. 查销售趋势 (按日期分组汇总)
	type TrendResult struct {
		Date   string
		Amount int64
	}// 用 SUBSTR 截取 created_at 的前 10 位 (即 YYYY-MM-DD) 作为分组依据
	var trends []TrendResult
	
	DB.Model(&TransactionRecord{}).
		Select("SUBSTR(created_at, 1, 10) as date, SUM(amount_minor) as amount").
		Where("skill_did = ? AND tx_type = ?", req.SkillDid, int32(query_service.TransactionType_REVENUE)).
		Group("SUBSTR(created_at, 1, 10)").
		Order("date ASC").
		Scan(&trends)

	// 组装返回体
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
