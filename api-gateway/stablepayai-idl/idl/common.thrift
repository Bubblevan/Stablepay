/**
 * StablePay DEMO 一期通用类型定义
 *
 * 说明：
 * - 本文件提供“最小自洽”的通用结构，便于先跑通一期链路。
 * - 若 stablepay-common 中已存在统一类型/错误码/trace 结构，应以 stablepay-common 为准并在后续对齐调整。
 */

namespace go stablepay.common

typedef string DID
typedef string TxId
typedef string TxHash

enum ErrorCode {
  SUCCESS = 0,

  INVALID_PARAMETERS = 10001,
  RESOURCE_NOT_FOUND = 10002,
  PERMISSION_DENIED = 10003,
  SIGNATURE_VERIFICATION_FAILED = 10004,

  INSUFFICIENT_BALANCE = 20001,
  PAYMENT_ALREADY_EXISTS = 20002,
  BLOCKCHAIN_NETWORK_ERROR = 20003,
  GAS_SUBSIDY_FAILED = 20004,

  INTERNAL_SERVER_ERROR = 30001,
  SERVICE_UNAVAILABLE = 30002,
  DATABASE_CONNECTION_ERROR = 30003,
  RATE_LIMIT_EXCEEDED = 30004,
}

enum Currency {
  USDC = 1,
  USDT = 2,
}

enum PaymentStatus {
  PENDING = 1,
  CONFIRMED = 2,
  FAILED = 3,
}

struct BaseReq {
  /** 建议由网关生成并透传 */
  1: optional string request_id,
  2: optional string trace_id,

  /** 建议使用毫秒时间戳；外部 API 可能使用 ISO8601，进入内部后建议转换 */
  3: optional i64 timestamp_ms,

  /** 幂等键：建议用于支付/链上执行等关键写操作 */
  4: optional string idempotency_key,
}

struct BaseResp {
  /** 业务错误码，语义与对外 HTTP code 一致 */
  1: i32 code,
  2: string message,
  3: optional string request_id,
  4: optional string trace_id,
}

struct PageRequest {
  1: i32 limit,
  2: i32 offset,
}

struct PageResult {
  1: i32 total,
}

/**
 * 金额（内部表达）：最小单位整数
 * - 一期约定：USDC/USDT 按 6 decimals 处理
 */
struct MoneyMinor {
  1: i64 amount_minor,
  2: Currency currency,
}

