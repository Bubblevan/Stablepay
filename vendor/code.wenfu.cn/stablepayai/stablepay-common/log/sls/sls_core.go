package sls

import (
	"fmt"
	"os"

	"github.com/aliyun/aliyun-log-go-sdk/producer"
	"go.uber.org/zap/zapcore"
)

// SLSCore SLS日志核心处理器
type SLSCore struct {
	producer *producer.Producer
	config   SLSConfig
	level    zapcore.Level
}

// SLSConfig SLS配置
type SLSConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	Endpoint        string `yaml:"endpoint" json:"endpoint"`
	AccessKeyID     string `yaml:"access_key_id" json:"access_key_id"`
	AccessKeySecret string `yaml:"access_key_secret" json:"access_key_secret"`
	Project         string `yaml:"project" json:"project"`
	Logstore        string `yaml:"logstore" json:"logstore"`
	Topic           string `yaml:"topic" json:"topic"`
	Source          string `yaml:"source" json:"source"`
}

// NewSLSCore 创建SLS核心处理器
func NewSLSCore(config SLSConfig, level zapcore.Level) (*SLSCore, error) {
	// 创建SLS Producer
	producerConfig := producer.GetDefaultProducerConfig()
	producerConfig.Endpoint = config.Endpoint
	producerConfig.AccessKeyID = config.AccessKeyID
	producerConfig.AccessKeySecret = config.AccessKeySecret
	producerConfig.MaxBatchCount = 100
	producerConfig.MaxBatchSize = 1024 * 1024        // 1MB
	producerConfig.DisableRuntimeMetrics = true      // 关闭 SDK 内部监控日志
	// 设置重试配置（如果支持的话）
	// producerConfig.MaxRetryCount = 3

	producerInstance := producer.InitProducer(producerConfig)
	producerInstance.Start()

	return &SLSCore{
		producer: producerInstance,
		config:   config,
		level:    level,
	}, nil
}

// Enabled 检查是否启用
func (s *SLSCore) Enabled(level zapcore.Level) bool {
	return level >= s.level
}

// With 添加字段
func (s *SLSCore) With(fields []zapcore.Field) zapcore.Core {
	return s
}

// Check 检查日志级别
func (s *SLSCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if s.Enabled(entry.Level) {
		return checked.AddCore(entry, s)
	}
	return checked
}

// Write 写入日志到SLS
func (s *SLSCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// 构建SLS日志数据
	logData := make(map[string]interface{})

	// 添加基础字段
	logData["__time__"] = entry.Time.Unix()
	logData["level"] = entry.Level.String()
	logData["message"] = entry.Message
	logData["caller"] = entry.Caller.String()

	// 添加自定义字段
	for _, field := range fields {
		switch field.Type {
		case zapcore.StringType:
			logData[field.Key] = field.String
		case zapcore.Int64Type:
			logData[field.Key] = field.Integer
		case zapcore.Float64Type:
			logData[field.Key] = field.Interface
		case zapcore.BoolType:
			logData[field.Key] = field.Interface
		default:
			logData[field.Key] = field.Interface
		}
	}

	// 转换数据格式
	logDataStr := make(map[string]string)
	for k, v := range logData {
		logDataStr[k] = fmt.Sprintf("%v", v)
	}

	// 发送到SLS
	log := producer.GenerateLog(uint32(entry.Time.Unix()), logDataStr)
	err := s.producer.SendLog(s.config.Project, s.config.Logstore, s.config.Topic, s.config.Source, log)
	if err != nil {
		// 如果SLS发送失败，记录到标准错误输出
		fmt.Fprintf(os.Stderr, "SLS send failed: %v\n", err)
		return err
	}

	return nil
}

// Sync 同步
func (s *SLSCore) Sync() error {
	if s.producer != nil {
		s.producer.Close(30)
	}
	return nil
}

// Close 关闭SLS核心处理器
func (s *SLSCore) Close() error {
	return s.Sync()
}
