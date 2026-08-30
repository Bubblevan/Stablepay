package middleware

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stablepay/api-gateway/internal/domain"
	"github.com/stablepay/api-gateway/internal/infrastructure/observability"
)

func AccessLog(logger *observability.Logger) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		start := time.Now()
		ctx.Next(c)
		requestID, _ := ctx.Get(domain.CtxRequestID)
		traceID, _ := ctx.Get(domain.CtxTraceID)
		did, _ := ctx.Get(domain.CtxDID)
		logger.Info("access", map[string]interface{}{
			"method":     string(ctx.Request.Method()),
			"path":       string(ctx.Path()),
			"query":      string(ctx.URI().QueryString()),
			"status":     ctx.Response.StatusCode(),
			"latency_ms": time.Since(start).Milliseconds(),
			"request_id": requestID,
			"trace_id":   traceID,
			"did":        did,
		})
	}
}
