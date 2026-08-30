package observability

import (
	commonlog "code.wenfu.cn/stablepayai/stablepay-common/log"
	"go.uber.org/zap"
	"github.com/stablepay/api-gateway/internal/infrastructure/config"
)

type Logger struct {
	base *commonlog.Logger
}

func NewLogger(cfg config.Logging) (*Logger, error) {
	logCfg := commonlog.NewConfig("api-gateway", "v0.1")
	logCfg.WithEnvironment(cfg.Environment)
	logCfg.Logging.Level = cfg.Level
	logCfg.Logging.Format = cfg.Format
	logCfg.Logging.Output = cfg.Output
	logCfg.Logging.FilePath = cfg.FilePath
	logCfg.Tracing.Enabled = cfg.EnableTracing
	logCfg.Tracing.Backend = cfg.TracingBackend
	if cfg.TracingEndpoint != "" {
		logCfg.Tracing.OTLP.Endpoint = cfg.TracingEndpoint
		logCfg.Tracing.Jaeger.Endpoint = cfg.TracingEndpoint
	}
	if err := logCfg.Validate(); err != nil {
		return nil, err
	}

	base, err := commonlog.NewLogger(logCfg)
	if err != nil {
		return nil, err
	}
	commonlog.SetGlobalLogger(base)
	return &Logger{base: base}, nil
}

func NewTestLogger() *Logger {
	logCfg := commonlog.NewConfig("api-gateway-test", "v0.1")
	logCfg.Logging.Output = "stdout"
	logCfg.Logging.Level = "error"
	logCfg.Tracing.Enabled = false
	base, err := commonlog.NewLogger(logCfg)
	if err != nil {
		return &Logger{base: &commonlog.Logger{Logger: zap.NewNop()}}
	}
	return &Logger{base: base}
}

func (l *Logger) Sync() error {
	if l == nil || l.base == nil || l.base.Logger == nil {
		return nil
	}
	return l.base.Sync()
}

func (l *Logger) Info(msg string, fields map[string]interface{}) {
	if l == nil || l.base == nil {
		return
	}
	l.base.InfoNoCtx(msg, fields)
}

func (l *Logger) Warn(msg string, fields map[string]interface{}) {
	if l == nil || l.base == nil {
		return
	}
	l.base.WarnNoCtx(msg, fields)
}

func (l *Logger) Error(msg string, fields map[string]interface{}) {
	if l == nil || l.base == nil {
		return
	}
	l.base.ErrorNoCtx(msg, fields)
}

func (l *Logger) Fatal(msg string, fields map[string]interface{}) {
	if l == nil || l.base == nil || l.base.Logger == nil {
		panic(msg)
	}
	l.base.Logger.Fatal(msg, toZapFields(fields)...)
}

func toZapFields(fields map[string]interface{}) []zap.Field {
	if len(fields) == 0 {
		return nil
	}
	result := make([]zap.Field, 0, len(fields))
	for key, value := range fields {
		result = append(result, zap.Any(key, value))
	}
	return result
}
