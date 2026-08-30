package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stablepay/api-gateway/internal/domain"
	"github.com/stablepay/api-gateway/internal/infrastructure/ratelimit"
	"github.com/stablepay/api-gateway/internal/adapter/httpresp"
)

func RateLimit(limiter ratelimit.Limiter) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		policy, ok := CurrentPolicy(ctx)
		if !ok {
			ctx.Next(c)
			return
		}

		ip := ctx.ClientIP()
		did := getCtxString(ctx, domain.CtxDID)
		routeName := policy.Name
		window := time.Minute

		if policy.RateLimits.IPPerMinute > 0 {
			allowed, err := limiter.Allow(c, fmt.Sprintf("ip:%s", ip), policy.RateLimits.IPPerMinute, window)
			if err != nil || !allowed {
				httpresp.Fail(ctx, domain.ErrRateLimitExceeded, map[string]interface{}{"dimension": "ip"})
				ctx.Abort()
				return
			}
		}
		if did != "" && policy.RateLimits.DIDPerMinute > 0 {
			allowed, err := limiter.Allow(c, fmt.Sprintf("did:%s", did), policy.RateLimits.DIDPerMinute, window)
			if err != nil || !allowed {
				httpresp.Fail(ctx, domain.ErrRateLimitExceeded, map[string]interface{}{"dimension": "did"})
				ctx.Abort()
				return
			}
		}
		if policy.RateLimits.RoutePerMinute > 0 {
			allowed, err := limiter.Allow(c, fmt.Sprintf("route:%s", routeName), policy.RateLimits.RoutePerMinute, window)
			if err != nil || !allowed {
				httpresp.Fail(ctx, domain.ErrRateLimitExceeded, map[string]interface{}{"dimension": "route"})
				ctx.Abort()
				return
			}
		}
		ctx.Next(c)
	}
}
