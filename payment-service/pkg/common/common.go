// Package common 提供共享的IDL类型定义
// 当 code.wenfu.cn/stablepayai/stablepay-common 可访问时，应替换为真实实现
package common

// ErrorCode 错误码类型
type ErrorCode int32

// ErrorCode 常量定义
const (
	ErrorCode_SUCCESS                     ErrorCode = 0
	ErrorCode_INVALID_PARAMETERS          ErrorCode = 10001
	ErrorCode_RESOURCE_NOT_FOUND          ErrorCode = 10002
	ErrorCode_PERMISSION_DENIED           ErrorCode = 10003
	ErrorCode_SIGNATURE_VERIFICATION_FAILED ErrorCode = 10004
	ErrorCode_DUPLICATE_NONCE             ErrorCode = 10005
	ErrorCode_IDEMPOTENCY_KEY_MISMATCH    ErrorCode = 10006
	ErrorCode_PAYMENT_AMOUNT_EXCEEDED     ErrorCode = 10007

	ErrorCode_INSUFFICIENT_BALANCE     ErrorCode = 20001
	ErrorCode_PAYMENT_ALREADY_EXISTS   ErrorCode = 20002
	ErrorCode_BLOCKCHAIN_NETWORK_ERROR ErrorCode = 20003
	ErrorCode_GAS_SUBSIDY_FAILED       ErrorCode = 20004
	ErrorCode_TRANSACTION_TIMEOUT      ErrorCode = 20005

	ErrorCode_INTERNAL_SERVER_ERROR     ErrorCode = 50001
	ErrorCode_SERVICE_UNAVAILABLE       ErrorCode = 50002
	ErrorCode_DATABASE_CONNECTION_ERROR ErrorCode = 50003
	ErrorCode_RATE_LIMIT_EXCEEDED       ErrorCode = 50004
)

// Currency 货币类型
type Currency int32

// Currency 常量定义
const (
	Currency_USDC Currency = 1
	Currency_USDT Currency = 2
)

// PaymentStatus 支付状态类型
type PaymentStatus int32

// PaymentStatus 常量定义
const (
	PaymentStatus_CREATED   PaymentStatus = 0
	PaymentStatus_PENDING   PaymentStatus = 1
	PaymentStatus_CONFIRMED PaymentStatus = 2
	PaymentStatus_COMPLETED PaymentStatus = 3
	PaymentStatus_FAILED    PaymentStatus = 4
	PaymentStatus_CANCELLED PaymentStatus = 5
)

// String 返回错误码的字符串表示
func (e ErrorCode) String() string {
	switch e {
	case ErrorCode_SUCCESS:
		return "SUCCESS"
	case ErrorCode_INVALID_PARAMETERS:
		return "INVALID_PARAMETERS"
	case ErrorCode_RESOURCE_NOT_FOUND:
		return "RESOURCE_NOT_FOUND"
	case ErrorCode_PERMISSION_DENIED:
		return "PERMISSION_DENIED"
	case ErrorCode_SIGNATURE_VERIFICATION_FAILED:
		return "SIGNATURE_VERIFICATION_FAILED"
	case ErrorCode_DUPLICATE_NONCE:
		return "DUPLICATE_NONCE"
	case ErrorCode_IDEMPOTENCY_KEY_MISMATCH:
		return "IDEMPOTENCY_KEY_MISMATCH"
	case ErrorCode_PAYMENT_AMOUNT_EXCEEDED:
		return "PAYMENT_AMOUNT_EXCEEDED"
	case ErrorCode_INSUFFICIENT_BALANCE:
		return "INSUFFICIENT_BALANCE"
	case ErrorCode_PAYMENT_ALREADY_EXISTS:
		return "PAYMENT_ALREADY_EXISTS"
	case ErrorCode_BLOCKCHAIN_NETWORK_ERROR:
		return "BLOCKCHAIN_NETWORK_ERROR"
	case ErrorCode_GAS_SUBSIDY_FAILED:
		return "GAS_SUBSIDY_FAILED"
	case ErrorCode_TRANSACTION_TIMEOUT:
		return "TRANSACTION_TIMEOUT"
	case ErrorCode_INTERNAL_SERVER_ERROR:
		return "INTERNAL_SERVER_ERROR"
	case ErrorCode_SERVICE_UNAVAILABLE:
		return "SERVICE_UNAVAILABLE"
	case ErrorCode_DATABASE_CONNECTION_ERROR:
		return "DATABASE_CONNECTION_ERROR"
	case ErrorCode_RATE_LIMIT_EXCEEDED:
		return "RATE_LIMIT_EXCEEDED"
	default:
		return "UNKNOWN"
	}
}

// String 返回货币的字符串表示
func (c Currency) String() string {
	switch c {
	case Currency_USDC:
		return "USDC"
	case Currency_USDT:
		return "USDT"
	default:
		return "UNKNOWN"
	}
}

// String 返回支付状态的字符串表示
func (p PaymentStatus) String() string {
	switch p {
	case PaymentStatus_CREATED:
		return "CREATED"
	case PaymentStatus_PENDING:
		return "PENDING"
	case PaymentStatus_CONFIRMED:
		return "CONFIRMED"
	case PaymentStatus_COMPLETED:
		return "COMPLETED"
	case PaymentStatus_FAILED:
		return "FAILED"
	case PaymentStatus_CANCELLED:
		return "CANCELLED"
	default:
		return "UNKNOWN"
	}
}
