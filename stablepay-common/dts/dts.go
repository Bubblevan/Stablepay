package dts

// Version 模块版本号
const Version = "v1.0.0"

// OperationType DTS操作类型
const (
	OperationInsert = "INSERT"
	OperationUpdate = "UPDATE"
	OperationDelete = "DELETE"
	OperationDDL    = "DDL"
)

// Config DTS配置
type Config struct {
	// 源数据源
	Source string
	// 目标数据源
	Target string
	// 批量大小
	BatchSize int
	// 并发数
	Concurrency int
	// 超时时间（秒）
	Timeout int
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		BatchSize:   1000,
		Concurrency: 10,
		Timeout:     300,
	}
}
