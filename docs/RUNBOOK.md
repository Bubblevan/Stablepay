# Local Runbook

This is the minimum source-derived startup order. It does not include secret values.

## Infrastructure

From the repository root:

```powershell
docker compose -f .\stablepayai-idl\docker-compose.infra.yml up -d
```

The checked-in compose file starts MySQL on host port `3307`, Redis on `6379`, RocketMQ NameServer on `9876`, and the RocketMQ broker ports. Verify containers and MySQL before starting services.

## Payment plane

The existing `scripts/start-local.ps1` validates the configured hot-wallet file and starts services in this order:

```text
DID Service
Blockchain Adapter
Query Service
Verification Service
Payment Service
API Gateway
```

It is a real Devnet-oriented script and therefore requires an explicit valid wallet path. It does not start the reference Merchant or Commerce Runtime.

## Runtime and Merchant

Start a Merchant with the existing Merchant run instructions, then configure the Runtime process from the ignored local environment:

```text
COMMERCE_RUNTIME_MYSQL_DSN
COMMERCE_RUNTIME_API_TOKEN
COMMERCE_RUNTIME_SETTLEMENT_NETWORK
COMMERCE_RUNTIME_USDC_MINT or COMMERCE_RUNTIME_USDT_MINT
STABLEPAY_E2E_AGENT_KEYPAIR_PATH
LLM_BASE_URL / LLM_API_KEY / LLM_MODEL (for LLM recovery)
```

Start the Runtime from `commerce-runtime`:

```powershell
go run ./cmd/server
```

Check:

```powershell
Invoke-WebRequest http://127.0.0.1:8090/healthz
Invoke-WebRequest http://127.0.0.1:8090/readyz
```

`healthz` is liveness. `readyz` validates Runtime composition, active Episode/Workflow supervisors, and MySQL connectivity. It does not imply that Merchant, Payment, Solana, or DeepSeek has been probed.

## External client

```powershell
cd commerce-runtime
go run ./cmd/stablepay-runtime acquire --file request.json
go run ./cmd/stablepay-runtime status --episode-id <episode-id>
go run ./cmd/stablepay-runtime approve --episode-id <episode-id> --approval-id <approval-id> --decision APPROVE --actor-ref parent:1
```

For MCP, use `POST /mcp` with `initialize`, `tools/list`, then `tools/call`. The tools are listed in the Runtime README and are authenticated by the same bearer/API-key boundary.

## Regression and optional integration

The default final gate is safe and deterministic:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-final.ps1
```

Optional, explicit checks:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-final.ps1 -IncludeMySQL
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-final.ps1 -IncludeS11LiveLocal
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-unified-business-e2e.ps1 -Mode U1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-unified-business-e2e.ps1 -Mode U2
```

U1 submits real Devnet work and U2 calls the configured real LLM provider; neither is run by the default gate. Record `RUN` with local evidence or `NOT RUN`.

## Stop

Use `scripts/stop-local.ps1` for the local service processes and the same compose file with `down` for infrastructure when cleanup is intended. Do not delete shared Docker volumes as part of a routine regression.
