package middleware

import (
	"context"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/google/uuid"
	"github.com/stablepay/api-gateway/internal/domain"
)

func RequestMeta() app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		requestID := string(ctx.GetHeader("X-Request-Id"))
		if requestID == "" {
			requestID = uuid.NewString()
		}
		traceID := string(ctx.GetHeader("X-Trace-Id"))
		if traceID == "" {
			traceID = requestID
		}

		ctx.Set(domain.CtxRequestID, requestID)
		ctx.Set(domain.CtxTraceID, traceID)
		ctx.Response.Header.Set("X-Request-Id", requestID)
		ctx.Response.Header.Set("X-Trace-Id", traceID)
		ctx.Next(c)
	}
}
