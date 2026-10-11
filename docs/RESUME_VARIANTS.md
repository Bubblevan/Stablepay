# StablePay Resume Variants

These variants describe the current working tree and retained run evidence. They are positioning choices, not new product claims; check the evidence ledger for working-tree-only changes and run qualifiers.

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

- 构建 Agent Commerce Runtime 持久化执行层，以 Episode 状态机编排商户发现、买家确认、x402/Solana 支付、验单交付和恢复；LLM 仅提交带证据的 proposal，支付副作用由 `RuntimeGuard` 与确定性执行路径控制。
- 在 12 家本地模拟 Merchant、24,000 条商品和隔离 MySQL 上打通约束筛选、库存/价格复核、报价预留、二次确认与模拟支付；回放 500 条锁定意图后全部到达终态（483 次模拟结算、17 次模拟拒付），覆盖 50 次价格变化、50 次缺货和 100 次过期确认拒绝，SQL invariant violations 为 0。另以 9 个定向 MySQL/API 场景验证买家许可的同格式替代品、缺货后基于新报价重新确认；不相关替代、不可行预算/区域和澄清路径均未产生购买副作用。该 9 场景窄范围测试未并入 500 条回放，也不是独立商品相关性评测。
- 在隔离 MySQL 的 300 次本地模拟购买回放中注入 200 次提交后响应丢失、100 次 API 重建/重试（含 1 次 checkout OS 进程重启）及 100 组并发重复请求；**287/300** 订单端到端到达模拟 `FULFILLED`，其余 **13/300** 被确定性 mock 支付拒绝。对 29 个预设单次瞬态配送故障，均在同一幂等键下重试恢复；SQL 与离线审计未发现重复订单/支付/结算/履约/退款、金额/货币或终态不一致。固定合成任务池、mock 支付与履约；不是外部支付 sandbox、Devnet 或生产成功率。
- 在隔离 MySQL 的 8 个崩溃窗口完成 160/160 次 Runtime 恢复，审计 PASS，重复支付事实、预算漂移、orphan/stuck episode 均为 0，TTR P50/P95 为 0.621/28.807 s；另在 200 VU 本地 Docker k6 窗口测得 1,319 HTTP req/s、72.6 completed Episodes/s、HTTP p95 114 ms、Episode p95 3.349 s，观测 HTTP/业务错误率 0%。
- 在 6 节点 Minikube（单台 Docker Desktop 主机）部署 12 个 ready Deployments，完成一次真实 0.01 USDC Solana Devnet 商户支付与交付；Runtime 重启后同请求重放未产生第二笔支付，MySQL 与跨服务审计一致。该结果是单次 Devnet 验收，不是生产可用率或重复支付统计。

The local shopping lifecycle and 300-order replay use isolated deterministic mocks; keep that qualifier next to their results. The 500-task product-quality figures `94.2%` and `72.6% → 91.8%` are not established: no independent, double-reviewed holdout has been evaluated. Do not headline `48/56`; the 56 include the expected `guard-rejected` safety-negative group.

### Optional public retrieval-reference result (separate from the StablePay holdout)

> 在 500-query Amazon ESCI US 测试集稀疏 Exact 样本（7,771 个已判 query-product 对；query IDs 与权重开发样本不重叠）上，将 Runtime BM25 与标题覆盖/有序双词重排结合；相较 BM25，Recall@5 macro 为 86.96%→88.51%、micro 为 85.35%→87.19%，NDCG@5 为 0.89069→0.90518。

Only use this as an explicitly named **public judged-candidate benchmark**. It does not include unjudged full-catalog negatives or StablePay inventory, region, price, checkout, or payment constraints. Exact-query hit@5 decreased from 98.0% to 97.6%, and the K=20 result is saturated; do not generalize the metric beyond this sample/cutoff. Artifact: `.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T114737.061945700Z/metrics.json`.

## Outcome-led project section

> **StablePay — Agent Commerce Runtime | Go、CloudWeGo（Hertz/Kitex/Thrift）、MCP、MySQL、Redis、RocketMQ、Docker/k6、Kubernetes、x402、Solana**
>
> **项目背景：** 面向 Agent 购买数字服务时的支付授权、重复副作用与中断恢复问题，构建从商户/商品发现、约束校验、买家确认到支付、交付核验和故障恢复的可审计交易执行链路。
>
> - **Agent 商品与交易闭环：** 接入 12 家本地模拟商户和 24,000 条商品，基于区域、预算、属性、库存/报价复核与买家确认约束生成购买流程；回放 500 条锁定意图后全部到达终态（483 次模拟结算、17 次模拟拒付），覆盖 50 次价格变化、50 次缺货、100 次过期确认拒绝，SQL 不变量违规为 0。另有独立 9 个 MySQL/API 场景验证仅接受买家许可的同格式替代品、缺货后刷新报价并重新确认；不相关替代、不可行预算/区域及澄清路径的购买副作用为 0。该 9 场景不并入 500 条回放；500 条回放未发起新模型调用，二者都不代表独立商品相关性评测。
> - **幂等交易与瞬态故障恢复：** 在隔离 MySQL 的 300 次本地模拟购买回放中注入 200 次提交后响应丢失、100 次 API 重建/重试（含 1 次 checkout OS 进程重启）和 100 组并发重复请求；287/300 订单端到端到达模拟 `FULFILLED`，13/300 被 mock 支付拒绝；29/29 预设单次瞬态配送故障在同一幂等键下恢复。SQL 与离线审计未发现重复订单、支付、结算、履约、退款或金额/状态不一致。固定合成任务池和确定性本地 mock，不代表外部支付 sandbox、Devnet 或生产成功率。
> - **支付与交付故障恢复：** 在独立 AgentPay live-local 集上，对三个固定故障画像各重复 25 次，75/75 触发故障的试验通过终态 grader；覆盖每次两次注入的商家瞬时失败、支付后无效交付，以及 mock 交易已提交但 submit 响应丢失后由 `STATUS_QUERY` 对账。每次仅 1 个 PaymentIntent/结算，重复结算和 payment resubmit 均为 0；其中 25/25 无效交付进入业务恢复。仅 deterministic mock，不是 75 个自然语言任务、外部支付 provider 或真实支付结果。
> - **受控执行与恢复：** 将 LLM 限定为 decision proposal，经 `RuntimeGuard` 和确定性执行路径控制副作用；Guard 测试拒绝 150/150 个不安全提案、接受 150/150 个有效提案，0 false accept/reject、0/40 side-effect escape；另有 160 次本地 Runtime crash/restart 试验跨 8 个持久化窗口全部恢复并通过 MySQL audit，重复 PaymentIntent/settlement、预算漂移、orphan/stuck Episode 均为 0，TTR P50/P95 为 0.621/28.807 s。
> - **云原生验收与性能：** 在单台 Docker Desktop 上运行的 6 节点 Minikube 部署 6 个 StablePay 服务及 Merchant、Runtime、MySQL、Redis、RocketMQ（共 12 个 ready Deployments），完成一次真实 0.01 USDC Solana Devnet 支付、验证与交付，Runtime 重启重放未新增支付；另以 Docker k6 在本地单机 200 VU 测得 1,319 HTTP req/s、72.6 completed Episodes/s、HTTP p95 114 ms、Episode p95 3.349 s，60 秒窗口观测 HTTP/业务错误率 0%。性能测试使用确定性本地依赖，不代表生产容量。

## Internship Kubernetes deployment (separate from the personal-project evidence)

The user confirms they deployed services to Alibaba Cloud ACK and used
Kubernetes during the internship. Keep that experience under the internship
entry rather than blending it into the StablePay local Minikube benchmark. A
conservative resume line, pending the exact service scope and ownership, is:

> 实习期间将 StablePay 微服务部署至阿里云 ACK，使用 Kubernetes 管理云端工作负载。

Do not attach the personal project's six-node topology, Devnet transaction,
or local benchmark numbers to this internship line. Add rollout/CI/CD,
cluster-size, traffic, or availability claims only if the user's internship
records independently support them.

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
