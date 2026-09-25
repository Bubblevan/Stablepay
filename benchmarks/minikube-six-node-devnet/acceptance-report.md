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

The six-tier `1/10/25/50/100/200 VU`, warmup + 60-second Docker k6 matrix has
**not yet been rerun**. The existing E3 workload is closed-loop and can create
new paid episodes repeatedly. At the benchmark's minimum price of 0.01 USDC,
the current Agent wallet balance is only about 1.96 test USDC; an uncapped run
could exceed it. The pressure suite will be run only with an explicit total
Devnet-spend cap and a workload policy that reports paid unique episodes
separately from repeated HTTP/idempotency traffic.
