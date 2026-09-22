# Interview Map

| Question | StablePay implementation | Relevant files | Invariant / failure case | Trade-off |
|---|---|---|---|---|
| How do you avoid duplicate payment? | Payment Intent economic identity plus durable ledger/reconciliation | `internal/payment`, `internal/reconciliation`, Payment Service | redelivery/retry reuses identity | payment status may remain unknown until reconcile |
| What happens in a crash window? | persisted Episode + execution projection + startup sweep | `internal/runtime`, repository execution status | restart resumes from authoritative state | operations status is not business authority |
| How is an LLM kept from paying? | proposal parse/context checks + RuntimeGuard + deterministic execution | `internal/llm`, `internal/decision`, `internal/application` | malformed/unsafe proposal cannot create side effect | rejected attempts are evidence, not business failures |
| What is Memory allowed to do? | scoped retrieval and MemoryUseTrace | `internal/memory`, `internal/repository` | historical fact cannot authorize current payment | advisory context can be stale |
| Why is initial selection deterministic? | Catalog CandidateSet selection | `internal/catalog`, `internal/runtime` | candidate authority remains Runtime-owned | semantics are less flexible than an unconstrained planner |
| How does parent approval survive restart? | durable request and decision facts | `internal/recovery`, `internal/application` | approval replay is idempotent | approval schema retains a legacy scope name |
| How does Workflow avoid leaking artifacts? | reference/hash/type/provenance resolver | `internal/workflow/artifact`, `internal/workflowruntime` | status omits body; data plane is bounded | v1 is sequential and single-sink |
| How do you compare reliability fairly? | external harness, fixed seed, mode-qualified artifacts | `internal/eval`, `internal/observability` | replay/local/real evidence never mixed | local latency is not network latency |
| How do Go workers shut down? | supervisor-owned context and wait group | `internal/runtime`, `internal/workflowruntime` | shutdown cancels adapter calls and waits | queue is a durable sweep/dedupe guard, not a broker |
