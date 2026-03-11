# 对外 HTTP API 契约（一期）

## 1. 适用范围

本文档描述 StablePay DEMO 一期对外 HTTP API 契约。对外 API 统一通过 **API Gateway** 暴露，后端 canonical API 为 RESTful `/api/v1/*`。

一期同时兼容 skill.md 嵌入场景的短链接入口：

- `GET /pay?skill=...&price=...`：支付挑战/引导入口（网关兼容短链）
- `GET /verify?skill=...&agent=...`：验证入口（网关映射到 `GET /api/v1/verify`）

> 约定：短链接是“入口/兼容”，真实支付提交仍走 `POST /api/v1/pay`。

## 2. 基础约定

### 2.1 URL 与版本

- canonical：`/api/v1/*`
- 破坏性变更：发布新版本前缀（例如 `/api/v2/*`）

### 2.2 统一响应格式

所有对外接口（含短链接映射后的返回）使用统一响应结构：

```json
{
  "code": 0,
  "message": "success",
  "data": {}
}
```

- `code`：业务错误码（见“错误码”）
- `message`：可读错误信息
- `data`：业务数据（成功时返回对象/数组，失败时可为空对象）

### 2.3 币种与精度

- 一期支持：`USDC`、`USDT`
- 外部 API：`amount` 使用 **字符串 decimal**（例如 `"5.00"`、`"0.50"`），避免浮点误差
- 内部服务/DB/链适配层：统一转换为最小单位整数（一期按 **6 decimals** 处理）

## 3. 认证与鉴权（一期最小可用）

技术方案给出的认证方式包括 DID 签名认证与 API Key 认证：

- 支付接口：需要 Agent DID 签名认证
- 验证接口：支持 API Key 或 DID 签名认证
- 查询接口：需要对应 DID 权限

**待确认/建议**（一期可先做最小实现，后续与 `stablepay-common` 对齐统一中间件）：

- 推荐统一使用请求头传递签名信息，例如：
  - `X-StablePay-DID`：发起方 DID
  - `X-StablePay-Timestamp`：ISO8601 或毫秒时间戳（二选一，需统一）
  - `X-StablePay-Signature`：对 `path + body + timestamp` 的签名
- API Key 使用请求头：`X-API-Key`

说明：在 `POST /api/v1/pay` 的一期示例中，`signature/timestamp` 也可放在 body 中。网关与后端服务应统一解析并校验。

## 4. 错误码

一期错误码（来自技术方案）：

| code | message | 说明 |
|---:|---|---|
| 0 | success | 请求成功 |
| 10001 | invalid parameters | 请求参数不合法 |
| 10002 | resource not found | 请求的资源不存在 |
| 10003 | permission denied | 无权限访问该资源 |
| 10004 | signature verification failed | DID 签名验证失败 |
| 20001 | insufficient balance | 钱包余额不足 |
| 20002 | payment already exists | 重复支付 |
| 20003 | blockchain network error | 区块链网络错误 |
| 20004 | gas subsidy failed | Gas 费补贴失败 |
| 30001 | internal server error | 系统内部错误 |
| 30002 | service unavailable | 依赖服务不可用 |
| 30003 | database connection error | 数据库连接错误 |
| 30004 | rate limit exceeded | 请求频率超限 |

HTTP 状态码使用（一期）：

- 200：成功
- 400：客户端请求错误（参数错误、签名失败等）
- 401：未认证（签名缺失）
- 402：需要支付（Payment Required，核心状态码）
- 403：无权限
- 404：资源不存在
- 429：触发限流
- 500：服务端错误
- 503：依赖服务故障

## 5. 接口列表（一期）

以下为一期最小可用字段集；未在文档中明确的字段以“建议字段”标注，后续可扩展。

### 5.1 DID 管理

#### POST /api/v1/did

请求：

- `user_type`：`agent` \| `developer`
- `metadata`：对象（`name`、`description` 等）

响应 `data`：

- `did`
- `public_key`
- `wallet_address`
- `created_at`（ISO8601）

#### POST /api/v1/did/verify

请求：

- `did`
- `message`
- `signature`
- `timestamp`（ISO8601）

响应 `data`：

- `valid`：bool

#### GET /api/v1/did/{did}

路径参数：

- `did`

响应 `data`（最小字段）：

- `did`
- `public_key`
- `wallet_address`

### 5.2 支付处理

#### POST /api/v1/pay

请求（最小字段）：

- `agent_did`
- `skill_did`：一期定义为 **收款方 DID（developer/payee DID）**
- `amount`：字符串 decimal
- `currency`：`USDC` \| `USDT`
- `signature`
- `timestamp`（ISO8601）

响应 `data`：

- `tx_id`
- `tx_hash`
- `status`：`confirmed` \| `failed` \| `pending`（建议统一枚举）
- `confirmed_at`（ISO8601，建议字段）

#### GET /api/v1/pay/{tx_id}

响应 `data`（最小字段）：

- `tx_id`
- `status`
- `tx_hash`（建议字段）
- `confirmed_at` / `failed_at`（建议字段）

#### GET /api/v1/pay/history

查询参数：

- `agent_did`
- `limit`
- `offset`

响应 `data`：

- `items[]`：每项包含 `tx_id`、`skill_did`、`amount`、`currency`、`status`、`created_at`
- `total`

### 5.3 购买验证

#### GET /api/v1/verify

查询参数：

- `agent_did`
- `skill_did`

响应 `data`（最小字段）：

- `purchased`
- `purchase_time`
- `tx_id`
- `amount`
- `currency`

#### POST /api/v1/verify/batch

请求：

- `agent_did`
- `skill_dids[]`

响应 `data`（建议字段）：

- `items[]`：每项包含 `skill_did`、`purchased`、`purchase_time`、`tx_id`

#### GET /api/v1/verify/proof

查询参数：

- `agent_did`
- `skill_did`

响应 `data`（最小字段）：

- `purchased`
- `purchase_time`
- `tx_id`
- `amount`
- `currency`
- `tx_hash`（建议字段）
- `proof_version`（建议字段）

### 5.4 数据查询

#### GET /api/v1/balance

查询参数：

- `agent_did`

响应 `data`：

- `balance`
- `currency`
- `monthly_spent`
- `monthly_limit`

#### GET /api/v1/transactions

查询参数：

- `did`
- `type`：`purchase` \| `revenue`
- `limit`
- `offset`

响应 `data`：

- `items[]`：每项包含 `tx_id`、`skill_did`、`amount`、`currency`、`type`、`created_at`
- `total`

#### GET /api/v1/revenue

查询参数：

- `skill_did`

响应 `data`（最小字段）：

- `total_revenue`
- `total_sales`
- `currency`
- `sales_trend[]`（`date`、`amount`）

### 5.5 短链接兼容入口（skill.md 场景）

#### GET /pay?skill={SKILL_DID}&price={PRICE}

- 语义：支付挑战/引导入口（网关短链），用于让 Agent 发现“该 skill 需要支付、支付多少”
- 网关行为（建议）：
  - 返回统一响应结构，或返回可被 Agent 直接消费的提示信息（待对齐）
  - 不替代 `POST /api/v1/pay` 的支付提交

#### GET /verify?skill={SKILL_DID}&agent={AGENT_DID}

- 语义：验证入口（网关短链）
- 网关行为：映射到 `GET /api/v1/verify`

