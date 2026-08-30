package rpc

import (
	"context"

	"github.com/stablepay/query-service/internal/application"
	"github.com/stablepay/query-service/internal/domain/entity"
	"github.com/stablepay/query-service/kitex_gen/stablepay/common"
	query_service "github.com/stablepay/query-service/kitex_gen/stablepay/query_service"
)

type Handler struct{ service *application.QueryService }

func NewHandler(service *application.QueryService) *Handler { return &Handler{service: service} }

func successBase() *common.BaseResp { return &common.BaseResp{Code: 0, Message: "success"} }
func errorBase(message string) *common.BaseResp {
	return &common.BaseResp{Code: 30003, Message: message}
}

func (h *Handler) GetBalanceSummary(ctx context.Context, req *query_service.GetBalanceSummaryRequest) (*query_service.GetBalanceSummaryResponse, error) {
	resp := query_service.NewGetBalanceSummaryResponse()
	if req == nil {
		resp.Base = errorBase("request is required")
		return resp, nil
	}
	result, err := h.service.GetBalanceSummary(ctx, string(req.AgentDid))
	if err != nil {
		resp.Base = errorBase(err.Error())
		return resp, nil
	}
	resp.Base = successBase()
	resp.BalanceMinor = result.BalanceMinor
	resp.Currency = common.Currency(result.Currency)
	resp.MonthlySpentMinor = result.MonthlySpentMinor
	resp.MonthlyLimitMinor = result.MonthlyLimitMinor
	return resp, nil
}

func (h *Handler) ListTransactions(ctx context.Context, req *query_service.ListTransactionsRequest) (*query_service.ListTransactionsResponse, error) {
	resp := query_service.NewListTransactionsResponse()
	if req == nil {
		resp.Base = errorBase("request is required")
		return resp, nil
	}
	limit, offset := int32(10), int32(0)
	if req.Page != nil {
		limit, offset = req.Page.Limit, req.Page.Offset
	}
	page, err := h.service.ListTransactions(ctx, entity.TransactionQuery{DID: string(req.Did), Type: entity.TransactionType(req.Type), Limit: int(limit), Offset: int(offset)})
	if err != nil {
		resp.Base = errorBase(err.Error())
		return resp, nil
	}
	resp.Base = successBase()
	resp.Items = make([]*query_service.TransactionItem, 0, len(page.Items))
	for _, item := range page.Items {
		resp.Items = append(resp.Items, &query_service.TransactionItem{TxId: common.TxId(firstNonEmpty(item.SourceTxID, item.TxID)), SkillDid: common.DID(item.SkillDID), AmountMinor: item.AmountMinor, Currency: common.Currency(item.Currency), Type: query_service.TransactionType(item.Type), CreatedAt: item.CreatedAt.Format("2006-01-02T15:04:05Z07:00")})
	}
	resp.Page = common.NewPageResult_()
	resp.Page.Total = int32(page.Total)
	return resp, nil
}

func (h *Handler) GetRevenueSummary(ctx context.Context, req *query_service.GetRevenueSummaryRequest) (*query_service.GetRevenueSummaryResponse, error) {
	resp := query_service.NewGetRevenueSummaryResponse()
	if req == nil {
		resp.Base = errorBase("request is required")
		return resp, nil
	}
	result, err := h.service.GetRevenueSummary(ctx, string(req.SkillDid))
	if err != nil {
		resp.Base = errorBase(err.Error())
		return resp, nil
	}
	resp.Base = successBase()
	resp.TotalSales = int32(result.TotalSales)
	resp.TotalRevenueMinor = result.TotalRevenueMinor
	resp.Currency = common.Currency_USDC
	resp.SalesTrend = make([]*query_service.SalesTrendItem, 0, len(result.Trend))
	for _, trend := range result.Trend {
		resp.SalesTrend = append(resp.SalesTrend, &query_service.SalesTrendItem{Date: trend.Date, AmountMinor: trend.AmountMinor})
	}
	return resp, nil
}

func (h *Handler) ListSales(ctx context.Context, req *query_service.ListSalesRequest) (*query_service.ListSalesResponse, error) {
	resp := query_service.NewListSalesResponse()
	if req == nil {
		resp.Base = errorBase("request is required")
		return resp, nil
	}
	items, total, limit, offset, err := h.service.ListSales(ctx, string(req.SkillDid), int(req.Limit), int(req.Offset))
	if err != nil {
		resp.Base = errorBase(err.Error())
		return resp, nil
	}
	resp.Base = successBase()
	resp.SkillDid = req.SkillDid
	resp.Total, resp.Limit, resp.Offset = total, int32(limit), int32(offset)
	resp.Items = make([]*query_service.SaleRecordItem, 0, len(items))
	for _, item := range items {
		row := &query_service.SaleRecordItem{TxId: common.TxId(firstNonEmpty(item.SourceTxID, item.TxID)), AgentDid: common.DID(item.AgentDID), SkillDid: common.DID(item.SkillDID), AmountMinor: item.AmountMinor, Currency: currencyLabel(item.Currency), CreatedAt: item.CreatedAt.Format("2006-01-02T15:04:05Z07:00")}
		if item.SourceTxID != "" {
			txID := common.TxId(item.SourceTxID)
			row.SourceTxId = &txID
		}
		resp.Items = append(resp.Items, row)
	}
	return resp, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func currencyLabel(code int32) string {
	if code == 2 {
		return "USDT"
	}
	return "USDC"
}
