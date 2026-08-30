package chainmoney

import (
	"fmt"
	"strings"
	"sync"
)

var (
	regMu sync.RWMutex
	// tokenRegistryKey 规则：
	// - 默认：chainType + symbol
	// - 若同链同 symbol 存在多合约，必须注册为 chainType + symbol + contract，并要求调用方提供 contract
	tokenRegistry = map[string]TokenMeta{}
)

func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

// normalizeContractByChain:
// - EVM 链（ETH/BSC/Polygon）：地址大小写不敏感，为兼容历史与提升命中率，统一转小写
// - 非 EVM 链（Tron/Solana）：地址是 Base58，大小写敏感，必须保留原始大小写
func normalizeContractByChain(chainType ChainType, contract string) string {
	c := strings.TrimSpace(contract)
	if c == "" {
		return ""
	}
	switch chainType {
	case ChainTypeEthereum, ChainTypeBSC, ChainTypePolygon:
		return strings.ToLower(c)
	case ChainTypeTron, ChainTypeSolana:
		return c
	default:
		// 未知链：保守起见不改大小写，避免破坏 Base58 类地址
		return c
	}
}

func makeKey(chainType ChainType, symbol, contract string) string {
	symbol = normalizeSymbol(symbol)
	contract = normalizeContractByChain(chainType, contract)
	if contract == "" {
		return fmt.Sprintf("%d:%s", chainType, symbol)
	}
	return fmt.Sprintf("%d:%s:%s", chainType, symbol, contract)
}

func hasAnyContractSpecificMeta(chainType ChainType, symbol string) bool {
	symbol = normalizeSymbol(symbol)
	prefix := fmt.Sprintf("%d:%s:", chainType, symbol)
	for k := range tokenRegistry {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// RegisterToken 注册 token 元数据（代码内 registry 用）
func RegisterToken(meta TokenMeta) {
	meta.Symbol = normalizeSymbol(meta.Symbol)
	meta.Contract = normalizeContractByChain(meta.ChainType, meta.Contract)
	key := makeKey(meta.ChainType, meta.Symbol, meta.Contract)

	regMu.Lock()
	defer regMu.Unlock()
	tokenRegistry[key] = meta
}

// GetTokenMeta 获取 token 元数据
// - 若 TokenRef 未提供 contract，优先使用 chainType+symbol 的注册项；
// - 若没有该项，则要求调用方提供 contract（用于同名多合约场景）。
func GetTokenMeta(ref TokenRef) (TokenMeta, error) {
	ref.Symbol = normalizeSymbol(ref.Symbol)
	ref.Contract = normalizeContractByChain(ref.ChainType, ref.Contract)

	regMu.RLock()
	defer regMu.RUnlock()

	// 1) 精确 key（含 contract）
	if ref.Contract != "" {
		if meta, ok := tokenRegistry[makeKey(ref.ChainType, ref.Symbol, ref.Contract)]; ok {
			return meta, nil
		}
		// 如果该 chain+symbol 存在 contract-specific 注册项，则 contract 必须匹配，否则直接报错（用于防假币）。
		if hasAnyContractSpecificMeta(ref.ChainType, ref.Symbol) {
			return TokenMeta{}, fmt.Errorf("token meta not found: %s", ref.Key())
		}
		// 否则回退到默认 key（不含 contract），并把 ref.Contract 回填到 meta 中，方便后续透传。
		if meta, ok := tokenRegistry[makeKey(ref.ChainType, ref.Symbol, "")]; ok {
			meta.Contract = ref.Contract
			return meta, nil
		}
		return TokenMeta{}, fmt.Errorf("token meta not found: %s", ref.Key())
	}

	// 2) 默认 key（不含 contract）
	if meta, ok := tokenRegistry[makeKey(ref.ChainType, ref.Symbol, "")]; ok {
		return meta, nil
	}

	// 3) 查找带合约的注册项（返回第一个匹配的）
	// 用于：只注册了带合约地址的 token，但调用方未提供 contract 的场景
	prefix := fmt.Sprintf("%d:%s:", ref.ChainType, normalizeSymbol(ref.Symbol))
	for k, meta := range tokenRegistry {
		if strings.HasPrefix(k, prefix) {
			return meta, nil
		}
	}

	return TokenMeta{}, fmt.Errorf("token meta not found: %d:%s", ref.ChainType, ref.Symbol)
}

func init() {
	// Native tokens
	RegisterToken(TokenMeta{ChainType: ChainTypeEthereum, Symbol: "ETH", Decimals: 18, IsNative: true})
	RegisterToken(TokenMeta{ChainType: ChainTypeBSC, Symbol: "BNB", Decimals: 18, IsNative: true})
	RegisterToken(TokenMeta{ChainType: ChainTypeTron, Symbol: "TRX", Decimals: 6, IsNative: true})
	RegisterToken(TokenMeta{ChainType: ChainTypeSolana, Symbol: "SOL", Decimals: 9, IsNative: true})

	// Stablecoins - default entries (for backward compatibility with contract-less lookups)
	RegisterToken(TokenMeta{ChainType: ChainTypeEthereum, Symbol: "USDT", Decimals: 6, Contract: "0xdAC17F958D2ee523a2206206994597C13D831ec7"})
	RegisterToken(TokenMeta{ChainType: ChainTypeTron, Symbol: "USDT", Decimals: 6, Contract: "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"})
	RegisterToken(TokenMeta{ChainType: ChainTypeSolana, Symbol: "USDT", Decimals: 6, Contract: "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB"})
	RegisterToken(TokenMeta{ChainType: ChainTypeBSC, Symbol: "USDT", Decimals: 18, Contract: "0x55d398326f99059fF775485246999027B3197955"})

	RegisterToken(TokenMeta{ChainType: ChainTypeEthereum, Symbol: "USDC", Decimals: 6, Contract: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48"})
	RegisterToken(TokenMeta{ChainType: ChainTypeSolana, Symbol: "USDC", Decimals: 6, Contract: "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v"})
	RegisterToken(TokenMeta{ChainType: ChainTypeBSC, Symbol: "USDC", Decimals: 18, Contract: "0x8AC76a51cc950d9822D68b83fE1Ad97B32Cd580d"})
}
