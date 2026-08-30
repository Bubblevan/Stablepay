/**
 * did-service 内部 RPC 契约（一期）
 */

namespace go stablepay.did_service

include "common.thrift"

enum UserType {
  AGENT = 1,
  DEVELOPER = 2,
}

struct CreateDIDRequest {
  1: common.BaseReq base,
  2: UserType user_type,
  /** 元数据（一期最小可用；字段可扩展） */
  3: optional map<string, string> metadata,
}

struct CreateDIDResponse {
  1: common.BaseResp base,
  2: common.DID did,
  3: string public_key,
  4: string wallet_address,
  /** ISO8601 */
  5: string created_at,
}

/** 绑定客户端/OWS 已有钱包（服务端不生成、不存私钥）；did 固定为 did:solana:{canonical_public_key} */
struct RegisterDIDRequest {
  1: common.BaseReq base,
  2: UserType user_type,
  3: string public_key,
  4: string wallet_address,
  5: string wallet_id,
  6: string wallet_name,
  7: optional map<string, string> metadata,
}

struct RegisterDIDResponse {
  1: common.BaseResp base,
  2: common.DID did,
  3: string public_key,
  4: string wallet_address,
  5: string created_at,
}

struct GetDIDRequest {
  1: common.BaseReq base,
  2: common.DID did,
}

struct GetDIDResponse {
  1: common.BaseResp base,
  2: common.DID did,
  3: string public_key,
  4: string wallet_address,
}

struct VerifySignatureRequest {
  1: common.BaseReq base,
  2: common.DID did,
  3: string message,
  4: string signature,
  /** 外部可能传 ISO8601；内部建议转 timestamp_ms，但一期先保留原始字段便于对齐 */
  5: string timestamp,
  /** 防重放 nonce（建议字段） */
  6: optional string nonce,
}

struct VerifySignatureResponse {
  1: common.BaseResp base,
  2: bool valid,
}

struct UpdateDIDConfigRequest {
  1: common.BaseReq base,
  2: common.DID did,
  /** 配置版本（建议字段） */
  3: optional i64 config_version,
  /** 配置变更（一期最小：KV；后续可演进为结构化配置） */
  4: map<string, string> config_kv,
}

struct UpdateDIDConfigResponse {
  1: common.BaseResp base,
  2: optional i64 new_config_version,
}

service DIDService {
  CreateDIDResponse CreateDID(1: CreateDIDRequest req),
  RegisterDIDResponse RegisterDID(1: RegisterDIDRequest req),
  GetDIDResponse GetDID(1: GetDIDRequest req),
  VerifySignatureResponse VerifySignature(1: VerifySignatureRequest req),
  UpdateDIDConfigResponse UpdateDIDConfig(1: UpdateDIDConfigRequest req),
}

