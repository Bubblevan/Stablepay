/**
 * payment-service 内部 RPC 契约（一期）
 *
 * 说明：
 * - 对外 canonical API：POST /api/v1/pay
 * - 内部金额统一使用最小单位整数 amount_minor（6 decimals）
 */

namespace go stablepay.payment_service

include "common.thrift"

struct InitiatePaymentRequest {
  1: common.BaseReq base,
  2: common.DID agent_did,
  /**
   * 一期：skill_did == 收款方 DID（developer/payee DID）
   */
  3: common.DID skill_did,
  4: i64 amount_minor,
  5: common.Currency currency,

  /**
   * 支付签名（外部可能在 body 或 header；内部保留字段便于对齐与审计）
   * 待确认：签名串拼装规则由网关或 stablepay-common 统一。
   */
  6: optional string signature,
  7: optional string timestamp,
}

struct InitiatePaymentResponse {
  1: common.BaseResp base,
  2: common.TxId tx_id,
  3: common.TxHash tx_hash,
  4: common.PaymentStatus status,
  5: string created_at,
  6: optional string confirmed_at,
  7: optional string failed_at,
}

struct GetPaymentStatusRequest {
  1: common.BaseReq base,
  2: common.TxId tx_id,
}

struct GetPaymentStatusResponse {
  1: common.BaseResp base,
  2: common.TxId tx_id,
  3: common.PaymentStatus status,
  4: optional common.TxHash tx_hash,
  5: optional string confirmed_at,
  6: optional string failed_at,
}

struct PaymentHistoryItem {
  1: common.TxId tx_id,
  2: common.DID skill_did,
  3: i64 amount_minor,
  4: common.Currency currency,
  5: common.PaymentStatus status,
  6: string created_at,
}

struct ListPaymentHistoryRequest {
  1: common.BaseReq base,
  2: common.DID agent_did,
  3: common.PageRequest page,
}

struct ListPaymentHistoryResponse {
  1: common.BaseResp base,
  2: list<PaymentHistoryItem> items,
  3: common.PageResult page,
}

service PaymentService {
  InitiatePaymentResponse InitiatePayment(1: InitiatePaymentRequest req),
  GetPaymentStatusResponse GetPaymentStatus(1: GetPaymentStatusRequest req),
  ListPaymentHistoryResponse ListPaymentHistory(1: ListPaymentHistoryRequest req),
}

