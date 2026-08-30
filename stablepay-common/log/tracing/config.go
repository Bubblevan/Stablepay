package tracing

import (
	"fmt"
)

// TracingConfig 链路追踪配置
type TracingConfig struct {
	Enabled     bool   `yaml:"enabled" json:"enabled"`           // 是否启用链路追踪
	ServiceName string `yaml:"service_name" json:"service_name"` // 服务名称
	Version     string `yaml:"version" json:"version"`           // 服务版本
	Environment string `yaml:"environment" json:"environment"`   // 环境名称

	// 阿里云链路追踪配置
	AliyunTracing AliyunTracingConfig `yaml:"aliyun_tracing" json:"aliyun_tracing"`

	// Jaeger配置（可选，用于本地开发）
	Jaeger JaegerConfig `yaml:"jaeger" json:"jaeger"`

	// OTLP配置（可选，用于其他后端）
	OTLP OTLPConfig `yaml:"otlp" json:"otlp"`

	// 采样配置
	Sampling SamplingConfig `yaml:"sampling" json:"sampling"`

	// SLS配置
	SLS SLSConfig `yaml:"sls" json:"sls"`
}

// AliyunTracingConfig 阿里云链路追踪配置
type AliyunTracingConfig struct {
	Enabled     bool   `yaml:"enabled"`      // 是否启用阿里云链路追踪
	Endpoint    string `yaml:"endpoint"`     // 阿里云链路追踪服务端点
	Token       string `yaml:"token"`        // 阿里云访问令牌
	Project     string `yaml:"project"`      // 项目名称
	Instance    string `yaml:"instance"`     // 实例名称
	ServiceName string `yaml:"service_name"` // 服务名称
	ServiceIP   string `yaml:"service_ip"`   // 服务IP
	ServicePort int    `yaml:"service_port"` // 服务端口
}

// JaegerConfig Jaeger配置
type JaegerConfig struct {
	Enabled   bool   `yaml:"enabled"`    // 是否启用Jaeger
	Endpoint  string `yaml:"endpoint"`   // Jaeger端点
	AgentHost string `yaml:"agent_host"` // Jaeger Agent主机
	AgentPort int    `yaml:"agent_port"` // Jaeger Agent端口
}

// OTLPConfig OTLP配置
type OTLPConfig struct {
	Enabled  bool              `yaml:"enabled"`  // 是否启用OTLP
	Endpoint string            `yaml:"endpoint"` // OTLP端点
	Protocol string            `yaml:"protocol"` // 协议类型: grpc, http
	Headers  map[string]string `yaml:"headers"`  // 请求头
}

// SamplingConfig 采样配置
type SamplingConfig struct {
	Type  string  `yaml:"type"`  // 采样类型: always, never, traceidratio, parentbased
	Ratio float64 `yaml:"ratio"` // 采样比例 (0.0-1.0)
}

// SLSConfig 阿里云SLS配置
type SLSConfig struct {
	Enabled         bool   `yaml:"enabled"`          // 是否启用SLS
	Endpoint        string `yaml:"endpoint"`         // SLS endpoint
	AccessKeyID     string `yaml:"access_key_id"`    // 访问密钥ID
	AccessKeySecret string `yaml:"access_key_secret"` // 访问密钥Secret
	Project         string `yaml:"project"`          // 项目名称
	Logstore        string `yaml:"logstore"`        // 日志库名称
	Topic           string `yaml:"topic"`           // 日志主题
	Source          string `yaml:"source"`         // 日志来源
}

// NewTracingConfig 创建链路追踪配置（强制用户传入）
func NewTracingConfig(serviceName, version string) *TracingConfig {
	return &TracingConfig{
		Enabled:     true,
		ServiceName: serviceName,
		Version:     version,
		Environment: "production", // 默认环境

		AliyunTracing: AliyunTracingConfig{
			Enabled:     false,
			Endpoint:    "",
			Token:       "",
			Project:     serviceName,
			Instance:    "default",
			ServiceName: serviceName,
			ServiceIP:   "127.0.0.1",
			ServicePort: 8080,
		},

		Jaeger: JaegerConfig{
			Enabled:   false,
			Endpoint:  "http://localhost:14268/api/traces",
			AgentHost: "localhost",
			AgentPort: 6831,
		},

		OTLP: OTLPConfig{
			Enabled:  false,
			Endpoint: "http://localhost:4317",
			Protocol: "grpc",
			Headers:  make(map[string]string),
		},

		Sampling: SamplingConfig{
			Type:  "parentbased",
			Ratio: 1.0,
		},
	}
}

// WithAliyunTracing 配置阿里云链路追踪
func (tc *TracingConfig) WithAliyunTracing(endpoint, token, project string) *TracingConfig {
	tc.AliyunTracing.Enabled = true
	tc.AliyunTracing.Endpoint = endpoint
	tc.AliyunTracing.Token = token
	tc.AliyunTracing.Project = project
	return tc
}

// WithJaeger 配置Jaeger
func (tc *TracingConfig) WithJaeger(endpoint string) *TracingConfig {
	tc.Jaeger.Enabled = true
	tc.Jaeger.Endpoint = endpoint
	return tc
}

// WithOTLP 配置OTLP
func (tc *TracingConfig) WithOTLP(endpoint, protocol string) *TracingConfig {
	tc.OTLP.Enabled = true
	tc.OTLP.Endpoint = endpoint
	tc.OTLP.Protocol = protocol
	return tc
}

// WithSampling 配置采样
func (tc *TracingConfig) WithSampling(samplingType string, ratio float64) *TracingConfig {
	tc.Sampling.Type = samplingType
	tc.Sampling.Ratio = ratio
	return tc
}

// WithEnvironment 设置环境
func (tc *TracingConfig) WithEnvironment(environment string) *TracingConfig {
	tc.Environment = environment
	return tc
}

// WithDisabled 禁用链路追踪
func (tc *TracingConfig) WithDisabled() *TracingConfig {
	tc.Enabled = false
	return tc
}

// Validate 验证配置
func (tc *TracingConfig) Validate() error {
	if tc.ServiceName == "" {
		return fmt.Errorf("service name cannot be empty")
	}

	if tc.Enabled {
		// 检查是否至少启用了一个后端
		if !tc.AliyunTracing.Enabled && !tc.Jaeger.Enabled && !tc.OTLP.Enabled {
			return fmt.Errorf("at least one tracing backend must be enabled")
		}

		// 验证阿里云配置
		if tc.AliyunTracing.Enabled {
			if tc.AliyunTracing.Endpoint == "" {
				return fmt.Errorf("aliyun tracing endpoint cannot be empty when enabled")
			}
			if tc.AliyunTracing.Token == "" {
				return fmt.Errorf("aliyun tracing token cannot be empty when enabled")
			}
		}

		// 验证Jaeger配置
		if tc.Jaeger.Enabled {
			if tc.Jaeger.Endpoint == "" {
				return fmt.Errorf("jaeger endpoint cannot be empty when enabled")
			}
		}

		// 验证OTLP配置
		if tc.OTLP.Enabled {
			if tc.OTLP.Endpoint == "" {
				return fmt.Errorf("otlp endpoint cannot be empty when enabled")
			}
			if tc.OTLP.Protocol != "grpc" && tc.OTLP.Protocol != "http" {
				return fmt.Errorf("otlp protocol must be 'grpc' or 'http'")
			}
		}

		// 验证采样配置
		if tc.Sampling.Ratio < 0.0 || tc.Sampling.Ratio > 1.0 {
			return fmt.Errorf("sampling ratio must be between 0.0 and 1.0")
		}
	}

	return nil
}

// DefaultTracingConfig 默认链路追踪配置
func DefaultTracingConfig() *TracingConfig {
	return &TracingConfig{
		Enabled:     true,
		ServiceName: "stablepay-service",
		Version:     "1.0.0",
		Environment: "production",

		AliyunTracing: AliyunTracingConfig{
			Enabled:     true,
			Endpoint:    "https://tracing-analysis.aliyuncs.com/adapt/your-project-name/api/traces",
			Token:       "",
			Project:     "stablepay-service",
			Instance:    "default",
			ServiceName: "stablepay-service",
			ServiceIP:   "127.0.0.1",
			ServicePort: 8080,
		},

		Jaeger: JaegerConfig{
			Enabled:   false,
			Endpoint:  "http://localhost:14268/api/traces",
			AgentHost: "localhost",
			AgentPort: 6831,
		},

		OTLP: OTLPConfig{
			Enabled:  false,
			Endpoint: "http://localhost:4317",
			Protocol: "grpc",
			Headers:  make(map[string]string),
		},

		Sampling: SamplingConfig{
			Type:  "parentbased",
			Ratio: 1.0,
		},
	}
}
