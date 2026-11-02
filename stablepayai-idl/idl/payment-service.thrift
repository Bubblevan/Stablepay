/**
 * payment-service 内部 RPC 契约（一期）
 *
 * 说明：
 * - 对外 canonical API：POST /api/v1/pay
 * - 业务金额统一使用最小单位整数 amount_minor（USDC/USDT 为 2 decimals）
 * - blockchain-adapter 在链上边界将业务 amount_minor 换算为 token raw units（6 decimals）
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
  8: optional string nonce,
  9: optional string signed_tx_base64,
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

struct GetPaymentRequirementRequest {
  1: common.BaseReq base,
  2: common.DID skill_did,
  3: optional common.DID agent_did,
  4: optional string skill_name,
  5: optional string amount,
  6: optional string price,
  7: optional common.Currency currency,
  8: optional string message,
}

struct GetPaymentRequirementResponse {
  1: common.BaseResp base,
  2: bool already_purchased,
  3: optional common.DID skill_did,
  4: optional string skill_name,
  5: optional string price,
  6: optional common.Currency currency,
  7: optional string message,
  8: optional string payment_endpoint,
}

/** Provider-neutral CNY order managed by payment-service. */
struct FiatPaymentOrder {
  1: string id,
  2: string request_id,
  3: string product_id,
  4: string subject,
  5: string quote_hash,
  6: string approval_ref,
  7: string agent_did,
  8: string skill_did,
  9: i64 amount_minor,
  10: string currency,
  11: string mode,
  12: string status,
  13: optional string trade_no,
  14: optional string verified_by,
  15: string created_at,
  16: optional string paid_at,
}

struct CreateFiatPaymentRequest {
  1: common.BaseReq base,
  2: string request_id,
  3: string product_id,
  4: string subject,
  5: string quote_hash,
  6: string approval_ref,
  7: common.DID agent_did,
  8: common.DID skill_did,
  9: i64 amount_minor,
  10: string currency,
  11: string nonce,
}
struct CreateFiatPaymentResponse { 1: common.BaseResp base, 2: FiatPaymentOrder order }
struct GetFiatPaymentRequest { 1: common.BaseReq base, 2: string order_id }
struct GetFiatPaymentResponse { 1: common.BaseResp base, 2: FiatPaymentOrder order }
struct FindFiatPaymentRequest { 1: common.BaseReq base, 2: string request_id }
struct FindFiatPaymentResponse { 1: common.BaseResp base, 2: FiatPaymentOrder order }
struct FiatPaymentActionRequest { 1: common.BaseReq base, 2: string order_id }
struct FiatPaymentActionResponse {
  1: common.BaseResp base,
  2: optional FiatPaymentOrder order,
  3: optional string cashier_url,
  4: optional string mode,
}
struct NotifyAlipayRequest { 1: common.BaseReq base, 2: map<string, string> parameters }
struct NotifyAlipayResponse { 1: common.BaseResp base, 2: optional FiatPaymentOrder order }

/** One metered API invocation protected by Alipay AI Pay Machine Pay. */
struct MachinePayAccessRequest {
  1: common.BaseReq base,
  2: string resource_id,
  3: string goods_name,
  4: i64 amount_minor,
  5: optional string payment_proof_header,
}
struct MachinePayAccessResponse {
  1: common.BaseResp base,
  2: bool authorized,
  3: string order_id,
  4: string payment_link,
  5: string resource_id,
  6: i64 amount_minor,
  7: string status,
}

service PaymentService {
  InitiatePaymentResponse InitiatePayment(1: InitiatePaymentRequest req),
  GetPaymentStatusResponse GetPaymentStatus(1: GetPaymentStatusRequest req),
  ListPaymentHistoryResponse ListPaymentHistory(1: ListPaymentHistoryRequest req),
  GetPaymentRequirementResponse GetPaymentRequirement(1: GetPaymentRequirementRequest req),
  CreateFiatPaymentResponse CreateFiatPayment(1: CreateFiatPaymentRequest req),
  GetFiatPaymentResponse GetFiatPayment(1: GetFiatPaymentRequest req),
  FindFiatPaymentResponse FindFiatPayment(1: FindFiatPaymentRequest req),
  FiatPaymentActionResponse CheckoutFiatPayment(1: FiatPaymentActionRequest req),
  FiatPaymentActionResponse ReconcileFiatPayment(1: FiatPaymentActionRequest req),
  FiatPaymentActionResponse SimulateFiatPayment(1: FiatPaymentActionRequest req),
  NotifyAlipayResponse NotifyAlipay(1: NotifyAlipayRequest req),
  MachinePayAccessResponse MachinePayAccess(1: MachinePayAccessRequest req),
}

