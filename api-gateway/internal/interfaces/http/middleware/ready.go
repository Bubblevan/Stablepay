package middleware

import (
	"context"
	"sync/atomic"

	"github.com/cloudwego/hertz/pkg/app"
	"stablepay/api-gateway/internal/domain"
	"stablepay/api-gateway/internal/interfaces/httpresp"
)

type ReadyState struct {
	ready atomic.Bool
}

func NewReadyState() *ReadyState {
	state := &ReadyState{}
	state.ready.Store(true)
	return state
}

func (s *ReadyState) SetReady(value bool) {
	s.ready.Store(value)
}

func (s *ReadyState) IsReady() bool {
	return s.ready.Load()
}

func ReadinessGuard(state *ReadyState) app.HandlerFunc {
	return func(c context.Context, ctx *app.RequestContext) {
		path := string(ctx.Path())
		if path == "/readyz" || path == "/healthz" {
			ctx.Next(c)
			return
		}
		if !state.IsReady() {
			httpresp.Fail(ctx, domain.ErrServiceUnavailable, map[string]interface{}{
				"detail": "service not ready",
			})
			ctx.Abort()
			return
		}
		ctx.Next(c)
	}
}
