# Resume Evidence Inventory

This inventory is the source-backed boundary for resume and interview claims after the StablePay freeze.

- Frozen repository HEAD: `ae67ae5e48a76848b5c0dfc4a68f79eef00a5705`
- Audit base: `ac2dc5144ea2b78f4dd832017f4d4f70a2e1622c`
- Evidence labels: `CODE_AUDITED`, `UNIT_TEST`, `LOCAL_INTEGRATION`, `LIVE_LOCAL`, `REAL_LLM`, `REAL_DEVNET`, `REPLAY`.
- The ignored `.local-run` files are local evidence outputs. They are not treated as committed fixtures.

## Claim inventory

| Claim | Resume-safe wording | Metric | Evidence type | Exact file / test / script | Commit | Limitation | Interview follow-up |
|---|---|---:|---|---|---|---|---|
| Agent-facing commerce runtime | Built a stateful Agent Commerce Runtime that accepts `AcquireCapabilityRequest` and drives persisted execution to a terminal artifact. | qualitative | CODE_AUDITED, UNIT_TEST | `commerce-runtime/cmd/server/main.go`; `internal/composition.NewProduction`; `internal/runtime.Runner.RunEpisode`; `internal/api` tests | `08e09b9`; freeze `ae67ae5` | Not a production-traffic claim. | Why is a Runtime needed above the payment services? |
| External surface | Exposed authenticated HTTP, MCP, and CLI ingress with status, events, parent decisions, idempotency, health, and readiness. | 3 ingress modes | UNIT_TEST, LOCAL_INTEGRATION | `internal/api/server.go`; `cmd/stablepay-runtime`; `cmd/external-agent-e2e`; `internal/api/server_test.go` | `08e09b9`; freeze `ae67ae5` | MCP is ingress only and shares the Runtime/application boundary. | How do you prove MCP cannot bypass the guard? |
| Payment reliability mechanism | Bound each economic attempt to a `PaymentIntent`, ledger projection, idempotency key, request fingerprint, and reconciliation path. | duplicate settlements observed: 0 in live-local trials | CODE_AUDITED, UNIT_TEST, LIVE_LOCAL | `internal/application/payment_runtime.go`: `ReservePaymentIntent`, `AuthorizeAndSubmitPayment`, `ReconcilePayment`; `.local-run/s11-live-local/metrics.json` | current tree; live-local recipe `cea47a6`; freeze `ae67ae5` | The zero is observed in this evaluated set, not a mathematical production guarantee. | What happens if the process dies after submit and before the response? |
| Crash/restart recovery | Resumes Episodes and Workflow runs from persisted authoritative state rather than from an in-memory step counter. | covered by restart tests | CODE_AUDITED, UNIT_TEST, LOCAL_INTEGRATION | `internal/runtime/runner.go`: `ResumeEpisode`, `ResumePersisted`; `internal/workflowruntime/runner.go`: `ResumeWorkflow`; API/workflow restart tests | `08e09b9`, `ac2dc51`; freeze `ae67ae5` | Operational execution status is not business authority. | Which state is authoritative after a crash window? |
| LLM safety boundary | Treats an LLM output as a decision proposal; `RuntimeGuard` validates state, target, evidence, budget, and memory references before deterministic execution. | guard-rejected safety group present; unsafe side effects observed: 0 | CODE_AUDITED, UNIT_TEST, LIVE_LOCAL | `internal/decision/decision.go`: `RuntimeGuard.ValidateRecoveryProposalWithMemory`; `internal/application/s6_runtime.go`; decision trace tests | `8a7bf7c`; freeze `ae67ae5` | F0 real DeepSeek: `NOT RUN`; local adversarial provider is not DeepSeek. | Why is `LLM != transaction authority`? |
| Memory provenance | Projects event/payment/delivery outcomes into scoped, expiring memory and records use/citation traces without granting payment authority. | memory-on/off and hit/miss task groups are isolated | CODE_AUDITED, UNIT_TEST, LOCAL_INTEGRATION | `internal/memory/projector.go`: `ProjectEpisode`; `internal/memory/use_trace.go`; S7 boundary/MySQL tests | `66ec12c`; freeze `ae67ae5` | No causal accuracy or universal recommendation claim. | What can Memory change, and what can it never authorize? |
| Workflow artifact data plane | Implements a static sequential workflow whose child Episodes produce hash/type/provenance-checked artifact references. | workflow/local restart and artifact boundary tests | UNIT_TEST, LOCAL_INTEGRATION | `internal/workflowruntime/runner.go`; `internal/workflow/artifact/resolver.go`; workflow E2E tests | `ef6dc0d`, `fcd1954`, `ac2dc51`; freeze `ae67ae5` | No dynamic planner, compensation, or parallel execution. | How does a parent workflow consume a child artifact safely? |
| Reliability harness | Produced externally shaped JSONL traces and mode-qualified metrics without calling internal `application.Service` directly. | 56 submitted / 56 valid / 0 collection errors | LIVE_LOCAL, REPLAY | `cmd/stablepay-agent-eval`; `cmd/s11-live-local-suite`; `scripts/test-s11-live-local.ps1`; `.local-run/s11-live-local/{episode_results,grade_results,metrics,report}` | `cea47a6`; freeze `ae67ae5` | `live_local` uses deterministic local adapters, not real network latency. | How are configured, triggered, recovered, and operationally retried faults separated? |
| Fault recovery | In 48 actually triggered fault trials, 40 recovered under the deterministic live-local setup. | `40/48 = 83.33%` | LIVE_LOCAL | `.local-run/s11-live-local/metrics.json`; `report.md`; `observability.Compute` | `cea47a6`; freeze `ae67ae5` | This is not a production failure-recovery rate. | Why is the denominator triggered faults rather than configured faults? |
| Safety observation | Across the evaluated live-local trials, no unauthorized side effect or duplicate settlement was observed. | `0` / `0` across evaluated trials | LIVE_LOCAL | `.local-run/s11-live-local/metrics.json`; `GradeEpisode`; `DecisionOutcomeTrace` | `cea47a6`; freeze `ae67ae5` | Observed result, not a universal safety proof. | How did the harness observe side effects before and after a rejected proposal? |
| Prior real payment path | Validated a real Solana Devnet settlement/verification path in prior U1 acceptance. | qualitative | REAL_DEVNET | `scripts/test-unified-business-e2e.ps1 -Mode U1`; recorded U1 artifacts | `9086871`, `b62e062`, `1fccccf` | Not rerun by F0; never call it “56 trials on Devnet.” | Which artifact proves the network and transaction identity? |
| Prior real recovery provider | Validated a real DeepSeek recovery integration in prior U2 acceptance. | qualitative | REAL_LLM | `scripts/test-unified-business-e2e.ps1 -Mode U2`; recorded U2 artifacts | `b423752` | F0 status is `NOT RUN`; require a persisted non-rule `ModelDecisionTrace` for a fresh claim. | What distinguishes a real LLM trace from a rule fallback? |

## Quantitative claim policy

The recommended resume headline uses at most two quantitative statements: the observed safety pair (`0` unauthorized side effects / `0` duplicate settlements) and the fault-trial recovery wording (`40/48` in deterministic live-local). The overall `48/56` is deliberately not a headline because the 56 include the expected `guard-rejected` safety-negative group.

The live-local latency pair `52.25 / 152.62 ms` is appendix-only, rounded from the recorded metrics. It describes deterministic local adapters and is not Solana, Payment Service, DeepSeek, or Internet latency.

## Claims intentionally excluded

- No millions-of-transactions, QPS, 99.99% availability, production-scale, or zero-failure claim.
- No claim that every happy path calls DeepSeek; a happy path can complete without `ModelDecisionTrace`.
- No claim that the live-local suite exercised real Payment Service or Devnet.
- No claim of RL, GRPO, post-training, self-evolution, dynamic planning, or parallel workflow execution.
