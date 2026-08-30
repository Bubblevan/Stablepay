# ADR-001：冻结 blockchain-adapter RPC v1

> 状态：Accepted  
> 日期：2026-08-30  
> 范围：`blockchain-adapter` 与所有调用方

## 背景

审计发现当前 canonical IDL 只有 3 个方法：`TransferStableCoin`、`GetBalance`、`GetTxStatus`；但 `blockchain-adapter/kitex_gen` 和活动 RPC 适配层已经实现了另外 2 个方法：`BuildUnsignedTransaction`、`SubmitSignedTransaction`。

如果继续保留 3 方法 IDL，生成链会把活动实现重新覆盖成不完整接口；如果只相信生成代码，又会失去明确的契约来源。两者必须收敛到同一份协议。

## 决策

`BlockchainAdapterService` v1 冻结为以下 5 个 RPC 方法：

| 方法 | 用途 | 幂等/重试规则 |
|---|---|---|
| `TransferStableCoin` | 服务端直接执行稳定币转账 | 只能携带稳定的 `idempotency_key`；不可盲目重试 |
| `GetBalance` | 查询钱包余额 | 可在超时后有限重试 |
| `GetTxStatus` | 查询链上交易状态 | 可重试；用于提交前后的状态确认 |
| `BuildUnsignedTransaction` | 构造客户端待签名交易 | 同一 `tx_id` 必须返回一致的业务结果或明确失效 |
| `SubmitSignedTransaction` | 提交客户端签名交易 | 先按 `tx_id`/交易 hash 查询，禁止网络超时后直接再次提交 |

当前工作区已完成重复 IDL 清理，唯一保留的契约源是：

- `stablepayai-idl/idl/blockchain-adapter.thrift`：唯一来源。

`blockchain-adapter/idl/` 和 `api-gateway/stablepayai-idl/idl/` 已清理。后续服务生成脚本必须直接引用 canonical IDL，不得重新创建副本。

## 影响

- 当前 `kitex_gen` 的 5 方法接口与 canonical IDL 对齐，不需要为了恢复一致性删除活动实现。
- `payment-service` 当前使用 `TransferStableCoin`、`GetBalance` 和 `GetTxStatus`；两阶段签名方法先作为稳定契约保留，后续由支付流程明确选用。
- `TransferStableCoin` 与两阶段签名流程不能在同一个支付尝试中混用，否则可能造成重复提交或状态无法解释。
- 任何新增链能力必须新增兼容方法或新版本 namespace，不得重用既有 field id 或改变既有方法语义。

## 验收

- `stablepayai-idl/scripts/verify-contract.ps1` 通过。
- 生成工具重新生成后，`blockchain-adapter` 的 service interface 仍包含且只包含这 5 个方法。
- 生成物、RPC 活动实现和调用方在一次完整构建中通过。
