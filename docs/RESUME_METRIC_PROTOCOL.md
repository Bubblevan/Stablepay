# StablePay Resume Metric Protocol (Frozen Resume)

This document defines how to interpret and re-test metrics in the frozen resume.
It does **not** authorize edits to `docs/RESUME_BOSS_FINAL.md`. A smoke result is
only evidence that the measurement pipeline runs; it is not a resume-quality
estimate unless its task labels and data provenance pass the gates below.

## Metric registry

| Metric in the draft | Operational definition | Denominator / evidence boundary |
|---|---|---|
| Exact target-product Recall@5 | For each purchase-intent query, count exact-target SKUs among the first five discovery candidates, divided by independently adjudicated exact-target SKUs for that query. Report macro (mean of per-query recall) and micro (pooled relevant SKU hits / pooled exact-target SKUs) separately. Accepted substitutes are a separate metric. | PLAN queries with frozen `relevant_skus`; labels are independent of the ranker and frozen before either arm runs. Current code scores the discovery candidate list after its category/item/format and allowed-substitution gates, but before live offer feasibility filters; it is not automatically recall over every raw listing in the 24,000-item directory. Do not use generated `gold_skus`, current-stock feasibility, or accepted alternatives as exact-target relevance. If labels are absent/unverified, report `UNAVAILABLE`, not a percentage. |
| Effective purchase-plan rate | A PLAN task succeeds only if the submitted top choice is an exact target or explicitly permitted substitute and passes every hard attribute, currency/total budget, requested quantity, region/stock, current snapshot offer/revision, and confirmation/consent requirement. A pinned-snapshot plan score does not itself exercise a post-quote mutation or re-approval. | Valid plans / independently labeled, snapshot-feasible PLAN tasks. Baseline and treatment receive the same task, parsed intent, product/catalog/inventory/price snapshot, and scorer. Publish numerator/denominator for each, paired delta and task-bootstrap 95% CI, and both-valid / baseline-only / treatment-only / neither counts. Score NO_PLAN and CLARIFY separately and include all-task correct-decision rate only as a distinct composite. |
| End-to-end valid delivery rate | Count a task only when an eligible, buyer-authorized purchase reaches the expected terminal state and the delivered artifact is independently checked against the expected content/provenance. | Publish both `valid deliveries / all predeclared purchase attempts` and `valid deliveries / successfully settled eligible purchases`; never substitute one denominator for the other. Report safe refusals/declines separately. State whether payment is Mock, Devnet, or another environment. |
| “300 purchases”, 95.7% fulfillment | In the audited v3 replay, 287 of 300 logical attempts reached Mock `FULFILLED`; 13 were deterministic payment declines, so the all-attempt rate is 287/300 = 95.67%. The 287 settled purchases form a different conditional denominator. | This is a fixed synthetic task pool and deterministic Mock adapter, not 300 unique prompts or a real-provider success rate. The same logical purchase IDs are replayed for checkout/fulfillment/idempotency scenarios; retries are not additional purchases. |
| Duplicate settlement = 0 | For each logical payment attempt/idempotency key, count settlement rows beyond the allowed single settlement; separately count duplicate PaymentIntents, orders, transactions, fulfillments/refunds and unmatched/mismatched facts. | Must come from the run's transaction/ledger audit and reconcile to its task IDs. “0 observed” applies only to the named cohort and adapter. The zero from Bullet 1, Bullet 3 and Bullet 4 are distinct audits of the same invariant; do not pool or add them. |
| Fault recovery = 100% | A predeclared injected-fault trial succeeds only if the episode reaches its expected terminal business state after restart **and** all required database invariants pass (payment/settlement uniqueness, budget projection, no orphan/stuck episode, valid artifact). | Successful audited trials / eligible triggered trials; report window × trial counts and the exact adapter/database topology. The 160 E4 trials (8 windows × 20) are distinct from the 300-purchase replay; do not call deterministic Mock recovery a real-chain recovery rate. |
| HTTP req/s | HTTP requests observed during the fixed measurement window divided by measured seconds; this includes status polling as well as create requests. | E3 at 200 VUs: 1,319.133 req/s over a 60-second measurement window, after warmup, on one Windows/Docker host with local deterministic dependencies. Not business transactions/s. |
| Completed Episodes/s | Unique Episodes reaching a completed terminal outcome during the measurement window divided by measured seconds. | E3: 72.6 Episodes/s. This is a throughput unit, not a 72.6% purchase-plan baseline. Keep it separate from request throughput and task success rate. |
| HTTP P95 / Episode P95 | HTTP P95 is per-request latency. Episode P95 is create-to-terminal end-to-end latency for completed Episodes. | E3 at 200 VUs: 114.393 ms HTTP P95 and 3.349 s Episode P95. They have different observations and must not be interchanged. |
| “18 Agent tools” | Count distinct callable tool names in the versioned MCP `tools/list` schema, not internal methods or endpoints. | Current audited Runtime surface has 7 named tools in `commerce-runtime/internal/api/server.go`; the repository does not substantiate 18 wallet/DID/payment tools. |

### Existing implementation map

- Shopping fields and scoring are implemented in
  `commerce-runtime/benchmarks/agentshopping/challenge_evaluation.go`:
  `TargetProductRecallAtK`, `FeasibleSKURecallAtK`, `PlanComparison`,
  `ValidPlanRate`, `CorrectDecisionRate`, per-task trials, and provenance status.
- `challenge_independent_oracle.go` adds a separately implemented raw-catalog
  contract check. Its “independent” means independent scorer logic, **not** an
  independently authored task/label set. It is still diagnostic when prompts
  and labels were generated from the same catalog.
- `mysql_http_purchase_replay_test.go` and
  `scripts/audit-agent-shopping-os-process-restart.ps1` cover the 300-purchase
  replay and audit; `scripts/run-e4-local.ps1` plus its MySQL audit cover E4;
  `scripts/benchmark-e3-load.ps1` extracts the k6 measurement-window metrics.
- The evaluator deliberately emits two plan comparisons. `plan_comparison`
  compares the legacy title/category/price baseline (`rankBaseline`) with the
  current candidate/offer treatment (`rankTreatment`) using the same parsed
  intent and generated `gold_skus`. `independent_synthetic_oracle` applies a
  stricter raw catalog contract to independently implemented candidate checks.
  They are different scorers and their deltas must never be combined. Both
  remain non-independent evidence if the suite is generator-coupled.

## Cross-bullet overlap and claim hygiene

- In the résumé text supplied for this audit, Bullet 3 says `286/300` and
  `95.7%`. Those do not arithmetically agree: `286/300 = 95.33%` (95.3% to one
  decimal). The checked-in evidence and audited replay artifacts describe
  `287/300 = 95.67%` (95.7%). Treat the pasted `286` as an unresolved source
  discrepancy; this audit does not edit the frozen résumé or substitute one
  count without reconciling the exact run artifact.
- The fixed 50-task Runtime matrix, the 300-logical-purchase replay, the 160 E4
  crash trials and E3 load test are separate run cohorts. Their repeated
  `zero duplicate settlement` findings are independent observations of one
  invariant, not additive successes or a universal guarantee.
- The 300-purchase replay is selected from the existing locked 500-task
  shopping suite. A separate 500-task lifecycle replay also exists. Therefore
  “500 tasks + 300 purchases” is not 800 unique shopping prompts; report the
  300 as a transaction-replay subset unless task-ID hashes prove otherwise.
- The 50-task matrix's 40/40 approved deliveries and its 50/50 correct expected
  outcomes are different denominators inside the same run. The 10 quote-change
  tasks, safe rejections, and clarifications are not purchases to add to 40.
- The separate 200-Episode benchmark repeats eight synthetic archetypes 25
  times. It validates the harness/fixtures, not 200 unique natural-language
  tasks; do not add its 100/100 paid-delivery result to the 50- or 300-task
  result.
- Public ESCI's 500 queries are a judged-candidate ranking benchmark; they are
  not 500 queries over StablePay's full 24,000-item merchant directory. Its
  `86.96%→88.51%` Exact Recall@5 and NDCG results are separate from synthetic
  purchase-plan metrics.
- The 12 merchant endpoints expose synthetic product listings only; their
  manifest says `purchase_enabled=false`, `payment_ready=false`, and no live
  inventory. Benchmark inventory/price/region are a pinned synthetic overlay.
- Existing chain evidence is Devnet: one 0.99 USDC purchase/delivery and a
  separate one-transaction Kubernetes acceptance. Neither establishes a
  Mainnet payment. The six Minikube nodes share one physical Docker Desktop
  host. ACK deployment is marked user-attested in the evidence inventory, not
  independently demonstrated by a checked-in deployment run artifact.

## Scalable experiment protocol

1. **Smoke (this run):** 30 tasks from the 12 × 2,000 local synthetic catalog;
   hash the catalog, inventory overlay, tasks and scorer; run the same baseline
   and treatment evaluator on oracle intents. This tests ingestion, task/run
   artifact integrity, paired scoring and serialization only. No model calls,
   payment, or MySQL writes. The tasks/labels are generated from the same
   catalog rules and are therefore explicitly diagnostic.
2. **Pilot:** freeze 50 independently authored/paraphrased requests and labels
   before scoring; use a development/validation split and retain every case.
   Human adjudication (or clearly identified AI assistance plus independent
   human adjudication) must decide exact relevance, accepted substitutes,
   feasibility, and PLAN/NO_PLAN/CLARIFY. Verify treatment-blind labels and
   source hashes.
3. **Scale:** only after the pilot scorer and labels pass manual spot checks,
   expand to a disjoint 500-task final set. Tune on development tasks only;
   score the final holdout once with identical inputs for baseline/treatment.
   Include all failures, per-stratum counts, paired outcomes and confidence
   intervals. No threshold, denominator, gold label, or task may be changed
   after outcomes are seen.
4. **Transaction/recovery claims:** use separate predeclared cohorts with
   unique logical IDs, isolated MySQL schema, fixed Mock/Devnet designation,
   frozen expected state and independent SQL audit. A smoke of 5–10 attempts
   validates the harness; it cannot establish the 300-trial rate.
5. **Load claims:** run only after functional checks pass; preserve warmup and
   measurement windows, environment manifest, container image digest, error
   counts and resource snapshots. A 1–10 VU smoke is plumbing validation, not
   the 200-VU result.

## Smoke artifact interpretation

The first smoke must be recorded as `PIPELINE_SMOKE_DIAGNOSTIC`, with suite and
evaluation hashes and raw per-task trials. Passing means only that the local
catalog can be read and the scorer can produce reproducible paired metrics.
Because the synthetic challenge generator derives prompts and labels from the
same catalog/feasibility rules, **no score from this smoke is resume evidence**.

### Executed smoke: 2026-10-13

- Suite: `.local-run/agent-shopping/challenge-suites/resume-metric-smoke-20261013-a/`
  (30 tasks; 22 PLAN, 4 NO_PLAN, 4 CLARIFY; 12 local Merchant APIs; 24,000
  synthetic listings). Task SHA-256:
  `992ea3495b1f667d3ab356443c68e0bab28c5c5cbd3dae34ee36c358018c2677`.
- Evaluation:
  `.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261013T150714.726167600Z/`.
  Status `ORACLE_INTENT_DIAGNOSTIC_NOT_RESUME_EVIDENCE`; 30/30 raw trials;
  no model calls, real payment, or simulation-database writes. Manifest artifact
  SHA-256 checks for metrics, parse results, suite and trials all passed.
- Primary generated-label comparison: baseline `15/22`, treatment `22/22`,
  paired `+7/22` tasks (`+31.82 pp`); outcomes: 15 both valid, 7 treatment-only,
  0 baseline-only, 0 neither. Exact target recall labels were all unreviewed.
  The evaluator's primary baseline is the legacy category/currency/price plus
  title/tag substring matcher sorted by price (`rankBaseline`), **not BM25**;
  this delta cannot be described as BM25→treatment improvement.
- Separate raw-contract oracle: baseline `7/22`, treatment `22/22`
  (`+68.18 pp`); exact-product Recall@5 was `110/1,118` micro (`9.84%`) and
  `16.31%` macro; task Hit@5 was `22/22`. These values are **not independent
  quality estimates**: all 22 PLAN tasks had labels generated from this same
  catalog/overlay, and every task had more than five relevant candidates.
  The 100% Hit@5 therefore says only that at least one generated relevant item
  appeared in the top five, not that the overall relevant set was retrieved.
- Go reported an inability to create its optional upload token under the
  read-restricted C: profile, but continued successfully; build cache and temp
  work were directed to D:. C: had about 9.9 GB free and D: about 1,362.7 GB.
  No files were downloaded by this offline module configuration.

This smoke exposed why the report must retain numerator/denominator, metric
status, and scorer identity together: a seemingly perfect `22/22` task-hit or
plan score coexists with low SKU-level recall and unreviewed labels. It
successfully validates the pipeline; it does not validate the résumé's
illustrative `94.2%` or `72.6%→91.8%` values.

### 50-case scale smoke: 2026-10-13

- Suite: `.local-run/agent-shopping/challenge-suites/resume-metric-pilot-50-20261014-a/`
  (36 PLAN, 7 NO_PLAN, 7 CLARIFY; automatically generated, not an independent
  pilot; 50 distinct prompt strings, with 13 `STANDARD+HARD_ATTRIBUTES`
  scenarios). Task SHA-256:
  `2be4b9472dd9e2a075ae88827788fc74f685a55ccc53b93cf46bff3a6811c8b4`.
- Run: `.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261013T152719.833092900Z/`;
  50/50 trial rows and all four manifest-bound artifact hashes verified.
  Primary generated-label comparison: `21/36→36/36` (`+41.67 pp`); separate
  raw-contract oracle: `6/36→36/36` (`+83.33 pp`). Exact-target Recall@5 was
  `12.04%` micro / `17.03%` macro; task Hit@5 was `36/36`. All exact relevance
  labels were unreviewed; each PLAN had 9–100 relevant listings, all above K=5,
  which explains why task Hit@5 saturated. The result is a
  scale/serialization diagnostic only.
  The 30- and 50-task suites reuse the same local task-ID range, so they are
  scoped by separate suite hashes and must not be merged as one dataset. Future
  cross-suite analysis must key each record by `(suite_hash, task_id)` or emit
  globally unique task IDs. A read-only cross-check found 30 shared IDs but
  zero identical prompts/rows; on matching IDs, the gold-SKU intersections
  sum to 2 and the exact-relevance intersections to 25. Both suites use the
  same catalog and inventory snapshot, so they are not independent datasets.
- One GPT-6 Luna attempt on this suite failed before producing a scored run:
  Hermes exited with code 1 and reported that its shared install lock was not
  writable. No install permissions, auth state, or credentials were changed.
  Because the CLI failed at its install-state check, provider invocation is not
  evidenced; usage/API-call count is therefore **unavailable**, not zero.
- A separate GPT-6 Luna subagent diagnostic then received only the 50 task IDs
  and natural-language prompts (not the gold labels) and returned parsed
  intents. The saved output has 50 rows, 50 unique IDs, no missing/extra IDs,
  and SHA-256
  `cf4bb5c3424970be9c0e3f8e1d13a43783668af8364685b768b9a6d35b591ac3`.
  A local field-by-field recheck found all 50 unique task IDs and exact matches
  for the eight extracted intent fields (including the required-attribute map)
  on `50/50`; the `clarification_required` classification also matched on
  `50/50`, including `7/7` expected CLARIFY cases. `confirmation_required` is
  intentionally absent from the parser schema because the Runtime always
  enforces buyer confirmation; it was not predicted or scored. This is an
  extraction-only blind diagnostic: prompts themselves were generated from
  those same structured intents, so it is not an independently authored
  language-understanding benchmark, a ranked product result, or evidence for a
  94.2% recall / effective-plan claim. The model-call transport/configuration
  and token usage were not captured; do not infer or estimate them. Preserve
  this as `TOKEN_METRIC_UNAVAILABLE` and `MODEL_RUN_PROVENANCE_INCOMPLETE` if
  tabulating the run.
- The next valid quality pilot still requires task prompts and labels prepared
  independently of the scorer and blind adjudication. A 50-case generator run
  does not satisfy that gate, regardless of its sample size or score.
