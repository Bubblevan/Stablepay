// Package config 配置管理
package config

import (
	"fmt"
	"log"
	"os"

	"github.com/stablepay/payment-service/pkg/constants"
	"gopkg.in/yaml.v3"
)

// Config 应用配置
type Config struct {
	App          AppConfig          `yaml:"app"`
	Server       ServerConfig       `yaml:"server"`
	Database     DatabaseConfig     `yaml:"database"`
	Redis        RedisConfig        `yaml:"redis"`
	RocketMQ     RocketMQConfig     `yaml:"rocketmq"`
	RpcClients   RpcClientsConfig   `yaml:"rpc_clients"`
	Payment      PaymentConfig      `yaml:"payment"`
	AgentHarness AgentHarnessConfig `yaml:"agent_harness"`
	Security     SecurityConfig     `yaml:"security"`
	RateLimit    RateLimitConfig    `yaml:"rate_limit"`
	Log          LogConfig          `yaml:"log"`
}

// AppConfig 应用配置
type AppConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Env     string `yaml:"env"`
}

// ServerConfig 服务配置
type ServerConfig struct {
	Rpc RpcServerConfig `yaml:"rpc"`
}

// HttpServerConfig HTTP 服务配置
type HttpServerConfig struct {
	Port         string `yaml:"port"`
	ReadTimeout  string `yaml:"read_timeout"`
	WriteTimeout string `yaml:"write_timeout"`
}

// RpcServerConfig RPC 服务配置
type RpcServerConfig struct {
	Port    string `yaml:"port"`
	Timeout string `yaml:"timeout"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	MySQL MySQLConfig `yaml:"mysql"`
}

// MySQLConfig MySQL 配置
type MySQLConfig struct {
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	Database        string `yaml:"database"`
	Charset         string `yaml:"charset"`
	MaxOpenConns    int    `yaml:"max_open_conns"`
	MaxIdleConns    int    `yaml:"max_idle_conns"`
	ConnMaxLifetime string `yaml:"conn_max_lifetime"`
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	Password     string `yaml:"password"`
	DB           int    `yaml:"db"`
	PoolSize     int    `yaml:"pool_size"`
	MinIdleConns int    `yaml:"min_idle_conns"`
}

// RocketMQConfig RocketMQ 配置
type RocketMQConfig struct {
	NameServers   []string          `yaml:"name_servers"`
	ProducerGroup string            `yaml:"producer_group"`
	RetryTimes    int               `yaml:"retry_times"`
	SendTimeout   int               `yaml:"send_timeout"`
	Topics        map[string]string `yaml:"topics"`
}

// RpcClientsConfig RPC 客户端配置
type RpcClientsConfig struct {
	DIDService        RpcClientConfig `yaml:"did_service"`
	BlockchainAdapter RpcClientConfig `yaml:"blockchain_adapter"`
}

// RpcClientConfig RPC 客户端配置
type RpcClientConfig struct {
	Address    string `yaml:"address"`
	TimeoutMs  int    `yaml:"timeout_ms"`
	RetryCount int    `yaml:"retry_count"`
}

// PaymentConfig 支付业务配置
type PaymentConfig struct {
	TimeoutMinutes      int      `yaml:"timeout_minutes"`
	MaxRetryCount       int      `yaml:"max_retry_count"`
	MaxAmountUsdc       string   `yaml:"max_amount_usdc"`
	PollIntervalSeconds int      `yaml:"poll_interval_seconds"`
	MaxPollCount        int      `yaml:"max_poll_count"`
	SupportedCurrencies []string `yaml:"supported_currencies"`
	Decimals            int      `yaml:"decimals"`

	// TreasuryWalletAddress 系统金库钱包地址。X 注册奖励从此地址向用户钱包发 USDC。
	// 必填,空时 /api/v1/internal/rewards/x-registration 直接返回 500。
	TreasuryWalletAddress string `yaml:"treasury_wallet_address"`

	// XRegistrationRewardUsdc 单笔 X 注册奖励的 USDC 数额（字符串，支持小数，如 "1.0"）。
	// 优先级低于调用方在请求体里传过来的 amount,但作为兜底默认。
	XRegistrationRewardUsdc string `yaml:"x_registration_reward_usdc"`
}

// AgentHarnessConfig is opt-in during migration. Enabling require_intent_for_payment
// turns policy decisions into a mandatory server-side gate before transfer.
type AgentHarnessConfig struct {
	Enabled                 bool     `yaml:"enabled"`
	RequireIntentForPayment bool     `yaml:"require_intent_for_payment"`
	AutoApproveMaxUsdc      string   `yaml:"auto_approve_max_usdc"`
	MaxIntentAmountUsdc     string   `yaml:"max_intent_amount_usdc"`
	IntentTTLSeconds        int      `yaml:"intent_ttl_seconds"`
	PolicyVersion           string   `yaml:"policy_version"`
	AllowedSkillDIDs        []string `yaml:"allowed_skill_dids"`
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	SignatureTtlMinutes      int `yaml:"signature_ttl_minutes"`
	NonceCacheMinutes        int `yaml:"nonce_cache_minutes"`
	IdempotencyKeyTtlMinutes int `yaml:"idempotency_key_ttl_minutes"`

	// InternalApiKey internal 端点共享密钥（如 /api/v1/internal/rewards/...）。
	// 必填,空时 internal 端点直接 403。
	InternalApiKey string `yaml:"internal_api_key"`
}

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	IpLimitPerMinute      int `yaml:"ip_limit_per_minute"`
	DidLimitPerMinute     int `yaml:"did_limit_per_minute"`
	PaymentLimitPerMinute int `yaml:"payment_limit_per_minute"`
}

// LogConfig 日志配置
type LogConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"`
	Output     string `yaml:"output"`
	FilePath   string `yaml:"file_path"`
	MaxSize    int    `yaml:"max_size"`
	MaxAge     int    `yaml:"max_age"`
	MaxBackups int    `yaml:"max_backups"`
}

// LoadConfig 加载配置文件
func LoadConfig(configPath string) (*Config, error) {
	if configPath == "" {
		configPath = "config/config.yaml"
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// 应用默认值
	applyDefaults(&config)

	// 允许 env 覆盖关键配置（生产环境通过 secret manager 注入）
	applyEnvOverrides(&config)
	if err := config.Validate(); err != nil {
		return nil, err
	}

	log.Printf("config loaded from: %s", configPath)
	return &config, nil
}

// applyEnvOverrides 用环境变量覆盖敏感 / 部署相关字段
func applyEnvOverrides(c *Config) {
	if v := os.Getenv("PAYMENT_ROCKETMQ_TOPIC"); v != "" {
		if c.RocketMQ.Topics == nil {
			c.RocketMQ.Topics = map[string]string{}
		}
		c.RocketMQ.Topics["payment_events"] = v
	}
	if v := os.Getenv("TREASURY_WALLET_ADDRESS"); v != "" {
		c.Payment.TreasuryWalletAddress = v
	}
	if v := os.Getenv("X_REGISTRATION_REWARD_USDC"); v != "" {
		c.Payment.XRegistrationRewardUsdc = v
	}
	if v := os.Getenv("INTERNAL_API_KEY"); v != "" {
		c.Security.InternalApiKey = v
	}
}

// Validate enforces the local deterministic-plane contract. A producer using a
// different physical topic can start successfully but can never close the
// payment -> verification loop, so fail before connecting to dependencies.
func (c *Config) Validate() error {
	topic := ""
	if c.RocketMQ.Topics != nil {
		topic = c.RocketMQ.Topics["payment_events"]
	}
	if topic != constants.MQTopicPaymentEvents {
		return fmt.Errorf("rocketmq.topics.payment_events must be %q, got %q", constants.MQTopicPaymentEvents, topic)
	}
	if len(c.RocketMQ.NameServers) == 0 {
		return fmt.Errorf("rocketmq.name_servers must not be empty")
	}
	return nil
}

// applyDefaults 应用默认值
func applyDefaults(c *Config) {
	if c.Server.Rpc.Port == "" {
		c.Server.Rpc.Port = "8888"
	}
	if c.Payment.TimeoutMinutes == 0 {
		c.Payment.TimeoutMinutes = 5
	}
	if c.Payment.MaxRetryCount == 0 {
		c.Payment.MaxRetryCount = 3
	}
	if c.Payment.MaxAmountUsdc == "" {
		c.Payment.MaxAmountUsdc = "1000.00"
	}
	if c.Payment.PollIntervalSeconds == 0 {
		c.Payment.PollIntervalSeconds = 3
	}
	if c.Payment.MaxPollCount == 0 {
		c.Payment.MaxPollCount = 20
	}
	if c.Security.SignatureTtlMinutes == 0 {
		c.Security.SignatureTtlMinutes = 5
	}
	if c.Security.NonceCacheMinutes == 0 {
		c.Security.NonceCacheMinutes = 10
	}
	if c.Security.IdempotencyKeyTtlMinutes == 0 {
		c.Security.IdempotencyKeyTtlMinutes = 30
	}
	if c.Payment.XRegistrationRewardUsdc == "" {
		c.Payment.XRegistrationRewardUsdc = "1.0"
	}
	if c.AgentHarness.AutoApproveMaxUsdc == "" {
		c.AgentHarness.AutoApproveMaxUsdc = "5.00"
	}
	if c.AgentHarness.MaxIntentAmountUsdc == "" {
		c.AgentHarness.MaxIntentAmountUsdc = c.Payment.MaxAmountUsdc
	}
	if c.AgentHarness.IntentTTLSeconds == 0 {
		c.AgentHarness.IntentTTLSeconds = 300
	}
}

// GetMySQLDSN 获取 MySQL DSN
func (c *Config) GetMySQLDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		c.Database.MySQL.Username,
		c.Database.MySQL.Password,
		c.Database.MySQL.Host,
		c.Database.MySQL.Port,
		c.Database.MySQL.Database,
		c.Database.MySQL.Charset,
	)
}

// GetRedisAddr 获取 Redis 地址
func (c *Config) GetRedisAddr() string {
	return fmt.Sprintf("%s:%d", c.Redis.Host, c.Redis.Port)
}
