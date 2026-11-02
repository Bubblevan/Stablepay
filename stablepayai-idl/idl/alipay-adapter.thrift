/** Alipay provider boundary. Only the adapter knows RSA2 keys and Alipay APIs. */
namespace go stablepay.alipay_adapter

include "common.thrift"

struct PaymentOrder {
  1: string order_id,
  2: string subject,
  3: i64 amount_minor,
}
struct CheckoutRequest { 1: PaymentOrder order }
struct CheckoutResponse { 1: common.BaseResp base, 2: string cashier_url }
struct QueryRequest { 1: PaymentOrder order }
struct TradeReceipt {
  1: string order_id,
  2: string trade_no,
  3: i64 amount_minor,
  4: string status,
  5: string verified_by,
}
struct QueryResponse { 1: common.BaseResp base, 2: TradeReceipt receipt }
struct VerifyNotificationRequest { 1: map<string, string> parameters }
struct VerifyNotificationResponse { 1: common.BaseResp base, 2: TradeReceipt receipt }

/** Alipay AI Pay Machine Pay (A2M) request, separate from page-pay orders. */
struct MachinePayOrder {
  1: string order_id,
  2: string resource_id,
  3: string goods_name,
  4: i64 amount_minor,
  5: string pay_before,
}
struct MachinePayPrecreateRequest { 1: MachinePayOrder order }
struct MachinePayPrecreateResponse {
  1: common.BaseResp base,
  2: string order_id,
  3: string payment_link,
}
struct MachinePayProofRequest { 1: string payment_proof_header }
struct MachinePayReceipt {
  1: string out_trade_no,
  2: string trade_no,
  3: string resource_id,
  4: i64 amount_minor,
  5: bool active,
  6: string verified_by,
}
struct MachinePayVerifyResponse {
  1: common.BaseResp base,
  2: MachinePayReceipt receipt,
}

service AlipayAdapterService {
  CheckoutResponse CreateCheckout(1: CheckoutRequest req),
  QueryResponse QueryTrade(1: QueryRequest req),
  VerifyNotificationResponse VerifyNotification(1: VerifyNotificationRequest req),
  MachinePayPrecreateResponse PrecreateMachinePay(1: MachinePayPrecreateRequest req),
  MachinePayVerifyResponse VerifyMachinePayProof(1: MachinePayProofRequest req),
}
