# Agent Shopping Evaluation Plan

Status: the locked 500-task intent-and-ranking evaluation and a 500-task
persistent HTTP-to-MySQL purchase lifecycle completed on 2026-10-10. The
lifecycle covers quote revisions, buyer consent, simulated payment, and SQL
audit on synthetic data. The online API now has an opt-in path that takes its
product snapshot directly from the 12 Merchant HTTP APIs; that exact full
lifecycle has not yet been rerun through the new path. Do not present the
synthetic results as production marketplace quality or as live Merchant
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
and the completed HTTP lifecycle run exercised 24,000 imported products. The
currently added online HTTP-source mode must still be rerun with the Merchant
processes listening so the final lifecycle evidence proves discovery used that
source snapshot, rather than only the cloned MySQL catalog.

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
3. **Next:** rerun that same locked lifecycle with `-UseMerchantHTTPSource`,
   require live-vs-seeded catalog hash equality, and retain the new manifest and
   audit artifacts. This requires all 12 local Merchant APIs to be healthy.
4. Add a distinct holdout with human-authored paraphrases not included in the
   supplied alias table; report it separately from this templated suite.
5. Expand the dedicated safety suite with impossible budgets, unsupported
   regions, ambiguous intent, and forbidden substitutions; preserve the
   zero-side-effect audit gate.
6. Keep local raw model artifacts
   ignored, and update resume evidence only with achieved numbers and explicit
   scope.
