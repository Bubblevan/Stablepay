package log

import (
	"context"
	"log"
)

// 全局 logger 实例
var globalLogger *Logger

// SetGlobalLogger 设置全局 logger 实例
// 应用启动时应该调用此函数设置全局 logger
func SetGlobalLogger(logger *Logger) {
	globalLogger = logger
}

// GetGlobalLogger 获取全局 logger 实例
func GetGlobalLogger() *Logger {
	return globalLogger
}

// Info 全局信息日志记录（带 trace context）
func Info(ctx context.Context, msg string, fields map[string]interface{}) {
	if globalLogger != nil {
		globalLogger.Info(ctx, msg, fields)
	} else {
		log.Printf("[INFO] %s %v", msg, fields)
	}
}

// Error 全局错误日志记录（带 trace context）
func Error(ctx context.Context, msg string, fields map[string]interface{}) {
	if globalLogger != nil {
		globalLogger.Error(ctx, msg, fields)
	} else {
		log.Printf("[ERROR] %s %v", msg, fields)
	}
}

// Warn 全局警告日志记录（带 trace context）
func Warn(ctx context.Context, msg string, fields map[string]interface{}) {
	if globalLogger != nil {
		globalLogger.Warn(ctx, msg, fields)
	} else {
		log.Printf("[WARN] %s %v", msg, fields)
	}
}

// Debug 全局调试日志记录（带 trace context）
func Debug(ctx context.Context, msg string, fields map[string]interface{}) {
	if globalLogger != nil {
		globalLogger.Debug(ctx, msg, fields)
	} else {
		log.Printf("[DEBUG] %s %v", msg, fields)
	}
}

// InfoNoCtx 全局信息日志记录（不需要 context）
func InfoNoCtx(msg string, fields map[string]interface{}) {
	if globalLogger != nil {
		globalLogger.InfoNoCtx(msg, fields)
	} else {
		log.Printf("[INFO] %s %v", msg, fields)
	}
}

// ErrorNoCtx 全局错误日志记录（不需要 context）
func ErrorNoCtx(msg string, fields map[string]interface{}) {
	if globalLogger != nil {
		globalLogger.ErrorNoCtx(msg, fields)
	} else {
		log.Printf("[ERROR] %s %v", msg, fields)
	}
}

// WarnNoCtx 全局警告日志记录（不需要 context）
func WarnNoCtx(msg string, fields map[string]interface{}) {
	if globalLogger != nil {
		globalLogger.WarnNoCtx(msg, fields)
	} else {
		log.Printf("[WARN] %s %v", msg, fields)
	}
}

// DebugNoCtx 全局调试日志记录（不需要 context）
func DebugNoCtx(msg string, fields map[string]interface{}) {
	if globalLogger != nil {
		globalLogger.DebugNoCtx(msg, fields)
	} else {
		log.Printf("[DEBUG] %s %v", msg, fields)
	}
}
