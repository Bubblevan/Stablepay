package domain

type AuthMode string

const (
	AuthNone AuthMode = "none"
	AuthDID  AuthMode = "did"
	AuthAPI  AuthMode = "api_key"
)

type RateLimitRule struct {
	IPPerMinute    int `mapstructure:"ip_per_minute" yaml:"ip_per_minute"`
	DIDPerMinute   int `mapstructure:"did_per_minute" yaml:"did_per_minute"`
	RoutePerMinute int `mapstructure:"route_per_minute" yaml:"route_per_minute"`
}

// MachinePayRoute enables a CNY per-call Alipay challenge before this API
// route reaches its normal Kitex downstream. The protocol stays separate from
// x402's PAYMENT-REQUIRED / PAYMENT-SIGNATURE headers.
type MachinePayRoute struct {
	Enabled     bool   `mapstructure:"enabled" yaml:"enabled"`
	ResourceID  string `mapstructure:"resource_id" yaml:"resource_id"`
	GoodsName   string `mapstructure:"goods_name" yaml:"goods_name"`
	AmountMinor int64  `mapstructure:"amount_minor" yaml:"amount_minor"`
}

type RoutePolicy struct {
	Name       string           `mapstructure:"name" yaml:"name"`
	Method     string           `mapstructure:"method" yaml:"method"`
	Path       string           `mapstructure:"path" yaml:"path"`
	Target     string           `mapstructure:"target" yaml:"target"`
	TimeoutMS  int              `mapstructure:"timeout_ms" yaml:"timeout_ms"`
	Retry      int              `mapstructure:"retry" yaml:"retry"`
	Auth       AuthMode         `mapstructure:"auth" yaml:"auth"`
	RateLimits RateLimitRule    `mapstructure:"rate_limits" yaml:"rate_limits"`
	MachinePay *MachinePayRoute `mapstructure:"machine_pay" yaml:"machine_pay"`
}
