package main

import (
	"context"
	query_service "query-service/kitex_gen/stablepay/query_service"
)

// QueryServiceImpl implements the last service interface defined in the IDL.
type QueryServiceImpl struct{}

// GetBalanceSummary implements the QueryServiceImpl interface.
func (s *QueryServiceImpl) GetBalanceSummary(ctx context.Context, req *query_service.GetBalanceSummaryRequest) (resp *query_service.GetBalanceSummaryResponse, err error) {
	// TODO: Your code here...
	return
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
	// TODO: Your code here...
	return
}
