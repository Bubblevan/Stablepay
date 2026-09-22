# StablePay Resume Variants

These variants describe the frozen repository only. They are positioning choices, not new product claims.

## Candidate project title

Recommended:

> **StablePay — Agent Commerce Runtime**

Subtitle:

> 面向 AI Agent 付费能力调用的受控执行、恢复与支付系统

Alternative titles:

1. **StablePay — Controlled Agent Commerce Runtime**
2. **StablePay — Agent Payment Execution and Recovery Runtime**

Avoid “AI payment platform”, “intelligent payment Agent”, and “LLM payment system”; they overstate the Runtime’s authority and blur the lower payment plane.

## One-line project description

> 面向 AI Agent 的 Agent Commerce Runtime：以 x402 与 Solana 支付平面承载受控执行、持久化恢复与可验证交付。

## Recommended project bullets

- 设计并落地 StablePay Agent Commerce Runtime，以持久化 Episode 状态机连接 HTTP/MCP/CLI 与 x402/Solana 支付平面；外部 Agent 只提交 `AcquireCapabilityRequest`，Runtime 负责编排执行。
- 围绕 `PaymentIntent`、`Ledger` 与幂等键实现 reserve/submit/reconcile 受控流程，结合 CAS、启动扫描与 exact redelivery 处理 crash/restart 窗口，避免重试被误当成新结算。
- 将 LLM 限定为带证据的 decision proposal，所有 recovery action 经过 `RuntimeGuard`、`MemoryUseTrace` 与确定性执行层；Memory 只提供 provenance-scoped advisory context，不拥有交易权限。
- 在 deterministic live-local 环境完成 56 次隔离试验，观察到 0 unauthorized side effects、0 duplicate settlements；48 次实际触发的 fault trials 中恢复 40 次，结果可由 JSONL trace 重算。

The final bullet is an evidence statement, not a production reliability guarantee. Do not headline `48/56`; the 56 include the expected `guard-rejected` safety-negative group.

## Backend-oriented version

Use when the role emphasizes Go services, persistence, and distributed reliability. Agent-runtime content is intentionally kept to roughly one third.

- 使用 Go 与 CloudWeGo Kitex/Thrift 组织 StablePay 六服务支付平面，并在 Commerce Runtime 中以 MySQL 持久化 Episode、PaymentIntent、Ledger 与执行状态。
- 通过幂等键、economic identity、CAS transition、reconciliation 与 RocketMQ/MySQL service boundaries 处理重复请求、未知支付结果和 crash/restart 窗口。
- 保留 x402 quote、DID authorization、Solana adapter、verification 与 entitlement 的明确边界，Runtime 只编排已有 payment plane，不复制支付事实。

## Agent Runtime / Harness-oriented version

Use when the role emphasizes agent systems, safety boundaries, observability, and evaluation.

- 构建面向外部 Agent 的 stateful harness，通过 HTTP/MCP/CLI 接收能力请求，以 persistent Episode runner 驱动 Merchant、Payment、Verification 与最终 artifact。
- 将 LLM 限定在 decision proposal boundary，使用 `RuntimeGuard`、evidence refs、Memory provenance 和 durable parent approval 阻断未经授权的 side effect。
- 设计 mode-qualified eval/export pipeline，区分 replay、live-local、REAL_LLM 与 REAL_DEVNET；确定性 live-local 观察到 0 unauthorized side effects、0 duplicate settlements。

## Algorithm / Agent-oriented version

Use when the role emphasizes decision boundaries and evaluation, without pretending StablePay is a model-training project.

- 设计 State → Observation → Decision Proposal → RuntimeGuard → Deterministic Execution Plane 的 Agent decision boundary。
- 用 evidence/RAG-style scoped context、MemoryUseTrace、parent approval 与 recovery provider 对比分析模型建议如何进入受控执行。
- 通过外部 eval harness、fault injection、JSONL trace 和 pass^k grouping 评估 proposal acceptance、guard rejection、recovery 与 side-effect safety；项目不包含 model training、RL 或 GRPO。

## Short answer to “what did you actually build?”

> 我做的是一个把 Agent 的购买意图接到受控支付执行面的 Runtime：模型可以提出恢复建议，但不能直接改支付状态；状态、证据、Guard、幂等和重启恢复才是交易 authority。
