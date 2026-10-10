# Agent Shopping Evaluation Plan

Status: the locked 500-task intent-and-ranking evaluation and persistent
HTTP-to-MySQL lifecycle completed on 2026-10-10. The final lifecycle run
loaded and hash-validated its 24,000-product catalog from 12 running local
Merchant HTTP fixtures (`lifecycle-20261010T163527.512255000Z-http-3d8d77529c31d84e`),
then exercised quotes, buyer consent, stock/price revisions, simulation-only
payment, and SQL audit in an isolated tenant. A separate 8-scenario,
MySQL-backed safety integration covers budget/region/no-substitution
refusals, explicit buyer rejection, stale quotes, idempotent confirmation,
and a durable ambiguity-to-clarification path with a linked follow-up task.
The six-tier concurrent shopping-API load test is now complete; the remaining
evaluation gap is a human-authored paraphrase holdout. These are still
synthetic local results, not production marketplace quality or Merchant
fulfillment.

## What the existing fixture proves—and does not prove

The ignored `.local-run/simulated-merchants` fixture has 12 isolated Merchant
instances and 24,000 seeded active products (`seed=20261010`). The public
listing endpoint is `GET /api/v1/products?page=N&size=100`; it exposes paging,
not search filters. The product record has SKU, title, description, price,
currency, author, tags, and active status. It does not contain inventory,
shipping/region, a quote revision, or a purchase confirmation protocol.
Purchasing is disabled, the Merchant IDs are synthetic, and no DID registry,
wallet, Payment Service, or Devnet path is configured.

`merchant_capabilities` describes executable Merchant services, not product
SKUs. Do not import these 24,000 products into that table or treat synthetic
seller addresses as registered payment identities. Product discovery uses a
separate read-only Merchant HTTP snapshot; the isolated MySQL tenant holds the
benchmark inventory/region/price overlay and transaction state.

The catalog importer validates all 240 listing pages (12 Merchants × 20 pages)
and the final HTTP lifecycle run exercised 24,000 imported products from the
running Merchant APIs. Startup validated the Merchant snapshot against the
isolated MySQL seed and recorded equal catalog, overlay, task, and source
manifest hashes. The shopping API itself is an in-process `httptest` server.
The sequential lifecycle result does not establish concurrent API capacity;
that is measured separately below.

## Concurrent shopping API load result

Run `lifecycle-20261010T170402.817022300Z-http-concurrent-af6795c4246d80be`
completed all six closed-loop worker-pool tiers (`1/10/25/50/100/200`). Each
tier replayed the same 500 locked GPT-6 Luna intents once as warmup and once as
measurement; this is a bounded request-count test, not a steady-duration k6
run. At 50 workers it completed 500 searches at **657.95 searches/s** with
**94.865 ms P95**, zero HTTP/business errors, and zero MySQL pool waits. At 100
and 200 workers it measured **643.81 / 712.10 searches/s**, with **165.535 /
348.708 ms P95** and 1,666 / 2,763 MySQL pool waits, respectively. Every tier
had 500/500 Top-1 gold hits. The isolated tenant audit passed with 6,000
tasks and 6,000 parse traces, and confirmed zero quote, order, reservation,
or simulated-payment side effects.

The run used 12 local Merchant HTTP fixtures for an immutable 24,000-product
snapshot, an in-process `httptest` shopping API, and isolated MySQL 8.0.46 on
Windows/amd64 (Go 1.26.5, 18 CPUs); the test pool was configured to 96 open and
96 idle connections. It made no new model calls and did not exercise purchase
or payment. Exact metrics, manifest, audit, and per-request trials are retained
locally under
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261010T170402.817022300Z-http-concurrent-af6795c4246d80be/`.

## Measured locked intent-and-ranking subset

Run `shopping-intents-20261010T135310.164825200Z` completed the fixed 500-task
suite (`seed=20261010`, task SHA-256
`sha256:4f7619a0455dfadfc20922c39b12824218250ebf80d8cc395eed2948a4f84b21`):

- GPT-6 Luna via the `openai-codex` provider produced 500/500 parse-valid and
  raw-model exact intents. The explicit taxonomy normalizer made zero
  corrections in this run.
- On the same 500 intents, B1 had `Recall@20 = 596/596` (macro and micro both
  100%), a task-level `any gold SKU in top 20` hit on 500/500 tasks (derived
  from the saved per-task `downstream_treatment.recall_hits` values), and
  500/500 valid snapshot plans. B0 had 389/500 valid plans (77.8%), so the
  observed paired plan-rate difference was 22.2 percentage points. The
  task-hit figure is a derived view of this same templated run, not an
  independent evaluation. Evaluation report schema v2 now emits task-hit
  numerator/rate/95% bootstrap CI for each policy and the paired treatment vs
  baseline delta/CI; this schema change does not retroactively alter the
  original run artifacts.
- The model ran in 10 batches. Hermes reported 82,434 input and 26,682 output
  tokens; API-call count is `TOKEN_METRIC_UNAVAILABLE`. Temperature, top-p, and
  max-token settings were not reported by the CLI. All 10 saved streams had
  zero `tool_use` and zero `tool_result` events; the run used the explicit
  no-tools `bot_room` allowlist.
- The reproducible report, manifest, 500 trials, raw responses, and streams
  are local-only under
  `.local-run/agent-shopping/evaluations/shopping-intents-20261010T135310.164825200Z/`.

This is a generated, templated suite derived from the same synthetic catalog;
the model was given catalog allowlists and the benchmark's explicit alias
dictionary. It is not a human-authored holdout or a measure of open-world
shopping language. The evaluator checks a deterministic inventory/price/
region snapshot; its `PRICE_CHANGE` and `STOCK_CHANGE` labels do not inject a
post-quote mutation. It does not execute buyer confirmation, checkout, payment,
or Devnet settlement. Those remain separate gates below.

## Interpretation of the requested resume metrics

The OCP comparison that supplied the example numbers explicitly calls them
illustrative hypothetical metrics, not measured OCP results. Treat them as
aspirational targets only. Before comparing runs, freeze these definitions:

- **Target-product recall:** for each task, relevant gold SKUs returned in the
  top K divided by all accepted gold SKUs; report macro task-average and micro
  SKU-level Recall@K, plus task-level `any accepted SKU in top K / tasks`.
  Specify K and the inventory/region/price snapshot.
- **Effective purchase-plan rate:** valid purchase plans divided by the number
  of tasks labeled purchaseable in the frozen suite. A valid plan satisfies
  every hard attribute, live stock/region/price and budget constraint, uses a
  current quote revision, and requires fresh buyer confirmation. Parse errors,
  missing candidates, and execution errors count as failures. Score correct
  no-plan/clarification behavior on infeasible tasks separately.
- **72.6% → 91.8%:** a paired comparison on the same frozen tasks, catalog
  snapshot, and evaluator, with raw counts, percentage-point delta, and
  confidence interval. Do not infer this from recall or terminal-state rate.
- **286/300 sandbox purchases:** 300 unique, predeclared purchase attempts;
  count a success only when the sandbox settlement and required order outcome
  are linked to one logical order and pass amount/currency/idempotency audit.
  Report declines/failures and duplicate successful settlements separately;
  name whether sandbox means local mock, provider sandbox, or Devnet.

The present suite is a generated fixture-consistency evaluation, not an
independent 500-task holdout. `GenerateTasks` selects viable targets before
constructing the prompt and gold labels; the evaluator then verifies gold
offers against the same eligibility dimensions (format, budget, region, and
stock). The latest locked run therefore reports B1 Recall@20 `596/596` and
valid snapshot plans `500/500`, against B0 `389/500`—not the example
`94.2%`, `72.6%`, or `91.8%`. It contains no independent natural-language
holdout or systematic infeasible/contradictory tasks. The HTTP lifecycle is a
separate 500-task run: all reached a terminal status, but only `483/500` were
mock-settled and `17/500` were mock-declined. No independent `286/300`
purchase suite was found; none of these are real-provider or Devnet purchase
results.

## External benchmark references (methodology, not StablePay scores)

Sources below were checked on 2026-10-11. Public tasks and labels may be used
to calibrate task design and metric implementation, but their scores must stay
separate from StablePay's own 500-task result. Public prompts/labels are not a
private blind holdout, and third-party catalog terms do not automatically
follow the benchmark-code license.

| Reference | Scope and scale | Reuse boundary | StablePay use |
|---|---|---|---|
| [EComAgentBench paper](https://arxiv.org/abs/2606.17698), [official repository](https://github.com/Morizeyao/EComAgentBench_), [product DB](https://huggingface.co/datasets/ecomagentbench/EComAgentBenchProductDB) | 662 English tasks, 8 shopping intents, 6,645 constraint rubrics; paper/repo describe a multi-million-product corpus and preference elicitation across user prompts, personas, and clarification turns. | Code is MIT; the Amazon Reviews-derived product corpus has separate research-use/source terms, and task-data redistribution terms must be checked independently before importing it. | Best reference for hidden preferences, clarifying questions, and constraint-completion task design. It does not validate inventory races, checkout, fulfillment, or payment. |
| [Amazon ESCI official repository](https://github.com/amazon-science/esci-data), [paper](https://arxiv.org/abs/2206.06588) | Human judgments label Exact, Substitute, Complement, or Irrelevant product matches; the repository documents reduced and full query/product-judgment sets. | Repository code is Apache-2.0; comply separately with dataset source and attribution conditions. | Calibrate Recall@K, nDCG, and substitution labels. It is a retrieval benchmark, not a multi-turn commerce or transaction test. |
| [ShoppingBench official repository](https://github.com/yjwjy/ShoppingBench), [paper](https://arxiv.org/abs/2508.04266) | 3,310 instructions and 2.5M+ product records; includes product discovery, same-store multi-item requests, coupons, and budget constraints. | Repository is Apache-2.0; third-party product-catalog rights are separate and should be checked before redistribution. | Useful for budget, coupon, and multi-product scenario coverage; not live inventory/price concurrency or payment-ledger correctness. |
| [WebMall official repository](https://github.com/wbsg-uni-mannheim/WebMall), [paper](https://arxiv.org/abs/2508.13024) | Four simulated shops, 4,421 offers, and 91 cross-shop tasks, including vague requests, comparison, alternatives/compatibility, cart, and simulated checkout. | No clear dataset license was visible in the repository root during review; obtain author clarification before copying or redistributing data. | Best reference for cross-shop navigation and checkout-flow cases, but its fixed simulated offers cannot establish dynamic stock, fulfillment, or payment correctness. |
| [IdempotencyBench official repository](https://github.com/gssanjana4/idempotencybench) | Repository describes 320 deterministic tasks with simulated payment side effects, timeout-after-commit, transport/agent retries, full replay, and an effect ledger. | Repository reports MIT for code and CC BY 4.0 for task/record data; the project is early-stage and its paper/results are not yet mature. Verify current terms before reuse. | Borrow fault categories and independent-effect-ledger design for the StablePay 300-attempt suite. It cannot replace StablePay's own isolated database/provider audit. |

Recommended use: treat ESCI as the retrieval-metric reference, EComAgentBench
as the clarification/hidden-preference design reference, and ShoppingBench or
WebMall as budget/multi-shop scenario references (WebMall only after license
clarification). Keep all public-benchmark results in separate reports. The
StablePay holdout must have independently authored prompts and labels,
catalog/task hashes frozen before ranker changes, and explicit feasible,
infeasible, ambiguous, stale-quote, and stock/price-race strata. Generated
StablePay fixture results remain labeled synthetic even if public benchmark
design patterns informed the scenarios.

No reviewed public benchmark validates StablePay's end-to-end payment
correctness. The 300-attempt claim requires a StablePay-specific isolated
simulation with predeclared logical orders, injected retry/crash/timeout
windows, a separate payment-effect ledger, and SQL reconciliation of order,
payment, amount, currency, and idempotency. Report completed unique settlements
and duplicate settlement effects independently; never infer a real-provider
or Devnet result from simulated checkout.

## Architecture for a safe local experiment

1. Start only the recorded simulated Merchant processes. Check readiness and
   manifest identity before querying them. Start the shopping API with
   `SHOP_SIM_MERCHANT_MANIFEST` (or `-UseMerchantHTTPSource`) to pin the
   product/taxonomy snapshot to the Merchant APIs; verify `/v1/catalog/source`
   reports `merchant_http_snapshot`, 12 merchants, and 24,000 products.
2. Build an immutable catalog snapshot over the HTTP APIs: 12 Merchants × 20
   pages × 100 rows = 240 listing requests. Validate each page's `total`, page
   size, stable SKU identity, no duplicate/missing SKU, and the final 2,000
   rows per Merchant. Record the snapshot SHA-256 and stop only processes
   owned by the harness.
3. Keep stock and delivery eligibility in a separate deterministic
   benchmark-only state overlay keyed by `(merchant_id, sku_id)`. The overlay
   may provide quantity, fulfillment mode/region, current price revision, and
   quote timestamp. It must never mutate the user's generated SQLite files or
   masquerade as Merchant-provided inventory. Revalidation reads this overlay
   immediately before a plan is offered.
4. Return a ranked `ShoppingPlan` with item, Merchant, price, quantity,
   fulfillment eligibility, quote revision, alternatives, and the reason each
   hard constraint passed. In the implemented lifecycle, a changed price or
   unavailable item invalidates the old quote and requires a new confirmation;
   the checkout/payment channel is isolated and simulation-only. It creates no
   real payment, wallet signature, or Devnet transaction.
5. Keep this product catalog separate from the existing Commerce Runtime
   capability catalog. A later integration may translate a confirmed product
   choice into the real payment plane only after Merchant identities,
   inventory/quote APIs, and an explicitly authorized payment environment
   exist.

## Frozen task set and comparison

Create a 500-case locked evaluation set with 500 distinct task IDs and distinct
gold target SKU sets, balanced across all 12 Merchant categories (41 or 42
core tasks per Merchant). Store both a Chinese buyer prompt and its structured
gold intent. Keep a separate development set for changing the ranker; do not
tune against the locked 500. Hash the task file, catalog snapshot, and state
overlay in the manifest.

All 500 core tasks must have at least one valid eventual purchase plan under
their declared state transitions. Stratify them across exact/category search,
item/format/style constraints, budget boundaries, preference trade-offs,
approved substitutions, and price/stock changes that require an explicit
second confirmation. Add a separate safety suite for impossible budgets,
region ineligibility, unavailable items with substitution forbidden, stale
quotes, ambiguous requests, and duplicate confirmation. Report this suite
separately so safe refusals do not inflate the purchase-plan denominator.

Run both versions on the same snapshot, task IDs, overlay, and ranking cutoff:

- **B0 baseline:** exact title/tag token match, apply declared hard budget,
  then sort by price. Freeze this implementation before looking at the locked
  results; label it as the benchmark baseline, not as a prior production
  capability.
- **B1 treatment:** normalize buyer intent into hard requirements and soft
  preferences; retrieve from the full snapshot; filter hard constraints before
  ranking; score the remaining choices using the declared preference weights;
  revalidate current price/stock/region before producing the plan; offer only
  allowed alternatives and request renewed confirmation after material
  changes.

Initially feed both rankers the same structured gold intent to isolate catalog
recall and plan selection. In a second, separately reported pass, feed the
Chinese prompt through the intent parser and score both raw-model and
post-normalization field accuracy against the gold intent. This prevents parser
errors from being confused with ranking errors. The completed intent pass used
GPT-6 Luna through the Codex provider, not DeepSeek; its manifest records model,
prompt hashes, and observed token usage while leaving unavailable API-call
counts unestimated.

## Metrics and decision rules

- **Target-product Recall@20:** per task, `|top-20 candidate SKUs ∩ gold SKUs|
  / |gold SKUs|`; publish both macro task-average and micro SKU-level recall,
  with a bootstrap 95% confidence interval. The proposed `94.2%` is a target,
  not an acceptance override.
- **Valid purchase-plan rate:** valid plans / all 500 feasible core tasks.
  A plan passes only if the SKU is in the gold set; every hard attribute,
  currency, budget (including any modeled delivery fee), quantity, current
  stock/region, and quote-revision check passes; ranking is consistent with
  the task preferences; and confirmation requirements are respected. The
  proposed `72.6% → 91.8%` is a target pair, not a promised outcome.
- **Safety suite:** report correct no-plan/clarification/re-confirmation
  outcomes, stale-plan rejection, and unauthorized checkout/payment side
  effects separately. The required unauthorized side-effect count is zero.
- **Operational metrics:** candidate count, catalog calls/cache hits, ingest
  and search p50/p95, errors, and end-to-end plan latency. Keep these separate
  from correctness rates.
- Use paired per-task comparisons (B0 vs B1), publish raw numerator/denominator
  and confidence interval, preserve every JSONL row, and do not reuse E3's
  `72.6 completed Episodes/s` as a `72.6%` shopping baseline—it is a different
  unit and experiment.

## Execution gates

1. **Complete:** validate the 12-service pagination contract and 24,000-SKU
   catalog ingest; lock the catalog and task hashes.
2. **Complete on the isolated MySQL snapshot:** exercise all 500 task lifecycles,
   including 50 price changes, 50 stock losses, fresh-consent checks, mock
   payment outcomes, idempotent replay, and SQL audit.
3. **Complete:** rerun the locked lifecycle with the 12-process Merchant HTTP
   snapshot, require live-vs-seeded hash equality, and retain the final metrics,
   audit, manifest, and trial artifacts. Run ID and exact output directory are
   recorded in `docs/RESUME_EVIDENCE.md`.
4. Add a distinct holdout with human-authored paraphrases not included in the
   supplied alias table; report it separately from this templated suite.
5. **Complete for the listed local safety cases:** MySQL/API integration covers
   impossible budgets, unsupported regions, forbidden substitutions, rejected
   buyer consent, stale price/stock quotes, duplicate confirmation, and
   ambiguity-to-clarification followed by a linked child task. Clarification,
   refusal, and rejected-consent paths assert zero unauthorized purchase-side
   effects; every per-test tenant is checked empty after cleanup. This is
   deterministic synthetic coverage, not a production authorization claim.
6. **Complete:** the six-tier concurrent shopping-API test uses isolated
   tenant state and deterministic recorded-intent replay; p50/p95 and
   completed search rate are reported separately from the Runtime k6 benchmark.
7. Keep local raw model artifacts
   ignored, and update resume evidence only with achieved numbers and explicit
   scope.
