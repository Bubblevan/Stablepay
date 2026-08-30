package log

import (
	"fmt"
)

// Config 统一配置结构
type Config struct {
	// 基础配置
	ServiceName string `yaml:"service_name" json:"service_name"` // 服务名称
	Version     string `yaml:"version" json:"version"`           // 服务版本
	Environment string `yaml:"environment" json:"environment"`   // 环境名称

	// 日志配置
	Logging LoggingConfig `yaml:"logging" json:"logging"`

	// 链路追踪配置
	Tracing TracingConfig `yaml:"tracing" json:"tracing"`

	// SLS配置
	SLS SLSConfig `yaml:"sls" json:"sls"`
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Enabled       bool   `yaml:"enabled" json:"enabled"`               // 是否启用日志
	Level         string `yaml:"level" json:"level"`                   // 日志级别: debug, info, warn, error
	Format        string `yaml:"format" json:"format"`                 // 日志格式: json, text
	Output        string `yaml:"output" json:"output"`                 // 输出目标: stdout, file, both, sls, stdout-sls, file-sls, both-sls
	FilePath      string `yaml:"file_path" json:"file_path"`           // 日志文件路径
	MaxSize       int    `yaml:"max_size" json:"max_size"`             // 最大文件大小(MB)
	MaxAge        int    `yaml:"max_age" json:"max_age"`               // 最大保留天数
	MaxBackups    int    `yaml:"max_backups" json:"max_backups"`       // 最大备份文件数
	Compress      bool   `yaml:"compress" json:"compress"`             // 是否压缩备份文件
	DailyRotation bool   `yaml:"daily_rotation" json:"daily_rotation"` // 启用按日期轮转
	DateFormat    string `yaml:"date_format" json:"date_format"`       // 日期格式
	TimeRotation  bool   `yaml:"time_rotation" json:"time_rotation"`   // 启用按时间轮转
	RotationTime  string `yaml:"rotation_time" json:"rotation_time"`   // 轮转时间 (HH:MM)
}

// TracingConfig 链路追踪配置
type TracingConfig struct {
	Enabled     bool   `yaml:"enabled" json:"enabled"`           // 是否启用链路追踪
	Backend     string `yaml:"backend" json:"backend"`           // 后端类型: jaeger, aliyun, otlp
	ServiceName string `yaml:"service_name" json:"service_name"` // 服务名称
	Version     string `yaml:"version" json:"version"`           // 服务版本
	Environment string `yaml:"environment" json:"environment"`   // 环境名称

	// Jaeger配置
	Jaeger JaegerConfig `yaml:"jaeger" json:"jaeger"`

	// 阿里云链路追踪配置
	Aliyun AliyunTracingConfig `yaml:"aliyun" json:"aliyun"`

	// OTLP配置
	OTLP OTLPConfig `yaml:"otlp" json:"otlp"`

	// 采样配置
	Sampling SamplingConfig `yaml:"sampling" json:"sampling"`
}

// JaegerConfig Jaeger配置
type JaegerConfig struct {
	Endpoint  string `yaml:"endpoint" json:"endpoint"`     // Jaeger端点
	AgentHost string `yaml:"agent_host" json:"agent_host"` // Jaeger Agent主机
	AgentPort int    `yaml:"agent_port" json:"agent_port"` // Jaeger Agent端口
}

// AliyunTracingConfig 阿里云链路追踪配置
type AliyunTracingConfig struct {
	Endpoint    string `yaml:"endpoint" json:"endpoint"`         // 阿里云链路追踪服务端点
	Token       string `yaml:"token" json:"token"`               // 阿里云访问令牌
	Project     string `yaml:"project" json:"project"`           // 项目名称
	Instance    string `yaml:"instance" json:"instance"`         // 实例名称
	ServiceName string `yaml:"service_name" json:"service_name"` // 服务名称
	ServiceIP   string `yaml:"service_ip" json:"service_ip"`     // 服务IP
	ServicePort int    `yaml:"service_port" json:"service_port"` // 服务端口
}

// OTLPConfig OTLP配置
type OTLPConfig struct {
	Endpoint string            `yaml:"endpoint" json:"endpoint"` // OTLP端点
	Protocol string            `yaml:"protocol" json:"protocol"` // 协议类型: grpc, http
	Headers  map[string]string `yaml:"headers" json:"headers"`   // 请求头
}

// SamplingConfig 采样配置
type SamplingConfig struct {
	Type  string  `yaml:"type" json:"type"`   // 采样类型: always, never, traceidratio, parentbased
	Ratio float64 `yaml:"ratio" json:"ratio"` // 采样比例 (0.0-1.0)
}

// SLSConfig 阿里云SLS配置
type SLSConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`                     // 是否启用SLS
	Endpoint        string `yaml:"endpoint" json:"endpoint"`                   // SLS endpoint
	AccessKeyID     string `yaml:"access_key_id" json:"access_key_id"`         // 访问密钥ID
	AccessKeySecret string `yaml:"access_key_secret" json:"access_key_secret"` // 访问密钥Secret
	Project         string `yaml:"project" json:"project"`                     // 项目名称
	Logstore        string `yaml:"logstore" json:"logstore"`                   // 日志库名称
	Topic           string `yaml:"topic" json:"topic"`                         // 日志主题
	Source          string `yaml:"source" json:"source"`                       // 日志来源
}

// NewConfig 创建新的统一配置
func NewConfig(serviceName, version string) *Config {
	return &Config{
		ServiceName: serviceName,
		Version:     version,
		Environment: "production",

		Logging: LoggingConfig{
			Enabled:       true,
			Level:         "info",
			Format:        "json",
			Output:        "stdout",
			FilePath:      "logs/app.log",
			MaxSize:       100,
			MaxAge:        30,
			MaxBackups:    10,
			Compress:      true,
			DailyRotation: true,
			DateFormat:    "2006-01-02",
			TimeRotation:  false,
			RotationTime:  "00:00",
		},

		Tracing: TracingConfig{
			Enabled:     false,
			Backend:     "jaeger",
			ServiceName: serviceName,
			Version:     version,
			Environment: "production",
			Jaeger: JaegerConfig{
				Endpoint:  "http://localhost:14268/api/traces",
				AgentHost: "localhost",
				AgentPort: 6831,
			},
			Aliyun: AliyunTracingConfig{
				Endpoint:    "",
				Token:       "",
				Project:     serviceName,
				Instance:    "default",
				ServiceName: serviceName,
				ServiceIP:   "127.0.0.1",
				ServicePort: 8080,
			},
			OTLP: OTLPConfig{
				Endpoint: "http://localhost:4317",
				Protocol: "grpc",
				Headers:  make(map[string]string),
			},
			Sampling: SamplingConfig{
				Type:  "parentbased",
				Ratio: 1.0,
			},
		},

		SLS: SLSConfig{
			Enabled:  false,
			Endpoint: "",
			Project:  serviceName,
			Logstore: "app-logs",
			Topic:    "application",
			Source:   serviceName,
		},
	}
}

// WithLogging 配置日志
func (c *Config) WithLogging(level, format, output string) *Config {
	c.Logging.Level = level
	c.Logging.Format = format
	c.Logging.Output = output
	return c
}

// WithTracing 配置链路追踪
func (c *Config) WithTracing(backend string) *Config {
	c.Tracing.Enabled = true
	c.Tracing.Backend = backend
	return c
}

// WithJaeger 配置Jaeger
func (c *Config) WithJaeger(endpoint string) *Config {
	c.Tracing.Enabled = true
	c.Tracing.Backend = "jaeger"
	c.Tracing.Jaeger.Endpoint = endpoint
	return c
}

// WithAliyunTracing 配置阿里云链路追踪
func (c *Config) WithAliyunTracing(endpoint, token, project string) *Config {
	c.Tracing.Enabled = true
	c.Tracing.Backend = "aliyun"
	c.Tracing.Aliyun.Endpoint = endpoint
	c.Tracing.Aliyun.Token = token
	c.Tracing.Aliyun.Project = project
	return c
}

// WithOTLP 配置OTLP
func (c *Config) WithOTLP(endpoint, protocol string) *Config {
	c.Tracing.Enabled = true
	c.Tracing.Backend = "otlp"
	c.Tracing.OTLP.Endpoint = endpoint
	c.Tracing.OTLP.Protocol = protocol
	return c
}

// WithSLS 配置SLS
func (c *Config) WithSLS(endpoint, accessKeyID, accessKeySecret, project, logstore string) *Config {
	c.SLS.Enabled = true
	c.SLS.Endpoint = endpoint
	c.SLS.AccessKeyID = accessKeyID
	c.SLS.AccessKeySecret = accessKeySecret
	c.SLS.Project = project
	c.SLS.Logstore = logstore
	return c
}

// WithEnvironment 设置环境
func (c *Config) WithEnvironment(environment string) *Config {
	c.Environment = environment
	c.Tracing.Environment = environment
	return c
}

// Validate 验证配置
func (c *Config) Validate() error {
	if c.ServiceName == "" {
		return fmt.Errorf("service name cannot be empty")
	}

	// 验证日志配置
	if c.Logging.Enabled {
		if c.Logging.Level != "debug" && c.Logging.Level != "info" && c.Logging.Level != "warn" && c.Logging.Level != "error" {
			return fmt.Errorf("invalid logging level: %s", c.Logging.Level)
		}
		if c.Logging.Format != "json" && c.Logging.Format != "text" {
			return fmt.Errorf("invalid logging format: %s", c.Logging.Format)
		}
		validOutputs := map[string]bool{
			"stdout":     true,
			"file":       true,
			"both":       true,
			"sls":        true,
			"stdout-sls": true,
			"file-sls":   true,
			"both-sls":   true,
		}
		if !validOutputs[c.Logging.Output] {
			return fmt.Errorf("invalid logging output: %s", c.Logging.Output)
		}
	}

	// 验证链路追踪配置
	if c.Tracing.Enabled {
		if c.Tracing.Backend != "jaeger" && c.Tracing.Backend != "aliyun" && c.Tracing.Backend != "otlp" {
			return fmt.Errorf("invalid tracing backend: %s", c.Tracing.Backend)
		}

		// 验证Jaeger配置
		if c.Tracing.Backend == "jaeger" && c.Tracing.Jaeger.Endpoint == "" {
			return fmt.Errorf("jaeger endpoint cannot be empty when jaeger backend is enabled")
		}

		// 验证阿里云配置
		if c.Tracing.Backend == "aliyun" {
			if c.Tracing.Aliyun.Endpoint == "" {
				return fmt.Errorf("aliyun tracing endpoint cannot be empty when aliyun backend is enabled")
			}
			if c.Tracing.Aliyun.Token == "" {
				return fmt.Errorf("aliyun tracing token cannot be empty when aliyun backend is enabled")
			}
		}

		// 验证OTLP配置
		if c.Tracing.Backend == "otlp" {
			if c.Tracing.OTLP.Endpoint == "" {
				return fmt.Errorf("otlp endpoint cannot be empty when otlp backend is enabled")
			}
			if c.Tracing.OTLP.Protocol != "grpc" && c.Tracing.OTLP.Protocol != "http" {
				return fmt.Errorf("otlp protocol must be 'grpc' or 'http'")
			}
		}

		// 验证采样配置
		if c.Tracing.Sampling.Ratio < 0.0 || c.Tracing.Sampling.Ratio > 1.0 {
			return fmt.Errorf("sampling ratio must be between 0.0 and 1.0")
		}
	}

	// 验证SLS配置
	if c.SLS.Enabled {
		if c.SLS.Endpoint == "" {
			return fmt.Errorf("sls endpoint cannot be empty when sls is enabled")
		}
		if c.SLS.AccessKeyID == "" {
			return fmt.Errorf("sls access key id cannot be empty when sls is enabled")
		}
		if c.SLS.AccessKeySecret == "" {
			return fmt.Errorf("sls access key secret cannot be empty when sls is enabled")
		}
		if c.SLS.Project == "" {
			return fmt.Errorf("sls project cannot be empty when sls is enabled")
		}
		if c.SLS.Logstore == "" {
			return fmt.Errorf("sls logstore cannot be empty when sls is enabled")
		}
	}

	return nil
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		ServiceName: "stablepay-service",
		Version:     "1.0.0",
		Environment: "production",

		Logging: LoggingConfig{
			Enabled:       true,
			Level:         "info",
			Format:        "json",
			Output:        "stdout",
			FilePath:      "logs/app.log",
			MaxSize:       100,
			MaxAge:        30,
			MaxBackups:    10,
			Compress:      true,
			DailyRotation: true,
			DateFormat:    "2006-01-02",
			TimeRotation:  false,
			RotationTime:  "00:00",
		},

		Tracing: TracingConfig{
			Enabled:     true,
			Backend:     "jaeger",
			ServiceName: "stablepay-service",
			Version:     "1.0.0",
			Environment: "production",
			Jaeger: JaegerConfig{
				Endpoint:  "http://localhost:14268/api/traces",
				AgentHost: "localhost",
				AgentPort: 6831,
			},
			Aliyun: AliyunTracingConfig{
				Endpoint:    "",
				Token:       "",
				Project:     "stablepay-service",
				Instance:    "default",
				ServiceName: "stablepay-service",
				ServiceIP:   "127.0.0.1",
				ServicePort: 8080,
			},
			OTLP: OTLPConfig{
				Endpoint: "http://localhost:4317",
				Protocol: "grpc",
				Headers:  make(map[string]string),
			},
			Sampling: SamplingConfig{
				Type:  "parentbased",
				Ratio: 1.0,
			},
		},

		SLS: SLSConfig{
			Enabled:  false,
			Endpoint: "",
			Project:  "stablepay-service",
			Logstore: "app-logs",
			Topic:    "application",
			Source:   "stablepay-service",
		},
	}
}
