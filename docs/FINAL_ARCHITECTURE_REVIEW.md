# Final Architecture Review

Audit base: `ac2dc5144ea2b78f4dd832017f4d4f70a2e1622c`.

## Scope

This review covers the final repository architecture, public Runtime surface, Episode/Workflow authority boundaries, persistence and crash recovery, security/redaction, LLM/Memory boundaries, eval integrity, and documentation consistency. It does not add a feature milestone.

## Frozen invariants

- State is authoritative; proposals are not state.
- LLM proposes; RuntimeGuard validates; deterministic adapters execute; events/ledger/traces record.
- `CommerceEpisode` owns business execution. `EpisodeExecutionStatus` is operational only.
- Payment, entitlement, catalog eligibility, validation, and parent approval each have explicit authorities.
- Memory is advisory and provenance-scoped.
- Workflow v1 is static, sequential, single-sink, and artifact-reference based.

## Audit result

| Area | Finding | Severity | Result |
|---|---|---:|---|
| Composition root | `cmd/server` wires existing components and checks readiness | P0/P1 | no correctness blocker observed |
| Runtime lifecycle | persisted `RunEpisode`/`ResumeEpisode`, supervisor scan, bounded retry and shutdown | P0/P1 | covered by code and tests |
| External ingress | HTTP/MCP/CLI share auth/application boundary; idempotency is explicit | P0/P1 | covered by API/E2E tests |
| Payment authority | Runtime does not replace Payment Service or Ledger authority | P0/P1 | code-audited; real payment evidence remains environment-scoped |
| LLM/Memory | provider and memory are proposal/advisory boundaries | P0/P1 | covered by trace/guard/memory tests |
| Workflow | static DAG/CAS/artifact provenance and bounded data plane | P0/P1 | covered by local E2E and restart tests |
| Observability | redacted export and replay/live qualification | P1 | covered by observability/eval tests |
| Documentation | root and Runtime README now describe final architecture; milestone history moved to docs | P2 | completed in F0 |

## Evidence gaps

- Real DeepSeek recovery: `NOT RUN` in F0. Any future claim must record provider/model/trace IDs.
- Real Devnet: `NOT RUN` in F0. Any future claim requires a recorded network and transaction artifact.
- MySQL workflow/repository integration: `RUN` on 2026-09-22 against an isolated local Docker database; all `internal/infrastructure/mysql` tests passed.
- Isolated live-local suite: `RUN` on 2026-09-22 from rebuilt binaries; 56 submitted/56 valid, 0 collection errors, 0 unsafe side effects, 0 duplicate settlements. The generated report is `.local-run/s11-live-local/report.md` and the JSON source is `.local-run/s11-live-local/metrics.json`.
- The live-local 48/56 task-success numerator includes the expected safety-negative guard-rejection group; `pass^8` is 6/7 task groups, not a claim that every adversarial case should fulfill.

## Limitations

See [LIMITATIONS.md](LIMITATIONS.md). In particular, no production traffic, QPS, availability, zero-failure, or guaranteed-safety claim is made.

## Final verdict

The repository is ready for project freeze when the deterministic regression gate passes, documentation links resolve, and every optional integration is recorded as `RUN` with evidence or `NOT RUN`. No P0 or P1 correctness blocker is introduced by this audit. The freeze does not authorize S9, S12, RL/GRPO, self-evolution, dynamic planning, parallel workflow execution, or new core payment/memory/recovery features.
