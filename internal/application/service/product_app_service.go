// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package service implements COLA-style Application Services.
//
// Application Service is the use-case orchestration layer. It should answer
// questions like "what must happen when an Agent executes a paid product?" while
// delegating domain rules to Domain objects and technical details to ports.
package service

import (
	"context"
	"fmt"
	"strings"

	appPort "github.com/stablepay/merchant-server/internal/application/port"
	"github.com/stablepay/merchant-server/internal/domain/entity"
	"github.com/stablepay/merchant-server/internal/domain/repository"
	domainSvc "github.com/stablepay/merchant-server/internal/domain/service"
)

// ProductAppService is the product use-case entry point.
//
// It orchestrates repository reads, domain validation, payment verification,
// x402 payment requirement construction, and merchant proof generation.
type ProductAppService struct {
	productRepo     repository.ProductRepository
	domainService   *domainSvc.ProductDomainService
	paymentVerifier appPort.PaymentVerifier
	giftCodeSvc     *GiftCodeService

	merchantPublicBaseURL string
	sellerAddress         string
	proofSecret           string
	facilitatorURL        string
	usdcMint              string
	solanaNetwork         string
}

// NewProductAppService creates the product application service.
func NewProductAppService(
	productRepo repository.ProductRepository,
	domainService *domainSvc.ProductDomainService,
	paymentVerifier appPort.PaymentVerifier,
	giftCodeSvc *GiftCodeService,
	merchantPublicBaseURL, sellerAddress, proofSecret, facilitatorURL, usdcMint, solanaNetwork string,
) *ProductAppService {
	return &ProductAppService{
		productRepo:           productRepo,
		domainService:         domainService,
		paymentVerifier:       paymentVerifier,
		giftCodeSvc:           giftCodeSvc,
		merchantPublicBaseURL: strings.TrimRight(strings.TrimSpace(merchantPublicBaseURL), "/"),
		sellerAddress:         strings.TrimSpace(sellerAddress),
		proofSecret:           strings.TrimSpace(proofSecret),
		facilitatorURL:        strings.TrimSpace(facilitatorURL),
		usdcMint:              strings.TrimSpace(usdcMint),
		solanaNetwork:         strings.TrimSpace(solanaNetwork),
	}
}

// ProductListItem is the Application-layer read model returned by product list
// use cases. Adapter DTOs can map from this model without exposing Domain Entity.
type ProductListItem struct {
	ID          string   `json:"id"`
	SKUID       string   `json:"sku_id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	ImageURL    string   `json:"image_url,omitempty"`
	Price       string   `json:"price"`
	Currency    string   `json:"currency"`
	Author      string   `json:"author,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Status      string   `json:"status"`
}

// ListProducts queries active products and returns a stable read model.
func (s *ProductAppService) ListProducts(ctx context.Context, page, size int) ([]*ProductListItem, int64, error) {
	page, size = normalizePagination(page, size)

	products, total, err := s.productRepo.FindAll(ctx, page, size)
	if err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}

	items := make([]*ProductListItem, 0, len(products))
	for _, p := range products {
		items = append(items, productToListItem(p))
	}

	return items, total, nil
}

// GetProductDetail returns a single product read model by public SKU ID.
func (s *ProductAppService) GetProductDetail(ctx context.Context, skuID string) (*ProductListItem, error) {
	skuID = strings.TrimSpace(skuID)
	if skuID == "" {
		return nil, fmt.Errorf("get product detail: sku_id is required")
	}

	product, err := s.productRepo.FindBySKUID(ctx, skuID)
	if err != nil {
		return nil, fmt.Errorf("get product detail: %w", err)
	}

	return productToListItem(product), nil
}

// ExecutePurchaseCommand is the input model for the execute-purchase use case.
type ExecutePurchaseCommand struct {
	SKUID            string
	AgentDID         string
	PaymentSignature string
}

// ExecutePurchaseResult is the use-case output.
//
// Adapter decides how to translate this result into HTTP 200 or HTTP 402. The
// Application layer does not know about Hertz's RequestContext.
type ExecutePurchaseResult struct {
	Purchased       bool
	Product         *ProductListItem
	PaymentRequired *domainSvc.X402PaymentRequired
	MerchantProof   *domainSvc.PurchaseProof
	GatewayProof    map[string]any
	TxID            string
	TxHash          string
	Content         map[string]any
}

// ExecutePurchase orchestrates the paid-product access flow.
//
// Use case decision chain:
//  1. Find the product by SKU.
//  2. Ask Domain whether it can be purchased.
//  3. Ask the payment verifier whether this Agent already paid.
//  4. If not paid, build x402 v2 PaymentRequired.
//  5. If paid, build a merchant access proof and return unlock content.
func (s *ProductAppService) ExecutePurchase(ctx context.Context, cmd ExecutePurchaseCommand) (*ExecutePurchaseResult, error) {
	cmd.SKUID = strings.TrimSpace(cmd.SKUID)
	cmd.AgentDID = strings.TrimSpace(cmd.AgentDID)
	cmd.PaymentSignature = strings.TrimSpace(cmd.PaymentSignature)

	if cmd.SKUID == "" {
		return nil, fmt.Errorf("execute purchase: sku_id is required")
	}
	if cmd.AgentDID == "" {
		return nil, fmt.Errorf("execute purchase: agent_did is required")
	}

	product, err := s.productRepo.FindBySKUID(ctx, cmd.SKUID)
	if err != nil {
		return nil, fmt.Errorf("execute purchase: %w", err)
	}
	if err := s.domainService.CanPurchase(product); err != nil {
		return nil, fmt.Errorf("execute purchase: %w", err)
	}

	verification, err := s.verifyPurchase(ctx, product, cmd)
	if err != nil {
		return nil, err
	}
	if verification == nil || !verification.Purchased {
		paymentRequired, err := s.buildPaymentRequired(product, missingPaymentError(cmd.PaymentSignature))
		if err != nil {
			return nil, err
		}
		return &ExecutePurchaseResult{
			Purchased:       false,
			Product:         productToListItem(product),
			PaymentRequired: paymentRequired,
		}, nil
	}

	proof, err := s.domainService.BuildSignedProof(cmd.AgentDID, product.SKUID, s.proofSecret)
	if err != nil {
		return nil, fmt.Errorf("execute purchase: build merchant proof: %w", err)
	}

	// Allocate a gift code from the pool (if any remaining).
	giftCode := ""
	if s.giftCodeSvc != nil {
		giftCode = s.giftCodeSvc.Allocate()
	}

	return &ExecutePurchaseResult{
		Purchased:     true,
		Product:       productToListItem(product),
		MerchantProof: proof,
		GatewayProof:  verification.Proof,
		TxID:          verification.TxID,
		TxHash:        verification.TxHash,
		Content:       buildUnlockedContent(product, proof, verification, giftCode),
	}, nil
}

func (s *ProductAppService) verifyPurchase(ctx context.Context, product *entity.Product, cmd ExecutePurchaseCommand) (*appPort.VerifyPurchaseResult, error) {
	if s.paymentVerifier == nil {
		return &appPort.VerifyPurchaseResult{Purchased: false}, nil
	}
	result, err := s.paymentVerifier.VerifyPurchase(ctx, appPort.VerifyPurchaseRequest{
		AgentDID:         cmd.AgentDID,
		SkillDID:         product.SkillDid,
		PaymentSignature: cmd.PaymentSignature,
	})
	if err != nil {
		return nil, fmt.Errorf("execute purchase: verify payment: %w", err)
	}
	return result, nil
}

func (s *ProductAppService) buildPaymentRequired(product *entity.Product, reason string) (*domainSvc.X402PaymentRequired, error) {
	resourceURL := s.resourceURL(product.SKUID)
	required, err := s.domainService.BuildPaymentRequiredV2(domainSvc.BuildPaymentRequiredInput{
		Product:        product,
		ResourceURL:    resourceURL,
		PayTo:          s.sellerAddress,
		Asset:          s.usdcMint,
		Network:        s.solanaNetwork,
		FacilitatorURL: s.facilitatorURL,
		ServiceName:    "StablePay Merchant",
		MimeType:       "application/json",
		Error:          reason,
		Extensions: map[string]any{
			"stablepay": map[string]any{
				"agentPay": true,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("execute purchase: build payment required: %w", err)
	}
	return required, nil
}

func (s *ProductAppService) resourceURL(skuID string) string {
	path := fmt.Sprintf("/api/v1/products/%s/execute", skuID)
	if s.merchantPublicBaseURL == "" {
		return path
	}
	return s.merchantPublicBaseURL + path
}

func productToListItem(p *entity.Product) *ProductListItem {
	if p == nil {
		return nil
	}
	return &ProductListItem{
		ID:          p.SKUID,
		SKUID:       p.SKUID,
		Title:       p.Title,
		Description: p.Description,
		ImageURL:    p.ImageURL,
		Price:       p.Price,
		Currency:    p.Currency,
		Author:      p.Author,
		Tags:        append([]string(nil), p.Tags...),
		Status:      string(p.Status),
	}
}

func normalizePagination(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return page, size
}

func missingPaymentError(paymentSignature string) string {
	if strings.TrimSpace(paymentSignature) == "" {
		return domainSvc.X402PaymentSignatureHeader + " header is required"
	}
	return "payment is required to access this resource"
}

func buildUnlockedContent(product *entity.Product, proof *domainSvc.PurchaseProof, verification *appPort.VerifyPurchaseResult, giftCode string) map[string]any {
	content := map[string]any{
		"product_id": product.SKUID,
		"title":      product.Title,
		"message":    fmt.Sprintf("Unlocked paid content for %s", product.Title),
		"proof":      proof,
	}
	if product.Description != "" {
		content["description"] = product.Description
	}
	if verification != nil {
		content["tx_id"] = verification.TxID
		content["tx_hash"] = verification.TxHash
	}
	if giftCode != "" {
		content["gift_code"] = giftCode
	}
	return content
}
