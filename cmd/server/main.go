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
			Encryption: config.EncryptionConfig{
				Key: "0123456789abcdef0123456789abcdef", // 默认32字节密钥（AES-256）
			},
		}
	}

	// 2. 初始化基础设施层（使用MySQL存储）
	dsn := cfg.GetMySQLDSN()
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to MySQL: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(cfg.Database.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.Database.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.Database.ConnMaxLifetime) * time.Second)
	if err := db.AutoMigrate(&repository.DidIdentityModel{}); err != nil {
		log.Printf("Warning: Failed to auto migrate: %v", err)
	}
	repo := repository.NewDBDIDRepository(db)
	log.Println("Using MySQL storage")

	// 3. 初始化应用层
	appService, err := app.NewDIDAppService(repo, cfg.Encryption.Key)
	if err != nil {
		log.Fatalf("Failed to create app service: %v", err)
	}

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
