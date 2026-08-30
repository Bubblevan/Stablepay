// Package vo 定义值对象
package vo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/stablepay/payment-service/pkg/common"
	"github.com/stablepay/payment-service/pkg/constants"
	"github.com/stablepay/payment-service/pkg/errors"
	"github.com/stablepay/payment-service/pkg/utils"
)

// DID W3C did:solana 标识符值对象
type DID struct {
	Value string
}

// NewDID 创建 DID 值对象
func NewDID(value string) (DID, error) {
	// 验证 DID 格式: did:solana:{base58_encoded_pubkey}
	if !strings.HasPrefix(value, "did:solana:") {
		return DID{}, errors.New(errors.INVALID_PARAMETERS, "invalid DID format, must start with 'did:solana:'")
	}

	parts := strings.Split(value, ":")
	if len(parts) != 3 || parts[2] == "" {
		return DID{}, errors.New(errors.INVALID_PARAMETERS, "invalid DID format")
	}

	return DID{Value: value}, nil
}

// String 返回 DID 字符串
func (d DID) String() string {
	return d.Value
}

// GetPublicKey 从 DID 提取公钥
func (d DID) GetPublicKey() string {
	parts := strings.Split(d.Value, ":")
	if len(parts) == 3 {
		return parts[2]
	}
	return ""
}

// Amount 金额值对象
type Amount struct {
	MinorUnit int64
	Currency  constants.Currency
}

// IsValidCurrency 检查币种是否有效
func IsValidCurrency(c constants.Currency) bool {
	switch c {
	case constants.CurrencyUSDC, constants.CurrencyUSDT:
		return true
	}
	return false
}

// GetCurrencyDecimals 获取币种精度
func GetCurrencyDecimals(c constants.Currency) int {
	switch c {
	case constants.CurrencyUSDC, constants.CurrencyUSDT:
		return constants.USDCDecimals
	}
	return constants.USDCDecimals
}

// NewAmount 从最小单位创建金额
func NewAmount(minorUnit int64, currency constants.Currency) (Amount, error) {
	if !IsValidCurrency(currency) {
		return Amount{}, errors.Newf(errors.INVALID_PARAMETERS, "unsupported currency: %d", currency)
	}
	return Amount{
		MinorUnit: minorUnit,
		Currency:  currency,
	}, nil
}

// NewAmountFromString 从字符串创建金额
// amount 格式如 "5.00"
func NewAmountFromString(amountStr string, currency constants.Currency) (Amount, error) {
	if !IsValidCurrency(currency) {
		return Amount{}, errors.Newf(errors.INVALID_PARAMETERS, "unsupported currency: %d", currency)
	}

	minorUnit, err := utils.StringToMinorUnit(amountStr)
	if err != nil {
		return Amount{}, errors.Wrap(errors.INVALID_PARAMETERS, err, "invalid amount format")
	}

	return Amount{
		MinorUnit: minorUnit,
		Currency:  currency,
	}, nil
}

// String 返回人类可读的金额字符串
func (a Amount) String() string {
	return fmt.Sprintf("%s %d", utils.FormatAmount(a.MinorUnit), a.Currency)
}

// StringFull 返回完整精度的金额字符串
func (a Amount) StringFull() string {
	return fmt.Sprintf("%s %d", utils.FormatAmountFull(a.MinorUnit), a.Currency)
}

// Add 金额相加
func (a Amount) Add(other Amount) (Amount, error) {
	if a.Currency != other.Currency {
		return Amount{}, errors.New(errors.INVALID_PARAMETERS, "cannot add amounts with different currencies")
	}
	return Amount{
		MinorUnit: a.MinorUnit + other.MinorUnit,
		Currency:  a.Currency,
	}, nil
}

// Sub 金额相减
func (a Amount) Sub(other Amount) (Amount, error) {
	if a.Currency != other.Currency {
		return Amount{}, errors.New(errors.INVALID_PARAMETERS, "cannot subtract amounts with different currencies")
	}
	return Amount{
		MinorUnit: a.MinorUnit - other.MinorUnit,
		Currency:  a.Currency,
	}, nil
}

// IsZero 是否为零
func (a Amount) IsZero() bool {
	return a.MinorUnit == 0
}

// IsPositive 是否为正
func (a Amount) IsPositive() bool {
	return a.MinorUnit > 0
}

// IsNegative 是否为负
func (a Amount) IsNegative() bool {
	return a.MinorUnit < 0
}

// GreaterThan 是否大于
func (a Amount) GreaterThan(other Amount) (bool, error) {
	if a.Currency != other.Currency {
		return false, errors.New(errors.INVALID_PARAMETERS, "cannot compare amounts with different currencies")
	}
	return a.MinorUnit > other.MinorUnit, nil
}

// LessThan 是否小于
func (a Amount) LessThan(other Amount) (bool, error) {
	if a.Currency != other.Currency {
		return false, errors.New(errors.INVALID_PARAMETERS, "cannot compare amounts with different currencies")
	}
	return a.MinorUnit < other.MinorUnit, nil
}

// Signature 签名值对象
type Signature struct {
	Value     string
	Timestamp int64
	Nonce     string
	SignData  string
}

// NewSignature 创建签名值对象
func NewSignature(value string, timestamp int64, nonce string, signData string) Signature {
	return Signature{
		Value:     value,
		Timestamp: timestamp,
		Nonce:     nonce,
		SignData:  signData,
	}
}

// IsExpired 检查签名是否已过期
func (s Signature) IsExpired(ttlMinutes int) bool {
	sigTime := time.Unix(s.Timestamp, 0)
	expireTime := sigTime.Add(time.Duration(ttlMinutes) * time.Minute)
	return time.Now().After(expireTime)
}

// GetSignData 获取待签名数据（旧格式，仅兼容非 openclaw 路径）
// 根据 agent_did, skill_did, amount, currency, timestamp, nonce 构造
func GetSignData(agentDID, skillDID string, amountMinor int64, currency constants.Currency,
	timestamp int64, nonce string) string {
	return fmt.Sprintf("%s|%s|%d|%d|%d|%s",
		agentDID, skillDID, amountMinor, currency, timestamp, nonce)
}

// PaymentBusinessSignPayload 与 stablepay-openclaw-plugin/src/pay_settlement.ts 一致：
// bizSignPayload = bizMessageCore + bizTs + bizNonce（timestamp 与 nonce 紧跟在 hash 后，无额外分隔符）
// bizMessageCore = agentDid|skill_did|amountMinor|ccy|sha256_hex_utf8(signed_tx_base64)
func PaymentBusinessSignPayload(agentDID, skillDID string, amountMinor int64, currency constants.Currency,
	signedTxBase64 string, timestamp int64, nonce string) string {
	sum := sha256.Sum256([]byte(signedTxBase64))
	hashHex := hex.EncodeToString(sum[:])
	return fmt.Sprintf("%s|%s|%d|%d|%s%d%s",
		agentDID, skillDID, amountMinor, int64(currency), hashHex, timestamp, nonce)
}

// PageParam 分页参数值对象
type PageParam struct {
	Page     int
	PageSize int
}

// NewPageParam 创建分页参数
func NewPageParam(page, pageSize int) PageParam {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = constants.DefaultPageSize
	}
	if pageSize > constants.MaxPageSize {
		pageSize = constants.MaxPageSize
	}
	return PageParam{
		Page:     page,
		PageSize: pageSize,
	}
}

// Offset 计算偏移量
func (p PageParam) Offset() int {
	return (p.Page - 1) * p.PageSize
}

// Limit 返回限制数量
func (p PageParam) Limit() int {
	return p.PageSize
}

// PageResult 分页结果值对象
type PageResult struct {
	Total    int64
	Page     int
	PageSize int
}

// NewPageResult 创建分页结果
func NewPageResult(total int64, page, pageSize int) PageResult {
	return PageResult{
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}
}

// TotalPages 总页数
func (p PageResult) TotalPages() int {
	if p.PageSize == 0 {
		return 0
	}
	pages := int(p.Total) / p.PageSize
	if int(p.Total)%p.PageSize > 0 {
		pages++
	}
	return pages
}

// HasNext 是否有下一页
func (p PageResult) HasNext() bool {
	return p.Page < p.TotalPages()
}

// HasPrev 是否有上一页
func (p PageResult) HasPrev() bool {
	return p.Page > 1
}

// PaymentRequirement HTTP 402 支付要求值对象
type PaymentRequirement struct {
	SkillDID  string
	SkillName string
	Price     Amount
	Endpoint  string
}

// NewPaymentRequirement 创建支付要求
func NewPaymentRequirement(skillDID, skillName string, price Amount, endpoint string) PaymentRequirement {
	return PaymentRequirement{
		SkillDID:  skillDID,
		SkillName: skillName,
		Price:     price,
		Endpoint:  endpoint,
	}
}

// CommonCurrencyToString 将 common.Currency 转换为字符串
func CommonCurrencyToString(c common.Currency) string {
	switch c {
	case common.Currency_USDC:
		return "USDC"
	case common.Currency_USDT:
		return "USDT"
	}
	return "UNKNOWN"
}

// StringToCommonCurrency 将字符串转换为 common.Currency
func StringToCommonCurrency(s string) common.Currency {
	switch s {
	case "USDC":
		return common.Currency_USDC
	case "USDT":
		return common.Currency_USDT
	}
	return 0
}
