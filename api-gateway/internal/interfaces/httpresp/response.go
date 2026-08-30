package httpresp

import (
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"stablepay/api-gateway/internal/domain"
)

func WriteEnvelope(ctx *app.RequestContext, statusCode int, code int, message string, data interface{}) {
	requestID, _ := ctx.Get(domain.CtxRequestID)
	reqID, _ := requestID.(string)
	ctx.JSON(statusCode, domain.Envelope{
		Code:      code,
		Message:   message,
		Data:      data,
		RequestID: reqID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

func Success(ctx *app.RequestContext, data interface{}) {
	WriteEnvelope(ctx, consts.StatusOK, 0, "success", data)
}

func Fail(ctx *app.RequestContext, errDef domain.ErrorDef, data interface{}) {
	WriteEnvelope(ctx, errDef.HTTPStatus, errDef.Code, errDef.Message, data)
}
