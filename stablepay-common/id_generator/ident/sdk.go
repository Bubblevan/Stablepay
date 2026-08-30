package ident

import (
	"context"
	"fmt"

	"code.wenfu.cn/stablepay/stablepay-common/id_generator/core"
)

// Config holds configuration for Random ID (UID/PID) generation.
type Config struct {
	Prefix string
	Prime  int64
	Offset int64
	Mask   int64
}

// Generator generates 16-digit Random IDs (User/Merchant IDs).
type Generator struct {
	config     Config
	allocator  *core.SegmentAllocator
	obfuscator *core.Obfuscator
}

// NewGenerator creates a new Random ID generator.
func NewGenerator(config Config, allocator *core.SegmentAllocator) (*Generator, error) {
	// Validate Prefix length if needed
	if len(config.Prefix) != 4 {
		return nil, fmt.Errorf("prefix must be 4 digits")
	}

	return &Generator{
		config:     config,
		allocator:  allocator,
		obfuscator: core.NewObfuscator(config.Prime, config.Offset, config.Mask),
	}, nil
}

// Generate generates a 16-digit ID.
func (g *Generator) Generate(ctx context.Context) (string, error) {
	seq, err := g.allocator.NextId(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get sequence: %w", err)
	}

	// 11 digits limit: 100,000,000,000
	limit := int64(100000000000)
	obfuscated := g.obfuscator.ObfuscateWithLimit(seq, limit)

	bodyStr := fmt.Sprintf("%011d", obfuscated)

	payload := g.config.Prefix + bodyStr
	checkDigit, err := core.LuhnCalculate(payload)
	if err != nil {
		return "", fmt.Errorf("luhn calculation failed: %w", err)
	}

	return fmt.Sprintf("%s%d", payload, checkDigit), nil
}
