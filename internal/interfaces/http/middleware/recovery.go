package middleware

import (
	"context"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"
	"stablepay/api-gateway/internal/domain"
	"stablepay/api-gateway/internal/interfaces/httpresp"
)

func Recovery() app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		defer func() {
			if rec := recover(); rec != nil {
				httpresp.Fail(ctx, domain.ErrInternalServer, map[string]interface{}{
					"detail": fmt.Sprintf("%v", rec),
				})
				ctx.Abort()
			}
		}()
		ctx.Next(c)
	}
}
