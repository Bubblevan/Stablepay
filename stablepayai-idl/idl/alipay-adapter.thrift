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

service AlipayAdapterService {
  CheckoutResponse CreateCheckout(1: CheckoutRequest req),
  QueryResponse QueryTrade(1: QueryRequest req),
  VerifyNotificationResponse VerifyNotification(1: VerifyNotificationRequest req),
}
