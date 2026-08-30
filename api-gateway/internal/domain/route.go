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

type RoutePolicy struct {
	Name       string        `mapstructure:"name" yaml:"name"`
	Method     string        `mapstructure:"method" yaml:"method"`
	Path       string        `mapstructure:"path" yaml:"path"`
	Target     string        `mapstructure:"target" yaml:"target"`
	TimeoutMS  int           `mapstructure:"timeout_ms" yaml:"timeout_ms"`
	Retry      int           `mapstructure:"retry" yaml:"retry"`
	Auth       AuthMode      `mapstructure:"auth" yaml:"auth"`
	RateLimits RateLimitRule `mapstructure:"rate_limits" yaml:"rate_limits"`
}
