# StablePay Freeze Manifest

## Canonical identity

- Audit base: `ac2dc5144ea2b78f4dd832017f4d4f70a2e1622c`
- Final freeze commit: the commit containing this manifest and the F0 documentation gate; report its full SHA in the handoff.
- Product identity: Agent Commerce Runtime for controlled paid-capability execution.

## Frozen modules

Contract, Episode, Catalog, Merchant/x402, Payment/Ledger/Reconciliation, Entitlement, Validator, Recovery/Parent Approval, LLM evidence/decision boundary, Memory projection, Runtime supervisor, Workflow v1/artifact data plane, HTTP/MCP/CLI surface, and Eval/Observability.

## Canonical invariants

```text
LLM proposes.
Runtime validates.
Deterministic Plane executes.
Event Log records.
```

Memory is advisory. Payment/entitlement/catalog/validation/approval authorities are not delegated to Memory or LLM. Workflow orchestrates Episodes and is static/sequential/single-sink in v1.

## Required regression commands

```text
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-final.ps1
go test ./... -count=1
go test ./... -count=3
go vet ./...
git diff --check
```

The default script does not call paid LLM or submit real Devnet payments. MySQL, live-local, real LLM, and real Devnet checks are explicit opt-in.

## Optional integrations

F0 records: MySQL workflow/repository integration `RUN`; isolated live-local suite `RUN`; U1 real-business Devnet `NOT RUN`; U2 real-LLM recovery `NOT RUN`. The live-local source artifacts are `.local-run/s11-live-local/metrics.json` and `.local-run/s11-live-local/report.md` and are labelled deterministic `live_local`.

## Known limitations

See [LIMITATIONS.md](LIMITATIONS.md). Future ideas such as parallel workflow, dynamic planning, additional payment networks, RL/GRPO, and self-evolution are not current requirements.
