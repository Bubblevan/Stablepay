# Money 包迁移指南

本文档帮助用户了解 Money 包的版本变更历史和 API 迁移指南。

> 说明：从 **0.4.0** 开始，`stablepay-common/money` 明确为**业务域金额库**（法币/业务稳定币），链相关能力迁移至 `stablepay-common/chainmoney`（链域金额库）。

---

## 版本变更概览

| 版本 | 发布日期 | 主要变化 |
|------|---------|---------|
| 0.4.0 | 2025-12 | 业务域/链域拆分；稳定币业务口径统一 2 位；链相关能力迁移到 chainmoney |
| 0.3.0 | 2024-12 | 多链支持、链特定精度、跨链汇总 |
| 0.2.0 | 2024-06 | int64 升级为 big.Int |
| 0.1.5 | 2024-03 | API 命名规范化 |
| 0.1.1 | 2024-01 | 汇率转换功能 |
| 0.1.0 | 2023-12 | 初始版本 |

---

## 0.4.0 迁移指南（重要）

### 核心变化

0.4.0 引入**领域拆分**，并统一业务稳定币精度：

- **`stablepay-common/money`（业务域）**
  - 仅表达 **法币 / 业务稳定币（USDT/USDC）**
  - 业务稳定币统一 **2 位小数**：`0.01` 存 `1`
  - 面额 → 最小单位默认使用 **Half-Even（银行家舍入）**
  - 增加 `CurrencyKind`（Fiat/Stablecoin）

- **`stablepay-common/chainmoney`（链域）**
  - 处理链上 token：原生币、ERC20/TRC20/SPL 等
  - token decimals、合约地址、链类型等由链域注册表维护
  - `MoneyChain` 内部使用最小单位 `big.Int`

### 重要的不兼容变更

0.4.0 起，以下链相关能力不再属于 `money`：

- 链类型与链信息（`ChainType/GetChain/ParseChainType`）
- 链特定稳定币与原生币常量（如 `USDT_BSC/USDT_ETH/ETH/BNB/...`）
- 链币种注册表与查询（`GetCurrency/GetNativeToken/GetCurrenciesByChain/GetStandardCurrency`）
- 地址验证与标准化（`ValidateAddress/NormalizeAddress`）
- 跨链汇总标准精度 API（`ToStandardPrecision/AddStandard/SubStandard/MulStandard/DivStandard`）

这些能力请迁移到 `chainmoney`（或对应链服务）中实现/调用。

### 迁移步骤

1) **区分“业务金额”与“链上金额”**
- **业务金额**：支付订单、报价、手续费（业务口径）等 → `money.Money`
- **链上金额**：RPC 返回、合约调用、余额、gas、链上手续费等 → `chainmoney.MoneyChain`

2) **业务稳定币从 6 位改为 2 位**

如果你的服务此前把 `money.USDT/money.USDC` 当作 **6 位** 使用（旧口径），需要做两件事：

- **代码迁移**：业务金额用 `money.USDT/money.USDC`（2 位）重新解析/格式化
- **数据迁移（如已有存量）**：若历史以 6 位最小单位存储（minorUnits），新口径需换算为 2 位：
  - \(newMinor = oldMinor / 10^{(6-2)} = oldMinor / 10000\)
  - 若不能整除，按业务约定选择舍入策略（默认建议 Half-Even）

3) **链上金额全部改用 chainmoney**

示例（链上最小单位字符串 → MoneyChain）：

```go
ref := chainmoney.TokenRef{
    ChainType:        chainmoney.ChainTypeEthereum,
    Symbol:           "USDT",
    ContractAddress:  "0xdAC17F958D2ee523a2206206994597C13D831ec7",
}
amt, _ := chainmoney.NewMoneyChainFromString("1000000", ref) // 1 USDT (6 decimals)
fmt.Println(amt.FormatDecimalAmount()) // "1"
```

4) **对外接口/展示**

- 业务展示：`money.Money` 用 `FormatDecimalAmount()` / `FormatDecimalAmountWithPrecision(n)`
- 链上展示：`chainmoney.MoneyChain.FormatDecimalAmount()`

---

## 0.3.0 迁移指南（历史版本）

> 说明：本节保留历史内容用于理解 0.3.0 的设计；从 0.4.0 开始，这些链能力已迁移至 `chainmoney`。

### 核心变化

0.3.0 是一个重大功能升级，引入了多链支持和跨链汇总能力：

| 变化项 | 说明 |
|-------|------|
| 🔗 **ChainType** | 新增链类型定义（Ethereum、BSC、TRON、Solana、Polygon） |
| 💰 **链特定币种** | 新增 USDT_BSC、USDT_ETH、USDC_BSC 等链特定常量 |
| 🔄 **跨链汇总** | 新增 ToStandardPrecision()、AddStandard() 等方法 |
| 🏠 **地址验证** | 新增 ValidateAddress()、NormalizeAddress() 函数 |
| 🐛 **精度修复** | 修复 BNB 精度（8→18）、BSC 上 USDT/USDC 精度（6→18） |

### 影响范围

| 场景 | 影响程度 | 说明 |
|-----|---------|------|
| 处理 BSC 链上 USDT/USDC | **高** | 精度从 6 位变为 18 位 |
| 使用 `money.BNB` | **高** | 精度从 8 位变为 18 位 |
| 只处理 ETH/TRON/Solana 链 | 低 | 精度定义未变化 |
| 只使用 `NewMoneyFromString()` | 无 | 不涉及精度转换 |

### 新增 API

#### 链相关

```go
// 链类型常量
money.ChainTypeEthereum  // 2
money.ChainTypeBSC       // 4
money.ChainTypeTron      // 1
money.ChainTypeSolana    // 3
money.ChainTypePolygon   // 5

// 链信息查询
chain, _ := money.GetChain(money.ChainTypeBSC)
chainType, _ := money.ParseChainType("bsc")

// 链特定币种查询
currency, _ := money.GetCurrency("USDT", money.ChainTypeBSC)
nativeToken, _ := money.GetNativeToken(money.ChainTypeBSC)
currencies, _ := money.GetCurrenciesByChain(money.ChainTypeBSC)
```

#### 链特定稳定币常量

```go
// USDT
money.USDT_ETH     // 6 位精度
money.USDT_BSC     // 18 位精度
money.USDT_TRON    // 6 位精度
money.USDT_SOL     // 6 位精度
money.USDT_POLYGON // 6 位精度

// USDC
money.USDC_ETH     // 6 位精度
money.USDC_BSC     // 18 位精度
money.USDC_SOL     // 6 位精度
money.USDC_POLYGON // 6 位精度
```

#### 跨链汇总方法

```go
// 转换为标准精度
standardMoney, _ := chainSpecificMoney.ToStandardPrecision()

// 跨链计算（自动转换精度）
total, _ := balance1.AddStandard(balance2)
diff, _ := balance1.SubStandard(balance2)
result, _ := balance.MulStandard(decimal.NewFromFloat(1.5))
result, _ := balance.DivStandard(decimal.NewFromInt(2))
```

#### 地址工具

```go
// 验证地址格式
err := money.ValidateAddress(address, money.ChainTypeEthereum)

// 标准化地址（EVM 转 EIP-55 校验和格式）
normalized, _ := money.NormalizeAddress(address, money.ChainTypeEthereum)
```

### 代码迁移示例

#### 场景一：处理 BSC 链上的 USDT 交易

```go
// ❌ 0.2.x 代码（BSC 场景下精度错误）
amount, _ := money.NewMoneyFromDecimalString("1.5", money.USDT)
// 结果：1500000（按 6 位精度），BSC 链上会解析为 0.0000000000015 USDT

// ✅ 0.3.0 代码（正确）
amount, _ := money.NewMoneyFromDecimalString("1.5", money.USDT_BSC)
// 结果：1500000000000000000（按 18 位精度）

// 或使用动态查询
currency, _ := money.GetCurrency("USDT", chainType)
amount, _ := money.NewMoneyFromDecimalString("1.5", currency)
```

#### 场景二：跨链余额汇总

```go
// ❌ 0.2.x 代码（不同精度直接相加会出错）
balanceETH, _ := money.NewMoneyFromString("1500000", money.USDT)
balanceBSC, _ := money.NewMoneyFromString("2500000000000000000", money.USDT)
// 无法正确汇总，因为精度不同

// ✅ 0.3.0 代码（使用跨链汇总 API）
balanceETH, _ := money.NewMoneyFromString("1500000", money.USDT_ETH)
balanceBSC, _ := money.NewMoneyFromString("2500000000000000000", money.USDT_BSC)
total, _ := balanceETH.AddStandard(balanceBSC)
// 结果：4000000（标准 6 位精度），正确！
```

#### 场景三：迁移自定义的链类型定义

```go
// ❌ 0.2.x 代码（服务内部定义）
type ChainType int32
const ChainTypeBSC ChainType = 4

// ✅ 0.3.0 代码（使用 money 包定义）
import "code.wenfu.cn/stablepay/stablepay-common/money"
// 直接使用 money.ChainType 和 money.ChainTypeBSC
```

#### 场景四：迁移自定义的精度逻辑

```go
// ❌ 0.2.x 代码（服务内部维护精度）
func getTokenDecimals(token string, chain int) int {
    if chain == 4 && token == "USDT" {
        return 18
    }
    return 6
}

// ✅ 0.3.0 代码（使用 money 包）
func getTokenDecimals(token string, chainType money.ChainType) int {
    currency, err := money.GetCurrency(token, chainType)
    if err != nil {
        return 6 // 默认值
    }
    return currency.Precision
}
```

### BNB 精度修复说明

0.3.0 将 BNB 精度从 **8 位修正为 18 位**（BSC 链标准）。

**影响评估**：

| 使用方式 | 是否受影响 | 说明 |
|---------|----------|------|
| `NewMoneyFromDecimalString("1.0", BNB)` | **是** | 结果从 100000000 变为 1000000000000000000 |
| `FormatDecimalAmount()` | **是** | 显示格式变化 |
| `NewMoneyFromString(minorUnit, BNB)` | 否 | 最小单位不涉及精度转换 |

**迁移建议**：检查所有使用 `money.BNB` 的代码，特别是涉及面额转换的地方。

### 链无关稳定币语义说明

0.3.0 明确了 `money.USDT` 和 `money.USDC` 的语义：

- 它们是**链无关的稳定币**，精度为「本意精度」6 位
- `ChainType = nil`
- 用于**跨链汇总**和**总余额展示**
- **不应用于处理链上交易**（应使用 `USDT_BSC`、`USDT_ETH` 等）

```go
// ✅ 正确使用场景
total := balanceETH.AddStandard(balanceBSC)  // 返回链无关 USDT
fmt.Printf("总余额: %s USDT\n", total.FormatDecimalAmount(2))

// ❌ 错误使用场景
amount, _ := money.NewMoneyFromDecimalString("1.0", money.USDT)
// 然后用这个 amount 发送 BSC 链上交易 —— 精度错误！
```

### 升级步骤

1. **升级 money 包版本**
   ```bash
   go get code.wenfu.cn/stablepay/stablepay-common/money@latest
   go mod tidy
   ```

2. **检查 Currency 使用场景**
   
   搜索以下关键词，逐一检查：
   - `money.USDT`、`money.USDC` —— 如果用于链上交易，改为链特定常量
   - `money.BNB` —— 检查精度修复影响
   - `GetCurrencyByCode(` —— 如果用于链上交易，改为 `GetCurrency()`
   - `NewMoneyFromDecimalString(` —— 确保传入正确的 Currency

3. **运行测试**
   ```bash
   go test -v ./...
   ```

4. **验证关键业务场景**
   - BSC 链上 USDT/USDC 金额显示是否正确
   - 跨链汇总计算是否正确
   - 地址验证是否正常

---

## 0.2.0 迁移指南

### 核心变化：从 int64 升级到 big.Int

0.2.0 将内部存储从 `int64` 升级为 `big.Int`：

**升级原因**：
- ✅ 支持任意精度，完美支持高精度加密货币（如 ETH 18位精度）
- ✅ 避免 int64 溢出问题（例如 10 ETH 超出 int64 范围）
- ✅ 与区块链 SDK 无缝集成
- ✅ 符合金融行业标准

### 新增 API

| API | 说明 | 使用场景 |
|-----|------|---------|
| `NewMoneyFromBigInt(amount, currency)` | 从 big.Int 创建 | 区块链交互、高精度币种 |
| `GetBigInt()` | 获取 big.Int 副本 | 区块链交易、高精度计算 |
| `GetString()` | 获取最小单位字符串 | 通用方法、数据库存储 |
| `GetInt64()` | 获取 int64（检查溢出） | 需要确保不溢出时使用 |

### 迁移示例

#### 区块链交易处理（ETH）

```go
// ❌ 0.1.x 代码（存在溢出风险）
amount := int64(10000000000000000000) // 编译错误！10 ETH 超出 int64

// ✅ 0.2.0 代码
blockchainAmount := new(big.Int)
blockchainAmount.SetString("10000000000000000000", 10) // 10 ETH
ethMoney, _ := money.NewMoneyFromBigInt(blockchainAmount, money.ETH)

// 转回 big.Int 用于区块链交易
txAmount := ethMoney.GetBigInt()
```

#### 数据库存储

```go
// 低精度币种（USD）- 可继续使用 int64
usdMoney, _ := money.NewMoneyFromInt64(10050, money.USD)
dbValue := usdMoney.GetInt64Unsafe() // 10050

// 高精度币种（ETH）- 使用字符串
ethMoney, _ := money.NewMoneyFromString("5500000000000000000", money.ETH)
dbString := ethMoney.GetString() // "5500000000000000000"
```

### 兼容性说明

0.2.0 保持了向后兼容性：

**完全兼容**：
- `NewMoneyFromInt64()` - 继续支持
- `NewMoneyFromString()` - 继续支持
- `NewMoneyFromDecimal()` / `NewMoneyFromDecimalString()` - 继续支持
- `GetInt64Unsafe()` - 继续支持（高精度可能截断）
- 所有算术运算和比较方法

---

## 0.1.5 迁移指南

### API 命名规范化

0.1.5 更新了 API 命名，使「面额」和「最小单位」的概念更加清晰：

| 旧 API | 新 API | 说明 |
|--------|--------|------|
| `NewMoney` | `NewMoneyFromMinorUnits` | 从最小单位创建 |
| `NewMoneyFromString` | `NewMoneyFromDecimalAmountString` | 从面额字符串创建 |
| `GetAmount` | `GetDecimalAmount` | 获取面额 |
| `GetAmountInt64` | `GetAmountInMinorUnits` | 获取最小单位 |
| `FormatAmountString` | `FormatDecimalAmount` | 格式化面额 |

旧 API 仍可用但已标记为 Deprecated。

---

## 常见问题

### Q: 0.3.0 升级会破坏现有代码吗？

A: 
- **不处理 BSC 链 USDT/USDC 的项目**：基本无影响
- **处理 BSC 链 USDT/USDC 的项目**：需要适配，精度从 6 位变为 18 位
- **使用 money.BNB 的项目**：需要检查，精度从 8 位变为 18 位

### Q: 如何判断我的代码是否受影响？

A: 搜索以下关键词：
```bash
grep -r "money.USDT" .
grep -r "money.USDC" .
grep -r "money.BNB" .
grep -r "GetCurrencyByCode" .
grep -r "NewMoneyFromDecimalString" .
```

如果这些代码涉及 BSC 链交易，则需要适配。

### Q: 链无关的 money.USDT 还能用吗？

A: 需要结合版本理解：
- **0.3.0 及以前**：它表示链无关稳定币（6 位），用于跨链汇总/展示
- **0.4.0 开始**：`money.USDT/money.USDC` 表示业务稳定币（2 位），链上请使用 `chainmoney`

### Q: 数据库需要修改吗？

A:
- **低精度币种（USD）**：可继续使用 BIGINT
- **高精度币种（链上 token）**：建议使用 VARCHAR/TEXT 存储最小单位字符串（或 big.Int 序列化）
- **业务稳定币（0.4.0）**：如历史以 6 位存储，需要按 0.4.0 的换算规则迁移

### Q: 如何验证迁移是否成功？

A:
1. 运行单元测试：`go test -v ./...`
2. 验证关键业务金额展示与计算（业务稳定币 2 位、法币可变精度）
3. 验证链上金额展示与入库/出库（token decimals/contract/chainType 全部来自 chainmoney）


