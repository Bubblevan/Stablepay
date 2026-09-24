# Resume Evidence Inventory

This inventory records evidence-backed resume and interview claims from the current repository and its local run artifacts. The freeze SHA below is a historical baseline, not the current code state.

- Historical frozen repository HEAD: `ae67ae5e48a76848b5c0dfc4a68f79eef00a5705`
- Audit base: `ac2dc5144ea2b78f4dd832017f4d4f70a2e1622c`
- Evidence labels: `CODE_AUDITED`, `UNIT_TEST`, `LOCAL_INTEGRATION`, `LIVE_LOCAL`, `E3_LOCAL`, `K6`, `MYSQL`, `REAL_LLM`, `REAL_DEVNET`, `REPLAY`.
- The ignored `.local-run` files are local evidence outputs. They are not treated as committed fixtures.

## Claim inventory

| Claim | Resume-safe wording | Metric | Evidence type | Exact file / test / script | Commit | Limitation | Interview follow-up |
|---|---|---:|---|---|---|---|---|
| Agent-facing commerce runtime | Built a stateful Agent Commerce Runtime that accepts `AcquireCapabilityRequest` and drives persisted execution to a terminal artifact. | qualitative | CODE_AUDITED, UNIT_TEST | `commerce-runtime/cmd/server/main.go`; `internal/composition.NewProduction`; `internal/runtime.Runner.RunEpisode`; `internal/api` tests | `08e09b9`; freeze `ae67ae5` | Not a production-traffic claim. | Why is a Runtime needed above the payment services? |
| External surface | Exposed authenticated HTTP, MCP, and CLI ingress with status, events, parent decisions, idempotency, health, and readiness. | 3 ingress modes | UNIT_TEST, LOCAL_INTEGRATION | `internal/api/server.go`; `cmd/stablepay-runtime`; `cmd/external-agent-e2e`; `internal/api/server_test.go` | `08e09b9`; freeze `ae67ae5` | MCP is ingress only and shares the Runtime/application boundary. | How do you prove MCP cannot bypass the guard? |
| Payment reliability mechanism | Bound each economic attempt to a `PaymentIntent`, ledger projection, idempotency key, request fingerprint, and reconciliation path. | duplicate settlements observed: 0 in live-local trials | CODE_AUDITED, UNIT_TEST, LIVE_LOCAL | `internal/application/payment_runtime.go`: `ReservePaymentIntent`, `AuthorizeAndSubmitPayment`, `ReconcilePayment`; `.local-run/s11-live-local/metrics.json` | current tree; live-local recipe `cea47a6`; freeze `ae67ae5` | The zero is observed in this evaluated set, not a mathematical production guarantee. | What happens if the process dies after submit and before the response? |
| Crash/restart recovery | Resumes Episodes and Workflow runs from persisted authoritative state rather than from an in-memory step counter. | covered by restart tests | CODE_AUDITED, UNIT_TEST, LOCAL_INTEGRATION | `internal/runtime/runner.go`: `ResumeEpisode`, `ResumePersisted`; `internal/workflowruntime/runner.go`: `ResumeWorkflow`; API/workflow restart tests | `08e09b9`, `ac2dc51`; freeze `ae67ae5` | Operational execution status is not business authority. | Which state is authoritative after a crash window? |
| LLM safety boundary | Treats an LLM output as a decision proposal; `RuntimeGuard` validates state, target, evidence, budget, and memory references before deterministic execution. | guard-rejected safety group present; unsafe side effects observed: 0 | CODE_AUDITED, UNIT_TEST, LIVE_LOCAL | `internal/decision/decision.go`: `RuntimeGuard.ValidateRecoveryProposalWithMemory`; `internal/application/s6_runtime.go`; decision trace tests | `8a7bf7c`; freeze `ae67ae5` | E1 ran real DeepSeek but underperformed Rule; it is interview-only negative ablation and excluded from resume claims. | Why is `LLM != transaction authority`? |
| Memory provenance | Projects event/payment/delivery outcomes into scoped, expiring memory and records use/citation traces without granting payment authority. | memory-on/off and hit/miss task groups are isolated | CODE_AUDITED, UNIT_TEST, LOCAL_INTEGRATION | `internal/memory/projector.go`: `ProjectEpisode`; `internal/memory/use_trace.go`; S7 boundary/MySQL tests | `66ec12c`; freeze `ae67ae5` | No causal accuracy or universal recommendation claim. | What can Memory change, and what can it never authorize? |
| Workflow artifact data plane | Implements a static sequential workflow whose child Episodes produce hash/type/provenance-checked artifact references. | workflow/local restart and artifact boundary tests | UNIT_TEST, LOCAL_INTEGRATION | `internal/workflowruntime/runner.go`; `internal/workflow/artifact/resolver.go`; workflow E2E tests | `ef6dc0d`, `fcd1954`, `ac2dc51`; freeze `ae67ae5` | No dynamic planner, compensation, or parallel execution. | How does a parent workflow consume a child artifact safely? |
| Reliability harness | Produced externally shaped JSONL traces and mode-qualified metrics without calling internal `application.Service` directly. | 56 submitted / 56 valid / 0 collection errors | LIVE_LOCAL, REPLAY | `cmd/stablepay-agent-eval`; `cmd/s11-live-local-suite`; `scripts/test-s11-live-local.ps1`; `.local-run/s11-live-local/{episode_results,grade_results,metrics,report}` | `cea47a6`; freeze `ae67ae5` | `live_local` uses deterministic local adapters, not real network latency. | How are configured, triggered, recovered, and operationally retried faults separated? |
| Runtime load benchmark | Load-tested the authenticated Runtime HTTP boundary at 1/10/25/50/100/200 VUs; at 200 VUs measured 1,319.133 HTTP req/s and 72.6 completed Episodes/s. | At 200 VUs: HTTP p95 `114.393 ms`, Episode p95 `3.349 s`, `0%` HTTP/business errors in the 60s measurement window; all six tiers passed | E3_LOCAL, K6, MYSQL | `scripts/benchmark-e3-load.ps1`; `.local-run/resume-benchmark/e3-steady-state-final/{report.md,metrics.json,manifest.json}` | `c098568`, `5ea06b5` | One local Windows host; Docker `grafana/k6` bridge network; MySQL and deterministic local dependencies. Not production-scale or Internet/payment-provider latency. | What did the 200-VU test measure, how was warmup excluded, and what limited throughput? |
| RuntimeGuard proposal boundary | Exercised positive and negative proposals at the live-local `CommitProposal` boundary. | 150/150 unsafe proposals rejected; 150/150 valid proposals accepted; 0 false accepts, 0 false rejects, and 0 side-effect escapes across 40 integrity trials | E2_LOCAL, UNIT_TEST | `.local-run/resume-benchmark/e2-guard/{metrics.json,results.jsonl,report.md}` | local E2 evidence; RuntimeGuard source audit | Bounded deterministic local dataset, not production traffic or a universal safety proof. | How did the positive/negative dataset map to the Guard contract, and how were side effects observed? |
| Crash/restart economic correctness | Killed and restarted the local Runtime at eight persisted workflow windows against an isolated MySQL schema and benchmark-only deterministic adapters. | 160/160 Episodes resumed and completed; audit PASS 160/160; duplicate PaymentIntent rows, duplicate `PAYMENT_SETTLED` rows, count mismatches, budget drift, orphaned Episodes, and stuck Episodes all `0`; TTR P50/P95 `0.621/28.807 s` | E4_LOCAL, MYSQL, REPLAY | `.local-run/resume-benchmark/e4-local-20260923T165522Z-8087/full/{metrics.json,audit-summary.json,manifest.json,trials.jsonl}` | local E4 evidence; `FROZEN_RUNTIME_CHANGED=NO` | Benchmark-only local mock merchant/payment/verification adapters; not a real payment-network fault test. | Which MySQL invariants were audited after each crash, and which crash windows are represented? |
| Fault recovery | In 48 actually triggered fault trials, 40 recovered under the deterministic live-local setup. | `40/48 = 83.33%` | LIVE_LOCAL | `.local-run/s11-live-local/metrics.json`; `report.md`; `observability.Compute` | `cea47a6`; freeze `ae67ae5` | This is not a production failure-recovery rate. | Why is the denominator triggered faults rather than configured faults? |
| Safety observation | Across the evaluated live-local trials, no unauthorized side effect or duplicate settlement was observed. | `0` / `0` across evaluated trials | LIVE_LOCAL | `.local-run/s11-live-local/metrics.json`; `GradeEpisode`; `DecisionOutcomeTrace` | `cea47a6`; freeze `ae67ae5` | Observed result, not a universal safety proof. | How did the harness observe side effects before and after a rejected proposal? |
| Prior real payment path | Validated a real Solana Devnet settlement/verification path in prior U1 acceptance. | qualitative | REAL_DEVNET | `scripts/test-unified-business-e2e.ps1 -Mode U1`; recorded U1 artifacts | `9086871`, `b62e062`, `1fccccf` | Not rerun by F0; never call it “56 trials on Devnet.” | Which artifact proves the network and transaction identity? |
| Prior real recovery provider | Validated a real DeepSeek recovery integration in prior U2 acceptance. | qualitative | REAL_LLM | `scripts/test-unified-business-e2e.ps1 -Mode U2`; recorded U2 artifacts | `b423752` | F0 status is `NOT RUN`; require a persisted non-rule `ModelDecisionTrace` for a fresh claim. | What distinguishes a real LLM trace from a rule fallback? |

## Quantitative claim policy

For a performance-focused resume, a safe quantified line is: “Load-tested a stateful commerce Runtime to 200 VUs, sustaining 1,319 HTTP req/s and 72.6 completed Episodes/s at 0% observed HTTP/business errors (60s measurement; local deterministic dependencies).” Keep the local scope adjacent to the numbers. A reliability-focused alternative is the observed safety pair (`0` unauthorized side effects / `0` duplicate settlements) plus `40/48` fault-trial recovery in deterministic live-local. The overall `48/56` is deliberately not a headline because the 56 include the expected `guard-rejected` safety-negative group.

The live-local latency pair `52.25 / 152.62 ms` is appendix-only, rounded from the recorded metrics. It describes deterministic local adapters and is not Solana, Payment Service, DeepSeek, or Internet latency.

The read-only evidence summarizer requires an explicit audited E4 selection, for example `python scripts/summarize-benchmark-evidence.py --e4-run .local-run/resume-benchmark/e4-local-20260923T165522Z-8087` (or pass its `full` subdirectory). It does not choose an arbitrary latest directory.

## B0.1 Resume-ready STAR drafts

These drafts keep each number next to its workload and environment. E1 is deliberately excluded from all resume claims.

### StablePay Agent Commerce Runtime — Go, MySQL, Docker k6

- **Situation:** Needed to characterize the authenticated Runtime API under increasing concurrency while distinguishing HTTP request throughput from completed commerce Episodes.
- **Task:** Measure a six-tier 1/10/25/50/100/200-VU matrix with a 15-second warmup and 60-second measurement per tier.
- **Action:** Ran Dockerized `grafana/k6` against the local Runtime/MySQL setup and recorded both request and business-completion metrics.
- **Result:** At 200 VUs, measured **1,319.133 HTTP req/s**, **72.6 completed Episodes/s**, **114.393 ms HTTP p95**, and **3.349 s Episode p95**, with **0% observed HTTP/business errors**; all six tiers passed.

### StablePay RuntimeGuard — Go, deterministic decision boundary

- **Situation:** Recovery proposals must not authorize unsafe actions or bypass the Runtime's deterministic guard.
- **Task:** Evaluate 150 valid and 150 unsafe proposals, plus 40 side-effect integrity checks, at the live-local `CommitProposal` boundary.
- **Action:** Ran the frozen E2 positive/negative dataset through the proposal and guard boundary and audited acceptance, false decisions, and side-effect escapes.
- **Result:** Rejected **150/150 unsafe** proposals and accepted **150/150 valid** proposals, with **0 false accepts**, **0 false rejects**, and **0/40 side-effect escapes**.

### StablePay crash recovery — Go Runtime, MySQL, deterministic local adapters

- **Situation:** A process crash across a persisted commerce workflow can otherwise leave an Episode stuck or repeat an economic side effect.
- **Task:** Verify restart behavior across eight crash windows and reconcile persisted payment and budget invariants.
- **Action:** Ran 20 trials per window with benchmark-only deterministic merchant/payment/verification adapters and an isolated MySQL schema; audited PaymentIntent, `PAYMENT_SETTLED`, budget projections, orphan, and stuck states after recovery.
- **Result:** **160/160** Episodes resumed and completed; audit **PASS 160/160**; duplicate intents/settlements, count mismatches, budget drift, orphaned and stuck Episodes were all **0**. TTR P50/P95 was **0.621/28.807 s**. `FROZEN_RUNTIME_CHANGED=NO`.

## Claims intentionally excluded

- No millions-of-transactions, unqualified QPS, 99.99% availability, production-scale, or universal zero-failure claim. The E3 figure is a bounded local benchmark only.
- No claim that every happy path calls DeepSeek; a happy path can complete without `ModelDecisionTrace`.
- E1 is classified `INTERVIEW_ONLY_NEGATIVE_ABLATION`, not a resume claim: on 360 real DeepSeek trials, recovery success was `58.33%` versus `100%` for Rule; retain only to discuss why the model treatment underperformed the deterministic control. Unsafe proposals remained `0%`; one proposal failed schema validation.
- No claim that the live-local suite exercised real Payment Service or Devnet.
- No claim of RL, GRPO, post-training, self-evolution, dynamic planning, or parallel workflow execution.
