# 对外接口说明

## 统一响应

```json
{
  "code": 0,
  "message": "success",
  "data": {},
  "request_id": "uuid",
  "timestamp": "2026-03-11T12:00:00Z"
}
```

## 鉴权输入兼容

支持两种输入：

- Header：
  - `X-StablePay-DID`
  - `X-StablePay-Signature`
  - `X-StablePay-Timestamp`
  - `X-StablePay-Nonce`
  - `X-API-Key`
- Body 兼容字段：
  - `did`
  - `signature`
  - `timestamp`
  - `nonce`

网关内部统一归一化到上下文：

- `ctx.did`
- `ctx.signature`
- `ctx.timestamp`
- `ctx.nonce`

## 路由列表

- `POST /api/v1/did`
- `POST /api/v1/did/verify`
- `GET /api/v1/did/:did`
- `POST /api/v1/pay`
- `GET /api/v1/pay/:tx_id`
- `GET /api/v1/pay/history`
- `GET /api/v1/verify`
- `POST /api/v1/verify/batch`
- `GET /api/v1/verify/proof`
- `GET /api/v1/balance`
- `GET /api/v1/transactions`
- `GET /api/v1/revenue`
- `GET /pay`
- `GET /verify`
- `GET /healthz`
- `GET /readyz`

## 快捷入口语义

- `/pay`：支付引导入口，返回 `200 JSON`，不做跳转
- `/verify`：兼容入口，网关内部映射到标准验证链路

## v0.1 DID 签名规范

```text
METHOD + "\n" +
PATH + "\n" +
RAW_QUERY + "\n" +
BODY_SHA256 + "\n" +
TIMESTAMP + "\n" +
NONCE
```
