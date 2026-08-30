package chainmoney

import (
	"fmt"
	"math/big"

	"github.com/shopspring/decimal"
)

// MoneyChain 链上口径金额（最小单位整数 + TokenMeta）
// 不可变：构造时复制 big.Int，getter 返回副本。
type MoneyChain struct {
	amount *big.Int
	meta   TokenMeta
}

func NewMoneyChainFromBigInt(amount *big.Int, ref TokenRef) (*MoneyChain, error) {
	if amount == nil {
		return nil, fmt.Errorf("amount is nil")
	}
	meta, err := GetTokenMeta(ref)
	if err != nil {
		return nil, err
	}
	return &MoneyChain{
		amount: new(big.Int).Set(amount),
		meta:   meta,
	}, nil
}

func NewMoneyChainFromString(amountMinor string, ref TokenRef) (*MoneyChain, error) {
	if amountMinor == "" {
		return nil, fmt.Errorf("amount is empty")
	}
	i := new(big.Int)
	if _, ok := i.SetString(amountMinor, 10); !ok {
		return nil, fmt.Errorf("invalid amount string: %s", amountMinor)
	}
	return NewMoneyChainFromBigInt(i, ref)
}

func (m *MoneyChain) GetString() string {
	if m == nil || m.amount == nil {
		return "0"
	}
	return m.amount.String()
}

func (m *MoneyChain) GetBigInt() *big.Int {
	if m == nil || m.amount == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(m.amount)
}

func (m *MoneyChain) Meta() TokenMeta {
	if m == nil {
		return TokenMeta{}
	}
	return m.meta
}

func (m *MoneyChain) IsZero() bool {
	if m == nil || m.amount == nil {
		return true
	}
	return m.amount.Sign() == 0
}

func (m *MoneyChain) IsNegative() bool {
	if m == nil || m.amount == nil {
		return false
	}
	return m.amount.Sign() < 0
}

func (m *MoneyChain) GreaterThan(other *MoneyChain) (bool, error) {
	cmp, err := m.Cmp(other)
	if err != nil {
		return false, err
	}
	return cmp > 0, nil
}

func (m *MoneyChain) LessThan(other *MoneyChain) (bool, error) {
	cmp, err := m.Cmp(other)
	if err != nil {
		return false, err
	}
	return cmp < 0, nil
}

// Equals 为兼容旧调用，语义同 Equal：比较两个 MoneyChain 是否相等（要求 token meta 一致）
func (m *MoneyChain) Equals(other *MoneyChain) (bool, error) {
	cmp, err := m.Cmp(other)
	if err != nil {
		return false, err
	}
	return cmp == 0, nil
}

func (m *MoneyChain) GreaterThanOrEqual(other *MoneyChain) (bool, error) {
	cmp, err := m.Cmp(other)
	if err != nil {
		return false, err
	}
	return cmp >= 0, nil
}

// NewMoneyChainFromBigIntWithMeta 直接使用提供的 TokenMeta 构造 MoneyChain（不依赖 registry）。
// 注意：优先推荐使用 NewMoneyChainFromBigInt/NewMoneyChainFromString（registry 单一可信来源）。
// 该方法主要用于“已从链上获取 decimals 的场景”作为兜底。
func NewMoneyChainFromBigIntWithMeta(amount *big.Int, meta TokenMeta) *MoneyChain {
	if amount == nil {
		amount = big.NewInt(0)
	}
	amountCopy := new(big.Int).Set(amount)
	metaCopy := meta
	return &MoneyChain{
		amount: amountCopy,
		meta:   metaCopy,
	}
}

// NewMoneyChainFromDecimalString 将面额字符串按 token decimals 转为最小单位（默认 Half-Even）
// 注意：链上接口应尽量传/返回最小单位；若不得已只能拿到面额字符串，可用此方法做解析。
func NewMoneyChainFromDecimalString(amountDecimal string, ref TokenRef) (*MoneyChain, error) {
	d, err := decimal.NewFromString(amountDecimal)
	if err != nil {
		return nil, fmt.Errorf("invalid decimal string: %s", amountDecimal)
	}
	meta, err := GetTokenMeta(ref)
	if err != nil {
		return nil, err
	}

	mul := pow10BigInt(int64(meta.Decimals))
	minorDec := d.Mul(decimal.NewFromBigInt(mul, 0)).RoundBank(0)
	return NewMoneyChainFromBigInt(minorDec.BigInt(), ref)
}

func pow10BigInt(n int64) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(n), nil)
}

// Add 链上金额加法（要求 token meta 一致）
func (m *MoneyChain) Add(other *MoneyChain) (*MoneyChain, error) {
	if m == nil || other == nil {
		return nil, fmt.Errorf("money is nil")
	}
	if m.meta != other.meta {
		return nil, fmt.Errorf("token mismatch: %s != %s", m.meta.Symbol, other.meta.Symbol)
	}
	return &MoneyChain{
		amount: new(big.Int).Add(m.amount, other.amount),
		meta:   m.meta,
	}, nil
}

// Subtract 为兼容旧调用，等价于 Sub
func (m *MoneyChain) Subtract(other *MoneyChain) (*MoneyChain, error) {
	return m.Sub(other)
}

// Sub 链上金额减法（要求 token meta 一致）
func (m *MoneyChain) Sub(other *MoneyChain) (*MoneyChain, error) {
	if m == nil || other == nil {
		return nil, fmt.Errorf("money is nil")
	}
	if m.meta != other.meta {
		return nil, fmt.Errorf("token mismatch: %s != %s", m.meta.Symbol, other.meta.Symbol)
	}
	return &MoneyChain{
		amount: new(big.Int).Sub(m.amount, other.amount),
		meta:   m.meta,
	}, nil
}

// Cmp 比较（要求 token meta 一致）
func (m *MoneyChain) Cmp(other *MoneyChain) (int, error) {
	if m == nil || other == nil {
		return 0, fmt.Errorf("money is nil")
	}
	if m.meta != other.meta {
		return 0, fmt.Errorf("token mismatch: %s != %s", m.meta.Symbol, other.meta.Symbol)
	}
	return m.amount.Cmp(other.amount), nil
}

// FormatDecimalAmount 将最小单位金额格式化为面额字符串（小数位数 = token decimals）
func (m *MoneyChain) FormatDecimalAmount() string {
	if m == nil {
		return "0"
	}
	return decimal.NewFromBigInt(m.amount, -int32(m.meta.Decimals)).String()
}

// Multiply 将金额按指定倍数放大/缩小（按 token decimals 回到最小单位，默认 Half-Even）
func (m *MoneyChain) Multiply(multiplier decimal.Decimal) (*MoneyChain, error) {
	if m == nil {
		return nil, fmt.Errorf("money is nil")
	}
	d := decimal.NewFromBigInt(m.amount, -int32(m.meta.Decimals))
	out := d.Mul(multiplier)
	minor := out.Mul(decimal.NewFromBigInt(pow10BigInt(int64(m.meta.Decimals)), 0)).RoundBank(0)
	return NewMoneyChainFromBigIntWithMeta(minor.BigInt(), m.meta), nil
}
