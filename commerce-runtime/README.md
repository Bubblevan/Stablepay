# StablePay Commerce Runtime — S10

`cmd/server` is the production composition root. It loads the durable MySQL
store, catalog/evidence/memory projections, DeepSeek-compatible decision
provider, HTTP Merchant adapter, DID policy, Kitex Payment adapter, entitlement
verification, validator registry and the persisted-state episode runner.

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
