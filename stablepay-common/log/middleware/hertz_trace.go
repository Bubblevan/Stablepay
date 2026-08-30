package middleware

import (
	"context"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// HertzTrace 创建 Hertz HTTP 链路追踪中间件
// 使用 OpenTelemetry 标准从 HTTP 请求 headers 中提取 trace context
// 支持 W3C TraceContext 标准 (traceparent header)，确保与 RPC 调用链的 trace ID 一致
//
// 使用示例:
//
//	import "code.wenfu.cn/stablepay/stablepay-common/log/middleware"
//
//	h := server.Default()
//	h.Use(middleware.HertzTrace("service-name"))
//
// 参数:
//   - serviceName: 服务名称，用于创建 tracer
//
// 返回:
//   - app.HandlerFunc: Hertz 中间件函数
func HertzTrace(serviceName string) app.HandlerFunc {
	tracerName := serviceName + "-http"

	return func(c context.Context, ctx *app.RequestContext) {
		// 将 Hertz 的 headers 转换为 http.Header，以便使用 OpenTelemetry propagation
		httpHeaders := make(http.Header)
		ctx.Request.Header.VisitAll(func(key, value []byte) {
			httpHeaders.Set(string(key), string(value))
		})

		// 使用 OpenTelemetry 标准 propagation 从 headers 中提取 trace context
		// 这会提取 traceparent header（W3C TraceContext 标准）
		traceCtx := otel.GetTextMapPropagator().Extract(c, propagation.HeaderCarrier(httpHeaders))

		// 检查是否成功提取到有效的 trace
		span := trace.SpanFromContext(traceCtx)
		if !span.SpanContext().IsValid() {
			// 如果没有提取到有效的 trace，创建一个新的 root span
			tracer := otel.Tracer(tracerName)
			var newSpan trace.Span
			traceCtx, newSpan = tracer.Start(c, "http-request")
			// 使用 defer 确保 span 会被正确结束
			defer newSpan.End()
		}

		// 将带有 trace 信息的 context 传递下去
		ctx.Next(traceCtx)
	}
}
