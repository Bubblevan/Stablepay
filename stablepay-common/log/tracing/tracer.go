package tracing

import (
	"context"
	"fmt"
	"log"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TracerManager 链路追踪管理器
type TracerManager struct {
	tracerProvider *sdktrace.TracerProvider
	tracer         trace.Tracer
	config         *TracingConfig
}

// NewTracerManager 创建链路追踪管理器
func NewTracerManager(config *TracingConfig) (*TracerManager, error) {
	if !config.Enabled {
		return &TracerManager{
			tracerProvider: nil,
			tracer:         trace.NewNoopTracerProvider().Tracer("noop"),
			config:         config,
		}, nil
	}

	// 创建资源
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(config.ServiceName),
			semconv.ServiceVersionKey.String(config.Version),
			semconv.DeploymentEnvironmentKey.String(config.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("创建资源失败: %w", err)
	}

	// 创建导出器
	exporters, err := createExporters(config)
	if err != nil {
		return nil, fmt.Errorf("创建导出器失败: %w", err)
	}

	// 创建采样器
	sampler := createSampler(config)

	// 创建TracerProvider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporters[0]),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// 设置全局TracerProvider
	otel.SetTracerProvider(tp)

	// 设置全局传播器
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	tracer := tp.Tracer(config.ServiceName)

	return &TracerManager{
		tracerProvider: tp,
		tracer:         tracer,
		config:         config,
	}, nil
}

// createExporters 创建导出器
func createExporters(config *TracingConfig) ([]sdktrace.SpanExporter, error) {
	var exporters []sdktrace.SpanExporter

	// 阿里云链路追踪导出器
	if config.AliyunTracing.Enabled {
		exporter, err := createAliyunExporter(config.AliyunTracing)
		if err != nil {
			log.Printf("[ERROR] 创建阿里云导出器失败: component=tracing exporter=aliyun error=%v", err)
		} else {
			exporters = append(exporters, exporter)
		}
	}

	// Jaeger导出器
	if config.Jaeger.Enabled {
		exporter, err := createJaegerExporter(config.Jaeger)
		if err != nil {
			log.Printf("[ERROR] 创建Jaeger导出器失败: component=tracing exporter=jaeger error=%v", err)
		} else {
			exporters = append(exporters, exporter)
		}
	}

	// OTLP导出器
	if config.OTLP.Enabled {
		exporter, err := createOTLPExporter(config.OTLP)
		if err != nil {
			log.Printf("[ERROR] 创建OTLP导出器失败: component=tracing exporter=otlp error=%v", err)
		} else {
			exporters = append(exporters, exporter)
		}
	}

	if len(exporters) == 0 {
		return nil, fmt.Errorf("没有可用的导出器")
	}

	return exporters, nil
}

// createAliyunExporter 创建阿里云导出器
func createAliyunExporter(config AliyunTracingConfig) (sdktrace.SpanExporter, error) {
	// 使用OTLP HTTP导出器连接到阿里云
	exporter, err := otlptracehttp.New(
		context.Background(),
		otlptracehttp.WithEndpoint(config.Endpoint),
		otlptracehttp.WithHeaders(map[string]string{
			"Authentication":         config.Token,
			"x-sls-otel-project":     config.Project,
			"x-sls-otel-instance-id": config.Instance,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("创建阿里云OTLP导出器失败: %w", err)
	}

	return exporter, nil
}

// createJaegerExporter 创建Jaeger导出器
func createJaegerExporter(config JaegerConfig) (sdktrace.SpanExporter, error) {
	exporter, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(config.Endpoint)))
	if err != nil {
		return nil, fmt.Errorf("创建Jaeger导出器失败: %w", err)
	}

	return exporter, nil
}

// createOTLPExporter 创建OTLP导出器
func createOTLPExporter(config OTLPConfig) (sdktrace.SpanExporter, error) {
	if config.Protocol == "grpc" {
		exporter, err := otlptracegrpc.New(
			context.Background(),
			otlptracegrpc.WithEndpoint(config.Endpoint),
			otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)
		if err != nil {
			return nil, fmt.Errorf("创建OTLP gRPC导出器失败: %w", err)
		}
		return exporter, nil
	}

	// HTTP协议
	exporter, err := otlptracehttp.New(
		context.Background(),
		otlptracehttp.WithEndpoint(config.Endpoint),
		otlptracehttp.WithHeaders(config.Headers),
	)
	if err != nil {
		return nil, fmt.Errorf("创建OTLP HTTP导出器失败: %w", err)
	}

	return exporter, nil
}

// createSampler 创建采样器
func createSampler(config *TracingConfig) sdktrace.Sampler {
	switch config.Sampling.Type {
	case "always":
		return sdktrace.AlwaysSample()
	case "never":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(config.Sampling.Ratio)
	case "parentbased":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(config.Sampling.Ratio))
	default:
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1.0))
	}
}

// GetTracer 获取Tracer
func (tm *TracerManager) GetTracer() trace.Tracer {
	return tm.tracer
}

// StartSpan 开始一个新的Span
func (tm *TracerManager) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return tm.tracer.Start(ctx, name, opts...)
}

// StartSpanWithAttributes 开始一个带属性的Span
func (tm *TracerManager) StartSpanWithAttributes(ctx context.Context, name string, attrs []attribute.KeyValue, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	opts = append(opts, trace.WithAttributes(attrs...))
	return tm.tracer.Start(ctx, name, opts...)
}

// AddSpanEvent 添加Span事件
func (tm *TracerManager) AddSpanEvent(ctx context.Context, name string, attrs []attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.AddEvent(name, trace.WithAttributes(attrs...))
	}
}

// SetSpanAttributes 设置Span属性
func (tm *TracerManager) SetSpanAttributes(ctx context.Context, attrs []attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

// SetSpanStatus 设置Span状态
func (tm *TracerManager) SetSpanStatus(ctx context.Context, code codes.Code, description string) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetStatus(code, description)
	}
}

// Shutdown 关闭链路追踪
func (tm *TracerManager) Shutdown(ctx context.Context) error {
	if tm.tracerProvider != nil {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return tm.tracerProvider.Shutdown(ctx)
	}
	return nil
}

// 注意：GetTraceID 和 GetSpanID 方法已删除
// 请使用阿里云框架提供的 trace ID 和 span ID 获取方法
// 或者直接使用 OpenTelemetry 标准方法：
// span := trace.SpanFromContext(ctx)
// traceID := span.SpanContext().TraceID().String()
// spanID := span.SpanContext().SpanID().String()

// IsEnabled 检查链路追踪是否启用
func (tm *TracerManager) IsEnabled() bool {
	return tm.config.Enabled && tm.tracerProvider != nil
}
