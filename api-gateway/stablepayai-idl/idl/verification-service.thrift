/**
 * verification-service 内部 RPC 契约（一期）
 *
 * 说明：
 * - 本服务主要通过消费支付事件写入购买关系；同时对外提供 verify/proof 等查询能力。
 */

namespace go stablepay.verification_service

include "common.thrift"

struct VerifyPurchaseRequest {
  1: common.BaseReq base,
  2: common.DID agent_did,
  3: common.DID skill_did,
}

struct VerifyPurchaseResponse {
  1: common.BaseResp base,
  2: bool purchased,
  3: optional string purchase_time,
  4: optional common.TxId tx_id,
  5: optional i64 amount_minor,
  6: optional common.Currency currency,
}

struct BatchVerifyItem {
  1: common.DID skill_did,
  2: bool purchased,
  3: optional string purchase_time,
  4: optional common.TxId tx_id,
}

struct BatchVerifyPurchaseRequest {
  1: common.BaseReq base,
  2: common.DID agent_did,
  3: list<common.DID> skill_dids,
}

struct BatchVerifyPurchaseResponse {
  1: common.BaseResp base,
  2: list<BatchVerifyItem> items,
}

struct GetPurchaseProofRequest {
  1: common.BaseReq base,
  2: common.DID agent_did,
  3: common.DID skill_did,
}

struct GetPurchaseProofResponse {
  1: common.BaseResp base,
  2: bool purchased,
  3: optional string purchase_time,
  4: optional common.TxId tx_id,
  5: optional i64 amount_minor,
  6: optional common.Currency currency,
  7: optional common.TxHash tx_hash,
  /** 建议字段：proof 版本 */
  8: optional string proof_version,
}

service VerificationService {
  VerifyPurchaseResponse VerifyPurchase(1: VerifyPurchaseRequest req),
  BatchVerifyPurchaseResponse BatchVerifyPurchase(1: BatchVerifyPurchaseRequest req),
  GetPurchaseProofResponse GetPurchaseProof(1: GetPurchaseProofRequest req),
}

