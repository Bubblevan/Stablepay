package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/stablepay/api-gateway/internal/application"
	"github.com/stablepay/api-gateway/internal/domain"
)

type Handler struct {
	app *application.Service
}

func NewHandler(appService *application.Service) *Handler {
	return &Handler{app: appService}
}

func (h *Handler) Proxy(routeName string, machinePay ...*domain.MachinePayRoute) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		req := map[string]interface{}{}
		for _, p := range ctx.Params {
			req[string(p.Key)] = string(p.Value)
		}
		mergeDecodedQuery(req, string(ctx.URI().QueryString()))
		if len(ctx.Request.Body()) > 0 {
			var body map[string]interface{}
			if err := json.Unmarshal(ctx.Request.Body(), &body); err != nil {
				Fail(ctx, domain.ErrInvalidParameters, map[string]interface{}{"error": err.Error()})
				return
			}
			for key, value := range body {
				req[key] = value
			}
		}

		setIfAbsent(req, "request_id", getCtxString(ctx, domain.CtxRequestID))
		req["trace_id"] = getCtxString(ctx, domain.CtxTraceID)
		setIfAbsent(req, "idempotency_key", string(ctx.GetHeader("X-Idempotency-Key")))
		setIfAbsent(req, "did", getCtxString(ctx, domain.CtxDID))
		setIfAbsent(req, "signature", getCtxString(ctx, domain.CtxSignature))
		setIfAbsent(req, "timestamp", getCtxString(ctx, domain.CtxTimestamp))
		setIfAbsent(req, "nonce", getCtxString(ctx, domain.CtxNonce))

		if len(machinePay) > 0 && machinePay[0] != nil && machinePay[0].Enabled {
			policy := machinePay[0]
			if strings.TrimSpace(stringFromMap(req, "idempotency_key")) == "" {
				Fail(ctx, domain.ErrInvalidParameters, map[string]interface{}{"detail": "X-Idempotency-Key is required for Machine Pay resources"})
				return
			}
			req["machinepay_resource_id"] = policy.ResourceID
			req["machinepay_goods_name"] = policy.GoodsName
			req["machinepay_amount_minor"] = policy.AmountMinor
			req["payment_proof_header"] = string(ctx.GetHeader("Payment-Proof"))
			access, status, code, err := h.app.MachinePayAccess(c, req)
			if err != nil {
				failFromCode(ctx, status, code, err)
				return
			}
			authorized, _ := access["authorized"].(bool)
			if !authorized {
				link := stringFromMap(access, "payment_link")
				if link == "" {
					Fail(ctx, domain.ErrInternalServer, map[string]interface{}{"detail": "Machine Pay challenge is missing its payment link"})
					return
				}
				ctx.Response.Header.Set("amtPaymentLink", link)
				ctx.JSON(consts.StatusPaymentRequired, map[string]interface{}{
					"error": "Payment Required", "order_id": access["order_id"], "resource_id": access["resource_id"],
					"amount_minor": access["amount_minor"], "currency": "CNY",
				})
				return
			}
		}
		if price, ok := req["price"]; ok {
			if _, exists := req["amount"]; !exists {
				req["amount"] = price
			}
		}

		data, httpStatus, code, err := h.app.Dispatch(c, routeName, req)
		if err != nil {
			failFromCode(ctx, httpStatus, code, err)
			return
		}
		if httpStatus == consts.StatusOK {
			Success(ctx, data)
			return
		}
		if httpStatus == consts.StatusPaymentRequired {
			WriteEnvelope(ctx, httpStatus, code, "Payment Required", data)
			return
		}
		// payment-service 等业务错误会把可读信息放在 JSON body的 message 里；不要只报「downstream status:400」
		detailErr := fmt.Errorf("downstream http %d", httpStatus)
		if data != nil {
			if msg, ok := data["message"].(string); ok && msg != "" {
				detailErr = fmt.Errorf("%s (downstream http %d)", msg, httpStatus)
			}
		}
		failFromCode(ctx, httpStatus, code, detailErr)
	}
}

func stringFromMap(in map[string]interface{}, key string) string {
	value, ok := in[key]
	if !ok || value == nil {
		return ""
	}
	if str, ok := value.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", value)
}

// NotifyAlipay is the sole provider callback endpoint. Alipay RSA2 verification
// happens in alipay-adapter through Payment Service Kitex; the gateway only
// parses the bounded form and returns Alipay's required acknowledgement.
func (h *Handler) NotifyAlipay(c context.Context, ctx *app.RequestContext) {
	if len(ctx.Request.Body()) > 64<<10 {
		ctx.String(consts.StatusRequestEntityTooLarge, "failure")
		return
	}
	values, err := url.ParseQuery(string(ctx.Request.Body()))
	if err != nil {
		ctx.String(consts.StatusBadRequest, "failure")
		return
	}
	parameters := make(map[string]string, len(values))
	for key, entries := range values {
		if len(entries) != 1 {
			ctx.String(consts.StatusBadRequest, "failure")
			return
		}
		parameters[key] = entries[0]
	}
	if err := h.app.NotifyAlipay(c, parameters); err != nil {
		ctx.String(consts.StatusBadRequest, "failure")
		return
	}
	ctx.Response.Header.Set("Content-Type", "text/plain; charset=utf-8")
	ctx.String(consts.StatusOK, "success")
}

func getCtxString(ctx *app.RequestContext, key string) string {
	value, exists := ctx.Get(key)
	if !exists {
		return ""
	}
	if str, ok := value.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", value)
}

func setIfAbsent(req map[string]interface{}, key, value string) {
	if value == "" {
		return
	}
	if _, exists := req[key]; exists {
		return
	}
	req[key] = value
}

func mergeDecodedQuery(req map[string]interface{}, rawQuery string) {
	for _, kv := range strings.Split(rawQuery, "&") {
		if kv == "" {
			continue
		}
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key, err := url.QueryUnescape(parts[0])
		if err != nil || key == "" {
			continue
		}
		value, err := url.QueryUnescape(parts[1])
		if err != nil {
			continue
		}
		req[key] = value
	}
}

func failFromCode(ctx *app.RequestContext, httpStatus int, code int, err error) {
	switch code {
	case 10001:
		Fail(ctx, domain.ErrInvalidParameters, map[string]interface{}{"detail": err.Error()})
	case 10002:
		Fail(ctx, domain.ErrResourceNotFound, map[string]interface{}{"detail": err.Error()})
	case 10003:
		Fail(ctx, domain.ErrPermissionDenied, map[string]interface{}{"detail": err.Error()})
	case 10004:
		Fail(ctx, domain.ErrSignatureVerification, map[string]interface{}{"detail": err.Error()})
	case 20001:
		Fail(ctx, domain.ErrInsufficientBalance, map[string]interface{}{"detail": err.Error()})
	case 20002:
		Fail(ctx, domain.ErrPaymentAlreadyExists, map[string]interface{}{"detail": err.Error()})
	case 20003:
		Fail(ctx, domain.ErrBlockchainNetwork, map[string]interface{}{"detail": err.Error()})
	case 20004:
		Fail(ctx, domain.ErrGasSubsidyFailed, map[string]interface{}{"detail": err.Error()})
	case 30002:
		Fail(ctx, domain.ErrServiceUnavailable, map[string]interface{}{"detail": err.Error()})
	case 30003:
		Fail(ctx, domain.ErrDatabaseConnection, map[string]interface{}{"detail": err.Error()})
	case 30004:
		Fail(ctx, domain.ErrRateLimitExceeded, map[string]interface{}{"detail": err.Error()})
	default:
		if httpStatus == consts.StatusNotFound {
			Fail(ctx, domain.ErrResourceNotFound, map[string]interface{}{"detail": err.Error()})
			return
		}
		Fail(ctx, domain.ErrInternalServer, map[string]interface{}{"detail": err.Error()})
	}
}
