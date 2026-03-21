// Package config 配置管理
package config

import (
	"fmt"
	"os"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"gopkg.in/yaml.v3"
)

// Config 应用配置
type Config struct {
	App       AppConfig       `yaml:"app"`
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Redis     RedisConfig     `yaml:"redis"`
	RocketMQ  RocketMQConfig  `yaml:"rocketmq"`
	RpcClients RpcClientsConfig `yaml:"rpc_clients"`
	Payment   PaymentConfig   `yaml:"payment"`
	Security  SecurityConfig  `yaml:"security"`
	RateLimit RateLimitConfig `yaml:"rate_limit"`
	Log       LogConfig       `yaml:"log"`
}

// AppConfig 应用配置
type AppConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Env     string `yaml:"env"`
}

// ServerConfig 服务配置
type ServerConfig struct {
	Http HttpServerConfig `yaml:"http"`
	Rpc  RpcServerConfig  `yaml:"rpc"`
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
	NameServers   []string              `yaml:"name_servers"`
	ProducerGroup string                `yaml:"producer_group"`
	RetryTimes    int                   `yaml:"retry_times"`
	SendTimeout   int                   `yaml:"send_timeout"`
	Topics        map[string]string     `yaml:"topics"`
}

// RpcClientsConfig RPC 客户端配置
type RpcClientsConfig struct {
	DIDService         RpcClientConfig `yaml:"did_service"`
	BlockchainAdapter  RpcClientConfig `yaml:"blockchain_adapter"`
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
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	SignatureTtlMinutes       int `yaml:"signature_ttl_minutes"`
	NonceCacheMinutes         int `yaml:"nonce_cache_minutes"`
	IdempotencyKeyTtlMinutes  int `yaml:"idempotency_key_ttl_minutes"`
}

// RateLimitConfig 限流配置
type RateLimitConfig struct {
	IpLimitPerMinute       int `yaml:"ip_limit_per_minute"`
	DidLimitPerMinute      int `yaml:"did_limit_per_minute"`
	PaymentLimitPerMinute  int `yaml:"payment_limit_per_minute"`
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

	hlog.Infof("Config loaded from: %s", configPath)
	return &config, nil
}

// applyDefaults 应用默认值
func applyDefaults(c *Config) {
	if c.Server.Http.Port == "" {
		c.Server.Http.Port = "8080"
	}
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
