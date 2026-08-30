// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package domain_service

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/stablepay/merchant-server/internal/domain/entity"
)

const (
	X402VersionV2 = 2

	X402SchemeExact = "exact"

	// X402PaymentRequiredHeader is the canonical HTTP response header in x402 v2.
	X402PaymentRequiredHeader = "PAYMENT-REQUIRED"

	// X402PaymentSignatureHeader is the canonical HTTP request header in x402 v2.
	X402PaymentSignatureHeader = "PAYMENT-SIGNATURE"

	// X402PaymentResponseHeader is the canonical HTTP response header after settlement.
	X402PaymentResponseHeader = "PAYMENT-RESPONSE"

	// X402PaymentRequiredHeaderV1 is the x402 v1 header name, kept for backward compatibility.
	X402PaymentRequiredHeaderV1 = "Payment-Required"
)

// X402ResourceInfo describes the protected resource in x402 v2.
type X402ResourceInfo struct {
	URL         string   `json:"url"`
	Description string   `json:"description,omitempty"`
	MimeType    string   `json:"mimeType,omitempty"`
	ServiceName string   `json:"serviceName,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	IconURL     string   `json:"iconUrl,omitempty"`
}

// X402PaymentRequirements describes one acceptable payment method in x402 v2.
type X402PaymentRequirements struct {
	Scheme            string         `json:"scheme"`
	Network           string         `json:"network"`
	Amount            string         `json:"amount"`
	Asset             string         `json:"asset"`
	PayTo             string         `json:"payTo"`
	MaxTimeoutSeconds int            `json:"maxTimeoutSeconds"`
	Extra             map[string]any `json:"extra,omitempty"`
}

// X402PaymentRequired is the x402 v2 PaymentRequired object.
type X402PaymentRequired struct {
	X402Version int                       `json:"x402Version"`
	Error       string                    `json:"error,omitempty"`
	Resource    X402ResourceInfo          `json:"resource"`
	Accepts     []X402PaymentRequirements `json:"accepts"`
	Extensions  map[string]any            `json:"extensions,omitempty"`
}

// BuildPaymentRequiredInput contains merchant-specific data needed to build an x402 v2 paywall.
type BuildPaymentRequiredInput struct {
	Product           *entity.Product
	ResourceURL       string
	PayTo             string
	Asset             string
	Network           string
	FacilitatorURL    string
	ServiceName       string
	MimeType          string
	IconURL           string
	Error             string
	MaxTimeoutSeconds int
	Extensions        map[string]any
}

// BuildPaymentRequiredV2 constructs an x402 v2 PaymentRequired domain object.
func (s *ProductDomainService) BuildPaymentRequiredV2(input BuildPaymentRequiredInput) (*X402PaymentRequired, error) {
	if err := s.CanPurchase(input.Product); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.ResourceURL) == "" {
		return nil, fmt.Errorf("x402 payment required: resource url is required")
	}
	if strings.TrimSpace(input.PayTo) == "" {
		return nil, fmt.Errorf("x402 payment required: payTo is required")
	}
	if strings.TrimSpace(input.Asset) == "" {
		return nil, fmt.Errorf("x402 payment required: asset is required")
	}
	if strings.TrimSpace(input.Network) == "" {
		return nil, fmt.Errorf("x402 payment required: network is required")
	}

	money, err := input.Product.PriceMoney()
	if err != nil {
		return nil, err
	}

	timeout := input.MaxTimeoutSeconds
	if timeout <= 0 {
		timeout = 300
	}

	extra := map[string]any{
		"name":      money.Currency,
		"version":   "2",
		"productId": input.Product.SKUID,
		"skillDid":  input.Product.SkillDid,
		"currency":  money.Currency,
	}
	if input.FacilitatorURL != "" {
		extra["facilitatorUrl"] = input.FacilitatorURL
	}

	extensions := input.Extensions
	if extensions == nil {
		extensions = map[string]any{}
	}

	return &X402PaymentRequired{
		X402Version: X402VersionV2,
		Error:       strings.TrimSpace(input.Error),
		Resource: X402ResourceInfo{
			URL:         strings.TrimSpace(input.ResourceURL),
			Description: input.Product.Description,
			MimeType:    firstNonEmpty(input.MimeType, "application/json"),
			ServiceName: truncateASCII(firstNonEmpty(input.ServiceName, "StablePay Merchant"), 32),
			Tags:        limitTags(input.Product.Tags, 5, 32),
			IconURL:     strings.TrimSpace(input.IconURL),
		},
		Accepts: []X402PaymentRequirements{
			{
				Scheme:            X402SchemeExact,
				Network:           strings.TrimSpace(input.Network),
				Amount:            money.AmountMinor,
				Asset:             strings.TrimSpace(input.Asset),
				PayTo:             strings.TrimSpace(input.PayTo),
				MaxTimeoutSeconds: timeout,
				Extra:             extra,
			},
		},
		Extensions: extensions,
	}, nil
}

// EncodePaymentRequiredHeader returns the base64-encoded JSON header value for HTTP transport.
func EncodePaymentRequiredHeader(required *X402PaymentRequired) (string, error) {
	if required == nil {
		return "", fmt.Errorf("x402 payment required is nil")
	}
	payload, err := json.Marshal(required)
	if err != nil {
		return "", fmt.Errorf("marshal x402 payment required: %w", err)
	}
	return base64.StdEncoding.EncodeToString(payload), nil
}

// BuildPaymentRequiredV1 constructs a v1-style payment required struct from the v2 domain object.
// This is used to set the legacy "Payment-Required" header for backward compatibility.
func BuildPaymentRequiredV1(v2 *X402PaymentRequired, product *entity.Product, payTo, asset, network, facilitatorURL string) map[string]any {
	if v2 == nil || len(v2.Accepts) == 0 {
		return nil
	}
	a := v2.Accepts[0]
	return map[string]any{
		"x402Version": 1,
		"error":       "Payment Required",
		"accepts": []map[string]any{
			{
				"scheme":            a.Scheme,
				"network":           a.Network,
				"maxAmountRequired": a.Amount,
				"payTo":             a.PayTo,
				"asset":             a.Asset,
				"description":       fmt.Sprintf("购买 %s", product.Title),
				"resource":          v2.Resource.URL,
				"maxTimeoutSeconds": a.MaxTimeoutSeconds,
				"extra": map[string]any{
					"facilitatorUrl": facilitatorURL,
					"currency":       product.Currency,
					"productId":      product.SKUID,
					"skillDid":       product.SkillDid,
				},
			},
		},
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func limitTags(tags []string, maxCount, maxLen int) []string {
	if maxCount <= 0 || maxLen <= 0 {
		return nil
	}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = truncateASCII(strings.TrimSpace(tag), maxLen)
		if tag == "" {
			continue
		}
		out = append(out, tag)
		if len(out) >= maxCount {
			break
		}
	}
	return out
}

func truncateASCII(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
