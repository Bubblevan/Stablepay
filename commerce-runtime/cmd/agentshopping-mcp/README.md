# Agent Shopping MCP Bridge

`agentshopping-mcp` is a stdio JSON-RPC MCP server for the isolated shopping
simulator in `cmd/agentshopping-sim`. It forwards a fixed allowlist of tool
calls to the simulator's authenticated loopback HTTP API; it does not connect
to production StablePay services, a real payment provider, or Solana. It serves
the stateless MCP revision `2026-07-28` and the legacy `2025-11-25` initialize
handshake, selecting one protocol era per stdio process.

## Configuration

The simulator must already be running and ready on loopback. Configure the MCP
client process with:

- `SHOP_SIM_BASE_URL` (optional; defaults to `http://127.0.0.1:18090`; only an
  HTTP loopback origin is accepted)
- `SHOP_SIM_API_KEY` (required; the same local simulation key used by the HTTP
  API; it is sent only in `X-Simulation-API-Key` to that loopback origin)

The bridge emits only MCP JSON-RPC responses on stdout. Diagnostics go to
stderr, and the key is never included in messages. The process does not load a
`.env` file.

## Tools

The bridge exposes search, task status, immutable quote, quote status, checkout,
order status, and simulated fulfillment. It deliberately has no buyer-confirm
tool. To approve or reject a quote, a human must run
`scripts/approve-simulated-shopping-quote.ps1 -QuoteId <quote-id>` in a separate
PowerShell session, inspect the quote and type the exact approval phrase. That
script uses a distinct local buyer-approval key which is not loaded by this MCP
process. The human can then provide the resulting confirmation ID to the agent
to continue with mock checkout. Human confirmation IDs are random and are not
derivable from quote IDs or idempotency keys. The simulator removes the
buyer-approval key from its environment before invoking the intent-parser child
process. All writes retain the HTTP API's server-side validation, idempotency,
inventory and budget checks; checkout creates only a deterministic mock payment
with `real_settlement=false`.

This is still a local benchmark-only approval boundary, not a production
identity or authorization system. Never point it at production credentials or
endpoints.

## Local verification

From `commerce-runtime`:

```powershell
$env:GOPROXY = 'off'
$env:GOFLAGS = '-mod=readonly'
go test ./cmd/agentshopping-mcp -count=1
```

Tests use a local fake HTTP server and verify both protocol eras, modern
`server/discover` and per-request metadata, fixed route allowlist, API-key
forwarding, error handling, and no real network dependency. This is local
protocol/unit coverage, not an end-to-end test with Hermes or an LLM.
