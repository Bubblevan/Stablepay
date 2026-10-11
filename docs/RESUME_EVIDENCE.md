# Resume Evidence Inventory

This inventory records evidence-backed resume and interview claims from the current repository and its local run artifacts. The freeze SHA below is a historical baseline, not the current code state.

- Historical frozen repository HEAD: `ae67ae5e48a76848b5c0dfc4a68f79eef00a5705`
- Audit base: `ac2dc5144ea2b78f4dd832017f4d4f70a2e1622c`
- Evidence labels: `CODE_AUDITED`, `UNIT_TEST`, `LOCAL_INTEGRATION`, `LOCAL_MOCK_DIAGNOSTIC`, `LIVE_LOCAL`, `E3_LOCAL`, `K6`, `MYSQL`, `K8S_LOCAL`, `REAL_LLM`, `REAL_DEVNET`, `REPLAY`, `USER_ATTESTED`, `CONFIG_ONLY`.
- The ignored `.local-run` files are local evidence outputs. They are not treated as committed fixtures.

## Claim inventory

| Claim | Resume-safe wording | Metric | Evidence type | Exact file / test / script | Commit | Limitation | Interview follow-up |
|---|---|---:|---|---|---|---|---|
| Agent-facing commerce runtime | Built a stateful Agent Commerce Runtime that accepts `AcquireCapabilityRequest` and drives persisted execution to a terminal artifact. | qualitative | CODE_AUDITED, UNIT_TEST | `commerce-runtime/cmd/server/main.go`; `internal/composition.NewProduction`; `internal/runtime.Runner.RunEpisode`; `internal/api` tests | `08e09b9`; freeze `ae67ae5` | Not a production-traffic claim. | Why is a Runtime needed above the payment services? |
| Controlled Runtime shopping-to-delivery path (interim regression only) | Exercised authenticated Runtime catalog search, exact Merchant/SKU pinning, signed buyer confirmation, deterministic Mock payment/entitlement/delivery, and same-request replay through the formal Runtime payment state machine. | 50/50 templated fixtures reached valid delivery; 50 PaymentIntents + 50 settlement ledger rows; duplicate intents/settlements/tx IDs `0`; payment-settled-to-delivery P95 `0.519 ms`; task E2E P95 `3.726 ms` | LOCAL_MOCK_DIAGNOSTIC, UNIT_TEST | `commerce-runtime/internal/api/m1_fixed_50_mock_e2e_test.go`; `commerce-runtime/internal/application/catalog_search.go`; `.local-run/commerce-runtime/m1-fixed-50/m1-runtime-fixed-50-20261011T145716.589580000Z/{suite.jsonl,trials.jsonl,metrics.json,manifest.json}` | Run used base SHA `295ce802e06e92f18c686d456351862399d80aaa`, `source_modified=true` | 50 templated tasks / three service types; task intent pre-normalized (no Agent/model parse); in-memory repository; direct test-runner execution; no MySQL audit, real Merchant/Payment Service, or chain settlement. Latencies are local diagnostic timings, not production or distributed-system SLOs. Do not use as an independent shopping success-rate claim. | Which path performs natural-language normalization, where exactly is buyer authorization enforced, and what does the zero-duplicate audit include? |
| External surface | Exposed authenticated HTTP, MCP, and CLI ingress with status, events, parent decisions, idempotency, health, and readiness. | 3 ingress modes | UNIT_TEST, LOCAL_INTEGRATION | `internal/api/server.go`; `cmd/stablepay-runtime`; `cmd/external-agent-e2e`; `internal/api/server_test.go` | `08e09b9`; freeze `ae67ae5` | MCP is ingress only and shares the Runtime/application boundary. | How do you prove MCP cannot bypass the guard? |
| Payment reliability mechanism | Bound each economic attempt to a `PaymentIntent`, ledger projection, idempotency key, request fingerprint, and reconciliation path. | duplicate settlements observed: 0 in live-local trials | CODE_AUDITED, UNIT_TEST, LIVE_LOCAL | `internal/application/payment_runtime.go`: `ReservePaymentIntent`, `AuthorizeAndSubmitPayment`, `ReconcilePayment`; `.local-run/s11-live-local/metrics.json` | current tree; live-local recipe `cea47a6`; freeze `ae67ae5` | The zero is observed in this evaluated set, not a mathematical production guarantee. | What happens if the process dies after submit and before the response? |
| Crash/restart recovery | Resumes Episodes and Workflow runs from persisted authoritative state rather than from an in-memory step counter. | covered by restart tests | CODE_AUDITED, UNIT_TEST, LOCAL_INTEGRATION | `internal/runtime/runner.go`: `ResumeEpisode`, `ResumePersisted`; `internal/workflowruntime/runner.go`: `ResumeWorkflow`; API/workflow restart tests | `08e09b9`, `ac2dc51`; freeze `ae67ae5` | Operational execution status is not business authority. | Which state is authoritative after a crash window? |
| LLM safety boundary | Treats an LLM output as a decision proposal; `RuntimeGuard` validates state, target, evidence, budget, and memory references before deterministic execution. | guard-rejected safety group present; unsafe side effects observed: 0 | CODE_AUDITED, UNIT_TEST, LIVE_LOCAL | `internal/decision/decision.go`: `RuntimeGuard.ValidateRecoveryProposalWithMemory`; `internal/application/s6_runtime.go`; decision trace tests | `8a7bf7c`; freeze `ae67ae5` | E1 ran real DeepSeek but underperformed Rule; it is interview-only negative ablation and excluded from resume claims. | Why is `LLM != transaction authority`? |
| Memory provenance | Projects event/payment/delivery outcomes into scoped, expiring memory and records use/citation traces without granting payment authority. | memory-on/off and hit/miss task groups are isolated | CODE_AUDITED, UNIT_TEST, LOCAL_INTEGRATION | `internal/memory/projector.go`: `ProjectEpisode`; `internal/memory/use_trace.go`; S7 boundary/MySQL tests | `66ec12c`; freeze `ae67ae5` | No causal accuracy or universal recommendation claim. | What can Memory change, and what can it never authorize? |
| Workflow artifact data plane | Implements a static sequential workflow whose child Episodes produce hash/type/provenance-checked artifact references. | workflow/local restart and artifact boundary tests | UNIT_TEST, LOCAL_INTEGRATION | `internal/workflowruntime/runner.go`; `internal/workflow/artifact/resolver.go`; workflow E2E tests | `ef6dc0d`, `fcd1954`, `ac2dc51`; freeze `ae67ae5` | No dynamic planner, compensation, or parallel execution. | How does a parent workflow consume a child artifact safely? |
| Reliability harness | Produced externally shaped JSONL traces and mode-qualified metrics without calling internal `application.Service` directly. | 56 submitted / 56 valid / 0 collection errors | LIVE_LOCAL, REPLAY | `cmd/stablepay-agent-eval`; `cmd/s11-live-local-suite`; `scripts/test-s11-live-local.ps1`; `.local-run/s11-live-local/{episode_results,grade_results,metrics,report}` | `cea47a6`; freeze `ae67ae5` | `live_local` uses deterministic local adapters, not real network latency. | How are configured, triggered, recovered, and operationally retried faults separated? |
| Runtime load benchmark | Load-tested the authenticated Runtime HTTP boundary at 1/10/25/50/100/200 VUs; at 200 VUs measured 1,319.133 HTTP req/s and 72.6 completed Episodes/s. | At 200 VUs: HTTP p95 `114.393 ms`, Episode p95 `3.349 s`, `0%` HTTP/business errors in the 60s measurement window; all six tiers passed | E3_LOCAL, K6, MYSQL | `scripts/benchmark-e3-load.ps1`; `.local-run/resume-benchmark/e3-steady-state-final/{report.md,metrics.json,manifest.json}` | `c098568`, `5ea06b5` | One local Windows host; Docker `grafana/k6` bridge network; MySQL and deterministic local dependencies. Not production-scale or Internet/payment-provider latency. | What did the 200-VU test measure, how was warmup excluded, and what limited throughput? |
| Agent commerce task outcome evaluation | Added dataset-declared ground truth for merchant/capability choice, artifact-body hash, paid completion, and safe negative decisions; evaluated through the public HTTP/MCP/CLI boundary. | 8 fixed profiles × 25 independent fresh-Runtime trials = 200 Episodes; 100/100 expected selections, 100/100 artifact hashes, 100/100 paid tasks reached validated delivery, 100/100 expected safe declines ended with zero intents/settlements; 0 collection errors; Episode p95 `114.304 ms` | LIVE_LOCAL, UNIT_TEST | `commerce-runtime/testdata/s11/agentpay-benchmark/{README.md,scenarios.jsonl}`; `scripts/run-agentpay-benchmark.ps1`; `benchmarks/agentpay-live-local-20261010-final/{manifest.json,metrics.json,report.md,episode_results.jsonl}` | source revision `2113186`; manifest records `source_modified=false` and dataset SHA-256 | Eight synthetic task archetypes repeated 25 times, not 200 unique prompts; deterministic in-memory merchant/payment/verification mocks; no real Payment Service, LLM, external network, or Devnet. | How was correctness grounded without an LLM judge, and what exactly was counted in paid delivery versus safe decline? |
| AgentPay fault-injected business recovery (v1) | Added two paid task profiles that inject a transient merchant failure before payment or an invalid first delivery after payment; executed each through the public HTTP/MCP client boundary with a fresh deterministic Runtime per trial. | 50/50 triggered fault trials graded successful; 25/25 merchant transient cases completed after two injected failures; 25/25 invalid-delivery cases entered business recovery and completed; every trial had exactly one PaymentIntent and one settlement; 0 duplicate settlements, payment resubmissions, or collection errors; Episode p95 `153.023 ms` | LIVE_LOCAL, FAULT_INJECTION, UNIT_TEST | `commerce-runtime/testdata/s11/agentpay-benchmark/scenarios-recovery-v1.jsonl`; `commerce-runtime/cmd/s11-live-local-suite`; `commerce-runtime/internal/livelocal/runtime.go`; `scripts/run-agentpay-benchmark.ps1`; `scripts/audit-agentpay-recovery-benchmark.ps1`; `.local-run/agentpay-recovery-v1-provenance-20261011/{manifest.json,metrics.json,report.md,episode_results.jsonl,grade_results.jsonl}` | Source revision `d516b04722b9674dce328b160a7815ae11eedae2`; manifest records `source_modified=true`; dataset SHA-256 `0b7b1227b7ee1e99e1703fb3294bd58d31218b4d5bad3922a1df68b28dcf2027` | Two fixed synthetic task profiles × 25 repetitions; in-memory store and deterministic mock Merchant/Payment/Verification, rule recovery only. The 50 trials are not 50 unique buyer tasks, real payment, LLM quality, or a production reliability estimate. | How do the report's 50/50 fault-recovery denominator and 25/25 business-recovery denominator differ, and what state makes the single-settlement check authoritative? |
| AgentPay fault recovery across merchant, delivery, and payment (v2) | Preserved the v1 dataset and added a benchmark-only committed-payment/submit-response-loss fault; ran all three deterministic fault profiles through fresh live-local Runtime instances and the public HTTP/MCP boundary. | 75/75 trials with an applied fault reached the expected terminal grade; merchant transient 25/25 recovered after two injections; invalid delivery 25/25 entered business recovery; payment response loss 25/25 resolved by `STATUS_QUERY`; each trial had exactly one PaymentIntent and settlement; 0 duplicate settlements, payment resubmissions, or collection errors; Episode p95 `153.120 ms` | LIVE_LOCAL, FAULT_INJECTION, UNIT_TEST | `commerce-runtime/testdata/s11/agentpay-benchmark/scenarios-recovery-v2.jsonl`; `commerce-runtime/internal/livelocal/runtime.go`; `commerce-runtime/internal/livelocal/runtime_test.go`; `scripts/run-agentpay-benchmark.ps1`; `scripts/audit-agentpay-recovery-benchmark.ps1`; `.local-run/agentpay-recovery-v2-final-20261011/{manifest.json,metrics.json,report.md,episode_results.jsonl,grade_results.jsonl}` | Source revision `d516b04722b9674dce328b160a7815ae11eedae2`; manifest records `source_modified=true`; dataset SHA-256 `d60c70efcf1850557b8a0b13680fcacb584e31b620ba26e8c0c7fb28f4ec9950` | Three fixed synthetic fault profiles × 25 repetitions, not 75 unique buyer tasks. In-memory deterministic mock only; “payment committed, response lost” is reconciled by a local mock query, not an external provider, chain, process crash, or production reliability estimate. | Why are fault-trial recovery and business recovery separate denominators, and what evidence proves the payment response-loss case did not resubmit or settle twice? |
| Agent shopping intent extraction and synthetic ranker diagnostic | Parsed 500 templated catalog-derived Chinese prompts with GPT-6 Luna and compared deterministic candidate-plan policies over the synthetic Merchant fixture. Retained as a parser/ranker diagnostic, not independent product-discovery evidence. | Raw-model exact intents `500/500`; generated-plan acceptance B1 `500/500` vs B0 `389/500` (`77.8%`); stored Recall@20 `596/596` is **not an independent retrieval score**: the gold SKU set and B1 candidate list use the same catalog-derived feasibility predicates; zero Hermes tool events | REAL_LLM, LOCAL_SYNTHETIC_EVAL | `commerce-runtime/benchmarks/agentshopping`; `commerce-runtime/cmd/agentshopping-sim`; `scripts/run-agent-shopping-simulation.ps1`; ignored `.local-run/agent-shopping/evaluations/shopping-intents-20261010T135310.164825200Z/{manifest.json,intent-metrics.json,intent-trials.jsonl,model-responses.jsonl,model-streams.jsonl}` | Source code and evidence inventory included together in this commit; task SHA-256 `4f7619a0455dfadfc20922c39b12824218250ebf80d8cc395eed2948a4f84b21` | Fixed-seed prompts and gold sets were generated from the same catalog snapshot; the model received catalog allowlists/alias tables. The 596 gold SKUs are per-task feasible products derived from the same item/format/style/region/stock/budget rules subsequently used by the treatment ranker, so the 100% SKU recall is mechanically non-discriminating for this task set. The task-hit count is an offline derivation from stored per-task records, not a new run. No human holdout or open-world accuracy claim; the evaluator does not exercise post-quote changes, confirmation, checkout, payment, or Devnet. | Why is the historical 596/596 recall not a meaningful search-quality estimate, and what independent relevance labels would a valid Recall@K need? |
| Agent shopping HTTP-to-MySQL lifecycle | Replayed the locked GPT-6 Luna intent outputs through the shopping HTTP API, sourcing the 24,000-product catalog from 12 running local Merchant HTTP fixtures and stock/transaction state from an isolated MySQL tenant. | 500/500 task flows reached terminal outcomes; 483 mock settlements, 17 simulated declines; 50 price changes, 50 stock-loss cases, 100 stale confirmations rejected; 4,352 HTTP requests, 0 unexpected failures; SQL audit 0 invariant violations; sequential per-task P50/P95 `90.424/137.707 ms` at `9.950 tasks/s` | RECORDED_GPT6_LUNA, MERCHANT_HTTP, HTTP_API, ISOLATED_MYSQL, MOCK_PAYMENT | `commerce-runtime/benchmarks/agentshopping/{mysql_http_lifecycle_suite_test.go,catalog_import.go,mysql_store.go,mysql_http_integration_test.go}`; `.local-run/agent-shopping/evaluations/shopping-intents-20261010T135310.164825200Z/{manifest.json,intent-metrics.json,intent-trials.jsonl,suite.jsonl}`; `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T080502.937245500Z-http-30dca910123d98dd/{manifest.json,metrics.json,audit-summary.json,trials.jsonl}` | Current run manifest and metrics bind task, catalog, overlay, and source-manifest hashes; the isolated tenant cleanup was verified by the test | All catalog/tasks are synthetic; the 500 GPT-6 Luna outputs were replayed (0 new model calls); Merchant fixtures and MySQL are local/isolated; the tested shopping API was an in-process `httptest` listener and tasks ran sequentially. Payment is simulation-only (no fulfillment, real provider, Devnet, or human holdout). | How are product facts separated from inventory authority, and what do the SQL audit counts prove versus not prove? |
| Agent shopping 300-attempt checkout replay (v1, historical) | Replayed 300 predeclared logical orders over the HTTP checkout boundary against isolated MySQL with response loss, API recreation, one OS-process restart, and duplicate requests. | 287/300 simulation-only settlements, 13 deterministic declines; 300 unique orders/payment IDs; zero duplicate order/payment/settlement and amount/currency mismatch counters; SQL audit PASS | HTTP_API, ISOLATED_MYSQL, FAULT_INJECTION, MOCK_PAYMENT | `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261010T230937.352979900Z-purchase-replay-fbb9448711231772/{manifest.json,metrics.json,audit-summary.json,trials.jsonl}` | Historical v1 run; superseded by the v2 checkout-plus-fulfillment run below for current evidence. | Locked MySQL catalog snapshot; deterministic mock only; one OS-process restart among 100 API recreations; not a provider/Devnet or production reliability claim. | What exactly was audited, and what does the single OS-process restart establish? |
| Agent shopping purchase, replay, fulfillment, and compensation (v2) | Implemented a benchmark-only persisted Agent purchase flow over isolated MySQL, from buyer-approved quote and inventory reservation through idempotent mock payment, fulfillment, and refund/restock compensation; exercised 300 logical orders under response-loss, API-recreation, restart, and duplicate-request faults. | End-to-end fulfilled **258/300 (86.00%)**; **29/300** refunded/restocked after predeclared delivery failures; **13/300** payment declines. Settlement was **287/300 (95.67%)**, not the end-to-end completion rate. 300 unique orders/payment IDs; zero duplicate payments/settlements/fulfillments/refunds or amount/state mismatches; 200 response losses, 100 API recreations (one OS-process restart), 100 duplicate pairs; 861 fulfillment HTTP requests; initial fulfillment request P95 `33.098 ms` (n=287); SQL + offline audit PASS | HTTP_API, ISOLATED_MYSQL, FAULT_INJECTION, MOCK_PAYMENT, MOCK_FULFILLMENT | `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261010T233527.056652900Z-purchase-replay-29c86a9579fc4f6d/{manifest.json,metrics.json,audit-summary.json,trials.jsonl}`; `scripts/benchmark-agent-shopping-300-purchases.ps1`; `scripts/audit-agent-shopping-purchase-replay.ps1`; benchmark implementation in `commerce-runtime/benchmarks/agentshopping` | 2026-10-11; artifacts contain catalog/task/overlay/source hashes; no new model calls; artifact SHA-256 values recorded in `docs/AGENT_SHOPPING_BENCHMARK_PLAN.md` | Synthetic catalog and locked MySQL snapshot; deterministic local payment/fulfillment/refund only. The 29 delivery failures were predeclared by task-ID hash. Refund/restock are one local DB transaction; no external-provider crash window, real payment/refund, Devnet, or production reliability rate. | How are payment and fulfillment idempotency independently audited, why are 29 failures not an observed rate, and what crash boundary remains untested? |
| Agent shopping purchase, replay, and bounded fulfillment retry (v3, measured) | Replayed 300 predeclared logical mock purchases against isolated MySQL; bounded retry recovered every one-shot transient delivery fault, with payment/order identity preserved across response loss and replay. | End-to-end `FULFILLED` **287/300 (95.67%)**; **13/300** deterministic mock payment declines; **29/29** scheduled transient delivery faults recovered; 300 unique task/order/payment IDs; zero duplicate order keys/orders/payments/settlements/fulfillments/refunds, amount/currency mismatches, orphan attempts, retry-sequence errors, or final-state mismatches; 200 post-commit response losses, 100 API recreations (one OS-process restart), 100 concurrent duplicate pairs; SQL + offline audit PASS | HTTP_API, ISOLATED_MYSQL, FAULT_INJECTION, MOCK_PAYMENT, MOCK_FULFILLMENT | `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T030423.991849100Z-purchase-replay-3c62658aebd5c5a0/{manifest.json,metrics.json,audit-summary.json,trials.jsonl}`; `scripts/benchmark-agent-shopping-300-purchases.ps1`; `scripts/audit-agent-shopping-purchase-replay.ps1` | 2026-10-11 local working-tree run; manifest records `source_revision=null`, so no committed source revision is asserted; zero new model calls; catalog/task/overlay/source-manifest hashes and artifact SHA-256 values retained | Deterministic local payment/fulfillment mocks over a fixed synthetic task pool; 29 delivery faults are injected, not naturally observed. Only one trial restarted the OS process; API recreations and concurrent replay are separate fault strata. No real provider, refund, Devnet, external fulfillment, or production reliability estimate. | What does the 300-attempt denominator represent, how were the SQL tables reconciled, and why is this not a real payment success rate? |
| Agent shopping concurrent discovery load (historical pre-query-batching baseline) | Replayed locked shopping intents through `POST /v1/intents/parse` over a concurrent HTTP boundary, with product facts from 12 local Merchant HTTP fixtures and inventory from an isolated MySQL tenant. | Six tiers `1/10/25/50/100/200`; at 50 workers `657.95 searches/s`, P95 `94.865 ms`; at 100/200 workers `643.81/712.10 searches/s`, P95 `165.535/348.708 ms`; all tiers 500/500 Top-1 synthetic-gold hits, 0 HTTP/business errors; audit PASS | RECORDED_GPT6_LUNA, MERCHANT_HTTP, HTTP_API, ISOLATED_MYSQL, CONCURRENT_LOAD | One strict run, closed-loop worker pool; each tier has 500 warmup and 500 measured requests; local Windows/amd64, Go 1.26.5, MySQL 8.0.46, pool max-open/max-idle 96; no new model calls; not k6 or a steady-duration VU test. At 100/200 workers observed 1,666/2,763 pool waits. These are historical pre-batching measurements, not results for the current one-query inventory lookup. | Same 500 generated intents were repeated across tiers; labels derive from catalog-generated exact groups, so 500/500 Top-1 is functional coverage, not human-labelled target relevance. Server is in-process `httptest`, Merchant is a local immutable HTTP snapshot, and MySQL is local/isolated. Search only—no quote, order, payment, fulfillment, or production traffic. | What did the concurrent test load, how was the tenant audited, and why is this not the same metric as Runtime k6 QPS? |
| Agent shopping concurrent discovery load after request-path query reduction | Replayed the locked recorded intents through `POST /v1/intents/parse`; reused already-loaded Buyer/Task rows and used insert-first idempotency for ordinary new tasks, with live inventory from isolated MySQL. | Six tiers `1/10/25/50/100/200`; throughput `28.99/172.39/270.16/325.52/405.81/422.66 searches/s`; P95 `47.82/80.37/118.79/192.25/316.90/574.03 ms`; every tier 500/500 measured requests, 0 HTTP/business errors, 500/500 synthetic Top-1 hits; SQL audit PASS. At 200 workers: 1,492 pool waits / `40,144.47 ms` cumulative wait. | RECORDED_GPT6_LUNA, MERCHANT_HTTP, HTTP_API, ISOLATED_MYSQL, CONCURRENT_LOAD | `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T041115.082000900Z-http-concurrent-27368916482f7c30/{manifest.json,metrics.json,audit-summary.json,trials.jsonl}`; one closed-loop matrix, 500 warmup + 500 measured per tier; MySQL 8.0.46, max connections 151, pool max-open/max-idle 128; zero new model calls. Execution catalog hash `f5100a…`; recorded intent catalog hash `7fdb29…`; exact v1 Merchant-product projection match is recorded separately. | One local host and one run; bounded fixed-worker replay, not k6 or steady-duration VUs. At 200 workers this is slower than the earlier baseline (`712.10 searches/s`, `348.708 ms` P95), and throughput flattens from 100 to 200 while P95 rises. Synthetic Top-1 is not human relevance or current parser quality. No real model call, checkout, payment, fulfillment, or production traffic. | Why does throughput flatten despite zero pool waits through 100 workers, and what host/MySQL resource evidence explains the regression versus the earlier run? |
| Two-stage synthetic catalog discovery with batched live inventory | Added a bounded first-stage Merchant listing search (100-SKU discovery window), then hydrated all exact `(merchant_id, sku_id)` pairs through one parameterized MySQL inventory query before live region/price/stock and hard-attribute filtering; applied the same two-stage boundary to the database-backed fallback path. | On the 24,000-product / 12-merchant isolated snapshot, the latest 25 sequential searches returned 20 candidates; P50/P95 `3.683/4.702 ms`. The 9-scenario MySQL/API lifecycle integration suite and the one-product Merchant snapshot path passed after the change. | UNIT_TEST, LOCAL_INTEGRATION, MYSQL, SYNTHETIC_CATALOG | `commerce-runtime/benchmarks/agentshopping/{retrieval.go,challenge_evaluation.go,mysql_store.go,mysql_store_query_test.go,challenge_evaluation_test.go,mysql_http_integration_test.go}`; `TestMySQLMerchantCatalogCandidateSearchAtScale`; `TestMySQLHTTPShoppingLifecycleRequiresFreshConsent`; `TestMySQLHTTPMerchantSnapshotCandidateSearch` | Working-tree verification on 2026-10-11; not yet committed. Scale search used the seeded isolated MySQL catalog snapshot to exercise the Merchant-catalog method; it did not keep 12 Merchant HTTP processes running during the 25 timing samples. | Single local host, fixed synthetic catalog, 25 sequential samples; not a sustained/concurrent load test or independent target-relevance evaluation. The generated challenge suite remains unreviewed and is not resume evidence. | Which facts enter retrieval before the hard business filters, what does the 100-item window trade off, and what limits the timing result? |
| Paired B0/B1 shopping evaluator and mixed decisions | Added same-task baseline/treatment plan validity, paired outcomes and bootstrap delta reporting; separated feasible-SKU Recall@K, task hit rate and valid-plan rate; fixed clarification accounting. | Initial GPT-6 Luna grading: B0 `249/350` (`71.1%`), B1 `321/350` (`91.7%`). A later offline replay of the exact same stored model responses under revised parser/candidate-window code yielded B0 `254/350` (`72.6%`), B1 `350/350` (`100%`), delta `+27.4 pp`, feasible-gold task-hit@20 `329/350` (`94.0%`); target-product Recall@20 remains unavailable (`0/350` independently labeled PLAN tasks). No new model calls in the replay. | REAL_LLM, OFFLINE_REPLAY, UNIT_TEST, LOCAL_SYNTHETIC_EVAL | `commerce-runtime/benchmarks/agentshopping/{challenge_evaluation.go,challenge_evaluation_test.go,challenge_parser.go,retrieval.go}`; initial `.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261010T200737.945097000Z/{manifest.json,metrics.json,trials.jsonl}`; replay `.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261010T220237.471963000Z/{manifest.json,metrics.json,trials.jsonl}` | Working-tree verification on 2026-10-11; task SHA-256 `2cf1359ddfe41b6979c2dcd8320fc9521b0a06d6b4d59fae91035079be364c7a`; 10 Hermes CLI batches; token usage recorded, API-call count unavailable. | All tasks/prompts and gold labels are algorithmically generated from the same synthetic catalog; the two scores are different evaluator/ranker revisions over the same model responses, not an independent before/after model experiment. No human adjudication or independent relevance labels. Neither `91.7%` nor `100%` is resume evidence, and neither may be represented as the illustrative `72.6% → 91.8%` result. No payment or simulation DB writes. | Why did the same stored response score change after evaluator revisions, and what independent labels and prompt split are required before claiming product relevance? |
| Agent shopping substitution and fail-closed controls | Added a benchmark-only curated same-format coffee substitute path; buyer-visible candidates and immutable quotes identify requested/offered items, with explicit buyer confirmation before checkout. | Fresh isolated-MySQL rerun after the separate buyer-approval credential: 9/9 scenarios PASS; one stockout exact target produced a labeled `SUBSTITUTE`; unrelated SKU and intent-budget escalation were rejected; explicit confirmation plus checkout replay produced exactly `1` order and `1` mock payment. Impossible-budget, unsupported-region, forbidden-substitution, and clarification paths retained `0` unauthorized purchase-side effects; duplicate confirmation rows `0`; tenant cleanup PASS. | MYSQL, HTTP_API, ISOLATED_MYSQL, SAFETY_TEST | `commerce-runtime/benchmarks/agentshopping/mysql_http_integration_test.go`: `TestMySQLHTTPShoppingLifecycleRequiresFreshConsent`; `.local-run/agent-shopping/integration-evaluations/mysql-http-fresh-consent-20261011T081248Z/go-test.jsonl` | Deterministic parser stub, synthetic fixture, and payment mock; narrow substitution allowlist. This does not establish generic substitute quality or production Merchant/payment behavior; `real_payment_adapter=NOT_PRESENT`. | How is a substitute distinguished from an exact hit, and what does the immutable quote require before checkout? |
| Benchmark-only fulfillment and compensation | Added a persisted mock fulfillment endpoint for paid orders; a declared delivery failure records a separate mock refund, returns inventory, and preserves the original settled-payment fact. | Isolated-MySQL HTTP integration PASS: one delivered order; one failed delivery reached `REFUNDED` with exact-amount mock refund and stock restock; a concurrent outstanding reservation remained intact; repeated fulfillment returned the same record; conflicting idempotency key was rejected; one declined payment created no fulfillment/refund. No duplicate fulfillment/refund rows. | MYSQL, HTTP_API, ISOLATED_MYSQL, UNIT_TEST | `commerce-runtime/benchmarks/agentshopping/{fulfillment.go,fulfillment_test.go,mysql_models.go,http_api.go}`; `scripts/test-agent-shopping-fulfillment.ps1` | Three deterministic synthetic fixtures in one targeted integration test. Fulfillment, refund, and inventory compensation are committed in one local-MySQL transaction; no external delivery/refund provider, inter-service crash window, or real settlement is exercised. This feature has not been folded into the historical 300/500-task metrics. | Which payment fact remains immutable after refund, how is stock restored without disturbing another reservation, and what failure boundary is still untested? |
| Local multi-node Kubernetes state-machine validation | Deployed the benchmark-only commerce Runtime and isolated MySQL as separate Deployments/Services on different workers in a three-node Minikube profile; exercised the persisted Agent payment Episode lifecycle through Kubernetes Service DNS. | 10/10 Episodes `FULFILLED`/`COMPLETED` at up to 3 concurrent requests; P50/P95 `1.857/2.010 s`; `0.966` Episodes/s; duplicate PaymentIntent/`PAYMENT_SETTLED`, budget drift, orphan, and stuck counts all `0` | K8S_LOCAL, MYSQL, REPLAY | `infra-deployment/k8s/minikube/e4-local.yaml`; `scripts/benchmark-minikube-e4.ps1`; `scripts/audit-minikube-e4.ps1`; `benchmarks/minikube-local-20260925/{environment-manifest.json,load-summary.json,audit-summary.json}` | local evidence run from Runtime source `c7f425b`; local deployment/test files added in working tree | Three Minikube nodes on one physical Docker Desktop host. Merchant/Payment/Verification are benchmark-only deterministic mocks; no production Payment Service or chain client; no crash injected. | Which components ran in separate Pods, how did Runtime resolve MySQL, and which boundaries were mocked? |
| Internship Alibaba Cloud ACK deployment (separate from StablePay local Minikube results) | User confirms deploying StablePay microservices to Alibaba Cloud ACK and using Kubernetes during the internship. | qualitative; no retained cluster-size, rollout, traffic, or availability figures | USER_ATTESTED, CONFIG_ONLY | User statement in this conversation; repository deployment configuration at `infra-deployment/k8s/ack/README.md`, `infra-deployment/k8s/ack/apps/*.yaml`, and `infra-deployment/k8s/ack/platform/ingress.yaml` | No deployment-run or pipeline artifact retained in the repository | ACK manifests and deployment guidance corroborate intended workload/configuration only; they do not independently prove a successful cloud rollout, exact personal ownership, CI/CD, or production scale. Keep separate from the local Minikube/Devnet evidence. | Which services did you personally deploy, what did you own in the rollout, and what operational checks did you perform? |
| Six-node Kubernetes real-payment acceptance | Deployed the six StablePay services, Merchant, Agent Runtime, MySQL, Redis, and RocketMQ as 12 ready Deployments across six Minikube nodes; completed one real low-value Solana Devnet purchase and replayed the request after Runtime restart. | One confirmed `0.01 USDC` payment; Merchant `402` challenge → delivery `200`; Episode `FULFILLED`/`COMPLETED`; same-request restart replay created no second payment; duplicate PaymentIntent/settlement queries returned 0 rows; budget and Payment/Verification/merchant/validator evidence matched | K8S_LOCAL, MYSQL, REAL_DEVNET, REPLAY | `benchmarks/minikube-six-node-devnet/acceptance-report.md`; `benchmarks/minikube-six-node-devnet/audit-acceptance.sql`; request `minikube-six-devnet-acceptance-20260925-03`; Episode `ce_9fc2b5eddfcbc645249fbd677baf6dd4`; Devnet tx `3r8EDrdrtNnGDorPKidUXhu6ugtWK5UMbE2mdUv89LJahCJ2TiCxAhGwkTTUpz7QTvnJcgXyQUET4LgwRY3D6XB8` | One physical Windows/Docker Desktop host; the six Kubernetes nodes are not six physical machines. This is one paid acceptance, not a repeated-payment reliability rate. | Which twelve workloads were ready, what did the post-restart replay prove, and which database/service records were reconciled? |
| RuntimeGuard proposal boundary | Exercised positive and negative proposals at the live-local `CommitProposal` boundary. | 150/150 unsafe proposals rejected; 150/150 valid proposals accepted; 0 false accepts, 0 false rejects, and 0 side-effect escapes across 40 integrity trials | E2_LOCAL, UNIT_TEST | `.local-run/resume-benchmark/e2-guard/{metrics.json,results.jsonl,report.md}` | local E2 evidence; RuntimeGuard source audit | Bounded deterministic local dataset, not production traffic or a universal safety proof. | How did the positive/negative dataset map to the Guard contract, and how were side effects observed? |
| Crash/restart economic correctness | Killed and restarted the local Runtime at eight persisted workflow windows against an isolated MySQL schema and benchmark-only deterministic adapters. | 160/160 Episodes resumed and completed; audit PASS 160/160; duplicate PaymentIntent rows, duplicate `PAYMENT_SETTLED` rows, count mismatches, budget drift, orphaned Episodes, and stuck Episodes all `0`; TTR P50/P95 `0.621/28.807 s` | E4_LOCAL, MYSQL, REPLAY | `.local-run/resume-benchmark/e4-local-20260923T165522Z-8087/full/{metrics.json,audit-summary.json,manifest.json,trials.jsonl}` | local E4 evidence; `FROZEN_RUNTIME_CHANGED=NO` | Benchmark-only local mock merchant/payment/verification adapters; not a real payment-network fault test. | Which MySQL invariants were audited after each crash, and which crash windows are represented? |
| Fault recovery | In 48 actually triggered fault trials, 40 recovered under the deterministic live-local setup. | `40/48 = 83.33%` | LIVE_LOCAL | `.local-run/s11-live-local/metrics.json`; `report.md`; `observability.Compute` | `cea47a6`; freeze `ae67ae5` | This is not a production failure-recovery rate. | Why is the denominator triggered faults rather than configured faults? |
| Safety observation | Across the evaluated live-local trials, no unauthorized side effect or duplicate settlement was observed. | `0` / `0` across evaluated trials | LIVE_LOCAL | `.local-run/s11-live-local/metrics.json`; `GradeEpisode`; `DecisionOutcomeTrace` | `cea47a6`; freeze `ae67ae5` | Observed result, not a universal safety proof. | How did the harness observe side effects before and after a rejected proposal? |
| Historical real payment path | Earlier U1 runs also validated real Solana Devnet settlement/verification. The current six-node acceptance above is the latest retained proof. | qualitative | REAL_DEVNET | `benchmarks/minikube-six-node-devnet/acceptance-report.md`; historical U1 script `scripts/test-unified-business-e2e.ps1 -Mode U1` | `9b885e2`; historical `9086871`, `b62e062`, `1fccccf` | One paid six-node acceptance only; never call it “56 trials on Devnet.” | Which report and transaction identity prove this was Devnet? |
| Prior real recovery provider | Validated a real DeepSeek recovery integration in prior U2 acceptance. | qualitative | REAL_LLM | `scripts/test-unified-business-e2e.ps1 -Mode U2`; recorded U2 artifacts | `b423752` | F0 status is `NOT RUN`; require a persisted non-rule `ModelDecisionTrace` for a fresh claim. | What distinguishes a real LLM trace from a rule fallback? |

The Merchant-source boundary has both component and full-suite verification:
the 240-page importer test reads 24,000 synthetic products from 12 in-process
HTTP fixtures; `TestMySQLHTTPMerchantSnapshotCandidateSearch` verifies exact
snapshot matching, rejects deliberate catalog drift, and keeps product facts
bound to Merchant HTTP while inventory remains in MySQL; and the latest locked
500-task lifecycle run (`lifecycle-20261010T163527.512255000Z-http-3d8d77529c31d84e`)
fetched the full catalog from 12 running local Merchant HTTP processes before
replaying the tasks through the shopping HTTP API and an isolated MySQL tenant.
Its audit recorded 2,850 events, exact expected row
counts (including 24,000 products, 96,000 regional inventory rows, 500 orders,
and 500 simulated payments), and no invariant violations. The catalog, overlay,
task-suite, and source-manifest hashes are in that run's metrics and manifest.
The shopping API under test was an in-process `httptest` server, and task flows
were sequential; the latest run measured P95 `133.925 ms` at `10.647 tasks/s`.
This is an end-to-end local integration result, not a concurrent throughput
test or production marketplace claim.

The separate concurrent discovery run (`lifecycle-20261010T170402.817022300Z-http-concurrent-af6795c4246d80be`)
tested `1/10/25/50/100/200` workers with 500 warmup and 500 measured requests
per tier. This is a historical pre-query-batching baseline. The 50-worker tier
completed `657.95 searches/s` at `94.865 ms` P95;
100/200 workers measured `643.81/712.10 searches/s` at `165.535/348.708 ms`
P95. All tiers had 500/500 Top-1 gold hits, zero HTTP/business errors, and a
passing isolated-MySQL audit. It was a bounded fixed-worker replay, not k6 or a
steady-duration load test; the endpoint performed search only and created no
orders or payments. A 2026-10-11 rerun after batching inventory lookup was
first stopped because the live Merchant snapshot included deterministic
benchmark metadata absent from the older product projection. The gate was not
bypassed: compatibility now requires an exact match of all legacy Merchant
facts, inventory, tasks, and source manifest, while separately recording the
current enriched execution hash. The latest full post-change matrix under that
contract and after request-path query reduction measured `28.99/172.39/270.16/
325.52/405.81/422.66 searches/s` with P95 `47.82/80.37/118.79/192.25/316.90/
574.03 ms` at `1/10/25/50/100/200` workers. Every tier passed the SQL audit
with zero HTTP/business errors. The 200-worker result remains below the
historical baseline; throughput flattens between 100 and 200 workers while P95
increases. Exact artifacts are in the claim inventory above.

`TestMySQLMerchantCatalogCandidateSearchAtScale` is a separate read-only query
diagnostic over the persisted 12-merchant/24,000-product isolated seed: the
post-batching run's 25 warmed searches returned 20 candidates and measured
P50/P95 of 3.683/4.702 ms. It validates sequential lookup only and is distinct
from the 200-worker HTTP result. The product snapshot for this diagnostic was
read from MySQL, so these latency numbers are not Merchant HTTP or public API
latency and are not a resume headline.

## Current shopping verification refresh (2026-10-11)

- Fresh 500-task HTTP-to-MySQL lifecycle: `lifecycle-20261011T080502.937245500Z-http-30dca910123d98dd`; 500/500 terminal, 4,352 HTTP requests, 0 unexpected failures, 483 simulated settlements/17 declines, 50 price revisions, 50 stock-loss mutations, 100 stale-quote rejections, SQL audit PASS with 0 invariant violations. Latest sequential P50/P95 `90.424/137.707 ms`, `9.950` terminal tasks/s. Zero new model calls; recorded GPT-6 Luna intents, synthetic catalog, isolated MySQL, mock-only payment. Tenant cleanup was verified by the test. Evidence: `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T080502.937245500Z-http-30dca910123d98dd/{manifest.json,metrics.json,audit-summary.json,trials.jsonl}`.
- Fresh buyer-approval MySQL/API safety rerun: 9/9 named scenarios passed after the separate buyer-approval credential change; duplicate confirmation rows `0`, isolated tenant cleanup passed. Captured Go JSON test log: `.local-run/agent-shopping/integration-evaluations/mysql-http-fresh-consent-20261011T081248Z/go-test.jsonl` (SHA-256 `3f15bfc1da6145c32eeb106cfecca38459eceeff272fcca4073ceeff053c1b91`). The test uses a deterministic parser and payment mock; it does not measure general substitute quality or a real payment adapter.

These are lifecycle and safety results, not independent product-discovery quality. The generated/replayed tasks do not provide the human-adjudicated `relevant_skus` needed for target-product Recall@K or a valid 72.6%→91.8% comparison.

## Quantitative claim policy

For a performance-focused resume, a safe quantified line is: “Load-tested a stateful commerce Runtime to 200 VUs, sustaining 1,319 HTTP req/s and 72.6 completed Episodes/s at 0% observed HTTP/business errors (60s measurement; local deterministic dependencies).” Keep the local scope adjacent to the numbers. A reliability-focused alternative is the observed safety pair (`0` unauthorized side effects / `0` duplicate settlements) plus `40/48` fault-trial recovery in deterministic live-local. The overall `48/56` is deliberately not a headline because the 56 include the expected `guard-rejected` safety-negative group.

For an end-to-end commerce resume line, use the six-node acceptance as a bounded integration result: one real `0.01 USDC` Devnet purchase reached verified delivery, and replaying the same request after Runtime restart did not create another payment; all 12 Deployments were ready across six Minikube nodes on one physical host. Do not turn this single acceptance into a payment success rate or a six-physical-node claim.

The live-local latency pair `52.25 / 152.62 ms` is appendix-only, rounded from the recorded metrics. It describes deterministic local adapters and is not Solana, Payment Service, DeepSeek, or Internet latency.

The Agent commerce task benchmark is also local-only: its `100/100` selection, artifact, paid-delivery, and safe-decline results are scores against eight fixed synthetic task profiles, with 25 isolated repetitions each. It validates the evaluation harness and this fixture set; it is not a claim about LLM task quality, production merchants, or Devnet payment success. This `100/100` run is not the planned 500-shopping-task evaluation; see `docs/AGENT_SHOPPING_BENCHMARK_PLAN.md`.

The separate shopping intent-to-plan run is now measured on a locked 500-task
synthetic suite: GPT-6 Luna raw exact intent `500/500`, B1 Recall@20 `596/596`,
and B1 valid plan `500/500`, compared with B0 `389/500`. This result is usable
only with the synthetic/template scope adjacent to the numbers. It does not
validate the advertised price-change or stock-change flow: those scenario
labels were present, but this evaluator did not apply a post-quote mutation,
request renewed confirmation, or submit a purchase. Raw output artifacts are
local and ignored; API-call count is unavailable, not estimated.

A separate no-model oracle diagnostic on the unreviewed challenge draft
(`shopping-challenge-20261010T191847.435777800Z`) measured B0 at `366/500`
valid plans and B1 at `464/500` (+19.6 percentage points, paired task-bootstrap
95% CI +15.0 to +24.4 points); feasible-SKU Recall@20 was 42.1% micro / 58.7%
macro. It uses gold intents and generated labels, so it is only an evaluator /
ranker diagnostic, not model quality, independently judged product recall, or
a resume claim. It does not establish the proposed 72.6% → 91.8% result.
The top-20 shortlist audit found 504 generated GoldSKU entries among 10,000
retrieved entries; at least one appeared in 464/500 tasks and all gold SKUs in
189/500. This measures filtering pressure against the synthetic snapshot only,
not independently adjudicated relevance.

A separate mixed-decision draft contains 350 plan, 75 no-plan, and 75
clarification tasks, with snapshot-derived labels. GPT-6 Luna completed it:
321/350 valid plans (91.7%) versus B0 249/350 (71.1%), paired +20.6 pp;
NO_PLAN and CLARIFY were each 75/75, with 0 parse failures. The draft remains
`AUTO_GENERATED_UNREVIEWED_DRAFT`; these are useful system diagnostics only,
not independent product-relevance or resume evidence. See
`docs/AGENT_SHOPPING_BENCHMARK_PLAN.md` for confidence intervals and token
accounting. The first attempt failed report serialization because unexpected
clarifications were counted against the wrong denominator; that scoring bug is
fixed and regression-tested.

A subsequent offline replay of that run's hash-verified saved structured
outputs (no new model calls) with updated parser/candidate logic produced
350/350 B1 valid plans versus 254/350 B0 (72.6%), paired +27.4 pp; generated
feasible-gold task-hit@20 was 329/350 (94.0%), and feasible-SKU Recall@20 was
41.9% micro / 59.0% macro. The output manifest is explicitly marked
`OFFLINE_PARSE_REPLAY_DIAGNOSTIC_NOT_RESUME_EVIDENCE`; target-product Recall@20
remains unavailable because there are zero independently labeled PLAN tasks.
This is a post-hoc code-path diagnostic on the same auto-generated,
unreviewed suite—not a fresh GPT-6 Luna run or resume claim. Do not report
`72.6% → 100%`, `94.0% target recall`, or use it as a substitute for the
illustrative `72.6% → 91.8%` claim. Artifacts:
`.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261010T220237.471963000Z/`.
A read-only recomputation of that run's 500 archived trials yields 500/500
correct disposition decisions (350 valid PLAN, 75 correct NO_PLAN, and 75
correct CLARIFY). This is the new all-task decision-accuracy definition, but
the run remains generator-authored and label-coupled; 100% is a warning about
benchmark difficulty, not resume evidence. The challenge evaluator now emits
this composite metric and its per-scenario rates as
`correct_decision_rate_all_tasks` while preserving the separate per-disposition
denominators.

Benchmark integrity follow-up (2026-10-11): `RelevantSKUs` validation now
rejects products outside the requested exact-item or explicitly permitted
substitution family, while allowing relevant listings with no current offer.
This protects future target-product Recall@K labels from unrelated same-
category SKUs. Regression tests pass. It validates structural consistency,
not that human authorship/adjudication declarations are true; the current
generated suites still have no independent relevance labels.

Difficulty audit: the mixed suite's 350 PLAN prompts share one generated base
template; all state the item explicitly and append exactly two hard-attribute
key/value filters selected from a known feasible target. The 75 NO_PLAN cases
are 55 one-minor-unit price boundaries plus 20 generated incompatible
attribute pairs; all 75 CLARIFY cases omit the same three fields and use one
template. In the saved replay, all 350 PLAN tasks had at least 64 first-stage
candidates, then only 1–18 remained after filtering (median 2); 21 raw top-20
gold misses still became valid plans via the 100-item pre-filter window. This
explains the perfect post-hoc plan rate as template/label-aligned diagnostic
behavior, not independent task generalization. See the benchmark plan for the
full audit. It is reproducible with `scripts/audit-agent-shopping-difficulty.ps1`,
which binds the exact task bytes to manifest/metrics, reconciles trial IDs and
metric numerators, and prints artifact hashes before reporting distributions.
No new model calls were made. The audit was executed successfully (exit code 0)
under Windows PowerShell 5.1 on 2026-10-11; its source is UTF-8 with BOM for
Windows PowerShell 5.1 compatibility.

Future challenge-run manifests use schema v4 and include SHA-256 for suite,
parse results, trials, metrics, and any saved raw model artifacts; offline
replay rejects a modified v4 artifact. Legacy v2/v3 run manifests remain
readable, but are not retroactively hash-bound. Verified with
`TestLoadChallengeReplayResultsVerifiesV4ArtifactHashes` and the existing v3
compatibility replay test.

The read-only evidence summarizer requires an explicit audited E4 selection and refuses to overwrite an existing summary, for example `python scripts/summarize-benchmark-evidence.py --e4-run .local-run/resume-benchmark/e4-local-20260923T165522Z-8087 --output resume-safe-summary-20261011.json` (or pass its `full` subdirectory). It does not choose an arbitrary latest directory; `--output` is a new filename inside the evidence root.

## B0.1 Resume STAR drafts and excluded diagnostics

These drafts keep each number next to its workload and environment. An entry
explicitly marked as a diagnostic/excluded result is evidence inventory only,
not approved resume wording. E1 is deliberately excluded from all resume claims.

### StablePay Agent Commerce Runtime — Go, MySQL, Docker k6

- **Situation:** Needed to characterize the authenticated Runtime API under increasing concurrency while distinguishing HTTP request throughput from completed commerce Episodes.
- **Task:** Measure a six-tier 1/10/25/50/100/200-VU matrix with a 15-second warmup and 60-second measurement per tier.
- **Action:** Ran Dockerized `grafana/k6` against the local Runtime/MySQL setup and recorded both request and business-completion metrics.
- **Result:** At 200 VUs, measured **1,319.133 HTTP req/s**, **72.6 completed Episodes/s**, **114.393 ms HTTP p95**, and **3.349 s Episode p95**, with **0% observed HTTP/business errors**; all six tiers passed.

### StablePay Agent payment lifecycle — Go Runtime, MySQL, Kubernetes

- **Situation:** Needed to verify that the persisted Agent payment lifecycle and budget ledger behave correctly when Runtime and its database are scheduled as separate Kubernetes workloads.
- **Task:** Run the full deterministic Episode lifecycle in a multi-node local Kubernetes topology without making real chain payments.
- **Action:** Deployed the benchmark-only Runtime and isolated MySQL into separate Pods on two Minikube workers, reached MySQL through ClusterIP DNS, and ran a single smoke followed by 10 Episodes at up to 3 concurrent requests; audited each Episode's PaymentIntent, settlement, budget, and terminal state in MySQL.
- **Result:** **10/10** load Episodes reached `FULFILLED` with execution `COMPLETED`; duplicate PaymentIntent/`PAYMENT_SETTLED`, budget drift, orphaned, and stuck counts were all **0**. P50/P95 Episode latency was **1.857/2.010 s** at **0.966 Episodes/s**. This was a three-node Minikube simulation on one physical host, using deterministic mock Merchant/Payment/Verification adapters; it did not exercise the production Payment Service, a real chain, or crash recovery.

### StablePay Agent commerce task benchmark — Go, RuntimeGuard, payment lifecycle

- **Situation:** Request throughput and terminal-state success alone do not show whether an Agent selected the intended offer, received the right artifact, or safely declined an ineligible purchase.
- **Task:** Add reproducible ground-truth evaluation for choice correctness, artifact correctness, paid delivery, and negative/no-payment behavior at the external Runtime boundary.
- **Action:** Added eight deterministic task profiles spanning price/quality/region/budget constraints and expected safe declines; each of 25 trials used a fresh Runtime and in-memory repository, deterministic mock adapters, and assertion-level grading of selected merchant/capability, delivered-body SHA-256, validation, PaymentIntent, settlement, and terminal state.
- **Result:** Across **200 Episodes** (eight fixed profiles × 25 repetitions), measured **100/100** correct expected offer selections, **100/100** correct artifact hashes, **100/100** paid tasks reaching validated delivery, and **100/100** expected safe declines with zero PaymentIntent/settlement; **0** collection errors and Episode p95 **114.304 ms**. This is a synthetic local harness result, not 200 unique prompts, model quality, real merchant traffic, or Devnet performance.

### Agent shopping intent-to-plan — development diagnostics, excluded from resume claims

The saved GPT-6 Luna runs are useful for debugging parser and ranker behavior,
but the tasks and gold labels were generated from the same synthetic catalog;
they do not satisfy the independent human-authored holdout requirement. The
earlier templated suite recorded raw exact intent `500/500`, B1 snapshot plans
`500/500`, and `596/596` feasible SKU hits. The latter is **not target-product
relevance Recall@20**. A later mixed draft measured B0 `249/350` and B1
`321/350` valid plans (`71.1%` → `91.7%`), with feasible-SKU Recall@20 of
`41.0%` micro / `57.5%` macro. Both runs are diagnostics only: do not use their
perfect scores or the `91.7%` result as a resume claim or as a substitute for
the illustrative `72.6%` → `91.8%` result. No new model run was made after the
explicit-format parser correction; the next quality score requires a frozen,
independently authored and adjudicated holdout.

### StablePay Agent shopping transaction lifecycle — Go, Merchant HTTP, MySQL

- **Situation:** Intent accuracy and candidate ranking did not establish that a shopping decision could survive price/stock changes and complete only after explicit buyer consent.
- **Task:** Validate the persisted shopping state machine against the same locked 500-task suite without making new model calls or touching production payment data.
- **Action:** Loaded and hash-validated 24,000 catalog products from 12 running local Merchant HTTP fixtures; kept regional inventory, buyer consent, quote/reservation/order state, and audit in an isolated MySQL tenant; replayed the locked GPT-6 Luna intents through the HTTP shopping API, with price/stock mutations, stale-consent rejection, deterministic mock checkout, and idempotency checks.
- **Result:** In the latest rerun, **500/500** task flows reached terminal outcomes (**483** simulated settlements, **17** declines); 50 price changes, 50 stock-loss cases, and 100 stale confirmations were handled; the **4,352-request** run had **0 unexpected HTTP failures** and **0 SQL invariant violations**. Sequential per-task P50/P95 were **90.424/137.707 ms** at **9.950 tasks/s**. This is a local synthetic integration using a loopback Merchant HTTP source, in-process `httptest` API, isolated MySQL, recorded intents, and mock-only payments; it is not a concurrent load test, fresh LLM evaluation, real fulfillment, or real settlement.

### StablePay Agent purchase idempotency and response-loss replay — Go HTTP API, MySQL

- **Situation:** A successful checkout whose response is lost can be retried, including after API-server recreation or concurrently, and must not create another order or payment effect.
- **Task:** Exercise 300 predeclared logical purchase keys with post-commit response loss, retry, API recreation, and concurrent duplicate requests against isolated MySQL.
- **Action:** Replayed 300 tasks from the locked generated suite without new model calls: 100 post-commit response-loss retries, 100 API-recreation retries (including one OS-process restart), and 100 concurrent duplicate-checkout pairs (600 checkout HTTP requests total). Fixed a concurrency race by rechecking idempotency after acquiring the confirmation lock and before stale-quote validation; reconciled orders, mock payments, settlements, task states, amounts, currencies, and idempotency keys in isolated MySQL. A read-only offline verifier then reconciled all saved trial rows against the run manifest, metrics, and SQL-audit summary and recorded artifact hashes.
- **Result:** **287/300** unique orders settled in the deterministic mock and **13/300** were deterministically declined. The captured SQL audit found **300** unique orders and mock payments, with **0** duplicate orders, payments, or settlements and **0** amount/currency mismatches; the offline evidence check passed across all 300 rows. This is a bounded local simulation with isolated MySQL—not a broad Runtime-process crash/restart reliability rate, a fresh database query by the offline verifier, a real provider sandbox, Devnet payment, or an independent shopping holdout.

### StablePay Agent purchase fulfillment and failure compensation — Go HTTP API, MySQL (v2 historical control)

- **Situation:** Checkout idempotency alone did not prove that successful mock payments had a terminal fulfillment outcome or that a failed delivery was safely compensated.
- **Task:** Extend the same 300 predeclared logical orders through isolated benchmark-only fulfillment, with replay and conflict checks, then reconcile order, payment, fulfillment, refund, reservation, and inventory state.
- **Action:** For each of 287 mock-settled orders, sent a fulfillment request, replayed the same idempotency key, and rejected a conflicting key. Applied 29 task-ID-hash-predeclared delivery failures; the local transaction recorded an exact mock refund and returned inventory while preserving the immutable original settled-payment fact. Replayed the existing response-loss/API-recreation/concurrent-duplicate schedule and reconciled both checkout and fulfillment effects in live isolated-MySQL SQL plus the offline artifact verifier.
- **Result:** **258/300 logical attempts (86.00%)** reached mock `FULFILLED`; **29/300 (9.67%)** were refunded/restocked after predeclared simulated delivery failures; **13/300 (4.33%)** were declined by the mock payment adapter. Thus **287/300 (95.67%)** is settlement rate, not end-to-end purchase completion; among settled orders, **258/287 (89.90%)** were fulfilled. The suite made **861** fulfillment HTTP requests (287 initial requests, 287 same-key replays, and 287 conflicting-key checks); the P95 for the **287 initial requests** was **33.098 ms**. SQL found zero duplicate fulfillment/refund rows, missing settled-payment fulfillment, declined-order effects, or payment/amount/state/reservation mismatches; checkout and fulfillment audits passed. This is a deterministic local simulation—not real delivery, payment, refund, Devnet, an external-provider crash-window test, or a production reliability rate. Artifacts: `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261010T233527.056652900Z-purchase-replay-29c86a9579fc4f6d/`.

### StablePay Agent purchase bounded retry recovery — Go HTTP API, MySQL (v3 measured)

- **Situation:** The v2 flow safely refunded/restocked orders on all 29 scheduled delivery failures, but did not distinguish a one-shot transient delivery fault from a terminal failure.
- **Task:** Add durable, bounded retry for the transient case while preserving the paid order, inventory reservation, and payment identity; retain compensation for permanent or exhausted failures.
- **Action:** Persisted fulfillment provider attempts and retry sequence, returned `RETRY_PENDING` after a one-shot injected failure, and retried with the same fulfillment idempotency key (maximum three attempts). Replayed the same 300-task hash with 200 post-commit checkout response losses, 100 API recreations (one OS-process restart in checkout replay), and 100 concurrent duplicate pairs. Separately restarted the API while one order was `RETRY_PENDING` to verify durable fulfillment recovery across a process boundary.
- **Result:** **287/300** logical orders reached mock `FULFILLED`; **13/300** were mock-payment declines; **29/29** predeclared one-shot transient delivery faults recovered. Relative to v2, 29 fewer orders required compensation and 29 more reached delivery on the same task snapshot, after changing the scheduled delivery fault from terminal to retryable. SQL and offline artifact audits passed with zero duplicate order/payment/settlement/fulfillment/refund rows, amount/currency mismatches, orphan attempts, retry-sequence errors, or final-state mismatches. The separate restart test observed two provider attempts, one fulfillment, and zero refunds. This is a fixed synthetic task pool and deterministic local mock—not a real provider sandbox, Devnet, or production reliability estimate. Artifacts: `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T030423.991849100Z-purchase-replay-3c62658aebd5c5a0/`; test `TestMySQLHTTPFulfillmentRetryResumesAfterProcessRestart`.

### StablePay Agent cross-process checkout recovery — Go HTTP API subprocess, MySQL

- **Situation:** Most API recreations in the 300-attempt replay are in-process test-server recreations; a process boundary needs an explicit subprocess restart check.
- **Task:** Verify one committed mock checkout can be retried after losing its response and restarting the API subprocess, without creating another order or payment.
- **Action:** Replayed one task from the locked local intent suite with no new model calls; dropped the first post-commit HTTP response, terminated the child API process, started a fresh child process against the same isolated MySQL database, and retried with the same idempotency key. Compared order/payment identities and ran live SQL lifecycle and purchase-replay audits, followed by an offline artifact consistency audit.
- **Result:** **1/1** simulated checkout recovered as `SETTLED` with the same order and payment identities after **1 OS-process restart**; SQL audit found **0** duplicate idempotency groups, orders, payments, settlements, or amount/currency mismatches. This is a single deterministic benchmark-harness/API subprocess case—not a restart test of the packaged Commerce Runtime executable, a statistical reliability estimate, real settlement, or Devnet payment. The 300-run v2 replay separately includes one OS-process restart in its broader fault schedule. Artifacts: `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261010T230133.684713600Z-os-process-restart-f648ac6ab7085a9f/`.

### StablePay Agent MCP purchase path — Go stdio subprocess, HTTP, isolated MySQL (single integration trial)

- **Situation:** MCP protocol and HTTP forwarding had passed smoke tests, but the compiled agent-facing process had not been exercised through a persistent purchase lifecycle.
- **Task:** Prove one synthetic shopping transaction crosses the actual MCP stdio process, authenticated API boundary, independent buyer-approval boundary, and isolated MySQL without real payment effects.
- **Action:** Launched the compiled MCP executable as a child process; discovered the current protocol and seven tools; searched a seeded product, issued an immutable quote, recorded test-only synthetic approval through the separate API credential, checked out, read order status, fulfilled, and replayed checkout and fulfillment. Audited SQL rows and inventory directly, then verified tenant cleanup.
- **Result:** **1/1** local deterministic mock purchase reached `FULFILLED`; SQL contained exactly one order, payment, confirmation, reservation, fulfillment, and fulfillment attempt; inventory ended at `on_hand=3`, `reserved=0`; duplicate replay preserved order/payment/fulfillment identities; `real_settlement=false`. The test caught and fixed an MCP bug that forwarded path-only `order_id` in the strict fulfillment POST body. This is integration correctness evidence—not a shopping-quality or reliability rate, real human approval, Hermes/Codex model run, real Merchant, provider sandbox, or Devnet claim. Test: `commerce-runtime/benchmarks/agentshopping/mysql_mcp_subprocess_e2e_test.go`; bridge fix/regression: `commerce-runtime/cmd/agentshopping-mcp/{bridge.go,bridge_test.go}`. The passing run, manifest, metrics, and trial are retained under `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T064821.076651500Z-mcp-subprocess/`; tenant cleanup is asserted by the test.

### StablePay Agent shopping discovery load — Go, Merchant HTTP, MySQL

- **Situation:** The sequential purchase-lifecycle suite did not quantify API behavior under concurrent catalog discovery.
- **Task:** Measure a six-tier concurrency matrix while replaying locked tasks and isolating all MySQL state from real commerce.
- **Action:** Ran `1/10/25/50/100/200` closed-loop workers, with 500 warmup and 500 measured requests per tier, using a hash-validated 24,000-product snapshot from 12 local Merchant HTTP fixtures and a 128-open/128-idle MySQL pool.
- **Result:** Throughput was **28.99/172.39/270.16/325.52/405.81/422.66 searches/s** across the six tiers; P95 was **47.82/80.37/118.79/192.25/316.90/574.03 ms**. Every tier completed **500/500 requests**, had **0 HTTP/business errors**, and passed the isolated-MySQL audit. The 200-tier recorded **1,492 pool waits / 40.145 s cumulative wait**. This full post-change matrix was slower than the historical baseline at 50/100/200 workers; do not present it as a performance improvement. Workload remains in-process `httptest` with recorded intents, not k6, checkout/payment, production traffic, or a human-authored holdout. Artifacts: `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T041115.082000900Z-http-concurrent-27368916482f7c30/`.

Diagnostic follow-up: the startup category/format product index reduces ranking
work without changing ranking semantics; a refreshed isolated microbench was
267.771 → 26.334 µs/op on a 2,000-product partition, only 0.241 ms/request
saved. Exact
tuple inventory batching and request-path persistence-read reductions are also
implemented, but the full matrix still shows a throughput/P95 regression versus
the earlier run. The benchmark did not capture host CPU, MySQL CPU, disk I/O, or
per-query timings, so the remaining bottleneck is not yet isolated.

A simulation-only response header now measures the bounded candidate-discovery
pipeline separately from total HTTP latency; the harness reports its P50/P95
and requires timing coverage for every successful measured request. This was
added after the matrix above and has only received offline unit-test coverage;
it does not change or add performance evidence.

### StablePay Agent shopping safety, substitution, and clarification — Go, HTTP, MySQL

- **Situation:** A missing product constraint or unsafe purchase condition must not silently become a quote or payment attempt.
- **Task:** Verify persisted clarification, buyer rejection, stale-quote handling, hard-constraint refusals, and explicitly allowed substitution through the shopping HTTP API.
- **Action:** Ran nine isolated-MySQL scenarios with a deterministic parser stub; checked task/parent-child linkage, substitute labeling, immutable quote replay, buyer confirmation, idempotent checkout, purchase-side-effect tables, inventory, and tenant cleanup.
- **Result:** A stocked-out exact coffee item surfaced only an explicitly allowed same-format substitute; the quote preserved requested item, offered item, and substitution notice. Checkout after buyer confirmation replayed to one order and one mock payment. Missing `item` produced a durable `CLARIFICATION_REQUIRED` task with a linked follow-up; impossible budget, unsupported region, forbidden substitution, and clarification paths created no unauthorized purchase-side effects. This is a narrow benchmark-only rule and local synthetic evidence—not generic semantic matching, production authorization, or real-payment evidence.

### StablePay Agent commerce acceptance — Go, Solana Devnet, Kubernetes, MySQL, Redis, RocketMQ

- **Situation:** Needed to prove that the Agent purchase lifecycle crossed the deployed StablePay services and could resume safely after Runtime restart.
- **Task:** Deploy the service stack in the requested six-node local Kubernetes topology and complete a low-value merchant purchase on Solana Devnet.
- **Action:** Brought up 12 ready Deployments (six StablePay services, Merchant, Agent Runtime, MySQL, Redis, RocketMQ NameServer and Broker); completed the Merchant `402` payment challenge, Devnet settlement, entitlement verification, delivery and payload validation, then replayed the same request after restarting Runtime.
- **Result:** One `0.01 USDC` Devnet payment reached `FULFILLED`/`COMPLETED`; PaymentIntent and `PAYMENT_SETTLED` counts were exactly one, duplicate queries returned zero rows, ledger budget projections matched, and replay created no second payment. This was six Kubernetes nodes on one physical Docker Desktop host and one paid acceptance, not a production cluster or repeated-payment rate.

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

## B0.2 Concise recruiter-facing draft (four evidence-backed themes)

This is the current strongest compact version against the OCP comparison. Keep
the environment qualifiers adjacent to every metric; these are separate
validation tracks, not one experiment that combined the synthetic shopping
marketplace with the Devnet payment.

**StablePay｜面向 Agent 的支付与交易执行平台**

**技术栈：** Go、CloudWeGo、MySQL、Redis、RocketMQ、MCP、Kubernetes、Docker/k6、x402、Solana

**项目背景：** 面向 Agent 执行付费服务和多步骤购物时的支付授权、结果未知、重复扣款与履约中断问题，持续建设持久化交易 Runtime、商户购物仿真和支付可靠性验证。

**个人贡献边界：** 你已说明实习期间在阿里云 ACK 使用 Kubernetes 部署 StablePay；这项经历应单独按真实职责描述，不能用本地 Minikube 报告替代 ACK 证据。当前仓库还记录了后续本地 Runtime 与 benchmark 工作，投递时应与实习期交付区分。

- **跨商家目录与购物交易：** 将 12 个本地 Merchant HTTP fixture（24,000 条合成商品）接入购物 API 和隔离 MySQL，覆盖价格/库存变化、报价刷新、买家确认与 mock checkout；最新 500 条固定意图回放全部进入终态，包含 50 次价格变化、50 次库存变化、100 次过时报价确认拦截，4,352 次 HTTP 请求无意外失败，SQL invariant violations 为 0。顺序执行 P95 `137.707 ms`、`9.950 tasks/s`。这是记录意图回放，不是独立人工购物评测或目标商品召回率。证据：`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T080502.937245500Z-http-30dca910123d98dd/{manifest.json,metrics.json,audit-summary.json}`。
- **幂等支付与履约补偿：** v3 对 300 个预声明逻辑订单注入提交后响应丢失、API 重建/重试、并发重复请求及一次 checkout OS 进程重启；端到端 mock 履约完成 `287/300（95.67%）`，`13/300` 被 mock 支付拒绝。29 次预定的一次性瞬态配送故障全部按同一幂等键恢复；300 个订单/支付 ID 唯一，重复订单键、订单、支付、结算、履约、退款和金额/状态不一致均为 0，隔离 MySQL 与离线证据审计均 PASS。另有独立的单订单 MySQL 测试验证 `RETRY_PENDING` 履约在 API OS 进程重启后恢复，SQL 观测到 2 次配送尝试、1 次履约、0 次退款。v2 的 `258/300` 是未启用瞬态重试的历史对照，仍保留原始产物。证据：`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T030423.991849100Z-purchase-replay-3c62658aebd5c5a0/{metrics.json,audit-summary.json,manifest.json,trials.jsonl}`；测试 `TestMySQLHTTPFulfillmentRetryResumesAfterProcessRestart`。固定合成任务池、确定性本地 mock，不是外部支付、Devnet 或生产可靠性率。
- **确定性 Guard 与崩溃恢复：** 在本地 Guard 边界拒绝 `150/150` 个不安全提案、接受 `150/150` 个合法提案，40 次副作用完整性检查为 0 次越权副作用；另在隔离 MySQL 和 benchmark-only adapters 下覆盖 8 个崩溃窗口，`160/160` Episodes 恢复完成，重复 PaymentIntent/`PAYMENT_SETTLED`、预算漂移、orphan/stuck 均为 0，`FROZEN_RUNTIME_CHANGED=NO`。证据：`.local-run/resume-benchmark/e2-guard/metrics.json`；`.local-run/resume-benchmark/e4-local-20260923T165522Z-8087/full/{metrics.json,audit-summary.json,manifest.json}`。
- **云原生交付与性能：** 六节点 Minikube（单台 Docker Desktop 主机）部署 12 个 Ready Deployments，完成一笔真实 `0.01 USDC` Solana Devnet 支付、验单和交付；Runtime 重启后重放同一请求没有产生第二笔支付。另在独立的本机 Docker k6 E3 矩阵中，200 VUs 测得 `1,319.133 HTTP req/s`、`72.6 completed Episodes/s`、HTTP P95 `114.393 ms`、Episode P95 `3.349 s`、观测到的 HTTP/business errors `0%`。这不是六节点压测结果，也不是生产集群容量。证据：`benchmarks/minikube-six-node-devnet/acceptance-report.md`；`.local-run/resume-benchmark/e3-steady-state-final/{report.md,metrics.json,manifest.json}`。

不要把以上结果与未完成人工 holdout 的目标混写：`94.2%` 目标商品召回率和 `72.6% → 91.8%` 购买方案率仍未被独立标注数据证明。OCP 材料中的 `286/300` 是示例目标；StablePay 另有 `287/300` 的本地确定性 mock 结果，但任务、付款边界和环境不同，不可宣称复现或横向等价。六节点 Devnet 结果是一笔验收，E3 是独立的本机负载测试，购物/履约订单均使用 mock 支付。

## Claims intentionally excluded

- No millions-of-transactions, unqualified QPS, 99.99% availability, production-scale, or universal zero-failure claim. The E3 figure is a bounded local benchmark only.
- The separate 2026-10-11 six-node Minikube E3 rerun is **partial diagnostic evidence**, not a completed six-tier resume result: the 1- and 10-VU 60-second windows measured 17.567 / 119.417 HTTP req/s and 4.300 / 14.717 completed Episodes/s, respectively. Tiers 25/50/100/200 were not run because the old runner stopped after the 10-VU process exited with code 105; the original manifest mislabeled this as a threshold failure even though `threshold_failures` was empty, the raw summary showed no crossed thresholds or failed checks, and HTTP/business error rates were 0%. The raw MySQL cgroup was about 95% of its 1-GiB limit, but 0.413 GiB was inactive file cache; effective headroom was about 0.434 GiB, above the 0.1-GiB guard. The read-only isolated-MySQL audit found 1,350/1,350 Episodes `FULFILLED`/`COMPLETED`, exactly one PaymentIntent and settlement per Episode, and zero duplicate groups, incomplete Episodes, or budget-projection drift; its hash-bound summary is `.local-run/e3-six-node-final-20261011-20261011-084524/audit-summary.json`. Original manifest and raw k6 artifacts remain unchanged; the current runner separates process/evidence failure from threshold failure. Grafana k6 defines the threshold expression as pass criteria and reports whether a threshold was crossed ([thresholds](https://grafana.com/docs/k6/latest/using-k6/thresholds/)).
- A later read-only resource recheck on 2026-10-11 confirmed all six nodes Ready, but `stablepay-six-m03` was at 1.891/2 GiB (0.109 GiB headroom), below the E3 0.5-GiB node guard; host RAM free was 10.00/63.37 GiB, C: had 13.61 GiB free, and D: had 1,373.27 GiB free. The isolated E3 MySQL cgroup was 0.951/1 GiB raw usage, including 0.385 GiB `inactive_file`; effective usage/headroom was 0.566/0.434 GiB. The preflight now records raw and cache-adjusted MySQL memory separately and blocks on effective MySQL headroom below 0.1 GiB. The matrix was not started because the m03 node guard remains below threshold; these are resource diagnostics, not load-test results.
- A further read-only snapshot at 2026-10-11 03:25 UTC found all six nodes Ready and `m03` at 1.893/2 GiB (0.107 GiB headroom). Its RocketMQ Broker, NameServer, and Verification containers used 0.92 / 0.74 / 0.025 GiB and all lacked Kubernetes memory requests/limits. The isolated E3 MySQL cgroup was 1,020,579,840/1,073,741,824 raw bytes, with 413,274,112 inactive-file bytes and about 0.434 GiB effective headroom. Kubernetes Metrics API was unavailable; the host free-RAM query was denied, while `vmmemWSL` showed about 28.49 GiB working set (C: 13.32 GiB free, D: 1,374.58 GiB free). No further load was run. Three strategic-merge resource/placement patches under `benchmarks/minikube-six-node-devnet/resource-headroom-patches/` now pass Kubernetes server-side dry-run but remain unapplied; applying them would roll out three Deployments and requires a quiet maintenance window. A new host/node/MySQL preflight must pass before resuming tiers.
- No claim that every happy path calls DeepSeek; a happy path can complete without `ModelDecisionTrace`.
- E1 is classified `INTERVIEW_ONLY_NEGATIVE_ABLATION`, not a resume claim: on 360 real DeepSeek trials, recovery success was `58.33%` versus `100%` for Rule; retain only to discuss why the model treatment underperformed the deterministic control. Unsafe proposals remained `0%`; one proposal failed schema validation.
- No claim that the live-local suite exercised real Payment Service or Devnet.
- No claim that the local Minikube three-node simulation used three physical machines or that its mock adapter path exercised the six production microservices. The repository's `infra-deployment/k8s/ack` manifests and the user's internship ACK deployment are separate evidence and should be described on their own facts.
- The AgentPay task-evaluation `100/100` figures are limited to eight deterministic synthetic profiles, repeated 25 times; do not present them as model accuracy, broad marketplace quality, or a real-payment success rate.
- Public references (EComAgentBench, Amazon ESCI, ShoppingBench, WebShop, DeepShop, ShoppingComp, Commerce Agent Bench, WebMall, WebArena-Verified, VoiSeR, STaRK-Amazon, Amazon-M2, τ²-bench, Amazon-Bench, MerchantBench, Shopping MMLU, RetailBench, ShopperBench, MMShopBench, and IdempotencyBench) are methodology-only; none of their tasks or scores are included in StablePay's reported metrics. The independent 500-task shopping holdout remains incomplete. The 300-attempt local mock replay audit passed, but it is not a real-provider or Devnet payment result.
- A 6,765,036-byte pinned ShoppingComp snapshot was downloaded to ignored `.local-run` reference data and SHA-256 verified. A reproducible exact-title compatibility audit compared its English/Chinese ground-truth set (194 task instances, 472 expert product references) with 24,000 active local Merchant products: `0/472` exact normalized-title matches and `0/194` task hits. This is **not** a recall score; the dataset is out of domain for the current synthetic digital-goods catalog and is excluded from StablePay quality claims.
- Benchmark-only fulfillment has durable bounded retry; both the v3 300-order replay (`287/300` fulfilled, `29/29` predeclared transient faults recovered) and the dedicated single-order MySQL test that resumes `RETRY_PENDING` after an OS process restart passed on 2026-10-11. The main run's one OS restart was for checkout response-loss replay; the separate test specifically covers restart during pending fulfillment. Both use isolated MySQL and deterministic mocks, not a real provider.
- A local double-review bundle finalizer and recomputation gate now exist for future shopping holdouts; the strict `--require-human-review` evaluator path requires exactly 500 frozen tasks and the pre-registered 350/75/75 decision mix across 13 strata, and embeds the protocol definition plus SHA-256 in v5 run manifests. Reviewer packets now strip stratum/author metadata, use independent random task aliases and ordering, and are retained/hash-bound in the v5 review bundle; identity and actual blinding declarations remain self-attested. No human-authored 500-task bundle has been supplied or evaluated yet; this is process tooling, not benchmark evidence, and does not make the illustrative 94.2% / 72.6% → 91.8% targets claimable. Smaller packets are tooling/diagnostic only.
- The generated mixed-shopping draft v3 contained direct expected-answer cues in all 75 NO_PLAN and all 75 CLARIFY prompts; its historical 500/500 decision result is diagnostic-only and invalid as a quality claim. Draft v4 removed those lexical cues but still treated soft-style-only omissions as blocking clarification; v5 added style-unspecified PLAN controls but exposed two item/style word-overlap leaks. Draft v6 passes the updated structural/label-leak audit (including 70 PLAN controls with style omitted), but remains auto-generated, unreviewed, and excluded from resume claims until an independent double-reviewed holdout exists. A fresh v6 audit found 500 distinct prompt strings but only 94 exact surface shapes after normalizing gold values (PLAN 57/350, NO_PLAN 7/75 with one 53-task cluster, CLARIFY 30/75). This heuristic is not a semantic-difficulty score. The 500/500 decision replay was on v2, not v6; v6 has no model evaluation. Following that audit, the diagnostic draft generator was changed to assign snapshot-compatible base tasks across the three preregistered NO_PLAN mechanisms (price infeasibility, unserved region, empty hard-attribute intersection), vary price deficits deterministically, and fail closed if the pinned synthetic snapshot cannot instantiate a requested quota. Unit tests cover 40-task balance and the unsupported-region case only; v6 artifacts were not changed, no v7 suite or model evaluation was produced, and this is not evidence of semantic difficulty or resume performance.
- The human-review audit schema now records scenario and final-disposition task counts, label/intent/disposition agreement, disposition Cohen's kappa (including an explicit undefined status), and exact plus macro/micro Jaccard agreement for feasible/relevant SKU sets and clarification fields. Each raw reviewer annotation is validated against the pinned snapshot before agreement is calculated; normalized exact prompt/context duplicates are rejected. These process guards are unit-tested but do not prescribe quota targets, identify semantic paraphrases, or prove reviewers' identities/blindness. No actual human labels exist yet, so no inter-rater result is available.
- Target-product Recall@K now marks computed values from unverified relevance labels as `AVAILABLE_UNVERIFIED_RELEVANCE_LABELS`; only structurally complete, hash-bound double-review declarations receive `AVAILABLE_DECLARED_DOUBLE_REVIEW_NOT_EXTERNALLY_VERIFIED`. The latter remains self-attested process evidence, not external proof or a resume result by itself.
- New reports attach that same relevance-label status to the derived `target_product_task_hit_at_k` metric in aggregate and per-scenario output; historical ignored `.local-run` artifacts were not rewritten. Its percentage cannot be separated from the label-provenance boundary.
- Challenge evaluator v6 separates nonempty clarification questions from a field-target cue proxy, rejects generic filler questions for labeled missing fields, and rejects CLARIFY labels blocked only by optional `preferred_style`. This is a lexical policy guard, not a semantic question-quality result; no human-reviewed holdout exists yet.
- The difficulty audit now recomputes both clarification-question counts from per-task trials and reconciles them against v6 metrics; legacy v5 artifacts remain auditable and report these new measures as unavailable rather than inventing them.
- `evaluate-challenge` now blocks model-backed runs without a recomputable human-review bundle unless `--allow-unreviewed-diagnostic-model-run` is explicitly supplied; that override is persisted in the manifest and the run remains diagnostic-only. Oracle and offline replay modes make no model calls.
- The benchmark-only `agentshopping-mcp` bridge exposes seven simulation tools and deliberately omits buyer confirmation; the agent child never receives the separate buyer-approval credential. A protocol/transport smoke passed on 2026-10-11, and a fresh MCP/MySQL subprocess E2E also passed: the compiled stdio process crossed the allowlisted HTTP client and authenticated simulation API into an isolated MySQL tenant, searched a seeded product, issued an immutable quote, received confirmation from a separate **synthetic test actor** (not human approval), completed one mock checkout, read order status, fulfilled the order, and replayed checkout/fulfillment idempotently. SQL contained exactly one confirmation, reservation, order, mock payment, fulfillment, and fulfillment attempt; terminal state was `FULFILLED`/`DELIVERED`, inventory returned to 3 on-hand / 0 reserved, and `real_settlement=false`. The tenant cleanup hook completed without error. Hash-bound artifacts: `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T091051.440810700Z-mcp-subprocess/`; MCP binary SHA-256 is in its manifest. This is one deterministic integration scenario, not a model-driven shopping quality score, human consent, external provider, or real-payment result.
- 2026-10-11 public ESCI reference diagnostic: a pinned 114,812,289-byte compressed test split (about 109.5 MiB) and deterministic samples are stored only in ignored `.local-run` on D:. The uniform sample's Exact-target macro Recall@20 was 95.42% for title overlap (95% task-bootstrap CI 94.27–96.51%), but all 500 queries hit at K=20 and 88.2% had six or more Exact products. A deliberately sparse-Exact sample (437 one-Exact queries plus 63 queries with 2–5 Exact products) measured macro Recall@5 of 89.30% for title overlap and 92.32% for BM25; at K=10 they reached 96.14% / 97.08%. These are judged-candidate-only external ranker diagnostics, not whole-catalog recall or StablePay scores; even the K=10 numbers exceeding 94.2% do not support the resume claim. No source payload was downloaded to C:, and the C: free-space check remained about 12.98 GiB. Definitions, caveats, and provenance: `docs/AGENT_SHOPPING_BENCHMARK_PLAN.md` and `.local-run/agent-shopping/reference-data/esci-us-test-20261011/`.
- 2026-10-11 current-catalog HTTP E2E diagnostic: `lifecycle-20261011T062444.190629000Z-http-c15f3fd84e449fe7` replayed 500 recorded intents through 12 local Merchant HTTP fixtures and a new isolated MySQL tenant; 500/500 reached terminal states, audit PASS with 0 invariant violations, 483 mock settlements / 17 mock declines, 50 price changes, 50 stock changes, and 100 stale-quote rejections; sequential P95 173.30 ms / 8.70 tasks/s. No new LLM calls or real payment/fulfillment occurred. This is correctness coverage on a fixed synthetic catalog, not a human relevance holdout or independent purchase-plan quality estimate; the co-recorded synthetic B0/B1 `389/500` vs `500/500` plan result and `596/596` gold hits are not resume claims. Full scope and artifact paths are recorded in `docs/AGENT_SHOPPING_BENCHMARK_PLAN.md`.
- No claim of RL, GRPO, post-training, self-evolution, dynamic planning, or parallel workflow execution.

## Latest shopping diagnostics (2026-10-11; excluded from resume claims)

- **Actual GPT-6 Luna parse/ranker run:** `.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T085437.957245400Z/`. On the auto-generated, unreviewed v7 500-task draft, model decision accuracy was 498/500 (99.6%), exact intent was 425/425 complete-intent tasks, B1 valid plans were 350/350 versus B0 206/350 (+41.14 pp), and feasible-SKU Recall@20 was 75.98% macro / 64.23% micro. Plan-hit@20 was 337/350 (96.29%); correct clarification was 73/75. Usage recorded 105,588 input and 33,552 output tokens; API-call count and temperature/top-p/max-tokens were unavailable. Exact-target product recall remains unavailable because there are no independent relevance labels. These near-ceiling scores diagnose an easy, generated suite; they are not the requested 94.2% relevance claim.
- **300 sandbox purchase/replay run:** `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T085605.852589900Z-purchase-replay-35649dd6f5292547/`. On a locked MySQL catalog snapshot and recorded GPT-6 Luna intent cohort, 300 unique mock orders/payments yielded 287 settled-and-fulfilled and 13 deterministic declines; 29/29 scheduled transient delivery faults recovered. The run covered 200 post-commit response losses, 100 duplicate-request pairs, 100 API recreations (one OS-process restart), with zero duplicate orders, payments, settlements, or fulfillments. SQL/offline audit PASS and the isolated tenant cleanup was verified. Payments and fulfillment were deterministic mocks; no Devnet or external provider was used.
- **v8 500-task transaction-lifecycle E2E:** `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T111312.706274600Z-challenge-http-1731bd3c8f0f353e/`. After a MySQL-backed HTTP regression fixed candidate-order loss after inventory lookup, the generated suite ran through the local API and isolated MySQL; 499/500 decisions matched labels, and 350/350 expected PLAN tasks reached terminal mock checkout (337 settled, 13 deterministic declines). The SQL audit and tenant cleanup passed with zero invariant violations. It exercised 38 price changes, 35 stock changes, and rejected 73 stale-quote confirmations; sequential task-level P95 was `181.766 ms` (`9.839 tasks/s`). The top SKU changed on 35/500 tasks versus the prior price-sorted response. One expected-CLARIFY task hit recorded `INVALID_INTENT` and safely made no order/payment. This replayed GPT-6 Luna records with zero new model calls and no real settlement. Prompts and labels remain generated from the same synthetic catalog, so this is not independent product-recall or valid-plan evidence and is excluded from resume claims.
- These are diagnostic and sandbox-correctness results only. The challenge labels are not human-reviewed, while the purchase run replays the prior intent cohort; neither supports an independent shopping-quality or real-payment success-rate claim.

## Runtime ranking experiment (2026-10-11; excluded from resume claims)

Discovery now uses bounded BM25 relevance over the transient buyer wording and
catalog title, description, tags, and attributes after existing category/item/
format eligibility. Exact-vs-substitute priority and hard-attribute preference
remain ahead of BM25; the live offer checks are unchanged. The stored GPT-6
Luna v7 parse outputs were replayed offline against the updated ranker: all
500 rows evaluated, with 0 parse failures, 498/500 decisions, and 350/350
valid plans. Those decision/plan metrics did not move and remain weak evidence
for retrieval because the generated tasks are easy and no independent
target-product labels exist. The offline replay is
`.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T090509.673892500Z/`.

The new ranker was also run directly on the already-local sparse ESCI reference
sample: 500 queries, 7,818 judged candidates, 652 Exact judgments. At K=5,
Runtime BM25 measured 91.92% macro / 89.88% micro Exact-target recall, versus
89.30% / 88.65% for title-overlap ranking; at K=10 the rates were 96.97% /
97.09% versus 96.14% / 96.32%. Both rankers reached 100% recall at K=20 because
the pool contains only judged candidates and many queries have multiple Exact
items. This is a candidate-pool ranking diagnostic, not full-catalog recall,
StablePay task quality, a purchase-plan rate, or a resume claim. The run and
sample hash are recorded in
`.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T090645.247069200Z/metrics.json`.
No data was downloaded during this run. Full package tests passed with
`GOPROXY=off`; the 12 local Merchant fixtures used for replay were stopped.

After the ranker update, the isolated 300-order lifecycle replay was run again
from the locked MySQL catalog: `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T090826.203333700Z-purchase-replay-5bd64b9f4bb89f8b/`.
It completed 287/300 mock settlements and fulfillments, with 13 deterministic
declines; all 29 predeclared transient delivery faults recovered in 316
provider attempts. The run injected 200 post-commit response losses, 100
concurrent duplicate pairs, 100 API recreations including one OS-process
restart, and observed zero duplicate order IDs, orders per task, payments per
order, settlements per order, or fulfillments per order. SQL and fulfillment
audits passed with zero invariant violations; lifecycle P50/P95 were
76.916/95.176 ms and fulfillment P95 was 20.410 ms. Test cleanup verified the
isolated tenant was empty; MySQL data was preserved. This remains deterministic
mock correctness evidence, not real payment or production reliability.

The MCP-to-MySQL gap is now closed for one deterministic subprocess scenario:
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T091051.440810700Z-mcp-subprocess/`.
The compiled MCP stdio child performed search, quote, checkout, order-status,
and fulfillment tools through its HTTP bridge; a separate synthetic test actor
confirmed the quote without giving its credential to the MCP child. Checkout
and fulfillment replays retained the original economic IDs. SQL showed exactly
one row in each confirmation, reservation, order, mock-payment, fulfillment,
and fulfillment-attempt table; final state was `FULFILLED`/`DELIVERED`, and
`real_settlement=false`. The isolated tenant cleanup completed without test
errors. This establishes protocol-to-persisted-mock-purchase integration, not
human authorization, model quality, Devnet settlement, or production
reliability.

## 2026-10-11 shopping E2E update (verified; quality claims still excluded)

The latest 500-task full HTTP lifecycle run is
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T092107.495161600Z-http-f4ffebb06598d1cf/`.
It replayed the locked GPT-6 Luna intent outputs through 12 local Merchant HTTP
fixtures, the shopping HTTP API, and an isolated MySQL tenant. All 500 tasks
reached terminal states: 483 deterministic mock settlements and 17 mock
declines. The run exercised 50 price changes, 50 stock-loss cases, and 100
stale-quote confirmation rejections over 4,352 HTTP requests, with zero
unexpected HTTP failures and zero SQL invariant violations. Per-task P50/P95
were 114.341/201.130 ms at 7.609 terminal tasks/s. Intent replay made zero new
model calls; payments and fulfillment were not real. The API under test was an
in-process `httptest` server, tasks were sequential, and the generated suite
does not constitute an independently reviewed shopping-quality holdout.

The latest 300-attempt purchase/replay run is
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T092309.371460200Z-purchase-replay-de5fc2809617f7c2/`.
It explicitly used the locked MySQL catalog snapshot because the live Merchant
HTTP catalog had a different product-projection hash; the harness refused the
first mismatched-source attempt rather than silently mixing snapshots. Of 300
unique mock orders/payments, 287 settled and fulfilled and 13 were deterministic
declines. All 29 scheduled transient delivery faults recovered. The test
injected 200 post-commit response losses, 100 API recreations (including one OS
process restart), and 100 concurrent duplicate-request pairs. The captured live
SQL and fulfillment audits found zero duplicate orders/payments/settlements,
orphan attempts, retry-sequence gaps, or final-state mismatches. The independent
PowerShell verifier checked all saved trial rows and artifact hashes; its
declared boundary is offline reconciliation of saved evidence, not a fresh SQL
query. Payment and fulfillment were deterministic local mocks, not Devnet or
external providers.

These runs establish persisted transaction-flow and idempotent-recovery
correctness in a local simulation. They do not establish the requested 94.2%
human-reviewed target-product recall, a 72.6%→91.8% independent plan-rate lift,
or a real-payment completion rate. The synthetic 500/500 terminal-flow result
must not be presented as any of those metrics.

## Disjoint public ranking reference and fresh API scale check (2026-10-11)

The ranking benchmark now supports query-ID exclusions. A new 500-query sparse
ESCI slice excludes the complete prior 500-query sample; the sampler verified
all 500 excluded IDs exist in the pinned test split, and a separate set
comparison observed zero overlap. The actual Go Runtime ranker scored 7,771
judged candidates (1,631 Exact judgments): at K=5, macro/micro Exact recall was
86.96%/85.35% versus title-overlap 85.94%/84.49%; at K=10 it was
97.05%/96.93% versus 95.55%/95.28%. Both reached 100% at K=20 because the
sample has only 15.54 judged candidates per query on average. The dataset is a
public reference, not StablePay's catalog, and has no unjudged negatives or
transaction feasibility labels; these numbers are not the 94.2% resume claim.
Artifacts:
`.local-run/agent-shopping/reference-data/esci-us-test-20261011/sparse-exact-disjoint-20261011/`
and `.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T093418.933528600Z/metrics.json`.

A fresh complete local shopping-API matrix used 500 warmup + 500 measured
requests at 1/10/25/50/100/200 fixed workers. With a 128-connection MySQL pool,
throughput was 34.40/254.51/370.32/459.31/465.94/488.42 req/s and P95 was
32.97/49.38/84.16/129.72/268.71/544.49 ms, with zero HTTP/business errors and
zero SQL invariant violations at all tiers. This is a single-host closed-loop
in-process HTTP harness, not k6 steady-duration or distributed deployment
evidence; the 200-worker latency is still high and is not presented as an
improvement over prior runs. Artifacts:
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T092717.471221000Z-http-concurrent-b1092972363c6005/`.

## Fresh model-backed shopping diagnostic — not a resume claim (2026-10-11)

Ran 500 generated challenge tasks through a fresh GPT-6 Luna (`openai-codex`,
reasoning none) parse plus the current local Merchant-catalog B0/B1 evaluator;
this was 10 Hermes CLI batches, not an offline replay. No DeepSeek was called.
Token usage was available (95,604 input / 33,562 output); sampling parameters
and API-call count were not reported. The manifest contains prompt/template/
response hashes. Artifacts:
`.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T094422.074741800Z/`.

Observed 499/500 correct decisions; 350/350 PLAN tasks were valid versus
206/350 (58.86%) for the paired B0 baseline; feasible-gold SKU Recall@20 was
78.04% macro / 67.02% micro; 342/350 PLAN tasks hit a feasible gold SKU;
NO_PLAN 75/75 and CLARIFY 74/75. One CLARIFY/MISSING_ITEM row failed parsing
(`INVALID_INTENT`). Target-product recall is unavailable because independent
exact-target labels are absent.

Exclude all these values from resume claims: this is an unreviewed generated
suite whose prompts and labels derive from the same synthetic catalog, not an
independent human holdout. This run evaluates model parsing, retrieval and plan
feasibility only; it did not execute orders or write the simulation database.
The 350/350 result is a diagnostic ceiling signal that the suite remains too
easy or insufficiently independent, not a validated purchase-plan success
rate. The separate HTTP-to-MySQL lifecycle evidence above uses deterministic
mock transaction execution and should retain its own scope statement.

## Runtime retrieval improvement on public reference data (2026-10-11; not a StablePay resume metric)

The benchmark-only Runtime ranker adds a fixed title-query coverage bonus
(weight 2.0) on top of BM25. The weight was selected on a 500-query
sparse-Exact development sample; two zero-overlap samples were then evaluated
without further tuning. On the sparse 500-query sample (7,771 judged
candidates, 1,631 Exact labels), Recall@5 improved from 86.96%/85.35% macro/
micro to 88.18%/86.88%, and NDCG@5 from 0.8907 to 0.9019. On a separate
uniform 500-query sample with 7,752 Exact labels, K=20 recall was 95.44%/90.25%
to 95.53%/90.40%, and NDCG@20 0.9814 to 0.9841. All three query sets were
pairwise disjoint; the 1,000 held-out query IDs were verified against the
pinned local ESCI source.

The ranker microbenchmark on 2,000 candidates was about 36.3 μs/op before and
after, with +224 B and +9 allocations per operation. `go test -p 1 -count=1
./...` passed. Artifacts:
`.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T095451.153935600Z/metrics.json`
and
`.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T095453.999476000Z/metrics.json`.

Do not claim either dataset result as the StablePay 94.2% resume target: the
test uses only ESCI judged candidates, excludes unjudged full-catalog negatives,
and contains no live offers, inventory, price, region, budget, or plan labels.
The sparse K=5 score remains below 94.2%; the 95.53% result is K=20 on a
different dense-Exact distribution. The independently reviewed StablePay
500-task target-recall and valid-plan gate remains unmet.

## Fresh target-relevance replay and 300-purchase E2E (2026-10-11)

- Added deterministic exact-target `relevant_skus` labels to the generated
  500-task suite, separate from stock/price/region-feasible `gold_skus`. A
  narrowly opt-in offline replay path verified that the artifact-hash-bound
  original prompts, intents, decisions, gold labels, task order, catalog and
  inventory were unchanged; only relevance labels were added. Replayed saved
  GPT-6 Luna parse results without new model calls. On the unreviewed synthetic
  suite, target SKU Recall@20 was **50.13% micro / 64.41% macro** (350 PLAN
  tasks; 12,934 labels), while task hit and valid-plan rates remained 100%.
  This separation exposes why a “found at least one feasible item” score is
  not equivalent to exact-target retrieval. Labels are machine-generated from
  the same catalog, so this is diagnostic only and does not substantiate the
  94.2% resume target. A follow-up ceiling audit found that 292/350 PLAN tasks
  have more than 20 relevant SKUs; observed Recall@20 equals its mathematical
  set-size ceiling exactly (6,484/12,934 = 50.13% micro / 64.41% macro). At
  K=50, recall was 85.21% / 93.33%, also exactly at ceiling; at K=100 it was
  100% / 100%. This cutoff dependence is a label-cardinality artifact, not
  evidence of ranker quality. The v7 evaluator now records theoretical
  micro/macro recall ceilings and the count of tasks with more relevant SKUs
  than K. None of these generated-suite scores is a resume claim. Artifacts:
  `.local-run/agent-shopping/challenge-suites/draft-mixed-v8/` and
  `.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T101548.658971600Z/`,
  `.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T101551.670669700Z/`,
  `.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T101554.543259000Z/`.
- Fresh 300-attempt isolated HTTP-to-MySQL purchase/recovery run passed on the
  locked MySQL catalog snapshot. It produced 300 unique orders and mock
  payments; 287 settled/fulfilled and 13 deterministic declines. Faults
  included 200 post-commit response losses, 100 API recreations (one OS-process
  restart), and 100 concurrent duplicate-request pairs. SQL/fulfillment audits
  found zero duplicate order groups/orders/payments/settlements/fulfillments,
  zero orphan attempts, and zero non-simulation effects; 29/29 transient
  delivery failures recovered. The cloned tenant was cleaned and verified
  empty. Payment, refund, and fulfillment providers were local mocks; no chain
  or external provider was contacted. This supports a carefully scoped
  sandbox orchestration/recovery claim, not real-payment reliability or
  equivalence to another project's 286/300 result. Artifacts:
  `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T100831.778001400Z-purchase-replay-b0a4c58c61931067/`.
- Ran 500 existing ESCI-US public test queries through the authenticated
  shopping HTTP API and isolated MySQL against each query's 8–40 provided
  judged candidates (7,771 candidate rows total). The API matched runtime
  ranking order on 500/500; exact-target Recall@5 was 88.03% macro / 86.70%
  micro, query hit@5 98.00%, NDCG@5 0.9015, and sequential P95 37.479 ms.
  This used one synthetic Merchant, in-stock US$10 offers, synthetic
  `CN-EAST` region, and a deterministic structured-intent oracle; it did not
  evaluate intent parsing, full-catalog retrieval, or purchases. Tenant audit
  found no quote/order/reservation/confirmation/payment side effects and
  cleanup passed. This is public candidate-pool reference evidence only, not
  a 94.2% resume claim. Artifacts:
  `.local-run/agent-shopping/reference-evaluations/esci-http-mysql-20261011T111958.605159700Z/`.
- Added a general title ordered-bigram coverage feature to discovery ranking,
  then selected weight 1.0 on a 500-query development sample and evaluated it
  once on a query-disjoint 500-query ESCI test sample. On that holdout the
  production ranker measured Recall@5 36.32% macro / 30.65% micro against a
  theoretical cutoff ceiling of 38.10% / 31.84%; Hit@5 was 100% and NDCG@5
  0.97294. Recall@20 was 95.54% macro / 90.44% micro. The K=20 macro figure
  is not an unqualified 94.2% claim: this is a judged candidate pool with an
  explicit cutoff and many Exact targets per query. The selected scorer then
  passed a 500-query authenticated HTTP/isolated-MySQL run (500/500 API order
  matches; sequential P95 22.068 ms; SQL audit and tenant cleanup passed; no
  purchase side effects).
  These are public-reference diagnostics only, not full-catalog or purchase
  quality claims. Artifacts:
  `.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T112536.543565500Z/`,
  `.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T113327.137649500Z/`,
  `.local-run/agent-shopping/reference-evaluations/esci-http-mysql-20261011T113245.948409300Z/`.
- Current-code v8 HTTP/MySQL checkout replay passed on the same 500 generated,
  unreviewed tasks: 499/500 decisions, 350/350 PLAN tasks reached terminal
  checkout outcomes (337 mock settlements, 13 deterministic declines), 38
  price changes, 35 stock changes, 73 stale-quote rejections, and zero
  unexpected HTTP failures or SQL invariant violations. The one expected 502
  corresponds to a recorded `INVALID_INTENT` clarification parse failure and
  created no purchase side effect. Sequential task P50/P95 was 92.534/140.961
  ms (12.127 tasks/s); this is one local run, not a load claim. No model calls,
  real settlement, or fulfillment adapter were used. The isolated tenant was
  cleaned and the simulation services stopped. Current ranker replay at K=20
  measured machine-labeled exact-target Recall 64.41% macro / 50.13% micro,
  exactly at the generated-label cardinality ceiling; not resume evidence.
  Artifacts:
  `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T113922.615260200Z-challenge-http-b322e71cecfcb042/`
  and
  `.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T114224.639869900Z/`.
- Re-ran the locked-snapshot 300-attempt isolated purchase/replay/fulfillment
  test against the current working tree. It produced 300 unique orders and
  mock payments (287 fulfilled/settled, 13 deterministic declines), recovered
  29/29 predeclared transient fulfillment failures, and injected 200 lost
  post-commit responses, 100 API recreations (one OS-process restart), and 100
  concurrent duplicate-request pairs. Audit PASS: duplicate order/payment/
  settlement/fulfillment/refund counts, orphan attempts, amount/state
  mismatches, and non-simulation effects were all zero. Fulfillment P95 was
  19.909 ms and task lifecycle P95 300.807 ms in this one local run. The
  isolated tenant was cleaned and MySQL stopped; payment and fulfillment were
  deterministic mocks, not external sandbox or Devnet. Artifact:
  `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T114338.530220000Z-purchase-replay-d78c331498dc0e60/`.
- Re-ran the full Go module suite after the current working-tree changes:
  `go test -p 1 -count=1 ./...` passed with `GOPROXY=off` and Go cache/temp
  redirected to D:. No external package download was needed.
- Evaluated the current production BM25 + title-coverage + ordered-bigram
  ranker on a 500-query ESCI sparse-Exact sample query-disjoint from the
  development sample (7,771 judged candidates; 1,631 Exact labels). At K=5,
  Exact Recall rose from 86.96% to 88.51% macro and 85.35% to 87.19% micro;
  NDCG@5 rose 0.89069→0.90518. Exact-query hit@5 fell 98.0%→97.6%; K=20 was
  saturated at the judged-candidate ceiling. This is a public judged-candidate
  reference only, not full-catalog discovery, offer feasibility, or the
  independently reviewed StablePay 94.2% target. Artifact:
  `.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T114737.061945700Z/metrics.json`.
