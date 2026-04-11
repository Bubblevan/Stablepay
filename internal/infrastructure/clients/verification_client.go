package clients

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/kitex/client"
	"github.com/cloudwego/kitex/pkg/retry"
	"stablepay/api-gateway/internal/application"
	"stablepay/api-gateway/kitex_gen/stablepay/common"
	"stablepay/api-gateway/kitex_gen/stablepay/verification_service"
	"stablepay/api-gateway/kitex_gen/stablepay/verification_service/verificationservice"
)

// KitexVerificationClient 通过 Kitex RPC 调用 verification-service（默认监听 8085）。
type KitexVerificationClient struct {
	cli verificationservice.Client
}

// NewKitexVerificationClient 创建客户端。destService 为 Kitex 目标服务名；hostPort 如 stablepay-verification-service:8085。
func NewKitexVerificationClient(destService, hostPort string, timeoutMs, retryCount int) (application.VerificationServiceClient, error) {
	opts := []client.Option{
		client.WithHostPorts(hostPort),
	}
	if timeoutMs > 0 {
		opts = append(opts, client.WithRPCTimeout(time.Duration(timeoutMs)*time.Millisecond))
	}
	if retryCount > 0 {
		opts = append(opts, client.WithFailureRetry(retry.NewFailurePolicy()))
	}
	cli, err := verificationservice.NewClient(destService, opts...)
	if err != nil {
		return nil, fmt.Errorf("kitex verification client: %w", err)
	}
	return &KitexVerificationClient{cli: cli}, nil
}

func (c *KitexVerificationClient) Verify(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	kreq := verification_service.NewVerifyPurchaseRequest()
	kreq.Base = &common.BaseReq{}
	kreq.AgentDid = common.DID(stringFromIface(req["agent_did"]))
	kreq.SkillDid = common.DID(stringFromIface(req["skill_did"]))
	resp, err := c.cli.VerifyPurchase(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	return verifyPurchaseToMap(resp), 200, int(resp.GetBase().GetCode()), nil
}

func (c *KitexVerificationClient) BatchVerify(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	kreq := verification_service.NewBatchVerifyPurchaseRequest()
	kreq.Base = &common.BaseReq{}
	kreq.AgentDid = common.DID(stringFromIface(req["agent_did"]))
	kreq.SkillDids = didSliceFromIface(req["skill_dids"])
	resp, err := c.cli.BatchVerifyPurchase(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	items := make([]map[string]interface{}, 0, len(resp.GetItems()))
	for _, it := range resp.GetItems() {
		if it == nil {
			continue
		}
		row := map[string]interface{}{
			"skill_did": string(it.GetSkillDid()),
			"purchased": it.GetPurchased(),
		}
		if it.IsSetPurchaseTime() {
			row["purchase_time"] = it.GetPurchaseTime()
		}
		if it.IsSetTxId() {
			row["tx_id"] = string(it.GetTxId())
		}
		items = append(items, row)
	}
	out := map[string]interface{}{
		"base":  baseToMap(resp.GetBase()),
		"items": items,
	}
	return out, 200, int(resp.GetBase().GetCode()), nil
}

func (c *KitexVerificationClient) GetProof(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	kreq := verification_service.NewGetPurchaseProofRequest()
	kreq.Base = &common.BaseReq{}
	kreq.AgentDid = common.DID(stringFromIface(req["agent_did"]))
	kreq.SkillDid = common.DID(stringFromIface(req["skill_did"]))
	resp, err := c.cli.GetPurchaseProof(ctx, kreq)
	if err != nil {
		return nil, 500, 0, err
	}
	out := map[string]interface{}{
		"base":      baseToMap(resp.GetBase()),
		"purchased": resp.GetPurchased(),
	}
	if resp.IsSetPurchaseTime() {
		out["purchase_time"] = resp.GetPurchaseTime()
	}
	if resp.IsSetTxId() {
		out["tx_id"] = string(resp.GetTxId())
	}
	if resp.IsSetAmountMinor() {
		out["amount_minor"] = resp.GetAmountMinor()
	}
	if resp.IsSetCurrency() {
		out["currency"] = int64(resp.GetCurrency())
	}
	if resp.IsSetTxHash() {
		out["tx_hash"] = string(resp.GetTxHash())
	}
	if resp.IsSetProofVersion() {
		out["proof_version"] = resp.GetProofVersion()
	}
	return out, 200, int(resp.GetBase().GetCode()), nil
}

func verifyPurchaseToMap(resp *verification_service.VerifyPurchaseResponse) map[string]interface{} {
	out := map[string]interface{}{
		"base":      baseToMap(resp.GetBase()),
		"purchased": resp.GetPurchased(),
	}
	if resp.IsSetPurchaseTime() {
		out["purchase_time"] = resp.GetPurchaseTime()
	}
	if resp.IsSetTxId() {
		out["tx_id"] = string(resp.GetTxId())
	}
	if resp.IsSetAmountMinor() {
		out["amount_minor"] = resp.GetAmountMinor()
	}
	if resp.IsSetCurrency() {
		out["currency"] = int64(resp.GetCurrency())
	}
	return out
}

func didSliceFromIface(v interface{}) []common.DID {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		out := make([]common.DID, len(t))
		for i, s := range t {
			out[i] = common.DID(s)
		}
		return out
	case []interface{}:
		out := make([]common.DID, 0, len(t))
		for _, x := range t {
			out = append(out, common.DID(fmt.Sprintf("%v", x)))
		}
		return out
	default:
		return nil
	}
}
