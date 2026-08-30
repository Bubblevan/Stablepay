# 事件消息契约（RocketMQ，一期）

## 1. 适用范围

本文档定义 StablePay DEMO 一期服务间异步事件契约。事件通过 RocketMQ 投递，payload 编码为 JSON。

一期约定：

- payload 编码：JSON
- 投递语义：at-least-once（至少一次）
- 幂等策略：消费者（例如 `verification-service`）以 `event_id` 或 `tx_id` 去重
- RocketMQ topic/tag：一期先按 `topic = event_type`，tag 后续细化

## 2. 事件列表（一期必须覆盖）

- `payment.success`
- `payment.failed`

必要时可补充（一期已在技术方案中出现）：

- `did.created`（建议）
- `did.config.updated`（建议）

## 3. 通用事件信封（建议）

一期已确定的支付事件体字段如下；后续如需统一“事件信封”，建议新增 `event_version`，并保持向后兼容（消费者忽略未知字段）。

## 4. 事件体定义

### 4.1 payment.success

语义：链上交易确认成功后发布，用于驱动购买关系写入、审计等下游处理。

JSON 示例：

```json
{
  "event_id": "uuid",
  "event_type": "payment.success",
  "topic": "payment.success",
  "idempotency_key": "tx_id",
  "tx_id": "pay_xxx",
  "agent_did": "did:solana:agent123",
  "skill_did": "did:solana:skill456",
  "amount": "5.00",
  "currency": "USDC",
  "tx_hash": "solana_tx_hash",
  "confirmed_at": "2026-02-27T12:00:05Z"
}
```

字段说明（最小集）：

- `event_id`：事件唯一标识（UUID）
- `event_type`：固定为 `payment.success`
- `topic`：一期固定等于 `event_type`
- `idempotency_key`：一期建议为 `tx_id`
- `tx_id`：支付交易 ID（业务侧生成）
- `agent_did`：付款方 DID
- `skill_did`：一期定义为收款方 DID（developer/payee DID）
- `amount`：字符串 decimal
- `currency`：`USDC` \| `USDT`
- `tx_hash`：链上交易哈希
- `confirmed_at`：确认时间（ISO8601）

### 4.2 payment.failed

语义：支付过程失败时发布（例如余额不足、链异常、签名失败等）。

JSON 示例：

```json
{
  "event_id": "uuid",
  "event_type": "payment.failed",
  "topic": "payment.failed",
  "idempotency_key": "tx_id",
  "tx_id": "pay_xxx",
  "agent_did": "did:solana:agent123",
  "skill_did": "did:solana:skill456",
  "amount": "5.00",
  "currency": "USDC",
  "reason_code": "insufficient_balance",
  "reason_message": "insufficient balance",
  "failed_at": "2026-02-27T12:00:05Z"
}
```

字段说明（最小集）：

- `reason_code`：机器可读失败原因（示例：`insufficient_balance`）
- `reason_message`：可读失败信息
- `failed_at`：失败时间（ISO8601）

## 5. 消费者幂等与去重（一期约定）

- `verification-service` 消费支付事件时必须实现幂等：
  - 建议使用 `event_id` 去重（强一致），或以 `tx_id` 去重（更贴近业务）
  - 推荐将去重键落库或写入缓存并设置合理 TTL

