# Agent Shopping Evaluation Plan

Status: the locked 500-task intent-and-ranking evaluation and persistent
HTTP-to-MySQL lifecycle completed on 2026-10-10. The final lifecycle run
loaded and hash-validated its 24,000-product catalog from 12 running local
Merchant HTTP fixtures (`lifecycle-20261010T163527.512255000Z-http-3d8d77529c31d84e`),
then exercised quotes, buyer consent, stock/price revisions, simulation-only
payment, and SQL audit in an isolated tenant. A separate 9-scenario,
MySQL-backed safety integration covers budget/region/no-substitution
refusals, explicit buyer rejection, stale quotes, idempotent confirmation,
and a durable ambiguity-to-clarification path with a linked follow-up task.
The ninth scenario covers a curated, buyer-visible same-format coffee
substitute, immutable quote details, explicit confirmation, idempotent mock
checkout, and SQL reconciliation. The full nine-scenario opt-in integration
group passed on 2026-10-11 against the dedicated local simulation database; the
test verified per-test tenant cleanup. It still performs no real settlement.
The six-tier concurrent shopping-API load test is now complete; the remaining
evaluation gap is a human-authored paraphrase holdout. These are still
synthetic local results, not production marketplace quality or Merchant
fulfillment. The 300-attempt checkout replay has since been extended through
benchmark-only fulfillment, refund, and inventory compensation; the v2 result
and its narrower claims are recorded below.

## What the existing fixture proves—and does not prove

The ignored `.local-run/simulated-merchants` fixture has 12 isolated Merchant
instances and 24,000 seeded active products (`seed=20261010`). The public
listing endpoint is `GET /api/v1/products?page=N&size=100`; it exposes paging,
not search filters. The product record has SKU, title, description, price,
currency, author, tags, and active status. It does not contain inventory,
shipping/region, a quote revision, or a purchase confirmation protocol.
Purchasing is disabled, the Merchant IDs are synthetic, and no DID registry,
wallet, Payment Service, or Devnet path is configured.

On 2026-10-11, a separate end-to-end smoke passed through the compiled MCP
stdio subprocess, an authenticated in-process HTTP API, and the already-running
dedicated local MySQL simulation schema. One seeded synthetic product was
searched, quoted, separately approved by a test-only synthetic actor, checked
out through deterministic mock payment, fulfilled, and replayed idempotently;
the SQL audit found exactly one confirmation, reservation, order, payment,
fulfillment, and fulfillment attempt, with no retained tenant rows after test
cleanup. This validates one adapter-to-persistence flow, not catalog quality,
human approval, a Hermes/Codex agent session, real Merchant/payment services,
or Devnet settlement. The test exposed and fixed a real MCP bridge defect:
the fulfillment path's `order_id` had also been sent in the strict POST body.
POST routes now forward only their explicitly declared body fields.

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

A full post-change matrix was completed on 2026-10-11 after exact-tuple
inventory batching and request-path query reduction, with a 128-open/128-idle
pool. Tiers `1/10/25/50/100/200` measured `28.99/172.39/270.16/325.52/405.81/
422.66 searches/s` at P95 `47.82/80.37/118.79/192.25/316.90/574.03 ms`.
Every tier passed with 500/500 requests, zero HTTP/business errors, and a
passing SQL audit. The 200-tier used 1,492 pool waits / 40.145 s cumulative
wait. This current matrix is **slower than the earlier baseline** at 50/100/200
workers, so the code changes are not evidence of a latency or throughput win.
Its artifacts are retained under
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T041115.082000900Z-http-concurrent-27368916482f7c30/`.

### 2026-10-11 latency diagnosis and code change

The saved `database/sql` pool deltas show a strong queueing signal: at 100
workers, **1,666 waits / 2.186 s cumulative wait**; at 200 workers, **2,763
waits / 54.918 s**. The test pool had 96 connections while concurrency reached
200. This supports connection-pool saturation as a major contributor; the run
did not collect MySQL CPU, per-query latency, disk-I/O, or Go CPU profiles, so
it does not prove pool wait is the only cause.

The request path had been scanning and ranking the full 24,000-product Merchant
snapshot for every search; a startup-built category/required-format index now
feeds the unchanged ranking function only the relevant partition. The
regression test confirms exact and permitted-substitute ranking matches the
full-scan result. A refreshed short local CPU microbenchmark on 24,000 synthetic
product records measured ranking at **267.771 µs/op** for a full scan versus
**26.334 µs/op** for a 2,000-product partition (about **10.17×** faster in that
isolated function). The absolute difference is only **0.241 ms/request**, so it cannot
explain the end-to-end 348.7 ms P95. The later full matrix above did not show
an end-to-end improvement. Subsequent code also batches exact inventory reads,
reuses the already-loaded Buyer/Task, and uses insert-first idempotency for
ordinary new tasks. Those changes lower measured pool-wait counts at 200, but
the latest throughput/P95 remains below the older run; host CPU, MySQL CPU,
disk-I/O, and per-query latency were not captured during that matrix. The `180
ms` figure in the OCP comparison text is a hypothetical resume example, not a
published OCP benchmark or a StablePay SLO. The refreshed ranking-only sample
is retained at `.local-run/agent-shopping/diagnostics/rank-discovery-20261011T042641Z.md`;
it excludes HTTP, MySQL, live-offer filters, persistence, and concurrency.

After that matrix, the simulation-only parse endpoint gained the response
header `X-Simulation-Catalog-Search-Micros`, timed around the bounded candidate
discovery pipeline (ranking, live inventory/price/region filtering, and final
candidate ordering). The concurrent harness now records its P50/P95 separately
from end-to-end HTTP latency and fails closed if a successful response lacks
the timing header. This instrumentation has only passed offline unit tests so
far; the 2026-10-11 matrix above predates it, and no new load has been run.

The wrapper now accepts a selected subset of the existing concurrency tiers;
the default remains the complete six-tier matrix. Once the isolated MySQL is
already running and its current connection use leaves adequate headroom, a
focused comparison can run only the 200-worker tier:

```powershell
.\scripts\benchmark-agent-shopping-concurrent.ps1 -MySQLPoolMaxOpen 96 -ConcurrencyLevels 200
.\scripts\benchmark-agent-shopping-concurrent.ps1 -MySQLPoolMaxOpen 128 -ConcurrencyLevels 200
```

Each invocation replays 500 warmup plus 500 measured requests, starts and then
stops only the benchmark's 12 Merchant fixtures, and writes a separate run
artifact. The isolated MySQL server previously reported `max_connections=151`;
the wrapper now runs a read-only SQL connection preflight before starting any
Merchant fixture. It compares `@@GLOBAL.max_connections` and
`Threads_connected` with the benchmark pool's currently open connections,
requires an eight-connection reserve, and records the observed counts in the
v2 load manifest. At `max_connections=151`, a 128-connection pool therefore
permits at most 15 other already-connected clients while retaining that
reserve. The load test repeats the check before schema migration or tenant
creation to catch connection changes between preflight and execution. The
wrapper refuses to run with less than 10 GB free on either the system or
repository drive or less than 8 GB of measurable available RAM, checking once
before fixture startup and again immediately before load. It records the
second snapshot in the v2 manifest. It uses the D-drive Go cache
and sets `GOPROXY=off` plus read-only module mode, so the benchmark cannot
download dependencies. If `127.0.0.1:13327` is not listening, it exits before
reading the local DB secret. No pool comparison has been run after the
retrieval-index change yet.

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

- **Target-product Recall@K:** for each feasible PLAN task, exact requested-item
  catalog SKUs returned in discovery top K divided by all independently judged
  exact-target SKUs. These labels are separate from stock, region, and price;
  accepted substitutes have a separate label and never enter target recall.
  Report macro/micro SKU recall and `any exact-target SKU in top K / labeled
  PLAN tasks`. Bind judgments to the catalog snapshot. Keep
  `feasible_sku_recall_at_k` separate; it is not target relevance.
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

For AgentPay recovery comparisons, define **task completion** as a persisted
`FULFILLED` Episode with valid artifact/validation evidence; record a `FAILED`
stop control as a task miss, while auditing payment safety separately. The
staged paired fixture
`scenarios-paired-recovery-v1.jsonl` holds task content, seed, and the injected
invalid-first-delivery fault constant while comparing a deterministic
stop-without-retry control with RuleRecoveryProvider. Since payment may already
be settled before that fault, report settled payments, validated delivery,
refund/compensation, and duplicate settlement effects as separate outcomes.
The one-pair integration smoke test validated the harness. A subsequent
25-pair run completed on 2026-10-11 and passed its independent audit:
stop-without-retry fulfilled `0/25`, RuleRecoveryProvider fulfilled `25/25`;
50/50 injected faults were observed; there were 50 mock PaymentIntents and 50
mock settlements, zero duplicate settlements, and zero collection errors. The
stop control's 25 settled mock payments had no delivery or refund/compensation.
The run repeats one deterministic task profile, so the `100 pp` within-fixture
delta is descriptive, not a generalizable shopping/reliability estimate or
resume claim. Artifacts: `.local-run/agentpay-paired-recovery-v1-20261011/`.

The challenge report's `target_product_recall_at_k.status` distinguishes
`AVAILABLE_UNVERIFIED_RELEVANCE_LABELS` from
`AVAILABLE_DECLARED_DOUBLE_REVIEW_NOT_EXTERNALLY_VERIFIED`. The latter means
task records contain structurally complete human-review declarations and
hashes; it still cannot prove that people actually authored or blindly reviewed
the packet. `target_product_task_hit_at_k` carries the same status because it
uses the same relevance labels. Numeric recall or task-hit rates over
unverified labels are diagnostic only, not independent product-relevance
results. The task-hit status is emitted by new reports; historical ignored
`.local-run` artifacts are not rewritten.

The present suite is a generated fixture-consistency evaluation, not an
independent 500-task holdout. `GenerateTasks` selects viable targets before
constructing the prompt and gold labels; the evaluator then verifies gold
offers against the same eligibility dimensions (format, budget, region, and
stock). The latest locked run therefore reports B1 Recall@20 `596/596` and
valid snapshot plans `500/500`, against B0 `389/500`—not the example
`94.2%`, `72.6%`, or `91.8%`. It contains no independent natural-language
holdout or systematic infeasible/contradictory tasks. The HTTP lifecycle is a
separate 500-task run: all reached a terminal status, but only `483/500` were
mock-settled and `17/500` were mock-declined. A follow-up, fault-injected
300-logical-purchase replay run is now complete (details below); it reuses
tasks from the locked generated suite and is not an independent shopping
holdout. Neither lifecycle run used a real payment provider or Devnet.

## Constraint-first challenge evaluator (implementation, not result evidence)

`agentshopping.EvaluateChallengeTasks` and `ChallengeTask` are a separate
scoring path; the historical 500-task suite and its metrics are unchanged.
It supports three expected dispositions: `PLAN`, `NO_PLAN`, and `CLARIFY`.
Plan task recall is task-level (`any accepted gold SKU in top K`) with only
feasible plan tasks in the denominator. Valid-plan rate is also measured over
all feasible tasks, so parse failures and empty candidate sets count as
failures. Correct no-plan requires both the exact complete intent and no
eligible offer in the pinned price/stock/region snapshot. Correct
clarification requires an explicit question, exact preservation of the known
partial intent (including the buyer-confirmation requirement), all
label-designated missing fields to remain unfilled, and field-specific lexical
cues for every blocking field. Generic filler such as “please clarify your
shopping requirements” is not enough. This cue check is a conservative lexical
proxy, not a semantic judge; naturalness and semantic adequacy still require
human review. Clarification tasks are rejected if the only missing field is
the optional `preferred_style`. Each disposition has its own denominator;
parse failures remain counted, and rates carry Wilson 95% intervals. The
report separates nonempty-question rate from
`clarification_question_missing_field_cue_rate`. The evaluator also reports
`correct_decision_rate_all_tasks`:
correct PLAN outcomes (valid purchase plans), correct NO_PLAN abstentions, and
correct CLARIFY responses divided by every task in the mixed suite. Parse
failures count as incorrect decisions. This all-task rate is a decision-quality
summary, not a purchase success rate; always publish the PLAN-only valid-plan
rate and the separate NO_PLAN/CLARIFY rates beside it so a safe abstention
cannot be misrepresented as a completed purchase.

The runtime obtains a fixed 100-product discovery window, applies current
stock/region/price and hard-attribute filters across that window, then returns
at most K candidates. Challenge evaluation mirrors that behavior for plan
generation; `Recall@K` is still computed only over the first K products before
business filters. Thus a feasible relevant product ranked 21–100 may rescue a
plan without being counted as a Recall@20 hit. The run manifest records both
the recall cutoff and runtime candidate-window size.

The evaluator validates gold feasibility against the supplied snapshot and
rejects mislabeled no-plan tasks when any candidate is currently purchasable.
The `agentshopping-sim evaluate-challenge` CLI accepts local challenge JSONL,
validates labels against the pinned catalog before model calls, and stores
hashed run artifacts. A 500-task, attribute-constrained local draft now exists
at `.local-run/agent-shopping/challenge-suites/draft-auto-v3`; it is
auto-generated and unreviewed, not a holdout or resume claim. Its current
`GoldSKUs` contain every feasible product matching the parsed hard constraints,
while the evaluator's task-level Recall@K searches that same filtered set. With
at most 18 gold candidates and K=20, this can be mechanically saturated; do
not report that recall metric until retrieval-before-filtering is evaluated
against independently adjudicated relevant-product labels. A publishable
holdout still needs independently authored prompts, adjudicated accepted
products, a frozen task/catalog hash, and a model run after the prompt/ranker
is frozen.

An audit of the historical 350-task GPT-6 Luna challenge run found nine PLAN
requests whose text explicitly required `冷萃包` but whose raw model output left
`required_format` empty and triggered clarification. The parser now has a
conservative deterministic recovery for this class: it fills a missing format
only when an explicit format clause in the buyer text contains exactly one
allowed catalog format/alias. The raw model result remains preserved in batch
artifacts and the recovered field is listed in `canonicalized_fields`; it does
not inspect gold labels, SKU lists, or inventory. Regression tests cover the
observed clause, aliases, absent/ambiguous clauses, and affirmative-vs-negated
format conflicts. The same recovery runs before a partial-intent clarification,
so an explicit format remains fixed while the Agent asks only for another
missing field. This was a post-run parser correction on generated development
data; old metrics remain unchanged, and no quality improvement is claimed
without a newly frozen independent holdout.

The follow-up oracle diagnostic `shopping-challenge-20261010T191847.435777800Z`
ran all 500 draft tasks using each task's gold intent and made no model/API
calls. B0 produced 366/500 valid plans (73.2%); B1 produced 464/500 (92.8%),
for a paired +19.6 percentage-point delta (task-bootstrap 95% CI: +15.0 to
+24.4 points). Feasible-SKU Recall@20 was 42.1% micro / 58.7% macro; task
hit@20 was 92.8%. These are evaluator/ranker diagnostics on auto-generated,
unreviewed gold labels, not model quality, target-product relevance, or resume
evidence. In particular, the oracle bypasses intent parsing, and the paired
plan delta must not be presented as the advertised 72.6% → 91.8% claim.

A shortlist audit of the saved per-task trials adds a useful difficulty
diagnostic: every task filled all 20 discovery slots; the 500 top-20 lists
contained 504 entries in the generated feasible GoldSKU sets out of 10,000
retrieved entries. At least one gold SKU appeared in 464/500 tasks, while all
gold SKUs appeared in 189/500. This shows that the evaluator is not merely
returning an unfiltered all-gold list, but it does not establish semantic
relevance: the non-gold count is defined against the same synthetic snapshot
and generated labels. Treat it as shortlist/constraint-filtering load only.

A separate mixed-decision draft, `.local-run/agent-shopping/challenge-suites/draft-mixed-v2`,
was generated from the same catalog snapshot with 350 `PLAN`, 75 `NO_PLAN`,
and 75 `CLARIFY` tasks. The no-plan group contains 20 individually-feasible
but jointly-unsatisfiable attribute pairs and 55 one-minor-unit-below-minimum
price boundaries; clarification prompts omit item, format, and style. Its task
hash is `sha256:2cf1359ddfe41b6979c2dcd8320fc9521b0a06d6b4d59fae91035079be364c7a`.
This adds decision coverage, not independent labels: the entire suite is still
algorithmically generated and marked `AUTO_GENERATED_UNREVIEWED_DRAFT`.

The mixed draft was evaluated by GPT-6 Luna in run
`shopping-challenge-20261010T200737.945097000Z` (task SHA-256
`sha256:2cf1359ddfe41b6979c2dcd8320fc9521b0a06d6b4d59fae91035079be364c7a`).
On the 350 generated PLAN tasks, B0 produced 249/350 valid plans (71.1%) and
B1 produced 321/350 (91.7%), a paired improvement of 20.6 percentage points.
B1 valid-plan Wilson 95% CI was 88.4%–94.2%; the paired delta task-bootstrap
95% CI was +15.1 to +26.0 percentage points. B1 task-hit@20 was also 321/350
(91.7%); feasible-SKU Recall@20 was 41.0% micro / 57.5% macro (task-bootstrap
95% CI: 53.7%–61.2%). The other generated
decisions were correct on 75/75 NO_PLAN and 75/75 CLARIFY tasks, with zero
unexpected plans on non-PLAN tasks and zero parse failures. Complete-intent
exact match was 416/425 (97.9%). These are GPT-6 Luna results, not oracle
results, but all prompts and labels remain auto-generated from the same
synthetic catalog and require independent human review; they are not a
human-authored holdout, independently judged target-product recall, or resume
claim. In particular, the 91.7% plan rate must not be relabeled as the example
72.6% → 91.8% result.

Hermes reported 90,173 input and 33,902 output tokens across 10 CLI batches.
API-call count is `TOKEN_METRIC_UNAVAILABLE`; temperature, top-p, and
max-token settings were unreported. The local manifest and report contain
prompt/template/response hashes and no API key. The run made no payment calls
and wrote no simulation database state. Artifacts are local-only under
`.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261010T200737.945097000Z/`.

The evaluator also supports `--replay-parse-results <path>` to re-grade a
previous run's hash-bound raw structured outputs with the current local parser
and ranker, without invoking Hermes or a model. It verifies the prior run
manifest and task/catalog/inventory/merchant hashes, retains source model and
response provenance, and writes a separate artifact explicitly marked as an
offline diagnostic, not resume evidence. Replaying the saved outputs above
after parser and candidate-window changes produced
`shopping-challenge-20261010T220237.471963000Z`: B0 valid plans 254/350
(72.6%), B1 350/350 (100%), paired delta +27.4 pp (task-bootstrap 95% CI
+22.9 to +32.0 pp), and feasible-gold task-hit@20 329/350 (94.0%). Feasible
SKU Recall@20 was 41.9% micro / 59.0% macro. These are a post-hoc offline
replay of generated, unreviewed tasks—not a new model evaluation or
independent holdout result. The 94.0% is a hit rate against generated
snapshot-feasible gold SKUs, **not** target-product Recall@20; the latter is
`NOT_AVAILABLE_NO_INDEPENDENT_RELEVANCE_LABELS` (0/350 labeled plan tasks).
The 100% B1 plan rate must not be promoted as resume evidence or represented as
the proposed `72.6% → 91.8%` result.

### 2026-10-11 mixed-v6 oracle diagnostic

The next 500-task draft (`draft-mixed-v6`, task SHA-256
`sha256:c9c7c1ccc7ff4e4cffb4a59abe96e51a1d5fc5fb11a24c2cbc35840d908ad6f8`)
was evaluated through the local Merchant HTTP catalog with `--oracle-intents`;
no model/API calls or real payments were made. The command validated and read
the 12 × 2,000-product snapshot and wrote its hashed evaluation artifacts under
`.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T054628.386073000Z/`.
The independent read-only difficulty audit passed. The comparison was B0
`206/350` valid plans (58.9%) versus B1 `350/350` (100%), a paired `+41.1 pp`
delta (95% task-bootstrap CI `+36.0` to `+46.3 pp`). B1 feasible-generated-gold
task-hit@20 was `292/350` (83.4%; Wilson 95% CI 79.2%–87.0%); feasible-SKU
Recall@20 was 38.9% micro / 51.9% macro. Correct no-plan was `75/75`, while
clarification was `0/75`; consequently correct decisions across all tasks were
`425/500` (85.0%). The oracle supplies the partial intent for CLARIFY tasks,
not a natural-language follow-up question, so the clarification miss is an
expected limitation of this diagnostic mode—not a model result.

This run does **not** establish target-product Recall@20:
`target_product_recall_at_k.status` is
`NOT_AVAILABLE_NO_INDEPENDENT_RELEVANCE_LABELS` (`0/350` plan tasks labeled).
The tasks and feasible-gold sets are still auto-generated from the same
synthetic catalog and are unreviewed. The 100% plan rate measures the
constraint/filter/plan path after gold intent is supplied; it is not model
intent quality, not an independently judged valid-purchase rate, and not
resume evidence. No database transaction rows were created by this oracle
evaluation; it is separate from the already-passing nine-scenario HTTP-to-
isolated-MySQL lifecycle integration recorded at the top of this plan. The
12 Merchant fixtures were stopped after the run. Disk/RAM guard snapshots
were: C free 12.99 GiB before and after, D free 1,373.23 GiB before / 1,373.19
GiB after (about 40 MiB used for build/cache/artifacts), and available RAM
9,894 MiB before / 9,653 MiB at the end of evaluation while the fixtures were
up / 9,960 MiB after fixture shutdown. No additional dataset was downloaded
for this run.

New challenge evaluations now write `stablepay.agent-shopping-challenge-run.v4`
manifests that bind the exact `suite.jsonl`, `parse-results.jsonl`,
`trials.jsonl`, and `metrics.json` bytes, plus optional saved model response
and stream files. Replay verifies those hashes before decoding stored outputs;
v2/v3 runs remain readable for compatibility, but their historical outputs do
not gain retroactive artifact hashes.

### Difficulty audit of the mixed generated suite

A read-only audit of the frozen `draft-mixed-v2/tasks.jsonl` and the offline
replay trials explains why `350/350` is not persuasive evidence of broad
shopping quality:

Reproduce the audit from the repository root. It binds the exact task-file
SHA-256 to both manifest and metrics, reconciles trial IDs/dispositions and
trial-derived metric numerators, then prints SHA-256 values for the four input
artifacts:

```powershell
.\scripts\audit-agent-shopping-difficulty.ps1 `
  -TasksPath .local-run\agent-shopping\challenge-suites\draft-mixed-v2\tasks.jsonl `
  -TrialsPath .local-run\agent-shopping\challenge-evaluations\shopping-challenge-20261010T220237.471963000Z\trials.jsonl `
  -ManifestPath .local-run\agent-shopping\challenge-evaluations\shopping-challenge-20261010T220237.471963000Z\manifest.json `
  -MetricsPath .local-run\agent-shopping\challenge-evaluations\shopping-challenge-20261010T220237.471963000Z\metrics.json
```

For this run the printed trial, manifest, and metrics SHA-256 values are
`e58ca1e31fad74f886dd0aef2cdc00f3b481e7767b99ac88cddfa01aa4c0bdec`,
`38c1526ea4679326b33cd2b2cd0c89bb5182bfc5fcab3b1f31ccd4f612cb8474`, and
`d6abe7ca54e67140d36fd5c8e48078b14230e0d33d7c24e8f7367e9721fffeda`,
respectively. The task-file hash is the same frozen SHA-256 recorded above.
The read-only audit was executed successfully against these artifacts under
Windows PowerShell 5.1 on 2026-10-11 (exit code 0); the script is saved as
UTF-8 with BOM so the Chinese prompt-clause check parses correctly in 5.1.

- The 350 PLAN prompts are instances of the same generated request template
  with slot substitutions and one appended hard-attribute clause. Every prompt
  contains the canonical item (`350/350`), almost all contain the canonical
  format (`347/350`) and style (`345/350`), and all expose exactly two
  machine-keyed attribute/value requirements in the prompt. The generator
  chooses these attributes from a known feasible target, then derives
  `gold_skus` from the same synthetic catalog and feasibility predicates.
- The 75 NO_PLAN prompts are only two generated traps: 55 set budget one minor
  unit below the minimum feasible offer and explicitly say not to raise it;
  20 state a generated pair of individually valid but jointly unsatisfiable
  attributes. The 75 CLARIFY prompts all omit the same three fields—item,
  format, and style—in the same template. None tests a follow-up turn.
- The replay model output matched all 425 complete intents exactly. All 350
  PLAN tasks had 64–100 first-stage candidates (median 83); after hard filters,
  the returned feasible set had 1–18 candidates (median 2, p95 6). On 21 tasks
  no generated feasible gold SKU was in the raw discovery top 20, yet B1 still
  generated a valid plan because the runtime-matched 100-item candidate window
  is filtered before the final 20-result cap. Thus `94.0%` task-hit@20 and
  `100%` valid-plan rate describe different stages, and the latter is largely
  a check that parsed constraints reproduce the generator's own labels.

The plan rate is therefore a deterministic synthetic pipeline diagnostic, not
proof that the tasks are all trivially easy, not independent target relevance,
and not a generalization score. The next model run remains gated on the
human-authored, double-reviewed holdout below; improve that suite's paraphrase,
ambiguity, constraint-conflict, and multi-turn coverage before running it.

A read-only recomputation from the archived per-task trials for
`shopping-challenge-20261010T220237.471963000Z` (an offline replay of the
stored GPT-6 Luna parse results on `draft-mixed-v2`, with no new model calls)
found **500/500 correct decisions**: 350 PLAN, 75 NO_PLAN, and 75 CLARIFY.
All 75 clarification prompts shared the same `item + format + style` missing
field pattern. This is direct evidence that the generated suite was too easy
to support a meaningful quality claim; it is not a résumé result. The newer
`draft-mixed-v3` only improves clarification-pattern structure and has not
been model-evaluated. The evaluator now also emits
`correct_decision_rate_all_tasks` (including parse failures as misses), while
keeping it distinct from the PLAN-only valid-purchase-plan rate.

#### Clarification draft diversity improvement (generator only)

The mixed-draft generator now rotates through six missing-field patterns:
item only, format only, style only, and each two-field combination. It keeps
the other stated item constraints in the prompt and label, uses six surface
openers, and records the missing-field pattern in the scenario stratum. For a
75-task clarification stratum, this produces a balanced 12/13 tasks per
pattern. Unit tests verify deterministic coverage, agreement between prompt
and missing-field labels, and that omitted source values do not leak into the
prompt. The existing `draft-mixed-v2` files and their evaluation metrics are
unchanged; no new 500-task artifact or model run was produced. These generated
variations remain diagnostic material, not human-authored holdout evidence.

The revised generator produced `.local-run/agent-shopping/challenge-suites/draft-mixed-v3`
from the same pinned synthetic snapshot. Its read-only structural audit found
500/500 distinct prompt strings; the `350 PLAN / 75 NO_PLAN / 75 CLARIFY` mix
was unchanged, while the 75 clarification cases covered all six missing-field
patterns at 12 or 13 tasks each. Comparing each clarification against its full
source intent found zero omitted-value leaks and zero missing known values.
Snapshot validation passed during generation; `go test
./benchmarks/agentshopping -count=1` passed offline. The task file is 455,828
bytes with SHA-256
`sha256:86a90de2f99e3d238fce014cf3250b922c9f14eb54f9ff7a0d3bcc43bae42856`;
the manifest SHA-256 is
`sha256:f00a8611502c2dfb60208454c46ee22648d85d3037e56e998a15986cb7003fb0`.
Re-run the local read-only audit with:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\audit-agent-shopping-draft.ps1 `
  -DraftDir .\.local-run\agent-shopping\challenge-suites\draft-mixed-v3 `
  -SourcePlanDraftDir .\.local-run\agent-shopping\challenge-suites\draft-auto-v3
```

It exits with `PASS_STRUCTURE_ONLY_NOT_HUMAN_REVIEWED` and repeats the hashes
and structural counts; it does not grade a model or promote the draft to a
holdout.
The 12 loopback Merchant fixtures used for local catalog ingestion were stopped
immediately afterward. No model evaluation or pressure test ran. The prompts
are still generator-authored from the same catalog/feasibility rules; unique
strings and structural variety do not establish human authorship, semantic
hardness, independent relevance, or resume-quality performance.

#### Direct answer-cue removal (generator v4, 2026-10-11)

A targeted read-only prompt audit of the stored v3 suite found direct
disposition leakage in **150/500 prompts**: all 75 NO_PLAN prompts explicitly
said to report no plan/not place an order, and all 75 CLARIFY prompts told the
model to ask for missing information. The historical 500/500 correct-decision
result therefore cannot support a decision-quality claim. The earlier v3
structural PASS did not test this property; the draft audit now reports these
lexical cues separately and marks v3 `FAIL_DIRECT_DISPOSITION_CUES`.

The generator now removes those outcome instructions. NO_PLAN budget-boundary
prompts rely on the same budget and purchase wording as PLAN tasks;
attribute-conflict prompts state constraints without telling the agent what
conclusion to reach; CLARIFY prompts ask an ordinary open shopping question
while leaving required details absent. The shared attribute-clause stripping
helper was updated so price-boundary cases cannot inherit stale attribute
text. Unit tests assert that the direct cue strings are absent.

The new local draft `.local-run/agent-shopping/challenge-suites/draft-mixed-v4`
has 500 distinct prompts (350 PLAN / 75 NO_PLAN / 75 CLARIFY), 22 scenario
strata, six balanced clarification missing-field patterns, zero literal leaks
of omitted clarification values, and zero matches for the audit's fixed direct
answer-cue patterns. Task SHA-256 is
`sha256:0b3f18c12980968b066245781f90100c9a85187fd02e55515807b6e62ca6695c`;
the task file is 434,653 bytes. Its original structural audit was
`PASS_STRUCTURE_ONLY_NOT_HUMAN_REVIEWED`; that older gate did not test whether
soft preferences were incorrectly required. Under the updated audit, v4 fails
the optional-style gate. The keyword gate cannot prove semantic independence
or naturalness. The catalog loader briefly started the
12 existing loopback fixtures for the required HTTP snapshot read and stopped
all 12 afterward. No model run, load test, dataset import, image pull, or
dependency download occurred. Do not run a model against this generated draft:
the independent human-authored and double-reviewed holdout remains the gate,
and v4 is not resume evidence.

#### Soft-preference label correction (generator v5, 2026-10-11)

Reviewing the v4 labels exposed a second source of artificially low validity:
`preferred_style` is a soft preference, but v4 treated 13 “only style missing”
tasks as mandatory CLARIFY. An agent that safely returned offers using the
stated item, required format, region, budget, and inventory should not fail
solely because the buyer did not state an optional style. The generator now
uses five decision-critical clarification patterns (item; required format;
both; item plus style; format plus style), 15 tasks each in the 75-task
CLARIFY stratum. A style omission never appears alone as a blocking
clarification.

To measure over-clarification rather than reward asking about every preference,
the new draft has a PLAN control stratum: exactly one fifth of PLAN tasks omit
the style preference while retaining their other constraints. Their feasible
SKU labels are recomputed without the preference. A matching synthetic
`tasting_notes` value is removed if it would quietly reintroduce the omitted
style as a hard attribute. The generator tests this rule, and the read-only
audit checks the 1-in-5 count plus intent/prompt/attribute leakage.

The first implementation was saved as
`.local-run/agent-shopping/challenge-suites/draft-mixed-v5` (500 tasks: 350
PLAN, 75 NO_PLAN, 75 CLARIFY; 70 PLAN tasks omit style). A stronger audit found
two residual word-overlap leaks where the requested item name itself contained
the omitted style label. The updated gate correctly marks v5
`FAIL_OPTIONAL_STYLE_PLAN_COVERAGE`; that artifact is preserved and is not a
valid draft.

The next generator revision excludes candidates whose remaining prompt or
hard-attribute clause contains the omitted style value. It is saved as
`.local-run/agent-shopping/challenge-suites/draft-mixed-v6`: 500 unique prompts,
350/75/75 decision mix, 70 style-unspecified PLAN controls, five clarification
patterns with 15 tasks each, zero fixed direct-answer cues, zero style-only
CLARIFY tasks, and zero audited style leakage. Task SHA-256:
`sha256:c9c7c1ccc7ff4e4cffb4a59abe96e51a1d5fc5fb11a24c2cbc35840d908ad6f8`;
size 432,891 bytes. Its audit is
`PASS_STRUCTURE_ONLY_NOT_HUMAN_REVIEWED`—not a quality score or resume claim.
All earlier drafts remain unchanged. No model evaluation was run; the
independent human-authored, double-reviewed holdout is still required.

A fresh read-only audit under Windows PowerShell 5.1 on 2026-10-11 reproduced
that structural PASS for v6:
500/500 prompt strings were unique; decision quotas, the 70 style-unspecified
PLAN controls, five clarification patterns (15 each), zero fixed answer-cue
matches, and zero audited style leakage all reconciled. This does not establish
semantic diversity. Source inspection shows the PLAN prompts still originate
from the single slot-filled sentence template in
`commerce-runtime/benchmarks/agentshopping/domain.go::GenerateTasks`; NO_PLAN
cases are generated by constructing an attribute conflict or a one-minor-unit
price boundary; CLARIFY uses a fixed opener list and missing-field patterns.
Thus unique product/category substitutions are not 500 independently authored
shopping scenarios. A heuristic exact-shape normalization (replacing gold
item/format/style/attribute values, mapped category/region names, and
currency-adjacent amounts) collapses the 500 v6 prompts to **94 surface
shapes**: PLAN 57/350 (largest exact-shape cluster 27), NO_PLAN 7/75 (largest
cluster 53), and CLARIFY 30/75 (largest cluster 3). This is a reproducible
template concentration diagnostic, not semantic clustering or a difficulty
score; unrecognized aliases can remain separate shapes. It is included in the
read-only draft audit output. Reproduce with:

```powershell
.\scripts\audit-agent-shopping-draft.ps1 `
  -DraftDir .\.local-run\agent-shopping\challenge-suites\draft-mixed-v6 `
  -SourcePlanDraftDir .\.local-run\agent-shopping\challenge-suites\draft-auto-v3
```

After the v6 audit, the diagnostic generator code was revised to select
snapshot-compatible base tasks in a fixed seeded order for the three
preregistered NO_PLAN mechanisms (price infeasibility, an unserved valid region,
and an empty hard-attribute intersection). Price deficits now vary
deterministically rather than always sitting one minor unit below the minimum.
A 40-task synthetic unit fixture checks two examples of each mechanism and
confirms that a snapshot without an unserved region is rejected; generation
fails closed when the requested quota cannot be supported. This code change did
not modify v6 or create/evaluate a v7 draft, and does not establish semantic
difficulty or holdout quality.

The often-cited 500/500 correct-decision result belongs to an offline replay of
stored GPT-6 Luna outputs on the older v2 draft, not v6. A targeted fixed-pattern
audit found an explicit no-plan cue in all 75 v2 NO_PLAN prompts; all 75 v2
CLARIFY prompts used the same all-fields-missing pattern. The later v3 draft had
explicit clarification cues in all 75 CLARIFY prompts as well. Do not attribute
the old score to v6 or present either draft's result as model quality. The v6
task hash remains
`sha256:c9c7c1ccc7ff4e4cffb4a59abe96e51a1d5fc5fb11a24c2cbc35840d908ad6f8`.

The first model attempt exposed a scoring defect: clarification questions from
non-CLARIFY tasks were added to a numerator whose denominator included only
expected CLARIFY tasks, allowing a Wilson interval to become NaN and preventing
JSON report publication. The numerator is now restricted to expected CLARIFY
tasks, with a regression case for over-clarification; the full package test
passes. A separate successful Oracle run on the same mixed suite remains
available for ranker comparison, but must not be conflated with the model run.

A narrow benchmark-only
substitution rule is now implemented for six explicitly curated synthetic
single-origin coffee items in the same required format. Candidate results and
immutable quotes identify the substitute and require renewed buyer confirmation;
unknown items and other categories fail closed. The nine-scenario MySQL
integration group passed, including this scenario, against the isolated
simulation database. This does not modify production Merchant/payment services
or establish generic semantic substitution quality.

## Independent human-authored holdout: required before resume claims

The existing 500-task files are generated from the same synthetic catalog and
are not a human-authored holdout. Public benchmark papers/datasets remain
methodology references only; they are not silently merged into the StablePay
suite or used as StablePay scores. A pinned ShoppingComp reference snapshot is
stored under the ignored `.local-run/agent-shopping/reference-data/` directory.
The read-only audit `scripts/audit-shoppingcomp-catalog-fit.py` verifies its
file hashes and checks expert product names against the current local Merchant
titles. On the 2026-10-11 snapshot, 194 English/Chinese ground-truth task
instances contained 472 expert product references; exact normalized-title
coverage was 0/472, with 0/194 tasks having an exact-title hit across 24,000
active local products. This is a catalog-fit diagnostic, **not** a semantic
recall score: the open-world consumer-product references do not match the
current synthetic, category-limited Merchant fixture. Keep ShoppingComp as a
scenario-design reference unless a reviewed product/catalog mapping is added.
No further model evaluation should be run until an independently authored and
adjudicated suite exists.

Use the following frozen 500-task protocol rather than selecting only easy
purchasable prompts. The official holdout has 350 feasible PLAN tasks (the
valid-plan denominator), 75 NO_PLAN tasks, and 75 CLARIFY tasks. PLAN is split
across seven challenge strata; NO_PLAN and CLARIFY each cover three failure or
ambiguity types. The prompt JSONL uses opaque `S01`–`S13` IDs so reviewers do not
see the expected disposition in the stratum name. The coordinator keeps the
mapping below; all counts and weights are fixed before authors write prompts.
Report every stratum separately, alongside the aggregate, so this deliberately
balanced challenge mix is not mistaken for marketplace prevalence.

| Stratum | Tasks | Expected disposition | Prompt-writing focus |
| --- | ---: | --- | --- |
| S01 | 50 | PLAN | Exact requested product with ordinary wording and enough context |
| S02 | 50 | PLAN | Multiple simultaneous hard product attributes |
| S03 | 50 | PLAN | Product/format aliases and close variant distinctions |
| S04 | 50 | PLAN | Budget, quantity, and total-cost boundary cases |
| S05 | 50 | PLAN | Multiple soft preferences requiring candidate ranking |
| S06 | 50 | PLAN | Explicitly permitted substitutes, separate from exact-target relevance |
| S07 | 50 | PLAN | Current offer feasibility across stock, region, and price |
| S08 | 25 | NO_PLAN | No feasible offer under the stated budget |
| S09 | 25 | NO_PLAN | No offer can serve the required delivery region |
| S10 | 25 | NO_PLAN | No catalog variant satisfies the conjunction of hard constraints |
| S11 | 25 | CLARIFY | Product identity is missing or under-specified |
| S12 | 25 | CLARIFY | A required attribute/choice is missing |
| S13 | 25 | CLARIFY | Buyer constraints are ambiguous and need a targeted question |

The strict `--require-human-review` evaluator gate enforces the exact 500-task
count, each stratum quota, and the disposition assigned to that stratum before
loading the catalog or calling a model. Its run manifest embeds the complete
protocol definition and a canonical SHA-256 fingerprint; replay validates that
fingerprint and the recorded scenario counts. This protocol is a prospective
design, not evidence that the prompts or labels already exist. Scenarios such as price
changes after a quote, stale confirmation, checkout idempotency, and payment
replay remain in the separately measured HTTP-to-MySQL lifecycle suite; they
must not be implied by PLAN-stratum scores.

For each task, annotate the complete intent and expected decision (`PLAN`,
`NO_PLAN`, or `CLARIFY`). For `PLAN`, `relevant_skus` contains exact requested-
item listings only, judged against the active catalog independently of live
stock, region, and price. Separately provide `accepted_substitute_skus` as an
explicit list of buyer-accepted alternatives (use `[]` when none are accepted);
alternatives never count as exact-target relevance. Set `gold_skus` to exact
products and/or explicitly accepted substitutes that are feasible for the
pinned inventory/region/budget snapshot; a substitute can validate a plan only
when it appears in `accepted_substitute_skus`. For
`NO_PLAN`, verify that no offer satisfies all stated hard constraints in the
snapshot; a relevant catalog product may still exist but be unavailable or
over budget. For `CLARIFY`, name the missing fields and preserve all known
constraints. Independently supply `buyer_default_region` and
`buyer_budget_minor` as session context; never copy these from `gold_intent`.
The parser receives only the prompt and these context fields, not the gold
intent or SKU labels. Bind relevance to the catalog hash and feasibility to
both catalog and inventory hashes.

Prompt authors must not see generated prompt templates, model output, or B0/B1
rankings. Two independent labelers annotate each task; a separate adjudicator
resolves disagreements. Freeze the task file and hashes before model execution.
Keep the prompt author, reviewer A, reviewer B, and any adjudicator as distinct
stable pseudonymous IDs. The prepared packet directory contains two blind prompt
packets, a shared `reviewer-catalog.jsonl`, and a packet manifest. The catalog is
rebuilt from the same local synthetic Merchant/inventory snapshot used by
evaluation; it includes listing text/attributes and per-region listing/live-offer
price, eligibility, stock, and revision, but excludes merchant DID/wallet data
and internal substitution-policy groups. Both reviewers receive the same catalog
reference plus only their own prompt packet. The packet manifest records the
catalog file hash, product/offer row counts, snapshot hashes, and source-manifest
hash. Prepare packets with the 12 local Merchant fixtures running:

```powershell
agentshopping-sim prepare-challenge-review --prompts <master-prompts.jsonl> --manifest .local-run/simulated-merchants/manifest.json --out-dir .local-run/agent-shopping/challenge-suites/<new-packet-dir>
```

The finalized review bundle contains ten files:
`prompts.jsonl`
(one blind human-authored prompt and independent buyer context per task),
plus `reviewer-a-prompts.jsonl` and `reviewer-b-prompts.jsonl` (the exact
metadata-stripped, independently shuffled packets sent to each reviewer),
`reviewer-catalog.jsonl` (the exact product/offer snapshot supplied to reviewers),
`reviewer-a.jsonl` and `reviewer-b.jsonl` (separate structured gold annotations),
`adjudications.jsonl` (only disagreements; an empty file is valid),
`tasks.jsonl` (final merged labels), `review-audit.json` (snapshot and input
hashes), and the exact `packet-manifest.json` created before review (packet,
catalog, and source hashes).
Each prompt packet exposes only a random per-reviewer task alias, prompt, buyer
region, and buyer budget; it omits stratum, prompt-author ID, and
authorship/blindness flags. Give each reviewer the shared catalog reference and
only their own prompt packet, not the master prompts, protocol mapping, other
reviewer's annotations, model outputs, or rank lists. Reviewers
return annotations using the packet's random `task_id`; finalization binds those
aliases back to exact master prompt/context rows and hashes the packet contents.
This improves blinding hygiene but still cannot prove what a person saw outside
the retained files. Freeze reviewer inputs before seeing model output. Label
`gold_intent` with the intent fields in `Intent`; every reviewer must explicitly
supply `accepted_substitute_skus` (an empty array means none accepted). For PLAN,
separately provide feasible `gold_skus` and exact-target `relevant_skus`.

Each input is one JSON object per line. Master prompt rows contain `task_id`,
`scenario`, `prompt`, `prompt_author_id`,
`prompt_written_without_generated_template: true`, `model_blind: true`,
`buyer_default_region`, and `buyer_budget_minor`. Each reviewer row contains
the random `task_id` from that reviewer's packet, a stable `reviewer_id`,
`model_blind: true`,
`labels_frozen_before_model_run: true`, and `label` with
`expected_disposition`, `gold_intent`, and an explicit `accepted_substitute_skus`
(use `[]` when no alternative is accepted), plus `gold_skus` and
`relevant_skus` for PLAN or `required_missing_fields` for CLARIFY. Adjudication rows contain
`task_id`, a distinct `adjudicator_id`, the complete `final_label`, and a
non-empty `rationale`. These are data-format requirements, not substitutes for
actually performing independent review.

`review-audit.json` records SHA-256 values for both exact reviewer prompt
packets as well as master prompts, annotations, adjudications, tasks, and the
catalog/inventory snapshot. The separately retained packet manifest binds the
reviewer-visible catalog file to the same catalog/inventory/source hashes and
the exact two prompt packets. It also reports exact agreement for complete labels,
disposition, and intent; Cohen's kappa for the three-way disposition label;
and exact-set plus macro/micro Jaccard agreement for feasible `gold_skus`,
stock-independent exact-target `relevant_skus`, accepted substitutes, and
clarification missing fields. Feasible-gold and exact-target SKU agreement
include only tasks both reviewers classified as PLAN; missing-field agreement
includes only tasks both classified as CLARIFY. Accepted-substitute agreement
includes every task because reviewers explicitly label that set for every
disposition. Zero denominators remain explicit. Kappa is
reported as null with a status when class marginals make it undefined. These
are inter-rater consistency measures, not proof of prompt naturalness or
reviewer identity.

The v5 audit records task counts by normalized scenario and final expected
disposition (`PLAN`, `NO_PLAN`, `CLARIFY`), so an all-easy or single-decision
suite cannot hide behind one aggregate score. The official v1 protocol fixes
the 350/75/75 mix across 13 strata before prompt authorship; the strict
evaluator gate checks every quota and expected disposition.

Finalize inputs with
`agentshopping-sim finalize-challenge-review --prompts <master-prompts.jsonl> --reviewer-a-prompts <packet-dir>/reviewer-a-prompts.jsonl --reviewer-b-prompts <packet-dir>/reviewer-b-prompts.jsonl --reviewer-catalog <packet-dir>/reviewer-catalog.jsonl --packet-manifest <packet-dir>/packet-manifest.json --reviewer-a <reviewer-a.jsonl> --reviewer-b <reviewer-b.jsonl> --adjudications <adjudications.jsonl> --out-dir .local-run/agent-shopping/challenge-suites/<new-bundle>`.
It refuses overwrites, checks the packet manifest against all supplied files,
checks the reviewer catalog byte-for-byte against the pinned local
Merchant/inventory snapshot, and validates each
reviewer's raw labels against that snapshot before computing agreement, then
validates the adjudicated/final labels before writing the canonical bundle.
This prevents an invalid raw label from being hidden by a valid adjudication.
It also rejects exact normalized prompt/context duplicates (case and whitespace
folded); semantic near-duplicate detection remains a human-review responsibility.
To evaluate that exact packet,
pass `--tasks <bundle>/tasks.jsonl --require-human-review --review-bundle
<bundle>`. This strict evaluation gate requires exactly 500 frozen tasks;
smaller review bundles remain useful for tooling tests but cannot be scored as
the advertised 500-task holdout. The gate re-reads both reviewer streams, reruns adjudication and
snapshot validation, compares the regenerated tasks and audit, and records the
review-audit file hash in the run manifest. It rejects altered or incomplete
files before any model call. Recalculation proves file consistency and binding,
not the real-world identity of pseudonyms or that the declared blind review
actually occurred; those remain self-attested. Passing the gate is not, by
itself, resume-evidence approval.

No independently authored and double-reviewed 500-task packet is currently
present. Generated drafts remain excluded from target-product recall claims;
do not run or report the advertised 94.2% / 72.6% → 91.8% targets until the
human packet exists and the results are separately reviewed.

The CLI now fails closed before any model call unless the caller supplies a
recomputable human-review bundle or explicitly opts into
`--allow-unreviewed-diagnostic-model-run`. That override is recorded in the
run manifest and marks the result diagnostic-only; oracle-intent evaluation
and offline parse replay remain model-call-free and need no override.

On 2026-10-11, label validation was tightened after tracing a metric-boundary
gap: `RelevantSKUs` could include curated substitutes, allowing an accepted
alternative to count as an exact target-product recall hit. It now contains
exact requested-item listings only; `AcceptedSubstituteSKUs` is a separate,
explicit buyer-accepted set, and any substitute in feasible `GoldSKUs` must be
in that set. Substitutes have their own discovery-hit count and reviewer-set
agreement. A regression test proves a substitute-only result does not count as
target recall and rejects a feasible but unapproved substitute. This fixes the
human-review metric contract; it does not retroactively change prior generated
runs, which have no independent human relevance labels.

Report raw numerators/denominators and 95% intervals. For discovery, report
`target_product_recall_at_k` over human relevance labels and feasible-gold
Recall@K separately, each with macro/micro rates and task-hit@K. For plan generation, compare B0
and B1 on the same frozen tasks and report valid plans / feasible PLAN tasks,
paired percentage-point delta, and a task-bootstrap interval. Score correct
NO_PLAN and CLARIFY separately; do not hide false-positive purchase plans in
the plan-rate denominator. Keep shopping quality separate from the 300-order
idempotency/settlement audit.

## 300 logical purchase replay and response-loss evidence

Run `lifecycle-20261010T230937.352979900Z-purchase-replay-fbb9448711231772`
completed with `PASS` against the isolated `stablepay_shopping_sim` MySQL
database. It replayed 300 predeclared logical order keys selected from the
locked 500-task GPT-6 Luna run; there were no new model calls. This run used
the catalog cloned from the locked MySQL seed (`catalog_source:
locked_mysql_snapshot`); it did not start the 12 Merchant HTTP fixtures. The
live Merchant catalog had a different catalog hash, so the runner's strict
cross-source check rejected that source rather than silently mixing snapshots.
The 300 trials covered 100 post-commit response losses followed by retry on
the same API instance, 100 post-commit response losses followed by API
recreation and retry (one of these 100 also restarted the OS process), and 100
concurrent duplicate-checkout request pairs (600 checkout HTTP requests
total).

- **287/300** unique logical orders settled through the deterministic
  simulation-only payment adapter; **13/300** received its deterministic
  decline outcome.
- SQL reconciliation found **300 unique orders** and **300 unique mock
  payments**, with **0** duplicate idempotency groups, duplicate orders per
  task, duplicate payments per order, duplicate settlements, or
  amount/currency mismatches. All lifecycle row-count/state invariants passed.
- One post-commit response-loss trial terminated the benchmark API OS process,
  started a fresh process against the same isolated database, and retried the
  same key; the remaining 99 API-recreation trials restarted the test HTTP
  server in-process. The aggregate run is not a 300-process-crash test.
- This measures retry/replay correctness against an isolated local mock, not
  a real payment-provider sandbox, Solana Devnet, or production payment path.
  “No duplicate settlement observed” is scoped to these 300 injected trials;
  it is not a universal guarantee. Per-trial traces and SQL audit are retained
  under
  `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261010T230937.352979900Z-purchase-replay-fbb9448711231772/`.

On 2026-10-11, the test's live MySQL reconciliation and the read-only offline
verifier both passed. The offline verifier reconciled all 300 trial rows
against the retained manifest, metrics, and audit summary. Reproduce with the
isolated MySQL already running (no Docker call or image pull):

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\benchmark-agent-shopping-300-purchases.ps1 -UseExistingMySQL -UseLockedMySQLCatalogSnapshot
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\audit-agent-shopping-purchase-replay.ps1 -RunDir .\.local-run\agent-shopping\lifecycle-evaluations\lifecycle-20261010T230937.352979900Z-purchase-replay-fbb9448711231772
```

The verifier checks the three 100-trial fault strata, 600 checkout requests,
200 recorded response losses, 100 API recreations including exactly one OS
process restart, unique task/order-key/order/payment IDs, `287 SETTLED` / `13
DECLINED`, and zero recorded duplicate or amount/currency counters. It is an
offline consistency check, not a fresh MySQL query. Artifact SHA-256 values:

- `manifest.json`: `sha256:29502f198109b1dbc84583808413fedbd4d4495a51e7bcf079b6d50f293b4183`
- `metrics.json`: `sha256:e00b02dee7b7b053e02e7aabdd348456fac3e0145765a61c12708b906fdeb11f`
- `audit-summary.json`: `sha256:b3749765e0fbc892260fdbbfa2462ac488a7ea72a333a10e113b2fc702325031`
- `trials.jsonl`: `sha256:92031e98c244b17411f3886f670906f64d71e75c26fa955b105a9c0723d4e048`

### 2026-10-11 v2: checkout replay plus fulfillment/compensation

The follow-up run
`lifecycle-20261010T233527.056652900Z-purchase-replay-29c86a9579fc4f6d`
passed the live isolated-MySQL audit and the independent offline artifact
verifier. It reused the same locked 300 logical orders and catalog snapshot;
there were no new model calls. The catalog source remained the locked MySQL
snapshot because its hash did not match the live Merchant HTTP catalog.

- **287/300** orders settled in the deterministic mock payment adapter and
  **13/300** were declined. Of the 287 settled orders, **258** reached mock
  `FULFILLED`; **29** had a predeclared simulated delivery failure and reached
  `REFUNDED` with a matching mock refund and returned inventory.
- The end-to-end fulfilled-order rate is **258/300 (86.00%)**. The **287/300
  (95.67%)** figure is settlement rate only; among settled orders, fulfillment
  was **258/287 (89.90%)**. The three mutually exclusive final outcomes are
  258 fulfilled, 29 refunded/restocked, and 13 payment-declined (300 total).
- For every settled order, the harness made the initial fulfillment request,
  retried with the same idempotency key, and checked that a conflicting key
  was rejected: **861 fulfillment HTTP requests** total. First-request latency
  was P50/P95 **20.569/33.098 ms**. End-to-end task HTTP lifecycle P50/P95 was
  **106.665/146.697 ms**; these are local test timings, not a production SLO.
- Fault strata remained **200** post-commit checkout response losses, **100**
  API recreations (exactly **one** OS-process restart), and **100** concurrent
  duplicate checkout pairs. Across 300 logical orders SQL found **0** duplicate
  idempotency groups/orders/payments/settlements or amount/currency mismatches.
- Fulfillment SQL audit found **0** duplicate fulfillments/refunds, settled
  payments without fulfillment, effects on declined orders, state/refund/amount/
  payment/reservation mismatches, or non-simulation effects. Both live SQL and
  offline artifact audits passed. This confirms only the explicitly audited
  local deterministic mock run; refund/restock are committed in one local DB
  transaction, so no external-provider or cross-service crash window is tested.

The 29 delivery failures are deliberately predeclared from the locked task IDs
using the documented SHA-256 schedule, not spontaneous failures or a measured
real-world failure rate. The run does not represent 287 delivered orders: its
outcomes are 258 delivered, 29 compensated/refunded, and 13 declined. It uses
neither a real payment/refund/delivery provider nor Devnet, and is not a
production reliability claim.

Artifacts are retained under
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261010T233527.056652900Z-purchase-replay-29c86a9579fc4f6d/`:

- `manifest.json`: `sha256:75e39166a1ff3a2cd1177e8fdba5daa52a7da5be3e08dcf8b71aaa48af37af41`
- `metrics.json`: `sha256:ba4a1c8de71a74273a7aa507b5311aeca0f3df36c5459a3d06f6a03a832af8c6`
- `audit-summary.json`: `sha256:99f86b975fa934f248e0e1479aa65a586d5b6c66b3ee349cb37b19b86b70b98d`
- `trials.jsonl`: `sha256:0cc38772a846eebb45c533aab621983a61b37b2e0f30740ec63fa90f171672f0`

### 2026-10-11 v3: bounded transient-fulfillment retry (measured)

Run `lifecycle-20261011T030423.991849100Z-purchase-replay-3c62658aebd5c5a0`
passed against the existing isolated `stablepay_shopping_sim` MySQL schema,
using the locked MySQL catalog snapshot. No Docker image was pulled, no
Merchant process was started or stopped, and no new model calls were made. The
test created a unique tenant, executed the 300 trials, ran live SQL audits, and
verified tenant cleanup on exit.

The benchmark-only MySQL adapter now persists each provider attempt in
`simulated_fulfillment_attempts`. A retryable first failure leaves the paid
order and consumed reservation intact, returns `202 RETRY_PENDING` with a
bounded backoff, and resumes under the same fulfillment idempotency key. The
retry plan and attempt ordinal are stored in MySQL so a fresh API process can
continue it. Retries are capped at three attempts; a permanent failure or an
exhausted retry budget still records one terminal compensation transaction.
An opt-in integration test exercises one transient failure across a real API
OS-process restart, followed by same-key replay, conflicting-key rejection,
and SQL reconciliation.

On 2026-10-11, `TestMySQLHTTPFulfillmentRetryResumesAfterProcessRestart` also
passed against the existing isolated MySQL schema. It persisted one
`RETRY_PENDING` attempt, terminated the API process, and recovered the same
paid order after restart: SQL observed exactly two provider-attempt rows, one
fulfillment, zero refunds, and zero retryable failures without retry; same-key
replay preserved identities and a conflicting key returned HTTP 409. This is
a separate one-order fault test, not 300 process restarts; it writes no saved
benchmark artifact beyond the test output.

- **287/300 (95.67%)** logical orders reached mock `FULFILLED`; **13/300** were
  deterministic mock-payment declines. This exceeds the illustrative 286/300
  count numerically, but it is not a comparable real-provider or production
  result.
- **29/29** predeclared one-shot transient delivery faults recovered under the
  same idempotency key. The fulfillment adapter recorded 316 provider attempts
  and 29 retry HTTP requests; no settled order remained without fulfillment.
- The fault mix remained 200 post-commit checkout response losses, 100 API
  recreations (exactly one OS-process restart), and 100 concurrent duplicate
  request pairs. Across 300 unique task/order/payment identities, SQL found 0
  duplicate order keys/groups, orders per task, payments per order,
  settlements per order, amount/currency mismatches, duplicate fulfillments or
  refunds, retry-sequence errors, orphan attempts, or final-state mismatches.
- The single OS-process restart inside the 300-run fault mix exercised checkout
  commit/replay. Durable fulfillment retry across an OS restart is established
  separately by the one-order integration test above.
- Live MySQL lifecycle and fulfillment audits passed with no query errors. The
  separate offline verifier also passed against retained per-trial artifacts;
  that verifier reconciles saved evidence and is not itself a fresh DB query.

This is a deterministic local mock benchmark on locked synthetic tasks; the 29
faults are scheduled injections, not an estimated natural delivery-failure
rate. It does not exercise a real payment provider, refund provider, Devnet,
external fulfillment, or production reliability. The 300 attempts reuse a
fixed synthetic task pool and should not be described as 300 independent user
scenarios.

Reproduce without Docker or image pulls when the isolated MySQL endpoint is
already running:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\benchmark-agent-shopping-300-purchases.ps1 -UseExistingMySQL -UseLockedMySQLCatalogSnapshot
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\audit-agent-shopping-purchase-replay.ps1 -RunDir .\.local-run\agent-shopping\lifecycle-evaluations\lifecycle-20261011T030423.991849100Z-purchase-replay-3c62658aebd5c5a0
```

The first command exited 0 and the second returned `PASS`. The offline audit
reports 287/300 fulfilled, 13 declined, and zero duplicate or
amount/currency violations. Artifact SHA-256 values:

- `manifest.json`: `sha256:ebeb409cf6ae751844fc6cf4422f0ce9504aa9c9aeec312e2892765795fd4206`
- `metrics.json`: `sha256:555b6d73ece25c81224dbe4432cf9efa3ef7a0bb0b1c7379a6f957335380babb`
- `audit-summary.json`: `sha256:3caae94bca5bdb45d106d3f602d8de8d5742bdad7023732ae6646611559d4b47`
- `trials.jsonl`: `sha256:7ee8ce4a3e6d89acaf9e75c7c59bd723d7a25e33acff3b94ed7c3dc47e8dea7f`

## Single checkout retry across an OS-process restart

Run `lifecycle-20261010T230133.684713600Z-os-process-restart-f648ac6ab7085a9f`
passed for one task (`shop-0001`) from the already-locked intent suite, with no
new model calls. The benchmark-only Go HTTP API child process committed a
simulation payment and order, then the harness discarded the HTTP response,
terminated that OS process, started a fresh process against the same isolated
`stablepay_shopping_sim` MySQL database, and retried the same idempotency key.
The retry returned the original order and payment identities.

- **1/1** simulated checkout recovered as `SETTLED` after **1 OS-process
  restart**; no real settlement or provider credentials were used.
- The live SQL lifecycle and purchase-replay audits passed with **0** duplicate
  idempotency groups, duplicate orders, duplicate payments, duplicate
  settlements, or amount/currency mismatches. The separate offline artifact
  audit also passed and recorded hashes for all four output files.
- This is a one-trial correctness demonstration using the benchmark HTTP API
  subprocess, not a test of the packaged Commerce Runtime executable, a
  reliability rate, or a real-provider/Devnet payment. The newer 300-attempt
  run above includes one OS-process restart among 100 API-recreation retries;
  the other 99 use in-process HTTP-server recreation.

Reproduce with the isolated local MySQL already running; this runner does not
start Docker, merchants, or download dependencies. `GOPROXY=off` makes a
missing cached dependency fail closed:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\benchmark-agent-shopping-os-process-restart.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\audit-agent-shopping-os-process-restart.ps1 -RunDir .\.local-run\agent-shopping\lifecycle-evaluations\lifecycle-20261010T230133.684713600Z-os-process-restart-f648ac6ab7085a9f
```

Artifacts are retained locally under
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261010T230133.684713600Z-os-process-restart-f648ac6ab7085a9f/`.
Artifact SHA-256 values from the passing offline verifier:

- `manifest.json`: `256e1569fb5babf9bf2bdf7a8bd7c8d6fda3765ff25e9b924c493ba483004921`
- `metrics.json`: `88b1aa23a44800a2a0b9db0ac951c8e3f73edb5bf3a190c26902b87918590cec`
- `audit-summary.json`: `b3d09c4612e017231f59d151d991ddb3c2e18dc5b4a90d7e30c638979bbf39b1`
- `trials.jsonl`: `eaff33b9f97a0ea065843b5f6608f83d25cdafa0483f07ac0aebe2a85e5adf77`

## External benchmark references (methodology, not StablePay scores)

Sources below were checked on 2026-10-11. Public tasks and labels may be used
to calibrate task design and metric implementation, but their scores must stay
separate from StablePay's own 500-task result. Public prompts/labels are not a
private blind holdout, and third-party catalog terms do not automatically
follow the benchmark-code license.

| Reference | Scope and scale | Reuse boundary | StablePay use |
|---|---|---|---|
| [EComAgentBench paper](https://arxiv.org/abs/2606.17698), [official repository](https://github.com/Morizeyao/EComAgentBench_), [product DB](https://huggingface.co/datasets/ecomagentbench/EComAgentBenchProductDB) | 662 English tasks, 8 shopping intents, 6,645 constraint rubrics; automatically constructed from product/review data, with preference distributed across visible query, tool-gated persona, and scripted clarification. The separate product DB is about 25 GB; rebuilding it uses tens of GB of Amazon source data. | Code is MIT; the Amazon Reviews-derived product corpus has separate research-use/source terms, and task-data redistribution terms must be checked independently before importing it. | Best reference for hidden preferences, clarifying questions, and criterion-level attribution. It does not validate inventory races, checkout, fulfillment, or payment. No DB or source data was downloaded. |
| [Shopping Reasoning Bench paper](https://arxiv.org/abs/2606.12608), [official dataset card](https://huggingface.co/datasets/amazon/ShoppingReasoningBench) | 525 expert-authored missions (232 single-turn, 293 multi-turn; 1,764 turns) and 10,863 importance-weighted binary rubrics; the dataset card lists 3.69 MB of files. | Dataset card lists CC BY-NC 4.0 and requires accepting an access condition; non-commercial/research boundary applies. | Closest scale match for human-authored multi-turn quality evaluation. Borrow required/optional atomic rubrics and turn-level consistency; it does not cover live catalog retrieval, stock/price mutations, checkout, or payment effects. Do not merge its tasks/scores into StablePay. |
| [Amazon ESCI official repository](https://github.com/amazon-science/esci-data), [paper](https://arxiv.org/abs/2206.06588) | Human judgments label Exact, Substitute, Complement, or Irrelevant product matches. The documented reduced set has 48,300 unique queries / 1,118,011 query-product judgments; the larger set has 130,652 queries / 2,621,288 judgments across English, Spanish, and Japanese. | Repository code is Apache-2.0; comply separately with dataset source and attribution conditions. No corpus was downloaded. | Calibrate Recall@K, nDCG, and substitution labels or run a separately named public retrieval check. It cannot establish StablePay's private 500-task result, live stock/region/budget feasibility, multi-turn intent handling, or transaction correctness. |
| [ShoppingBench official repository](https://github.com/yjwjy/ShoppingBench), [AAAI 2026 proceedings](https://ojs.aaai.org/index.php/AAAI/article/view/40640), [paper](https://arxiv.org/abs/2508.04266) | 3,310 instructions and 2.5M+ product records; includes product discovery, same-store multi-item requests, coupons, and budget constraints. Its product-document file is listed as about 1.4 GB compressed / 4.8 GB uncompressed. The AAAI abstract reports GPT-4.1 absolute success below 50% on that benchmark; this is benchmark-specific, not a directly comparable StablePay score. | Repository is Apache-2.0; third-party product-catalog rights are separate and should be checked before redistribution. | Useful for budget, coupon, and multi-product scenario coverage; not live inventory/price concurrency or payment-ledger correctness. Read the paper/repo only; do not pull its product corpus for this project. |
| [DeepShop official repository](https://github.com/youganglyu/DeepShop) | 600 evolving product-search queries across five categories, organized by easy/medium/hard attribute, filtering, and sorting complexity. | Repository lists MIT; the benchmark is primarily single-store search and does not supply dynamic stock, checkout, or payment-state evidence. | Use its staged query-complexity design as a hardness template; do not equate longer prompts with harder decisions or import its score into StablePay. |
| [ShoppingComp official repository](https://github.com/ByteDance-BandAI/ShoppingComp), [dataset card](https://huggingface.co/datasets/huaixiao/ShoppingComp), [paper](https://arxiv.org/abs/2511.22978) | Current repository/card lists 97 expert ground-truth tasks plus 48 safety/trap tasks per language; the paper reports 145 instances / 558 scenarios. These are not interchangeable counts: pin the data revision and distinguish task instances from scenarios. | Repository/dataset card lists MIT; the four task JSONL files (6,765,036 bytes total) are pinned and hash-verified under ignored `.local-run/agent-shopping/reference-data/shoppingcomp-201dd305/`. No product corpus was downloaded. Check third-party product/content rights independently before redistributing task text or evidence. | Strong reference for atomic hard-constraint rubrics, adversarial negatives, evidence, and safety; not a replayable inventory/checkout simulator. |
| [Commerce Agent Bench official repository](https://github.com/Accio-org/CommerceAgentBench) | 107 cross-domain browser, CLI, API/MCP, and file tasks; runs isolated environments with mock services and explicit state validators. | Code is Apache-2.0 and task data CC BY 4.0; the repository has separate notices for third-party assets. | Borrow environment isolation, deterministic mocks, state validators, and run evidence; it is not a dedicated buyer-shopping/payment benchmark. |
| [STATE-Bench official repository](https://github.com/microsoft/STATE-Bench) | 450 synthetic enterprise workflow tasks across travel, customer support, and shopping; each runs with a task-local database, domain tools, and simulated user. Reports completion over five runs (`pass@1`) and tasks successful in all five (`pass^5`). | Repository is MIT; the published task data is synthetically generated. No benchmark files or environment were downloaded for this review. | Strong reference for final-state/procedure grading and repeatability. Shopping includes product search, cart, promotions, loyalty, shipping, and compatibility; support includes returns/refunds. It does not prove StablePay payment-ledger correctness. |
| [ShopGym paper](https://arxiv.org/abs/2605.16116), [official repository](https://github.com/agentic-foundation-modeling-research/shop-gym) | Paper reports 224 generated tasks across six sandbox shops (three synthetic-data and three real-data shops); the current project page lists 10 shops. Tasks are grounded in catalog, navigation, policy, and interaction structure. | Code repository is MIT; no aggregate dataset footprint is stated. The task pipeline is generated, not an expert-authored blind holdout. | Borrow resettable shop environments and real-vs-synthetic comparisons; do not use its generated labels as independent human ground truth. |
| [WebShop official repository](https://github.com/princeton-nlp/WebShop), [paper](https://arxiv.org/abs/2207.01206) | Paper reports 1.18M products, 12,087 crowdsourced shopping instructions, and 1,600+ human demonstrations; tasks combine natural-language constraints with search, detail-page choices, and purchase actions. | Code is MIT and the repository provides environment/data setup, but that code license does not itself settle rights for the underlying product content. | Strong reference for harder constraint-composition prompts, search reformulation, page noise, and interaction traces; not a payment-safety benchmark. |
| [WebMall official repository](https://github.com/wbsg-uni-mannheim/WebMall), [paper](https://arxiv.org/abs/2508.13024) | Four simulated shops, 4,421 offers, and 91 cross-shop tasks, including vague requests, comparison, alternatives/compatibility, cart, and simulated checkout. | No clear dataset license was visible in the repository root during review; obtain author clarification before copying or redistributing data. | Best reference for cross-shop navigation and checkout-flow cases, but its fixed simulated offers cannot establish dynamic stock, fulfillment, or payment correctness. |
| [WebArena-Verified official repository](https://github.com/ServiceNow/webarena-verified), [dataset page](https://huggingface.co/datasets/AmineHA/WebArena-Verified) | 812 verified web-agent tasks and a 258-task hard subset; task data, deterministic evaluators, and a Docker-backed `shopping` site are available. | Repository is Apache-2.0. Check licenses for the separately built site images and components before redistributing the full environment. | Useful for browser navigation, cart changes, and state/trace evaluation when an API path fails; it is not a cross-merchant comparison or real-payment benchmark. |
| [τ²/τ³-bench official repository](https://github.com/sierra-research/tau2-bench), [retail split guide](https://docs.nvidia.com/nemo/labs-voice-agent/evaluate-voice-agents/domain-guides/tau-2-retail/), [τ³-bench 1.0.0 release](https://github.com/sierra-research/tau2-bench/releases), [original paper](https://arxiv.org/abs/2406.12045) | Stateful tool-agent-user simulation with policy constraints and final-state evaluation. The cited τ² retail split has 114 tasks (74 train / 40 test); keep that split distinct from newer releases. The τ³-bench 1.0.0 release says 26 retail tasks were revised in its task-quality pass—this is a count of tasks touched by fixes, not the domain's total task count. Pin the release and split when comparing results. | Repository is MIT. Exact task contents and score comparability are release/split dependent. | Borrow multi-turn user changes and final database-state assertions; the retail tasks are customer-service oriented, not product ranking, cross-shop discovery, or payment-side-effect proof. Repeated-run `pass^k` is a useful reliability metric, but keep it separate from one-run success. |
| [ShopBench / Shopping MMLU official repository](https://github.com/KL4805/ShoppingMMLU), [paper](https://arxiv.org/abs/2410.20745) | 57 task types spanning shopping concepts, knowledge reasoning, user-behavior alignment, and multilinguality; includes static multiple-choice, retrieval, ranking, NER, and generation tasks. | Repository is Apache-2.0 and publishes `data.zip`; the benchmark derives from Amazon data, so verify upstream product-data terms separately before redistribution. | Good broad shopping-domain and attribute/ranking calibration. It is a static QA/evaluation set, not a stateful commerce-agent or checkout environment. |
| [VoiSeR official AWS Open Data entry](https://registry.opendata.aws/amazon-conversational-product-search/) | Product and utterance assets for spoken product-search refinement; the entry lists product, utterance, and image files. Exact sample counts were not confirmed in this review. | CC BY-SA 4.0; attribution and share-alike obligations apply. | Useful only as a design reference for iterative preference refinement; audio-specific data and licensing make direct import unnecessary for the current text shopping benchmark. |
| [STaRK-Amazon official dataset page](https://stark.stanford.edu/dataset_amazon.html), [code](https://github.com/snap-stanford/stark) | Semi-structured Amazon graph with 1,035,542 entities and 9,443,802 relations; QA set includes 9,100 synthetic and 81 human-authored queries over product/brand/color/category relations. | Dataset page lists CC BY 4.0; follow attribution and verify terms for any derived redistribution. | Useful for constructing harder relational preferences and attribute distractors; graph relations are not inventory or substitute authorization. |
| [Amazon-M2 official project page](https://www.amazon.science/publications/amazon-m2-a-multilingual-multi-locale-shopping-session-dataset-for-recommendation-and-text-generation), [paper](https://papers.nips.cc/paper_files/paper/2023/file/193df57a2366d032fb18dcac0698d09a-Paper-Datasets_and_Benchmarks.pdf) | Multilingual shopping-session benchmark with 1,410,675 products, 3,606,249 training sessions, and 361,659 test sessions; focuses on next-product recommendation and text generation. | Paper states Apache-2.0; retain source and license attribution when using the dataset. | A secondary reference for preference context, locale variation, and long-tail difficulty; it predicts interactions rather than validating a budget/region-constrained purchase plan. |
| [RetailBench official repository](https://github.com/linghuazhang01/RetailBench), [paper](https://arxiv.org/abs/2603.16453) | Interactive single-store operator simulation over a 180-day horizon, with SKU assortment, suppliers, replenishment, inventory aging/stockouts, prices, sales, returns, and cash flow. | Repository is MIT; large LLM rollout logs may not be included in each release. | Useful for designing dynamic stock/price and delayed-feedback scenarios, but it evaluates a merchant operator—not a buyer selecting across merchants. |
| [E-Commerce Bench official repository](https://github.com/QwenLM/E-CommerceBench), [paper](https://arxiv.org/abs/2608.30730) | Merchant-side 365-day simulation with 6,886 products, 576 suppliers (including 152 fraudulent suppliers), market shocks, inventory/pricing decisions, returns, and delayed escrow settlement. | Repository declares Apache-2.0; product data is described as desensitized from a real commerce platform, and no separate data-rights statement or aggregate file footprint was confirmed. Setup installs Python dependencies; do not clone/install for this project without a separate need and storage review. | Borrow deterministic changing-world, supply-shock, and delayed-cash-flow design. It evaluates merchant operations, not buyer discovery or payment-ledger correctness. |
| [IdempotencyBench official repository](https://github.com/gssanjana4/idempotencybench) | Repository describes 320 deterministic tasks with simulated payment side effects, timeout-after-commit, transport/agent retries, full replay, and an effect ledger. | Repository reports MIT for code and CC BY 4.0 for task/record data; the project is early-stage and its paper/results are not yet mature. Verify current terms before reuse. | Borrow fault categories and independent-effect-ledger design for the StablePay 300-attempt suite. It cannot replace StablePay's own isolated database/provider audit. |
| [PayBench official project](https://paybench.org/) | Delegated-payment safety benchmark: public Phase 1 has 50 fully mocked scenarios; Phase 2 is described as 226 offer-grounded trap/lookalike scenarios, including 113 matched pairs, and remains unpublished/private. | No explicit dataset reuse license was visible on the project page. Treat as a methodology reference unless the author clarifies terms; do not copy scenario text. Results are simulated and the project itself says transfer to real payment infrastructure is untested. | Borrow matched safe/unsafe checkout cases and jointly report unsafe-payment and unnecessary-refusal rates. It does not prove StablePay ledger idempotency, settlement, or crash recovery. |
| [TREC 2025 Product Search and Recommendations](https://trec-product-search.github.io/), [recommendation instructions](https://trec-product-search.github.io/recommendations.html), [dataset card](https://huggingface.co/datasets/trec-product-search/product-recommendation-2025) | Provides product search/query-reformulation and separately ranked substitute/complement tasks. The Hugging Face card reports 48.6k rows and a 212 MB repository footprint for the 2025 recommendation dataset. | Dataset card lists Apache-2.0; the corpus draws on Amazon M2/ESCI and the instructions distinguish source datasets, so preserve provenance and recheck upstream terms before redistribution. No files were fetched. | Borrow separate substitute/complement labels and query-reformulation evaluation. It does not test multi-turn state, live stock/price races, fulfillment, or checkout/payment. |

Additional paper-only/design references: [Amazon-Bench](https://arxiv.org/abs/2508.15832)
describes 400 function-driven Amazon web tasks and distinguishes harmless from
account-state-harming failures; borrow that safety-impact distinction, not its
online account environment. [MerchantBench](https://arxiv.org/abs/2607.28956)
describes seller-side long-horizon simulation over 98,843 product records, 365
days, and 26 tools; use it only as a reference for delayed inventory/financial
feedback, not buyer-side product discovery. [ShopperBench (EACL 2026)](https://aclanthology.org/2026.eacl-industry.62/), also described on [Amazon Science](https://www.amazon.science/publications/shopperbench-a-benchmark-for-personalized-shopping-with-persona-guided-simulation),
studies persona-conditioned shopping trajectories and reports Persona Fidelity,
Persona-Query Alignment, and Path Consistency; it builds on WebShop and τ2-Bench.
A public task bundle and data license were not confirmed during this review.
[Shopping Reasoning Bench](https://arxiv.org/abs/2606.12608)
contains 525 expert-authored missions (232 single-turn and 293 multi-turn) with
10,863 importance-weighted binary rubrics; reported overall model pass rates
were 57–77%, with performance degrading later in multi-turn conversations.
Borrow its expert rubric structure and turn-by-turn consistency checks, not its
scores; its dataset card is CC BY-NC 4.0 and access-gated. [MMShopBench](https://arxiv.org/abs/2607.29002)
reports 289 manually annotated real-log, multimodal multi-turn cases over a
frozen 100,000-product sandbox; its [repository](https://github.com/H-cool/MMshopbench)
points to a separate Hugging Face dataset and lists about 9 GB of additional
indexes/models (including a 7.6 GB image model), while the linked dataset page
lists about 557 MB. A complete redistributable asset bundle and its rights
were not confirmed. [REALWORLDSHOP](https://arxiv.org/abs/2609.38974),
a recent preprint, describes 3.28 million grounded products and
profile-grounded shopping episodes, emphasizing state tracking, constraint
updates, ambiguity, and multi-intent/bundle tasks. The arXiv landing page did
not expose a public code/data link or dataset reuse license at review time; its
4.8 MB submission is the paper, not the multi-million-product corpus. Use these
as task-design references only; neither paper's scale or scores is evidence for
StablePay, and no data was downloaded.

Resource check (2026-10-11): this review read public paper/project pages only;
no dataset, model, repository, or container image was downloaded or cloned.
Large candidates were deliberately kept as references: EComAgentBench's DB is
about 25 GB, ShoppingBench's product documents are about 1.4 GB compressed,
and MMShopBench documents multi-GB indexes/models. TREC 2025's recommendation
dataset is listed at 212 MB, but remains unfetched; the separate general TREC
product-search corpus is listed at 16 GB and is not needed here. The host had
about 1.37 TB free on `D:` at the time of this review; available-memory
telemetry was denied by Windows, so no RAM figure is claimed.

Recommended use: treat ESCI as the retrieval-metric reference,
EComAgentBench as the clarification/hidden-preference design reference and
Shopping Reasoning Bench as the expert-authored multi-turn rubric reference,
ShoppingBench, WebShop, and DeepShop as multi-constraint/query-difficulty
references; ShoppingComp for expert rubrics and adversarial safety cases;
Commerce Agent Bench for isolated mock environments and state validators;
STATE-Bench for final-state/procedure scoring and `pass^5` repeatability; ShopGym
for resettable synthetic-vs-real shop design; τ²/τ³-bench for policy-following,
persisted-state grading, and repeated-run reliability; WebMall
as a cross-shop reference only after license clarification; and WebArena-Verified
as a browser-state/trace reference. Use STaRK-Amazon and Amazon-M2 only to
inspire harder preference/context cases; use VoiSeR only for dialogue-refinement
ideas unless its share-alike terms are acceptable. Use τ²-bench for policy-driven
dialogue/state transitions, RetailBench and MerchantBench for stock/price and
delayed-feedback design, Amazon-Bench for safety-impact categories, and
IdempotencyBench only for retry/effect-ledger fault categories. Keep third-party
prompts, catalogs, and scores out of StablePay's private holdout.
Keep all public-benchmark scores and labels out of StablePay's private holdout.
The StablePay holdout must have independently authored prompts and labels,
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

- **Target-product Recall@20:** on feasible PLAN tasks, measure
  `|discovery top-20 SKUs ∩ exact-target relevant active SKUs| / |exact-target
  relevant active SKUs|`. Relevance is exact requested-item matching and is
  independent of current stock, region, and price; labels still satisfy the
  buyer's stated hard product requirements. Accepted substitutes do not count
  in this numerator or denominator.
  runtime-matched discovery window is 100 listings, but Recall@20 scores only
  its first 20 before current offer-feasibility filters.
  Report macro task-average, micro SKU-level, task-hit@20, and a task-bootstrap
  interval. Human-reviewed tasks store these labels in `relevant_skus`.
- **Accepted-substitute discovery:** separately count whether an explicitly
  accepted substitute appears in the discovery top K. Human reviewers must
  provide `accepted_substitute_skus`, including `[]` when no substitute is
  allowed. This diagnostic does not replace target-product Recall@K or make an
  unapproved substitute a valid plan.
- **Snapshot-feasible SKU Recall@20:** separately measure discovery against
  `gold_skus`, the acceptable products currently feasible under the pinned
  stock/region/budget snapshot. This is a diagnostic of discovery plus
  feasibility and is not target-product relevance. Generated drafts without
  `relevant_skus` retain a compatibility fallback to `gold_skus`, but that
  result is not independent relevance evidence and cannot support the proposed
  `94.2%` claim. That figure remains a target, not an acceptance override.
- **Valid purchase-plan rate:** valid plans / all tasks labeled `PLAN` and
  verified feasible in the pinned snapshot (for the mixed challenge suite,
  this denominator is the PLAN stratum, not all 500 decisions). A plan passes
  only if the SKU is in `gold_skus`; every hard attribute,
  currency, budget (including any modeled delivery fee), quantity, current
  stock/region, and quote-revision check passes; ranking is consistent with
  the task preferences; and confirmation requirements are respected. The
  predeclared B0 baseline and B1 treatment must use the same parsed intent for
  each task; publish both rates, the paired per-task delta, its bootstrap 95%
  interval, and the both/baseline-only/treatment-only/neither counts. The
  proposed `72.6% → 91.8%` is a target pair, not a promised outcome.
- **Safety suite:** report correct no-plan/clarification (including the
  missing-field cue proxy)/re-confirmation
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

## Agent tool-interface status

The benchmark-only `commerce-runtime/cmd/agentshopping-mcp` bridge exposes the
isolated shopping API's search, task/quote reads, mock checkout, order status,
and mock fulfillment through a fixed MCP tool allowlist. It intentionally does
not expose buyer confirmation: the API now requires a second local credential
for that route, and `scripts/approve-simulated-shopping-quote.ps1` fetches and
displays the immutable quote before requiring a human to type the exact amount.
Human-confirmation IDs are random rather than predictable from quote/idempotency
inputs, and the simulator removes the approval key from its environment before
launching the intent-parser child process. The key is stored separately from the
MySQL env file and is not loaded by the MCP bridge. This is a local benchmark
control, not production identity or authorization. The bridge accepts stateless
MCP request metadata and `server/discover` under `2026-07-28`, plus the legacy
`2025-11-25` initialization handshake. A fresh 2026-10-11 subprocess
verification now passes: the executable was built with dependency fetching
disabled and build cache/temp output on D:; the current stateless protocol
handshake lists exactly seven tools, then a search tool call traverses stdio
into a dynamically bound fake loopback HTTP server. The smoke verifies route,
request body, dummy API-key forwarding, response propagation, and clean process
exit; it makes no real API, database, payment, or fulfillment call. C: remained
at 12.97 GiB free. A separate gated
`TestMySQLMCPSubprocessPurchaseLifecycle` then passed the real compiled MCP
subprocess → authenticated API → isolated MySQL path. It verified protocol
discovery, all seven tools, search, immutable quote, the absence of any
agent-callable confirmation tool, test-only synthetic approval on a separate
credential, mock checkout, order status, fulfillment, idempotent replay, and
direct SQL row/inventory invariants. The audit found one order/payment and one
confirmation/reservation/fulfillment/attempt; `on_hand=3`, `reserved=0`, and
`real_settlement=false`. Its parser and API server are deterministic test
fixtures; this is one local correctness trial, not a marketplace-quality or
reliability rate. The failure that revealed the `order_id` POST-body leak is
now covered by a bridge regression test. Both focused Go packages and the
gated MySQL E2E pass with module fetching disabled and Go cache/temp output on
D:. The MCP child receives only the fake API credential, not database or
buyer-approval credentials. Hashed executable and run outputs are retained in
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T064821.076651500Z-mcp-subprocess/`.
Hermes' installed Python MCP SDK targets `2025-11-25`, but its subprocess launch
from this Windows sandbox failed with `WinError 5`, and the Hermes CLI install
lock was not writable. This is not yet a Hermes/LLM end-to-end run, full MCP
conformance evidence, or a shopping-quality measurement; do not infer a new
shopping score from the bridge implementation.

## 2026-10-11 public ranking reference and current-catalog HTTP lifecycle

To check whether the earlier 100% synthetic retrieval result was caused by an
easy task set, a pinned test-split sample of the Amazon Shopping Queries ESCI
dataset was downloaded to the ignored D-drive `.local-run` directory. The
compressed source is 114,812,289 bytes (about 109.5 MiB), SHA-256
`bb0db292e2185fea215bab11ef5d583aaf28eb1fc03cbcbca01a0c324009e11c`, from
Hugging Face mirror revision
`13a365008a1a633310a1ff58992b789fcf526b0e`. No source dataset was placed on C:;
the C: free-space check remained about 12.98 GiB. The mirror's card claims
Apache-2.0, but upstream terms have not been independently cleared; keep the
corpus and sampled rows local and do not redistribute them.

The reproducible diagnostic sampled 500 test queries with at least one Exact
judgment and deduplicated within-query product IDs, leaving 9,214 judged query-
product pairs (18.428 candidates/query): 7,324 Exact, 1,262 Substitute, 98
Complement, and 530 Irrelevant. At K=20, title-overlap ranked Exact-target
macro recall `95.42%` (task-bootstrap 95% CI `94.27–96.51%`), micro recall
`89.27%`, task-hit `500/500`, and graded nDCG `0.9785`. A simple BM25 over title
and product text measured `95.33%` macro / `89.06%` micro Exact recall and
`0.9762` nDCG; it did not improve on title overlap. Task-hit@20 is mechanically
uninformative here because the judged candidate set averages fewer than 20
items and every sampled query has an Exact item. This is a judged-candidate
ranking reference, not whole-catalog retrieval recall; it contains no unjudged
catalog negatives, prices, stock, region, budget, consent, checkout, or payment
labels. It is not a StablePay result and must not be used for the 94.2% resume
target. Artifacts and hashes are in
`.local-run/agent-shopping/reference-data/esci-us-test-20261011/`; rerun with
`python scripts/benchmark-esci-reference.py --queries 500 --seed 20261010 --cutoff 20`.

To test that saturation diagnosis rather than rely on it, the full test split
was scanned once for Exact-count strata: 437 queries have exactly one Exact
product, 952 have 2–5, and 12,113 have 6 or more. A deterministic
`sparse_exact` cohort contains all 437 single-Exact queries plus 63 uniformly
sampled 2–5-Exact queries, excluding the 6+-Exact majority. On these 500
queries (7,818 judged pairs; 15.636 candidates/query), title-overlap Exact
macro recall at K=1/5/10 was `64.15% / 89.30% / 96.14%`, versus BM25
`65.14% / 92.32% / 97.08%`. At K=5, title-overlap task-hit was `90.4%`
(95% task-bootstrap CI `87.8–92.8%`) and BM25 was `93.8%` (CI
`91.6–95.8%`); at K=10 task-hit rose to `96.4% / 97.4%`. Unlike the uniform
sample, this cohort makes single-target misses visible and gives the two
simple rankers a measurable difference. It is an intentionally
stratified public diagnostic, not a prevalence-weighted estimate, a StablePay
result, or a commerce E2E test; confidence intervals describe this constructed
cohort and do not establish marketplace generalization. No pricing, inventory,
region, buyer consent, checkout, or payment is measured. The script records the
sampling profile and exact-count strata in the manifest; outputs are isolated
under `.local-run/agent-shopping/reference-data/esci-us-test-20261011/sparse-exact-20261011/`.
The K=10 values exceeding 94.2% are not comparable to, and cannot support,
StablePay's target claim: they are an external ranker on ESCI's judged
candidate sets, not StablePay's search against its catalog or the proposed
human-reviewed task denominator.

The runner also produced a same-sample K sensitivity check at K=1/5/10. On the
uniform sample, title-overlap macro Exact recall was `11.02% / 40.11% / 67.65%`
and BM25 was `11.13% / 40.09% / 67.59%`; task-hit was already `96.0% / 99.4% /
99.8%` and `94.6% / 99.8% / 100.0%`, respectively. This confirms that
task-hit@K is much less discriminating than exact-product recall for these
multi-target queries. K-sensitivity artifacts are under
`.local-run/agent-shopping/reference-data/esci-us-test-20261011/k-sensitivity-20261011/`.

The locked 500-task HTTP-to-MySQL lifecycle was also rerun against a newly
created isolated tenant seeded from the live HTTP snapshot of all 12 local
Merchant fixtures. This avoided overwriting the older tenant. Run
`lifecycle-20261011T062444.190629000Z-http-c15f3fd84e449fe7` completed 500/500
recorded-intent flows to a terminal state, with 4,352 HTTP requests, zero
unexpected HTTP failures, and SQL audit PASS / zero invariant violations.
There were 483 deterministic mock settlements and 17 deterministic mock
declines; 50 price changes, 50 stock changes, and 100 stale-quote confirmation
attempts were exercised. Sequential lifecycle P95 was `173.30 ms` at `8.70
tasks/s`. No new model calls, external payment, Devnet transaction, or real
fulfillment occurred. The 500 parser outputs were recorded-response replay.

The same run's recomputed synthetic structured-search report shows B0/B1 valid
plans `389/500` vs `500/500`, while both have `596/596` snapshot-derived gold
SKU hits at K=20. These are feasibility/ranking diagnostics over the same
recorded labels and a generated catalog, not independent relevance judgments;
the apparent 100% retrieval/plan rates cannot support product-quality claims.
The run manifest explicitly records that the catalog differs from the catalog
used by the recorded intent replay and that the report was recomputed on the
current snapshot. Metrics, SQL audit, manifest, and trials are retained under
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T062444.190629000Z-http-c15f3fd84e449fe7/`.

A second fresh rerun on 2026-10-11 used another unique isolated tenant seeded
from the current 12-Merchant HTTP snapshot:
`lifecycle-20261011T071215.595159200Z-http-edbe98a5b09f5c52`. It passed all
500 recorded-intent transaction flows, with 4,352 HTTP requests, 500 orders,
500 simulated payments, 500 confirmations, 100 expected stale-quote conflicts,
and zero unexpected HTTP failures or SQL invariant violations. The deterministic
mock schedule produced 483 settlements and 17 declines; the run exercised 50
price revisions and 50 stock-loss mutations. Sequential task-lifecycle P50/P95
were `90.07/139.71 ms` at `10.06 tasks/s`. The run made no new model calls and
performed no real settlement or fulfillment. The 500/500 plan gate is over the
locked synthetic suite and replayed GPT-6 Luna outputs, not an independent
quality score. The local ShoppingComp compatibility audit found exact-title
matches for `0/472` reference products, so those open-world tasks were not
misrepresented as mapped StablePay E2E trials. Metrics, SQL audit, manifest,
and trials are retained under
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T071215.595159200Z-http-edbe98a5b09f5c52/`.

This is a bounded local E2E correctness experiment and an external ranking
reference. The independent, blinded human-authored 500-task shopping holdout
remains outstanding; neither result should be described as the target resume
benchmark.

### B0.1 — optional-style decision fix and oracle rerun (2026-10-11)

Metric boundaries are now explicit: target-product Recall@K requires independent
human-adjudicated relevant SKU labels; snapshot-feasible SKU Recall@K measures
retrieval of generated offers feasible under the pinned inventory; task-hit@K
only asks whether at least one generated feasible SKU appears; valid-plan rate
requires every hard constraint and current offer fact to pass; decision accuracy
requires independently reviewed PLAN / NO_PLAN / CLARIFY labels. None of these
synthetic oracle outputs substitutes for the missing human labels.

The prior challenge harness/runtime incorrectly treated an omitted
`preferred_style` as a blocking missing field. Style is a soft ranking hint, not
a required slot, so a task with item, format, region, and budget but no stated
style must remain actionable. The single- and batch-intent prompts and
validators now permit an empty style; clarification generation now cycles only
through missing item, missing format, and both missing. The challenge-suite
validator rejects `preferred_style` as a required missing field. NO_PLAN quota
allocation now redistributes only across mechanisms supported by the pinned
snapshot; it does not manufacture inventory gaps to satisfy a preset stratum.
Offline tests passed for `./benchmarks/agentshopping` and
`./cmd/agentshopping-sim` with `GOPROXY=off`.

The new v7 generated diagnostic has 500 tasks: 350 PLAN, 75 NO_PLAN, and 75
CLARIFY. Its available NO_PLAN mechanisms are 50 price-boundary and 25 empty
attribute-intersection cases; the current catalog cannot support a
region-unserved quota. Clarifications are evenly split 25/25/25 across missing
item, missing format, and both. The read-only difficulty audit confirms the
task, trial, manifest, and metrics hashes agree; all 350 PLAN prompts carry two
generated hard attributes. The artifacts are under
`.local-run/agent-shopping/challenge-suites/draft-mixed-v7/` and
`.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T074204.097744100Z/`.

With gold intents supplied directly to the oracle (zero model calls), B0 valid
plans were `206/350` and B1 valid plans `350/350` (paired delta `+41.14 pp`);
plan task-hit@20 was `292/350`; snapshot-feasible SKU macro Recall@20 was
`51.88%`, with micro Recall@20 `38.91%` (`335/861`). The runtime correction explains removal of the 70
style-only false clarifications from the earlier oracle run; the retrieval
metric also differs from the older run, whose executable/source version is not
proven identical, so do not attribute that entire difference to this fix. The
100% oracle decision/valid-plan rates are not model results: expected intent and
disposition are given to the evaluator. Independent target-product Recall@K
remains `NOT_AVAILABLE_NO_INDEPENDENT_RELEVANCE_LABELS` (0/350 plan tasks have
human relevance labels). This diagnostic wrote no simulation database rows,
made no model/API calls, and performed no real payment.


A follow-up oracle diagnostic on the same pinned v7 task bytes completed as
`shopping-challenge-20261011T075533.169504600Z`. The read-only difficulty audit
reconciled 500 tasks/trials and all task, manifest, and metrics hashes. It found
B0 valid plans at 206/350 (58.86%) and B1 at 350/350 (100%; paired +41.14 pp);
plan-hit@20 was 337/350 (96.29%). Snapshot-feasible SKU Recall@20 was 75.98%
macro / 64.23% micro (553/861); target-product Recall@20 remains unavailable
because 0/350 PLAN tasks have independently reviewed relevance labels. Of the
350 PLAN prompts, 350 contain the canonical item literal, 347 the canonical
format, 277 the canonical style, and all encode exactly two required hard
attributes. NO_PLAN is only 50 price-boundary + 25 empty-attribute-intersection
tasks because this snapshot cannot support an unserved-region quota; CLARIFY is
25 each for missing item, missing format, and both.

The apparent 100% values have different, bounded meanings: exact intents are
425/425 and decisions 500/500 because this run is ORACLE-INTENT mode (expected
intent/disposition supplied; zero model calls), not model accuracy. “Valid plan”
selects an auto-generated gold SKU that passes the snapshot's hard-attribute,
price, region, stock, quote, and confirmation checks; it is not an independent
user-relevance judgment. B1 filters/ranks the full 100-product discovery window
before truncating to 20, while plan-hit@20 checks gold SKUs in the first 20 raw
discovery results; 13 tasks therefore had a valid plan despite no raw top-20
gold hit. The earlier v7 run
`shopping-challenge-20261011T074204.097744100Z` has different retrieval counts;
the manifests do not pin an evaluator source/binary hash, so that delta cannot
be attributed to a particular code change. Latest artifacts and hashes are
under `.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T075533.169504600Z/` and
`.local-run/agent-shopping/challenge-suites/draft-mixed-v7/`. The v7 suite
remains generated, unreviewed diagnostic data and is not a resume-quality score.

No additional public dataset or repository was downloaded in this pass. The
existing ShoppingComp and ESCI snapshots on D: are references only. Dataset
review indicates ShoppingComp is the closest source for scenario rubrics,
Amazon ESCI for graded product relevance, and WebShop for multi-step task shape;
none supplies StablePay's complete PLAN / NO_PLAN / CLARIFY or live inventory,
consent, and payment labels. Keep the StablePay holdout self-authored and
independently reviewed rather than presenting a public benchmark or this
synthetic oracle as the 94.2% / 91.8% resume result.

### B0.2 — public-reference review and fresh 500-task HTTP/MySQL E2E (2026-10-11)

No new dataset bytes were downloaded. The already-present D:-drive snapshots
were reused: ShoppingComp is pinned at `201dd305bba00801df730934e833e6fa546876cc`
and is 6,765,036 bytes across 290 English/Chinese task instances; the local
ESCI preparation pins a 114,812,289-byte compressed test split and a deterministic
500-query / 9,214 judged-pair sample. ShoppingComp is a small expert-rubric and
safety-case reference; ESCI supplies Exact/Substitute/Complement/Irrelevant
product judgments. Neither contains StablePay's live offer, stock, delivery,
consent, or payment ground truth. The ESCI file is a third-party preparation;
its local manifest preserves the mirror revision and cautions against source
redistribution. The [ShoppingComp dataset card](https://huggingface.co/datasets/huaixiao/ShoppingComp)
and [Amazon ESCI repository](https://github.com/amazon-science/esci-data) were
used for methodology/source cross-checks. Public-query relevance was not
silently mapped onto the unrelated synthetic Merchant catalog.

A fresh run of `TestMySQLShoppingLocked500HTTPLifecycle` completed as
`lifecycle-20261011T080502.937245500Z-http-30dca910123d98dd`. It loaded the
current 12 Merchant HTTP fixtures (24,000 synthetic active products), seeded a
new tenant in the isolated `stablepay_shopping_sim` MySQL database, and replayed
the existing 500 recorded GPT-6 Luna intent responses through the HTTP API.
All 500 plans were valid and reached a terminal state; 4,352 HTTP requests had
2,152 responses 200, 2,100 responses 201, and 100 expected stale-quote 409s,
with zero unexpected HTTP failures. The deterministic payment mock produced
483 settlements and 17 declines; the run exercised 50 price revisions, 50
stock-loss mutations, and 100 stale-quote rejection attempts. SQL audit passed
with zero invariant violations. Sequential lifecycle P50/P95 were 90.42/137.71
ms and throughput was 9.95 terminal tasks/s. The test cleanup hook removed the
new tenant and verified its tenant-scoped row counts returned to zero; the
12 fixtures were stopped afterward. There were zero new model calls, no real
settlement, and no fulfillment against a real carrier.

This is transaction-lifecycle E2E correctness evidence over a synthetic,
recorded-intent cohort, not a product-quality score: its 500/500 gold hits and
valid plans are not independent relevance or decision labels. ESCI ranking
metrics remain a separately named public reference and are not included in the
StablePay resume denominator. Target-product Recall@K and the blinded,
human-reviewed 500-task holdout remain unavailable. Go cache and temporary
files were directed to D:; C: had 12.74 GiB free before and after the run. The
isolated MySQL data mount is on D:, and no dataset was downloaded during this
run.

### B0.3 — buyer-approval MySQL safety rerun (2026-10-11)

The known 9-scenario `TestMySQLHTTPShoppingLifecycleRequiresFreshConsent`
verification was rerun after the separate buyer-approval credential was added.
All 9 scenarios passed against the existing isolated `stablepay_shopping_sim`
database using a fresh tenant; the test cleanup hook verified that tenant had
zero remaining rows. Cases covered curated substitution and fresh confirmation,
price and stock revision re-quoting, last-moment stock loss, explicit rejection,
ambiguous clarification, impossible budget, unsupported region, and forbidden
substitution. The result reported zero duplicate confirmation rows and no real
payment adapter. Go's JSON event log is retained at
`.local-run/agent-shopping/integration-evaluations/mysql-http-fresh-consent-20261011T081248Z/go-test.jsonl`
(SHA-256 `3f15bfc1da6145c32eeb106cfecca38459eceeff272fcca4073ceeff053c1b91`).
This closes the stale integration-verification note in `RESUME_EVIDENCE.md`; it
does not establish generic substitute quality or real-provider behavior.

### B0.4 — real GPT-6 Luna challenge parse and 300-order sandbox replay (2026-10-11)

The 500-task `draft-mixed-v7` suite was run through the actual
`openai-codex/gpt-6-luna` provider path (10 batches of 50; no DeepSeek or `.env`
key). The completed run is
`.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T085437.957245400Z/`.
It recorded 105,588 input and 33,552 output tokens; API-call count,
temperature, `top_p`, and `max_tokens` were unavailable from the provider/CLI.
The run manifest contains model/prompt/template hashes and no credential
material. All 500 parse and trial rows are present; parser failures were 0.

Observed diagnostic metrics: correct decision 498/500 (99.6%); exact intent
425/425 complete-intent tasks; correct clarification 73/75; B1 valid plan
350/350 versus B0 206/350 (paired +41.14 percentage points); raw-discovery
plan-hit@20 337/350 (96.29%); snapshot-feasible SKU Recall@20 75.98% macro /
64.23% micro (553/861). The 500 tasks are auto-generated and unreviewed, with
canonical item/format cues and catalog-derived labels. Therefore these values
are not an independent relevance score: exact-target product Recall@K remains
`NOT_AVAILABLE_NO_INDEPENDENT_RELEVANCE_LABELS`, and none of these metrics is
a resume holdout claim. The near-ceiling decision/plan rates support the concern
that this draft is too easy for the desired quality claim. The evaluator and
prompt parsing changes passed targeted Go tests; JSON arrays are extracted
strictly from model output, while irrecoverably malformed batches are accounted
as parse failures rather than aborting the run.

The 300-order test was then run against the existing isolated MySQL database
with its locked catalog snapshot (the HTTP-catalog variant refused to proceed
because the live catalog hash differed from the purchase baseline). Artifacts:
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T085605.852589900Z-purchase-replay-35649dd6f5292547/`.
All 300 logical attempts produced 300 unique orders and mock payments: 287
settled/fulfilled and 13 deterministically declined. It injected 200 lost
post-commit responses, 100 concurrent duplicate pairs, 100 API recreations
(including one OS-process restart); no duplicate order/payment/settlement or
fulfillment was observed. All 29 scheduled transient delivery faults recovered
in 316 provider attempts. SQL/offline audit PASS; lifecycle P50/P95 were
77.111/98.668 ms and fulfillment P50/P95 were 16.915/21.234 ms. The isolated
tenant was cleaned and verified empty. This run reused the recorded GPT-6 Luna
intent cohort (zero new model calls) and deterministic mock payment/fulfillment;
it is not a real-payment, Devnet, external-provider, or production reliability
claim.

### B0.16 — ESCI public-reference HTTP/MySQL search run (2026-10-11)

Ran the already-local 500-query ESCI-US test sample through the authenticated
shopping HTTP API backed by an isolated MySQL tenant. The sample contained
7,771 judged candidate rows and 1,631 exact labels. This was a sequential,
single synthetic Merchant fixture; all projected inventory was in stock at
US$10 and the region overlay was the local synthetic `CN-EAST` region. The
request adapter used a deterministic structured-intent oracle derived from
each query's sample group, so this measures candidate retrieval/ranking, not
natural-language intent parsing. No model call or dataset download occurred.

The API candidate order matched the runtime ranker's order for 500/500
queries. Exact target Recall@5 was 88.03% macro / 86.70% micro; query hit@5
was 98.00%, and NDCG@5 was 0.9015. Sequential request latency was P50/P95
24.713/37.479 ms (37.65 queries/s). All 500 requests returned HTTP 200. The
isolated tenant contained 500 task/trace rows during the audit and zero quote,
order, reservation, confirmation, or payment side effects; audit and cleanup
passed. Artifacts are in
`.local-run/agent-shopping/reference-evaluations/esci-http-mysql-20261011T111958.605159700Z/`.

This is useful public judged-candidate-pool reference evidence, not a full
marketplace-catalog recall result: each query was ranked only against its
provided ESCI candidate pool, the exact-match target is narrower than ESCI's
graded relevance labels, and structured intents were supplied by an oracle.
It does not substantiate the 94.2% resume target or end-to-end purchase
quality. Keep it as a reference benchmark, not a StablePay resume claim.

### B0.17 — title phrase reranking, query-disjoint holdout, and HTTP/MySQL confirmation (2026-10-11)

Added a bounded ordered-title-bigram coverage feature to the existing BM25
discovery ranker. It rewards adjacent query-token pairs preserved in the
product title; it is a soft ranking signal and does not override exact-item,
substitution, hard-attribute, stock, region, price, or approval rules. The
catalog index now caches the title token sequence used by this feature.

The feature weight was swept only on the 500-query `sparse-exact` development
sample (652 Exact judgments, 7,818 judged candidates), with phrase weights
0.25/0.5/1/2/4/8 and K=5/10/20. Weight 1.0 was selected for the declared
primary K=5 exact-recall objective: relative to the prior title-coverage-only
ranker, development Recall@5 changed from 93.34%/91.41% macro/micro to
93.59%/91.72%; query hit@5 from 94.6% to 94.8%; NDCG@5 from 0.89167 to
0.89372. The selected configuration was then evaluated once on the fixed
500-query `uniform-exact-holdout` sample (9,194 judged candidates), whose
manifest excludes both the development sample and the earlier sparse sample.
At K=5 it scored 36.32%/30.65% macro/micro Recall, versus 36.24%/30.62% for
the prior ranker; query hit@5 was 100% and NDCG@5 was 0.97294 versus 0.97194.
At K=20 it scored 95.54% macro / 90.44% micro Recall and NDCG 0.98448.

The K=5 recall ceiling on this uniform holdout is only 38.10% macro / 31.84%
micro because many queries have more than five Exact judgments. The evaluator
now reports this mathematical ceiling alongside observed recall. Therefore
the 95.54% macro K=20 figure cannot be shortened into an unqualified “94.2%
recall” claim; cutoff, averaging method, judged-pool boundary, and relevance
definition must be stated.

Finally, the selected production ranker was executed through the authenticated
HTTP API with an isolated MySQL tenant on the same query-disjoint holdout:
500/500 HTTP 200 responses, API order matched the runtime ranker for 500/500,
K=5 exact recall 36.29%/30.64%, hit 100%, NDCG 0.97270, and P50/P95 latency
18.418/22.068 ms (sequential throughput 51.21 requests/s). The SQL audit passed; all 500 task/trace rows were removed,
and there were zero quotes, reservations, confirmations, orders, or payments.
The D-drive-backed simulation MySQL container was stopped afterward and its
ignored data was retained. No data was downloaded and no model was called.
Artifacts:
`.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T112536.543565500Z/`
(development sweep),
`.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T113327.137649500Z/`
(query-disjoint ranker evaluation), and
`.local-run/agent-shopping/reference-evaluations/esci-http-mysql-20261011T113245.948409300Z/`
(final HTTP/MySQL run after the allocation optimization). These remain public judged-candidate-pool search evidence,
not full-marketplace retrieval or purchase-quality evidence, and not a resume
claim.

### B0.5 — Runtime BM25 ranking and public reference run (2026-10-11)

Discovery now carries the transient buyer prompt into candidate ranking and
computes BM25 over title, description, tags and attributes. Existing routing
and eligibility checks run first; exact items remain ahead of curated
substitutes, and complete hard-attribute matches remain ahead of text
relevance. No price, inventory, region, consent, checkout, or payment rule was
weakened. Chinese Han text is tokenized into overlapping bigrams; English and
numeric terms remain word tokens.

Targeted retrieval tests and the complete
`go test -count=1 ./benchmarks/agentshopping ./cmd/agentshopping-sim` passed.
The saved GPT-6 Luna parse results were replayed offline, without new model
calls: 500/500 tasks evaluated, 0 parse failures, 498/500 decisions, and
350/350 valid plans. This confirms no regression on that generated diagnostic
suite, but that suite does not discriminate ranking quality well enough to
claim a product-relevance improvement.

The new code path was then run on the existing pinned sparse-Exact ESCI sample
(500 query groups, 7,818 judged candidates, 652 Exact labels). Runtime BM25
scored 91.92% macro / 89.88% micro Exact recall at K=5; the same runner's
title-overlap reference scored 89.30% / 88.65%. At K=10 the respective results
were 96.97% / 97.09% and 96.14% / 96.32%; both reached 100% at K=20, reflecting
the judged-candidate-only boundary. The benchmark output is bound to the
sample hash at
`.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T090645.247069200Z/metrics.json`.
It excludes unjudged catalog negatives and all Merchant feasibility,
transaction, payment, and fulfillment labels, so these figures are reference
ranking diagnostics only—not StablePay's 94.2% target-product recall or any
resume metric. No additional dataset bytes were downloaded. The 12 simulated
Merchant fixtures were shut down after the local replay.

The current code then completed a fresh 300-attempt locked-MySQL mock purchase
replay at
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T090826.203333700Z-purchase-replay-5bd64b9f4bb89f8b/`.
Results: 287 settled/fulfilled, 13 deterministic declines; 29/29 scheduled
transient delivery failures recovered in 316 provider attempts; 200
post-commit response losses, 100 concurrent duplicate pairs, 100 API
recreations including one process restart; zero duplicate orders, payments,
settlements, or fulfillments. SQL and fulfillment audit summaries passed with
zero invariant violations, and the test cleanup hook verified an empty tenant.
Sequential lifecycle P50/P95 were 76.916/95.176 ms and fulfillment P95 was
20.410 ms. This rerun used only deterministic local mock payment/fulfillment
adapters; the existing isolated MySQL dataset was preserved, no Docker action
was taken, and no real settlement was attempted.

### B0.6 — MCP subprocess to persisted mock purchase (2026-10-11)

The compiled `agentshopping-mcp` binary was launched as a real stdio child
process. Its tool calls crossed the MCP protocol, allowlisted HTTP bridge, the
authenticated local simulation API, and an isolated MySQL tenant. One seeded
purchase traversed search → immutable quote → confirmation by a separate
synthetic test actor → mock checkout → order-status read → fulfillment. The
buyer-approval credential was not passed to the MCP process, and the test does
not claim human consent. Checkout and fulfillment were replayed with the same
idempotency keys.

The run passed at
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T091051.440810700Z-mcp-subprocess/`.
SQL recorded exactly one confirmation, reservation, order, mock payment,
fulfillment and fulfillment attempt; final state was `FULFILLED` / `DELIVERED`,
inventory was 3 on-hand / 0 reserved, and real settlement was false. The
test's tenant cleanup completed without error. Manifest records the MCP binary
SHA-256. This is integration evidence for one deterministic scenario, not
human-approved purchase quality, model decision quality, Devnet settlement, or
production reliability.

### B0.7 — full 500-task lifecycle and fresh 300-attempt resilience rerun (2026-10-11)

The benchmark inputs were already prepared and hash-locked, so this pass moved
directly to execution rather than another data-acquisition phase. Before the
500-task E2E, the local resource guard observed C: free 12.6 GiB, D: free
1,372.3 GiB, and 9.2 GiB available RAM; the isolated MySQL endpoint was
`127.0.0.1:13327`. Twelve simulated Merchant HTTP fixtures were started by this
run, passed health checks, and were then cleanly stopped. No dataset was
downloaded and no new DeepSeek or GPT-6 Luna API call was made.

The full lifecycle run is
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T092107.495161600Z-http-f4ffebb06598d1cf/`.
It used the current Merchant HTTP product snapshot and the recorded 500-task
GPT-6 Luna intent replay against an isolated MySQL tenant. Results: 500/500
plans accepted and tasks terminal; 483 mock payments settled, 17 declined; 50
price changes, 50 stock-loss cases, and 100 stale quote confirmations rejected;
4,352 HTTP requests, zero unexpected HTTP failures. The captured SQL summary
reports 500 tasks, confirmations, reservations, orders, and simulated-payment
rows; 600 quote/confirmation-attempt rows; 150 inventory adjustments; and zero
invariant violations. Sequential task P50/P95 were 114.341/201.130 ms and
throughput was 7.609 terminal tasks/s. The 500/500 plan/terminal result is
specific to this generated fixed suite, not an independent plan-quality result.

The fresh 300-attempt run is
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T092309.371460200Z-purchase-replay-de5fc2809617f7c2/`.
An initial Merchant-HTTP-source attempt correctly stopped at its snapshot-hash
guard: the live v2 product projection (`f5100a…`) differs from the locked MySQL
v1 projection (`7fdb29…`), while task, overlay, and manifest hashes matched.
The benchmark was rerun explicitly with
`-UseLockedMySQLCatalogSnapshot`; no hash gate was bypassed. Results: 300 unique
mock orders/payments; 287 settled and fulfilled, 13 deterministic declines;
29/29 predeclared transient delivery faults recovered in 316 provider attempts;
200 post-commit response losses; 100 API recreations (including one OS-process
restart); 100 concurrent duplicate pairs; and zero duplicate order/payment/
settlement/fulfillment or lifecycle invariant counts. The test performed its
live SQL audit before cleaning its isolated tenant. The separate
`scripts/audit-agent-shopping-purchase-replay.ps1` pass verified saved trial
rows, summary fields, and hashes offline (not a fresh DB query).

This is local simulated commerce evidence only: HTTP API harness, isolated
MySQL, synthetic merchants/catalog/tasks, recorded intent outputs, and mock
payment/fulfillment. It does not support human relevance/plan-quality claims,
real-provider reliability, or Devnet settlement. C: remained above the 10 GiB
guard throughout; all generated artifacts and Go cache remained under the
ignored D: `.local-run` directory. No source changes were made by the two runs.

### B0.8 — complete shopping API concurrency matrix and pool sizing (2026-10-11)

The full six-tier run is
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T092717.471221000Z-http-concurrent-b1092972363c6005/`.
It used 500 warmup plus 500 measured searches at each of 1/10/25/50/100/200
closed-loop workers, 128 MySQL max-open/max-idle connections, the recorded
500-task intent replay, current 24,000-product Merchant HTTP snapshot, and an
isolated tenant. Every tier passed with 500/500 measured requests, zero HTTP or
business errors, and 500/500 synthetic Top-1 matches. Throughput was
34.40/254.51/370.32/459.31/465.94/488.42 req/s; P95 was
32.97/49.38/84.16/129.72/268.71/544.49 ms. At 200 workers the SQL pool recorded
1,142 waits and 38.713 seconds cumulative wait. This closed-loop matrix is not
a sustained-duration k6 or deployed multi-host test.

Two bounded 200-worker pool checks followed: 143 connections
(`lifecycle-20261011T093146.252131700Z-http-concurrent-0ab2cce914024a8d`)
measured 459.75 req/s, P95 534.15 ms, and 30.605 seconds cumulative pool wait;
96 connections (`lifecycle-20261011T093214.063091700Z-http-concurrent-741c418662b40989`)
measured 462.65 req/s, P95 630.73 ms, and 86.151 seconds cumulative wait.
The modest P95 decrease at 143 came with lower throughput and only one sample;
128 remains the best observed throughput/latency balance, not a proven optimum.
The MySQL reserve guard permits up to 143 only after verifying the live server
connection budget (`max_connections=151`, eight reserved); no database limit
was changed. Removing the ORM's implicit transaction around the standalone
pending-task INSERT was also tested and reverted because its 200-worker rerun
did not improve the result (P95 559.43 ms, 469.85 req/s). No performance win is
claimed; latency at 200 remains above 0.5 seconds in this local harness.

### B0.9 — non-overlapping ESCI Runtime-ranking evaluation (2026-10-11)

The local ESCI source was already staged on D:. The sampler now accepts an
explicit prior sample to exclude, verifies each excluded query ID occurs in the
pinned source, and records the exclusion hash. It selected 500 new sparse-Exact
query groups after excluding all 500 IDs from the earlier sparse sample; the
two files have zero overlapping query IDs. No new dataset payload was
downloaded. New sample manifest and data:
`.local-run/agent-shopping/reference-data/esci-us-test-20261011/sparse-exact-disjoint-20261011/{reference-sample-manifest.json,esci-us-test-500-queries.jsonl}`.

The actual Go Runtime ranking diagnostic is
`.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T093418.933528600Z/metrics.json`.
On 7,771 judged candidates and 1,631 Exact labels, Runtime BM25 measured Exact
Recall@5 86.96% macro / 85.35% micro and Recall@10 97.05% / 96.93%; title
overlap measured 85.94% / 84.49% at @5 and 95.55% / 95.28% at @10. Both rankers
reached 100% at @20 because the sample averages 15.54 judged candidates/query;
that cutoff is therefore uninformative. This is a genuinely disjoint query-ID
sample from the same public ESCI test split, not an independent StablePay
catalog or human shopping holdout. It evaluates only judged candidates and
excludes unjudged catalog negatives, Merchant feasibility, inventory, budget,
approval, checkout, payment and fulfillment; do not use it as the requested
StablePay 94.2% resume claim.

### B0.10 — fresh GPT-6 Luna 500-task discovery/plan diagnostic (2026-10-11)

After the local challenge data and Merchant fixtures were ready, a fresh model-
backed evaluation was run rather than another audit-only pass. The 12 local
Merchant HTTP fixtures passed health checks and were stopped after the run.
Artifacts are under
`.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T094422.074741800Z/`.
The run parsed all 500 `draft-mixed-v7` tasks with `openai-codex` / `gpt-6-luna`
(`reasoning=none`, 10 Hermes CLI batches); it was not a stored-output replay and
did not call DeepSeek. Token usage was available: 95,604 input and 33,562 output
tokens. Sampling parameters and API-call count were not reported by the CLI;
the manifest records that unavailability, plus prompt-template, prompt and
response hashes.

Results: 499/500 correct decisions; on 350 PLAN tasks, 350 valid plans versus
206/350 for B0 (58.86%), a paired diagnostic delta of +41.14 percentage points.
Feasible-gold SKU Recall@20 was 78.04% macro / 67.02% micro, and 342/350 PLAN
tasks retrieved at least one feasible gold SKU. NO_PLAN was 75/75; CLARIFY was
74/75. One clarification task (`challenge-shop-0344`, `CLARIFY+MISSING_ITEM`)
failed intent validation (`INVALID_INTENT`). Exact-target product recall is
unavailable because this generated suite has no independent `relevant_skus`
labels.

This is an actual GPT-6 Luna + local catalog ranking/plan-evaluation run, but
not a full order/payment E2E: it made no simulation-database writes and no
purchase, payment, or fulfillment calls. The 100% PLAN-valid rate and large B0
delta are not independent quality evidence: task prompts and gold labels are
generated from the same small synthetic catalog, and provenance remains
unverified. Do not use these values as resume claims. The separate B0.7 HTTP-to-
MySQL lifecycle and purchase/replay results remain deterministic transaction
simulation evidence, not evidence that this fresh model run executed orders.

### B0.11 — Runtime title-coverage rerank on disjoint ESCI query samples (2026-10-11)

To address the gap between the synthetic suite's perfect plan-valid score and
its unavailable independent target recall, the Runtime discovery ranker now
adds a bounded query-token coverage bonus from product titles (weight 2.0) on
top of BM25. The weight was selected on the 500-query sparse-Exact development
sample only. The immutable catalog index caches title tokens with the complete
search document; hard-attribute, exact-item/substitute, stock, and price
precedence are unchanged. A repeatable `--exclude-sample` option now accepts
multiple prior samples and rejects overlapping or unresolved excluded IDs.

On the development sample, Runtime BM25 Recall@5 changed from 91.92% macro /
89.88% micro to 93.34% / 91.41%; NDCG@5 changed from 0.8790 to 0.8917.
The selected weight was then checked without tuning on two samples with zero
query-ID overlap against the development set (and zero overlap with each
other):

* Sparse-Exact 500-query sample, 7,771 judged candidates and 1,631 Exact
  targets: Recall@5 improved from 86.96% macro / 85.35% micro to 88.18% /
  86.88%; NDCG@5 was 0.8907 → 0.9019. Exact-query hit rate remained 98.0%.
  Artifact: `.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T095451.153935600Z/metrics.json`.
* Uniform 500-query sample drawn after excluding both earlier 500-query samples
  (1,000 excluded IDs, all verified against the source), with 9,194 judged
  candidates and 7,752 Exact labels: at K=20, macro/micro recall was 95.44% /
  90.25% for the original BM25 and 95.53% / 90.40% for the selected rerank;
  NDCG@20 was 0.9814 → 0.9841. This broader sample is not the sparse-Exact
  distribution and must not be compared to its K=5 numbers. Artifact:
  `.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T095453.999476000Z/metrics.json`.

The bounded 2,000-candidate Go microbenchmark measured about 36.3 μs/op both
with and without the title bonus, with the bonus adding 224 B and 9 allocations
per operation. This is not an end-to-end HTTP performance claim. The complete
`go test -p 1 -count=1 ./...` suite passed after the ranker change.

The reference data is Amazon ESCI's US test split and each query's judged
candidates only; it has no unjudged full-market negatives or StablePay live
offer/region/budget labels. In particular, the 95.53% figure is K=20 on a
different, dense-Exact distribution, not the requested StablePay 94.2% target
recall claim. The sparse K=5 holdout remains 88.18%. Treat this as a measured
ranker improvement on a public reference benchmark only; the official
500-task, human-reviewed StablePay relevance and plan-quality gate remains open.

### B0.12 — exact-target labels and fresh offline 500-task replay (2026-10-11)

The mixed-draft generator now writes `relevant_skus` separately from feasible
`gold_skus`. For each PLAN task, relevant SKUs are active USDC listings matching
the exact requested category/item/format and all hard attributes. Stock,
delivery region, and price do not affect relevance; they remain feasibility
filters. NO_PLAN and CLARIFY tasks carry no product-relevance labels. The
generator is deterministic and the labels are still synthetic, unreviewed
machine labels—not human judgments.

To avoid spending another model call merely because labels were added, the
replay path gained an explicit `--allow-relevance-label-only-replay` opt-in.
It requires a v4/v5 artifact-hash-bound source suite and verifies task order,
prompts, intents, decision labels, feasible gold labels, catalog, inventory,
and source manifest are identical; only missing `relevant_skus` may be added.
The default replay remains strict. Source and evaluation task hashes are both
recorded in the output manifest.

Using the saved 2026-10-11 GPT-6 Luna run
`shopping-challenge-20261011T094422.074741800Z`, the new v8 suite was actually
re-evaluated through the Runtime parser-result replay, discovery ranker, live
snapshot feasibility checks, and plan validator. No model calls were made.
Artifacts:
`.local-run/agent-shopping/challenge-suites/draft-mixed-v8/` and
`.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T100628.845975700Z/`.

On 500 generated tasks (350 PLAN), stored outputs again produced 499/500
correct decisions, 350/350 valid plans versus 206/350 B0, feasible-SKU
Recall@20 75.98% macro / 64.23% micro, and 350/350 target-product task hits.
The newly computable target-SKU Recall@20 is only **64.41% macro**
(task-bootstrap 95% CI 61.91–67.09%) / **50.13% micro** (6,484 of 12,934
relevant SKUs retrieved); its status is `AVAILABLE_UNVERIFIED_RELEVANCE_LABELS`.
Thus task-hit and valid-plan rates do not imply high SKU-level target recall.
The high decision/plan scores still come from a generated suite derived from
the same synthetic catalog and are diagnostic only; neither the 94.2% recall
nor the 72.6%→91.8% independent plan-rate claim is established.

### B0.13 — fresh 300 logical-purchase crash/replay E2E (2026-10-11)

After correcting local port detection (the Windows environment returned zero
rows from `Get-NetTCPConnection` even while all 12 fixture APIs were healthy),
the complete `benchmark-agent-shopping-300-purchases.ps1` run passed using the
existing isolated loopback MySQL and `-UseLockedMySQLCatalogSnapshot`. The
Merchant HTTP snapshot had a different catalog hash than the locked intent
run, so the test refused to mix them; rerunning on the matching locked MySQL
snapshot was successful. No Docker image, external dataset, new model call,
real payment, chain settlement, or real fulfillment was used.

Run artifacts:
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T100831.778001400Z-purchase-replay-b0a4c58c61931067/`.
All 300 logical purchase attempts produced 300 unique orders and 300 unique
mock payment records: 287 settled and fulfilled, 13 deterministic declines.
There were 200 post-commit response losses, 100 API recreations (including one
OS-process restart), and 100 concurrent duplicate-request pairs. The SQL and
fulfillment audits passed: duplicate order groups/orders/payments/settlements/
fulfillments were all zero; 29/29 predeclared transient delivery failures
recovered; no orphan fulfillment attempts or non-simulation effects were
observed. The isolated tenant was removed and verified empty. This is
deterministic local mock correctness evidence, not a real-payment or
production reliability rate, and the 287/300 result should not be conflated
with the illustrative 286/300 external-project target.

### B0.14 — report Recall@K cardinality ceilings (2026-10-11)

The 500-task v8 result prompted a ceiling audit. Across the 350 PLAN tasks,
the generated machine-label set contains 12,934 exact-match SKUs (mean 36.95;
292 tasks have more than 20). For any task with more than K relevant SKUs,
Recall@K cannot exceed K divided by that set size. The evaluator now emits the
maximum possible retrieved-SKU count and macro/micro recall at K, plus the
number of plan tasks whose relevance-set cardinality exceeds K. A regression
test checks the 80% upper bound for 25 relevant SKUs at K=20; challenge report
schema is now v7.

The observed v8 results reached this label-cardinality ceiling at every
measured cutoff: K=20 was 6,484/12,934 = 50.13% micro and 64.41% macro;
K=50 was 11,021/12,934 = 85.21% micro and 93.33% macro; K=100 was 100% for
both. The corresponding numbers of PLAN tasks with more relevant SKUs than K
were 292, 66, and 0. Run artifacts are
`shopping-challenge-20261011T101548.658971600Z/` (K=20),
`shopping-challenge-20261011T101551.670669700Z/` (K=50), and
`shopping-challenge-20261011T101554.543259000Z/` (K=100) under
`.local-run/agent-shopping/challenge-evaluations/`.

Reaching the ceiling means all returned SKUs matched the same programmatic
exact-item/attribute predicate used to construct the relevance labels; it
does **not** demonstrate independently judged ranking quality. The apparent
94.2% result can be crossed by changing K, not by improving the ranker, so
neither this cutoff sweep nor its 100% K=100 result is a resume claim. Keep the
predeclared production cutoff fixed and replace this generated suite with
independently authored prompts and adjudicated relevance labels before making
the requested StablePay target claim.

### B0.15 — v8 challenge HTTP-to-MySQL transaction E2E (2026-10-11)

The 500-task v8 challenge was executed end-to-end through the local shopping
HTTP API using the 12 loopback Merchant HTTP catalog fixtures and a dedicated
isolated MySQL tenant. The challenge reused the saved GPT-6 Luna parse records;
there were no new model calls. The run passed its SQL audit and per-test tenant
cleanup. It recorded 350 expected PLAN tasks, 75 NO_PLAN tasks, and 75
CLARIFY tasks. All 350 PLAN tasks had a valid top candidate and reached a
terminal simulation-only checkout outcome: 337 mock payments settled and 13
were deterministically declined. Of the 75 clarification tasks, 74 persisted
`CLARIFICATION_REQUIRED`; one had a recorded `INVALID_INTENT` parse failure
and safely created no order or payment. Overall challenge decisions were
499/500, not 500/500.

The run also caught a ranking integration defect: the MySQL inventory overlay
query is unordered, and the response path had re-sorted its rows by price/SKU,
discarding the BM25 product order. Candidate responses now restore the
pre-filter discovery rank after the overlay join. A MySQL-backed HTTP
regression with a cheaper, less-relevant decoy verifies that the higher-ranked
product remains first. Re-running the complete v8 suite changed the top SKU on
35/500 tasks while preserving 499/500 labeled decisions and 350/350 valid
plans. It exercised 38 price increases, 35 stock-loss changes, and 73
stale-quote confirmation rejections before fresh quote/consent flows. The run
issued 3,556 HTTP requests, with task-level P50/P95 `111.270/181.766 ms` and
sequential throughput `9.839 tasks/s`; this single-host rerun was slower than
the prior sample, so the change is not a performance claim. The 350
order/payment records reconciled with zero SQL invariant violations. Current
artifacts:
`.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T111312.706274600Z-challenge-http-1731bd3c8f0f353e/`.

This is transaction-lifecycle correctness evidence on generated,
unreviewed, same-catalog tasks, not a concurrent load test, real payment,
Devnet settlement, or human-reviewed shopping-quality score. Target-product
Recall@K was not measured in this HTTP run; the earlier 64.41% macro / 50.13%
micro v8 figure uses unreviewed machine-generated relevance labels and is not a
resume claim. Keep both the 499/500 decision score and 350/350 plan rate
diagnostic-only until the prompt set and labels pass independent review.

### B0.18 — current-code v8 HTTP/MySQL replay and ranker reevaluation (2026-10-11)

Re-ran the current working tree after the shopping Runtime and benchmark changes.
The 500-task challenge traversed the loopback Merchant HTTP fixtures, shopping
HTTP API, and a fresh isolated MySQL tenant. It passed with 499/500 labeled
decisions, 350/350 expected PLAN tasks reaching terminal checkout simulation,
337 mock payments settled and 13 deterministically declined. The run applied
38 price changes and 35 stock changes; 73 stale-quote confirmations were
blocked. There were 3,556 HTTP requests: 2,009 HTTP 200, 1,473 HTTP 201, 73
expected HTTP 409, and one expected HTTP 502 for the recorded
`INVALID_INTENT` clarification parse failure. No unexpected HTTP failures or
SQL invariant violations occurred. Task-level P50/P95 was 92.534/140.961 ms
and sequential terminal throughput was 12.127 tasks/s. The 502 case created
no candidate or purchase side effect. The tenant cleanup gate passed; all 12
Merchant processes and the labeled MySQL simulation container were stopped,
with ignored D:-drive data retained.

Artifacts: `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T113922.615260200Z-challenge-http-b322e71cecfcb042/`.
This run reused saved GPT-6 Luna parse results (zero model calls), an
auto-generated/unreviewed task suite, deterministic mock payment, and no
fulfillment adapter (`simulated_fulfillment_attempts=0`). It is a current-code
checkout and transaction-safety replay—not independent decision quality, real
payment, fulfillment, or a production performance result. Its single-run
latency/throughput is not a controlled performance comparison.

The current ranker was separately reevaluated offline on the same v8 tasks at
K=20, again with no model calls:
`.local-run/agent-shopping/challenge-evaluations/shopping-challenge-20261011T114224.639869900Z/`.
Machine-labeled target-SKU Recall@20 remained 64.41% macro / 50.13% micro and
equaled the label-cardinality ceiling; 292/350 PLAN tasks have more than 20
labeled SKUs. Task hit@20 and valid-plan rate are both 350/350, while
feasible-SKU Recall@20 is 75.98% macro / 64.23% micro. These unreviewed
same-catalog labels and ceiling-bound metrics do not support the 94.2% recall
or 72.6%→91.8% resume claims. The independent, human-reviewed holdout remains
the quality gate.

### B0.19 — current-code 300 purchase/replay/fulfillment rerun (2026-10-11)

Re-ran the locked-snapshot 300-attempt transaction suite against the current
working tree and the isolated `stablepay_shopping_sim` MySQL schema. All 300
logical attempts yielded one unique order and one unique mock-payment record:
287 settled and fulfilled, 13 deterministic mock declines. Fault injection
covered 200 post-commit response losses, 100 API recreations (including one
OS-process restart), and 100 concurrent duplicate-request pairs. All 29
predeclared transient fulfillment failures recovered; there were 316 provider
delivery attempts total. The audit passed with zero duplicate order groups,
orders, payments, settlements, fulfillments, or refunds; zero orphan attempts,
attempt-sequence gaps, state/amount/currency mismatches, settled payments
without fulfillment, or non-simulation effects. Fulfillment HTTP P95 was
19.909 ms; task lifecycle P95 was 300.807 ms for this single local run. The
tenant cleanup gate passed; the labeled MySQL container was stopped and its
ignored D:-drive data preserved.

Artifact: `.local-run/agent-shopping/lifecycle-evaluations/lifecycle-20261011T114338.530220000Z-purchase-replay-d78c331498dc0e60/`.
This is deterministic local mock payment and fulfillment against the locked
synthetic catalog; it is not 300 external payment-sandbox or Devnet
transactions and must not be described as production success-rate evidence.
The complete `go test -p 1 -count=1 ./...` suite also passed with Go cache and
temporary directories on D: and `GOPROXY=off`.

### B0.20 — current production ranker on disjoint ESCI sparse-Exact sample (2026-10-11)

Ran the current production BM25 + title-coverage + ordered-title-bigram scorer
on the pre-staged 500-query sparse-Exact sample whose query IDs are disjoint
from the scorer's development sample. Among 7,771 judged query-product pairs
and 1,631 Exact labels, Recall@5 increased versus BM25 from 86.96% to 88.51%
macro and 85.35% to 87.19% micro; NDCG@5 increased from 0.89069 to 0.90518.
Exact-query hit@5 moved from 98.0% to 97.6%, so the reranker improves aggregate
retrieved Exact coverage while slightly reducing the number of queries with
any Exact result. At K=10 Exact recall was effectively unchanged/slightly
lower (97.05% to 96.97% macro), while NDCG improved; K=20 reached the
candidate-pool ceiling for every ranker. This is a public ESCI judged-candidate
reference, not full-catalog StablePay discovery or a private human-reviewed
500-task result.

Artifact: `.local-run/agent-shopping/reference-evaluations/esci-runtime-ranking-20261011T114737.061945700Z/metrics.json`.
The sample was already present locally; no network download or model call was
made. This result can support a separately labeled public-benchmark resume
bullet, but not the requested 94.2% StablePay target-product recall claim.
