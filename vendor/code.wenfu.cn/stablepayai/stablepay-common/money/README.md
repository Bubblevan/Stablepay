# StablePay Money（0.4.0，业务域）

`stablepay-common/money` 是 **业务域金额值对象库**：只用于 **法币** 与 **业务稳定币（USDT/USDC）** 的金额表达、计算与格式化。

> 链上金额（token decimals、合约地址、链类型、原生币、链上稳定币精度差异）请使用 `stablepay-common/chainmoney`。

## 安装

```bash
go get code.wenfu.cn/stablepay/stablepay-common/money
```

## 0.4.0 业务口径（必须了解）

- **稳定币（USDT/USDC）业务口径统一 2 位小数**：`0.01` 存 `1`（minor units）
- **法币精度可变**：例如 JPY=0、USD=2、KWD=3
- **舍入规则**：面额 → 最小单位默认使用 **Half-Even（银行家舍入）**
- **职责边界**：`money` 不再包含任何链相关 API（详见 `money/MIGRATION_GUIDE.md` 的 0.4.0 章节）

## 关键结构体与能力（仍然保留）

### Currency / CurrencyKind

- `Currency{Code, Precision, Kind}`
- `CurrencyKindFiat`、`CurrencyKindStablecoin`
- 常用内置币种：法币（USD/CNY/.../KWD/OMR/BHD 等）与稳定币（USDT/USDC）

### Money（不可变值对象）

`Money` 内部以 **最小单位 big.Int** 存储，保证精确计算；并提供：
- 构造：`NewMoneyFromString` / `NewMoneyFromDecimalString` / `NewMoneyFromDecimal` / `NewMoneyFromBigInt` / `NewMoneyFromInt64` / `Zero`
- 访问：`GetString` / `GetDecimal` / `GetBigInt` / `GetInt64` / `GetInt64Unsafe` / `GetCurrency` / `GetPrecision`
- 运算：`Add` / `Subtract` / `Multiply` / `Divide` / `Negate` / `Abs`
- 比较：`Compare` / `Equals` / `GreaterThan` / `LessThan` / `GreaterThanOrEqual` / `LessThanOrEqual`
- 舍入：`RoundWithMode` + 便利方法（`RoundHalfEven` 等）+ `RoundTo`
- 批量：`Sum` / `Max` / `Min`
- 汇率：`ExchangeRate` / `ExchangeRateProvider` / `SimpleExchangeRateProvider` + `ConvertTo/ConvertWithProvider/...`
- 序列化：`MarshalJSON` / `UnmarshalJSON`

## 快速开始（业务金额）

> 例子全部是 **业务域金额**：法币 + USDT/USDC（2 位），不涉及链上 decimals。

```go
package main

import (
	"fmt"

	"code.wenfu.cn/stablepay/stablepay-common/money"
)

func main() {
	// 业务稳定币：2 位小数（0.01 存 1）
	usdt1, _ := money.NewMoneyFromDecimalString("12.34", money.USDT) // minor=1234
	fmt.Println(usdt1.GetString())          // "1234"
	fmt.Println(usdt1.FormatDecimalAmount()) // "12.34"

	// 法币：精度取决于 Currency
	jpy, _ := money.NewMoneyFromDecimalString("100", money.JPY) // 0 位
	fmt.Println(jpy.GetString())          // "100"
	fmt.Println(jpy.FormatDecimalAmount()) // "100"

	kwd, _ := money.NewMoneyFromDecimalString("1.234", money.KWD) // 3 位
	fmt.Println(kwd.GetString())          // "1234"
	fmt.Println(kwd.FormatDecimalAmount()) // "1.234"
}
```

## 舍入（Half-Even）

面额 → 最小单位默认使用银行家舍入：

```go
// USD 2位：1.005 -> 1.00（Half-Even）
m, _ := money.NewMoneyFromDecimalString("1.005", money.USD)
fmt.Println(m.GetString())           // "100"
fmt.Println(m.FormatDecimalAmount()) // "1.00"
```

## 常用运算与比较

```go
import "github.com/shopspring/decimal"

sum, _ := a.Add(b)
diff, _ := a.Subtract(b)
mul, _ := a.Multiply(decimal.NewFromFloat(2.5))
div, _ := a.Divide(decimal.NewFromFloat(2))

eq := a.Equals(b)
cmp, _ := a.Compare(b) // -1/0/1
```

## 汇率转换

```go
import "github.com/shopspring/decimal"

provider := money.NewSimpleExchangeRateProvider()
provider.SetRate(money.USD, money.CNY, decimal.RequireFromString("7.2"))

usd, _ := money.NewMoneyFromDecimalString("12.34", money.USD)
cny, _ := usd.ConvertWithProvider(money.CNY, provider)
fmt.Println(cny.FormatDecimalAmount())
```

## JSON 序列化

```go
b, _ := usdt1.MarshalJSON()
fmt.Println(string(b))

var m money.Money
_ = m.UnmarshalJSON(b)
```

## 和 MoneyDTO 的配合范式（跨服务/跨层传递）

`Money` 是 Go 进程内值对象；跨服务（Thrift/HTTP/RPC）建议使用 `MoneyDTO`：

- `amount_minor`：**最小单位整数**（字符串），避免精度丢失
- `currency_code`：币种码（`"USD" / "USDT" / "KWD"` 等）

对应 Thrift 定义（位于 `stablepay-idl/idl/common/money.thrift`）：

```go
// generated: package commonmoney
type MoneyDTO struct {
    AmountMinor  string `json:"amount_minor"`
    CurrencyCode string `json:"currency_code"`
}
```

### 推荐约定

- **服务边界入参**：收到 `MoneyDTO` 后，立即转换为 `money.Money` 再进入业务计算
- **服务边界出参**：业务侧返回时，将 `money.Money` 转回 `MoneyDTO` 再输出
- **DB 存储**：建议同样存 `(amount_minor, currency_code)` 两列，避免 float/decimal 序列化差异

### Go 示例（Money ↔ MoneyDTO）

```go
import (
    "fmt"

    "code.wenfu.cn/stablepay/stablepay-common/money"
    // 以 chaincore 生成代码为例（package 名是 commonmoney）
    "code.wenfu.cn/stablepay/stablepay-idl/generated/go/chaincore/commonmoney"
)

func MoneyToDTO(m *money.Money) *commonmoney.MoneyDTO {
    if m == nil {
        return nil
    }
    return &commonmoney.MoneyDTO{
        AmountMinor:  m.GetString(),
        CurrencyCode: m.GetCurrency().Code,
    }
}

func DTOToMoney(dto *commonmoney.MoneyDTO) (*money.Money, error) {
    if dto == nil {
        return nil, fmt.Errorf("dto is nil")
    }
    curr, ok := money.GetCurrencyByCode(dto.CurrencyCode)
    if !ok {
        return nil, fmt.Errorf("unknown currency_code: %s", dto.CurrencyCode)
    }
    // 注意：amount_minor 是最小单位整数
    return money.NewMoneyFromString(dto.AmountMinor, curr)
}
```

> 注意：`MoneyDTO` 仅表达**业务口径**金额；链上金额请使用 `chainmoney` 的 token+decimals 口径（`MoneyChain`），不要把链上最小单位塞进业务 MoneyDTO。

## 何时用 chainmoney（链上金额）

当你处理 **链上金额**（RPC 返回、合约调用、余额、Gas、链上 USDT/USDC 在不同链上 decimals 不同等）时：

- 使用 `stablepay-common/chainmoney.MoneyChain`
- 使用 `TokenRef{chain_type, symbol, contract}` 精确标识 token

> 迁移说明见 `money/MIGRATION_GUIDE.md` 的 0.4.0 章节。

## API 参考（0.4.0）

### 构造函数

| 函数 | 说明 |
|------|------|
| `NewMoneyFromString(amountStr, currency)` | 从**最小单位字符串**创建（推荐通用方法） |
| `NewMoneyFromDecimalString(amountStr, currency)` | 从**面额字符串**创建（默认 Half-Even） |
| `NewMoneyFromDecimal(amount, currency)` | 从**面额 decimal** 创建（默认 Half-Even） |
| `NewMoneyFromBigInt(amount, currency)` | 从 **big.Int 最小单位** 创建（会复制入参） |
| `NewMoneyFromInt64(amount, currency)` | 从 **int64 最小单位** 创建（便利方法） |
| `Zero(currency)` | 创建零金额 |

### Currency 相关

| 函数/方法 | 说明 |
|---|---|
| `GetCurrencyByCode(code)` | 通过 code 获取预置币种 |
| `GetAllFiatCurrencies()` | 所有法币 |
| `GetAllStablecoinCurrencies()` | 所有业务稳定币 |
| `Currency.IsValid()` | 校验币种（仅允许 Fiat/Stablecoin） |
### Money 访问器 / 格式化

| 方法 | 说明 |
|---|---|
| `GetString()` | 获取最小单位字符串（推荐持久化/跨服务传递） |
| `GetDecimal()` | 获取面额 `decimal.Decimal` |
| `GetBigInt()` | 获取最小单位 `big.Int`（返回副本） |
| `GetInt64()` | 获取最小单位 `int64`（溢出会返回错误） |
| `GetInt64Unsafe()` | 获取最小单位 `int64`（不检查溢出） |
| `GetCurrency()` | 获取币种 |
| `GetPrecision()` | 获取币种精度 |
| `FormatDecimalAmount()` | 按币种精度格式化面额 |
| `FormatDecimalAmountWithPrecision(n)` | 指定展示精度格式化 |
| `FormatDecimalAmountTrimZero()` | 格式化并去尾零 |

### Money 运算 / 比较 / 舍入

| 类别 | 方法 |
|---|---|
| 运算 | `Add` `Subtract` `Multiply` `Divide` `Negate` `Abs` |
| 比较 | `Compare` `Equals` `GreaterThan` `LessThan` `GreaterThanOrEqual` `LessThanOrEqual` |
| 舍入 | `RoundWithMode` `RoundUp` `RoundDown` `RoundHalfUp` `RoundHalfEven` `RoundTo` |
| 批量 | `Sum` `Max` `Min` |

### 汇率

| 类型/方法 | 说明 |
|---|---|
| `ExchangeRate` | 汇率对象（from/to/rate/timestamp） |
| `SimpleExchangeRateProvider` | 简单汇率提供者（内存 map） |
| `ConvertTo` | 直接用 rate 转换 |
| `ConvertWithExchangeRate` | 用 ExchangeRate 转换 |
| `ConvertWithProvider` | 用 provider 转换 |

## 注意事项（0.4.0）

### 1) 稳定币精度（业务口径 2 位）

- `money.USDT/money.USDC` 在 **0.4.0 是业务口径 2 位**（0.01 存 1）
- **链上 USDT/USDC 的 decimals** 不在 `money` 管理，必须用 `chainmoney`

### 2) 币种一致性

- `Money` 的加减比较要求 **Currency 完全一致**；不同币种不能直接运算

### 3) 存储与传输建议

- **数据库/跨服务传递**建议存：`amount_minor`（string）+ `currency_code`
- 需要 `int64` 时用 `GetInt64()`（可检测溢出），避免滥用 `GetInt64Unsafe()`

## 测试

```bash
go test -v ./...
```

## 迁移指南

请参考 [MIGRATION_GUIDE.md](MIGRATION_GUIDE.md)，其中包含：
- 版本演进历史（0.1.x → 0.4.0）
- 0.4.0 领域拆分与迁移步骤
