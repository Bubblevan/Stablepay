// Package utils 提供通用工具函数
package utils

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/stablepay/payment-service/pkg/constants"
)

// AmountUtil 金额处理工具
type AmountUtil struct {
	decimals int
}

// NewAmountUtil 创建金额工具
func NewAmountUtil(decimals int) *AmountUtil {
	return &AmountUtil{decimals: decimals}
}

// DefaultAmountUtil 默认 USDC/USDT 金额工具（6位小数）
var DefaultAmountUtil = NewAmountUtil(constants.USDCDecimals)

// ToMinorUnit 将字符串金额转换为最小单位整数
// 例如: "5.00" -> 5000000 (USDC 6位小数)
func (a *AmountUtil) ToMinorUnit(amount string) (int64, error) {
	// 去除首尾空格
	amount = strings.TrimSpace(amount)

	// 解析小数
	parts := strings.Split(amount, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("invalid amount format: %s", amount)
	}

	// 整数部分
	integerPart := parts[0]
	if integerPart == "" {
		integerPart = "0"
	}

	// 小数部分
	decimalPart := ""
	if len(parts) == 2 {
		decimalPart = parts[1]
		// 截断或补零到指定位数
		if len(decimalPart) > a.decimals {
			decimalPart = decimalPart[:a.decimals]
		} else {
			decimalPart = decimalPart + strings.Repeat("0", a.decimals-len(decimalPart))
		}
	} else {
		decimalPart = strings.Repeat("0", a.decimals)
	}

	// 合并为整数
	combined := integerPart + decimalPart

	// 转换为 int64
	result, err := strconv.ParseInt(combined, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse amount: %w", err)
	}

	return result, nil
}

// FromMinorUnit 将最小单位整数转换为字符串金额
// 例如: 5000000 -> "5.000000"
func (a *AmountUtil) FromMinorUnit(amountMinor int64) string {
	// 处理负数
	sign := ""
	if amountMinor < 0 {
		sign = "-"
		amountMinor = -amountMinor
	}

	// 转换为字符串
	s := strconv.FormatInt(amountMinor, 10)

	// 补零到足够长度
	if len(s) <= a.decimals {
		s = strings.Repeat("0", a.decimals-len(s)+1) + s
	}

	// 插入小数点
	decimalPos := len(s) - a.decimals
	result := sign + s[:decimalPos] + "." + s[decimalPos:]

	return result
}

// FromMinorUnitTrimZeros 从最小单位转换并去除末尾零
// 例如: 5000000 -> "5"
func (a *AmountUtil) FromMinorUnitTrimZeros(amountMinor int64) string {
	result := a.FromMinorUnit(amountMinor)
	// 去除末尾零和小数点
	result = strings.TrimRight(result, "0")
	result = strings.TrimRight(result, ".")
	return result
}

// Add 金额相加
func (a *AmountUtil) Add(a1, a2 int64) int64 {
	return a1 + a2
}

// Sub 金额相减
func (a *AmountUtil) Sub(a1, a2 int64) int64 {
	return a1 - a2
}

// Mul 金额乘以整数系数
func (a *AmountUtil) Mul(amount int64, factor int64) int64 {
	return amount * factor
}

// Compare 比较金额大小
// 返回: -1 (a1 < a2), 0 (a1 == a2), 1 (a1 > a2)
func (a *AmountUtil) Compare(a1, a2 int64) int {
	if a1 < a2 {
		return -1
	}
	if a1 > a2 {
		return 1
	}
	return 0
}

// IsZero 检查金额是否为零
func (a *AmountUtil) IsZero(amount int64) bool {
	return amount == 0
}

// IsPositive 检查金额是否为正
func (a *AmountUtil) IsPositive(amount int64) bool {
	return amount > 0
}

// IsNegative 检查金额是否为负
func (a *AmountUtil) IsNegative(amount int64) bool {
	return amount < 0
}

// Max 返回较大金额
func (a *AmountUtil) Max(a1, a2 int64) int64 {
	if a1 > a2 {
		return a1
	}
	return a2
}

// Min 返回较小金额
func (a *AmountUtil) Min(a1, a2 int64) int64 {
	if a1 < a2 {
		return a1
	}
	return a2
}

// ParseMaxAmount 解析最大金额配置
func ParseMaxAmount(amountStr string) (int64, error) {
	return DefaultAmountUtil.ToMinorUnit(amountStr)
}

// FormatAmount 格式化金额显示（去除末尾零）
func FormatAmount(amountMinor int64) string {
	return DefaultAmountUtil.FromMinorUnitTrimZeros(amountMinor)
}

// FormatAmountFull 格式化金额显示（保留完整精度）
func FormatAmountFull(amountMinor int64) string {
	return DefaultAmountUtil.FromMinorUnit(amountMinor)
}

// StringToMinorUnit 字符串转最小单位（简写）
func StringToMinorUnit(amount string) (int64, error) {
	return DefaultAmountUtil.ToMinorUnit(amount)
}

// MinorUnitToString 最小单位转字符串（简写）
func MinorUnitToString(amountMinor int64) string {
	return DefaultAmountUtil.FromMinorUnitTrimZeros(amountMinor)
}
