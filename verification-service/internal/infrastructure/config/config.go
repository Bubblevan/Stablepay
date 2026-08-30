package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	RPCAddress        string
	MySQLDSN          string
	RocketNameservers []string
	RocketGroup       string
	RocketTopic       string
}

func Load() Config {
	nameservers := make([]string, 0)
	for _, value := range strings.Split(env("ROCKETMQ_NAMESERVER", "127.0.0.1:9876"), ",") {
		if value = strings.TrimSpace(value); value != "" {
			nameservers = append(nameservers, value)
		}
	}
	return Config{
		RPCAddress:        env("VERIFICATION_RPC_ADDRESS", ":8085"),
		MySQLDSN:          env("VERIFICATION_MYSQL_DSN", mysqlDSN()),
		RocketNameservers: nameservers,
		RocketGroup:       env("VERIFICATION_ROCKETMQ_GROUP", "verification_group"),
		RocketTopic:       env("VERIFICATION_ROCKETMQ_TOPIC", "payment_events"),
	}
}

func mysqlDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", env("MYSQL_USER", "stablepay"), env("MYSQL_PASSWORD", "stablepay123"), env("MYSQL_HOST", "stablepay-mysql"), env("MYSQL_PORT", "3306"), env("MYSQL_DBNAME", "stablepay_verification_db"))
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
