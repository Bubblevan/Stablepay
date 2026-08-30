# StablePay 完整测试手册

> 版本：基于 2026-04-05 当前运行环境撰写  
> 网关地址：`http://localhost:28080`（Docker 映射；内部 `:8080`）

---

## 零、必读：钱包体系说明（不懂先看这里）

### 两种钱包，完全不同的用途

| | 热钱包 (hotwallet) | Agent 钱包 (local-dev / OWS) |
|---|---|---|
| **是什么** | 平台的资金钱包 | Agent（用户）的身份钱包 |
| **存在哪** | `blockchain-adapter/conf/hotwallet.json` | 插件本地加密文件 / OWS Vault |
| **用途** | payment-service 调它执行**链上 SPL Token 转账** | 给 HTTP 请求签名（DID 鉴权） |
| **需要有 USDC** | ✅ 是的，它是资金来源 | ❌ 不需要，只做签名 |
| **私钥在哪** | `hotwallet.json` 明文存储（开发用） | 插件加密本地文件（AES-256-GCM） |
| **你需要另外创建吗** | 已经有了 | 插件会自动生成 |

**重要结论：**
- **不需要**创建 OWS 钱包。Windows 上 OWS SDK 没有原生 binding，插件自动用 `local-dev` 模式（生成 ed25519 密钥对，加密存本地）
- 热钱包是**平台的**，不是用户的。这是**托管模型**——用户存钱进来，平台热钱包替他们执行链上转账
- `hotwallet.json` 的私钥只用于：(1) 平台签交易上链；(2) 测试时临时当 agent 的 DID

---

## 一、启动确认

所有容器都应该已经在运行：

```
stablepay-api-gateway       :28080→:8080
stablepay-payment-service   :8082
stablepay-did-service       :8081 (Kitex RPC), :8181 (HTTP)
stablepay-blockchain-adapter :8083
stablepay-query-service     :8084, :8184 (HTTP)
stablepay-verification-service :8085, :8185 (HTTP)
```

验证：
```bash
curl -s http://localhost:28080/healthz
# → {"status":"ok"}
```

---

## 二、签名工具说明（DID 鉴权路由必须用）

Gateway 的 DID 鉴权路由（`/api/v1/pay`、`/api/v1/did/verify` 等）需要 4 个 Header：

```
X-StablePay-DID        你的 DID，如 did:solana:FMNs...
X-StablePay-Signature  对 canonical 的 ed25519 签名（Base58）
X-StablePay-Timestamp  ISO 8601 时间戳，如 2026-04-05T12:00:00Z（5分钟内有效）
X-StablePay-Nonce      随机字符串（全局唯一，不可重用）
```

**canonical 格式：**
```
METHOD\n/path\nqueryString\nSHA256(body_hex)
```

测试中我们用热钱包私钥来做签名（热钱包 DID 已在 did-service 注册过）：

```bash
# 热钱包信息（从 blockchain-adapter/conf/hotwallet.json 读取）
HOT_PRIV="3LiPaQYYmorCC3zFKdjfu1ie1e4WpcVN1GSYUo4wpHKBkf9TVL5noXurR5ersNRDvWtZQHcQS2kcA6rW4Ncukxgu"
HOT_DID="did:solana:FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z"
SKILL_DID="did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD"

# 通用签名函数（每次调用前必须原子性重新生成 TS + NONCE）
sign_request() {
  local method="$1" path="$2" query="$3" bodyfile="$4"
  TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  NONCE="n-$(date +%s%N | sha256sum | head -c 12)"
  SIG=$(cd D:/MyLab/StablePay/did-service && go run .codex-tmp/gw_sign.go \
        sign-raw-file "$HOT_PRIV" "$method" "$path" "${query:-__EMPTY__}" "$bodyfile" "$TS" "$NONCE")
}
```

> **注意**：每次请求必须用新的 TS + NONCE 组合，nonce 用过一次就失效（replay protection）

---

## 三、HTTP API 测试（通过网关）

### 3.1 健康检查（无需鉴权）

```bash
curl -s http://localhost:28080/healthz
```
**返回：**
```json
{"status":"ok"}
```

---

### 3.2 DID 服务

#### ① 创建 DID（无需鉴权）

```bash
curl -s -X POST http://localhost:28080/api/v1/did \
  -H "Content-Type: application/json" \
  -d '{
    "user_type":"agent",
    "metadata":{
      "public_key":"FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z",
      "wallet_address":"FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z",
      "source":"test"
    }
  }'
```

**返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "did": "did:solana:7xKXtg2CW87d97TXJSDpbD5jBkheTqA36vBRFQkfT7vN",
    "public_key": "7xKXtg2CW87d97TXJSDpbD5jBkheTqA36vBRFQkfT7vN",
    "wallet_address": "7xKXtg2CW87d97TXJSDpbD5jBkheTqA36vBRFQkfT7vN",
    "created_at": "2026-04-05T12:00:00Z"
  }
}
```

---

#### ② 注册 DID（客户端自持私钥，无需鉴权）

> 这是 OpenClaw 插件调用的路径。客户端自己生成密钥对，只把公钥注册进来。

```bash
# 用热钱包公钥演示注册（实际插件会用自己生成的公钥）
curl -s -X POST http://localhost:28080/api/v1/did \
  -H "Content-Type: application/json" \
  -d '{
    "user_type": "agent",
    "metadata": {
      "public_key": "FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z",
      "wallet_address": "FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z",
      "source": "stablepay-openclaw-plugin"
    }
  }'
```

**返回（与创建 DID 相同结构）：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "did": "did:solana:FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z",
    "public_key": "FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z",
    "wallet_address": "FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z",
    "created_at": "2026-04-05T12:00:00Z"
  }
}
```

---

#### ③ 查询 DID（需要 DID 鉴权）

```bash
HOT_PRIV="3LiPaQYYmorCC3zFKdjfu1ie1e4WpcVN1GSYUo4wpHKBkf9TVL5noXurR5ersNRDvWtZQHcQS2kcA6rW4Ncukxgu"
HOT_DID="did:solana:FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z"
TARGET_DID="did:solana:FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z"

echo '{}' > /tmp/empty.json

TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
NONCE="n-$(date +%s%N | sha256sum | head -c 12)"
SIG=$(cd D:/MyLab/StablePay/did-service && go run .codex-tmp/gw_sign.go \
      sign-raw-file "$HOT_PRIV" "GET" "/api/v1/did/$TARGET_DID" "__EMPTY__" /tmp/empty.json "$TS" "$NONCE")

curl -s "http://localhost:28080/api/v1/did/$TARGET_DID" \
  -H "X-StablePay-DID: $HOT_DID" \
  -H "X-StablePay-Signature: $SIG" \
  -H "X-StablePay-Timestamp: $TS" \
  -H "X-StablePay-Nonce: $NONCE"
```

**返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "did": "did:solana:FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z",
    "public_key": "FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z",
    "wallet_address": "FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z"
  }
}
```

**DID 不存在时返回：**
```json
{
  "code": 10002,
  "message": "did not found",
  "data": null
}
```

---

#### ④ 验证签名（需要 DID 鉴权）

```bash
echo '{"did":"did:solana:FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z","message":"hello","signature":"xxxxx","timestamp":"2026-04-05T12:00:00Z","nonce":"abc123"}' > /tmp/verify_body.json

TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
NONCE="n-$(date +%s%N | sha256sum | head -c 12)"
SIG=$(cd D:/MyLab/StablePay/did-service && go run .codex-tmp/gw_sign.go \
      sign-raw-file "$HOT_PRIV" "POST" "/api/v1/did/verify" "__EMPTY__" /tmp/verify_body.json "$TS" "$NONCE")

curl -s -X POST http://localhost:28080/api/v1/did/verify \
  -H "Content-Type: application/json" \
  -H "X-StablePay-DID: $HOT_DID" \
  -H "X-StablePay-Signature: $SIG" \
  -H "X-StablePay-Timestamp: $TS" \
  -H "X-StablePay-Nonce: $NONCE" \
  -d @/tmp/verify_body.json
```

**返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "valid": true
  }
}
```

---

### 3.3 支付服务

> **当前支付模型（重要）**：
> - `POST /api/v1/pay` 是唯一正式公开支付入口。
> - `POST /api/v1/pay` 是唯一正式公开支付入口。
> - 客户端只需提交业务授权签名字段（`signature/timestamp/nonce`），不再提交 `signed_tx_base64`。

#### ① 获取支付要求（无需鉴权，skill 开发者会调用这个给 agent 看）

```bash
SKILL_DID="did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD"

curl -s "http://localhost:28080/api/v1/pay/require?skill_did=${SKILL_DID}&agent_did=${HOT_DID}&skill_name=ShowMeTheMoney&price=1.00&currency=USDC&message=Pay+to+unlock"
```

**返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "skill_did": "did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD",
    "skill_name": "ShowMeTheMoney",
    "price": "1.00",
    "currency": "USDC",
    "message": "Pay to unlock",
    "payment_endpoint": "/api/v1/pay"
  }
}
```

---

#### ② 发起支付（需要 DID 鉴权，最核心的接口）

> **说明**：这里应使用 Agent 自己的 OWS 钱包，不要再用平台 hotwallet 伪装 agent。

```bash
HOT_DID="did:solana:FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z"
SKILL_DID="did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD"
IDEM_KEY="test-pay-$(date +%s)"

# 1. 生成业务签名所需的 nonce 和 timestamp（payload 里的）
PAY_TS=$(date +%s)
PAY_NONCE="pay-$(date +%s%N | head -c 16)"

# 2. 构造请求体（单步支付授权，不再携带 signed_tx_base64）
python3 -c "
import json
print(json.dumps({
  'agent_did': '$HOT_DID',
  'skill_did': '$SKILL_DID',
  'amount': '1.00',
  'currency': 'USDC',
  'signature': 'placeholder_biz_sig',
  'timestamp': $PAY_TS,
  'nonce': '$PAY_NONCE',
}))
" > /tmp/pay_body.json

# 3. 计算网关 DID 鉴权签名（必须在生成 body 之后立即签，5分钟内有效；推荐 OWS sign message）
GW_TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GW_NONCE="gw-$(date +%s%N | sha256sum | head -c 12)"
GW_SIG="<OWS_SIGN_MESSAGE_OUTPUT_BASE58>"

# 4. 发送请求
curl -s -X POST http://localhost:28080/api/v1/pay \
  -H "Content-Type: application/json" \
  -H "X-StablePay-DID: $HOT_DID" \
  -H "X-StablePay-Signature: $GW_SIG" \
  -H "X-StablePay-Timestamp: $GW_TS" \
  -H "X-StablePay-Nonce: $GW_NONCE" \
  -H "X-Idempotency-Key: $IDEM_KEY" \
  -d @/tmp/pay_body.json
```

**返回（PENDING 状态）：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "tx_id": "tx_a1b2c3d4e5f6",
    "status": "PENDING",
    "created_at": "2026-04-05T12:00:01Z"
  }
}
```

**5-10 秒后链上确认，状态变为 COMPLETED：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "tx_id": "tx_a1b2c3d4e5f6",
    "tx_hash": "4EU97X8kUrY1Bi2xMsURjMF8eKQaJStsoUW9EbM327P8...",
    "status": "COMPLETED",
    "created_at": "2026-04-05T12:00:01Z",
    "confirmed_at": "2026-04-05T12:00:08Z"
  }
}
```

**重复支付（幂等）：**
```json
{
  "code": 10007,
  "message": "duplicate transaction",
  "data": null
}
```

**余额不足：**
```json
{
  "code": 10005,
  "message": "insufficient balance",
  "data": null
}
```

---

#### ③ 查询支付状态（需要 DID 鉴权）

```bash
TX_ID="tx_a1b2c3d4e5f6"   # 替换为上一步返回的 tx_id

echo '{}' > /tmp/empty.json
TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
NONCE="n-$(date +%s%N | sha256sum | head -c 12)"
SIG=$(cd D:/MyLab/StablePay/did-service && go run .codex-tmp/gw_sign.go \
      sign-raw-file "$HOT_PRIV" "GET" "/api/v1/pay/$TX_ID" "__EMPTY__" /tmp/empty.json "$TS" "$NONCE")

curl -s "http://localhost:28080/api/v1/pay/$TX_ID" \
  -H "X-StablePay-DID: $HOT_DID" \
  -H "X-StablePay-Signature: $SIG" \
  -H "X-StablePay-Timestamp: $TS" \
  -H "X-StablePay-Nonce: $NONCE"
```

**返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "tx_id": "tx_a1b2c3d4e5f6",
    "agent_did": "did:solana:FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z",
    "skill_did": "did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD",
    "amount": "1.000000",
    "currency": "USDC",
    "status": "COMPLETED",
    "tx_hash": "4EU97X8kUrY1Bi2xMsURjMF8eKQaJStsoUW9EbM327P8...",
    "created_at": "2026-04-05T12:00:01Z",
    "confirmed_at": "2026-04-05T12:00:08Z"
  }
}
```

---

#### ④ 查询支付历史（需要 DID 鉴权）

```bash
QUERY="agent_did=${HOT_DID}&page=1&page_size=10"

echo '{}' > /tmp/empty.json
TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
NONCE="n-$(date +%s%N | sha256sum | head -c 12)"
SIG=$(cd D:/MyLab/StablePay/did-service && go run .codex-tmp/gw_sign.go \
      sign-raw-file "$HOT_PRIV" "GET" "/api/v1/pay/history" "$QUERY" /tmp/empty.json "$TS" "$NONCE")

curl -s "http://localhost:28080/api/v1/pay/history?${QUERY}" \
  -H "X-StablePay-DID: $HOT_DID" \
  -H "X-StablePay-Signature: $SIG" \
  -H "X-StablePay-Timestamp: $TS" \
  -H "X-StablePay-Nonce: $NONCE"
```

**返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "tx_id": "tx_a1b2c3d4e5f6",
        "skill_did": "did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD",
        "amount": "1.000000",
        "currency": "USDC",
        "status": "COMPLETED",
        "created_at": "2026-04-05T12:00:01Z"
      }
    ],
    "total": 1,
    "page": 1,
    "page_size": 10
  }
}
```

---

### 3.4 验证服务（需要 API Key）

> 这些接口给 skill 开发者的后端用，不是 agent 用的。用 `X-API-Key: stablepay-dev-key`。

#### ① 验证是否已购买（最常用）

```bash
AGENT_DID="did:solana:FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z"
SKILL_DID="did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD"

curl -s "http://localhost:28080/api/v1/verify?agent_did=$AGENT_DID&skill_did=$SKILL_DID" \
  -H "X-API-Key: stablepay-dev-key"
```

**已购买：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "purchased": true,
    "purchase_time": "2026-04-05T12:00:08Z"
  }
}
```

**未购买：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "purchased": false
  }
}
```

---

#### ② 批量验证

```bash
curl -s -X POST http://localhost:28080/api/v1/verify/batch \
  -H "Content-Type: application/json" \
  -H "X-API-Key: stablepay-dev-key" \
  -d '{
    "agent_did": "did:solana:FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z",
    "skill_dids": [
      "did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD",
      "did:solana:AnotherSkillDID111111111111111111111111111111"
    ]
  }'
```

**返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "results": {
      "did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD": {
        "purchased": true,
        "purchase_time": "2026-04-05T12:00:08Z"
      },
      "did:solana:AnotherSkillDID111111111111111111111111111111": {
        "purchased": false
      }
    }
  }
}
```

---

#### ③ 查询支付凭证

```bash
curl -s "http://localhost:28080/api/v1/verify/proof?agent_did=$AGENT_DID&skill_did=$SKILL_DID" \
  -H "X-API-Key: stablepay-dev-key"
```

**已购买时返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "tx_id": "tx_a1b2c3d4e5f6",
    "tx_hash": "4EU97X8kUrY1Bi2xMsURjMF8eKQaJStsoUW9EbM327P8...",
    "agent_did": "did:solana:FMNs...",
    "skill_did": "did:solana:9wNE...",
    "amount": "1.000000",
    "currency": "USDC",
    "purchase_time": "2026-04-05T12:00:08Z"
  }
}
```

---

### 3.5 查询服务（需要 DID 鉴权）

#### ① 查询余额

```bash
echo '{}' > /tmp/empty.json
QUERY="agent=${HOT_DID}"

TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
NONCE="n-$(date +%s%N | sha256sum | head -c 12)"
SIG=$(cd D:/MyLab/StablePay/did-service && go run .codex-tmp/gw_sign.go \
      sign-raw-file "$HOT_PRIV" "GET" "/api/v1/balance" "$QUERY" /tmp/empty.json "$TS" "$NONCE")

curl -s "http://localhost:28080/api/v1/balance?${QUERY}" \
  -H "X-StablePay-DID: $HOT_DID" \
  -H "X-StablePay-Signature: $SIG" \
  -H "X-StablePay-Timestamp: $TS" \
  -H "X-StablePay-Nonce: $NONCE"
```

**返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "balance": "39.000000",
    "currency": "USDC",
    "wallet_address": "FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z",
    "updated_at": "2026-04-05T12:00:08Z"
  }
}
```

---

#### ② 查询交易记录

```bash
QUERY="agent_did=${HOT_DID}&page=1&page_size=5"
echo '{}' > /tmp/empty.json
TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
NONCE="n-$(date +%s%N | sha256sum | head -c 12)"
SIG=$(cd D:/MyLab/StablePay/did-service && go run .codex-tmp/gw_sign.go \
      sign-raw-file "$HOT_PRIV" "GET" "/api/v1/transactions" "$QUERY" /tmp/empty.json "$TS" "$NONCE")

curl -s "http://localhost:28080/api/v1/transactions?${QUERY}" \
  -H "X-StablePay-DID: $HOT_DID" \
  -H "X-StablePay-Signature: $SIG" \
  -H "X-StablePay-Timestamp: $TS" \
  -H "X-StablePay-Nonce: $NONCE"
```

**返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "tx_id": "tx_a1b2c3d4e5f6",
        "type": "payment",
        "amount": "1.000000",
        "currency": "USDC",
        "status": "COMPLETED",
        "counterparty": "did:solana:9wNE...",
        "created_at": "2026-04-05T12:00:01Z"
      }
    ],
    "total": 1,
    "page": 1,
    "page_size": 5
  }
}
```

---

#### ③ Skill 开发者：查询销售收入（需要 DID 鉴权）

```bash
QUERY="skill_did=${SKILL_DID}"
echo '{}' > /tmp/empty.json
TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
NONCE="n-$(date +%s%N | sha256sum | head -c 12)"
SIG=$(cd D:/MyLab/StablePay/did-service && go run .codex-tmp/gw_sign.go \
      sign-raw-file "$HOT_PRIV" "GET" "/api/v1/revenue" "$QUERY" /tmp/empty.json "$TS" "$NONCE")

curl -s "http://localhost:28080/api/v1/revenue?${QUERY}" \
  -H "X-StablePay-DID: $HOT_DID" \
  -H "X-StablePay-Signature: $SIG" \
  -H "X-StablePay-Timestamp: $TS" \
  -H "X-StablePay-Nonce: $NONCE"
```

**返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "skill_did": "did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD",
    "total_revenue": "1.000000",
    "currency": "USDC",
    "transaction_count": 1
  }
}
```

---

### 3.6 Blockchain Adapter（内部服务，不对外暴露）

> Blockchain Adapter 是内部 Kitex RPC 服务（`:8083`），不通过 Gateway 暴露。
> payment-service 在容器内直接调用它，你无法用 curl 直接测。
>
> **验证它工作正常的方式**：看支付流程是否能成功上链（即 3.3.② 发起支付后能拿到 tx_hash）

如果需要直接测试（用内部 HTTP 端口，如果有的话）：

```bash
# Kitex RPC 服务无法用 curl，只能确认端口在监听
curl -s http://localhost:8083 || echo "Expected: TCP connection, not HTTP"
```

**间接验证**（发起支付后检查 tx_hash 是否上链）：
```bash
# 在 Solana devnet 浏览器查找 tx_hash
# https://explorer.solana.com/tx/<TX_HASH>?cluster=devnet
```

---

## 四、常见错误与解决

| 错误 | 原因 | 解决方法 |
|---|---|---|
| `missing did signature fields` | 缺少 DID 鉴权 Header | 添加 4 个 X-StablePay- headers |
| `timestamp out of range` | TS 超过 5 分钟 | 重新生成 TS（必须原子性执行整个签名块） |
| `nonce already used` | NONCE 被重用 | 每次请求生成新 NONCE |
| `signature invalid` | 签名错误 | 检查 canonical 格式；确保 body 文件与请求 body 完全一致 |
| `insufficient balance` | 热钱包 USDC 不足 | 热钱包余额见 blockchain-adapter/conf/hotwallet.json |
| `did not found` | DID 未在 did-service 注册 | 先调 POST /api/v1/did 注册 |
| `invalid signature` | 业务签名或 DID Header 签名不匹配 | 重新按 canonical 与业务 signData 使用 OWS 生成签名 |
| `duplicate transaction` | X-Idempotency-Key 重用 | 更换幂等键 |

---

## 四点五、构建注意事项（vendor/replace）

- `api-gateway` 使用 `-mod=vendor` 构建时，`go.mod` 和 `vendor/modules.txt` 必须一致。
- 不要保留无效本地 replace（例如本机不存在 `../stablepay-common` 却仍写在 `go.mod`），否则会出现 `inconsistent vendoring`。
- 如需本地联调多仓，优先使用 `go.work` 管理工作区，避免污染服务目录内的 `go.mod`。

---

## 五、OpenClaw 插件模拟测试

### 5.0 前置：必须设置的环境变量

```bash
# ⚠️ 这个是插件加密本地状态文件的主密钥，你自己设定任意字符串
# 忘了设或者改了，本地状态就读不出来了
export STABLEPAY_PLUGIN_MASTER_KEY="my-dev-secret-2026"

# 如果用 ows-cli / wsl-ows runtime（Windows 上一般用不上）
# export STABLEPAY_OWS_PASSPHRASE="your-ows-wallet-passphrase"
```

> **关于 OWS 钱包：Windows 上不需要创建 OWS 钱包。**
> 插件检测到没有 OWS SDK 或 CLI 时会自动使用 `local-dev` 模式：
> 生成 ed25519 密钥对 → 加密存到 `~/.stablepay-openclaw/stablepay-local-state.enc`
> 只需要设置 `STABLEPAY_PLUGIN_MASTER_KEY` 即可。

---

### 5.1 使用 demo-pay.mjs 模拟插件工具调用

```bash
cd D:/MyLab/StablePay/stablepay-openclaw-plugin

# 先构建（如果 dist/ 不是最新的）
npm run build

# 运行完整流程模拟
STABLEPAY_PLUGIN_MASTER_KEY="my-dev-secret-2026" node demo-pay.mjs
```

**demo-pay.mjs 模拟的工具调用顺序：**
1. `runtime.getStatus()` → 检查本地钱包
2. `client.executeDemoSkill()` → GET /api/v1/pay/require（期望 402）
3. `runtime.signMessage(paymentSignData)` → 业务签名
4. `runtime.signMessage(canonical, append_timestamp_nonce: true)` → 网关 DID 鉴权签名
5. `client.paySigned()` → POST /api/v1/pay（带 DID 鉴权 headers）

---

### 5.2 使用 demo-skill.mjs 模拟完整 skill 购买流程

```bash
# 先启动 demo backend
cd D:/MyLab/StablePay/stablepay-openclaw-plugin/showmethemoney-skill/demo-backend
GATEWAY_BASE_URL=http://127.0.0.1:28080 \
SKILL_DID=did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD \
STABLEPAY_API_KEY=stablepay-dev-key \
node server.mjs &
# → 监听在 http://127.0.0.1:8787

# 然后运行 demo-skill.mjs
cd D:/MyLab/StablePay/stablepay-openclaw-plugin
STABLEPAY_PLUGIN_MASTER_KEY="my-dev-secret-2026" node demo-skill.mjs
```

---

### 5.3 逐个工具模拟（模拟 OpenClaw 调用插件的过程）

以下代码等价于 OpenClaw 调用各个 `stablepay_*` 工具时插件内部执行的操作。

#### 工具 1：`stablepay_runtime_status`

```bash
STABLEPAY_PLUGIN_MASTER_KEY="my-dev-secret-2026" node -e "
import('./dist/runtime.js').then(async ({ StablePayRuntime }) => {
  const cfg = {
    backendBaseUrl: 'http://127.0.0.1:28080',
    owsRuntime: 'auto',
    walletNamePrefix: 'stablepay',
    didRegisterPath: '/api/v1/did',
    localStatePath: require('os').homedir() + '/.stablepay-openclaw/stablepay-local-state.enc',
    localStateKeyEnv: 'STABLEPAY_PLUGIN_MASTER_KEY',
    owsPassphraseEnv: 'STABLEPAY_OWS_PASSPHRASE',
    owsVaultPath: '',
    requestTimeoutMs: 8000,
    rewardAmount: 1,
    owsRestBaseUrl: '', owsRestSignPath: '/v1/sign/message',
    owsRestApiKeyEnv: 'STABLEPAY_OWS_REST_API_KEY',
    owsRestAuthMode: 'bearer', owsRestWalletId: '',
    owsRestChainId: 'solana:5eykt4UsFv8P8NJdTREpY1vzqKqZKvdp',
    verifyPageBaseUrl: 'http://127.0.0.1:3000/verify'
  };
  const rt = new StablePayRuntime(cfg);
  console.log(JSON.stringify(await rt.getStatus(), null, 2));
});
"
```

**第一次运行（没有钱包）返回：**
```json
{
  "requested_driver": "auto",
  "active_driver": "local-dev",
  "available_drivers": ["local-dev"],
  "local_state_path": "C:/Users/yourname/.stablepay-openclaw/stablepay-local-state.enc",
  "has_wallet": false,
  "wallet": null,
  "payment_config": null,
  "policy": null,
  "notes": [
    "OWS Node SDK could not be loaded in this environment. On the current Windows machine, the official package does not ship a win32 native binding yet.",
    "The plugin will use a local AES-256-GCM encrypted state file as the current development fallback."
  ]
}
```

**创建钱包后返回：**
```json
{
  "requested_driver": "auto",
  "active_driver": "local-dev",
  "available_drivers": ["local-dev"],
  "local_state_path": "C:/Users/yourname/.stablepay-openclaw/stablepay-local-state.enc",
  "has_wallet": true,
  "wallet": {
    "wallet_id": "localdev_a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "wallet_name": "stablepay-alice",
    "did": "did:solana:8xKyZ9vQ2mL4nP6rT1wE3bF5gH7jM0sD",
    "wallet_address": "8xKyZ9vQ2mL4nP6rT1wE3bF5gH7jM0sD",
    "runtime_driver": "local-dev",
    "backend_did": ""
  },
  "payment_config": null,
  "policy": null,
  "notes": [...]
}
```

---

#### 工具 2：`stablepay_create_local_wallet`

> ⚠️ 这个工具会在本地生成新的 ed25519 密钥对并加密存储。**不需要热钱包私钥，不需要 OWS 钱包，不需要 USDC。**

```bash
STABLEPAY_PLUGIN_MASTER_KEY="my-dev-secret-2026" node -e "
import('./dist/runtime.js').then(async ({ StablePayRuntime }) => {
  const cfg = { /* 同上 */ };
  const rt = new StablePayRuntime(cfg);
  const result = await rt.createLocalWallet({ user_id: 'alice', user_type: 'agent' });
  console.log(JSON.stringify(result, null, 2));
});
"
```

**OpenClaw 中调用参数：**
```json
{
  "user_id": "alice",
  "user_type": "agent"
}
```

**返回：**
```json
{
  "runtime_driver": "local-dev",
  "wallet_id": "localdev_a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "wallet_name": "stablepay-alice",
  "did": "did:solana:8xKyZ9vQ2mL4nP6rT1wE3bF5gH7jM0sD",
  "public_key": "8xKyZ9vQ2mL4nP6rT1wE3bF5gH7jM0sD",
  "wallet_address": "8xKyZ9vQ2mL4nP6rT1wE3bF5gH7jM0sD",
  "created_at": "2026-04-05T12:00:00.000Z",
  "notes": [
    "OWS Node SDK could not be loaded in this environment...",
    "The plugin will use a local AES-256-GCM encrypted state file..."
  ]
}
```

> **注意**：每次调用会覆盖之前的钱包状态。如果已有钱包，先确认是否需要保留。

---

#### 工具 3：`stablepay_register_local_did`

> 把刚生成的钱包公钥注册到 did-service，这样 Gateway 才能验证它的签名。

**OpenClaw 中调用参数：**
```json
{
  "user_type": "agent"
}
```

插件会调用 `POST http://127.0.0.1:28080/api/v1/did`，body 为：
```json
{
  "user_type": "agent",
  "public_key": "8xKyZ9vQ2mL4nP6rT1wE3bF5gH7jM0sD",
  "wallet_address": "8xKyZ9vQ2mL4nP6rT1wE3bF5gH7jM0sD",
  "metadata": {
    "sign_runtime": "local-dev",
    "source": "stablepay-openclaw-plugin"
  }
}
```

**成功返回：**
```json
{
  "backend_did": "did:solana:8xKyZ9vQ2mL4nP6rT1wE3bF5gH7jM0sD",
  "wallet_address": "8xKyZ9vQ2mL4nP6rT1wE3bF5gH7jM0sD",
  "wallet_id": "localdev_a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}
```

---

#### 工具 4：`stablepay_configure_payment_limits`

> 设置支付上限。这是本地操作，不请求网络，只写加密状态文件。

**OpenClaw 中调用参数：**
```json
{
  "single_purchase_limit_usdc": 5.0,
  "auto_purchase_threshold_usdc": 2.0,
  "currency": "USDC"
}
```

**返回：**
```json
{
  "ok": true,
  "payment_config": {
    "singlePurchaseLimitUsdc": 5.0,
    "autoPurchaseThresholdUsdc": 2.0,
    "currency": "USDC",
    "updatedAt": "2026-04-05T12:00:00.000Z"
  },
  "local_state_path": "C:/Users/yourname/.stablepay-openclaw/stablepay-local-state.enc"
}
```

> **说明**：
> - `single_purchase_limit_usdc`：单次购买最大金额，超过此值会直接拒绝
> - `auto_purchase_threshold_usdc`：低于此值自动购买，高于此值需要用户手动确认
> - 例如设置 `single=5, auto=2`：1 USDC 自动购买，3 USDC 需确认，6 USDC 直接拒绝

---

#### 工具 5：`stablepay_execute_paid_skill_demo`（完整购买流程）

> 这是最核心的工具，模拟 agent 购买 skill 的完整流程。

**OpenClaw 中调用参数：**
```json
{
  "execute_url": "http://127.0.0.1:8787/execute",
  "retry_attempts": 6,
  "retry_delay_ms": 1500
}
```

**内部执行步骤与各步骤的数据：**

**步骤 1 - 调用 skill 后端，收到 402：**
```
GET http://127.0.0.1:8787/execute?agent_did=did:solana:8xKy...
```
```json
{
  "code": 402,
  "message": "Payment Required",
  "payment_requirement": {
    "data": {
      "skill_did": "did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD",
      "skill_name": "ShowMeTheMoney",
      "price": "1.00",
      "currency": "USDC",
      "message": "Pay to unlock ShowMeTheMoney",
      "payment_endpoint": "/api/v1/pay"
    }
  }
}
```

**步骤 2 - 检查支付限额（本地）：**
```
price=1.00 ≤ auto_purchase_threshold=2.0 → 自动购买，无需确认
```

**步骤 3 - 签名业务数据：**
```
paymentSignData = "did:solana:8xKy...|did:solana:9wNE...|100|1|1712318400|1712318400123-abcd1234"
                   ↑agentDid          ↑skillDid           ↑minor↑currency code  ↑unix ts  ↑nonce
```

**步骤 4 - 计算 Gateway DID 鉴权 canonical：**
```
payBody = {"agent_did":"did:solana:8xKy...","skill_did":"did:solana:9wNE...","amount":"1.00",...}
canonical = "POST\n/api/v1/pay\n\n<sha256(payBody)>"
signPayload = canonical + isoTimestamp + nonce
```

**步骤 5 - POST /api/v1/pay 结果：**
```json
{
  "tx_id": "tx_8xky9wne1712318401",
  "status": "PENDING",
  "created_at": "2026-04-05T12:00:01Z"
}
```

**步骤 6 - 等待后重试 GET /execute，返回 200：**
```json
{
  "ok": true,
  "protected_result": "Show me the money: access granted",
  "agent_did": "did:solana:8xKy...",
  "skill_did": "did:solana:9wNE..."
}
```

**最终插件工具返回给 OpenClaw 的文本：**
```
Paid skill demo completed successfully.
Agent DID: did:solana:8xKyZ9vQ2mL4nP6rT1wE3bF5gH7jM0sD
Skill DID: did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD
Quoted price: 1.00 USDC
Pay tx_id: tx_8xky9wne1712318401
Execute URL: http://127.0.0.1:8787/execute
Retry attempts: 6
Final execute status: 200
JSON:
{
  "first_attempt": { "code": 402, ... },
  "payment_requirement": { "skill_did": "...", "price": "1.00", ... },
  "gateway_auth": {
    "did": "did:solana:8xKy...",
    "timestamp": "2026-04-05T12:00:01.000Z",
    "nonce": "1712318401000-abcd1234-gw",
    "canonical": "POST\n/api/v1/pay\n\n3a7f2..."
  },
  "pay_response": { "tx_id": "tx_8xky9wne1712318401", "status": "PENDING" },
  "final_attempt": { "ok": true, "protected_result": "Show me the money: access granted" }
}
```

**超出支付上限时返回：**
```
Local payment policy denied the purchase before signing.
Quoted price: 10.00 USDC
Single purchase limit: 5.0 USDC
No payment request was sent to StablePay.
```

**需要手动确认时返回：**
```
Manual confirmation is required before paying this skill.
Quoted price: 3.00 USDC
Auto purchase threshold: 2.0 USDC
This tool stopped before signing so the user can confirm explicitly.
```

---

#### 工具 6：`stablepay_sign_message`（调试用）

**OpenClaw 中调用参数：**
```json
{
  "message": "hello stablepay",
  "chain": "solana",
  "append_timestamp_nonce": false
}
```

**返回：**
```json
{
  "did": "did:solana:8xKyZ9vQ2mL4nP6rT1wE3bF5gH7jM0sD",
  "wallet_id": "localdev_a1b2c3d4-...",
  "wallet_address": "8xKyZ9vQ2mL4nP6rT1wE3bF5gH7jM0sD",
  "public_key": "8xKyZ9vQ2mL4nP6rT1wE3bF5gH7jM0sD",
  "runtime_driver": "local-dev",
  "signature": "5Dn7r9...base58_encoded_ed25519_sig...",
  "payload": "hello stablepay",
  "timestamp": "2026-04-05T12:00:00.000Z",
  "nonce": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "appended_timestamp_nonce": false
}
```

---

## 六、OpenClaw 插件配置（在 OpenClaw 的 plugins.entries 里设置）

```json
{
  "plugins": {
    "entries": {
      "stablepay": {
        "enabled": true,
        "config": {
          "backendBaseUrl": "http://127.0.0.1:28080",
          "requestTimeoutMs": 8000,
          "owsRuntime": "auto"
        }
      }
    }
  },
  "tools": {
    "allow": [
      "stablepay_runtime_status",
      "stablepay_create_local_wallet",
      "stablepay_register_local_did",
      "stablepay_configure_payment_limits",
      "stablepay_build_payment_policy",
      "stablepay_sign_message",
      "stablepay_execute_paid_skill_demo",
      "stablepay_query_balance"
    ]
  }
}
```

> **关键**：`backendBaseUrl` 必须是 `28080`，不是 `8080`。

---

## 七、快速验证脚本（一键运行全流程）

```bash
#!/usr/bin/env bash
# 保存为 D:/MyLab/StablePay/scripts/smoke_test.sh
set -e

GW="http://localhost:28080"
HOT_PRIV="3LiPaQYYmorCC3zFKdjfu1ie1e4WpcVN1GSYUo4wpHKBkf9TVL5noXurR5ersNRDvWtZQHcQS2kcA6rW4Ncukxgu"
HOT_DID="did:solana:FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z"
HOT_PUB="FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z"
SKILL_DID="did:solana:9wNEugNiMaRVw6suArSf88MmM7VQDhkKZxA4KDzwt1RD"

echo "[1/5] 健康检查..."
curl -sf "$GW/healthz" | grep -q '"ok"' && echo "✓ Gateway OK"

echo "[2/5] DID 查询（无鉴权）..."
curl -sf -X POST "$GW/api/v1/did" \
  -H "Content-Type: application/json" \
  -d "{\"user_type\":\"agent\",\"metadata\":{\"public_key\":\"$HOT_PUB\",\"wallet_address\":\"$HOT_PUB\"}}" | python3 -m json.tool | grep -q '"did"' && echo "✓ DID Create OK"

echo "[3/5] 支付要求（无鉴权）..."
curl -sf "$GW/api/v1/pay/require?skill_did=$SKILL_DID&agent_did=$HOT_DID&price=1.00&currency=USDC" \
  | python3 -m json.tool | grep -q '"price"' && echo "✓ Pay Require OK"

echo "[4/5] 验证服务（API Key）..."
curl -sf "$GW/api/v1/verify?agent_did=$HOT_DID&skill_did=$SKILL_DID" \
  -H "X-API-Key: stablepay-dev-key" | python3 -m json.tool | grep -q '"purchased"' && echo "✓ Verify OK"

echo "[5/5] 发起支付（DID 鉴权）..."
PAY_TS=$(date +%s)
PAY_NONCE="pay-$(date +%s%N | head -c 12)"
IDEM="smoke-$(date +%s)"
python3 -c "
import json
print(json.dumps({
  'agent_did': '$HOT_DID',
  'skill_did': '$SKILL_DID',
  'amount': '1.00',
  'currency': 'USDC',
  'signature': 'test_sig',
  'timestamp': $PAY_TS,
  'nonce': '$PAY_NONCE'
}))
" > /tmp/smoke_pay.json
GW_TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GW_NONCE="gw-$(date +%s%N | sha256sum | head -c 12)"
GW_SIG=$(cd D:/MyLab/StablePay/did-service && go run .codex-tmp/gw_sign.go \
          sign-raw-file "$HOT_PRIV" "POST" "/api/v1/pay" "__EMPTY__" /tmp/smoke_pay.json "$GW_TS" "$GW_NONCE")
curl -sf -X POST "$GW/api/v1/pay" \
  -H "Content-Type: application/json" \
  -H "X-StablePay-DID: $HOT_DID" \
  -H "X-StablePay-Signature: $GW_SIG" \
  -H "X-StablePay-Timestamp: $GW_TS" \
  -H "X-StablePay-Nonce: $GW_NONCE" \
  -H "X-Idempotency-Key: $IDEM" \
  -d @/tmp/smoke_pay.json | python3 -m json.tool | grep -q '"tx_id"' && echo "✓ Pay OK"

echo ""
echo "=== 所有 smoke test 通过 ==="
```
