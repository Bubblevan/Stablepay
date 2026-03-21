package http

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"stablepay/api-gateway/internal/application"
	"stablepay/api-gateway/internal/domain"
)

type Handler struct {
	app *application.Service
}

func NewHandler(appService *application.Service) *Handler {
	return &Handler{app: appService}
}

func (h *Handler) Proxy(routeName string) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		req := map[string]interface{}{}
		for _, p := range ctx.Params {
			req[string(p.Key)] = string(p.Value)
		}
		for _, kv := range strings.Split(string(ctx.URI().QueryString()), "&") {
			if kv == "" {
				continue
			}
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) == 2 {
				req[parts[0]] = parts[1]
			}
		}
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

		req["request_id"] = getCtxString(ctx, domain.CtxRequestID)
		req["trace_id"] = getCtxString(ctx, domain.CtxTraceID)
		req["did"] = getCtxString(ctx, domain.CtxDID)
		req["signature"] = getCtxString(ctx, domain.CtxSignature)
		req["timestamp"] = getCtxString(ctx, domain.CtxTimestamp)
		req["nonce"] = getCtxString(ctx, domain.CtxNonce)
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
		failFromCode(ctx, httpStatus, code, fmt.Errorf("downstream status: %d", httpStatus))
	}
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
