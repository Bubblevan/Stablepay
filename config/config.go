// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package config 提供全局配置的加载与访问。
//
// 遵循 COLA 架构的 Infrastructure 层设计原则：
// - 配置加载属于基础设施细节，对上层隐藏实现
// - 各层通过依赖注入获取配置，不直接引用此包
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ServerConfig 服务端监听配置
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	Name string `yaml:"name"`
}

// StablePayConfig StablePay Gateway 相关配置
type StablePayConfig struct {
	GatewayBaseURL string `yaml:"gateway_base_url"`
	APIKey         string `yaml:"api_key"`
	FacilitatorURL string `yaml:"facilitator_url"`
}

// MerchantConfig 商户钱包配置
type MerchantConfig struct {
	SellerAddress string `yaml:"seller_address"`
	ProofSecret   string `yaml:"proof_secret"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Path         string `yaml:"path"`
	AutoMigrate  bool   `yaml:"auto_migrate"`
	SeedEnabled  bool   `yaml:"seed_enabled"`
}

// BlockchainConfig 区块链网络配置
type BlockchainConfig struct {
	SolanaNetwork string `yaml:"solana_network"`
	USDCMint      string `yaml:"usdc_mint"`
}

// Config 应用总配置
type Config struct {
	Server     ServerConfig     `yaml:"server"`
	StablePay  StablePayConfig  `yaml:"stablepay"`
	Merchant   MerchantConfig   `yaml:"merchant"`
	Database   DatabaseConfig   `yaml:"database"`
	Blockchain BlockchainConfig `yaml:"blockchain"`
}

// DefaultConfig 返回带默认值的配置

// Load 从指定路径加载 YAML 配置文件。
// 如果 path 为空，依次尝试:
//  1. 环境变量 CONFIG_PATH 指定的路径
//  2. ./config/config.yaml
//  3. ./config.yaml
//
// 配置文件不存在时返回默认配置（不报错），解析失败时报错。
func Load(path string) (*Config, error) {
	cfg := defaultConfig()

	resolved := resolvePath(path)
	if resolved == "" {
		return cfg, nil // 无配置文件，使用默认值
	}

	data, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil // 配置文件不存在，使用默认值
		}
		return nil, fmt.Errorf("read config file %s: %w", resolved, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config file %s: %w", resolved, err)
	}

	return cfg, nil
}

// resolvePath 解析配置文件路径
func resolvePath(path string) string {
	if path != "" {
		return path
	}

	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		return envPath
	}

	// 尝试常见位置
	candidates := []string{
		"config/config.yaml",
		"config.yaml",
	}
	// 也检查可执行文件所在目录
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "config/config.yaml"),
			filepath.Join(exeDir, "config.yaml"),
		)
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	return ""
}

// defaultConfig 返回安全的默认配置
func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8787,
			Name: "stablepay-merchant-server",
		},
		StablePay: StablePayConfig{
			GatewayBaseURL: "https://ai.wenfu.cn",
			APIKey:         "stablepay-dev-key",
			FacilitatorURL: "https://ai.wenfu.cn",
		},
		Merchant: MerchantConfig{
			SellerAddress: "2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR",
			ProofSecret:   "replace-this-with-a-long-random-secret",
		},
		Database: DatabaseConfig{
			Path:        "./data/merchant.db",
			AutoMigrate: true,
			SeedEnabled: true,
		},
		Blockchain: BlockchainConfig{
			SolanaNetwork: "solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp",
			USDCMint:      "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		},
	}
}
