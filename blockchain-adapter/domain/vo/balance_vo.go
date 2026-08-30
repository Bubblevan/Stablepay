// Package vo 定义值对象（Value Object）
// 值对象：不可变，通过属性相等性判断
package vo

import "fmt"

// BalanceVO 余额值对象
type BalanceVO struct {
	Address  string // 钱包地址
	Balance  uint64 // 余额（最小单位）
	Decimals uint8  // 精度
	Symbol   string // 代币符号（USDC/USDT/SOL）
}

// NewBalanceVO 创建余额值对象
func NewBalanceVO(address string, balance uint64, decimals uint8, symbol string) BalanceVO {
	return BalanceVO{
		Address:  address,
		Balance:  balance,
		Decimals: decimals,
		Symbol:   symbol,
	}
}

// FormatHumanReadable 格式化为人类可读（例如：1.5 USDC）
func (b BalanceVO) FormatHumanReadable() string {
	amount := float64(b.Balance)
	for i := uint8(0); i < b.Decimals; i++ {
		amount /= 10
	}
	return formatAmount(amount, b.Decimals, b.Symbol)
}

// IsZero 是否为零余额
func (b BalanceVO) IsZero() bool {
	return b.Balance == 0
}

// IsSufficient 余额是否充足
func (b BalanceVO) IsSufficient(required uint64) bool {
	return b.Balance >= required
}

// Equal 值对象相等性比较
func (b BalanceVO) Equal(other BalanceVO) bool {
	return b.Address == other.Address &&
		b.Balance == other.Balance &&
		b.Decimals == other.Decimals &&
		b.Symbol == other.Symbol
}

// formatAmount 格式化金额
func formatAmount(amount float64, decimals uint8, symbol string) string {
	return fmt.Sprintf("%.*f %s", decimals, amount, symbol)
}
