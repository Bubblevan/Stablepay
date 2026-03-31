package mysql

import (
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/stablepay/payment-service/internal/domain/entity"
	"github.com/stablepay/payment-service/internal/infrastructure/config"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// NewMySQLConnection creates the payment-service GORM connection.
func NewMySQLConnection(cfg *config.Config) (*gorm.DB, error) {
	dsn := cfg.GetMySQLDSN()

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

// AutoMigrate ensures the payment tables exist in local/dev Docker runs.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&entity.Payment{},
		&entity.PaymentIdempotency{},
		&entity.BlockchainCallback{},
	); err != nil {
		return fmt.Errorf("failed to auto migrate payment tables: %w", err)
	}

	hlog.Info("AutoMigrate completed")
	return nil
}
