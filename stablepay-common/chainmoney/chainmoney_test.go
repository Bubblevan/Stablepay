package chainmoney

import (
	"math/big"
	"testing"

	bizmoney "code.wenfu.cn/stablepay/stablepay-common/money"
)

func TestRegistry_DefaultLookup(t *testing.T) {
	meta, err := GetTokenMeta(TokenRef{ChainType: ChainTypeBSC, Symbol: "usdt"})
	if err != nil {
		t.Fatalf("GetTokenMeta err=%v", err)
	}
	if meta.Decimals != 18 {
		t.Fatalf("decimals=%d want=18", meta.Decimals)
	}
}

func TestRegistry_ContractCaseSensitivity_NonEVM(t *testing.T) {
	// Solana USDT mint：Base58，大小写敏感；不应被归一化成小写
	meta, err := GetTokenMeta(TokenRef{ChainType: ChainTypeSolana, Symbol: "usdt"})
	if err != nil {
		t.Fatalf("GetTokenMeta err=%v", err)
	}
	if meta.Contract != "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB" {
		t.Fatalf("solana usdt contract=%s want=%s", meta.Contract, "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB")
	}

	// Tron USDT：同样应保留大小写（Base58）
	meta2, err := GetTokenMeta(TokenRef{ChainType: ChainTypeTron, Symbol: "USDT"})
	if err != nil {
		t.Fatalf("GetTokenMeta err=%v", err)
	}
	if meta2.Contract != "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t" {
		t.Fatalf("tron usdt contract=%s want=%s", meta2.Contract, "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t")
	}
}

func TestBusinessToChain_NoLossScaleUp(t *testing.T) {
	// 业务 USDT 2位：1.23 => 123
	biz, _ := bizmoney.NewMoneyFromString("123", bizmoney.USDT)

	// BSC USDT 18位：应为 123 * 10^(18-2) = 123 * 10^16
	chain, err := BusinessToChain(biz, TokenRef{ChainType: ChainTypeBSC, Symbol: "USDT"})
	if err != nil {
		t.Fatalf("BusinessToChain err=%v", err)
	}
	if got := chain.GetString(); got != "1230000000000000000" {
		t.Fatalf("chain minor=%s want=1230000000000000000", got)
	}
}

func TestDownscale_ErrorOrExplicitRounding(t *testing.T) {
	// 人为构造：fromScale=3 toScale=2 的 downscale 行为，通过 ChainToBusiness 来触发
	// 这里用一个假的 MoneyChain meta（decimals=3）+ amount=1005（=1.005）
	mc := &MoneyChain{
		amount: mustBigInt("1005"),
		meta:   TokenMeta{ChainType: ChainTypeEthereum, Symbol: "FAKE", Decimals: 3},
	}

	// 默认 Half-Even：1.005 -> 1.00（100.5 cents -> 100）
	biz, err := ChainToBusiness(mc, "USD")
	if err != nil {
		t.Fatalf("ChainToBusiness err=%v", err)
	}
	if got := biz.GetString(); got != "100" {
		t.Fatalf("biz minor=%s want=100", got)
	}

	// Ceil：1.005 -> 1.01（101）
	biz2, err := ChainToBusinessCeil(mc, "USD")
	if err != nil {
		t.Fatalf("ChainToBusinessCeil err=%v", err)
	}
	if got := biz2.GetString(); got != "101" {
		t.Fatalf("biz minor=%s want=101", got)
	}
}

func mustBigInt(s string) *big.Int {
	i := new(big.Int)
	i.SetString(s, 10)
	return i
}
