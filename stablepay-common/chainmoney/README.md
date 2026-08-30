# StablePay ChainMoney（链域）

`stablepay-common/chainmoney` 是 **链域金额库**：用于表达与计算 **链上 token 金额**（最小单位整数 + token 元数据）。

它解决的问题：
- 链上金额必须使用 **token decimals**（USDT 在 ETH/TRON=6、BSC=18…）
- 同链同 symbol 可能存在多合约，需要通过 **contract** 做区分（防假币）
- 链上金额内部使用 `big.Int`，避免精度丢失

> 业务口径金额（法币/业务稳定币 2 位）请使用 `stablepay-common/money`。

## 安装

```bash
go get code.wenfu.cn/stablepay/stablepay-common/chainmoney
```

## 核心概念

### TokenRef（唯一标识一个 token）

```go
type TokenRef struct {
    ChainType ChainType // 链
    Symbol    string    // token symbol（会自动大写）
    Contract  string    // 可选：同名多合约时必须传；也用于防假币
}
```

### TokenMeta（token 元数据）

```go
type TokenMeta struct {
    ChainType ChainType
    Symbol    string
    Decimals  int32
    IsNative  bool
    Contract  string
}
```

### MoneyChain（链上金额值对象）

- 内部存 **最小单位 `big.Int`**
- 绑定 `TokenMeta`（decimals 等）
- 构造时会复制 `big.Int`，getter 返回副本，避免外部篡改

## Registry（元数据注册表）

chainmoney 内置了一个代码级 registry（见 `registry.go:init()`），包含常见链的原生币与 USDT/USDC。

### GetTokenMeta 的 contract 规则（重要）

- 如果某个 `chain + symbol` 存在**合约特化**注册项（`chain:symbol:contract`），那么调用方 **必须提供 contract**，否则会报错（用于防假币）。
- 如果不存在合约特化项，则 `TokenRef` 不带 contract 也能找到默认项；带 contract 时会把 contract 回填到返回 meta 中，方便透传。

## 快速开始

### 1) 从最小单位字符串创建（推荐）

```go
ref := chainmoney.TokenRef{
    ChainType: chainmoney.ChainTypeEthereum,
    Symbol:    "USDT",
}
m, _ := chainmoney.NewMoneyChainFromString("1000000", ref) // 1 USDT (6 decimals)
fmt.Println(m.GetString())          // "1000000"
fmt.Println(m.FormatDecimalAmount()) // "1"
```

### 2) 从面额字符串创建（兜底）

链上接口应尽量传/返最小单位；如果只能拿到面额字符串：

```go
ref := chainmoney.TokenRef{ChainType: chainmoney.ChainTypeBSC, Symbol: "USDT"}
m, _ := chainmoney.NewMoneyChainFromDecimalString("1.23", ref) // BSC USDT 18 decimals
fmt.Println(m.GetString()) // "1230000000000000000"
```

### 3) 运算与比较（要求 token meta 一致）

```go
a, _ := chainmoney.NewMoneyChainFromString("100", ref)
b, _ := chainmoney.NewMoneyChainFromString("20", ref)

sum, _ := a.Add(b)
diff, _ := a.Sub(b)

cmp, _ := a.Cmp(b) // -1/0/1
gt, _ := a.GreaterThan(b)
```

### 4) 带 contract 的 token（防假币）

TRON USDT 在 registry 里存在 canonical 合约特化项，因此推荐传入 contract：

```go
ref := chainmoney.TokenRef{
    ChainType: chainmoney.ChainTypeTron,
    Symbol:    "USDT",
    Contract:  "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t",
}
m, _ := chainmoney.NewMoneyChainFromString("1000000", ref)
```

如果链上回调里带了合约地址，建议**强制透传**到 TokenRef，避免同名假币误入。

## 与 money 的转换（业务 ↔ 链）

`convert.go` 提供了业务金额 `money.Money` 与链上金额 `chainmoney.MoneyChain` 的转换函数：

- `BusinessToChain(...)`
- `ChainToBusiness(...)`

> 提示：链上 decimals 与业务稳定币 2 位之间会存在精度/舍入问题；请按业务场景选择合适的舍入策略（Half-Even / Ceil 等）。

## 常见坑

- **不要把链上最小单位当成业务 money**：业务稳定币 2 位，链上 USDT/USDC decimals 可能是 6/18。
- **不要忽略 contract**：同链同 symbol 可能多合约；registry 的 contract 规则可用于防假币。


