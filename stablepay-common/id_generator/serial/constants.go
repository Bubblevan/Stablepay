package serial

// Data Center Codes (2 digits) - 位置 16~17
// 用于全球化部署
const (
	DCDefault = "01" // 默认机房
)

// Environment Codes (1 digit) - 位置 22
// 0 代表预发环境，1 代表生产环境
const (
	EnvPreProd = "0" // 预发
	EnvProd    = "1" // 生产
)

// Reserved Codes (2 digits) - 位置 23~24
const (
	ReservedDefault = "00" // 默认预留
)
