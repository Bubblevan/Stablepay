// Package main 服务启动入口（COLA架构）
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cloudwego/kitex/server"
	"github.com/stablepay/blockchain-adapter/adapter/rpc"
	"github.com/stablepay/blockchain-adapter/app/service"
	"github.com/stablepay/blockchain-adapter/infrastructure/blockchain"
	"github.com/stablepay/blockchain-adapter/infrastructure/repository"
	"github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/blockchain_adapter/blockchainadapterservice"

	"gopkg.in/yaml.v3"
	"gorm.io/gorm/logger"
)

// Config 配置
type Config struct {
	Server struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"server"`
	Solana struct {
		Network       string `yaml:"network"`
		RPCEndpoint   string `yaml:"rpc_endpoint"`
		HotWalletPath string `yaml:"hotwallet_path"`
		SubsidyRatio  float64 `yaml:"subsidy_ratio"`
	} `yaml:"solana"`
	MySQL struct {
		DSN string `yaml:"dsn"`
	} `yaml:"mysql"`
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "conf/dev.yaml", "配置文件路径")
	flag.Parse()

	log.Println("============================================================")
	log.Println("StablePay Blockchain Adapter Service (COLA Architecture)")
	log.Println("============================================================")

	// 1. 加载配置
	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	log.Printf("✅ 配置加载完成: %s", configPath)

	// 2. 初始化数据库
	db, err := repository.InitDB(cfg.MySQL.DSN, logger.Info)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer repository.CloseDB()
	repository.AutoMigrate(db)
	log.Println("✅ 数据库初始化完成")

	// 3. 初始化基础设施（实现领域网关）
	solanaGateway, err := blockchain.NewSolanaGateway(cfg.Solana.Network, cfg.Solana.RPCEndpoint)
	if err != nil {
		log.Fatalf("初始化 Solana 网关失败: %v", err)
	}

	hotWallet, err := blockchain.NewHotWallet(cfg.Solana.HotWalletPath)
	if err != nil {
		log.Fatalf("加载热钱包失败: %v", err)
	}
	log.Printf("✅ 热钱包加载成功: %s", hotWallet.GetAddress())

	txBuilder := blockchain.NewTransactionBuilder(cfg.Solana.Network)

	subsidyRepo := repository.NewGasSubsidyRepository(db)
	log.Println("✅ 基础设施初始化完成")

	// 4. 初始化应用服务（依赖领域网关接口）
	transferService := service.NewTransferCmdService(solanaGateway, subsidyRepo, hotWallet)
	balanceService := service.NewBalanceQueryService(solanaGateway)
	txStatusService := service.NewTxStatusQueryService(solanaGateway, subsidyRepo)
	buildTxService := service.NewBuildTxService(solanaGateway, txBuilder)
	submitTxService := service.NewSubmitTxService(solanaGateway, subsidyRepo, hotWallet)
	log.Println("✅ 应用服务初始化完成")

	// 5. 初始化适配器（依赖应用服务）
	transferAdapter := rpc.NewTransferRPCAdapter(transferService)
	balanceAdapter := rpc.NewBalanceRPCAdapter(balanceService)
	txStatusAdapter := rpc.NewTxStatusRPCAdapter(txStatusService)
	buildTxAdapter := rpc.NewBuildTxRPCAdapter(buildTxService)
	submitTxAdapter := rpc.NewSubmitTxRPCAdapter(submitTxService)
	rpcHandler := rpc.NewBlockchainAdapterRPC(transferAdapter, balanceAdapter, txStatusAdapter, buildTxAdapter, submitTxAdapter)
	log.Println("✅ 适配器初始化完成")

	// 6. 设置优雅关闭
	setupGracefulShutdown()

	// 7. 启动服务
	addr, _ := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port))
	log.Println("============================================================")
	log.Printf("🚀 服务启动成功，监听 %s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Println("============================================================")

	svr := blockchainadapterservice.NewServer(rpcHandler, server.WithServiceAddr(addr))
	if err := svr.Run(); err != nil {
		log.Fatalf("服务运行失败: %v", err)
	}
}

// loadConfig 从 YAML 文件加载配置
// 支持从文件路径读取配置，如果文件不存在则返回错误
func loadConfig(path string) (*Config, error) {
	// 读取配置文件
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("配置文件不存在: %s, 请创建配置文件或使用默认配置", path)
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 解析 YAML
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// K8s/ACK 等环境可用环境变量覆盖 RPC（例如国内经香港代理访问 devnet）
	if v := strings.TrimSpace(os.Getenv("SOLANA_RPC_ENDPOINT")); v != "" {
		cfg.Solana.RPCEndpoint = v
	}

	// 验证必需配置
	if err := validateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	// 设置默认值
	setDefaultConfig(&cfg)

	return &cfg, nil
}

// validateConfig 验证配置有效性
func validateConfig(cfg *Config) error {
	if cfg.Solana.RPCEndpoint == "" {
		return fmt.Errorf("solana.rpc_endpoint 不能为空")
	}
	if cfg.Solana.HotWalletPath == "" {
		return fmt.Errorf("solana.hotwallet_path 不能为空")
	}
	if cfg.MySQL.DSN == "" {
		return fmt.Errorf("mysql.dsn 不能为空")
	}
	return nil
}

// setDefaultConfig 设置默认配置值
func setDefaultConfig(cfg *Config) {
	if cfg.Solana.Network == "" {
		cfg.Solana.Network = "devnet"
	}
	if cfg.Solana.SubsidyRatio == 0 {
		cfg.Solana.SubsidyRatio = 1.0 // 默认 100% 补贴（热钱包全额支付）
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "0.0.0.0"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8888
	}
}

// setupGracefulShutdown 设置优雅关闭处理
// 捕获系统信号，确保服务可以优雅地关闭
func setupGracefulShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("接收到信号: %v, 开始优雅关闭...", sig)

		// 创建超时上下文
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// 执行清理操作
		select {
		case <-ctx.Done():
			log.Println("优雅关闭超时，强制退出")
		default:
			log.Println("执行清理操作...")
			repository.CloseDB()
			log.Println("✅ 清理完成，服务退出")
		}

		os.Exit(0)
	}()
}
