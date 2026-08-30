package clients

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/pkg/retry"
	"github.com/stablepay/api-gateway/internal/application"
	"github.com/stablepay/api-gateway/kitex_gen/stablepay/common"
	"github.com/stablepay/api-gateway/kitex_gen/stablepay/payment_service"
	"github.com/stablepay/api-gateway/kitex_gen/stablepay/payment_service/paymentservice"
)

// KitexPaymentClient is the gateway's only payment-service client. The public
// HTTP API is translated here into the canonical internal RPC contract.
type KitexPaymentClient struct {
	cli paymentservice.Client
}

func NewKitexPaymentClient(destService, hostPort string, timeoutMs, retryCount int) (application.PaymentServiceClient, error) {
	opts := []client.Option{client.WithHostPorts(hostPort), client.WithResolver(nil)}
	if timeoutMs > 0 {
		opts = append(opts, client.WithRPCTimeout(time.Duration(timeoutMs)*time.Millisecond))
	}
	if retryCount > 0 {
		opts = append(opts, client.WithFailureRetry(retry.NewFailurePolicy()))
	}
	cli, err := paymentservice.NewClient(destService, opts...)
	if err != nil {
		return nil, fmt.Errorf("kitex payment client: %w", err)
	}
	return &KitexPaymentClient{cli: cli}, nil
}

func (c *KitexPaymentClient) Pay(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	amountMinor, err := parseAmountMinor(req)
	if err != nil {
		return nil, 400, 10001, err
	}
	currency, err := parseCurrency(stringFromIface(req["currency"]))
	if err != nil {
		return nil, 400, 10001, err
	}
	kreq := payment_service.NewInitiatePaymentRequest()
	kreq.Base = baseRequest(req)
	kreq.AgentDid = common.DID(stringFromIface(req["agent_did"]))
	kreq.SkillDid = common.DID(stringFromIface(req["skill_did"]))
	kreq.AmountMinor = amountMinor
	kreq.Currency = currency
	setOptional(&kreq.Signature, stringFromIface(req["signature"]))
	setOptional(&kreq.Timestamp, stringFromIface(req["timestamp"]))
	setOptional(&kreq.Nonce, stringFromIface(req["nonce"]))
	setOptional(&kreq.SignedTxBase64, stringFromIface(req["signed_tx_base64"]))

	resp, err := c.cli.InitiatePayment(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	return map[string]interface{}{
		"base":         baseToMap(resp.GetBase()),
		"tx_id":        string(resp.GetTxId()),
		"tx_hash":      string(resp.GetTxHash()),
		"status":       resp.GetStatus().String(),
		"created_at":   resp.GetCreatedAt(),
		"confirmed_at": resp.GetConfirmedAt(),
		"failed_at":    resp.GetFailedAt(),
	}, 200, baseCode(resp.GetBase()), nil
}

func (c *KitexPaymentClient) GetPaymentRequirement(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	kreq := payment_service.NewGetPaymentRequirementRequest()
	kreq.Base = baseRequest(req)
	kreq.SkillDid = common.DID(stringFromIface(req["skill_did"]))
	setOptional(&kreq.AgentDid, stringFromIface(req["agent_did"]))
	setOptional(&kreq.SkillName, stringFromIface(req["skill_name"]))
	setOptional(&kreq.Amount, stringFromIface(req["amount"]))
	setOptional(&kreq.Price, stringFromIface(req["price"]))
	setOptional(&kreq.Message, stringFromIface(req["message"]))
	if value := stringFromIface(req["currency"]); value != "" {
		currency, err := parseCurrency(value)
		if err != nil {
			return nil, 400, 10001, err
		}
		kreq.Currency = &currency
	}
	resp, err := c.cli.GetPaymentRequirement(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	out := map[string]interface{}{
		"base":              baseToMap(resp.GetBase()),
		"already_purchased": resp.GetAlreadyPurchased(),
		"skill_did":         resp.GetSkillDid(),
		"skill_name":        resp.GetSkillName(),
		"price":             resp.GetPrice(),
		"currency":          resp.GetCurrency().String(),
		"message":           resp.GetMessage(),
		"payment_endpoint":  resp.GetPaymentEndpoint(),
	}
	if resp.GetAlreadyPurchased() {
		return out, 200, baseCode(resp.GetBase()), nil
	}
	return out, 402, 402, nil
}

func (c *KitexPaymentClient) GetPayment(ctx context.Context, txID string) (map[string]interface{}, int, int, error) {
	kreq := payment_service.NewGetPaymentStatusRequest()
	kreq.Base = common.NewBaseReq()
	kreq.TxId = common.TxId(txID)
	resp, err := c.cli.GetPaymentStatus(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	return map[string]interface{}{
		"base":         baseToMap(resp.GetBase()),
		"tx_id":        string(resp.GetTxId()),
		"status":       resp.GetStatus().String(),
		"tx_hash":      string(resp.GetTxHash()),
		"confirmed_at": resp.GetConfirmedAt(),
		"failed_at":    resp.GetFailedAt(),
	}, 200, baseCode(resp.GetBase()), nil
}

func (c *KitexPaymentClient) GetPaymentHistory(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	page := intFromReq(req, "page", 1)
	pageSize := intFromReq(req, "page_size", 20)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	kreq := payment_service.NewListPaymentHistoryRequest()
	kreq.Base = baseRequest(req)
	kreq.AgentDid = common.DID(stringFromIface(req["agent_did"]))
	kreq.Page = &common.PageRequest{Limit: int32(pageSize), Offset: int32((page - 1) * pageSize)}
	resp, err := c.cli.ListPaymentHistory(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	items := make([]map[string]interface{}, 0, len(resp.GetItems()))
	for _, item := range resp.GetItems() {
		if item == nil {
			continue
		}
		items = append(items, map[string]interface{}{
			"tx_id": item.GetTxId(), "skill_did": item.GetSkillDid(),
			"amount_minor": item.GetAmountMinor(), "currency": item.GetCurrency().String(),
			"status": item.GetStatus().String(), "created_at": item.GetCreatedAt(),
		})
	}
	total := int32(0)
	if resp.GetPage() != nil {
		total = resp.GetPage().GetTotal()
	}
	return map[string]interface{}{
		"base": baseToMap(resp.GetBase()), "items": items, "total": total,
		"page": page, "page_size": pageSize,
	}, 200, baseCode(resp.GetBase()), nil
}

func baseRequest(req map[string]interface{}) *common.BaseReq {
	base := common.NewBaseReq()
	setOptional(&base.RequestId, stringFromIface(req["request_id"]))
	setOptional(&base.TraceId, stringFromIface(req["trace_id"]))
	setOptional(&base.IdempotencyKey, stringFromIface(req["idempotency_key"]))
	return base
}

func parseAmountMinor(req map[string]interface{}) (int64, error) {
	value := stringFromIface(req["amount_minor"])
	if value == "" {
		return 0, fmt.Errorf("amount_minor is required")
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid amount_minor: %q", value)
	}
	return n, nil
}

func parseCurrency(value string) (common.Currency, error) {
	currency, err := common.CurrencyFromString(strings.ToUpper(strings.TrimSpace(value)))
	if err != nil {
		return 0, fmt.Errorf("unsupported currency: %s", value)
	}
	return currency, nil
}

func setOptional(target **string, value string) {
	if value != "" {
		*target = &value
	}
}

func baseCode(base *common.BaseResp) int {
	if base == nil {
		return 0
	}
	return int(base.GetCode())
}

var _ application.PaymentServiceClient = (*KitexPaymentClient)(nil)
