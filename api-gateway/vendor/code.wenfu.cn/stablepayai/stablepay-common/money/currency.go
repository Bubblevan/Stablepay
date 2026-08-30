package money

import (
	"database/sql/driver"
	"fmt"
)

// CurrencyKind 币种类型（业务域）
// 注意：业务域 money 只关心 法币/稳定币；链上原生币/其它加密资产不应出现在此包中。
type CurrencyKind int

const (
	CurrencyKindUnknown    CurrencyKind = 0
	CurrencyKindFiat       CurrencyKind = 1
	CurrencyKindStablecoin CurrencyKind = 2
)

// Currency 货币类型（业务域）
//
// - **amount** 的最小单位由 Precision 决定
// - **稳定币（USDT/USDC）业务口径统一 2 位小数**：0.01U 存 1
// - 链上 token decimals / 合约地址等属于链域，必须在 chainmoney 中处理
type Currency struct {
	Code      string       // 货币代码，如 "USD", "USDT"
	Precision int          // 精度（小数位数）
	Kind      CurrencyKind // 币种类型：法币/稳定币
}

// 常见主权货币
var (
	// 美元 - 2位小数
	USD = Currency{Code: "USD", Precision: 2, Kind: CurrencyKindFiat}
	// 人民币 - 2位小数
	CNY = Currency{Code: "CNY", Precision: 2, Kind: CurrencyKindFiat}
	// 欧元 - 2位小数
	EUR = Currency{Code: "EUR", Precision: 2, Kind: CurrencyKindFiat}
	// 英镑 - 2位小数
	GBP = Currency{Code: "GBP", Precision: 2, Kind: CurrencyKindFiat}
	// 日元 - 0位小数
	JPY = Currency{Code: "JPY", Precision: 0, Kind: CurrencyKindFiat}
	// 港币 - 2位小数
	HKD = Currency{Code: "HKD", Precision: 2, Kind: CurrencyKindFiat}
	// 新加坡元 - 2位小数
	SGD = Currency{Code: "SGD", Precision: 2, Kind: CurrencyKindFiat}
	// 澳元 - 2位小数
	AUD = Currency{Code: "AUD", Precision: 2, Kind: CurrencyKindFiat}
	// 加拿大元 - 2位小数
	CAD = Currency{Code: "CAD", Precision: 2, Kind: CurrencyKindFiat}
	// 瑞士法郎 - 2位小数
	CHF = Currency{Code: "CHF", Precision: 2, Kind: CurrencyKindFiat}
	// 巴西雷亚尔 - 2位小数
	BRL = Currency{Code: "BRL", Precision: 2, Kind: CurrencyKindFiat}
	// 印尼卢比 - 0位小数
	IDR = Currency{Code: "IDR", Precision: 0, Kind: CurrencyKindFiat}
	// 墨西哥比索 - 2位小数
	MXN = Currency{Code: "MXN", Precision: 2, Kind: CurrencyKindFiat}
	// 马来西亚林吉特 - 2位小数
	MYR = Currency{Code: "MYR", Precision: 2, Kind: CurrencyKindFiat}
	// 菲律宾比索 - 2位小数
	PHP = Currency{Code: "PHP", Precision: 2, Kind: CurrencyKindFiat}
	// 泰铢 - 2位小数
	THB = Currency{Code: "THB", Precision: 2, Kind: CurrencyKindFiat}
	// 越南盾 - 0位小数
	VND = Currency{Code: "VND", Precision: 0, Kind: CurrencyKindFiat}
	// 阿联酋迪拉姆 - 2位小数
	AED = Currency{Code: "AED", Precision: 2, Kind: CurrencyKindFiat}
	// 沙特里亚尔 - 2位小数
	SAR = Currency{Code: "SAR", Precision: 2, Kind: CurrencyKindFiat}
	// 卡塔尔里亚尔 - 2位小数
	QAR = Currency{Code: "QAR", Precision: 2, Kind: CurrencyKindFiat}
	// 科威特第纳尔 - 3位小数
	KWD = Currency{Code: "KWD", Precision: 3, Kind: CurrencyKindFiat}
	// 阿曼里亚尔 - 3位小数
	OMR = Currency{Code: "OMR", Precision: 3, Kind: CurrencyKindFiat}
	// 巴林第纳尔 - 3位小数
	BHD = Currency{Code: "BHD", Precision: 3, Kind: CurrencyKindFiat}
)

// 常见稳定币（业务口径）
var (
	// USDT (Tether) - 2位小数（业务域稳定币统一 2 位）
	USDT = Currency{Code: "USDT", Precision: 2, Kind: CurrencyKindStablecoin}
	// USDC (USD Coin) - 2位小数（业务域稳定币统一 2 位）
	USDC = Currency{Code: "USDC", Precision: 2, Kind: CurrencyKindStablecoin}
)

// NewCurrency 创建新的货币类型
func NewCurrency(code string, precision int) Currency {
	return Currency{
		Code:      code,
		Precision: precision,
		Kind:      CurrencyKindFiat, // 默认视为法币（业务域不应通过此方法创建稳定币/链上币）
	}
}

// NewStablecoinCurrency 创建稳定币货币类型（业务域）
func NewStablecoinCurrency(code string, precision int) Currency {
	return Currency{
		Code:      code,
		Precision: precision,
		Kind:      CurrencyKindStablecoin,
	}
}

// IsValid 验证货币是否有效
func (c Currency) IsValid() bool {
	// 检查货币代码是否为空
	if c.Code == "" {
		return false
	}

	// 检查精度是否合理
	if c.Precision < 0 || c.Precision > 18 {
		return false
	}

	// 业务域 money 只允许：法币/稳定币
	return c.Kind == CurrencyKindFiat || c.Kind == CurrencyKindStablecoin
}

// GetPrecision 获取货币精度
func (c Currency) GetPrecision() int {
	return c.Precision
}

// String 返回货币字符串表示
func (c Currency) String() string {
	return c.Code
}

// GetCurrencyByCode 根据货币代码获取预定义的货币
func GetCurrencyByCode(code string) (Currency, bool) {
	currencies := map[string]Currency{
		// 主权货币
		"USD": USD, "CNY": CNY, "EUR": EUR, "GBP": GBP, "JPY": JPY,
		"HKD": HKD, "SGD": SGD, "AUD": AUD, "CAD": CAD, "CHF": CHF,
		"BRL": BRL, "IDR": IDR, "MXN": MXN, "MYR": MYR, "PHP": PHP,
		"THB": THB, "VND": VND, "AED": AED, "SAR": SAR, "QAR": QAR,
		"KWD": KWD, "OMR": OMR, "BHD": BHD,
		// 稳定币（业务域）
		"USDT": USDT, "USDC": USDC,
	}

	currency, exists := currencies[code]
	return currency, exists
}

// GetAllFiatCurrencies 获取所有主权货币
func GetAllFiatCurrencies() []Currency {
	return []Currency{
		USD, CNY, EUR, GBP, JPY, HKD, SGD, AUD, CAD, CHF,
		BRL, IDR, MXN, MYR, PHP, THB, VND, AED, SAR, QAR,
		KWD, OMR, BHD,
	}
}

// GetAllStablecoinCurrencies 获取所有稳定币（业务域）
func GetAllStablecoinCurrencies() []Currency {
	return []Currency{USDT, USDC}
}

// GetAllCurrencies 获取所有预定义货币
func GetAllCurrencies() []Currency {
	var all []Currency
	all = append(all, GetAllFiatCurrencies()...)
	all = append(all, GetAllStablecoinCurrencies()...)
	return all
}

// GORM 支持：实现 database/sql Scanner 接口
// 用于从数据库读取货币代码字符串并转换为 Currency
func (c *Currency) Scan(value interface{}) error {
	if value == nil {
		*c = Currency{}
		return nil
	}

	var code string
	switch v := value.(type) {
	case []byte:
		code = string(v)
	case string:
		code = v
	default:
		return &CurrencyScanError{Value: value}
	}

	currency, exists := GetCurrencyByCode(code)
	if !exists {
		// 如果找不到预定义的货币，返回一个默认的 Currency（只包含 Code）
		*c = Currency{Code: code, Precision: 2, Kind: CurrencyKindUnknown}
		return nil
	}

	*c = currency
	return nil
}

// GORM 支持：实现 database/sql Valuer 接口
// 用于将 Currency 转换为货币代码字符串并存入数据库
func (c Currency) Value() (driver.Value, error) {
	if c.Code == "" {
		return nil, nil
	}
	return c.Code, nil
}

// CurrencyScanError 货币扫描错误
type CurrencyScanError struct {
	Value interface{}
}

func (e *CurrencyScanError) Error() string {
	return fmt.Sprintf("无法将 %T 类型扫描为 Currency", e.Value)
}
