package middleware

import (
	"context"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stablepay/api-gateway/internal/domain"
)

func PolicyInjector(policy domain.RoutePolicy) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		ctx.Set(domain.CtxRouteName, policy.Name)
		ctx.Set("ctx_policy", policy)
		ctx.Next(c)
	}
}

func CurrentPolicy(ctx *app.RequestContext) (domain.RoutePolicy, bool) {
	value, ok := ctx.Get("ctx_policy")
	if !ok {
		return domain.RoutePolicy{}, false
	}
	policy, ok := value.(domain.RoutePolicy)
	return policy, ok
}
