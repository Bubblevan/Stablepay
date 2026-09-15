package config

import "os"

type Config struct {
	MySQLDSN       string
	RuntimeVersion string
}

func FromEnv() Config {
	version := os.Getenv("COMMERCE_RUNTIME_VERSION")
	if version == "" {
		version = "commerce-runtime-mvp.1"
	}
	return Config{MySQLDSN: os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"), RuntimeVersion: version}
}
