# Agent Shopping Evaluation Plan

Status: experiment design only. The 500-task targets below are not measured
results and must not be put on a resume until the locked run completes.

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
Chinese prompt through the intent parser and score field-level extraction
against the gold intent. This prevents parser errors from being confused with
ranking errors. No DeepSeek or other paid/external LLM call is part of this
plan; a provider-backed parsing experiment needs separate explicit
authorization and must record model/config/prompt hashes and actual token
usage only.

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
3. Run the safety suite first with a mock plan executor and prove no payment
   adapter is reachable.
4. Run B0 and B1 over all 500 locked cases, compare paired results, and inspect
   every mismatch before publishing a resume claim.
5. Commit the benchmark code, fixed dataset, manifest, raw traces, and report;
   update resume evidence only with achieved numbers and explicit scope.
