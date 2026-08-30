# StablePay Backend Value Test TRD

## 1. Document Goal

This document translates the value-test PRD into executable technical work. It defines:

- what to test
- how to split implementation into verifiable tasks
- where to observe results
- how to validate correctness
- how to report gaps without overstating backend capability

## 2. System Under Test

### 2.1 Services

- `api-gateway`
- `did-service`
- `payment-service`
- `verification-service`
- `query-service`
- `blockchain-adapter`

### 2.2 Infra Dependencies

- MySQL
- Redis
- RocketMQ
- ACK Kubernetes

### 2.3 Code and Config Evidence

- payment nonce uniqueness and idempotency tables:
  [init_db.sql](D:\MyLab\StablePay\payment-service\scripts\init_db.sql:1)
- payment security and MQ config:
  [configmaps.yaml](D:\MyLab\StablePay\infra-deployment\k8s\base\configmaps.yaml:1)
- probes and service deployments:
  [services.yaml](D:\MyLab\StablePay\infra-deployment\k8s\base\services.yaml:1)
- gateway route auth model and candidate benchmark endpoints:
  [config.yaml](D:\MyLab\StablePay\api-gateway\configs\config.yaml:1)
- gateway Redis-to-memory fallback wiring:
  [bootstrap.go](D:\MyLab\StablePay\api-gateway\internal\app\bootstrap.go:1)
- current payment runtime entry with real RocketMQ producer:
  [main.go](D:\MyLab\StablePay\payment-service\cmd\payment-service\main.go:1)
- payment nonce check in main payment flow:
  [payment_service.go](D:\MyLab\StablePay\payment-service\internal\application\service\payment_service.go:1)
- legacy payment entry still containing `noopEventPublisher`:
  [handler.go](D:\MyLab\StablePay\payment-service\handler.go:1)
- verification-side RocketMQ consumer:
  [consumer.go](D:\MyLab\StablePay\verification-service\consumer.go:1)

## 3. Observability Design

### 3.1 Metrics

- request latency: p50, p95, p99
- throughput: RPS
- error rate
- replay rejection rate
- idempotency success rate
- event persistence latency
- Pod recovery time
- rollout and rollback duration

### 3.2 Logs

Each experiment should retain logs keyed by:

- `request_id`
- `tx_id`
- `agent_did`
- `skill_did`
- `idempotency_key`
- `nonce`
- `error_code`

### 3.3 Database Verification Points

- `stablepay_payment_db.payment_transactions`
- `stablepay_payment_db.payment_idempotency_keys`
- `stablepay_verification_db.purchase_records`

### 3.4 Trace Guidance

If end-to-end tracing is added later, propagate a shared trace or request ID across:

- API Gateway
- Payment Service
- Verification Service
- Query Service

For the current phase, request-level correlation via logs and DB rows is sufficient.

## 4. Implementation Constraints

### 4.1 API Baseline Constraint

API baseline measurement must be expressed as gateway and verification-query interface latency and error rate. It must not be written as:

- supports X orders per second
- supports X payment settlements per second
- completed X payment chain writes per second

The safer wording is:

- completed API Gateway baseline testing in ACK
- measured p95 and p99 latency and error rate for health, auth, 402 challenge, signature verification, and query interfaces

### 4.2 MQ Chain Constraint

Historical material mentioned a placeholder `noopEventPublisher`. The current repository contains both:

- a legacy path in [handler.go](D:\MyLab\StablePay\payment-service\handler.go:1) that still uses `noopEventPublisher`
- a current runtime path in [main.go](D:\MyLab\StablePay\payment-service\cmd\payment-service\main.go:1) that wires a real RocketMQ producer from [producer.go](D:\MyLab\StablePay\payment-service\internal\adapter\mq\producer.go:1)

Therefore, any MQ eventual-consistency result is valid only if the target environment is confirmed to run the real producer path.

### 4.3 Redis Failure Constraint

Redis fault injection is valuable, but it may expose a gap rather than a success story. Gateway fallback is present in code, but payment correctness under Redis failure must be proven from the actual runtime path, not from design intent.

If fallback only keeps the service alive but weakens replay protection or idempotency guarantees, that result must be reported as a limitation rather than a resilience achievement.

## 5. Test Data Design

### 5.1 DID Test Data

- one valid DID and signature set
- one invalid signature set
- one expired timestamp set
- one repeated nonce set

### 5.2 Payment Test Data

- one valid `agent_did`
- one valid `skill_did`
- repeated `idempotency_key`
- repeated `nonce`
- a batch of N unique payment requests for MQ delay statistics

### 5.3 Environment Labels

All outputs should record:

- environment name
- timestamp
- service version or image tag
- namespace
- node or cluster identifier if available

## 6. Implementation Breakdown

The implementation is split into eight verifiable tasks. Tasks are execution units. Experiments are reporting units. Multiple tasks may contribute to one experiment result.

### 6.1 Task 1: Environment and Observability Baseline

#### Input

- target environment address or namespace
- service config and deployment manifests
- available CLI tooling
- DB, Docker, and Kubernetes access capability

#### Output

- environment checklist
- route and auth checklist
- observability checklist
- blockers and gaps for later tasks

#### Verification

- verify reachable endpoints
- verify CLI availability
- verify DB, K8S, and Docker accessibility
- verify candidate benchmark routes from gateway config

#### Risk

- local ports may not point to StablePay services
- Docker or K8S context may be unavailable
- missing pressure tools will block later tasks

### 6.2 Task 2: API Gateway Baseline Pressure Test

#### Input

- pressure tool such as `k6` or `wrk`
- candidate routes:
  - `/healthz`
  - `/api/v1/did/verify`
  - `/api/v1/pay/require`
  - `/api/v1/verify` or `/verify` when a valid API-key path exists
  - `/api/v1/transactions` or `/api/v1/revenue`

#### Output

- p95 and p99 latency
- RPS
- error rate

#### Verification

- retain raw benchmark output
- summarize by endpoint and RPS tier
- explicitly avoid converting results into order-processing claims
- separately note that this task does not prove full payment-chain completion

#### Risk

- benchmark can only prove API behavior, not full settlement completion
- query routes may still include demo or placeholder behavior
- local environment may not reflect ACK behavior

### 6.3 Task 3: DID Authentication and Signature Verification Correctness

#### Input

- valid DID-signed requests
- invalid signature requests
- expired timestamp requests
- repeated nonce requests

#### Output

- valid request success rate
- invalid signature rejection rate
- expired request rejection rate
- replay rejection rate

#### Verification

- batch request statistics
- gateway and DID-service logs
- HTTP code and business code classification

#### Risk

- malformed test data may be mistaken for server failure
- timestamp skew windows may differ by environment

### 6.4 Task 4: Idempotency Correctness

#### Input

- one stable request payload
- same `idempotency_key`
- repeated payment invocation count such as 100

#### Output

- number of accepted responses
- final `payment_transactions` row count
- final `payment_idempotency_keys` row count

#### Verification

- SQL cardinality checks
- logs by `idempotency_key` and `tx_id`

#### Risk

- pre-existing dirty data may affect counting
- request mismatch invalidates the idempotency scenario

### 6.5 Task 5: Concurrent Correctness Pressure Test

#### Input

- concurrent workers or VUs
- repeated same `idempotency_key`
- repeated same `nonce`
- sustained concurrency window

#### Output

- duplicate suppression effectiveness under concurrency
- replay rejection effectiveness under concurrency
- latency and error changes under concurrent contention

#### Verification

- concurrent pressure run output
- DB final-state verification
- race-condition evidence from logs

#### Risk

- client bottleneck may distort server conclusions
- concurrent behavior may diverge from single-thread replay results

### 6.6 Task 6: Payment Chain Readiness and MQ E2E Validation

#### Input

- actual deployed payment-service entrypoint
- RocketMQ topic and broker connectivity
- verification-service consumer readiness

#### Output

- chain readiness verdict:
  - producer is real
  - broker is reachable
  - consumer is active
- permission to proceed with eventual-consistency timing tests

#### Verification

- inspect runtime path and image entry
- inspect logs for successful event publish and consume
- confirm target environment does not use the legacy noop publisher path

#### Risk

- testing against the wrong runtime entry invalidates all MQ conclusions
- broker reachability may fail even when code is correct

### 6.7 Task 7: MQ Eventual Consistency Timing

#### Input

- N successful payment requests
- publish and persistence timestamps
- DB query window for `purchase_records`

#### Output

- p50, p95, p99 event-to-record latency
- missing record count
- duplicate record count

#### Verification

- SQL against `purchase_records`
- payment and verification logs
- optional broker-side evidence

#### Risk

- only valid if Task 6 passes
- clock skew may distort latency calculation

### 6.8 Task 8: Redis Failure Injection and Kubernetes Recovery

#### Input

- Redis failure action
- Pod delete, rollout restart, rollout undo actions
- health and correctness probes

#### Output

- service availability during dependency failure
- correctness degradation or preservation verdict
- Pod recovery, rollout, and rollback timing

#### Verification

- fault-before and fault-after comparison
- DB verification for duplicate or replay regressions
- rollout status and health polling

#### Risk

- service may stay alive while correctness weakens
- single-replica behavior may amplify downtime
- cluster and local results may differ

## 7. Experiment Specifications

## 7.1 EXP-01 API Baseline

### Objective

Measure gateway-facing latency, throughput, and error rate on health, auth-related, 402 challenge, signature verification, and query interfaces.

### Endpoints

- `/healthz`
- `/api/v1/did/verify`
- `/api/v1/pay/require`
- `/api/v1/verify` or `/verify` when a valid API-key path exists
- `/api/v1/revenue` or `/api/v1/transactions`

### Method

- use `k6` or `wrk`
- run warm-up for 1 minute
- run steady load for 3 to 5 minutes
- test at 50, 100, 300, 500 RPS

### Metrics

- p50, p95, p99 latency
- average RPS
- non-2xx and non-expected response rate

### Pass Criteria

- `/healthz` p95 <= 30 ms
- `/api/v1/pay/require` p95 <= 80 ms
- query endpoint p95 <= 120 ms
- error rate < 0.5%

### Output

- latency chart by endpoint
- final threshold table
- approved wording:
  - completed API Gateway baseline testing in ACK, with core interfaces reaching `X` RPS at p95 `Y ms` and error rate `Z%`

### Explicit Non-Claim

Do not describe EXP-01 as order throughput, payment completion throughput, or settlement throughput. This experiment only proves API-layer behavior.

## 7.2 EXP-02 DID Signature Verification

### Objective

Prove the backend rejects invalid, expired, and replayed signatures rather than merely accepting valid requests.

### Method

- send valid signed requests in batch
- send invalid signature requests in batch
- send expired timestamp requests in batch
- send repeated nonce requests in batch

### Metrics

- valid verification throughput
- invalid signature rejection rate
- expired timestamp rejection rate
- repeated nonce rejection rate

### Pass Criteria

- invalid signature rejection = 100%
- expired timestamp rejection = 100%
- repeated nonce rejection = 100%

### Notes

This experiment has high resume value because it proves security correctness rather than nominal throughput.

## 7.3 EXP-03 Idempotency and Anti-Replay

### Objective

Verify duplicate payment requests cannot create duplicate business records.

### Method A: Same Idempotency Key

- send 100 identical payment requests
- keep `agent_did`, `skill_did`, payload, and `idempotency_key` the same

### Method B: Same Nonce Replay

- send 100 requests with identical `nonce`
- vary timing but keep replay semantics

### Verification SQL

Check payment table cardinality:

```sql
SELECT COUNT(*) AS payment_rows
FROM stablepay_payment_db.payment_transactions
WHERE agent_did = ? AND skill_did = ?;
```

Check idempotency key cardinality:

```sql
SELECT COUNT(*) AS idem_rows
FROM stablepay_payment_db.payment_idempotency_keys
WHERE idempotency_key = ?;
```

### Metrics

- total requests
- accepted requests
- rejected requests
- final persisted payment rows
- final persisted idempotency rows

### Pass Criteria

- 100 duplicate payment requests produce exactly 1 final payment row
- repeated nonce traffic produces 0 extra payment rows

### Resume Value

Best phrasing:

- repeated 100 payment replays but persisted only 1 final payment record

## 7.4 EXP-04 MQ Readiness Gate

### Objective

Confirm that the target environment is running the real MQ producer path rather than a legacy noop path before any eventual-consistency timing claim is made.

### Method

- inspect deployed entrypoint and logs
- verify real producer initialization
- verify `payment_events` publish log exists
- verify verification consumer is active

### Pass Criteria

- deployed payment-service runtime uses the real producer path
- publish and consume logs are both observable

### Failure Handling

If this gate fails, EXP-05 must be reported as blocked rather than executed.

## 7.5 EXP-05 MQ Eventual Consistency

### Objective

Measure end-to-end latency from payment success event publication to purchase record persistence.

### Chain

`payment-service -> RocketMQ payment_events -> verification-service -> purchase_records`

### Method

- generate N successful payments
- record payment success timestamp
- record MQ publish timestamp if available
- query `purchase_records` insert time
- calculate event-to-record delay

### Verification SQL

```sql
SELECT agent_did, skill_did, tx_id, purchase_time, created_at
FROM stablepay_verification_db.purchase_records
WHERE created_at >= ?;
```

### Metrics

- p50 latency
- p95 latency
- p99 latency
- success rate
- duplicate consumption count
- missing record count

### Pass Criteria

- p95 event-to-record latency <= 1 second
- missing record count = 0
- duplicate purchase record count = 0

### Notes

This converts "used RocketMQ" into a measurable backend consistency result.

## 7.6 EXP-06 Redis Failure Injection

### Objective

Verify whether core service paths stay available and correct when Redis becomes unavailable.

### Method

- stop Redis or break Redis connectivity
- repeat selected requests:
  - health check
  - signature verification
  - nonce check
  - limiter-sensitive requests
- compare behavior before and during Redis failure

### Metrics

- health endpoint availability
- request success rate
- correctness regression count
- latency increase

### Pass Criteria

- health endpoint remains available
- no duplicate payments created
- no replayed nonce bypass accepted

### Important Caveat

If Redis failure disables replay protection or idempotency guarantees, this experiment must be reported as a failure or gap, not a positive result.

## 7.7 EXP-07 Kubernetes Recovery

### Objective

Measure service recovery under deployment and failure events.

### Scenarios

- `kubectl rollout restart deployment/<service>`
- `kubectl rollout undo deployment/<service>`
- `kubectl delete pod <pod-name>`

### Metrics

- time from action start to probe-ready state
- maximum error spike window
- time to full service recovery

### Pass Criteria

- Pod self-heal <= 60 s
- rollout complete <= 120 s
- rollback complete <= 90 s

### Current Scope Note

The repository clearly includes readiness and liveness probes. HPA-based autoscaling can be treated as phase two unless HPA manifests and metrics source are added.

## 8. Suggested Tooling

### Load

- `k6`
- `wrk`

### Verification

- MySQL query scripts
- service logs
- Kubernetes rollout status

### Reporting

- CSV or Markdown tables
- latency percentile summary
- SQL proof snapshots

## 9. Result Recording Template

Use the following table for each experiment:

| Field | Value |
| --- | --- |
| Experiment ID | |
| Date | |
| Environment | |
| Service version | |
| Input scale | |
| Success threshold | |
| Measured result | |
| Pass or fail | |
| Evidence location | |
| Resume-ready sentence | |

## 10. Failure Interpretation Rules

- High throughput with correctness failure is a failed experiment
- Low latency with duplicate payments is a failed experiment
- Successful health checks with broken replay protection is a failed resilience claim
- Recovery without data correctness must not be counted as success
- MQ latency claims are invalid if the environment still runs a noop publisher path

## 11. STAR Resume Conversion Guide

## 11.1 Situation

StablePay is a payment microservice system whose value depends on correctness and consistency, not just request throughput.

## 11.2 Task

Design and execute a backend value test framework to quantify latency, errors, replay protection, idempotency, eventual consistency, and Kubernetes recovery.

## 11.3 Action

Built eight implementation tasks across gateway load, DID verification, duplicate replay, concurrent correctness, MQ readiness, MQ event persistence, Redis failure injection, and Kubernetes self-heal; validated results through logs and MySQL evidence.

## 11.4 Result

Replace placeholders with measured values:

- held `/api/v1/pay/require` p95 under `X ms` at `Y RPS`
- blocked invalid signatures and expired nonces at `100%`
- replayed identical payment requests `100` times but persisted only `1` final payment record
- kept `payment_events -> purchase_records` p95 within `X ms`
- restored service within `X s` after Pod deletion

## 12. Recommended Resume Bullets

### Version A

Designed and executed a payment-system backend value test framework across API Gateway, DID verification, RocketMQ, Redis, MySQL, and ACK Kubernetes, quantifying latency, anti-replay correctness, event consistency, and Pod recovery rather than only reporting raw throughput.

### Version B

Validated StablePay payment correctness through idempotency and replay testing, proving repeated payment requests persisted only one final record and invalid signature or expired nonce traffic was blocked at 100%.

### Version C

Measured asynchronous payment consistency from `payment_events` to `purchase_records` and quantified Kubernetes rollout, rollback, and self-heal recovery windows to turn a microservice demo into verifiable backend engineering outcomes.

## 13. Next-Step Implementation Suggestions

1. Add per-request correlation IDs across gateway, payment, and verification services
2. Export structured metrics for latency and event lag
3. Add HPA manifests and metrics source if autoscaling becomes a required claim
4. Add one-click scripts under `value-test/backend` for each experiment
