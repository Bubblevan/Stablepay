// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package entity implements the COLA-style Domain layer core entities.
//
// Entity code contains business attributes, behavior, and invariants. It should
// not depend on HTTP frameworks, databases, or StablePay Gateway clients.
package entity

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ProductStatus describes the lifecycle state of a product.
type ProductStatus string

const (
	ProductStatusActive   ProductStatus = "active"   // 上架，可购买
	ProductStatusInactive ProductStatus = "inactive" // 下架，不可购买
	ProductStatusDraft    ProductStatus = "draft"    // 草稿，不可购买
)

var (
	ErrInvalidProduct        = errors.New("invalid product")
	ErrProductNotFound       = errors.New("product not found")
	ErrProductNotPurchasable = errors.New("product is not purchasable")
)

const (
	// SkillDidPrefix is the Solana DID prefix used to derive skill_did from a seller address.
	SkillDidPrefix = "did:solana:"
)

// Product is the product domain entity.
//
// It is intentionally not a database PO and not an API DTO. Product captures the
// merchant-side business meaning of an item that an Agent may purchase.
type Product struct {
	// ID is the internal persistence ID. It is not exposed as the public product ID.
	ID int64 `json:"-"`

	// SKUID is the stable public product identifier used in URLs and x402 resources.
	SKUID       string        `json:"sku_id"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	ImageURL    string        `json:"image_url,omitempty"`
	Price       string        `json:"price"`
	Currency    string        `json:"currency"`
	Author      string        `json:"author,omitempty"`
	Tags        []string      `json:"tags,omitempty"`
	Status      ProductStatus `json:"status"`

	// SkillDid binds this merchant product to the StablePay payment identity.
	// In the current Solana MVP it is commonly did:solana:<seller_pubkey>.
	SkillDid  string    `json:"skill_did,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IsPurchasable returns whether the product can enter the payment flow.
func (p *Product) IsPurchasable() bool {
	return p != nil && p.ValidatePurchasable() == nil
}

// ValidatePurchasable checks the core invariant before building x402 payment requirements.
func (p *Product) ValidatePurchasable() error {
	if p == nil {
		return ErrProductNotFound
	}
	if err := p.ValidateBasic(); err != nil {
		return fmt.Errorf("%w: %v", ErrProductNotPurchasable, err)
	}
	if p.Status != ProductStatusActive {
		return fmt.Errorf("%w: status=%s", ErrProductNotPurchasable, p.Status)
	}
	if strings.TrimSpace(p.SkillDid) == "" {
		return fmt.Errorf("%w: skill_did is required", ErrProductNotPurchasable)
	}
	if _, err := p.PriceMoney(); err != nil {
		return fmt.Errorf("%w: %v", ErrProductNotPurchasable, err)
	}
	return nil
}

// ValidateBasic checks fields needed for storing or displaying a product.
func (p *Product) ValidateBasic() error {
	if p == nil {
		return ErrProductNotFound
	}
	if strings.TrimSpace(p.SKUID) == "" {
		return fmt.Errorf("%w: sku_id is required", ErrInvalidProduct)
	}
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("%w: title is required", ErrInvalidProduct)
	}
	if strings.TrimSpace(p.Price) == "" {
		return fmt.Errorf("%w: price is required", ErrInvalidProduct)
	}
	if strings.TrimSpace(p.Currency) == "" {
		return fmt.Errorf("%w: currency is required", ErrInvalidProduct)
	}
	if !IsValidProductStatus(p.Status) {
		return fmt.Errorf("%w: unsupported status=%s", ErrInvalidProduct, p.Status)
	}
	return nil
}

// PriceMoney returns a Money value object for this product price.
func (p *Product) PriceMoney() (Money, error) {
	if p == nil {
		return Money{}, ErrProductNotFound
	}
	return NewMoneyFromDecimal(p.Price, p.Currency)
}

// GetPriceMinorUnits returns the product price in minor units.
func (p *Product) GetPriceMinorUnits() string {
	money, err := p.PriceMoney()
	if err != nil {
		return "0"
	}
	return money.AmountMinor
}

// Activate marks the product as active.
func (p *Product) Activate() {
	p.Status = ProductStatusActive
	p.touch()
}

// Deactivate marks the product as inactive.
func (p *Product) Deactivate() {
	p.Status = ProductStatusInactive
	p.touch()
}

// MarkDraft marks the product as draft.
func (p *Product) MarkDraft() {
	p.Status = ProductStatusDraft
	p.touch()
}

func (p *Product) touch() {
	p.UpdatedAt = time.Now()
}

// IsValidProductStatus validates supported lifecycle states.
func IsValidProductStatus(status ProductStatus) bool {
	switch status {
	case ProductStatusActive, ProductStatusInactive, ProductStatusDraft:
		return true
	default:
		return false
	}
}

// ProductBuilder helps construct Product while keeping defaults in one place.
type ProductBuilder struct {
	product *Product
}

// NewProductBuilder creates a product builder with safe MVP defaults.
func NewProductBuilder() *ProductBuilder {
	now := time.Now()
	return &ProductBuilder{
		product: &Product{
			Status:    ProductStatusDraft,
			Currency:  CurrencyUSDC,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}

func (b *ProductBuilder) WithID(id int64) *ProductBuilder {
	b.product.ID = id
	return b
}

func (b *ProductBuilder) WithSKUID(skuID string) *ProductBuilder {
	b.product.SKUID = strings.TrimSpace(skuID)
	return b
}

func (b *ProductBuilder) WithTitle(title string) *ProductBuilder {
	b.product.Title = strings.TrimSpace(title)
	return b
}

func (b *ProductBuilder) WithDescription(desc string) *ProductBuilder {
	b.product.Description = strings.TrimSpace(desc)
	return b
}

func (b *ProductBuilder) WithImageURL(imageURL string) *ProductBuilder {
	b.product.ImageURL = strings.TrimSpace(imageURL)
	return b
}

func (b *ProductBuilder) WithPrice(price, currency string) *ProductBuilder {
	b.product.Price = strings.TrimSpace(price)
	b.product.Currency = strings.ToUpper(strings.TrimSpace(currency))
	return b
}

func (b *ProductBuilder) WithAuthor(author string) *ProductBuilder {
	b.product.Author = strings.TrimSpace(author)
	return b
}

func (b *ProductBuilder) WithTags(tags []string) *ProductBuilder {
	cleaned := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			cleaned = append(cleaned, tag)
		}
	}
	b.product.Tags = cleaned
	return b
}

func (b *ProductBuilder) WithStatus(status ProductStatus) *ProductBuilder {
	b.product.Status = status
	return b
}

func (b *ProductBuilder) WithSkillDid(skillDid string) *ProductBuilder {
	b.product.SkillDid = strings.TrimSpace(skillDid)
	return b
}

func (b *ProductBuilder) WithSellerAddress(sellerAddress string) *ProductBuilder {
	b.product.SkillDid = SkillDidPrefix + strings.TrimSpace(sellerAddress)
	return b
}

// Build constructs and validates a product entity.
func (b *ProductBuilder) Build() (*Product, error) {
	if err := b.product.ValidateBasic(); err != nil {
		return nil, err
	}
	return b.product, nil
}

// MustBuild constructs a product and panics on invalid input.
// Use it only for seed data and tests.
func (b *ProductBuilder) MustBuild() *Product {
	p, err := b.Build()
	if err != nil {
		panic(err)
	}
	return p
}
