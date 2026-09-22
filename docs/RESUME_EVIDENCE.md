# Resume Evidence

This is a source-backed claim inventory, not a finished resume. Each claim names the implementation and its evidence boundary.

| Claim | Exact source | Environment | Commit / caveat |
|---|---|---|---|
| Built an Agent Commerce Runtime that accepts a capability request and drives persisted payment/delivery execution | `commerce-runtime/internal/runtime`, `internal/application`, `cmd/server` | local code + deterministic tests | `08e09b9` and current hardening; not a production-traffic claim |
| Exposed an authenticated external HTTP, MCP, and CLI surface with idempotency and parent approval | `internal/api`, `cmd/stablepay-runtime`, API tests | local HTTP | `08e09b9`; MCP is ingress only |
| Added a guard boundary where LLM recovery output is a proposal, not payment authority | `internal/decision`, `internal/llm`, `internal/trace` | unit/local integration | `8a7bf7c`, current tree; live DeepSeek requires a ModelDecisionTrace |
| Added transaction-grounded advisory memory with provenance and use traces | `internal/memory`, `internal/repository`, S7 tests | unit/MySQL when configured | `66ec12c`; no causal accuracy claim |
| Added a durable sequential workflow artifact data plane | `internal/workflow`, `internal/workflowruntime`, `internal/workflow/artifact` | local HTTP/in-memory | `ef6dc0d`, `fcd1954`, `ac2dc51`; no parallel/dynamic workflow |
| Added external eval/observability export with mode-qualified metrics | `internal/eval`, `internal/observability`, `cmd/stablepay-agent-eval` | replay/live-local depending artifact | `cea47a6`; committed example is replay, not live |
| Validated real payment and real recovery paths | `scripts/test-unified-business-e2e.ps1`, prior acceptance history | REAL_DEVNET / REAL_LLM | cite only the corresponding recorded run artifact; F0 does not infer a fresh run |

Do not use a headline percentage, QPS, availability number, or “zero failure” phrase unless a committed artifact names the trial set, mode, environment, numerator/denominator, and commit.
