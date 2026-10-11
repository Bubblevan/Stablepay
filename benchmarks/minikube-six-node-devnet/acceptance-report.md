# StablePay six-node Minikube + Devnet acceptance

Status: **PASS — one end-to-end, low-value Devnet payment and delivery**
Profile: `stablepay-six`
Kubernetes: `v1.37.0`
Physical hosts: **1** (Docker Desktop/WSL2); six Kubernetes nodes do not represent six physical machines.

## Topology

All six nodes were Ready: one control plane (`stablepay-six`) and five workers
(`stablepay-six-m02` through `stablepay-six-m06`). The six StablePay services
(API Gateway, DID, Payment, Blockchain Adapter, Verification, Query) plus the
Merchant backend, Agent Runtime, MySQL, Redis, RocketMQ NameServer and Broker
were deployed as 12 ready Deployments in `stablepay-k8s-e2e`. Application pods
were scheduled across workers m02–m06. MySQL and Redis used persistent PVCs.

## Real Devnet flow

- Request: `minikube-six-devnet-acceptance-20260925-03`
- Episode: `ce_9fc2b5eddfcbc645249fbd677baf6dd4`
- Final state / execution: `FULFILLED` / `COMPLETED`
- Payment: one `CONFIRMED` PaymentIntent, `amount_minor=1` (0.01 USDC)
- Solana Devnet transaction: `3r8EDrdrtNnGDorPKidUXhu6ugtWK5UMbE2mdUv89LJahCJ2TiCxAhGwkTTUpz7QTvnJcgXyQUET4LgwRY3D6XB8`
- Payment Service: exactly one matching transaction, status `3` (completed)
- Verification Service: one purchase record with matching tx ID, amount and hash
- Merchant: initial payment challenge HTTP 402, final delivery HTTP 200
- Runtime validation: valid, `NON_EMPTY`; payload hash matched delivery evidence

The same request ID was replayed after the Runtime restart. It resumed the
already-paid episode; no second Devnet payment was created.

## MySQL audit

The read-only queries are in [`audit-acceptance.sql`](audit-acceptance.sql).
Observed results:

- PaymentIntent count: **1**; duplicate PaymentIntent query: **0 rows**
- `PAYMENT_SETTLED` count: **1**; duplicate settlement query: **0 rows**
- Budget projection, expected = actual: `consumed=1`, `available=0`, `sunk_cost=1`
- Episode exists, is terminal and execution is `COMPLETED`; not orphaned or stuck
- Payment, verification, merchant-delivery and validation records agree on the
  same transaction and delivered payload

## Fixes exercised

Runtime retry handling now backs off while entitlement evidence is pending,
instead of hammering the API Gateway. A committed capability selection remains
usable after the discovery candidate-set TTL expires; immutable payload and
catalog snapshot binding are still validated. Regression tests passed for the
Runtime and application packages before the image was rolled out.

No Payment Service production source was changed. Minikube uses its local
deployment configuration to run schema migration; the API Gateway rate limit
was not relaxed. The earlier failed attempts are retained as separate request
fixtures; only the successful `...-03` flow is counted as a paid acceptance.

## Pressure suite status

At the original acceptance date (2026-09-25), the six-tier
`1/10/25/50/100/200 VU`, warmup + 60-second Docker k6 matrix had not been rerun.
The original full-stack Devnet workload is closed-loop and can create paid
episodes repeatedly; it must not be used for an uncapped pressure run. The
deterministic-local E3 diagnostic below is a separate namespace and payment
boundary, so its requests do not spend Devnet USDC.

### Later six-node local E3 diagnostic (2026-10-11)

The first two tiers ran against the real Runtime HTTP boundary in the isolated
`stablepay-e3-local` namespace, whose Kubernetes label records
`deterministic-local-only`. Each tier used a 15-second warmup and 60-second
measurement window:

| VUs | HTTP req/s | Completed Episodes/s | HTTP P95 | Episode P95 | HTTP/business errors |
|---:|---:|---:|---:|---:|---:|
| 1 | 17.567 | 4.300 | 10.679 ms | 329 ms | 0% / 0% |
| 10 | 119.417 | 14.717 | 36.080 ms | 803 ms | 0% / 0% |

This is **partial diagnostic evidence, not a completed matrix**: tiers 25, 50,
100, and 200 did not run. The isolated MySQL audit passed for 1,350 Episodes:
all were `FULFILLED`/`COMPLETED`, each had exactly one PaymentIntent and one
settlement, with zero duplicates, incomplete Episodes, or budget-projection
drift. The audit is for the deterministic local adapter only; it is not a
Devnet payment result. Artifacts are under
`.local-run/e3-six-node-final-20261011-20261011-084524/`.

The old runner recorded the 10-VU k6 process exit code 105 as a threshold
failure even though its `threshold_failures` list was empty and the exported
summary showed no crossed thresholds or failed checks. Current runner code
keeps process/evidence failures separate from actual threshold failures; this
historical run has not been rewritten. Grafana documents threshold expressions
as pass criteria and the summary status separately ([thresholds](https://grafana.com/docs/k6/latest/using-k6/thresholds/),
[summary schema](https://grafana.com/docs/k6/latest/results-output/end-of-test/custom-summary/)).

### Current resource blocker and prepared remediation

A read-only snapshot on 2026-10-11 confirmed all six nodes Ready, but
`stablepay-six-m03` was at **1.893/2 GiB**. It hosts the RocketMQ Broker,
NameServer, and Verification Service, all without Kubernetes memory
requests/limits. Container cgroup usage was about 0.92 GiB, 0.74 GiB, and
25 MiB respectively. The Kubernetes Metrics API is unavailable. The isolated
E3 MySQL cgroup was at 1,020,579,840/1,073,741,824 raw bytes, of which
413,274,112 bytes were inactive file cache; effective headroom was about
0.43 GiB, above the runner's 0.1-GiB MySQL guard. The node's 0.107-GiB
headroom is below the 0.5-GiB node guard, so the remaining tiers must stay
`NOT RUN` until a fresh safe preflight passes.

[`resource-headroom-patches/`](resource-headroom-patches/README.md) contains a prepared,
**unapplied** six-node placement/resource patch: it moves the NameServer to
`m06`, Verification to `m05`, and adds measured-resource-aware requests and
limits. Applying it restarts Deployments and requires a quiet maintenance
window; the live cluster was not changed during this diagnosis. After an
authorized rollout, recheck all six node headrooms and host free RAM before
continuing the matrix. Do not reduce the six-node topology or substitute the
separate single-host result.
