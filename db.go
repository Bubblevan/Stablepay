package main

import (
	"fmt"
	"os"
	"strconv"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var DB *gorm.DB

type TransactionRecord struct {
	ID          uint   `gorm:"primarykey"`
	TxId        string `gorm:"uniqueIndex;type:varchar(191)"`
	SourceTxId  string `gorm:"index;type:varchar(191);column:source_tx_id"`
	AgentDid    string `gorm:"index"`
	SkillDid    string `gorm:"index"`
	AmountMinor int64
	Currency    int32
	TxType      int32  // 1: PURCHASE, 2: REVENUE
	CreatedAt   string `gorm:"type:varchar(191)"`
}

func InitDB() {
	mysqlHost := getenvDefault("MYSQL_HOST", "stablepay-mysql")
	mysqlPort := getenvIntDefault("MYSQL_PORT", 3306)
	mysqlUser := getenvDefault("MYSQL_USER", "stablepay")
	mysqlPassword := getenvDefault("MYSQL_PASSWORD", "stablepay123")
	mysqlDBName := getenvDefault("MYSQL_DBNAME", "stablepay_query_db")

	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		mysqlUser,
		mysqlPassword,
		mysqlHost,
		mysqlPort,
		mysqlDBName,
	)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		panic(fmt.Errorf("failed to connect mysql: %w", err))
	}
	db.AutoMigrate(&TransactionRecord{})
	DB = db
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvIntDefault(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return i
}
