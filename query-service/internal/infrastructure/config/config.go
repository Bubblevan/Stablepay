package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	RPCAddress        string
	MySQLDSN          string
	SolanaRPCEndpoint string
	USDCMint          string
	MonthlyLimitMinor int64
}

func Load() Config {
	return Config{
		RPCAddress:        env("QUERY_RPC_ADDRESS", ":8084"),
		MySQLDSN:          env("QUERY_MYSQL_DSN", mysqlDSN()),
		SolanaRPCEndpoint: env("SOLANA_RPC_ENDPOINT", "https://api.devnet.solana.com"),
		USDCMint:          env("QUERY_BALANCE_USDC_MINT", "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"),
		MonthlyLimitMinor: envInt64("QUERY_MONTHLY_LIMIT_MINOR", 50_000*1_000_000),
	}
}

func mysqlDSN() string {
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		env("MYSQL_USER", "stablepay"), env("MYSQL_PASSWORD", "stablepay123"),
		env("MYSQL_HOST", "stablepay-mysql"), env("MYSQL_PORT", "3306"), env("MYSQL_DBNAME", "stablepay_query_db"))
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	value, err := strconv.ParseInt(env(key, ""), 10, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
