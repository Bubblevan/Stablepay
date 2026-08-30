package chainmoney

import "fmt"

// ChainType 链类型（与 stablepay-idl/idl/chaincore/chaincore.thrift 对齐）
// TRON=1, ETHEREUM=2, SOLANA=3, BSC=4
type ChainType int32

const (
	ChainTypeTron      ChainType = 1
	ChainTypeEthereum  ChainType = 2
	ChainTypeSolana    ChainType = 3
	ChainTypeBSC       ChainType = 4
	ChainTypePolygon   ChainType = 5
	ChainTypeArbitrum  ChainType = 6
	ChainTypeAvalanche ChainType = 7
	ChainTypeBase      ChainType = 8
)

func (ct ChainType) String() string {
	switch ct {
	case ChainTypeTron:
		return "TRON"
	case ChainTypeEthereum:
		return "ETHEREUM"
	case ChainTypeSolana:
		return "SOLANA"
	case ChainTypeBSC:
		return "BSC"
	case ChainTypePolygon:
		return "POLYGON"
	case ChainTypeArbitrum:
		return "ARBITRUM"
	case ChainTypeAvalanche:
		return "AVALANCHE"
	case ChainTypeBase:
		return "BASE"
	default:
		return "UNKNOWN"
	}
}

// ParseChainType parses a string to ChainType. Returns 0 and false if unknown.
func ParseChainType(s string) (ChainType, bool) {
	m := map[string]ChainType{
		"TRON": ChainTypeTron, "tron": ChainTypeTron,
		"ETHEREUM": ChainTypeEthereum, "ethereum": ChainTypeEthereum, "ETH": ChainTypeEthereum, "eth": ChainTypeEthereum,
		"SOLANA": ChainTypeSolana, "solana": ChainTypeSolana, "SOL": ChainTypeSolana,
		"BSC": ChainTypeBSC, "bsc": ChainTypeBSC,
		"POLYGON": ChainTypePolygon, "polygon": ChainTypePolygon,
		"ARBITRUM": ChainTypeArbitrum, "arbitrum": ChainTypeArbitrum,
		"AVALANCHE": ChainTypeAvalanche, "avalanche": ChainTypeAvalanche,
		"BASE": ChainTypeBase, "base": ChainTypeBase,
	}
	ct, ok := m[s]
	return ct, ok
}

// TokenRef 代币引用（防止“只靠 symbol”产生歧义）
// - Contract 仅在同链同 symbol 可能存在多合约的场景才需要传；否则留空，由 registry 决定。
type TokenRef struct {
	ChainType ChainType
	Symbol    string
	Contract  string
}

func (tr TokenRef) Key() string {
	if tr.Contract == "" {
		return fmt.Sprintf("%d:%s", tr.ChainType, tr.Symbol)
	}
	return fmt.Sprintf("%d:%s:%s", tr.ChainType, tr.Symbol, tr.Contract)
}

// TokenMeta token 元数据（链域）
type TokenMeta struct {
	ChainType ChainType
	Symbol    string
	Decimals  int32
	Contract  string
	IsNative  bool
}
