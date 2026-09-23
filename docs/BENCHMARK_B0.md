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

E4 is an external-process experiment against a Runtime with durable MySQL
state. `s11-live-local` is useful for HTTP/recovery demonstrations, but it uses
an in-memory repository and therefore cannot prove restart recovery. Build or
run the production composition (`commerce-runtime/cmd/server`) with a
deterministic local merchant/payment/verification path and point the harness at
that executable.

The checked-in body template is
`testdata/benchmark/e4-submit-body.json`. It is an `AcquireCapabilityRequest`
for `POST /v1/episodes`; the harness rewrites `request_id`, session, and input
URI for every trial and sends a matching `Idempotency-Key`. The Runtime must
have a catalog capability matching `acquisition_goal.task_type` and a local
payment path for the requested `USDC` budget.

Example configuration:

```powershell
$env:RUNTIME_EXECUTABLE = 'D:\path\to\commerce-runtime.exe'
$env:RUNTIME_ARGUMENTS = ''
$env:RUNTIME_WORKDIR = 'D:\path\to\Stablepay\commerce-runtime'
$env:BASE_URL = 'http://127.0.0.1:8090'
$env:SUBMIT_BODY_PATH = 'D:\path\to\Stablepay\testdata\benchmark\e4-submit-body.json'
$env:API_TOKEN = $env:COMMERCE_RUNTIME_API_TOKEN
.\scripts\benchmark-e4-chaos.ps1
```

The production server reads its listen address from
`COMMERCE_RUNTIME_HTTP_ADDR`; it does not take an `-addr` command-line flag.
The executable can be built with:

```powershell
.\scripts\build-e4-runtime.ps1
$env:RUNTIME_EXECUTABLE = (Resolve-Path .\commerce-runtime\commerce-runtime.exe).Path
```

The process must expose `/readyz`, `/v1/episodes`, and
`/v1/episodes/{id}`. A terminal state after restart is only the operational
part of E4. Duplicate settlement, duplicate PaymentIntent, duplicate merchant
invocation, orphan/stuck episodes, budget drift, and artifact provenance still
require a post-trial audit from the durable stores; without that audit the
report remains `NO_RESUME_CLAIM_UNTIL_AUDIT`.
