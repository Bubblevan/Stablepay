// Package config 配置管理
// COLA V5: Infrastructure Layer
package config

import (
	"fmt" // 格式化包，用于字符串拼接
	"os"  // 操作系统包，用于文件读取

	"gopkg.in/yaml.v3" // YAML解析库，用于解析配置文件
)

// Config 全局配置结构体
type Config struct {
	Server     ServerConfig     `yaml:"server"`     // 服务器配置
	Log        LogConfig        `yaml:"log"`        // 日志配置
	Database   DatabaseConfig   `yaml:"database"`   // 数据库配置（新增）
	Downstream DownstreamConfig `yaml:"downstream"` // 下游服务配置
}

// ServerConfig 服务配置结构体
type ServerConfig struct {
	Host string `yaml:"host"` // 监听地址，如 "0.0.0.0"
	Port int    `yaml:"port"` // 监听端口，如 8081
}

// LogConfig 日志配置结构体
type LogConfig struct {
	Level  string `yaml:"level"`  // 日志级别：debug, info, warn, error
	Format string `yaml:"format"` // 日志格式：json, console
}

// DatabaseConfig 数据库配置结构体（新增）
type DatabaseConfig struct {
	Host            string `yaml:"host"`              // MySQL主机地址
	Port            int    `yaml:"port"`              // MySQL端口
	User            string `yaml:"user"`              // 用户名
	Password        string `yaml:"password"`          // 密码
	DBName          string `yaml:"dbname"`            // 数据库名
	Charset         string `yaml:"charset"`           // 字符集，默认utf8mb4
	MaxOpenConns    int    `yaml:"max_open_conns"`    // 最大打开连接数
	MaxIdleConns    int    `yaml:"max_idle_conns"`    // 最大空闲连接数
	ConnMaxLifetime int    `yaml:"conn_max_lifetime"` // 连接最大生命周期（秒）
}

// DownstreamConfig 下游服务配置结构体
type DownstreamConfig struct {
	PaymentService    string `yaml:"payment_service"`    // 支付服务地址
	BlockchainAdapter string `yaml:"blockchain_adapter"` // 区块链适配器地址
}

// Load 加载配置文件
// 参数 path: 配置文件路径
// 返回: 解析后的配置指针，错误信息
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) // 读取配置文件内容
	if err != nil {                // 读取失败
		return nil, fmt.Errorf("failed to read config file: %w", err) // 返回包装错误
	}

	var cfg Config                       // 声明配置变量
	if err := yaml.Unmarshal(data, &cfg); err != nil { // 解析YAML到结构体
		return nil, fmt.Errorf("failed to parse config: %w", err) // 解析失败返回错误
	}

	cfg.setDefaults() // 设置默认值

	return &cfg, nil // 返回配置指针
}

// setDefaults 设置配置默认值
// 如果配置项为空，则设置为合理的默认值
func (c *Config) setDefaults() {
	if c.Server.Host == "" { // 如果主机为空
		c.Server.Host = "0.0.0.0" // 默认监听所有接口
	}
	if c.Server.Port == 0 { // 如果端口为0
		c.Server.Port = 8081 // DID Service默认端口8081
	}
	if c.Log.Level == "" { // 如果日志级别为空
		c.Log.Level = "info" // 默认info级别
	}
	if c.Log.Format == "" { // 如果日志格式为空
		c.Log.Format = "json" // 默认JSON格式
	}
	// 数据库默认值
	if c.Database.Host == "" { // 如果数据库主机为空
		c.Database.Host = "127.0.0.1" // 默认本地地址
	}
	if c.Database.Port == 0 { // 如果数据库端口为0
		c.Database.Port = 3306 // 默认MySQL端口
	}
	if c.Database.Charset == "" { // 如果字符集为空
		c.Database.Charset = "utf8mb4" // 默认utf8mb4
	}
	if c.Database.MaxOpenConns == 0 { // 如果最大连接数为0
		c.Database.MaxOpenConns = 20 // 默认20个最大连接
	}
	if c.Database.MaxIdleConns == 0 { // 如果最大空闲连接为0
		c.Database.MaxIdleConns = 10 // 默认10个空闲连接
	}
	if c.Database.ConnMaxLifetime == 0 { // 如果连接生命周期为0
		c.Database.ConnMaxLifetime = 3600 // 默认3600秒（1小时）
	}
}

// GetAddr 获取服务监听地址
// 返回: 格式为 "host:port" 的地址字符串
func (c *Config) GetAddr() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port) // 拼接地址字符串
}

// GetMySQLDSN 获取MySQL数据源名称（DSN）
// 返回: GORM可用的MySQL连接字符串
func (c *Config) GetMySQLDSN() string {
	// 格式：user:password@tcp(host:port)/dbname?charset=utf8mb4&parseTime=True&loc=Local
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
		c.Database.User,     // 用户名
		c.Database.Password, // 密码
		c.Database.Host,     // 主机
		c.Database.Port,     // 端口
		c.Database.DBName,   // 数据库名
		c.Database.Charset,  // 字符集
	)
}
