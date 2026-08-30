package http

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"stablepay/api-gateway/internal/domain"
	"stablepay/api-gateway/internal/interfaces/http/middleware"
)

func RegisterRoutes(
	h *server.Hertz,
	handler *Handler,
	routes []domain.RoutePolicy,
	readyState *middleware.ReadyState,
	authVerify app.HandlerFunc,
	replayProtection app.HandlerFunc,
	rateLimit app.HandlerFunc,
) {
	h.GET("/healthz", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]any{"status": "ok"})
	})
	h.HEAD("/healthz", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]any{"status": "ok"})
	})
	h.GET("/readyz", func(ctx context.Context, c *app.RequestContext) {
		if !readyState.IsReady() {
			c.JSON(consts.StatusServiceUnavailable, map[string]any{"status": "not_ready"})
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"status": "ready"})
	})

	for _, policy := range routes {
		routeHandler := handler.Proxy(policy.Name)
		chain := []app.HandlerFunc{
			middleware.PolicyInjector(policy),
			authVerify,
			replayProtection,
			rateLimit,
			routeHandler,
		}
		switch strings.ToUpper(policy.Method) {
		case consts.MethodGet:
			h.GET(policy.Path, chain...)
		case consts.MethodPost:
			h.POST(policy.Path, chain...)
		case consts.MethodPut:
			h.PUT(policy.Path, chain...)
		case consts.MethodDelete:
			h.DELETE(policy.Path, chain...)
		}
	}
}
