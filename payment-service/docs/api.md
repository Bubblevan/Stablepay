# Payment Service API 接口文档

## 接口规范

### 基础 URL

```
https://api.stablepay.co
```

### 统一响应格式

```json
{
  "code": 0,
  "message": "success",
  "data": { ... },
  "request_id": "uuid",
  "timestamp": 1704067200
}
```

### HTTP 状态码

- `200` - 成功
- `400` - 请求参数错误
- `402` - 需要支付（Payment Required）
- `429` - 请求频率超限
- `500` - 服务器内部错误

---

## 1. 发起支付

发起一笔新的支付交易。

### 请求

```http
POST /api/v1/pay
Content-Type: application/json
X-Idempotency-Key: {client_generated_uuid}
```

### 请求参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| agent_did | string | 是 | 支付方 DID |
| skill_did | string | 是 | 收款方 Skill DID |
| amount | string | 是 | 支付金额（如 "5.00"）|
| currency | string | 是 | 币种：USDC / USDT |
| signature | string | 是 | 支付签名 |
| timestamp | int64 | 是 | 签名时间戳（秒）|
| nonce | string | 是 | 防重放随机数 |

### 请求示例

```json
{
  "agent_did": "did:solana:4fK9x2Hy...",
  "skill_did": "did:solana:dev123...",
  "amount": "5.00",
  "currency": "USDC",
  "signature": "base58_encoded_signature",
  "timestamp": 1704067200,
  "nonce": "random_nonce_string"
}
```

### 签名数据构造

```
sign_data = agent_did + "|" + skill_did + "|" + amount_minor + "|" + currency + "|" + timestamp + "|" + nonce
```

### 响应示例

**成功 (200)**

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "tx_id": "550e8400-e29b-41d4-a716-446655440000",
    "tx_hash": "5x8fA8vGz...",
    "status": "pending",
    "created_at": "2024-01-01T00:00:00Z"
  },
  "request_id": "req_123456",
  "timestamp": 1704067200
}
```

**余额不足 (400)**

```json
{
  "code": 20001,
  "message": "insufficient balance",
  "data": {
    "required": "5.00",
    "actual": "2.50",
    "currency": "USDC"
  },
  "request_id": "req_123456",
  "timestamp": 1704067200
}
```

---

## 2. 查询支付状态

查询指定交易的支付状态。

### 请求

```http
GET /api/v1/pay/{tx_id}
```

### 路径参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| tx_id | string | 是 | 交易 ID |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "tx_id": "550e8400-e29b-41d4-a716-446655440000",
    "agent_did": "did:solana:4fK9x2Hy...",
    "skill_did": "did:solana:dev123...",
    "amount": "5.00",
    "currency": "USDC",
    "status": "confirmed",
    "tx_hash": "5x8fA8vGz...",
    "created_at": "2024-01-01T00:00:00Z",
    "confirmed_at": "2024-01-01T00:00:05Z"
  },
  "request_id": "req_123456",
  "timestamp": 1704067200
}
```

### 状态说明

| 状态 | 说明 |
|------|------|
| CREATED | 已创建，待处理 |
| PENDING | 链上交易中 |
| CONFIRMED | 链上已确认 |
| COMPLETED | 已完成 |
| FAILED | 支付失败 |
| CANCELLED | 已取消 |

---

## 3. 查询支付历史

查询用户的支付历史记录。

### 请求

```http
GET /api/v1/pay/history?agent_did={did}&page=1&page_size=20
```

### 查询参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| agent_did | string | 是 | Agent DID |
| status | string | 否 | 状态过滤 |
| page | int | 否 | 页码，默认 1 |
| page_size | int | 否 | 每页数量，默认 20，最大 100 |

### 响应示例

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [
      {
        "tx_id": "550e8400-e29b-41d4-a716-446655440000",
        "skill_did": "did:solana:dev123...",
        "amount": "5.00",
        "currency": "USDC",
        "status": "completed",
        "created_at": "2024-01-01T00:00:00Z"
      }
    ],
    "total": 100,
    "page": 1,
    "page_size": 20
  },
  "request_id": "req_123456",
  "timestamp": 1704067200
}
```

---

## 4. 获取支付要求（HTTP 402）

检查资源是否需要支付，返回 HTTP 402 响应。

### 请求

```http
GET /api/v1/pay/require?skill_did={did}&agent_did={did}
```

### 查询参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| skill_did | string | 是 | Skill DID |
| agent_did | string | 否 | Agent DID（用于检查是否已购买）|

### 响应示例

**需要支付 (402)**

```json
{
  "code": 402,
  "message": "Payment Required",
  "data": {
    "skill_did": "did:solana:dev123",
    "skill_name": "AI 写作助手",
    "price": "5.00",
    "currency": "USDC",
    "payment_endpoint": "https://api.stablepay.co/api/v1/pay"
  },
  "request_id": "req_123456",
  "timestamp": 1704067200
}
```

**已购买 (200)**

```json
{
  "code": 0,
  "message": "Already purchased",
  "data": {
    "purchased": true,
    "purchase_time": "2024-01-01T00:00:00Z"
  },
  "request_id": "req_123456",
  "timestamp": 1704067200
}
```

---

## 5. 短链验证（兼容接口）

用于 skill.md 中的短链验证。

### 请求

```http
GET /verify?skill={skill_did}&agent={agent_did}
```

### 响应示例

```json
{
  "purchased": true,
  "purchase_time": "2024-01-01T00:00:00Z"
}
```

---

## 错误码对照表

| 错误码 | 错误信息 | 说明 |
|--------|----------|------|
| 0 | success | 请求成功 |
| 10001 | invalid parameters | 请求参数不合法 |
| 10004 | signature verification failed | 签名验证失败 |
| 20001 | insufficient balance | 钱包余额不足 |
| 20002 | payment already exists | 重复支付 |
| 20003 | blockchain network error | 区块链网络错误 |
| 20005 | payment amount exceeded | 支付金额超限 |
| 20006 | duplicate nonce | 重复 nonce |
| 20007 | idempotency key mismatch | 幂等键与请求不匹配 |
| 30001 | internal server error | 系统内部错误 |
| 30004 | rate limit exceeded | 请求频率超限 |

---

## 幂等性说明

### 客户端生成幂等性键

```
X-Idempotency-Key: {UUID}
```

### 服务端幂等性保护

- 幂等性键格式：`agent_did:skill_did:client_idempotency_key`
- 有效期：30 分钟
- 相同幂等性键 + 相同请求参数 = 返回缓存结果
- 相同幂等性键 + 不同请求参数 = 返回错误

---

## 安全说明

### 签名有效期

签名时间戳与服务器时间差必须 ≤ 5 分钟。

### Nonce 防重放

每个请求的 `nonce` 必须全局唯一，已使用的 nonce 会被拒绝。

### 金额限制

- 单笔支付上限：1000 USDC
- 金额必须为正数
