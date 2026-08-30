package chainmoney

import (
	"fmt"
	"math/big"

	bizmoney "code.wenfu.cn/stablepay/stablepay-common/money"
)

// BusinessToChainHalfEven 业务口径 -> 链上口径（显式 Half-Even）
func BusinessToChainHalfEven(biz *bizmoney.Money, token TokenRef) (*MoneyChain, error) {
	return businessToChainWithMode(biz, token, roundingHalfEven)
}

// BusinessToChainCeil 业务口径 -> 链上口径（显式 Ceil，用于 expected 防少收场景）
func BusinessToChainCeil(biz *bizmoney.Money, token TokenRef) (*MoneyChain, error) {
	return businessToChainWithMode(biz, token, roundingCeil)
}

// BusinessToChain 业务口径 -> 链上口径（默认：不允许发生降精度；若 decimals < bizScale 直接报错）
func BusinessToChain(biz *bizmoney.Money, token TokenRef) (*MoneyChain, error) {
	return businessToChainWithMode(biz, token, roundingErrorOnDownscale)
}

// ChainToBusiness 链上口径 -> 业务口径（默认 Half-Even）
func ChainToBusiness(chain *MoneyChain, currencyCode string) (*bizmoney.Money, error) {
	return chainToBusinessWithMode(chain, currencyCode, roundingHalfEven)
}

// ChainToBusinessCeil 链上口径 -> 业务口径（显式 Ceil）
func ChainToBusinessCeil(chain *MoneyChain, currencyCode string) (*bizmoney.Money, error) {
	return chainToBusinessWithMode(chain, currencyCode, roundingCeil)
}

type roundingMode int

const (
	roundingErrorOnDownscale roundingMode = iota
	roundingHalfEven
	roundingCeil
)

func businessToChainWithMode(biz *bizmoney.Money, token TokenRef, mode roundingMode) (*MoneyChain, error) {
	if biz == nil {
		return nil, fmt.Errorf("biz money is nil")
	}
	meta, err := GetTokenMeta(token)
	if err != nil {
		return nil, err
	}

	fromScale := int32(biz.GetPrecision())
	toScale := meta.Decimals

	amt := biz.GetBigInt()
	out, err := scaleConvert(amt, fromScale, toScale, mode)
	if err != nil {
		return nil, err
	}
	return &MoneyChain{
		amount: out,
		meta:   meta,
	}, nil
}

func chainToBusinessWithMode(chain *MoneyChain, currencyCode string, mode roundingMode) (*bizmoney.Money, error) {
	if chain == nil {
		return nil, fmt.Errorf("chain money is nil")
	}
	c, ok := bizmoney.GetCurrencyByCode(currencyCode)
	if !ok {
		return nil, fmt.Errorf("unknown currency code: %s", currencyCode)
	}
	fromScale := chain.meta.Decimals
	toScale := int32(c.GetPrecision())

	out, err := scaleConvert(chain.GetBigInt(), fromScale, toScale, mode)
	if err != nil {
		return nil, err
	}
	return bizmoney.NewMoneyFromBigInt(out, c)
}

// scaleConvert 将 amount 从 fromScale 转到 toScale
// - 放大（toScale>fromScale）：乘以 10^(diff)，无损\n+// - 缩小（toScale<fromScale）：需要舍入；mode 控制策略
func scaleConvert(amount *big.Int, fromScale, toScale int32, mode roundingMode) (*big.Int, error) {
	if amount == nil {
		return nil, fmt.Errorf("amount is nil")
	}
	if fromScale == toScale {
		return new(big.Int).Set(amount), nil
	}

	diff := int64(toScale - fromScale)
	if diff > 0 {
		mul := pow10(diff)
		return new(big.Int).Mul(amount, mul), nil
	}

	// downscale
	if mode == roundingErrorOnDownscale {
		return nil, fmt.Errorf("downscale not allowed: fromScale=%d toScale=%d", fromScale, toScale)
	}

	div := pow10(-diff)
	q, r := new(big.Int).QuoRem(amount, div, new(big.Int))
	if r.Sign() == 0 {
		return q, nil
	}

	switch mode {
	case roundingCeil:
		// 对正数向上取整；对负数向上（朝 +∞）等价于截断即可
		if amount.Sign() > 0 {
			return q.Add(q, big.NewInt(1)), nil
		}
		return q, nil
	case roundingHalfEven:
		return roundHalfEven(q, r, div, amount.Sign()), nil
	default:
		return nil, fmt.Errorf("unknown rounding mode")
	}
}

func pow10(n int64) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(n), nil)
}

// roundHalfEven 银行家舍入：比较 2*|r| 与 |div|
// - < 0.5：不变
// - > 0.5：按 sign 加/减 1
// - = 0.5：向偶数舍入（q 为偶数则不变，否则按 sign 加/减 1）
func roundHalfEven(q, r, div *big.Int, sign int) *big.Int {
	absR := new(big.Int).Abs(r)
	absDiv := new(big.Int).Abs(div)

	twoR := new(big.Int).Mul(absR, big.NewInt(2))
	cmp := twoR.Cmp(absDiv)
	if cmp < 0 {
		return q
	}
	if cmp > 0 {
		return addBySign(q, sign)
	}

	// tie
	if isEven(q) {
		return q
	}
	return addBySign(q, sign)
}

func addBySign(q *big.Int, sign int) *big.Int {
	if sign >= 0 {
		return new(big.Int).Add(q, big.NewInt(1))
	}
	return new(big.Int).Sub(q, big.NewInt(1))
}

func isEven(x *big.Int) bool {
	return new(big.Int).And(x, big.NewInt(1)).Sign() == 0
}
