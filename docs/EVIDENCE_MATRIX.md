# Evidence Matrix

Audit base: `ac2dc5144ea2b78f4dd832017f4d4f70a2e1622c`.

Evidence types are intentionally distinct: `CODE_AUDITED`, `UNIT_TEST`, `LOCAL_INTEGRATION`, `LIVE_LOCAL`, `REAL_LLM`, `REAL_DEVNET`, and `REPLAY`.

| Capability | Evidence type | Commit / source | Test or entry point | Environment | Result | Limitation |
|---|---|---|---|---|---|---|
| Contract validation and immutable snapshots | CODE_AUDITED, UNIT_TEST | current tree; `internal/contract` | package tests | deterministic Go tests | enforced by code and tests | no claim about external deployment |
| Episode state machine and budget authority | CODE_AUDITED, UNIT_TEST | current tree; `internal/episode`, `internal/application` | episode/application tests | deterministic Go tests | state transitions and budget invariants covered | no compensation semantics |
| Recovery guard and parent approval | CODE_AUDITED, UNIT_TEST | `c80e238`, `83f69cf`, current tree | recovery, decision, application tests | deterministic Go tests | proposals cannot directly commit side effects | approval scope naming debt remains |
| Persistent memory provenance | CODE_AUDITED, UNIT_TEST, LOCAL_INTEGRATION | `66ec12c`, current tree | memory and S7 boundary tests | in-memory/MySQL when configured | advisory scope and provenance checks covered | memory is not causal accuracy proof |
| External Runtime API/MCP/CLI | UNIT_TEST, LOCAL_INTEGRATION | `08e09b9`, `internal/api`, `cmd/stablepay-runtime` | API tests and external-agent entry point | local HTTP | authenticated ingress and idempotency covered | real downstream requires configured services |
| Real-business acceptance path | REAL_DEVNET | `9086871`, `b62e062`, `1fccccf`; `scripts/test-unified-business-e2e.ps1 -Mode U1` | explicit opt-in script | local services + Solana Devnet | prior acceptance path recorded in project history | not rerun by F0 |
| Real recovery provider | REAL_LLM | `b423752`; `scripts/test-unified-business-e2e.ps1 -Mode U2` | explicit opt-in script | configured OpenAI-compatible provider | prior acceptance path recorded in project history | not rerun by F0 unless explicitly invoked |
| Workflow orchestration and artifact data plane | UNIT_TEST, LOCAL_INTEGRATION | `ef6dc0d`, `fcd1954`, `ac2dc51` | workflow local, HTTP Merchant, parent-threshold, restart tests | deterministic local HTTP/in-memory | static sequential workflow and provenance checks pass | no dynamic/parallel workflow |
| Eval integrity and redacted export | UNIT_TEST, REPLAY | `190c11b`, `8e88216`, `dd3e0cd`, `cea47a6` | observability/eval tests; committed example | replay/local | numerator/denominator and mode boundaries are explicit | replay is not live provider evidence |
| Isolated live-local external-client suite | LIVE_LOCAL | `cea47a6`; `scripts/test-s11-live-local.ps1` | `.local-run/s11-live-local/{metrics,report}.(json|md)` on 2026-09-22 | deterministic local adapters | 56 submitted, 56 valid, 0 collection errors; task success 48/56 and recovery success 32/40; unsafe side effects 0; duplicate settlements 0; `pass^8` 6/7 | the 7th group is the expected guard-rejection safety case; this is not real DeepSeek/Payment/Devnet latency |
| Six-service contract gate | UNIT_TEST, LOCAL_INTEGRATION | current tree | `scripts/test-six-services.ps1` | local Go toolchain | record actual command output in final handoff | infrastructure E2E is separate |

The F0 final handoff records exact command results. A missing live artifact is written as `NOT RUN`, never inferred from a code path or scenario label.
