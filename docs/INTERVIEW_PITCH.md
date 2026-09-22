# StablePay Interview Pitch Pack

This script is written for the frozen repository. It uses evidence-qualified wording and keeps F0 `REAL_LLM: NOT RUN` and `REAL_DEVNET: NOT RUN` explicit.

## 30 seconds

> StablePay 解决的是 Agent 想购买能力时，不能把自然语言或模型输出直接交给支付 API 的问题。我做了一个 Agent Commerce Runtime：外部 Agent 只提交 `AcquireCapabilityRequest`，Runtime 持久化 Episode，驱动 Merchant、x402、Payment、Verification 和最终 artifact。核心难点是把 LLM 变成 proposal，而不是 transaction authority，再用 RuntimeGuard、幂等 PaymentIntent 和重启恢复保护确定性执行。在 deterministic live-local 的隔离试验中，观察到 0 unauthorized side effects 和 0 duplicate settlements；这些不是生产规模保证。

## 90 seconds

> 直接把 Agent 接到 payment API 不够，因为 Agent 需要发现能力、处理 402/x402 challenge、选择 Merchant、支付、验证交付，还要在 delivery invalid 或未知支付结果时继续执行。我的做法是加一层 Commerce Runtime。Episode 是业务 authority，`EpisodeExecutionStatus` 只记录运行状态；Runner 从持久化 state resume，PaymentIntent、Ledger 和 reconciliation 负责经济身份与重试边界。
>
> LLM 只接收 DecisionContext 并返回 DecisionProposal。`RuntimeGuard` 检查当前 state、candidate/evidence refs、预算、目标 Merchant、尝试次数和 Memory refs，Guard 通过后才由确定性 application service commit。Memory 只提供 scoped、带 provenance 的 advisory context，parent approval 则以 durable fact 保存。
>
> 我没有把所有数字混成一个成功率。S11 live-local 的 56 个 trial 中包含一个预期的 `guard-rejected` safety-negative 组；因此简历不 headline `48/56`。更安全的表述是：在 48 次实际触发的 fault trials 中恢复 40 次，同时观察到 0 unauthorized side effects 和 0 duplicate settlements。真实 DeepSeek 和真实 Devnet 是独立的 prior acceptance evidence，F0 本轮都标记为 `NOT RUN`。

## 3 minutes

### Problem

Agent 需要“买到一个能力并拿到可验证结果”，但支付系统的事实不能由模型自行决定。一次失败可能发生在 Merchant delivery、Payment submit、verification 或进程 crash 的窗口中。

### Architecture

```text
AcquireCapabilityRequest
  -> HTTP / MCP / CLI
  -> Composition Root
  -> persisted Episode + Runner
  -> Catalog / Merchant / x402 / Payment / Verification
  -> Event + Ledger + Trace + final artifact
```

The invariant is:

```text
State -> Action/Observation -> Decision Proposal -> RuntimeGuard
      -> Deterministic Execution Plane -> Event/Ledger/Trace
```

### Happy path

`CreateEpisode` snapshots the contract. `RunEpisode` moves through discovery, invocation, negotiation, payment, delivery, and validation. `ReservePaymentIntent` records budget reservation and the local intent before the payment adapter is called. `AuthorizeAndSubmitPayment` calls the controlled adapter; `ReconcilePayment` resolves unknown outcomes before another economic attempt. A valid artifact and entitlement proof lead to `FULFILLED`.

### Failure path

An invalid delivery or provider failure produces persisted evidence and enters recovery. `ExecuteRecoveryDecision` builds a bounded context, obtains a proposal from the configured provider, runs it through `RuntimeGuard`, and commits only an allowed action. A parent threshold creates a durable approval request; `RecordParentDecision` can be replayed after restart.

### LLM and evaluation

The decision trace stores provider/model/context/evidence hashes and guard outcome. The eval harness enters through public HTTP/MCP/CLI, polls public observability, and grades JSONL results. It separates configured faults from triggered faults, business recovery from operational retry, functional tasks from the `guard-rejected` safety-negative group, and replay/live-local/real-provider modes.

## 5 minutes

Use the 3-minute pitch, then add the following details when asked.

### Workflow

Workflow v1 is a static, sequential, single-sink data plane. A `WorkflowDefinition` validates step dependencies and input/output types. `Manager.run` creates child Episodes, waits for durable parent state, and `completeStepFromArtifact` verifies reference, hash, content type, size, and provenance before the next step. It does not implement a dynamic planner or parallel payments.

### Memory

`Projector.ProjectEpisode` derives scoped outcome records from episode events, payment intents, deliveries, and validation evidence. Retrieval is bounded by requester/parent/merchant/capability/catalog provenance. `MemoryUseTrace` records retrieved and cited refs plus the proposal and guard result. A stale or incorrect memory entry can influence a proposal context but cannot authorize a payment or bypass state checks.

### Crash recovery

The dangerous window is between a persisted `PAYMENT_SUBMITTING` transition and the transport response. The persisted PaymentIntent and transport trace make the outcome queryable. On restart, the Runner scans runnable persisted Episodes and calls `ResumeEpisode`; the payment path reconciles an unknown intent before submitting another economic attempt. Workflow supervisors use the same persisted-state idea for child Episodes and CAS transitions.

### Trade-offs and limitations

- The authority boundary is stricter and less flexible than letting a planner mutate state directly.
- Deterministic local adapters make eval reproducible but do not represent Internet, Solana, Payment Service, or DeepSeek latency.
- Real payment fault injection is incomplete for a live Kitex Payment Service.
- Workflow v1 is sequential and has no compensation, dynamic branching, parallel execution, or workflow-level memory.
- There is no production QPS/availability claim.

### Evidence handoff

Point to [RESUME_EVIDENCE.md](RESUME_EVIDENCE.md), [ENGINEERING_STORIES.md](ENGINEERING_STORIES.md), and [CODE_WALK.md](CODE_WALK.md) for exact sources. The frozen final review is [FINAL_ARCHITECTURE_REVIEW.md](FINAL_ARCHITECTURE_REVIEW.md).
