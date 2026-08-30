// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package port defines Application-layer ports.
//
// A port is an ability that a use case needs from the outside world. The
// Application layer owns the interface, while Infrastructure provides the
// implementation. This keeps use cases independent from HTTP clients, SQL
// drivers, and Gateway SDKs.
package port

import "context"

// VerifyPurchaseRequest describes the data needed to verify whether an Agent
// has paid for a StablePay product/skill.
type VerifyPurchaseRequest struct {
	AgentDID         string
	SkillDID         string
	PaymentSignature string
}

// VerifyPurchaseResult is the normalized verification result returned to the
// Application layer. It intentionally hides the concrete Gateway response shape.
type VerifyPurchaseResult struct {
	Purchased bool
	TxID      string
	TxHash    string
	Proof     map[string]any
}

// PaymentVerifier verifies payment state through a facilitator/Gateway.
//
// Infrastructure clients implement this interface. Application services depend
// on this interface, not on a concrete StablePay HTTP client.
type PaymentVerifier interface {
	VerifyPurchase(ctx context.Context, req VerifyPurchaseRequest) (*VerifyPurchaseResult, error)
}
