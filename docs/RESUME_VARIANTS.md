# StablePay Resume Variants

These variants describe the currently committed repository and retained run evidence. They are positioning choices, not new product claims.

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
- 为 Agent 商业闭环增加 ground-truth task eval：8 个固定画像各运行 25 次，共 200 个隔离 Episode；本地 mock 下选型、artifact hash、付费后校验交付、安全拒绝分别为 100/100，Episode p95 114.3 ms。该结果用于验证 harness 与固定样例，不是模型准确率或生产支付成功率。
- 在 deterministic live-local 环境完成 56 次隔离试验，观察到 0 unauthorized side effects、0 duplicate settlements；48 次实际触发的 fault trials 中恢复 40 次，结果可由 JSONL trace 重算。
- 在六节点 Minikube 部署 12 个 ready Deployments，完成一次 0.01 USDC Solana Devnet 商户支付、验证与交付；Runtime 重启后重放同一 request 未产生第二笔付款，MySQL 与跨服务记录对账一致。六节点运行于一台物理机，且这是单次验收，不代表生产可用率。

The final bullet is an evidence statement, not a production reliability guarantee. Do not headline `48/56`; the 56 include the expected `guard-rejected` safety-negative group.

## Outcome-led project section

> **StablePay — Agent Commerce Runtime | Go, x402, Solana, MySQL, Redis, RocketMQ, Kubernetes**
>
> - 构建面向 Agent 的持久化 Commerce Runtime，以 Episode 状态机串联商户发现、x402 402 challenge、支付、entitlement、交付校验和最终 artifact；Runtime 仅接受受控决策，不把 LLM 输出作为交易 authority。
> - 建立可复算的 Agent commerce task benchmark，以八类固定选择/拒绝画像校验商户与 capability 选择、交付内容哈希、支付后交付和零支付安全拒绝；本地确定性 mock 下 200 次独立 Runtime 试验四项分别为 100/100，0 collection errors，Episode p95 114.3 ms（8 个画像各重复 25 次，不代表 200 个独立 prompts 或真实支付链路）。
> - 在六节点 Minikube 中部署六个 StablePay 服务及 Merchant、Runtime、MySQL、Redis、RocketMQ（共 12 个 ready Deployments），完成一次真实 Solana Devnet `0.01 USDC` 购买；请求经 `402 → payment → verified delivery` 到达 `FULFILLED/COMPLETED`，Runtime 重启后同请求重放未产生第二笔付款，PaymentIntent/settlement 重复查询为 0、预算投影与跨服务交易记录一致。该六节点拓扑运行于单台 Docker Desktop 主机。
> - 对持久化支付/账本状态机做 160 次本地确定性 crash/restart 试验，160/160 恢复并通过 MySQL 审计；另在 200 VU 本地 Docker k6 测试中测得 1,319 HTTP req/s、72.6 completed Episodes/s、HTTP p95 114 ms、Episode p95 3.349 s，观测 HTTP/业务错误率 0%。两项均使用确定性本地依赖，不代表真实链路或生产容量。

实习期间的 ACK/Kubernetes 部署应写在对应“实习经历”下，与 StablePay 的 Minikube/Devnet 证据分开；避免将个人项目测试拓扑和实习生产/云上部署混为一段。

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
