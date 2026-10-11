# AgentPay deterministic task benchmark v1

This fixed dataset contains eight distinct buyer-task profiles: four eligible
paid acquisitions with different semantic/budget selection conditions, and four
negative cases that must terminate without creating a PaymentIntent or
settlement. The eligible offers are three local mock capabilities with
different semantic tags and price hints. Each paid task has a ground-truth
merchant, capability, delivery-body SHA-256, one-intent/one-settlement
expectation, and a deterministic local payment adapter.

Run from the repository root:

```powershell
./scripts/run-agentpay-benchmark.ps1 -Trials 25
```

The default run executes 25 independent Runtime instances per task profile
(200 Episodes total). Pass `-OutDir benchmarks/agentpay-live-local-<date>` to
retain an evidence package under `benchmarks/`; the destination must be new or
empty. Each output includes `manifest.json`, `episode_results.jsonl`,
`grade_results.jsonl`, `metrics.json`, and `report.md`.

The manifest hashes the exact input dataset and records the trial count and
execution boundary. Every trial uses a fresh in-memory repository, deterministic
Merchant/Payment/Verification mocks, and the public HTTP/MCP/CLI client surface
over loopback. It makes no Solana transaction, DeepSeek request, Payment Service
call, or external network request. The three mock offers and eight task profiles
are a controlled harness, not a representative production catalog or 200 unique
real-world buyer prompts.

Metrics distinguish:

- capability/merchant selection accuracy, only over tasks with declared
  expected selections;
- delivered artifact body correctness, only over tasks with a declared
  expected hash;
- paid-task-to-validated-delivery success, with every submitted
  `RequirePayment` task—including collection failures—in the denominator;
- safe negative-task completion, represented in the deterministic per-task
  grader by expected terminal state and zero-intent/zero-settlement assertions.

For the retained v1 run, the dataset-level audit passed 200/200 task grades:
100/100 paid profiles reached validated delivery and 100/100 ineligible
profiles safely declined without a PaymentIntent or settlement. These counts
are repeated trials of eight profiles, not unique task prompts.

All percentages are descriptive for this fixed local fixture. They are not
Devnet success rates, model quality measurements, production SLOs, or evidence
of a larger third-party merchant marketplace.

## Recovery fault extension v1

`scenarios-recovery-v1.jsonl` is a separate, narrow extension; it does not
change the eight-profile v1 dataset or its historical metrics. It covers two
paid AgentPay profiles: two injected transient merchant failures before the
payment challenge, and one invalid first delivery after payment. Each profile
is expanded into fresh isolated Runtime trials. The grader requires the
expected merchant and artifact, one PaymentIntent, one settlement, and no
second payment; the delivery case must also enter business recovery.

Run it without changing the default suite:

```powershell
.\scripts\run-agentpay-benchmark.ps1 `
  -DatasetPath .\commerce-runtime\testdata\s11\agentpay-benchmark\scenarios-recovery-v1.jsonl `
  -Trials 25 `
  -OutDir '.local-run\agentpay-recovery-v1-<unique-run-id>' `
  -BinDir '.local-run\bin-agentpay-recovery-v1-<unique-run-id>'
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\audit-agentpay-recovery-benchmark.ps1 `
  -RunDir '.\.local-run\agentpay-recovery-v1-<unique-run-id>'
```

The runner uses the existing local Go module cache with `GOPROXY=off`, so a
missing dependency fails closed instead of downloading. It records the Git
base revision and whether the working tree was modified. The 50-trial result
in the evidence inventory is 25 repetitions of each of only two deterministic
fault profiles, using in-memory state and mock Merchant/Payment/Verification;
it is not a natural-language task-quality score, external payment result, or
production reliability estimate.

## Recovery fault extension v2

`scenarios-recovery-v2.jsonl` preserves the v1 fixture and adds a third paid
profile: the benchmark-only local Payment adapter deterministically commits
the mock transaction but loses the submit response. The Runtime persists an
`UNKNOWN` outcome, queries status, and reconciles the same PaymentIntent and
transaction. It adds no Payment Service, chain, MySQL, Docker, or network
dependency. The other two profiles are unchanged in intent and are copied into
the v2 manifest so the three fault types can be replayed as one matrix.

Run 25 fresh Runtime trials per profile and independently audit the result:

```powershell
.\scripts\run-agentpay-benchmark.ps1 `
  -DatasetPath .\commerce-runtime\testdata\s11\agentpay-benchmark\scenarios-recovery-v2.jsonl `
  -Trials 25 `
  -OutDir '.local-run\agentpay-recovery-v2-<unique-run-id>' `
  -BinDir '.local-run\bin-agentpay-recovery-v2-<unique-run-id>'
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\audit-agentpay-recovery-benchmark.ps1 `
  -RunDir '.\.local-run\agentpay-recovery-v2-<unique-run-id>' `
  -DatasetPath .\commerce-runtime\testdata\s11\agentpay-benchmark\scenarios-recovery-v2.jsonl
```

The retained 75-trial v2 run is 25 repetitions of each of three deterministic
fault profiles. Its 75/75 is a triggered-fault trial recovery score, not 75
unique tasks or an empirical reliability estimate. The response-loss profile
specifically proves only the in-process mock status-query recovery path; it
does not simulate an external payment provider or a process crash.

## Paired recovery-policy ablation v1

`scenarios-paired-recovery-v1.jsonl` is a separate two-arm comparison of one
fixed paid task with the same deterministic invalid-first-delivery fault. The
`baseline_stop` arm uses a benchmark-only deterministic policy that proposes
`STOP` after the invalid delivery; the `rule` arm uses the normal deterministic
RuleRecoveryProvider and retries the same merchant. The baseline is therefore
a **stop-without-retry control**, not a missing provider or a no-payment case.
Both arms may already have one settled mock payment when delivery fails. The
audit must report fulfilled tasks, settlements, refunds/compensations, and
duplicate settlements separately. Both rows keep `FULFILLED` as the buyer's
task objective, so the stop control correctly fails task grading when it ends
`FAILED`; its payment-safety assertions are evaluated independently. Zero
duplicate settlement does not imply zero loss.

The two rows share task ID, seed, request content, and injected fault; request
IDs and Runtime isolation IDs remain arm-specific. Expand with an equal number
of trials per arm to compare by `(task_id, trial_index)`. The smoke integration
test exercises one pair through the public MCP boundary. On 2026-10-11, the
suite ran 25 paired trial indices and the independent audit passed: the stop
control fulfilled `0/25`, RuleRecoveryProvider fulfilled `25/25`, with 50/50
fault applications, one mock PaymentIntent and settlement per arm, zero
duplicate settlements, and zero collection errors. The stop arm also left
`25/25` mock settlements without delivery or refund/compensation; this is an
important failed-purchase outcome, not a safety win. The general episode-grade
rate is not the purchase completion rate; use the audit's fulfillment counts.
Artifacts are local-only under
`.local-run/agentpay-paired-recovery-v1-20261011/`.

This repeats one deterministic synthetic task profile 25 times per arm; it is
not 25 unique buyer tasks, a marketplace estimate, or resume-claim evidence.
It uses only in-memory state and local deterministic mock adapters; no
container, database, real payment provider, chain call, or external model is
involved.
