# Minikube Agent Payment State-Machine Validation

Status: **PASS — steady-state business invariants**

## Topology

- Minikube v1.39.0, Kubernetes v1.37.0, Docker Desktop Docker driver, Docker Engine 29.6.2.
- Three Kubernetes nodes ran as Docker containers on one physical Windows host.
- `stablepay-e4-runtime` ran on worker `stablepay-multi-m02`; isolated MySQL ran on worker `stablepay-multi-m03`.
- Runtime reached MySQL through the in-cluster ClusterIP Service DNS name. Host test traffic used a loopback-only `kubectl port-forward`.
- A PVC retained the dedicated `stablepay_k8s_e2e` database.

## Workload and result

The benchmark-only E4 Runtime composition exercised the persisted Episode lifecycle with a 1,000-minor-unit budget. Merchant, Payment, authorization, and Verification were deterministic local mocks; no production Payment Service client, chain client, or real chain transaction was used.

One smoke Episode and a final load of 10 Episodes at up to 3 concurrent requests completed. In the load run, **10/10** Episodes reached `FULFILLED` and execution `COMPLETED`; each traversed the same eight state-after values from `DISCOVERING` through `FULFILLED`.

| Metric | Result |
|---|---:|
| Completed Episodes | 10/10 |
| Maximum concurrency | 3 |
| Elapsed time | 10.351 s |
| Completed throughput | 0.966 Episodes/s |
| Episode latency P50 / P95 | 1.857 / 2.010 s |
| PaymentIntent rows per Episode | exactly 1 |
| `PAYMENT_SETTLED` rows per Episode | exactly 1 |
| Duplicate intents / settlements | 0 / 0 |
| Budget projection drift | 0 |
| Orphaned / stuck / non-terminal | 0 / 0 / 0 |

Budget totals across the 10 load Episodes matched MySQL ledger facts: `expected_consumed=actual_consumed=10000`, `expected_available=actual_available=0`, and `expected_sunk_cost=actual_sunk_cost=10000`.

## Scope limits

This was a single-host, three-node Minikube simulation, not a three-machine cluster or ACK run. It validates the Runtime's full deterministic Agent payment Episode state machine, Kubernetes scheduling/service discovery, MySQL persistence, and business invariants; it does **not** validate the six production microservices as an integrated request path, real-chain settlement, or crash recovery. The separate crash-recovery gate is therefore `NOT_TESTED`.

The repository's generic E4 MySQL audit tool also checks `task_completion_after_restart`; it correctly returns its crash-recovery gate as unmet for these steady-state trials because no restart was injected. The separate Minikube audit summary evaluates only the applicable payment, budget, and terminal-state invariants and labels crash recovery `NOT_TESTED`.

Detailed artifacts: `environment-manifest.json`, `load-summary.json`, `load-results.json`, `audit-summary.json`, and `per-episode-audit.jsonl`.
