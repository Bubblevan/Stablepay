# StablePay API 测试指南

本文档包含所有微服务的 API 测试接口和预期返回。

## 启动方法
```bash
docker-compose -f docker-compose.infra.yml down -v
docker-compose -f docker-compose.infra.yml up -d
docker-compose -f docker-compose.infra.yml ps
docker logs stablepay-rocketmq-broker
# 创建Verification需要的topic
docker exec -it stablepay-rocketmq-broker bash -c "sh mqadmin updatetopic -n rocketmq-nameserver:9876 -c DefaultCluster -t payment_events"
```

```bash
cd verification-service
go run .
```

```bash
cd query-service
go run .
```

```bash
cd did-service
go run cmd/server/main.go
```

```bash
cd blockchain-adapter
go run cmd/server/main.go -config conf/dev.local.yaml

```

```bash
cd payment-service
go run cmd/payment-service/main.go

```

```bash
cd api-gateway
go run cmd/api-gateway/main.go -config configs/config.yaml
```

## 服务状态

| 服务 | 端口 | 状态检查 |
|------|------|---------|
| API Gateway | 8080 | /healthz, /readyz |
| DID Service | 8081 | 通过 Gateway 代理 |
| Payment Service | 8082 | 通过 Gateway 代理 |
| Blockchain Adapter | 8083 | 独立服务 |
| Query Service | 8084 | 通过 Gateway 代理 |
| Verification Service | 8085 | 通过 Gateway 代理 |

---

## 1. 健康检查

### Gateway 健康状态
```bash
curl http://localhost:8080/healthz
```
**预期返回：**
```json
{"status":"ok"}
```

### Gateway 就绪状态
```bash
curl http://localhost:8080/readyz
```
**预期返回：**
```json
{"status":"ready"}
```

---

## 2. DID 服务

### 创建 DID
```bash
curl -X POST http://localhost:8080/api/v1/did \
  -H "Content-Type: application/json" \
  -d '{
    "wallet_address": "your_wallet_address",
    "chain_type": "solana"
  }'
```
**预期返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "created_at": "2026-03-21T15:49:01Z",
    "did": "did:solana:0ec8c962-b3c0-45a2-b674-18da2dba75e6",
    "public_key": "mock_public_key",
    "user_type": null,
    "wallet_address": "mock_wallet_address"
  },
  "request_id": "8a983129-492a-4cfb-91de-0b32357ed1e7",
  "timestamp": "2026-03-21T15:49:01Z"
}
```

### 查询 DID
```bash
curl http://localhost:8080/api/v1/did/did:solana:0ec8c962-b3c0-45a2-b674-18da2dba75e6
```
**预期返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "did": "did:solana:...",
    "wallet_address": "...",
    "public_key": "...",
    "created_at": "..."
  }
}
```

### 验证 DID（需要签名）
```bash
curl -X POST http://localhost:8080/api/v1/did/verify \
  -H "Content-Type: application/json" \
  -d '{
    "did": "did:solana:0ec8c962-b3c0-45a2-b674-18da2dba75e6",
    "signature": "base64_encoded_signature",
    "message": "message_that_was_signed"
  }'
```
**注意：** 当前版本验证接口需要额外的签名参数，创建 DID 后需要通过钱包签名消息才能验证。

**预期返回（成功）：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "valid": true,
    "did": "did:solana:..."
  }
}
```

**预期返回（失败）：**
```json
{
  "code": 10004,
  "message": "signature verification failed",
  "data": {
    "detail": "missing did signature fields"
  }
}
```

---

## 3. 支付服务

### 创建支付
```bash
curl -X POST http://localhost:8080/api/v1/pay \
  -H "Content-Type: application/json" \
  -d '{
    "did": "did:solana:0ec8c962-b3c0-45a2-b674-18da2dba75e6",
    "amount": "10.00",
    "currency": "USDC",
    "recipient": "recipient_wallet_address",
    "signature": "...",
    "nonce": "...",
    "idempotency_key": "unique_key_for_this_request"
  }'
```
**预期返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "tx_id": "txn_xxx",
    "status": "pending",
    "created_at": "2026-03-21T..."
  }
}
```

### 查询支付状态
```bash
curl http://localhost:8080/api/v1/pay/txn_xxx
```
**预期返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "tx_id": "txn_xxx",
    "status": "confirmed",
    "confirmations": 5,
    "tx_hash": "0x..."
  }
}
```

### 支付历史
```bash
curl "http://localhost:8080/api/v1/pay/history?did=did:solana:...&page=1&page_size=10"
```
**预期返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [...],
    "total": 100,
    "page": 1,
    "page_size": 10
  }
}
```

---

## 4. 查询服务

### 余额查询
```bash
curl "http://localhost:8080/api/v1/balance?did=did:solana:0ec8c962-b3c0-45a2-b674-18da2dba75e6&currency=USDC"
```
**预期返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "did": "did:solana:...",
    "balances": [
      {"currency": "USDC", "amount": "100.00"},
      {"currency": "USDT", "amount": "50.00"}
    ]
  }
}
```

### 交易列表
```bash
curl "http://localhost:8080/api/v1/transactions?did=did:solana:...&page=1&page_size=10"
```
**预期返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "items": [...],
    "total": 100,
    "page": 1,
    "page_size": 10
  }
}
```

### 收入统计
```bash
curl "http://localhost:8080/api/v1/revenue?did=did:solana:...&start_date=2026-03-01&end_date=2026-03-21"
```
**预期返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total_sales": 100,
    "total_revenue": "1000.00",
    "currency": "USDC"
  }
}
```

---

## 5. 验证服务

### 验证单笔交易
```bash
curl "http://localhost:8080/api/v1/verify?tx_hash=0x..."
```
**预期返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "verified": true,
    "tx_hash": "0x...",
    "confirmations": 32
  }
}
```

### 批量验证
```bash
curl -X POST http://localhost:8080/api/v1/verify/batch \
  -H "Content-Type: application/json" \
  -d '{
    "tx_hashes": ["0x...", "0x..."]
  }'
```
**预期返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "results": [
      {"tx_hash": "0x...", "verified": true},
      {"tx_hash": "0x...", "verified": false}
    ]
  }
}
```

### 获取证明
```bash
curl "http://localhost:8080/api/v1/verify/proof?tx_hash=0x..."
```
**预期返回：**
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "tx_hash": "0x...",
    "proof": "...",
    "timestamp": "..."
  }
}
```

---

## 6. 页面接口

### 支付页面
```bash
curl http://localhost:8080/pay
```
**预期返回：** HTML 页面

### 验证页面
```bash
curl http://localhost:8080/verify
```
**预期返回：** HTML 页面

---

## 建议测试流程

1. **健康检查** → 确认网关正常
2. **创建 DID** → 拿到 did
3. **查询 DID** → 验证创建成功
4. **余额查询** → 确认账户状态
5. **创建支付** → 拿到 tx_id
6. **查询支付状态** → 跟踪交易
7. **支付历史** → 查看记录
8. **验证交易** → 确认链上状态

---

## 常见问题

### Q: DID 验证返回 "signature verification failed"
A: DID 验证需要提供钱包签名，创建 DID 后需要用对应私钥签名消息，然后提交 signature 和 message 参数。

### Q: 创建支付返回 "insufficient balance"
A: 账户余额不足，需要先充值或使用测试网 faucet。

### Q: 查询交易返回 "transaction not found"
A: 交易可能还在确认中，或 tx_id 错误，请稍后重试。
