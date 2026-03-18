package main

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB

type TransactionRecord struct {
	ID          uint   `gorm:"primarykey"`
	TxId        string `gorm:"uniqueIndex"`
	AgentDid    string `gorm:"index"`
	SkillDid    string `gorm:"index"`
	AmountMinor int64
	Currency    int32
	TxType      int32  // 1: PURCHASE, 2: REVENUE
	CreatedAt   string
}

func InitDB() {
	db, err := gorm.Open(sqlite.Open("query.db"), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}
	db.AutoMigrate(&TransactionRecord{})
	DB = db
}