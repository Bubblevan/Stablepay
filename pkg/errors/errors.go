// Package errors 定义 Payment Service 的错误码和错误处理
package errors

import (
	"fmt"
)

// ErrorCode 错误码定义
// 与 common.thrift 中的 ErrorCode 保持一致
type ErrorCode int32

const (
	// 成功
	SUCCESS ErrorCode = 0

	// 1xxxx - 通用错误
	INVALID_PARAMETERS           ErrorCode = 10001
	RESOURCE_NOT_FOUND           ErrorCode = 10002
	PERMISSION_DENIED            ErrorCode = 10003
	SIGNATURE_VERIFICATION_FAILED ErrorCode = 10004

	// 2xxxx - 支付相关错误
	INSUFFICIENT_BALANCE     ErrorCode = 20001
	PAYMENT_ALREADY_EXISTS   ErrorCode = 20002
	BLOCKCHAIN_NETWORK_ERROR ErrorCode = 20003
	GAS_SUBSIDY_FAILED       ErrorCode = 20004
	PAYMENT_AMOUNT_EXCEEDED  ErrorCode = 20005
	DUPLICATE_NONCE          ErrorCode = 20006
	IDEMPOTENCY_KEY_MISMATCH ErrorCode = 20007
	PAYMENT_TIMEOUT          ErrorCode = 20008

	// 3xxxx - 系统错误
	INTERNAL_SERVER_ERROR    ErrorCode = 30001
	SERVICE_UNAVAILABLE      ErrorCode = 30002
	DATABASE_CONNECTION_ERROR ErrorCode = 30003
	RATE_LIMIT_EXCEEDED      ErrorCode = 30004
)

// 错误码到错误信息的映射
var errorCodeMessages = map[ErrorCode]string{
	SUCCESS:                       "success",
	INVALID_PARAMETERS:            "invalid parameters",
	RESOURCE_NOT_FOUND:            "resource not found",
	PERMISSION_DENIED:             "permission denied",
	SIGNATURE_VERIFICATION_FAILED: "signature verification failed",
	INSUFFICIENT_BALANCE:          "insufficient balance",
	PAYMENT_ALREADY_EXISTS:        "payment already exists",
	BLOCKCHAIN_NETWORK_ERROR:      "blockchain network error",
	GAS_SUBSIDY_FAILED:            "gas subsidy failed",
	PAYMENT_AMOUNT_EXCEEDED:       "payment amount exceeded maximum limit",
	DUPLICATE_NONCE:               "duplicate nonce detected",
	IDEMPOTENCY_KEY_MISMATCH:      "idempotency key mismatch with request",
	PAYMENT_TIMEOUT:               "payment timeout",
	INTERNAL_SERVER_ERROR:         "internal server error",
	SERVICE_UNAVAILABLE:           "service unavailable",
	DATABASE_CONNECTION_ERROR:     "database connection error",
	RATE_LIMIT_EXCEEDED:           "rate limit exceeded",
}

// Error 业务错误结构
type Error struct {
	Code    ErrorCode
	Message string
	Cause   error
}

// Error 实现 error 接口
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[code:%d] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[code:%d] %s", e.Code, e.Message)
}

// Unwrap 返回底层错误
func (e *Error) Unwrap() error {
	return e.Cause
}

// WithCause 添加错误原因
func (e *Error) WithCause(err error) *Error {
	e.Cause = err
	return e
}

// WithMessage 添加详细错误信息
func (e *Error) WithMessage(msg string) *Error {
	e.Message = msg
	return e
}

// New 创建新的业务错误
func New(code ErrorCode, message ...string) *Error {
	msg := errorCodeMessages[code]
	if len(message) > 0 && message[0] != "" {
		msg = message[0]
	}
	return &Error{
		Code:    code,
		Message: msg,
	}
}

// Newf 使用格式化字符串创建错误
func Newf(code ErrorCode, format string, args ...interface{}) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// Wrap 包装底层错误
func Wrap(code ErrorCode, err error, message ...string) *Error {
	e := New(code, message...)
	e.Cause = err
	return e
}

// IsErrorCode 检查错误是否匹配指定的错误码
func IsErrorCode(err error, code ErrorCode) bool {
	if e, ok := err.(*Error); ok {
		return e.Code == code
	}
	return false
}

// GetErrorCode 从错误中获取错误码
func GetErrorCode(err error) ErrorCode {
	if err == nil {
		return SUCCESS
	}
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return INTERNAL_SERVER_ERROR
}

// GetErrorMessage 获取错误码对应的默认消息
func GetErrorMessage(code ErrorCode) string {
	if msg, ok := errorCodeMessages[code]; ok {
		return msg
	}
	return "unknown error"
}
