package log

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"code.wenfu.cn/stablepayai/stablepay-common/log/sls"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger 结构化日志器
type Logger struct {
	*zap.Logger
	requestIDKey string
}

// 注意：LogConfig 和 SLSConfig 已移动到 config.go 中的统一配置结构

// NewLogger 创建新的日志器（使用统一配置）
func NewLogger(config *Config) (*Logger, error) {
	// 设置日志级别
	level := zapcore.InfoLevel
	switch config.Logging.Level {
	case "debug":
		level = zapcore.DebugLevel
	case "info":
		level = zapcore.InfoLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	default:
		return nil, fmt.Errorf("无效的日志级别: %s", config.Logging.Level)
	}

	// 配置编码器
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.LevelKey = "level"
	encoderConfig.MessageKey = "message"
	encoderConfig.CallerKey = "caller"
	encoderConfig.EncodeTime = zapcore.RFC3339TimeEncoder
	encoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	var encoder zapcore.Encoder
	switch config.Logging.Format {
	case "json":
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	case "text":
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	default:
		return nil, fmt.Errorf("不支持的日志格式: %s", config.Logging.Format)
	}

	// 配置输出
	var cores []zapcore.Core

	switch config.Logging.Output {
	case "stdout":
		cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level))
	case "file":
		if err := ensureLogDir(config.Logging.FilePath); err != nil {
			return nil, fmt.Errorf("创建日志目录失败: %w", err)
		}

		// 生成带日期的文件路径
		filePath := generateLogFilePath(&config.Logging)
		fileWriter := &lumberjack.Logger{
			Filename:   filePath,
			MaxSize:    config.Logging.MaxSize,
			MaxAge:     config.Logging.MaxAge,
			MaxBackups: config.Logging.MaxBackups,
			Compress:   config.Logging.Compress,
			LocalTime:  true,
		}
		cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(fileWriter), level))
	case "both":
		if err := ensureLogDir(config.Logging.FilePath); err != nil {
			return nil, fmt.Errorf("创建日志目录失败: %w", err)
		}

		// 生成带日期的文件路径
		filePath := generateLogFilePath(&config.Logging)
		fileWriter := &lumberjack.Logger{
			Filename:   filePath,
			MaxSize:    config.Logging.MaxSize,
			MaxAge:     config.Logging.MaxAge,
			MaxBackups: config.Logging.MaxBackups,
			Compress:   config.Logging.Compress,
			LocalTime:  true,
		}
		cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level))
		cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(fileWriter), level))
	case "sls":
		// SLS输出
		if config.SLS.Enabled {
			slsCore, err := sls.NewSLSCore(sls.SLSConfig{
				Enabled:         config.SLS.Enabled,
				Endpoint:        config.SLS.Endpoint,
				AccessKeyID:     config.SLS.AccessKeyID,
				AccessKeySecret: config.SLS.AccessKeySecret,
				Project:         config.SLS.Project,
				Logstore:        config.SLS.Logstore,
				Topic:           config.SLS.Topic,
				Source:          config.SLS.Source,
			}, level)
			if err != nil {
				return nil, fmt.Errorf("创建SLS核心处理器失败: %w", err)
			}
			cores = append(cores, slsCore)
		} else {
			// 如果SLS未启用，使用stdout作为后备
			cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level))
		}
	case "stdout-sls":
		// stdout + SLS输出
		cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level))
		if config.SLS.Enabled {
			slsCore, err := sls.NewSLSCore(sls.SLSConfig{
				Enabled:         config.SLS.Enabled,
				Endpoint:        config.SLS.Endpoint,
				AccessKeyID:     config.SLS.AccessKeyID,
				AccessKeySecret: config.SLS.AccessKeySecret,
				Project:         config.SLS.Project,
				Logstore:        config.SLS.Logstore,
				Topic:           config.SLS.Topic,
				Source:          config.SLS.Source,
			}, level)
			if err != nil {
				return nil, fmt.Errorf("创建SLS核心处理器失败: %w", err)
			}
			cores = append(cores, slsCore)
		}
	case "file-sls":
		// file + SLS输出
		if err := ensureLogDir(config.Logging.FilePath); err != nil {
			return nil, fmt.Errorf("创建日志目录失败: %w", err)
		}

		filePath := generateLogFilePath(&config.Logging)
		fileWriter := &lumberjack.Logger{
			Filename:   filePath,
			MaxSize:    config.Logging.MaxSize,
			MaxAge:     config.Logging.MaxAge,
			MaxBackups: config.Logging.MaxBackups,
			Compress:   config.Logging.Compress,
			LocalTime:  true,
		}
		cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(fileWriter), level))

		if config.SLS.Enabled {
			slsCore, err := sls.NewSLSCore(sls.SLSConfig{
				Enabled:         config.SLS.Enabled,
				Endpoint:        config.SLS.Endpoint,
				AccessKeyID:     config.SLS.AccessKeyID,
				AccessKeySecret: config.SLS.AccessKeySecret,
				Project:         config.SLS.Project,
				Logstore:        config.SLS.Logstore,
				Topic:           config.SLS.Topic,
				Source:          config.SLS.Source,
			}, level)
			if err != nil {
				return nil, fmt.Errorf("创建SLS核心处理器失败: %w", err)
			}
			cores = append(cores, slsCore)
		}
	case "both-sls":
		// both (stdout + file) + SLS输出
		if err := ensureLogDir(config.Logging.FilePath); err != nil {
			return nil, fmt.Errorf("创建日志目录失败: %w", err)
		}

		filePath := generateLogFilePath(&config.Logging)
		fileWriter := &lumberjack.Logger{
			Filename:   filePath,
			MaxSize:    config.Logging.MaxSize,
			MaxAge:     config.Logging.MaxAge,
			MaxBackups: config.Logging.MaxBackups,
			Compress:   config.Logging.Compress,
			LocalTime:  true,
		}
		cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(os.Stdout), level))
		cores = append(cores, zapcore.NewCore(encoder, zapcore.AddSync(fileWriter), level))

		if config.SLS.Enabled {
			slsCore, err := sls.NewSLSCore(sls.SLSConfig{
				Enabled:         config.SLS.Enabled,
				Endpoint:        config.SLS.Endpoint,
				AccessKeyID:     config.SLS.AccessKeyID,
				AccessKeySecret: config.SLS.AccessKeySecret,
				Project:         config.SLS.Project,
				Logstore:        config.SLS.Logstore,
				Topic:           config.SLS.Topic,
				Source:          config.SLS.Source,
			}, level)
			if err != nil {
				return nil, fmt.Errorf("创建SLS核心处理器失败: %w", err)
			}
			cores = append(cores, slsCore)
		}
	default:
		return nil, fmt.Errorf("不支持的输出类型: %s", config.Logging.Output)
	}

	core := zapcore.NewTee(cores...)
	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1))

	// 如果启用了tracing，自动初始化TracerProvider
	if config.Tracing.Enabled {
		// 初始化TracerProvider，这样ExtractTraceFromRPCRequest就不会返回0000了
		if err := initTracerProvider(&config.Tracing); err != nil {
			return nil, fmt.Errorf("初始化TracerProvider失败: %w", err)
		}
	}

	return &Logger{
		Logger:       logger,
		requestIDKey: "request_id",
	}, nil
}

// ensureLogDir 确保日志目录存在
func ensureLogDir(filePath string) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return nil
}

// generateLogFilePath 生成带日期的日志文件路径
func generateLogFilePath(config *LoggingConfig) string {
	if !config.DailyRotation {
		return config.FilePath
	}

	// 获取当前日期
	now := time.Now()
	dateStr := now.Format(config.DateFormat)

	// 解析文件路径
	dir := filepath.Dir(config.FilePath)
	ext := filepath.Ext(config.FilePath)
	name := strings.TrimSuffix(filepath.Base(config.FilePath), ext)

	// 生成带日期的文件名
	// 例如: logs/stablepay-shopify.log -> logs/stablepay-shopify-2024-01-15.log
	newFileName := fmt.Sprintf("%s-%s%s", name, dateStr, ext)
	return filepath.Join(dir, newFileName)
}

// Info 记录信息级别日志
func (l *Logger) Info(ctx context.Context, msg string, fields map[string]interface{}) {
	// 如果 ctx 不为 nil，获取并缓存trace信息，确保同一个方法内的多次日志使用相同的trace ID
	if ctx != nil {
		ctx, _ = GetTraceContextWithCache(ctx)
	}
	zapFields := l.buildZapFields(ctx, fields)
	l.Logger.Info(msg, zapFields...)
}

// Error 记录错误级别日志
func (l *Logger) Error(ctx context.Context, msg string, fields map[string]interface{}) {
	// 如果 ctx 不为 nil，获取并缓存trace信息，确保同一个方法内的多次日志使用相同的trace ID
	if ctx != nil {
		ctx, _ = GetTraceContextWithCache(ctx)
	}
	zapFields := l.buildZapFields(ctx, fields)
	l.Logger.Error(msg, zapFields...)
}

// Warn 记录警告级别日志
func (l *Logger) Warn(ctx context.Context, msg string, fields map[string]interface{}) {
	// 如果 ctx 不为 nil，获取并缓存trace信息，确保同一个方法内的多次日志使用相同的trace ID
	if ctx != nil {
		ctx, _ = GetTraceContextWithCache(ctx)
	}
	zapFields := l.buildZapFields(ctx, fields)
	l.Logger.Warn(msg, zapFields...)
}

// Debug 记录调试级别日志
func (l *Logger) Debug(ctx context.Context, msg string, fields map[string]interface{}) {
	// 如果 ctx 不为 nil，获取并缓存trace信息，确保同一个方法内的多次日志使用相同的trace ID
	if ctx != nil {
		ctx, _ = GetTraceContextWithCache(ctx)
	}
	zapFields := l.buildZapFields(ctx, fields)
	l.Logger.Debug(msg, zapFields...)
}

// InfoNoCtx 记录信息级别日志（不需要context）
func (l *Logger) InfoNoCtx(msg string, fields map[string]interface{}) {
	zapFields := l.buildZapFieldsNoCtx(fields)
	l.Logger.Info(msg, zapFields...)
}

// ErrorNoCtx 记录错误级别日志（不需要context）
func (l *Logger) ErrorNoCtx(msg string, fields map[string]interface{}) {
	zapFields := l.buildZapFieldsNoCtx(fields)
	l.Logger.Error(msg, zapFields...)
}

// WarnNoCtx 记录警告级别日志（不需要context）
func (l *Logger) WarnNoCtx(msg string, fields map[string]interface{}) {
	zapFields := l.buildZapFieldsNoCtx(fields)
	l.Logger.Warn(msg, zapFields...)
}

// DebugNoCtx 记录调试级别日志（不需要context）
func (l *Logger) DebugNoCtx(msg string, fields map[string]interface{}) {
	zapFields := l.buildZapFieldsNoCtx(fields)
	l.Logger.Debug(msg, zapFields...)
}

// buildZapFields 构建zap字段
func (l *Logger) buildZapFields(ctx context.Context, fields map[string]interface{}) []zap.Field {
	fieldsLen := 0
	if fields != nil {
		fieldsLen = len(fields)
	}
	zapFields := make([]zap.Field, 0, fieldsLen+4) // +4 for request_id, user_id, trace_id, span_id

	// 如果 ctx 不为 nil，则添加上下文相关字段
	if ctx != nil {
		// 添加请求ID
		if requestID := GetRequestID(ctx); requestID != "" {
			zapFields = append(zapFields, zap.String("request_id", requestID))
		}

		// 添加用户ID
		if userID := GetUserID(ctx); userID != "" {
			zapFields = append(zapFields, zap.String("user_id", userID))
		}

		// 添加链路追踪ID（使用GetTraceContextWithCache确保trace信息被正确缓存）
		_, traceInfo := GetTraceContextWithCache(ctx)
		if traceInfo.Valid {
			if traceInfo.TraceID != "" {
				zapFields = append(zapFields, zap.String("trace_id", traceInfo.TraceID))
			}
			if traceInfo.SpanID != "" {
				zapFields = append(zapFields, zap.String("span_id", traceInfo.SpanID))
			}
		}
	}

	// 添加自定义字段（如果 fields 不为 nil）
	if fields != nil {
		for key, value := range fields {
			zapFields = append(zapFields, zap.Any(key, value))
		}
	}

	return zapFields
}

// buildZapFieldsNoCtx 构建zap字段（不需要context）
func (l *Logger) buildZapFieldsNoCtx(fields map[string]interface{}) []zap.Field {
	fieldsLen := 0
	if fields != nil {
		fieldsLen = len(fields)
	}
	zapFields := make([]zap.Field, 0, fieldsLen)

	// 添加自定义字段（如果 fields 不为 nil）
	if fields != nil {
		for key, value := range fields {
			zapFields = append(zapFields, zap.Any(key, value))
		}
	}

	return zapFields
}
