/**
 * blockchain-adapter 内部 RPC 契约（一期）
 *
 * 说明：
 * - 本服务只负责链上执行与查询，不负责支付业务规则判断。
 * - 一期仅支持 Solana 主网；币种为 USDC/USDT。
 */

namespace go stablepay.blockchain_adapter

include "common.thrift"

enum TxStatus {
  PENDING = 1,
  CONFIRMED = 2,
  FAILED = 3,
}

struct TransferStableCoinRequest {
  1: common.BaseReq base,

  /** 付款方钱包地址（若由服务端代构造交易则需要；待确认） */
  2: optional string from_wallet_address,

  /** 收款方钱包地址 */
  3: string to_wallet_address,

  /** 最小单位金额（6 decimals） */
  4: i64 amount_minor,
  5: common.Currency currency,

  /**
   * 客户端签名后的交易（base64），用于完全由客户端构造/签名交易的模式（待确认）。
   * 一期可先按 Payment Service 与 Adapter 的实现方式二选一。
   */
  6: optional string signed_tx_base64,
}

struct TransferStableCoinResponse {
  1: common.BaseResp base,
  2: common.TxHash tx_hash,
  3: TxStatus status,
  /** ISO8601（建议字段） */
  4: optional string confirmed_at,
}

struct GetBalanceRequest {
  1: common.BaseReq base,
  2: string wallet_address,
  3: common.Currency currency,
}

struct GetBalanceResponse {
  1: common.BaseResp base,
  2: i64 balance_minor,
  3: common.Currency currency,
}

struct GetTxStatusRequest {
  1: common.BaseReq base,
  2: common.TxHash tx_hash,
}

struct GetTxStatusResponse {
  1: common.BaseResp base,
  2: TxStatus status,
  3: optional string confirmed_at,
  4: optional string failed_at,
  5: optional string reason_code,
  6: optional string reason_message,
}

service BlockchainAdapterService {
  TransferStableCoinResponse TransferStableCoin(1: TransferStableCoinRequest req),
  GetBalanceResponse GetBalance(1: GetBalanceRequest req),
  GetTxStatusResponse GetTxStatus(1: GetTxStatusRequest req),
}

