# Agent Shopping Evaluation Plan

Status: the locked 500-task intent-and-ranking sub-experiment completed on
2026-10-10 and exceeded its numeric targets on this synthetic fixture. The
broader purchase lifecycle, real state-transition, confirmation, and safety
suite remain incomplete; do not present the current numbers as production
marketplace quality or as proof of those unmeasured behaviors.

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
seller addresses as registered payment identities. Product discovery should
have its own read-only catalog adapter and benchmark index.

The earlier page-20 smoke test establishes pagination metadata for one page per
Merchant. It does not yet prove a complete 24,000-row ingest or shopping
quality.

## Measured locked intent-and-ranking subset

Run `shopping-intents-20261010T135310.164825200Z` completed the fixed 500-task
suite (`seed=20261010`, task SHA-256
`sha256:4f7619a0455dfadfc20922c39b12824218250ebf80d8cc395eed2948a4f84b21`):

- GPT-6 Luna via the `openai-codex` provider produced 500/500 parse-valid and
  raw-model exact intents. The explicit taxonomy normalizer made zero
  corrections in this run.
- On the same 500 intents, B1 had `Recall@20 = 596/596` (macro and micro both
  100%) and 500/500 valid snapshot plans. B0 had 389/500 valid plans (77.8%),
  so the observed paired plan-rate difference was 22.2 percentage points.
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

## Architecture for a safe local experiment

1. Start only the recorded simulated Merchant processes. Check readiness and
   manifest identity before querying them.
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
   hard constraint passed. A changed price, unavailable item, or disallowed
   substitute returns `NEEDS_CONFIRMATION`/`NO_VALID_PLAN`; it must not silently
   proceed. The simulator stops at an accepted local plan. It creates no real
   payment, PaymentIntent, wallet signature, or Devnet transaction.
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

1. Verify the 12-service pagination contract and complete 24,000-SKU ingest.
2. Validate task/oracle and state-overlay invariants with boundary and
   adversarial tests; freeze hashes.
3. Add a distinct holdout with human-authored paraphrases not included in the
   supplied alias table; report it separately from this templated suite.
4. Implement and test price/stock mutation followed by stale-quote rejection,
   renewed buyer confirmation, and safe decline/accept paths.
5. Run the dedicated safety suite with a mock plan executor and prove no
   payment adapter is reachable; then run the full shopping lifecycle and
   audit its state transitions.
6. Commit the benchmark code and methodology; keep local raw model artifacts
   ignored, and update resume evidence only with achieved numbers and explicit
   scope.
