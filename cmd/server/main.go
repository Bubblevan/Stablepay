// Package main 是 Blockchain Adapter 服务的启动入口
// 整合所有组件并启动 Kitex RPC 服务
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"gorm.io/gorm/logger"

	"stablepay.blockchain_adapter/data-access-layer/db"
	internalsolana "stablepay.blockchain_adapter/internal/solana"
	"stablepay.blockchain_adapter/internal/handler"
	"stablepay.blockchain_adapter/internal/service"
	"stablepay.blockchain_adapter/kitex_gen/stablepay/blockchain_adapter/blockchainadapterservice"

	"gopkg.in/yaml.v3"
)

// Config 服务配置
type Config struct {
	Solana SolanaConfig `yaml:"solana"`
	MySQL  MySQLConfig  `yaml:"mysql"`
}

// SolanaConfig Solana 配置
type SolanaConfig struct {
	Network      string `yaml:"network"`
	RPCEndpoint  string `yaml:"rpc_endpoint"`
	WSEndpoint   string `yaml:"ws_endpoint"`
	HotWalletPath string `yaml:"hotwallet_path"`
}

// MySQLConfig MySQL 配置
type MySQLConfig struct {
	DSN string `yaml:"dsn"`
}

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "conf/dev.yaml", "配置文件路径")
	flag.Parse()

	log.Println("============================================================")
	log.Println("StablePay Blockchain Adapter Service")
	log.Println("============================================================")

	// 1. 加载配置
	log.Println("[1/6] 加载配置...")
	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	log.Printf("    网络: %s", cfg.Solana.Network)
	log.Printf("    RPC: %s", cfg.Solana.RPCEndpoint)

	// 2. 初始化数据库
	log.Println("[2/6] 初始化数据库...")
	database, err := db.Init(cfg.MySQL.DSN, logger.Info)
	if err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}
	defer db.Close()
	log.Println("    数据库连接成功")

	// 自动迁移表结构
	if err := db.AutoMigrate(database); err != nil {
		log.Fatalf("数据库迁移失败: %v", err)
	}
	log.Println("    表结构迁移完成")

	// 3. 初始化 Solana 客户端
	log.Println("[3/6] 初始化 Solana 客户端...")
	solanaClient, err := internalsolana.NewClient(cfg.Solana.Network)
	if err != nil {
		log.Fatalf("初始化 Solana 客户端失败: %v", err)
	}
	log.Printf("    连接到: %s", solanaClient.GetEndpoint())

	// 4. 加载热钱包
	log.Println("[4/6] 加载热钱包...")
	hotWalletPath := cfg.Solana.HotWalletPath
	if hotWalletPath == "" {
		hotWalletPath = "conf/hotwallet.json"
	}
	hotWallet, err := internalsolana.LoadHotWallet(hotWalletPath)
	if err != nil {
		log.Fatalf("加载热钱包失败: %v", err)
	}
	// 验证热钱包
	if err := hotWallet.Validate(); err != nil {
		log.Fatalf("热钱包验证失败: %v", err)
	}
	log.Printf("    地址: %s", hotWallet.Address)
	log.Printf("    DID: %s", hotWallet.DID)
	log.Printf("    角色: %s", hotWallet.Role)

	// 5. 初始化 DAL 和 Service
	log.Println("[5/6] 初始化服务层...")

	// DAL
	subsidyDAL := db.NewGasSubsidyDAL(database)

	// Services
	transferSvc := service.NewTransferService(solanaClient, hotWallet, subsidyDAL)
	balanceSvc := service.NewBalanceService(solanaClient)
	txStatusSvc := service.NewTxStatusService(solanaClient, subsidyDAL)

	// Handlers
	transferHandler := handler.NewTransferHandler(transferSvc)
	balanceHandler := handler.NewBalanceHandler(balanceSvc)
	txStatusHandler := handler.NewTxStatusHandler(txStatusSvc)

	// 聚合 Handler
	adapterHandler := handler.NewBlockchainAdapterHandler(
		transferHandler,
		balanceHandler,
		txStatusHandler,
	)

	log.Println("    Transfer Service: OK")
	log.Println("    Balance Service: OK")
	log.Println("    TxStatus Service: OK")

	// 6. 启动 Kitex 服务
	log.Println("[6/6] 启动 RPC 服务...")
	log.Println("============================================================")
	log.Println("✅ Blockchain Adapter 服务已启动")
	log.Println("============================================================")
	log.Println()
	log.Println("RPC 接口:")
	log.Println("  - TransferStableCoin: 执行稳定币转账并代付 Gas")
	log.Println("  - GetBalance: 查询钱包余额")
	log.Println("  - GetTxStatus: 查询交易状态")
	log.Println()
	log.Println("按 Ctrl+C 停止服务")
	log.Println()

	// 创建并运行 Kitex 服务
	svr := blockchainadapterservice.NewServer(adapterHandler)
	if err := svr.Run(); err != nil {
		log.Fatalf("服务运行失败: %v", err)
	}
}

// loadConfig 加载配置文件
func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 设置默认值
	if cfg.Solana.Network == "" {
		cfg.Solana.Network = "devnet"
	}
	if cfg.Solana.HotWalletPath == "" {
		cfg.Solana.HotWalletPath = "conf/hotwallet.json"
	}

	return &cfg, nil
}
