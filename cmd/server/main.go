// Package main 服务入口
// DID Service MVP - Kitex RPC Server
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cloudwego/kitex/server"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/stablepay/did-service/adapter"
	"github.com/stablepay/did-service/app"
	"github.com/stablepay/did-service/infrastructure/config"
	"github.com/stablepay/did-service/infrastructure/repository"
	"github.com/stablepay/did-service/kitex_gen/stablepay/did_service/didservice"
)

func main() {
	// 1. 加载配置
	cfg, err := config.Load("conf/dev.yaml")
	if err != nil {
		// 如果配置文件不存在，使用默认配置
		log.Printf("Config file not found, using defaults: %v", err)
		cfg = &config.Config{
			Server: config.ServerConfig{
				Host: "0.0.0.0",
				Port: 8081,
			},
			Log: config.LogConfig{
				Level:  "info",
				Format: "json",
			},
			Database: config.DatabaseConfig{
				Host:            "127.0.0.1",
				Port:            3306,
				User:            "root",
				Password:        "password",
				DBName:          "did_service",
				Charset:         "utf8mb4",
				MaxOpenConns:    20,
				MaxIdleConns:    10,
				ConnMaxLifetime: 3600,
			},
		}
	}

	// 2. 初始化基础设施层
	// repo := repository.NewMemoryDIDRepository() // 使用内存存储时取消注释

	// 从配置构建MySQL连接DSN (Data Source Name)
	dsn := cfg.GetMySQLDSN()
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to MySQL: %v", err)
	}

	// 设置连接池
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetime) * time.Second)

	// 自动迁移表结构
	if err := db.AutoMigrate(&repository.DidIdentityModel{}); err != nil {
		log.Printf("Warning: Failed to auto migrate: %v", err)
	}

	repo := repository.NewDBDIDRepository(db)

	// 3. 初始化应用层
	appService := app.NewDIDAppService(repo)

	// 4. 初始化适配器层
	handler := adapter.NewDIDHandler(appService)

	// 5. 创建Kitex服务
	addr, _ := net.ResolveTCPAddr("tcp", cfg.GetAddr())
	srv := didservice.NewServer(handler,
		server.WithServiceAddr(addr),
	)

	// 6. 关闭
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down server...")
		cancel()
		srv.Stop()
	}()

	// 7. 启动服务
	log.Printf("DID Service starting on %s", cfg.GetAddr())
	log.Printf("Using memory storage (MVP mode)")

	if err := srv.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}

	<-ctx.Done()
	fmt.Println("DID Service stopped")
}
