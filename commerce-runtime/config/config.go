package config

import "os"

type Config struct {
	MySQLDSN       string
	RuntimeVersion string
	LLMProvider    string
	LLMBaseURL     string
	LLMAPIKey      string
	LLMModel       string
}

func FromEnv() Config {
	version := os.Getenv("COMMERCE_RUNTIME_VERSION")
	if version == "" {
		version = "commerce-runtime-mvp.1"
	}
	return Config{MySQLDSN: os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"), RuntimeVersion: version, LLMProvider: os.Getenv("LLM_PROVIDER"), LLMBaseURL: os.Getenv("LLM_BASE_URL"), LLMAPIKey: os.Getenv("LLM_API_KEY"), LLMModel: os.Getenv("LLM_MODEL")}
}
