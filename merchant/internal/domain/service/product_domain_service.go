// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package domain_service implements COLA-style Domain Services.
//
// Entity owns behavior that naturally belongs to one domain object. Domain
// Service owns stateless business logic that coordinates entities or builds
// domain-level results such as x402 payment requirements.
package domain_service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/stablepay/merchant-server/internal/domain/entity"
	"github.com/stablepay/merchant-server/internal/domain/repository"
)

// ProductDomainService contains product-related domain logic.
type ProductDomainService struct {
	repo repository.ProductRepository
}

// NewProductDomainService creates a product domain service.
func NewProductDomainService(repo repository.ProductRepository) *ProductDomainService {
	return &ProductDomainService{repo: repo}
}

// CanPurchase checks whether a product can enter the x402 payment flow.
func (s *ProductDomainService) CanPurchase(product *entity.Product) error {
	if product == nil {
		return entity.ErrProductNotFound
	}
	return product.ValidatePurchasable()
}

// UsdcToMinorUnits converts a USDC decimal string to minor units.
func (s *ProductDomainService) UsdcToMinorUnits(amount string) string {
	minor, err := entity.DecimalToMinorUnits(amount, entity.USDCDecimals)
	if err != nil {
		return "0"
	}
	return minor
}

// PurchaseProof is a domain-level proof issued after the merchant verifies access.
type PurchaseProof struct {
	ProofVersion string `json:"proof_version"`
	ProofID      string `json:"proof_id"`
	AgentDID     string `json:"agent_did"`
	ProductID    string `json:"product_id"`
	IssuedAt     string `json:"issued_at"`
	Signature    string `json:"signature"`
}

// BuildSignedProof builds an HMAC-SHA256 proof for purchased content access.
func (s *ProductDomainService) BuildSignedProof(agentDID, productID, proofSecret string) (*PurchaseProof, error) {
	agentDID = strings.TrimSpace(agentDID)
	productID = strings.TrimSpace(productID)
	proofSecret = strings.TrimSpace(proofSecret)
	if agentDID == "" {
		return nil, fmt.Errorf("build proof: agent_did is required")
	}
	if productID == "" {
		return nil, fmt.Errorf("build proof: product_id is required")
	}
	if proofSecret == "" {
		return nil, fmt.Errorf("build proof: proof secret is required")
	}

	issuedAt := time.Now().UTC().Format(time.RFC3339)
	proofID := fmt.Sprintf("proof_%s_%d", productID, time.Now().UnixMilli())
	message := strings.Join([]string{"v1", proofID, agentDID, productID, issuedAt}, "|")

	mac := hmac.New(sha256.New, []byte(proofSecret))
	_, _ = mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))

	return &PurchaseProof{
		ProofVersion: "v1",
		ProofID:      proofID,
		AgentDID:     agentDID,
		ProductID:    productID,
		IssuedAt:     issuedAt,
		Signature:    signature,
	}, nil
}
