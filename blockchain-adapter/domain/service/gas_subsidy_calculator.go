// Package service 定义领域服务
// 领域服务：封装不属于任何实体的业务逻辑
package service

import (
	"github.com/stablepay/blockchain-adapter/domain/entity"
)

// GasSubsidyCalculator Gas 补贴计算器
// 职责：封装 Gas 补贴计算的业务规则
type GasSubsidyCalculator struct {
	subsidyRatio float64 // 补贴比例（1.0 = 100%）
}

// NewGasSubsidyCalculator 创建计算器
// 默认全额补贴（100%）
func NewGasSubsidyCalculator() *GasSubsidyCalculator {
	return &GasSubsidyCalculator{
		subsidyRatio: 1.0,
	}
}

// NewGasSubsidyCalculatorWithRatio 指定补贴比例创建计算器
func NewGasSubsidyCalculatorWithRatio(ratio float64) *GasSubsidyCalculator {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	return &GasSubsidyCalculator{
		subsidyRatio: ratio,
	}
}

// Calculate 计算补贴
// 输入：实际 Gas 费用
// 输出：修改实体中的补贴金额和 Agent 承担金额
func (c *GasSubsidyCalculator) Calculate(entity *entity.GasSubsidyEntity, actualGas uint64) {
	entity.CalculateSubsidy(actualGas, c.subsidyRatio)
}

// GetSubsidyRatio 获取当前补贴比例
func (c *GasSubsidyCalculator) GetSubsidyRatio() float64 {
	return c.subsidyRatio
}

// SetSubsidyRatio 设置补贴比例
func (c *GasSubsidyCalculator) SetSubsidyRatio(ratio float64) {
	if ratio >= 0 && ratio <= 1 {
		c.subsidyRatio = ratio
	}
}

// SubsidyRatioVO 补贴比例值对象
type SubsidyRatioVO struct {
	AgentPart     float64 // Agent 承担比例
	StablePayPart float64 // 平台承担比例
}

// DefaultSubsidyRatio 默认全额补贴
var DefaultSubsidyRatio = SubsidyRatioVO{
	AgentPart:     0.0,
	StablePayPart: 1.0,
}

// FiftyFiftySubsidyRatio 五五分摊
var FiftyFiftySubsidyRatio = SubsidyRatioVO{
	AgentPart:     0.5,
	StablePayPart: 0.5,
}

// ToFloat64 转换为单一比例值（StablePay 部分）
func (r SubsidyRatioVO) ToFloat64() float64 {
	return r.StablePayPart
}
