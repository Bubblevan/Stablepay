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
