package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
	"github.com/stablepay/api-gateway/internal/domain"
)

type Server struct {
	Address          string `mapstructure:"address" yaml:"address"`
	ReadTimeoutMS    int    `mapstructure:"read_timeout_ms" yaml:"read_timeout_ms"`
	WriteTimeoutMS   int    `mapstructure:"write_timeout_ms" yaml:"write_timeout_ms"`
	EnableAccessLog  bool   `mapstructure:"enable_access_log" yaml:"enable_access_log"`
	GracefulStopSecs int    `mapstructure:"graceful_stop_secs" yaml:"graceful_stop_secs"`
}

type Security struct {
	AllowedAPIKeys        []string `mapstructure:"allowed_api_keys" yaml:"allowed_api_keys"`
	AllowedTimestampSkewS int      `mapstructure:"allowed_timestamp_skew_sec" yaml:"allowed_timestamp_skew_sec"`
	RequireNonce          bool     `mapstructure:"require_nonce" yaml:"require_nonce"`
	CanonicalVersion      string   `mapstructure:"canonical_version" yaml:"canonical_version"`
}

type Redis struct {
	Enabled  bool   `mapstructure:"enabled" yaml:"enabled"`
	Addr     string `mapstructure:"addr" yaml:"addr"`
	Password string `mapstructure:"password" yaml:"password"`
	DB       int    `mapstructure:"db" yaml:"db"`
}

type Resilience struct {
	DefaultTimeoutMS int `mapstructure:"default_timeout_ms" yaml:"default_timeout_ms"`
	DefaultRetry     int `mapstructure:"default_retry" yaml:"default_retry"`
}

type Logging struct {
	Level           string `mapstructure:"level" yaml:"level"`
	Format          string `mapstructure:"format" yaml:"format"`
	Output          string `mapstructure:"output" yaml:"output"`
	FilePath        string `mapstructure:"file_path" yaml:"file_path"`
	EnableTracing   bool   `mapstructure:"enable_tracing" yaml:"enable_tracing"`
	TracingBackend  string `mapstructure:"tracing_backend" yaml:"tracing_backend"`
	TracingEndpoint string `mapstructure:"tracing_endpoint" yaml:"tracing_endpoint"`
	Environment     string `mapstructure:"environment" yaml:"environment"`
}

type Downstream struct {
	DIDService              string `mapstructure:"did_service" yaml:"did_service"`
	PaymentService          string `mapstructure:"payment_service" yaml:"payment_service"`
	VerificationService     string `mapstructure:"verification_service" yaml:"verification_service"`
	QueryService            string `mapstructure:"query_service" yaml:"query_service"`
	DIDServiceAddr          string `mapstructure:"did_service_addr" yaml:"did_service_addr"`
	PaymentServiceAddr      string `mapstructure:"payment_service_addr" yaml:"payment_service_addr"`
	VerificationServiceAddr string `mapstructure:"verification_service_addr" yaml:"verification_service_addr"`
	QueryServiceAddr        string `mapstructure:"query_service_addr" yaml:"query_service_addr"`
}

type AppConfig struct {
	Server     Server               `mapstructure:"server" yaml:"server"`
	Security   Security             `mapstructure:"security" yaml:"security"`
	Redis      Redis                `mapstructure:"redis" yaml:"redis"`
	Resilience Resilience           `mapstructure:"resilience" yaml:"resilience"`
	Logging    Logging              `mapstructure:"logging" yaml:"logging"`
	Downstream Downstream           `mapstructure:"downstream" yaml:"downstream"`
	Routes     []domain.RoutePolicy `mapstructure:"routes" yaml:"routes"`
}

func Load(configPath string) (*AppConfig, error) {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	v.SetEnvPrefix("STABLEPAY")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := &AppConfig{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if cfg.Server.Address == "" {
		cfg.Server.Address = ":8080"
	}
	if cfg.Security.CanonicalVersion == "" {
		cfg.Security.CanonicalVersion = "v0.1"
	}
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "json"
	}
	if cfg.Logging.Output == "" {
		cfg.Logging.Output = "stdout"
	}
	if cfg.Logging.FilePath == "" {
		cfg.Logging.FilePath = "logs/api-gateway.log"
	}
	if cfg.Logging.Environment == "" {
		cfg.Logging.Environment = "local"
	}
	if cfg.Logging.TracingBackend == "" {
		cfg.Logging.TracingBackend = "otlp"
	}
	return cfg, nil
}
