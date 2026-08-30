package http

import (
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stablepay/api-gateway/internal/domain"
	"github.com/stablepay/api-gateway/internal/adapter/httpresp"
)

func WriteEnvelope(ctx *app.RequestContext, statusCode int, code int, message string, data interface{}) {
	httpresp.WriteEnvelope(ctx, statusCode, code, message, data)
}

func Success(ctx *app.RequestContext, data interface{}) {
	httpresp.Success(ctx, data)
}

func Fail(ctx *app.RequestContext, errDef domain.ErrorDef, data interface{}) {
	httpresp.Fail(ctx, errDef, data)
}
