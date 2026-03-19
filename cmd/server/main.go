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

	"github.com/cloudwego/kitex/server"

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
		}
	}

	// 2. 初始化基础设施层
	repo := repository.NewMemoryDIDRepository()

	// 3. 初始化应用层
	appService := app.NewDIDAppService(repo)

	// 4. 初始化适配器层
	handler := adapter.NewDIDHandler(appService)

	// 5. 创建Kitex服务
	addr, _ := net.ResolveTCPAddr("tcp", cfg.GetAddr())
	srv := didservice.NewServer(handler,
		server.WithServiceAddr(addr),
	)

	// 6. 优雅关闭
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
