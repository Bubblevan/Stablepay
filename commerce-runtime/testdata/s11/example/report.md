# StablePay S11.2 Agent Eval / Metrics Integrity

This committed report is a replay example, not a live DeepSeek/payment/Devnet claim; use the live-local recipe for current external-client evidence.

**EXAMPLE / REPLAY - NOT A LIVE BENCHMARK**

- Schema: `s11.v1`
- Generated: `2026-09-21T07:27:44Z`
- Sensitive fields omitted: `true` (private keys, API keys, DSNs, prompts and raw LLM response bodies are not exported)

## Evidence boundary

This report is computed from replayed, externally shaped Runtime observability snapshots. `offline`, `replay`, and `live` are counted separately, and headline rates must be read with their numerator/denominator. A live row without a `ModelDecisionTrace` is not described as having passed through DeepSeek; a real happy path may be `CLI -> Runtime -> Merchant -> Payment -> Devnet -> Verification -> FULFILLED` without an LLM recovery call. Small samples are descriptive only.

Modes: `replay:6`; live rows with model trace: `0`; live rows without model trace: `0`; requested fault cases: `4`; applied fault cases: `4`.

## Headline metrics

| metric | value |
|---|---:|
| `task_success_rate` | 66.67% (4/6) |
| `recovery_success_rate` | 50.00% (1/2) |
| `llm_proposal_acceptance_rate` | 50.00% (1/2) |
| `guard_rejection_rate` | 33.33% (1/3) |
| `unsafe_side_effect_count` | 0 |
| `memory_retrieval_hit_rate` | 100.00% (2/2) |
| `memory_citation_rate` | 50.00% (1/2) |
| `memory_guided_action_success` | 1/1 |
| `duplicate_settlement_count` | 0 |
| `payment_retry_count` | 0 |
| `delivery_retry_count` | 0 |
| `episode_latency_p50/p95_ms` | 1500.00 / 2000.00 |
| `llm_latency_p50/p95_ms` | 350.00 / 395.00 |
| `payment_confirmation_latency_p50/p95_ms` | 0.00 / 0.00 |
| `recovery_latency_p50/p95_ms` | 1500.00 / 1500.00 |

## Experiment matrix

### Memory on/off

| cell | episodes | task success | recovery success | p50 ms | p95 ms |
|---|---:|---:|---:|---:|---:|
| `off` | 3 | 33.33% | 0.00% | 1000.00 | 1900.00 |
| `on` | 3 | 100.00% | 100.00% | 2000.00 | 2000.00 |

### LLM vs RuleRecoveryProvider

| cell | episodes | task success | recovery success | p50 ms | p95 ms |
|---|---:|---:|---:|---:|---:|
| `llm` | 3 | 66.67% | 100.00% | 1000.00 | 1900.00 |
| `rule` | 3 | 66.67% | 0.00% | 2000.00 | 2000.00 |

### Failure injection rate

| cell | episodes | task success | recovery success | p50 ms | p95 ms |
|---|---:|---:|---:|---:|---:|
| `0%` | 2 | 100.00% | 0.00% | 225.00 | 247.50 |
| `10%` | 2 | 100.00% | 100.00% | 2000.00 | 2000.00 |
| `20%` | 2 | 0.00% | 0.00% | 1500.00 | 1950.00 |

### Happy vs recovery path

| cell | episodes | task success | recovery success | p50 ms | p95 ms |
|---|---:|---:|---:|---:|---:|
| `happy` | 2 | 100.00% | 0.00% | 225.00 | 247.50 |
| `recovery` | 4 | 50.00% | 50.00% | 2000.00 | 2000.00 |

## Failure-injection recovery steps

| failure | samples | applied | recovered | p50 steps | p95 steps | mean steps |
|---|---:|---:|---:|---:|---:|---:|
| `delivery_invalid` | 1 | 1 | 1 | 1.00 | 1.00 | 1.00 |
| `merchant_transient` | 1 | 1 | 0 | 0.00 | 0.00 | 0.00 |

## Episode evidence

| case | mode | ingress | memory | provider | fault | applied | state | episode_id | payment_tx | artifact | validation | duration ms |
|---|---|---|---|---|---|---:|---|---|---|---|---|---:|
| `replay-happy-memory-on-0` | `replay` | `http` | `on` | `llm` | `none` | false | `FULFILLED` | `ce-s11-replay-happy-on` | `` | `` | `` | 0.00 |
| `replay-happy-memory-off-0` | `replay` | `mcp` | `off` | `rule` | `none` | false | `FULFILLED` | `ce-s11-replay-happy-off` | `` | `` | `` | 0.00 |
| `replay-delivery-invalid-10` | `replay` | `http` | `on` | `llm` | `delivery_invalid` | true | `FULFILLED` | `ce-s11-replay-delivery` | `` | `` | `` | 0.00 |
| `replay-merchant-transient-20` | `replay` | `cli` | `off` | `rule` | `merchant_transient` | true | `FAILED` | `ce-s11-replay-merchant` | `` | `` | `` | 0.00 |
| `replay-payment-transient-10` | `replay` | `http` | `on` | `rule` | `payment_transient` | true | `FULFILLED` | `ce-s11-replay-payment` | `` | `` | `` | 0.00 |
| `replay-llm-malformed-20` | `replay` | `mcp` | `off` | `llm` | `llm_malformed` | true | `FAILED` | `ce-s11-replay-llm` | `` | `` | `` | 0.00 |

## Reproducibility

Use the committed dataset seed and scenario metadata with `stablepay-agent-eval run`. The harness submits only through the public Runtime HTTP/MCP boundary, polls the public observability endpoint, and writes `episode_results.jsonl`, `metrics.json`, and `report.md`. Faults require an external injector/controller; absent an injector, the report records `applied=false` rather than fabricating a fault result.
