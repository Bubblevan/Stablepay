# StablePay B0 Resume Benchmark Suite

This suite measures resume-relevant outcomes against the current production
Runtime source and does not edit `docs/RESUME_VARIANTS.md`.

The required execution order is E2 → E1 → E3 → E4. A false accept or protected
side-effect escape in E2 stops the sequence. A real DeepSeek treatment is
optional only because it requires credentials; when unavailable E1 records
`NOT RUN` and never substitutes a local fake. Real Devnet is not used.

## Evidence contract

Every run writes under `.local-run/resume-benchmark/`:

- `manifest.json` with dataset hash, Git SHA, Runtime source SHA, seed, variant,
  environment, and timestamps;
- frozen JSONL dataset and raw result JSONL;
- aggregate `metrics.json` with numerators/denominators and 95% Wilson CIs;
- `report.md` with methodology and `Resume Translation` STAR mapping.

The ignored `.local-run` directory is intentionally not committed. API keys,
raw prompts, and raw provider response bodies are not written by the suite.

## Commands

From `D:\MyLab\projects\Stablepay`:

```powershell
.\scripts\benchmark-e2-guard.ps1
.\scripts\benchmark-e1-recovery.ps1
.\scripts\benchmark-e1-recovery.ps1 -RunDeepSeek
.\scripts\benchmark-e3-load.ps1
.\scripts\benchmark-e4-chaos.ps1
python .\scripts\summarize-benchmark-evidence.py
```

E1 freezes 120 scenarios (12 families × 10 deterministic variants), runs the
Rule control once per scenario, and—when real provider configuration is
present—runs DeepSeek three times per scenario. E2 freezes 300 cases with 150
valid and 150 invalid proposals and calls the production guard. E3 uses Docker
`grafana/k6` on the Runtime HTTP boundary at concurrency 1/10/25/50/100/200
with warmup and a 60-second measurement. Its manifest records the Docker
version, k6 tag/digest, network mode, and container-visible URL. E4 kills only a Runtime process started by the harness
after a persisted target state is observed, then restarts the same command.

E3 and E4 intentionally withhold claims when k6, a safe local Runtime target,
an explicit chaos command, or economic side-effect audit evidence is missing.

## E4 concrete setup

E4 uses `scripts/run-e4-local.ps1`, which builds the benchmark-only
`commerce-runtime/cmd/e4-local-runtime` executable. It retains the real
application service, Runtime runner, API, and MySQL repositories, while using
local deterministic merchant, payment, authorization, and verification
adapters. The mock payment idempotency record is durable in MySQL so a Runtime
kill after mock acceptance but before Runtime commit can be reconciled after
restart. No Payment Service client or blockchain client is constructed.
The E4-only MySQL store decorator adds a controlled 150 ms dwell to the short
`DISCOVERING`, `NEGOTIATING`, `VALIDATING_DELIVERY`, and `RECOVERING` windows;
the merchant/payment/verification mocks provide the dwell for adapter-backed
states. An empty first delivery is injected only for trials targeting
`RECOVERING`; all other windows get a valid first delivery so unrelated
recovery branches do not contaminate the crash-window measurement. No
production Runtime package is changed.

The runner creates a dedicated Docker `mysql:8.0` container with its own named
data volume and fresh `stablepay_e4_*` schema. It binds MySQL only to
`127.0.0.1:13308`; the `.env` MySQL DSN and existing Runtime/database are not
used. Docker server version, MySQL image tag/digest, container, volume, and
schema are recorded in the setup/manifest. The container is retained for audit
inspection. The runner executes one `PAYING`-window smoke trial and audits it,
then runs the episode-state crash-window matrix (20 trials/window by default).
The workflow-only `child_episode_created` state is excluded because this
fixture submits an Episode, not a workflow run. The body budget is 1,000 USDC
minor units. E4 records `FROZEN_RUNTIME_CHANGED=NO`; only E4 tooling and its
fixture body are changed.

Run from the repository root:

```powershell
.\scripts\run-e4-local.ps1
```

The script reads only the API token from `.env`, uses isolated HTTP and MySQL
ports, and writes the schema name, raw `trials.jsonl`, and per-episode audit
artifacts under `.local-run/resume-benchmark/e4-local-*`. It stops before the
full matrix if the smoke trial or its SQL audit fails. After the matrix it runs
the same audit against every `episode_id` from `trials.jsonl`: duplicate
PaymentIntent and `PAYMENT_SETTLED` queries, ledger-derived consumed/available/
sunk-cost projections, and terminal/orphan/stuck state checks. The schema is
retained for inspection; no production schema is modified or dropped.
