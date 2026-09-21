package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	MySQLDSN              string
	RuntimeVersion        string
	LLMProvider           string
	LLMBaseURL            string
	LLMAPIKey             string
	LLMModel              string
	HTTPAddr              string
	APIToken              string
	AllowInsecure         bool
	PaymentServiceAddr    string
	DIDServiceAddr        string
	BlockchainAdapterAddr string
	AgentKeypairPath      string
	GatewayBaseURL        string
	GatewayAPIKey         string
	MerchantTimeout       time.Duration
	MaxRunnerSteps        int
	SupervisorInterval    time.Duration
	RunnerRetryMax        time.Duration
}

func FromEnv() Config {
	version := os.Getenv("COMMERCE_RUNTIME_VERSION")
	if version == "" {
		version = "commerce-runtime-mvp.1"
	}
	addr := firstNonEmpty(os.Getenv("COMMERCE_RUNTIME_HTTP_ADDR"), ":8090")
	merchantTimeout := durationEnv("COMMERCE_RUNTIME_MERCHANT_TIMEOUT", 20*time.Second)
	maxSteps := intEnv("COMMERCE_RUNTIME_MAX_RUNNER_STEPS", 64)
	supervisorInterval := durationEnv("COMMERCE_RUNTIME_SUPERVISOR_INTERVAL", 2*time.Second)
	runnerRetryMax := durationEnv("COMMERCE_RUNTIME_RUNNER_RETRY_MAX", 30*time.Second)
	return Config{
		MySQLDSN: os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"), RuntimeVersion: version,
		LLMProvider: firstNonEmpty(os.Getenv("LLM_PROVIDER"), "deepseek"), LLMBaseURL: os.Getenv("LLM_BASE_URL"), LLMAPIKey: os.Getenv("LLM_API_KEY"), LLMModel: firstNonEmpty(os.Getenv("LLM_MODEL"), os.Getenv("LLM_MODEL_ID")),
		HTTPAddr: addr, APIToken: strings.TrimSpace(os.Getenv("COMMERCE_RUNTIME_API_TOKEN")), AllowInsecure: strings.EqualFold(os.Getenv("COMMERCE_RUNTIME_ALLOW_INSECURE"), "true"),
		PaymentServiceAddr: firstNonEmpty(os.Getenv("STABLEPAY_PAYMENT_SERVICE_ADDR"), "127.0.0.1:8888"), DIDServiceAddr: firstNonEmpty(os.Getenv("STABLEPAY_DID_SERVICE_ADDR"), "127.0.0.1:8081"), BlockchainAdapterAddr: firstNonEmpty(os.Getenv("STABLEPAY_BLOCKCHAIN_ADAPTER_ADDR"), "127.0.0.1:8083"), AgentKeypairPath: os.Getenv("STABLEPAY_E2E_AGENT_KEYPAIR_PATH"),
		GatewayBaseURL: firstNonEmpty(os.Getenv("STABLEPAY_GATEWAY_BASE_URL"), "http://127.0.0.1:8080"), GatewayAPIKey: firstNonEmpty(os.Getenv("STABLEPAY_API_KEY"), "stablepay-dev-key"), MerchantTimeout: merchantTimeout, MaxRunnerSteps: maxSteps, SupervisorInterval: supervisorInterval, RunnerRetryMax: runnerRetryMax,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func intEnv(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
