# StablePay Commerce Runtime

The Commerce Runtime is the upper control plane for controlled paid-capability execution. External agents submit only an `AcquireCapabilityRequest`; the Runtime drives the persisted Episode state machine and returns status, parent-approval state, validation evidence, and the final artifact.

## Architecture

```text
External Agent -> HTTP / MCP / CLI -> Commerce Runtime
                              -> Contract / Episode / Catalog
                              -> Merchant / x402 / Payment / Entitlement
                              -> Recovery / Evidence / DecisionProvider / Memory
                              -> Workflow
                              -> Event / Ledger / Trace
```

`cmd/server` is the production composition root. It wires MySQL, catalog, evidence, memory, the DeepSeek-compatible provider, Merchant adapter, DID policy, Kitex Payment adapter, entitlement verification, validator registry, Episode runner, and Workflow runner. These are existing components; the composition root does not contain a second payment, merchant, LLM, memory, or workflow implementation.

The authoritative path is:

```text
State -> proposal -> RuntimeGuard -> deterministic execution -> event/ledger/trace
```

LLM is a recovery DecisionProvider, not an authority over money or state. Memory is advisory, transaction-grounded, and provenance checked.

## Episode state machine

The persisted Episode state is authoritative:

```text
ACCEPTED -> DISCOVERING -> INVOKING -> NEGOTIATING
                                      -> PAYING -> CLAIMING
                                      -> INVOKING_DELIVERY -> VALIDATING_DELIVERY
                                      -> FULFILLED

VALIDATING_DELIVERY / other recoverable failures -> RECOVERING
NEGOTIATING or RECOVERING -> AWAITING_PARENT -> NEGOTIATING
Any terminal path -> FAILED / BLOCKED / ABORTED / EXPIRED
```

`RunEpisode` and `ResumeEpisode` share the same persisted-state implementation. The supervisor scans durable runnable Episodes, skips terminal/awaiting-parent Episodes, respects bounded retry scheduling, and resumes after restart. `EpisodeExecutionStatus` is operational evidence only; it cannot change business state, payment facts, budget, entitlement, or delivery authority.

## Recovery and parent approval

Recovery proposals are parsed, context-checked, memory/evidence-checked, and passed through `RuntimeGuard` before the application transition is committed. A durable `ParentApprovalRequest` and `ParentDecisionFact` are used for recovery-driven escalation and quote-threshold approval. The current S5 schema has the historical `ALLOW_SWITCH` scope name; quote-threshold approval is recorded as `ReasonParentConfirmation` and does not silently authorize a merchant switch. This is documented naming debt, not a new authority.

## LLM boundary

The OpenAI-compatible transport supports DeepSeek through `LLM_BASE_URL`, `LLM_API_KEY`, and `LLM_MODEL`. The model response is stored as bounded evidence/trace data; raw prompts and raw response bodies are not exported. A live DeepSeek claim requires a non-rule `ModelDecisionTrace` in the same episode’s observability export. A happy path can legitimately complete without an LLM call.

## Memory

Memory stores transaction-grounded episodic experience and `MemoryUseTrace` records. Retrieval is advisory and must be scoped to the current catalog snapshot, merchant/capability, and validity window. It cannot authorize payment, budget, entitlement, candidate eligibility, or validator execution. Use `COMMERCE_RUNTIME_MEMORY_MODE=on|off` to choose the runtime variant.

## Workflow and artifact data plane

Workflow is a static, sequential DAG. A `WorkflowDefinition` creates a durable `WorkflowRun` whose steps create child Episodes. A fulfilled child exposes validated artifact metadata as `workflow-artifact://run/step`; the resolver checks run/step ownership, fulfillment, hash, content type, size, and provenance before the next Merchant receives a bounded body. Status and observability omit artifact bodies by default; `/artifact` or `include_artifact_body=true` is explicit.

## Runtime service

Run the production composition root:

```powershell
go run ./cmd/server
```

Required configuration is supplied through the process environment, normally from an ignored local `.env`:

```text
COMMERCE_RUNTIME_MYSQL_DSN
COMMERCE_RUNTIME_API_TOKEN
COMMERCE_RUNTIME_SETTLEMENT_NETWORK
COMMERCE_RUNTIME_USDC_MINT or COMMERCE_RUNTIME_USDT_MINT
STABLEPAY_E2E_AGENT_KEYPAIR_PATH
LLM_BASE_URL / LLM_API_KEY / LLM_MODEL       # required for recovery_provider=llm
```

Optional runtime controls include `COMMERCE_RUNTIME_HTTP_ADDR`, `COMMERCE_RUNTIME_MEMORY_MODE`, `COMMERCE_RUNTIME_RECOVERY_PROVIDER`, `COMMERCE_RUNTIME_SUPERVISOR_INTERVAL`, `COMMERCE_RUNTIME_RUNNER_RETRY_MAX`, and the service/gateway addresses. The binary does not print or export secret values.

`/healthz` is an unauthenticated liveness probe. `/readyz` is an unauthenticated readiness probe that checks the production composition, runner/workflow supervisors, and MySQL connectivity; it does not claim that every downstream service has been probed.

## HTTP API

Authenticated external routes:

```text
POST /v1/episodes
GET  /v1/episodes/:id
GET  /v1/episodes/:id/events
GET  /v1/episodes/:id/observability
POST /v1/episodes/:id/parent-decisions
POST /v1/workflows
GET  /v1/workflows/:id/:version
POST /v1/workflow-runs
GET  /v1/workflow-runs/:id
GET  /v1/workflow-runs/:id/events
GET  /v1/workflow-runs/:id/artifact
POST /v1/workflow-runs/:id/parent-decisions
POST /mcp
```

Episode/workflow creation requires an `Idempotency-Key` matching `request_id` (or the request ID is used when the header is omitted). Parent decision idempotency is keyed by `approval_id`. HTTP and MCP share the same authorization and application entry boundary.

## MCP

`POST /mcp` supports JSON-RPC `initialize`, `notifications/initialized`, `tools/list`, and `tools/call`. The registered tools are:

```text
stablepay.acquire
stablepay.status
stablepay.approve
stablepay.run_workflow
stablepay.workflow_status
stablepay.workflow_approve
```

MCP is ingress only. A tool call cannot bypass `RuntimeGuard`, the Episode repository, payment idempotency, parent approval, or the Workflow runner.

## CLI

The CLI is an external HTTP client and imports no internal Runtime packages:

```powershell
go run ./cmd/stablepay-runtime acquire --file request.json
go run ./cmd/stablepay-runtime status --episode-id <episode-id>
go run ./cmd/stablepay-runtime approve --episode-id <episode-id> --approval-id <approval-id> --decision APPROVE --actor-ref parent:1
go run ./cmd/stablepay-runtime workflow register --file definition.json
go run ./cmd/stablepay-runtime workflow run --file run.json
go run ./cmd/stablepay-runtime workflow status --workflow-run-id <run-id>
```

`cmd/external-agent-e2e` is the external-client flow used for status polling, optional parent approval, and terminal artifact collection. It is separate from internal application tests.

## Evaluation and observability

`GET /v1/episodes/:id/observability` returns redacted episode, event, execution, ledger, payment-intent, model-decision, memory-use, artifact, and validation facts. Artifact bodies are omitted unless explicitly requested.

The external harness supports:

```powershell
go run ./cmd/stablepay-agent-eval plans
go run ./cmd/stablepay-agent-eval run --dataset testdata/s11/scenarios.jsonl --out-dir .local-run/s11
go run ./cmd/stablepay-agent-eval report --input .local-run/s11/episode_results.jsonl --out-dir .local-run/s11
go run ./cmd/stablepay-agent-eval export --episode-id <episode-id> --out .local-run/s11/trace.json
go run ./cmd/stablepay-agent-eval check --server http://127.0.0.1:8090
```

Replay, offline, live-local, and real-business evidence are separate. The committed example under `testdata/s11/example` is not a live benchmark. Fault cases are only reported as applied when an external controller records a trigger; otherwise they remain configured/requested but not applied.

## Testing and evidence

```powershell
cd ..
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-final.ps1
```

The final gate does not automatically call paid LLM or submit Devnet payments. Use the explicit scripts for MySQL, live-local, real LLM, or real Devnet paths and record their actual result. See:

- [../docs/ARCHITECTURE.md](../docs/ARCHITECTURE.md)
- [../docs/EVIDENCE_MATRIX.md](../docs/EVIDENCE_MATRIX.md)
- [../docs/RUNBOOK.md](../docs/RUNBOOK.md)
- [../docs/TEST_MATRIX.md](../docs/TEST_MATRIX.md)
- [../docs/LIMITATIONS.md](../docs/LIMITATIONS.md)
