package log

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/bytedance/gopkg/cloud/metainfo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// ContextKey 上下文键类型
type ContextKey string

const (
	// RequestIDKey 请求ID键
	RequestIDKey ContextKey = "request_id"
	// UserIDKey 用户ID键
	UserIDKey ContextKey = "user_id"
	// TraceIDKey trace ID键
	TraceIDKey ContextKey = "trace_id"
	// SpanIDKey span ID键
	SpanIDKey ContextKey = "span_id"
	// ClientNameKey 客户端名称键
	ClientNameKey ContextKey = "client_name"
	// MethodKey 方法名键
	MethodKey ContextKey = "method"
	// TraceContextKey 完整的trace context的key，用于缓存
	TraceContextKey ContextKey = "trace_context"
)

// 注意：全局缓存方案已被移除，因为存在内存泄漏和并发问题
// 现在使用context中的缓存机制

// LoggerContextKey 日志器上下文键类型
type LoggerContextKey string

const (
	// LoggerKey 日志器键
	LoggerKey LoggerContextKey = "logger"
)

// WithRequestID 设置请求ID到上下文
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// GetRequestID 从上下文获取请求ID
func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(RequestIDKey).(string); ok {
		return requestID
	}
	return ""
}

// GenerateRequestID 生成请求ID（如果上下文中没有的话）
func GenerateRequestID(ctx context.Context) string {
	// 先尝试从上下文获取现有的请求ID
	if existingID := GetRequestID(ctx); existingID != "" {
		return existingID
	}

	// 如果没有，生成一个新的请求ID
	// 格式：req_时间戳_随机数
	timestamp := time.Now().Format("20060102150405")
	random := fmt.Sprintf("%04d", time.Now().Nanosecond()%10000)
	return fmt.Sprintf("req_%s_%s", timestamp, random)
}

// WithUserID 设置用户ID到上下文
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

// GetUserID 从上下文获取用户ID
func GetUserID(ctx context.Context) string {
	if userID, ok := ctx.Value(UserIDKey).(string); ok {
		return userID
	}
	return ""
}

// WithLogger 设置日志器到上下文
func WithLogger(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, LoggerKey, logger)
}

// GetLoggerFromContext 从上下文获取日志器
func GetLoggerFromContext(ctx context.Context) *Logger {
	if logger, ok := ctx.Value(LoggerKey).(*Logger); ok {
		return logger
	}
	return nil
}

// TraceContext 包含 traceID 和 spanID 的结构体
type TraceContext struct {
	TraceID string
	SpanID  string
	Valid   bool
}

// GetTraceContext 从上下文获取追踪信息（封装了 OpenTelemetry 的获取逻辑）
// 如果没有有效的追踪信息，会自动生成新的 traceID 和 spanID
// 为了避免重复生成，会检查context中是否已经缓存了trace信息
// 注意：这个方法不能修改context，如果需要保存trace信息到context中，请使用GetTraceContextWithCache
func GetTraceContext(ctx context.Context) TraceContext {
	// 首先检查context中是否已经缓存了trace信息
	if cachedTrace := ctx.Value(TraceContextKey); cachedTrace != nil {
		if traceCtx, ok := cachedTrace.(TraceContext); ok && traceCtx.Valid {
			return traceCtx
		}
	}

	// 检查context中是否已经存储了trace ID和span ID
	if traceID, ok := ctx.Value(TraceIDKey).(string); ok && traceID != "" {
		spanID := ""
		if spanIDValue, ok := ctx.Value(SpanIDKey).(string); ok {
			spanID = spanIDValue
		}
		return TraceContext{
			TraceID: traceID,
			SpanID:  spanID,
			Valid:   true,
		}
	}

	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		// 如果没有有效的追踪信息，自动生成新的
		// 使用全局 TracerProvider 创建一个新的 span
		tracer := otel.Tracer("auto-generate")
		_, newSpan := tracer.Start(ctx, "auto-generated")

		// 立即结束span，因为我们只需要trace信息
		// 这样可以避免内存泄漏
		newSpan.End()

		// 检查生成的span是否有效
		spanContext := newSpan.SpanContext()
		if spanContext.IsValid() {
			traceCtx := TraceContext{
				TraceID: spanContext.TraceID().String(),
				SpanID:  spanContext.SpanID().String(),
				Valid:   true,
			}
			// 注意：这里不能直接修改传入的ctx，因为context是不可变的
			// 调用者需要自己处理返回的context
			return traceCtx
		}

		// 如果生成的span无效（比如使用了NoopTracerProvider），
		// 则使用随机生成的方式创建trace信息
		return generateFallbackTraceContext()
	}

	traceCtx := TraceContext{
		TraceID: span.SpanContext().TraceID().String(),
		SpanID:  span.SpanContext().SpanID().String(),
		Valid:   true,
	}
	return traceCtx
}

// GetTraceContextWithCache 从上下文获取追踪信息，并将结果缓存到context中
// 返回更新后的context和trace信息
func GetTraceContextWithCache(ctx context.Context) (context.Context, TraceContext) {
	// 首先检查context中是否已经缓存了trace信息
	if cachedTrace := ctx.Value(TraceContextKey); cachedTrace != nil {
		if traceCtx, ok := cachedTrace.(TraceContext); ok && traceCtx.Valid {
			return ctx, traceCtx
		}
	}

	// 检查context中是否已经存储了trace ID和span ID
	if traceID, ok := ctx.Value(TraceIDKey).(string); ok && traceID != "" {
		spanID := ""
		if spanIDValue, ok := ctx.Value(SpanIDKey).(string); ok {
			spanID = spanIDValue
		}
		traceCtx := TraceContext{
			TraceID: traceID,
			SpanID:  spanID,
			Valid:   true,
		}
		// 将trace信息缓存到context中
		ctx = context.WithValue(ctx, TraceContextKey, traceCtx)
		return ctx, traceCtx
	}

	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		// 如果没有有效的追踪信息，自动生成新的
		// 使用全局 TracerProvider 创建一个新的 span
		tracer := otel.Tracer("auto-generate")
		_, newSpan := tracer.Start(ctx, "auto-generated")

		// 立即结束span，因为我们只需要trace信息
		// 这样可以避免内存泄漏
		newSpan.End()

		// 检查生成的span是否有效
		spanContext := newSpan.SpanContext()
		if spanContext.IsValid() {
			traceCtx := TraceContext{
				TraceID: spanContext.TraceID().String(),
				SpanID:  spanContext.SpanID().String(),
				Valid:   true,
			}
			// 将生成的trace信息缓存到context中
			ctx = context.WithValue(ctx, TraceContextKey, traceCtx)
			return ctx, traceCtx
		}

		// 如果生成的span无效（比如使用了NoopTracerProvider），
		// 则使用随机生成的方式创建trace信息
		fallbackTrace := generateFallbackTraceContext()
		// 将生成的trace信息缓存到context中
		ctx = context.WithValue(ctx, TraceContextKey, fallbackTrace)
		return ctx, fallbackTrace
	}

	traceCtx := TraceContext{
		TraceID: span.SpanContext().TraceID().String(),
		SpanID:  span.SpanContext().SpanID().String(),
		Valid:   true,
	}
	// 将有效的trace信息缓存到context中
	ctx = context.WithValue(ctx, TraceContextKey, traceCtx)
	return ctx, traceCtx
}

// generateFallbackTraceContext 当OpenTelemetry无法生成有效trace时的备用方案
func generateFallbackTraceContext() TraceContext {
	// 生成32位十六进制字符串作为TraceID
	traceID := generateRandomHex(32)
	// 生成16位十六进制字符串作为SpanID
	spanID := generateRandomHex(16)

	return TraceContext{
		TraceID: traceID,
		SpanID:  spanID,
		Valid:   true,
	}
}

// generateRandomHex 生成指定长度的随机十六进制字符串
func generateRandomHex(length int) string {
	const hexChars = "0123456789abcdef"
	result := make([]byte, length)

	// 使用时间戳和简单的算法生成伪随机数
	// 在实际生产环境中，建议使用crypto/rand
	seed := time.Now().UnixNano()
	for i := range result {
		seed = seed*1103515245 + 12345 // 线性同余生成器
		index := seed % 16
		if index < 0 {
			index = -index
		}
		result[i] = hexChars[index]
	}
	return string(result)
}

// HasValidTrace 检查上下文是否有有效的追踪信息
func HasValidTrace(ctx context.Context) bool {
	return GetTraceContext(ctx).Valid
}

// EnsureRequestID 确保上下文中有请求ID，如果没有则自动生成
func EnsureRequestID(ctx context.Context) context.Context {
	if GetRequestID(ctx) == "" {
		requestID := GenerateRequestID(ctx)
		ctx = WithRequestID(ctx, requestID)
	}
	return ctx
}

// ensureTraceContext 确保上下文中有有效的trace信息，如果没有则自动生成
func ensureTraceContext(ctx context.Context) context.Context {
	// 如果context为nil，创建一个新的context
	if ctx == nil {
		ctx = context.Background()
	}

	// 检查是否已经有有效的trace信息
	if HasValidTrace(ctx) {
		return ctx
	}

	// 如果没有有效的trace信息，创建一个新的span来生成trace
	tracer := otel.Tracer("auto-generate")
	ctx, span := tracer.Start(ctx, "auto-generated-trace")

	// 将span信息存储到context中，供后续使用
	ctx = trace.ContextWithSpan(ctx, span)

	// 立即结束span，因为我们只需要trace信息
	// 日志系统内部管理span生命周期，不让业务逻辑操心
	span.End()

	return ctx
}

// ExtractTraceFromHTTPRequest 从HTTP请求中提取trace信息
// 如果没有找到trace信息，会自动生成新的trace
func ExtractTraceFromHTTPRequest(req *http.Request) context.Context {
	// 使用OpenTelemetry的传播机制从HTTP请求头中提取trace上下文
	ctx := otel.GetTextMapPropagator().Extract(req.Context(), propagation.HeaderCarrier(req.Header))

	// 检查是否成功提取到trace信息，如果没有则自动生成
	if !HasValidTrace(ctx) {
		ctx = ensureTraceContext(ctx)
	}

	return ctx
}

// ExtractTraceFromRPCContext 从RPC上下文中提取trace信息
// 如果没有找到trace信息，会自动生成新的trace
// 改进：优先使用OpenTelemetry传播机制，同时兼容自定义字段
func ExtractTraceFromRPCContext(ctx context.Context, metadata map[string]string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	
	// 首先尝试从 OpenTelemetry 传播机制中提取（标准方式）
	if metadata != nil && len(metadata) > 0 {
		// 将map[string]string转换为http.Header
		httpHeaders := make(http.Header)
		for k, v := range metadata {
			httpHeaders.Set(k, v)
		}
		// 使用OpenTelemetry的传播机制从RPC元数据中提取trace上下文
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(httpHeaders))
		
		// 如果成功提取到trace信息，直接返回
		if HasValidTrace(ctx) {
			return ctx
		}
	}
	
	// 如果OpenTelemetry传播机制没有提取到，尝试从metadata中直接提取自定义字段
	if metadata != nil {
		traceID := metadata["X-Trace-Id"]
		if traceID == "" {
			traceID = metadata["trace-id"]
		}
		if traceID == "" {
			traceID = metadata["trace_id"]
		}
		
		if traceID != "" {
			spanID := metadata["X-Span-Id"]
			if spanID == "" {
				spanID = metadata["span-id"]
			}
			if spanID == "" {
				spanID = metadata["span_id"]
			}
			
			// 将提取到的trace信息存储到Context中
			ctx = context.WithValue(ctx, TraceIDKey, traceID)
			if spanID != "" {
				ctx = context.WithValue(ctx, SpanIDKey, spanID)
			}
			
			// 尝试将trace信息注入到OpenTelemetry context中，以便后续span可以继承
			ctx = ensureTraceContext(ctx)
			return ctx
		}
	}
	
	// 如果都没有找到trace信息，自动生成新的trace
	ctx = ensureTraceContext(ctx)
	return ctx
}

// ExtractTraceFromRPCRequest 从RPC请求中提取trace信息并设置到context中
// 如果没有找到trace信息，会自动生成新的trace
// 这是一个通用方法，可以被其他服务直接使用
func ExtractTraceFromRPCRequest(ctx context.Context, req interface{}) context.Context {
	// 使用 Kitex metainfo 从 context 中提取 trace 信息
	traceID, _ := metainfo.GetValue(ctx, "X-Trace-Id")
	spanID, _ := metainfo.GetValue(ctx, "X-Span-Id")
	clientName, _ := metainfo.GetValue(ctx, "X-Client-Name")
	method, _ := metainfo.GetValue(ctx, "X-Method")

	// 如果找到了trace信息，将其存储到Context中
	if traceID != "" {
		ctx = context.WithValue(ctx, TraceIDKey, traceID)
		ctx = context.WithValue(ctx, SpanIDKey, spanID)
		ctx = context.WithValue(ctx, ClientNameKey, clientName)
		ctx = context.WithValue(ctx, MethodKey, method)

		// 记录接收到的trace信息
		if logger := GetLoggerFromContext(ctx); logger != nil {
			logger.Info(ctx, "从RPC metainfo中提取trace信息", map[string]interface{}{
				"trace_id":    traceID,
				"span_id":     spanID,
				"client_name": clientName,
				"method":      method,
			})
		}
	} else {
		// 如果没有找到trace信息，自动生成新的trace
		ctx = ensureTraceContext(ctx)

		// 获取生成的trace信息并存储到Context中
		traceContext := GetTraceContext(ctx)
		if traceContext.Valid {
			ctx = context.WithValue(ctx, TraceIDKey, traceContext.TraceID)
			ctx = context.WithValue(ctx, SpanIDKey, traceContext.SpanID)

			// 记录自动生成的trace信息
			if logger := GetLoggerFromContext(ctx); logger != nil {
				logger.Info(ctx, "自动生成RPC trace信息", map[string]interface{}{
					"trace_id": traceContext.TraceID,
					"span_id":  traceContext.SpanID,
				})
			}
		}
	}

	return ctx
}

// ExtractTraceFromHeaders 从Header map中提取trace信息（更通用的方法）
// 如果没有找到trace信息，会自动生成新的trace
func ExtractTraceFromHeaders(ctx context.Context, headers map[string]string) context.Context {
	traceID := headers["X-Trace-Id"]
	spanID := headers["X-Span-Id"]
	clientName := headers["X-Client-Name"]
	method := headers["X-Method"]

	// 如果找到了trace信息，将其存储到Context中
	if traceID != "" {
		ctx = context.WithValue(ctx, TraceIDKey, traceID)
		ctx = context.WithValue(ctx, SpanIDKey, spanID)
		ctx = context.WithValue(ctx, ClientNameKey, clientName)
		ctx = context.WithValue(ctx, MethodKey, method)

		// 记录接收到的trace信息
		if logger := GetLoggerFromContext(ctx); logger != nil {
			logger.Info(ctx, "从Header中提取trace信息", map[string]interface{}{
				"trace_id":    traceID,
				"span_id":     spanID,
				"client_name": clientName,
				"method":      method,
			})
		}
	} else {
		// 如果没有找到trace信息，自动生成新的trace
		ctx = ensureTraceContext(ctx)

		// 获取生成的trace信息并存储到Context中
		traceContext := GetTraceContext(ctx)
		if traceContext.Valid {
			ctx = context.WithValue(ctx, TraceIDKey, traceContext.TraceID)
			ctx = context.WithValue(ctx, SpanIDKey, traceContext.SpanID)
			ctx = context.WithValue(ctx, ClientNameKey, clientName)
			ctx = context.WithValue(ctx, MethodKey, method)

			// 记录自动生成的trace信息
			if logger := GetLoggerFromContext(ctx); logger != nil {
				logger.Info(ctx, "自动生成Header trace信息", map[string]interface{}{
					"trace_id":    traceContext.TraceID,
					"span_id":     traceContext.SpanID,
					"client_name": clientName,
					"method":      method,
				})
			}
		}
	}

	return ctx
}

// ExtractTraceFromCustomHeaders 从自定义Header名称中提取trace信息（最灵活的方法）
// 如果没有找到trace信息，会自动生成新的trace
func ExtractTraceFromCustomHeaders(ctx context.Context, headers map[string]string, traceIDKey, spanIDKey, clientNameKey, methodKey string) context.Context {
	traceID := headers[traceIDKey]
	spanID := headers[spanIDKey]
	clientName := headers[clientNameKey]
	method := headers[methodKey]

	// 如果找到了trace信息，将其存储到Context中
	if traceID != "" {
		ctx = context.WithValue(ctx, TraceIDKey, traceID)
		ctx = context.WithValue(ctx, SpanIDKey, spanID)
		ctx = context.WithValue(ctx, ClientNameKey, clientName)
		ctx = context.WithValue(ctx, MethodKey, method)

		// 记录接收到的trace信息
		if logger := GetLoggerFromContext(ctx); logger != nil {
			logger.Info(ctx, "从自定义Header中提取trace信息", map[string]interface{}{
				"trace_id":     traceID,
				"span_id":      spanID,
				"client_name":  clientName,
				"method":       method,
				"trace_id_key": traceIDKey,
				"span_id_key":  spanIDKey,
			})
		}
	} else {
		// 如果没有找到trace信息，自动生成新的trace
		ctx = ensureTraceContext(ctx)

		// 获取生成的trace信息并存储到Context中
		traceContext := GetTraceContext(ctx)
		if traceContext.Valid {
			ctx = context.WithValue(ctx, TraceIDKey, traceContext.TraceID)
			ctx = context.WithValue(ctx, SpanIDKey, traceContext.SpanID)
			ctx = context.WithValue(ctx, ClientNameKey, clientName)
			ctx = context.WithValue(ctx, MethodKey, method)

			// 记录自动生成的trace信息
			if logger := GetLoggerFromContext(ctx); logger != nil {
				logger.Info(ctx, "自动生成自定义Header trace信息", map[string]interface{}{
					"trace_id":     traceContext.TraceID,
					"span_id":      traceContext.SpanID,
					"client_name":  clientName,
					"method":       method,
					"trace_id_key": traceIDKey,
					"span_id_key":  spanIDKey,
				})
			}
		}
	}

	return ctx
}

// GetTraceIDFromContext 从Context中安全获取Trace ID
func GetTraceIDFromContext(ctx context.Context) string {
	if traceID, ok := ctx.Value(TraceIDKey).(string); ok {
		return traceID
	}
	return ""
}

// GetSpanIDFromContext 从Context中安全获取Span ID
func GetSpanIDFromContext(ctx context.Context) string {
	if spanID, ok := ctx.Value(SpanIDKey).(string); ok {
		return spanID
	}
	return ""
}

// GetClientNameFromContext 从Context中安全获取客户端名称
func GetClientNameFromContext(ctx context.Context) string {
	if clientName, ok := ctx.Value(ClientNameKey).(string); ok {
		return clientName
	}
	return ""
}

// GetMethodFromContext 从Context中安全获取方法名
func GetMethodFromContext(ctx context.Context) string {
	if method, ok := ctx.Value(MethodKey).(string); ok {
		return method
	}
	return ""
}

// initTracerProvider 初始化TracerProvider，使用 kitex-contrib/obs-opentelemetry 的 provider
// 这是让 tracing.NewClientSuite() 和 tracing.NewServerSuite() 正常工作的关键
//
// 重要说明：
// 1. TracerProvider 的作用：让 trace context 能在服务间传递 (trace propagation)
// 2. ExportEndpoint 的作用：将 traces 导出到后端系统进行可视化 (Jaeger/ARMS/Zipkin等)
// 3. Trace propagation 不依赖 ExportEndpoint！即使不配置 endpoint，trace ID 也能正常传递
// 4. 如果需要在 Jaeger/ARMS 中查看 trace 链路图，才需要配置 endpoint
func initTracerProvider(config *TracingConfig) error {
	// initTracerProvider 初始化TracerProvider,确保ExtractTraceFromRPCRequest能生成有效的trace信息
	//
	// 关键发现: provider.NewOpenTelemetryProvider() 并不会自动设置全局的TextMapPropagator!
	// 我们必须手动调用 otel.SetTracerProvider() 和 otel.SetTextMapPropagator()
	//
	// 这是一个简单的实现,不依赖复杂的 provider 配置
	// 关键是设置全局的TracerProvider和Propagator,这样tracing.NewClientSuite()才能正常工作

	// 创建一个简单的TracerProvider
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)

	// 设置全局传播器 - 这是trace propagation的关键!!!
	// W3C TraceContext标准确保trace ID能在服务间传递
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return nil
}
