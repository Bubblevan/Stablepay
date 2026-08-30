package rpc

import (
	"context"

	"github.com/stablepay/verification-service/internal/application"
	"github.com/stablepay/verification-service/kitex_gen/stablepay/common"
	"github.com/stablepay/verification-service/kitex_gen/stablepay/verification_service"
)

type Handler struct{ service *application.Service }

func NewHandler(service *application.Service) *Handler { return &Handler{service: service} }

func base(code int32, message string) *common.BaseResp {
	return &common.BaseResp{Code: code, Message: message}
}

func (h *Handler) VerifyPurchase(ctx context.Context, req *verification_service.VerifyPurchaseRequest) (*verification_service.VerifyPurchaseResponse, error) {
	resp := verification_service.NewVerifyPurchaseResponse()
	if req == nil {
		resp.Base = base(application.CodeInvalidParameters, "request is required")
		return resp, nil
	}
	result, err := h.service.VerifyPurchase(ctx, string(req.AgentDid), string(req.SkillDid))
	if err != nil {
		resp.Base = base(application.CodeInternalError, err.Error())
		return resp, nil
	}
	fillPurchaseResponse(resp, result)
	return resp, nil
}

func (h *Handler) BatchVerifyPurchase(ctx context.Context, req *verification_service.BatchVerifyPurchaseRequest) (*verification_service.BatchVerifyPurchaseResponse, error) {
	resp := verification_service.NewBatchVerifyPurchaseResponse()
	if req == nil {
		resp.Base = base(application.CodeInvalidParameters, "request is required")
		return resp, nil
	}
	items, code, message, err := h.service.BatchVerifyPurchase(ctx, string(req.AgentDid), didStrings(req.SkillDids))
	if err != nil {
		resp.Base = base(application.CodeInternalError, err.Error())
		return resp, nil
	}
	resp.Base = base(code, message)
	resp.Items = make([]*verification_service.BatchVerifyItem, 0, len(items))
	for i, item := range items {
		row := verification_service.NewBatchVerifyItem()
		row.SkillDid = req.SkillDids[i]
		row.Purchased = item.Purchased
		if item.PurchaseTime != nil {
			value := item.PurchaseTime.Format("2006-01-02T15:04:05Z07:00")
			row.PurchaseTime = &value
		}
		if item.TxID != "" {
			value := common.TxId(item.TxID)
			row.TxId = &value
		}
		resp.Items = append(resp.Items, row)
	}
	return resp, nil
}

func (h *Handler) GetPurchaseProof(ctx context.Context, req *verification_service.GetPurchaseProofRequest) (*verification_service.GetPurchaseProofResponse, error) {
	resp := verification_service.NewGetPurchaseProofResponse()
	if req == nil {
		resp.Base = base(application.CodeInvalidParameters, "request is required")
		return resp, nil
	}
	result, err := h.service.GetPurchaseProof(ctx, string(req.AgentDid), string(req.SkillDid))
	if err != nil {
		resp.Base = base(application.CodeInternalError, err.Error())
		return resp, nil
	}
	resp.Base = base(result.Code, result.Message)
	resp.Purchased = result.Purchased
	if result.PurchaseTime != nil {
		value := result.PurchaseTime.Format("2006-01-02T15:04:05Z07:00")
		resp.PurchaseTime = &value
	}
	if result.TxID != "" {
		value := common.TxId(result.TxID)
		resp.TxId = &value
	}
	if result.AmountMinor != nil {
		resp.AmountMinor = result.AmountMinor
	}
	if result.Currency != nil {
		value := common.Currency(*result.Currency)
		resp.Currency = &value
	}
	if result.TxHash != "" {
		value := common.TxHash(result.TxHash)
		resp.TxHash = &value
	}
	if result.Purchased {
		version := "v1"
		resp.ProofVersion = &version
	}
	return resp, nil
}

func fillPurchaseResponse(resp *verification_service.VerifyPurchaseResponse, result application.PurchaseResult) {
	resp.Base = base(result.Code, result.Message)
	resp.Purchased = result.Purchased
	if result.PurchaseTime != nil {
		value := result.PurchaseTime.Format("2006-01-02T15:04:05Z07:00")
		resp.PurchaseTime = &value
	}
	if result.TxID != "" {
		value := common.TxId(result.TxID)
		resp.TxId = &value
	}
	if result.AmountMinor != nil {
		resp.AmountMinor = result.AmountMinor
	}
	if result.Currency != nil {
		value := common.Currency(*result.Currency)
		resp.Currency = &value
	}
}

func didStrings(values []common.DID) []string {
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = string(value)
	}
	return result
}
