package money

import (
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"github.com/shopspring/decimal"
)

// MoneyError 表示Money相关的错误
type MoneyError struct {
	Type    string
	Message string
	Details string
}

func (e *MoneyError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("[%s] %s: %s", e.Type, e.Message, e.Details)
	}
	return fmt.Sprintf("[%s] %s", e.Type, e.Message)
}

// 预定义的错误类型
var (
	ErrInvalidCurrency     = &MoneyError{Type: "InvalidCurrency", Message: "无效的货币类型"}
	ErrInvalidAmount       = &MoneyError{Type: "InvalidAmount", Message: "无效的金额"}
	ErrCurrencyMismatch    = &MoneyError{Type: "CurrencyMismatch", Message: "货币类型不匹配"}
	ErrDivisionByZero      = &MoneyError{Type: "DivisionByZero", Message: "除零错误"}
	ErrNegativeMultiplier  = &MoneyError{Type: "NegativeMultiplier", Message: "乘数不能为负数"}
	ErrAmountOverflow      = &MoneyError{Type: "AmountOverflow", Message: "金额溢出"}
	ErrInsufficientFunds   = &MoneyError{Type: "InsufficientFunds", Message: "余额不足"}
	ErrInvalidExchangeRate = &MoneyError{Type: "InvalidExchangeRate", Message: "无效的汇率"}
)

// RoundingMode 舍入模式
type RoundingMode int

const (
	RoundHalfUp   RoundingMode = iota // 四舍五入
	RoundHalfDown                     // 五舍六入
	RoundHalfEven                     // 银行家舍入
	RoundUp                           // 向上舍入
	RoundDown                         // 向下舍入
	RoundCeiling                      // 向正无穷舍入
	RoundFloor                        // 向负无穷舍入
)

// Money 表示一个货币金额，使用 big.Int 存储以支持任意精度
// 这是金融行业的标准做法，支持高精度加密货币如 ETH (18位)
type Money struct {
	amount   *big.Int // 最小单位金额：以最小货币单位存储（如 150 表示 150 cents = $1.50）
	currency Currency // 货币类型（包含精度信息）
}

// NewMoneyFromInt64 从 int64 最小单位金额创建Money实例（便利方法）
// 参数 amount: 以最小货币单位表示的金额（如 150 表示 150 cents = $1.50）
// 注意：仅适用于低精度币种（USDT/USDC/TRX等），高精度币种请使用 NewMoneyFromString
func NewMoneyFromInt64(amount int64, currency Currency) (*Money, error) {
	if !currency.IsValid() {
		return nil, &MoneyError{Type: "InvalidCurrency", Message: "无效的货币类型", Details: currency.Code}
	}

	return &Money{
		amount:   big.NewInt(amount),
		currency: currency,
	}, nil
}

// NewMoneyFromBigInt 从 big.Int 最小单位金额创建Money实例
// 参数 amount: 以最小货币单位表示的金额，使用 big.Int 类型（如 big.NewInt(1000000000000000000) 表示 1 ETH）
// 注意：此方法会复制传入的 big.Int，避免外部修改影响 Money 对象
func NewMoneyFromBigInt(amount *big.Int, currency Currency) (*Money, error) {
	if !currency.IsValid() {
		return nil, &MoneyError{Type: "InvalidCurrency", Message: "无效的货币类型", Details: currency.Code}
	}

	if amount == nil {
		return nil, &MoneyError{Type: "InvalidAmount", Message: "金额不能为空"}
	}

	// 复制 big.Int 避免外部修改
	amountCopy := new(big.Int).Set(amount)

	return &Money{
		amount:   amountCopy,
		currency: currency,
	}, nil
}

// NewMoneyFromString 从最小单位字符串创建Money实例（推荐使用）
// 参数 amountStr: 以最小货币单位表示的金额字符串（如 "1000000000000000000" 表示 1 ETH）
func NewMoneyFromString(amountStr string, currency Currency) (*Money, error) {
	if !currency.IsValid() {
		return nil, &MoneyError{Type: "InvalidCurrency", Message: "无效的货币类型", Details: currency.Code}
	}

	if amountStr == "" {
		return nil, &MoneyError{Type: "InvalidAmount", Message: "金额字符串不能为空"}
	}

	amount := new(big.Int)
	_, ok := amount.SetString(amountStr, 10)
	if !ok {
		return nil, &MoneyError{Type: "InvalidAmount", Message: "无效的金额字符串", Details: amountStr}
	}

	return &Money{
		amount:   amount,
		currency: currency,
	}, nil
}

// NewMoneyFromDecimal 从面额创建Money实例
// 参数 amount: 面额，如 1.50 表示 $1.50
func NewMoneyFromDecimal(amount decimal.Decimal, currency Currency) (*Money, error) {
	if !currency.IsValid() {
		return nil, &MoneyError{Type: "InvalidCurrency", Message: "无效的货币类型", Details: currency.Code}
	}

	precision := currency.GetPrecision()
	multiplier := getMultiplier(precision)

	// 转换为最小单位（默认：银行家舍入 Half-Even）
	amountDecimal := amount.Mul(decimal.NewFromInt(multiplier)).RoundBank(0)

	// 转为 big.Int
	amountBigInt := amountDecimal.BigInt()

	return &Money{
		amount:   amountBigInt,
		currency: currency,
	}, nil
}

// NewMoneyFromDecimalString 从面额字符串创建Money实例
// 参数 amountStr: 面额字符串，如 "1.50" 表示 $1.50
func NewMoneyFromDecimalString(amountStr string, currency Currency) (*Money, error) {
	if !currency.IsValid() {
		return nil, &MoneyError{Type: "InvalidCurrency", Message: "无效的货币类型", Details: currency.Code}
	}

	if amountStr == "" {
		return nil, &MoneyError{Type: "InvalidAmount", Message: "面额字符串不能为空"}
	}

	amount, err := decimal.NewFromString(amountStr)
	if err != nil {
		return nil, &MoneyError{Type: "InvalidAmount", Message: "无效的面额格式", Details: amountStr}
	}

	return NewMoneyFromDecimal(amount, currency)
}

// Zero 创建零金额的Money实例
func Zero(currency Currency) (*Money, error) {
	return NewMoneyFromInt64(0, currency)
}

// 辅助函数：获取精度倍数
func getMultiplier(precision int) int64 {
	multiplier := int64(1)
	for i := 0; i < precision; i++ {
		multiplier *= 10
	}
	return multiplier
}

// validateAmountInMinorUnits 验证最小单位金额是否有效（已废弃，big.Int 无需验证）
func validateAmountInMinorUnits(amountInMinorUnits int64) error {
	// big.Int 支持任意精度，无需验证
	return nil
}

// GetDecimal 获取面额
// 返回值: 面额的 decimal 表示，如 1.50 表示 $1.50
func (m *Money) GetDecimal() decimal.Decimal {
	if m == nil || m.amount == nil {
		return decimal.Zero
	}
	// 使用 decimal.NewFromBigInt，第二个参数是指数（负数表示小数位）
	return decimal.NewFromBigInt(m.amount, -int32(m.currency.GetPrecision()))
}

// GetString 获取最小单位字符串（推荐使用）
// 返回值: 以最小货币单位表示的金额字符串，如 "1000000000000000000" 表示 1 ETH
func (m *Money) GetString() string {
	if m == nil || m.amount == nil {
		return "0"
	}
	return m.amount.String()
}

// GetInt64 获取 int64 金额（仅用于低精度币种）
// 返回值: (金额, error)，如果超出 int64 范围则返回错误
func (m *Money) GetInt64() (int64, error) {
	if m == nil || m.amount == nil {
		return 0, nil
	}

	// 检查是否溢出
	if !m.amount.IsInt64() {
		return 0, &MoneyError{
			Type:    "AmountOverflow",
			Message: "金额超出 int64 范围，请使用 GetString()",
		}
	}

	return m.amount.Int64(), nil
}

// GetInt64Unsafe 获取 int64 金额（不检查溢出，仅用于测试和确定不会溢出的场景）
// 警告：如果金额超出 int64 范围会发生截断
func (m *Money) GetInt64Unsafe() int64 {
	if m == nil || m.amount == nil {
		return 0
	}
	return m.amount.Int64()
}

// GetBigInt 获取 big.Int 金额（推荐用于高精度币种）
// 返回值: 返回一个新的 big.Int 副本，避免外部修改影响 Money 对象
// 注意：返回的是最小单位金额（如 1000000000000000000 表示 1 ETH）
func (m *Money) GetBigInt() *big.Int {
	if m == nil || m.amount == nil {
		return big.NewInt(0)
	}
	// 返回副本，避免外部修改
	return new(big.Int).Set(m.amount)
}

// GetPrecision 获取精度
func (m *Money) GetPrecision() int {
	return m.currency.GetPrecision()
}

// GetCurrency 获取货币类型
func (m *Money) GetCurrency() Currency {
	return m.currency
}

// IsZero 判断是否为零金额
func (m *Money) IsZero() bool {
	if m == nil || m.amount == nil {
		return true
	}
	return m.amount.Sign() == 0
}

// IsPositive 判断是否为正数
func (m *Money) IsPositive() bool {
	if m == nil || m.amount == nil {
		return false
	}
	return m.amount.Sign() > 0
}

// IsNegative 判断是否为负数
func (m *Money) IsNegative() bool {
	if m == nil || m.amount == nil {
		return false
	}
	return m.amount.Sign() < 0
}

// Add 加法运算
func (m *Money) Add(other *Money) (*Money, error) {
	if err := m.validateCurrencyMatch(other); err != nil {
		return nil, err
	}

	result := new(big.Int)
	result.Add(m.amount, other.amount)

	return &Money{
		amount:   result,
		currency: m.currency,
	}, nil
}

// Subtract 减法运算
func (m *Money) Subtract(other *Money) (*Money, error) {
	if err := m.validateCurrencyMatch(other); err != nil {
		return nil, err
	}

	result := new(big.Int)
	result.Sub(m.amount, other.amount)

	return &Money{
		amount:   result,
		currency: m.currency,
	}, nil
}

// Multiply 乘法运算（保持最高精度，不进行舍入）
func (m *Money) Multiply(multiplier decimal.Decimal) (*Money, error) {
	if multiplier.IsNegative() {
		return nil, ErrNegativeMultiplier
	}

	// 使用decimal进行精确计算
	result := m.GetDecimal().Mul(multiplier)

	// 转换为 big.Int
	return NewMoneyFromDecimal(result, m.currency)
}

// Divide 除法运算（保持最高精度，不进行舍入）
func (m *Money) Divide(divisor decimal.Decimal) (*Money, error) {
	if divisor.IsZero() {
		return nil, ErrDivisionByZero
	}

	if divisor.IsNegative() {
		return nil, &MoneyError{Type: "NegativeDivisor", Message: "除数不能为负数"}
	}

	// 使用decimal进行精确计算
	result := m.GetDecimal().Div(divisor)

	// 转换为 big.Int
	return NewMoneyFromDecimal(result, m.currency)
}

// Negate 取反
func (m *Money) Negate() *Money {
	result := new(big.Int)
	result.Neg(m.amount)
	return &Money{
		amount:   result,
		currency: m.currency,
	}
}

// Abs 取绝对值
func (m *Money) Abs() *Money {
	if m.amount.Sign() < 0 {
		result := new(big.Int)
		result.Abs(m.amount)
		return &Money{
			amount:   result,
			currency: m.currency,
		}
	}
	return m.Clone()
}

// Equals 判断是否相等
func (m *Money) Equals(other *Money) bool {
	if other == nil {
		return false
	}

	return m.currency == other.currency && m.amount.Cmp(other.amount) == 0
}

// GreaterThan 判断是否大于
func (m *Money) GreaterThan(other *Money) (bool, error) {
	if err := m.validateCurrencyMatch(other); err != nil {
		return false, err
	}

	return m.amount.Cmp(other.amount) > 0, nil
}

// GreaterThanOrEqual 判断是否大于等于
func (m *Money) GreaterThanOrEqual(other *Money) (bool, error) {
	if err := m.validateCurrencyMatch(other); err != nil {
		return false, err
	}

	return m.amount.Cmp(other.amount) >= 0, nil
}

// LessThan 判断是否小于
func (m *Money) LessThan(other *Money) (bool, error) {
	if err := m.validateCurrencyMatch(other); err != nil {
		return false, err
	}

	return m.amount.Cmp(other.amount) < 0, nil
}

// LessThanOrEqual 判断是否小于等于
func (m *Money) LessThanOrEqual(other *Money) (bool, error) {
	if err := m.validateCurrencyMatch(other); err != nil {
		return false, err
	}

	return m.amount.Cmp(other.amount) <= 0, nil
}

// Compare 比较两个Money对象
// 返回值: -1 (小于), 0 (等于), 1 (大于)
func (m *Money) Compare(other *Money) (int, error) {
	if err := m.validateCurrencyMatch(other); err != nil {
		return 0, err
	}

	return m.amount.Cmp(other.amount), nil
}

// validateCurrencyMatch 验证货币类型是否匹配
func (m *Money) validateCurrencyMatch(other *Money) error {
	if other == nil {
		return &MoneyError{Type: "InvalidAmount", Message: "金额不能为空"}
	}

	if m.currency != other.currency {
		return &MoneyError{Type: "CurrencyMismatch", Message: "货币类型不匹配", Details: fmt.Sprintf("%s != %s", m.currency, other.currency)}
	}

	return nil
}

// Format 格式化金额显示
func (m *Money) Format() string {
	return m.FormatWithLocale("en-US")
}

// FormatWithLocale 使用指定地区格式化金额显示
func (m *Money) FormatWithLocale(locale string) string {
	amountStr := m.formatDecimalAmount()

	// 统一使用 "金额 货币代码" 格式
	return fmt.Sprintf("%s %s", amountStr, m.currency)
}

// FormatDecimalAmount 格式化面额字符串
// 返回值: 格式化后的面额字符串，如 "1.50"
func (m *Money) FormatDecimalAmount() string {
	return m.formatDecimalAmount()
}

// formatDecimalAmount 格式化面额字符串（内部方法）
func (m *Money) formatDecimalAmount() string {
	decimalAmount := m.GetDecimal()

	// 根据精度格式化
	if m.currency.GetPrecision() == 0 {
		return decimalAmount.StringFixed(0)
	}

	return decimalAmount.StringFixed(int32(m.currency.GetPrecision()))
}

// FormatWithSymbol 带符号格式化
func (m *Money) FormatWithSymbol() string {
	return fmt.Sprintf("%s %s", m.formatDecimalAmount(), m.currency)
}

// FormatWithoutSymbol 不带符号格式化
func (m *Money) FormatWithoutSymbol() string {
	return m.formatDecimalAmount()
}

// String 实现Stringer接口
func (m *Money) String() string {
	return m.Format()
}

// MarshalJSON 实现JSON序列化
// JSON 格式: {"amount":"1000000000000000000","currency":"ETH","precision":18}
// 注意: amount 字段为最小单位金额字符串 (minor units string)
func (m *Money) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"amount":"%s","currency":"%s","precision":%d}`,
		m.GetString(), m.currency, m.currency.GetPrecision())), nil
}

// UnmarshalJSON 实现JSON反序列化
// JSON 格式: {"amount":"1000000000000000000","currency":"ETH","precision":18}
// 注意: amount 字段应为最小单位金额字符串 (minor units string)
func (m *Money) UnmarshalJSON(data []byte) error {
	var tmp struct {
		Amount   string `json:"amount"`
		Currency string `json:"currency"`
	}

	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	currency, ok := GetCurrencyByCode(tmp.Currency)
	if !ok {
		return &MoneyError{Type: "InvalidCurrency", Message: "未知的货币类型"}
	}

	money, err := NewMoneyFromString(tmp.Amount, currency)
	if err != nil {
		return err
	}

	*m = *money
	return nil
}

// Clone 克隆Money对象
func (m *Money) Clone() *Money {
	result := new(big.Int)
	result.Set(m.amount)
	return &Money{
		amount:   result,
		currency: m.currency,
	}
}

// IsValid 验证Money对象是否有效
func (m *Money) IsValid() bool {
	return m != nil && m.currency.IsValid()
}

// IsGreaterThanZero 判断是否大于零
func (m *Money) IsGreaterThanZero() bool {
	if m == nil || m.amount == nil {
		return false
	}
	return m.amount.Sign() > 0
}

// IsLessThanZero 判断是否小于零
func (m *Money) IsLessThanZero() bool {
	if m == nil || m.amount == nil {
		return false
	}
	return m.amount.Sign() < 0
}

// IsGreaterThanOrEqualToZero 判断是否大于等于零
func (m *Money) IsGreaterThanOrEqualToZero() bool {
	if m == nil || m.amount == nil {
		return true
	}
	return m.amount.Sign() >= 0
}

// IsLessThanOrEqualToZero 判断是否小于等于零
func (m *Money) IsLessThanOrEqualToZero() bool {
	if m == nil || m.amount == nil {
		return true
	}
	return m.amount.Sign() <= 0
}

// GetIntegerPart 获取整数部分
func (m *Money) GetIntegerPart() int64 {
	if m == nil || m.amount == nil {
		return 0
	}
	multiplier := getMultiplier(m.currency.GetPrecision())
	result := new(big.Int)
	result.Div(m.amount, big.NewInt(multiplier))
	return result.Int64()
}

// GetFractionalPart 获取小数部分（以最小单位表示）
func (m *Money) GetFractionalPart() int64 {
	if m == nil || m.amount == nil {
		return 0
	}
	multiplier := getMultiplier(m.currency.GetPrecision())
	result := new(big.Int)
	result.Mod(m.amount, big.NewInt(multiplier))
	return result.Int64()
}

// applyRounding 应用舍入模式
// 参数 decimalAmount: 要舍入的面额
// 返回值: 舍入后的整数部分
func applyRounding(decimalAmount decimal.Decimal, mode RoundingMode) int64 {
	switch mode {
	case RoundHalfUp:
		// 四舍五入：>= 0.5 向上舍入
		return decimalAmount.Round(0).IntPart()
	case RoundHalfDown:
		// 五舍六入：> 0.5 向上舍入，<= 0.5 向下舍入
		truncated := decimalAmount.Truncate(0)
		fractional := decimalAmount.Sub(truncated)
		half, _ := decimal.NewFromString("0.5")
		if fractional.GreaterThan(half) {
			return truncated.Add(decimal.NewFromInt(1)).IntPart()
		}
		return truncated.IntPart()
	case RoundHalfEven:
		// 银行家舍入：向最接近的偶数舍入
		truncated := decimalAmount.Truncate(0)
		fractional := decimalAmount.Sub(truncated)
		half, _ := decimal.NewFromString("0.5")

		if fractional.GreaterThan(half) {
			return truncated.Add(decimal.NewFromInt(1)).IntPart()
		} else if fractional.LessThan(half) {
			return truncated.IntPart()
		} else {
			// = 0.5，向最接近的偶数舍入
			if truncated.Mod(decimal.NewFromInt(2)).IsZero() {
				return truncated.IntPart()
			} else {
				return truncated.Add(decimal.NewFromInt(1)).IntPart()
			}
		}
	case RoundUp:
		// 向上舍入：任何小数都向上舍入
		return decimalAmount.Ceil().IntPart()
	case RoundDown:
		// 向下舍入：任何小数都向下舍入
		return decimalAmount.Truncate(0).IntPart()
	case RoundCeiling:
		// 向正无穷舍入（与RoundUp相同）
		return decimalAmount.Ceil().IntPart()
	case RoundFloor:
		// 向负无穷舍入（与RoundDown相同）
		return decimalAmount.Floor().IntPart()
	default:
		return decimalAmount.Round(0).IntPart()
	}
}

// RoundWithMode 使用指定舍入模式舍入
func (m *Money) RoundWithMode(mode RoundingMode) *Money {
	decimalAmount := m.GetDecimal()

	// 直接对decimal值进行舍入
	var roundedAmount decimal.Decimal
	switch mode {
	case RoundHalfUp:
		roundedAmount = decimalAmount.Round(0)
	case RoundHalfDown:
		truncated := decimalAmount.Truncate(0)
		fractional := decimalAmount.Sub(truncated)
		half, _ := decimal.NewFromString("0.5")
		if fractional.GreaterThan(half) {
			roundedAmount = truncated.Add(decimal.NewFromInt(1))
		} else {
			roundedAmount = truncated
		}
	case RoundHalfEven:
		// 银行家舍入
		truncated := decimalAmount.Truncate(0)
		fractional := decimalAmount.Sub(truncated)
		half, _ := decimal.NewFromString("0.5")

		if fractional.GreaterThan(half) {
			roundedAmount = truncated.Add(decimal.NewFromInt(1))
		} else if fractional.LessThan(half) {
			roundedAmount = truncated
		} else {
			// = 0.5，向最接近的偶数舍入
			if truncated.Mod(decimal.NewFromInt(2)).IsZero() {
				roundedAmount = truncated
			} else {
				roundedAmount = truncated.Add(decimal.NewFromInt(1))
			}
		}
	case RoundUp:
		roundedAmount = decimalAmount.Ceil()
	case RoundDown:
		roundedAmount = decimalAmount.Truncate(0)
	case RoundCeiling:
		roundedAmount = decimalAmount.Ceil()
	case RoundFloor:
		roundedAmount = decimalAmount.Floor()
	default:
		roundedAmount = decimalAmount.Round(0)
	}

	// 将舍入后的decimal值转换为最小单位
	multiplierInt := getMultiplier(m.currency.GetPrecision())
	amountDecimal := roundedAmount.Mul(decimal.NewFromInt(multiplierInt))
	amount := amountDecimal.BigInt()

	return &Money{
		amount:   amount,
		currency: m.currency,
	}
}

// RoundUp 向上舍入（便利方法）
func (m *Money) RoundUp() *Money {
	return m.RoundWithMode(RoundUp)
}

// RoundDown 向下舍入（便利方法）
func (m *Money) RoundDown() *Money {
	return m.RoundWithMode(RoundDown)
}

// RoundHalfUp 四舍五入（便利方法）
func (m *Money) RoundHalfUp() *Money {
	return m.RoundWithMode(RoundHalfUp)
}

// RoundHalfEven 银行家舍入（便利方法）
func (m *Money) RoundHalfEven() *Money {
	return m.RoundWithMode(RoundHalfEven)
}

// RoundTo 舍入到指定精度
func (m *Money) RoundTo(precision int) *Money {
	if precision < 0 {
		return m.Clone()
	}

	if precision >= m.currency.GetPrecision() {
		return m.Clone()
	}

	// 计算舍入因子
	roundFactor := big.NewInt(getMultiplier(m.currency.GetPrecision() - precision))

	// 舍入: (amount + roundFactor/2) / roundFactor * roundFactor
	half := new(big.Int).Div(roundFactor, big.NewInt(2))
	result := new(big.Int).Add(m.amount, half)
	result.Div(result, roundFactor)
	result.Mul(result, roundFactor)

	return &Money{
		amount:   result,
		currency: m.currency,
	}
}

// RoundToCurrencyPrecision 舍入到货币精度
func (m *Money) RoundToCurrencyPrecision() *Money {
	return m.Clone() // 已经是货币精度
}

// ========== 加密货币特有功能 ==========

// Sum 计算多个Money对象的总和
func Sum(moneys ...*Money) (*Money, error) {
	if len(moneys) == 0 {
		return nil, fmt.Errorf("至少需要一个Money对象")
	}

	if len(moneys) == 1 {
		return moneys[0].Clone(), nil
	}

	result := moneys[0].Clone()
	for i := 1; i < len(moneys); i++ {
		sum, err := result.Add(moneys[i])
		if err != nil {
			return nil, err
		}
		result = sum
	}

	return result, nil
}

// Max 返回多个Money对象中的最大值
func Max(moneys ...*Money) (*Money, error) {
	if len(moneys) == 0 {
		return nil, fmt.Errorf("至少需要一个Money对象")
	}

	max := moneys[0]
	for i := 1; i < len(moneys); i++ {
		greater, err := moneys[i].GreaterThan(max)
		if err != nil {
			return nil, err
		}
		if greater {
			max = moneys[i]
		}
	}

	return max.Clone(), nil
}

// Min 返回多个Money对象中的最小值
func Min(moneys ...*Money) (*Money, error) {
	if len(moneys) == 0 {
		return nil, fmt.Errorf("至少需要一个Money对象")
	}

	min := moneys[0]
	for i := 1; i < len(moneys); i++ {
		less, err := moneys[i].LessThan(min)
		if err != nil {
			return nil, err
		}
		if less {
			min = moneys[i]
		}
	}

	return min.Clone(), nil
}

// ========== 汇率转换功能 ==========

// ExchangeRate 表示汇率
type ExchangeRate struct {
	FromCurrency Currency
	ToCurrency   Currency
	Rate         decimal.Decimal
	Timestamp    time.Time
}

// NewExchangeRate 创建新的汇率
func NewExchangeRate(from, to Currency, rate decimal.Decimal) (*ExchangeRate, error) {
	if !from.IsValid() || !to.IsValid() {
		return nil, &MoneyError{Type: "InvalidCurrency", Message: "无效的货币类型"}
	}

	if rate.IsNegative() || rate.IsZero() {
		return nil, &MoneyError{Type: "InvalidExchangeRate", Message: "汇率必须为正数"}
	}

	return &ExchangeRate{
		FromCurrency: from,
		ToCurrency:   to,
		Rate:         rate,
		Timestamp:    time.Now(),
	}, nil
}

// Convert 使用汇率转换金额（使用银行家舍入）
func (er *ExchangeRate) Convert(money *Money) (*Money, error) {
	if money == nil {
		return nil, &MoneyError{Type: "InvalidAmount", Message: "金额不能为空"}
	}

	if money.GetCurrency() != er.FromCurrency {
		return nil, &MoneyError{Type: "CurrencyMismatch", Message: "货币类型不匹配"}
	}

	// 直接使用 ConvertTo 方法，避免代码重复
	return money.ConvertTo(er.ToCurrency, er.Rate)
}

// GetReverseRate 获取反向汇率
func (er *ExchangeRate) GetReverseRate() *ExchangeRate {
	return &ExchangeRate{
		FromCurrency: er.ToCurrency,
		ToCurrency:   er.FromCurrency,
		Rate:         decimal.NewFromInt(1).Div(er.Rate),
		Timestamp:    er.Timestamp,
	}
}

// ExchangeRateProvider 汇率提供者接口
type ExchangeRateProvider interface {
	GetExchangeRate(from, to Currency) (*ExchangeRate, error)
}

// SimpleExchangeRateProvider 简单的汇率提供者实现
type SimpleExchangeRateProvider struct {
	rates map[string]decimal.Decimal
}

// NewSimpleExchangeRateProvider 创建简单的汇率提供者
func NewSimpleExchangeRateProvider() *SimpleExchangeRateProvider {
	return &SimpleExchangeRateProvider{
		rates: make(map[string]decimal.Decimal),
	}
}

// SetRate 设置汇率
func (p *SimpleExchangeRateProvider) SetRate(from, to Currency, rate decimal.Decimal) {
	key := fmt.Sprintf("%s_%s", from, to)
	p.rates[key] = rate
}

// GetExchangeRate 获取汇率
func (p *SimpleExchangeRateProvider) GetExchangeRate(from, to Currency) (*ExchangeRate, error) {
	if from == to {
		return NewExchangeRate(from, to, decimal.NewFromInt(1))
	}

	key := fmt.Sprintf("%s_%s", from, to)
	if rate, exists := p.rates[key]; exists {
		return NewExchangeRate(from, to, rate)
	}

	// 尝试反向汇率
	reverseKey := fmt.Sprintf("%s_%s", to, from)
	if reverseRate, exists := p.rates[reverseKey]; exists {
		return NewExchangeRate(from, to, decimal.NewFromInt(1).Div(reverseRate))
	}

	return nil, &MoneyError{Type: "InvalidExchangeRate", Message: "未找到汇率", Details: fmt.Sprintf("%s to %s", from, to)}
}

// ConvertTo 转换到指定货币（使用银行家舍入）
func (m *Money) ConvertTo(toCurrency Currency, rate decimal.Decimal) (*Money, error) {
	if m.GetCurrency() == toCurrency {
		return m.Clone(), nil
	}

	if rate.IsNegative() || rate.IsZero() {
		return nil, &MoneyError{Type: "InvalidExchangeRate", Message: "汇率必须为正数"}
	}

	// 获取原金额的面额（保持完整精度）
	sourceAmount := m.GetDecimal()

	// 乘以汇率得到目标面额（保持完整精度）
	targetAmount := sourceAmount.Mul(rate)

	// 转换为目标币种的最小单位（按目标币种精度，使用银行家舍入）
	targetPrecision := toCurrency.GetPrecision()
	targetMultiplier := decimal.NewFromInt(getMultiplier(targetPrecision))

	// 目标最小单位 = 目标面额 × 10^精度
	targetMinorUnits := targetAmount.Mul(targetMultiplier)

	// 使用银行家舍入（RoundHalfEven）
	targetMinorUnitsRounded := targetMinorUnits.RoundBank(0)

	// 转换为 big.Int
	targetBigInt := targetMinorUnitsRounded.BigInt()

	return &Money{
		amount:   targetBigInt,
		currency: toCurrency,
	}, nil
}

// ConvertWithExchangeRate 使用ExchangeRate对象转换金额
func (m *Money) ConvertWithExchangeRate(exchangeRate *ExchangeRate) (*Money, error) {
	if exchangeRate == nil {
		return nil, &MoneyError{Type: "InvalidExchangeRate", Message: "汇率不能为空"}
	}

	return exchangeRate.Convert(m)
}

// ConvertWithProvider 使用汇率提供者转换金额
func (m *Money) ConvertWithProvider(toCurrency Currency, provider ExchangeRateProvider) (*Money, error) {
	if provider == nil {
		return nil, &MoneyError{Type: "InvalidExchangeRate", Message: "汇率提供者不能为空"}
	}

	exchangeRate, err := provider.GetExchangeRate(m.GetCurrency(), toCurrency)
	if err != nil {
		return nil, err
	}

	return m.ConvertWithExchangeRate(exchangeRate)
}

// （说明）money 是业务域金额库，不处理跨链/链精度差异；链上金额与 token decimals 请使用 stablepay-common/chainmoney。

// FormatDecimalAmountWithPrecision 格式化面额字符串（指定显示精度）
// displayPrecision: 显示的小数位数，如果为负数则使用币种精度
func (m *Money) FormatDecimalAmountWithPrecision(displayPrecision int) string {
	decimalAmount := m.GetDecimal()

	if displayPrecision < 0 {
		displayPrecision = m.currency.GetPrecision()
	}

	return decimalAmount.StringFixed(int32(displayPrecision))
}

// FormatDecimalAmountTrimZero 格式化面额字符串（去除尾部零）
func (m *Money) FormatDecimalAmountTrimZero() string {
	decimalAmount := m.GetDecimal()
	return decimalAmount.String()
}
