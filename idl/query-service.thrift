/**
 * query-service 内部 RPC 契约（一期）
 */

namespace go stablepay.query_service

include "common.thrift"

enum TransactionType {
  PURCHASE = 1,
  REVENUE = 2,
}

struct GetBalanceSummaryRequest {
  1: common.BaseReq base,
  2: common.DID agent_did,
}

struct GetBalanceSummaryResponse {
  1: common.BaseResp base,
  2: i64 balance_minor,
  3: common.Currency currency,
  4: i64 monthly_spent_minor,
  5: i64 monthly_limit_minor,
}

struct TransactionItem {
  1: common.TxId tx_id,
  2: common.DID skill_did,
  3: i64 amount_minor,
  4: common.Currency currency,
  5: TransactionType type,
  6: string created_at,
}

struct ListTransactionsRequest {
  1: common.BaseReq base,
  2: common.DID did,
  3: TransactionType type,
  4: common.PageRequest page,
}

struct ListTransactionsResponse {
  1: common.BaseResp base,
  2: list<TransactionItem> items,
  3: common.PageResult page,
}

struct SalesTrendItem {
  1: string date,
  2: i64 amount_minor,
}

struct GetRevenueSummaryRequest {
  1: common.BaseReq base,
  2: common.DID skill_did,
}

struct GetRevenueSummaryResponse {
  1: common.BaseResp base,
  2: i64 total_revenue_minor,
  3: i32 total_sales,
  4: common.Currency currency,
  5: list<SalesTrendItem> sales_trend,
}

/** 与 HTTP /internal/sales 单条记录一致（currency 为展示用字符串 USDC/USDT） */
struct SaleRecordItem {
  1: common.TxId tx_id,
  2: optional common.TxId source_tx_id,
  3: common.DID agent_did,
  4: common.DID skill_did,
  5: i64 amount_minor,
  6: string currency,
  7: string created_at,
}

struct ListSalesRequest {
  1: common.BaseReq base,
  2: common.DID skill_did,
  3: i32 limit,
  4: i32 offset,
}

struct ListSalesResponse {
  1: common.BaseResp base,
  2: common.DID skill_did,
  3: list<SaleRecordItem> items,
  4: i64 total,
  5: i32 limit,
  6: i32 offset,
}

service QueryService {
  GetBalanceSummaryResponse GetBalanceSummary(1: GetBalanceSummaryRequest req),
  ListTransactionsResponse ListTransactions(1: ListTransactionsRequest req),
  GetRevenueSummaryResponse GetRevenueSummary(1: GetRevenueSummaryRequest req),
  ListSalesResponse ListSales(1: ListSalesRequest req),
}

