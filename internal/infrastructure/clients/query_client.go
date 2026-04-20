package clients

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/pkg/retry"
	"stablepay/api-gateway/internal/application"
	"stablepay/api-gateway/kitex_gen/stablepay/common"
	"stablepay/api-gateway/kitex_gen/stablepay/query_service"
	"stablepay/api-gateway/kitex_gen/stablepay/query_service/queryservice"
)

// KitexQueryClient 通过 Kitex RPC 调用 query-service（默认监听 8084）。
type KitexQueryClient struct {
	cli queryservice.Client
}

// NewKitexQueryClient 创建客户端。destService 为 Kitex 目标服务名；hostPort 如 stablepay-query-service:8084。
func NewKitexQueryClient(destService, hostPort string, timeoutMs, retryCount int) (application.QueryServiceClient, error) {
	opts := []client.Option{
		client.WithHostPorts(hostPort),
	}
	if timeoutMs > 0 {
		opts = append(opts, client.WithRPCTimeout(time.Duration(timeoutMs)*time.Millisecond))
	}
	if retryCount > 0 {
		opts = append(opts, client.WithFailureRetry(retry.NewFailurePolicy()))
	}
	cli, err := queryservice.NewClient(destService, opts...)
	if err != nil {
		return nil, fmt.Errorf("kitex query client: %w", err)
	}
	return &KitexQueryClient{cli: cli}, nil
}

func (c *KitexQueryClient) GetBalance(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	start := time.Now()
	agentDID := stringFromIface(req["agent_did"])
	log.Printf("[Gateway:QueryClient] GetBalance started for DID: %s", agentDID)

	kreq := query_service.NewGetBalanceSummaryRequest()
	kreq.Base = &common.BaseReq{}
	kreq.AgentDid = common.DID(agentDID)

	log.Printf("[Gateway:QueryClient] Calling GetBalanceSummary RPC...")
	rpcStart := time.Now()
	resp, err := c.cli.GetBalanceSummary(ctx, kreq)
	rpcDuration := time.Since(rpcStart)
	log.Printf("[Gateway:QueryClient] RPC call completed in %v", rpcDuration)

	if err != nil {
		log.Printf("[Gateway:QueryClient] ERROR: RPC call failed: %v", err)
		return nil, 500, 0, err
	}

	base := resp.GetBase()
	log.Printf("[Gateway:QueryClient] RPC response: code=%d, message=%s", base.GetCode(), base.GetMessage())
	log.Printf("[Gateway:QueryClient] Balance data: minor=%d, currency=%d, spent=%d, limit=%d",
		resp.GetBalanceMinor(), resp.GetCurrency(), resp.GetMonthlySpentMinor(), resp.GetMonthlyLimitMinor())

	out := map[string]interface{}{
		"base":                baseToMap(resp.GetBase()),
		"balance_minor":       resp.GetBalanceMinor(),
		"currency":            int64(resp.GetCurrency()),
		"monthly_spent_minor": resp.GetMonthlySpentMinor(),
		"monthly_limit_minor": resp.GetMonthlyLimitMinor(),
	}
	log.Printf("[Gateway:QueryClient] GetBalance completed in %v", time.Since(start))
	return out, 200, int(resp.GetBase().GetCode()), nil
}

func (c *KitexQueryClient) GetTransactions(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	kreq := query_service.NewListTransactionsRequest()
	kreq.Base = &common.BaseReq{}
	kreq.Did = common.DID(stringFromIface(req["did"]))
	kreq.Type = parseQueryTxType(stringFromIface(req["type"]))
	limit := int32(intFromReq(req, "limit", 10))
	offset := int32(intFromReq(req, "offset", 0))
	if limit <= 0 {
		limit = 10
	}
	if offset < 0 {
		offset = 0
	}
	kreq.Page = &common.PageRequest{Limit: limit, Offset: offset}
	resp, err := c.cli.ListTransactions(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	items := make([]map[string]interface{}, 0, len(resp.GetItems()))
	for _, it := range resp.GetItems() {
		if it == nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"tx_id":        string(it.GetTxId()),
			"skill_did":    string(it.GetSkillDid()),
			"amount_minor": it.GetAmountMinor(),
			"currency":     int64(it.GetCurrency()),
			"type":         int64(it.GetType()),
			"created_at":   it.GetCreatedAt(),
		})
	}
	out := map[string]interface{}{
		"base":  baseToMap(resp.GetBase()),
		"items": items,
	}
	if pg := resp.GetPage(); pg != nil {
		out["page"] = map[string]interface{}{"total": pg.GetTotal()}
	}
	return out, 200, int(resp.GetBase().GetCode()), nil
}

func (c *KitexQueryClient) GetRevenue(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	kreq := query_service.NewGetRevenueSummaryRequest()
	kreq.Base = &common.BaseReq{}
	kreq.SkillDid = common.DID(stringFromIface(req["skill_did"]))
	resp, err := c.cli.GetRevenueSummary(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	trend := make([]map[string]interface{}, 0, len(resp.GetSalesTrend()))
	for _, t := range resp.GetSalesTrend() {
		if t == nil {
			continue
		}
		trend = append(trend, map[string]interface{}{
			"date":         t.GetDate(),
			"amount_minor": t.GetAmountMinor(),
		})
	}
	out := map[string]interface{}{
		"base":                baseToMap(resp.GetBase()),
		"total_revenue_minor": resp.GetTotalRevenueMinor(),
		"total_sales":         resp.GetTotalSales(),
		"currency":            int64(resp.GetCurrency()),
		"sales_trend":         trend,
	}
	return out, 200, int(resp.GetBase().GetCode()), nil
}

// GetSales 走 ListSales RPC，响应字段与 query-service HTTP /internal/sales（listSalesRecords）一致；无 source_tx_id 时不写入该键。
func (c *KitexQueryClient) GetSales(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	skillDID := stringFromIface(req["skill_did"])
	limit := intFromReq(req, "limit", 10)
	offset := intFromReq(req, "offset", 0)
	kreq := query_service.NewListSalesRequest()
	kreq.Base = &common.BaseReq{}
	kreq.SkillDid = common.DID(skillDID)
	kreq.Limit = int32(limit)
	kreq.Offset = int32(offset)
	resp, err := c.cli.ListSales(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	items := make([]map[string]interface{}, 0, len(resp.GetItems()))
	for _, it := range resp.GetItems() {
		if it == nil {
			continue
		}
		row := map[string]interface{}{
			"tx_id":        string(it.GetTxId()),
			"agent_did":    string(it.GetAgentDid()),
			"skill_did":    string(it.GetSkillDid()),
			"amount_minor": it.GetAmountMinor(),
			"currency":     it.GetCurrency(),
			"created_at":   it.GetCreatedAt(),
		}
		if it.IsSetSourceTxId() && string(it.GetSourceTxId()) != "" {
			row["source_tx_id"] = string(it.GetSourceTxId())
		}
		items = append(items, row)
	}
	out := map[string]interface{}{
		"base": map[string]interface{}{
			"code":    int(resp.GetBase().GetCode()),
			"message": resp.GetBase().GetMessage(),
		},
		"skill_did": skillDID,
		"items":     items,
		"total":     resp.GetTotal(),
		"limit":     int(resp.GetLimit()),
		"offset":    int(resp.GetOffset()),
	}
	return out, 200, int(resp.GetBase().GetCode()), nil
}

func parseQueryTxType(s string) query_service.TransactionType {
	switch s {
	case "", "1", "PURCHASE":
		return query_service.TransactionType_PURCHASE
	case "2", "REVENUE":
		return query_service.TransactionType_REVENUE
	}
	if n, err := strconv.Atoi(s); err == nil {
		return query_service.TransactionType(n)
	}
	return query_service.TransactionType_PURCHASE
}

func intFromReq(req map[string]interface{}, key string, def int) int {
	v, ok := req[key]
	if !ok || v == nil {
		return def
	}
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		n, err := strconv.Atoi(x)
		if err != nil {
			return def
		}
		return n
	default:
		n, err := strconv.Atoi(fmt.Sprintf("%v", v))
		if err != nil {
			return def
		}
		return n
	}
}
