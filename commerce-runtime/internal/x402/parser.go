// Package x402 parses the bounded, machine-readable payment challenge emitted
// by the existing merchant HTTP contract. It is deliberately not an LLM or
// floating-point parser.
package x402

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
)

const MaxChallengeBytes = 256 << 10

var (
	ErrInvalidChallenge    = errors.New("invalid x402 payment challenge")
	ErrUnsupportedScheme   = errors.New("unsupported x402 payment scheme")
	ErrUnsupportedProtocol = errors.New("unsupported x402 protocol version")
	ErrChallengeTooLarge   = errors.New("x402 payment challenge exceeds the bounded size")
)

type ParsedRequirement struct {
	ProtocolVersion   string
	Scheme            string
	Network           string
	Asset             string
	AmountMinor       int64
	Currency          string
	PayTo             string
	ResourceURL       string
	ProductID         string
	SkillDID          string
	MaxTimeoutSeconds int
	RawPayload        []byte
	RawPayloadHash    string
	CanonicalPayload  []byte
	CanonicalHash     string
}

type topLevel struct {
	X402Version int               `json:"x402Version"`
	Resource    json.RawMessage   `json:"resource"`
	Accepts     []json.RawMessage `json:"accepts"`
}

type v2Resource struct {
	URL string `json:"url"`
}
type v2Accept struct {
	Scheme            string                     `json:"scheme"`
	Network           string                     `json:"network"`
	Amount            json.RawMessage            `json:"amount"`
	Asset             string                     `json:"asset"`
	PayTo             string                     `json:"payTo"`
	MaxTimeoutSeconds int                        `json:"maxTimeoutSeconds"`
	Extra             map[string]json.RawMessage `json:"extra"`
}
type v1Accept struct {
	Scheme            string                     `json:"scheme"`
	Network           string                     `json:"network"`
	MaxAmountRequired json.RawMessage            `json:"maxAmountRequired"`
	Amount            json.RawMessage            `json:"amount"`
	Asset             string                     `json:"asset"`
	PayTo             string                     `json:"payTo"`
	Resource          string                     `json:"resource"`
	MaxTimeoutSeconds int                        `json:"maxTimeoutSeconds"`
	Extra             map[string]json.RawMessage `json:"extra"`
}

func ParseRequired(headers map[string]string, body []byte) (ParsedRequirement, error) {
	if len(body) > MaxChallengeBytes {
		return ParsedRequirement{}, ErrChallengeTooLarge
	}
	var raw []byte
	if value := headerValue(headers, "PAYMENT-REQUIRED"); value != "" {
		decoded, err := decodeHeader(value)
		if err != nil {
			return ParsedRequirement{}, fmt.Errorf("%w: payment-required header: %v", ErrInvalidChallenge, err)
		}
		raw = decoded
	} else if value := headerValue(headers, "Payment-Required"); value != "" {
		decoded, err := decodeHeader(value)
		if err != nil {
			return ParsedRequirement{}, fmt.Errorf("%w: legacy payment-required header: %v", ErrInvalidChallenge, err)
		}
		raw = decoded
	} else {
		raw = append([]byte(nil), body...)
	}
	if len(raw) == 0 || len(raw) > MaxChallengeBytes {
		return ParsedRequirement{}, ErrInvalidChallenge
	}
	var top topLevel
	if err := json.Unmarshal(raw, &top); err != nil {
		return ParsedRequirement{}, fmt.Errorf("%w: malformed JSON: %v", ErrInvalidChallenge, err)
	}
	if top.X402Version == 2 {
		return parseV2(raw, top)
	}
	if top.X402Version == 1 {
		return parseV1(raw, top)
	}
	return ParsedRequirement{}, ErrUnsupportedProtocol
}

func decodeHeader(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(value); err == nil {
			return decoded, nil
		}
	}
	// The existing merchant keeps the legacy v1 header as JSON for backwards
	// compatibility, so accept raw JSON only after all base64 forms failed.
	if json.Valid([]byte(value)) {
		return []byte(value), nil
	}
	return nil, errors.New("header is not valid base64 or JSON")
}

func parseV2(raw []byte, top topLevel) (ParsedRequirement, error) {
	if len(top.Accepts) == 0 {
		return ParsedRequirement{}, fmt.Errorf("%w: accepts is empty", ErrInvalidChallenge)
	}
	var resource v2Resource
	if len(top.Resource) > 0 && string(top.Resource) != "null" {
		if err := json.Unmarshal(top.Resource, &resource); err != nil {
			return ParsedRequirement{}, ErrInvalidChallenge
		}
	}
	var accept v2Accept
	if err := json.Unmarshal(top.Accepts[0], &accept); err != nil {
		return ParsedRequirement{}, ErrInvalidChallenge
	}
	if strings.TrimSpace(resource.URL) == "" || strings.TrimSpace(string(accept.Amount)) == "" || strings.TrimSpace(accept.PayTo) == "" || strings.TrimSpace(accept.Asset) == "" || strings.TrimSpace(accept.Network) == "" {
		return ParsedRequirement{}, fmt.Errorf("%w: required v2 fields are missing", ErrInvalidChallenge)
	}
	amount, err := parseMinorAmount(accept.Amount, canonicalCurrency(extraString(accept.Extra, "currency"), accept.Asset))
	if err != nil {
		return ParsedRequirement{}, err
	}
	if accept.Scheme != "exact" {
		return ParsedRequirement{}, ErrUnsupportedScheme
	}
	if accept.MaxTimeoutSeconds <= 0 || accept.MaxTimeoutSeconds > 86400 {
		return ParsedRequirement{}, fmt.Errorf("%w: timeout", ErrInvalidChallenge)
	}
	result := ParsedRequirement{ProtocolVersion: "x402-v2", Scheme: "exact", Network: strings.TrimSpace(accept.Network), Asset: strings.TrimSpace(accept.Asset), AmountMinor: amount,
		Currency: canonicalCurrency(extraString(accept.Extra, "currency"), accept.Asset), PayTo: strings.TrimSpace(accept.PayTo), ResourceURL: canonicalURL(resource.URL),
		ProductID: extraString(accept.Extra, "productId"), SkillDID: extraString(accept.Extra, "skillDid"), MaxTimeoutSeconds: accept.MaxTimeoutSeconds,
		RawPayload: append([]byte(nil), raw...)}
	return finalize(result)
}

func parseV1(raw []byte, top topLevel) (ParsedRequirement, error) {
	if len(top.Accepts) == 0 {
		return ParsedRequirement{}, fmt.Errorf("%w: accepts is empty", ErrInvalidChallenge)
	}
	var accept v1Accept
	if err := json.Unmarshal(top.Accepts[0], &accept); err != nil {
		return ParsedRequirement{}, ErrInvalidChallenge
	}
	amountRaw := accept.MaxAmountRequired
	if len(amountRaw) == 0 {
		amountRaw = accept.Amount
	}
	if strings.TrimSpace(accept.PayTo) == "" || strings.TrimSpace(accept.Asset) == "" || strings.TrimSpace(accept.Network) == "" || len(amountRaw) == 0 {
		return ParsedRequirement{}, fmt.Errorf("%w: required v1 fields are missing", ErrInvalidChallenge)
	}
	currency := canonicalCurrency(extraString(accept.Extra, "currency"), accept.Asset)
	amount, err := parseMinorAmount(amountRaw, currency)
	if err != nil {
		return ParsedRequirement{}, err
	}
	if accept.Scheme != "exact" {
		return ParsedRequirement{}, ErrUnsupportedScheme
	}
	if accept.MaxTimeoutSeconds <= 0 || accept.MaxTimeoutSeconds > 86400 {
		return ParsedRequirement{}, fmt.Errorf("%w: timeout", ErrInvalidChallenge)
	}
	resource := strings.TrimSpace(accept.Resource)
	if resource == "" {
		resource = extraString(accept.Extra, "resource")
	}
	if resource == "" {
		return ParsedRequirement{}, fmt.Errorf("%w: resource is required", ErrInvalidChallenge)
	}
	result := ParsedRequirement{ProtocolVersion: "x402-v1", Scheme: "exact", Network: strings.TrimSpace(accept.Network), Asset: strings.TrimSpace(accept.Asset), AmountMinor: amount,
		Currency: canonicalCurrency(currency, accept.Asset), PayTo: strings.TrimSpace(accept.PayTo), ResourceURL: canonicalURL(resource), ProductID: extraString(accept.Extra, "productId"),
		SkillDID: extraString(accept.Extra, "skillDid"), MaxTimeoutSeconds: accept.MaxTimeoutSeconds, RawPayload: append([]byte(nil), raw...)}
	return finalize(result)
}

func finalize(result ParsedRequirement) (ParsedRequirement, error) {
	if result.ResourceURL == "" || !validURL(result.ResourceURL) || result.AmountMinor <= 0 || result.Currency == "" {
		return ParsedRequirement{}, ErrInvalidChallenge
	}
	canonical := struct {
		ProtocolVersion   string `json:"protocol_version"`
		Scheme            string `json:"scheme"`
		Network           string `json:"network"`
		Asset             string `json:"asset"`
		AmountMinor       int64  `json:"amount_minor"`
		Currency          string `json:"currency"`
		PayTo             string `json:"pay_to"`
		ResourceURL       string `json:"resource_url"`
		ProductID         string `json:"product_id,omitempty"`
		SkillDID          string `json:"skill_did,omitempty"`
		MaxTimeoutSeconds int    `json:"max_timeout_seconds"`
	}{result.ProtocolVersion, result.Scheme, result.Network, result.Asset, result.AmountMinor, result.Currency, result.PayTo, result.ResourceURL, result.ProductID, result.SkillDID, result.MaxTimeoutSeconds}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return ParsedRequirement{}, err
	}
	result.CanonicalPayload = payload
	rawDigest := sha256.Sum256(result.RawPayload)
	canonicalDigest := sha256.Sum256(payload)
	result.RawPayloadHash = "sha256:" + hex.EncodeToString(rawDigest[:])
	result.CanonicalHash = "sha256:" + hex.EncodeToString(canonicalDigest[:])
	return result, nil
}

func parseMinorAmount(raw json.RawMessage, currency string) (int64, error) {
	var value string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return 0, ErrInvalidChallenge
		}
	} else {
		value = strings.TrimSpace(string(raw))
	}
	if value == "" || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return 0, ErrInvalidChallenge
	}
	if strings.ContainsAny(value, "eE") {
		return 0, ErrInvalidChallenge
	}
	if strings.Contains(value, ".") {
		decimals := currencyDecimals(currency)
		if decimals < 0 {
			return 0, fmt.Errorf("%w: decimal amount requires a known currency", ErrInvalidChallenge)
		}
		parts := strings.Split(value, ".")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || len(parts[1]) > decimals {
			return 0, ErrInvalidChallenge
		}
		fraction := parts[1] + strings.Repeat("0", decimals-len(parts[1]))
		value = parts[0] + fraction
	}
	if strings.ContainsAny(value, "+-") {
		return 0, ErrInvalidChallenge
	}
	number := new(big.Int)
	if _, ok := number.SetString(value, 10); !ok || number.Sign() <= 0 || !number.IsInt64() {
		return 0, ErrInvalidChallenge
	}
	return number.Int64(), nil
}

func currencyDecimals(currency string) int {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "USDC", "USDT":
		return 6
	case "SOL":
		return 9
	case "USD":
		return 2
	default:
		return -1
	}
}
func canonicalCurrency(value, asset string) string {
	if strings.TrimSpace(value) != "" {
		return strings.ToUpper(strings.TrimSpace(value))
	}
	return strings.ToUpper(strings.TrimSpace(asset))
}
func extraString(values map[string]json.RawMessage, key string) string {
	raw := values[key]
	var value string
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &value)
	}
	return strings.TrimSpace(value)
}
func validURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https")
}
func canonicalURL(value string) string {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	return parsed.String()
}
func headerValue(headers map[string]string, name string) string {
	for key, value := range headers {
		if strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// EqualResourceURL applies only harmless URL canonicalization; it does not
// allow a quote for another host, path, or query.
func EqualResourceURL(left, right string) bool { return canonicalURL(left) == canonicalURL(right) }

// DecodeBodyRequirement is useful for contract fixtures that put the v2
// object in the response body and do not set a protocol header.
func DecodeBodyRequirement(body []byte) (ParsedRequirement, error) {
	return ParseRequired(nil, bytes.TrimSpace(body))
}
