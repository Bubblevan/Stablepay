# StablePay Commerce Runtime - S11

`cmd/server` is the production composition root. It loads the durable MySQL
store, catalog/evidence/memory projections, DeepSeek-compatible decision
provider, HTTP Merchant adapter, DID policy, Kitex Payment adapter, entitlement
verification, validator registry and the persisted-state episode runner.
The runner also starts a persisted runnable sweep (default every 2 seconds),
with bounded exponential retry scheduling and an operations-only
`episode_execution_status` projection.

External agents use only the Runtime surface:

```text
POST /v1/episodes
GET  /v1/episodes/:id
GET  /v1/episodes/:id/events
POST /v1/episodes/:id/parent-decisions
POST /mcp                 # initialize, tools/list, tools/call
GET  /healthz
GET  /readyz
```

Episode status includes an `execution` object with `status`, `attempt_count`,
last start/finish times, sanitized error code/message, and `next_retry_at`.
This projection never changes the authoritative Episode state or payment facts.

HTTP/MCP requests require `Authorization: Bearer $COMMERCE_RUNTIME_API_TOKEN`
or `X-API-Key`, except health and readiness probes. `POST /v1/episodes` takes
an `AcquireCapabilityRequest`; `Idempotency-Key` must match `request_id`.
MCP tools are `stablepay.acquire`, `stablepay.status`, and `stablepay.approve`.

Required production configuration includes:

```text
COMMERCE_RUNTIME_MYSQL_DSN
COMMERCE_RUNTIME_API_TOKEN
LLM_BASE_URL                 # DeepSeek/OpenAI-compatible /v1 endpoint
LLM_API_KEY
LLM_MODEL
COMMERCE_RUNTIME_SETTLEMENT_NETWORK
COMMERCE_RUNTIME_USDC_MINT or COMMERCE_RUNTIME_USDT_MINT
STABLEPAY_E2E_AGENT_KEYPAIR_PATH
COMMERCE_RUNTIME_SUPERVISOR_INTERVAL # default 2s
COMMERCE_RUNTIME_RUNNER_RETRY_MAX    # default 30s
```

The public CLI is an HTTP client and does not import Runtime internals:

```text
go run ./cmd/stablepay-runtime acquire --file acquire.json
go run ./cmd/stablepay-runtime status --episode-id <id>
go run ./cmd/stablepay-runtime approve --episode-id <id> --approval-id <approval> --decision APPROVE --actor-ref parent:1
```

For a complete external-client poll/approval flow, use
`cmd/external-agent-e2e`. It submits the request, polls the public status API,
handles an optional parent approval, and exits only after `FULFILLED` with the
final artifact.

## S11 Agent Eval / Observability / Reliability Benchmark

S11 adds a read-only public export:

```text
GET /v1/episodes/:id/observability
```

It contains the episode, event log, execution status, ledger, payment intents,
model decision traces, memory-use traces, artifact and validation evidence.
Sensitive credentials, prompts, raw LLM responses, private keys, API keys and
DSNs are not exported.

The external benchmark CLI writes reproducible JSONL/JSON/Markdown outputs:

```text
go run ./cmd/stablepay-agent-eval plans
go run ./cmd/stablepay-agent-eval run --dataset testdata/s11/scenarios.jsonl --out-dir .local-run/s11
go run ./cmd/stablepay-agent-eval report --input .local-run/s11/episode_results.jsonl --out-dir .local-run/s11
go run ./cmd/stablepay-agent-eval export --episode-id <id> --out .local-run/s11/trace.json
go run ./cmd/stablepay-agent-eval check --server http://127.0.0.1:8090
```

`run` uses only the external Runtime HTTP/MCP surface. A live fault case is
marked `applied=false` unless an external controller is configured with
`--injector`; this prevents replay/offline evidence from being misreported as
live DeepSeek, payment or Devnet evidence. `cmd/s11-fault-proxy` is an opt-in
HTTP injector for merchant, LLM and verification dependencies. Payment fault
plans remain replay/controller cases unless a Kitex-aware injector is present.
