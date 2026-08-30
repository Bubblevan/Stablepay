// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/stablepay/merchant-server/internal/adapter/dto"
	appSvc "github.com/stablepay/merchant-server/internal/application/service"
	domainSvc "github.com/stablepay/merchant-server/internal/domain/service"
)

// ProductHandler translates HTTP requests into product application use cases.
//
// Handler methods intentionally do not contain product payment rules. They only
// parse path/query/header values, call ProductAppService, and translate use-case
// results into HTTP status codes, headers, and JSON bodies.
type ProductHandler struct {
	productAppService *appSvc.ProductAppService
	sellerAddress     string
	facilitatorURL    string
}

// NewProductHandler creates a product HTTP adapter.
func NewProductHandler(productAppService *appSvc.ProductAppService, sellerAddress, facilitatorURL string) *ProductHandler {
	return &ProductHandler{
		productAppService: productAppService,
		sellerAddress:     sellerAddress,
		facilitatorURL:    facilitatorURL,
	}
}

// ListProducts handles GET /api/v1/products.
func (h *ProductHandler) ListProducts(ctx context.Context, c *app.RequestContext) {
	if h.productAppService == nil {
		writeError(c, consts.StatusInternalServerError, "PRODUCT_APP_SERVICE_NOT_READY", "product application service is not ready")
		return
	}

	page, size := parsePagination(c)
	items, total, err := h.productAppService.ListProducts(ctx, page, size)
	if err != nil {
		writeError(c, consts.StatusInternalServerError, "LIST_PRODUCTS_FAILED", err.Error())
		return
	}

	products := make([]dto.ProductItem, 0, len(items))
	for _, item := range items {
		products = append(products, mapProductItem(item))
	}

	c.JSON(consts.StatusOK, dto.ProductListResp{
		BaseResponse: dto.Success("products listed"),
		Products:     products,
		Pagination: &dto.Pagination{
			Total: total,
			Page:  page,
			Size:  size,
		},
	})
}

// GetProduct handles GET /api/v1/products/:id.
func (h *ProductHandler) GetProduct(ctx context.Context, c *app.RequestContext) {
	if h.productAppService == nil {
		writeError(c, consts.StatusInternalServerError, "PRODUCT_APP_SERVICE_NOT_READY", "product application service is not ready")
		return
	}

	skuID := strings.TrimSpace(c.Param("id"))
	if skuID == "" {
		writeError(c, consts.StatusBadRequest, "MISSING_PRODUCT_ID", "product id is required")
		return
	}

	item, err := h.productAppService.GetProductDetail(ctx, skuID)
	if err != nil {
		writeAppError(c, err)
		return
	}

	product := mapProductItem(item)
	c.JSON(consts.StatusOK, dto.ProductDetailResp{
		BaseResponse: dto.Success("product found"),
		Product:      &product,
	})
}

// ExecutePurchase handles GET /api/v1/products/:id/execute.
//
// x402 v2/v1 dual-header strategy:
//   - v2 header  "PAYMENT-REQUIRED" — canonical x402 v2 (base64 JSON)
//   - v1 header  "Payment-Required" — backward compat (base64 JSON)
//   - JSON body  v2 format
func (h *ProductHandler) ExecutePurchase(ctx context.Context, c *app.RequestContext) {
	if h.productAppService == nil {
		writeError(c, consts.StatusInternalServerError, "PRODUCT_APP_SERVICE_NOT_READY", "product application service is not ready")
		return
	}

	skuID := strings.TrimSpace(c.Param("id"))
	if skuID == "" {
		writeError(c, consts.StatusBadRequest, "MISSING_PRODUCT_ID", "product id is required")
		return
	}

	agentDID := strings.TrimSpace(c.Query("agent_did"))
	if agentDID == "" {
		writeError(c, consts.StatusBadRequest, "MISSING_AGENT_DID", "agent_did query parameter is required")
		return
	}

	// Try x402 v2 header first, fall back to v1 "Payment-Signature"
	paymentSignature := strings.TrimSpace(string(c.GetHeader(domainSvc.X402PaymentSignatureHeader)))
	if paymentSignature == "" {
		paymentSignature = strings.TrimSpace(string(c.GetHeader("Payment-Signature")))
	}

	result, err := h.productAppService.ExecutePurchase(ctx, appSvc.ExecutePurchaseCommand{
		SKUID:            skuID,
		AgentDID:         agentDID,
		PaymentSignature: paymentSignature,
	})
	if err != nil {
		writeAppError(c, err)
		return
	}
	if result == nil {
		writeError(c, consts.StatusInternalServerError, "EMPTY_PURCHASE_RESULT", "execute purchase returned empty result")
		return
	}

	if !result.Purchased {
		writePaymentRequired(c, result, h)
		return
	}

	writePurchaseSuccess(c, result)
}

func writePaymentRequired(c *app.RequestContext, result *appSvc.ExecutePurchaseResult, h *ProductHandler) {
	if result.PaymentRequired == nil {
		writeError(c, consts.StatusInternalServerError, "PAYMENT_REQUIRED_MISSING", "payment required result is missing")
		return
	}

	// === x402 v2 header ===
	v2HeaderValue, err := domainSvc.EncodePaymentRequiredHeader(result.PaymentRequired)
	if err != nil {
		writeError(c, consts.StatusInternalServerError, "PAYMENT_REQUIRED_ENCODE_FAILED", err.Error())
		return
	}
	c.Header(domainSvc.X402PaymentRequiredHeader, v2HeaderValue)

	// === x402 v1 header (backward compat) ===
	if len(result.PaymentRequired.Accepts) > 0 {
		a := result.PaymentRequired.Accepts[0]
		v1Accept := dto.X402AcceptV1{
			Scheme:            a.Scheme,
			Network:           a.Network,
			MaxAmountRequired: a.Amount,
			PayTo:             a.PayTo,
			Asset:             a.Asset,
			Description:       result.PaymentRequired.Resource.Description,
			Resource:          result.PaymentRequired.Resource.URL,
			MaxTimeoutSeconds: a.MaxTimeoutSeconds,
			Extra:             a.Extra,
		}
		v1Resp := dto.PaymentRequiredRespV1{
			X402Version: 1,
			Error:       "Payment Required",
			Accepts:     []dto.X402AcceptV1{v1Accept},
		}
		if v1JSON, err := json.Marshal(v1Resp); err == nil {
			c.Header(domainSvc.X402PaymentRequiredHeaderV1, string(v1JSON))
		}
	}

	c.Header("Accept-Payment", "x402, stablepay-v1")
	c.JSON(consts.StatusPaymentRequired, mapPaymentRequired(result.PaymentRequired))
}

func writePurchaseSuccess(c *app.RequestContext, result *appSvc.ExecutePurchaseResult) {
	if result.TxID != "" || result.TxHash != "" {
		payload, err := json.Marshal(map[string]string{
			"tx_id":   result.TxID,
			"tx_hash": result.TxHash,
		})
		if err == nil {
			c.Header(domainSvc.X402PaymentResponseHeader, string(payload))
		}
	}

	product := mapProductItem(result.Product)
	c.JSON(consts.StatusOK, dto.PurchaseExecuteResp{
		BaseResponse:  dto.Success("paid content unlocked"),
		Product:       &product,
		MerchantProof: result.MerchantProof,
		GatewayProof:  result.GatewayProof,
		TxID:          result.TxID,
		TxHash:        result.TxHash,
		Content:       result.Content,
	})
}

func mapProductItem(item *appSvc.ProductListItem) dto.ProductItem {
	if item == nil {
		return dto.ProductItem{}
	}
	return dto.ProductItem{
		ID:          item.ID,
		SKUID:       item.SKUID,
		Title:       item.Title,
		Description: item.Description,
		Price:       item.Price,
		Currency:    item.Currency,
		Author:      item.Author,
		Tags:        append([]string(nil), item.Tags...),
		Status:      item.Status,
		ImageURL:    item.ImageURL,
	}
}

func mapPaymentRequired(required *domainSvc.X402PaymentRequired) dto.PaymentRequiredResp {
	if required == nil {
		return dto.PaymentRequiredResp{}
	}

	accepts := make([]dto.X402PaymentRequirement, 0, len(required.Accepts))
	for _, item := range required.Accepts {
		accepts = append(accepts, dto.X402PaymentRequirement{
			Scheme:            item.Scheme,
			Network:           item.Network,
			Amount:            item.Amount,
			Asset:             item.Asset,
			PayTo:             item.PayTo,
			MaxTimeoutSeconds: item.MaxTimeoutSeconds,
			Extra:             item.Extra,
		})
	}

	return dto.PaymentRequiredResp{
		X402Version: required.X402Version,
		Error:       required.Error,
		Resource: dto.X402ResourceInfo{
			URL:         required.Resource.URL,
			Description: required.Resource.Description,
			MimeType:    required.Resource.MimeType,
			ServiceName: required.Resource.ServiceName,
			Tags:        append([]string(nil), required.Resource.Tags...),
			IconURL:     required.Resource.IconURL,
		},
		Accepts:    accepts,
		Extensions: required.Extensions,
	}
}

func parsePagination(c *app.RequestContext) (int, int) {
	page := parsePositiveInt(c.DefaultQuery("page", strconv.Itoa(dto.DefaultPage)), dto.DefaultPage)
	size := parsePositiveInt(c.DefaultQuery("size", strconv.Itoa(dto.DefaultSize)), dto.DefaultSize)
	if size > dto.MaxSize {
		size = dto.MaxSize
	}
	return page, size
}

func parsePositiveInt(raw string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

func writeAppError(c *app.RequestContext, err error) {
	msg := err.Error()
	status := consts.StatusInternalServerError
	code := "INTERNAL_ERROR"

	switch {
	case strings.Contains(msg, "is required"):
		status = consts.StatusBadRequest
		code = "BAD_REQUEST"
	case strings.Contains(msg, "not found"):
		status = consts.StatusNotFound
		code = "NOT_FOUND"
	case strings.Contains(msg, "not purchasable"):
		status = consts.StatusBadRequest
		code = "PRODUCT_NOT_PURCHASABLE"
	}

	writeError(c, status, code, msg)
}

func writeError(c *app.RequestContext, status int, code, message string) {
	c.JSON(status, dto.Error(code, message))
}

func nowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}
