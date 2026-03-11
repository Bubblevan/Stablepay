# 内部 RPC 契约（Kitex/Thrift，一期）

## 1. 适用范围

本文档描述 StablePay DEMO 一期微服务之间的内部 RPC（Kitex/Thrift）契约与调用关系，契约文件位于 `idl/*.thrift`。

内部 RPC 的目标：

- 优先稳定：字段语义清晰、可向后兼容演进
- 降低耦合：服务边界清晰，避免跨库/跨域直接访问
- 与外部 HTTP API 解耦：外部 RESTful 为对外 canonical API；内部 RPC 用于服务间高频调用

## 2. 服务依赖关系（一期最小可用）

### 2.1 必须覆盖的调用

- `payment-service -> did-service`
  - 用途：验证 Agent DID 的签名合法性、必要时解析 DID 信息
- `payment-service -> blockchain-adapter`
  - 用途：发起链上转账、查询交易状态
- `query-service -> blockchain-adapter`
  - 用途：查询链上余额、查询交易状态（如需）
- `api-gateway -> did-service / payment-service / verification-service / query-service`
  - 用途：请求转发与应用级适配（统一响应、trace/request_id、短链接 → RESTful）

### 2.2 事件驱动依赖（非 RPC）

`verification-service <- payment-service` 通过 RocketMQ 消费支付事件（见 `docs/event-contract.md`）。

## 3. RPC 服务清单（一期）

> 说明：一期采用“双层网关”。外层为阿里云 API Gateway；内层为 `api-gateway` 微服务（薄层）。内层网关到下游可走 HTTP 或 Kitex RPC。
>
> **建议**：服务间（含内层网关到下游）优先使用 Kitex RPC；如一期工程上先走 HTTP，也应保持 DTO 字段命名与本仓库一致，避免后续迁移成本。

### 3.1 did-service（`idl/did-service.thrift`）

核心方法（最小集）：

- `VerifySignature`：验证 `did` 对 `message` 的签名
- `GetDID`：查询 DID 基础信息（public_key、wallet_address）

### 3.2 blockchain-adapter（`idl/blockchain-adapter.thrift`）

核心方法（最小集）：

- `TransferStableCoin`：执行稳定币转账（amount 使用最小单位整数，6 decimals）
- `GetBalance`：查询钱包余额（最小单位整数）
- `GetTxStatus`：查询交易状态

### 3.3 payment-service（`idl/payment-service.thrift`）

核心方法（最小集）：

- `InitiatePayment`：创建支付并协调链上执行
- `GetPaymentStatus`：查询支付状态
- `ListPaymentHistory`：支付历史（一期最小字段集）

### 3.4 verification-service（`idl/verification-service.thrift`）

核心方法（最小集）：

- `VerifyPurchase`：校验购买关系（是否已购买、购买时间等）
- `BatchVerifyPurchase`：批量验证
- `GetPurchaseProof`：购买证明（一期最小字段集，允许扩展）

### 3.5 query-service（`idl/query-service.thrift`）

核心方法（最小集）：

- `GetBalanceSummary`：余额与消费统计
- `ListTransactions`：交易记录列表
- `GetRevenueSummary`：收益统计

## 4. 通用约定

### 4.1 错误码

内部 RPC 使用与外部一致的业务错误码（见 `idl/common.thrift` 与 `docs/external-api-contract.md`）。

### 4.2 金额与精度

- RPC 内部统一使用最小单位整数 `amount_minor`（一期按 6 decimals）
- RPC 层不接收/传递浮点

### 4.3 幂等

- 支付链路的关键写操作（尤其是 `InitiatePayment` 与链上转账）应支持幂等
- RPC 请求可携带 `idempotency_key`（建议使用 `tx_id` 或业务侧生成的幂等键）

