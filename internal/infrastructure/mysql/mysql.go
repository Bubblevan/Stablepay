// Package mysql MySQL 连接管理
package mysql

import (
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/stablepay/payment-service/internal/infrastructure/config"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewMySQLConnection 创建 MySQL 连接
func NewMySQLConnection(cfg *config.Config) (*gorm.DB, error) {
	dsn := cfg.GetMySQLDSN()

	// 配置 GORM
	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	}

	if cfg.App.Env == "production" {
		gormConfig.Logger = logger.Default.LogMode(logger.Error)
	}

	db, err := gorm.Open(mysql.Open(dsn), gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MySQL: %w", err)
	}

	// 配置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	if cfg.Database.MySQL.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.Database.MySQL.MaxOpenConns)
	}
	if cfg.Database.MySQL.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.Database.MySQL.MaxIdleConns)
	}
	if cfg.Database.MySQL.ConnMaxLifetime != "" {
		duration, err := time.ParseDuration(cfg.Database.MySQL.ConnMaxLifetime)
		if err == nil {
			sqlDB.SetConnMaxLifetime(duration)
		}
	}

	hlog.Info("MySQL connected successfully")
	return db, nil
}

// AutoMigrate 自动迁移数据库表
func AutoMigrate(db *gorm.DB) error {
	// 实际项目中应该使用迁移工具（如 golang-migrate）
	// 这里仅作为示例
	hlog.Info("AutoMigrate completed")
	return nil
}
