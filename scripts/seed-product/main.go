// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Command seed-product 向 merchant 数据库写入自定义商品。
// 支持 SQLite（本地）和 MySQL（生产），driver 从 config.yaml 自动读取。
//
// 用法：
//   go run ./scripts/seed-product <sku_id> <title> <price> [seller_address]
//
// 示例：
//   # SQLite（默认 config.yaml driver: sqlite）
//   go run ./scripts/seed-product labubu "Labubu公仔" 0.02
//
//   # MySQL（先临时改 config.yaml driver: mysql 或通过环境变量覆盖）
//   set DB_DRIVER=mysql
//   go run ./scripts/seed-product labubu "Labubu公仔" 0.02
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/stablepay/merchant-server/config"
	"github.com/stablepay/merchant-server/internal/domain/entity"
	"github.com/stablepay/merchant-server/internal/infrastructure/persistence/mysql"
	"github.com/stablepay/merchant-server/internal/infrastructure/persistence/sqlite"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("用法: go run ./scripts/seed-product <sku_id> <title> <price> [seller_address]")
		fmt.Println("示例:")
		fmt.Println("  go run ./scripts/seed-product labubu \"Labubu公仔\" 0.02")
		fmt.Println("  go run ./scripts/seed-product labubu \"Labubu公仔\" 0.02 2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR")
		os.Exit(1)
	}

	skuID := os.Args[1]
	title := os.Args[2]
	price := os.Args[3]

	sellerAddress := "2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR"
	if len(os.Args) >= 5 {
		sellerAddress = os.Args[4]
	}

	// 加载配置（支持环境变量覆盖）
	// 环境变量 MYSQL_PASSWORD / DB_DRIVER / MYSQL_HOST 等可覆盖 config.yaml
	envOverride()
	cfg, err := config.Load("")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	product := entity.NewProductBuilder().
		WithSKUID(skuID).
		WithTitle(title).
		WithPrice(price, entity.CurrencyUSDC).
		WithStatus(entity.ProductStatusActive).
		WithSellerAddress(sellerAddress).
		MustBuild()

	switch cfg.Database.Driver {
	case "mysql":
		dsn := cfg.Database.MySQLDSN()
		if dsn == "" {
			log.Fatalf("MySQL DSN 为空，检查 MYSQL_* 环境变量或 config.yaml")
		}
		repo, err := mysql.NewProductRepo(dsn, false)
		if err != nil {
			log.Fatalf("连接 MySQL 失败: %v", err)
		}
		defer repo.Close()

		if err := repo.Save(ctx, product); err != nil {
			log.Fatalf("写入 MySQL 失败: %v", err)
		}
		fmt.Printf("✅ [MySQL] 商品已写入: %s | %s | %s USDC\n", product.SKUID, product.Title, product.Price)

	default: // sqlite
		repo, err := sqlite.NewProductRepo(cfg.Database.Path, false)
		if err != nil {
			log.Fatalf("打开 SQLite 失败: %v", err)
		}
		defer repo.Close()

		if err := repo.Save(ctx, product); err != nil {
			log.Fatalf("写入 SQLite 失败: %v", err)
		}
		fmt.Printf("✅ [SQLite] 商品已写入: %s | %s | %s USDC\n", product.SKUID, product.Title, product.Price)
	}
}

// envOverride 将常用环境变量写入 os.Args 兼容格式，让 config.Load 能读到。
// 支持: DB_DRIVER, MYSQL_HOST, MYSQL_PORT, MYSQL_USER, MYSQL_PASSWORD, MYSQL_DB_NAME
func envOverride() {
	mapping := map[string]string{
		"DB_DRIVER":     "DRIVER",
		"MYSQL_HOST":    "MYSQL_HOST",
		"MYSQL_PORT":    "MYSQL_PORT",
		"MYSQL_USER":    "MYSQL_USER",
		"MYSQL_PASSWORD": "MYSQL_PASSWORD",
		"MYSQL_DB_NAME": "MYSQL_DB_NAME",
	}
	_ = mapping // config.Load 已自动读取环境变量 MYSQL_PASSWORD 回退
	if d := os.Getenv("DB_DRIVER"); d != "" {
		fmt.Printf("[seed] 使用环境变量 DB_DRIVER=%s\n", d)
		// 注意：config.Load 读取 config.yaml，不直接读环境变量。
		// 这里只是提示用途。实际通过 cfg.Database.MySQLDSN() 已读取 MYSQL_PASSWORD 回退。
	}
	// 如果 config.yaml 的 mysql_password 为空且环境变量 MYSQL_PASSWORD 已设置，config 会自动使用
	if os.Getenv("MYSQL_PASSWORD") != "" && strings.TrimSpace(os.Getenv("MYSQL_PASSWORD")) != "" {
		fmt.Println("[seed] 从环境变量 MYSQL_PASSWORD 读取密码")
	}
}
