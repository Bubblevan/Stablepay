# Test Matrix

The test matrix maps invariants to evidence. A package count is not itself a reliability percentage.

| Invariant | Test / entry point | Expected evidence |
|---|---|---|
| Request normalization, deadlines, minor-unit money, immutable snapshot | `commerce-runtime/internal/contract/*_test.go` | invalid contracts rejected; canonical hash stable |
| Episode transition authority and terminal semantics | `commerce-runtime/internal/episode/episode_test.go` | only allowed transitions; terminal reason and budget invariants |
| Payment/ledger idempotency and reconciliation | `internal/payment/*_test.go`, `internal/reconciliation/*_test.go`, Payment Service idempotency tests | same economic identity on replay; duplicate settlement count remains measured |
| RuntimeGuard proposal boundary | `internal/decision/*_test.go`, `internal/runtime/*_test.go`, trace tests | rejected proposal has no protected side effect |
| Durable recovery and parent approval | `internal/recovery/*_test.go`, `internal/application/*acceptance_test.go` | durable request/decision replay and guarded recovery |
| Memory advisory/provenance boundary | `internal/memory/*_test.go`, `internal/application/s7_*_test.go`, MySQL S7 tests | scoped retrieval/use trace; no authority elevation |
| Auth, idempotency, health/readiness, observability redaction | `internal/api/server_test.go` and API integration tests | unauthorized requests fail; probes remain public; secrets omitted |
| Runtime restart/resume | `internal/runtime` tests and workflow restart tests | persisted state is reloaded; worker cancellation is bounded |
| Workflow static DAG/CAS/single sink | `internal/workflow/*_test.go`, `internal/repository/workflow_test.go` | invalid graph rejected; CAS/event replay deterministic |
| Workflow artifact data plane | `internal/workflow/artifact/*_test.go`, `internal/workflowruntime/workflow_*_e2e_test.go` | hash/type/provenance/size checks; no default body leak |
| External HTTP/MCP/CLI boundary | `cmd/external-agent-e2e`, `cmd/stablepay-runtime`, `internal/api` | client crosses public surface only |
| Eval integrity and mode separation | `internal/observability/*_test.go`, `internal/eval/*_test.go`, `testdata/s11/example` | replay/live/offline separated; numerator/denominator present |
| Six payment-plane contracts | `scripts/test-six-services.ps1` | all six service tests plus canonical IDL verification |
| Final deterministic gate | `scripts/test-final.ps1` | count=1/count=3, vet, diff check; no paid LLM/Devnet by default |

Optional integration rows are environment-dependent and must be recorded as `RUN` or `NOT RUN`: MySQL repository round-trip, isolated live-local suite, real DeepSeek recovery, and real Devnet business E2E.
