# StablePay Backend Value Test PRD

## 1. Document Info

- Project: StablePay backend value test
- Scope: `api-gateway`, `did-service`, `payment-service`, `verification-service`, `query-service`, `blockchain-adapter`
- Related infra: Redis, RocketMQ, MySQL, ACK Kubernetes
- Target directory: `D:\MyLab\StablePay\value-test\backend\docs`

## 2. Background

StablePay is not a generic CRUD backend. It is a payment-oriented microservice system built around API Gateway, Kitex RPC, RocketMQ, Redis, MySQL, and Kubernetes. For this kind of system, pure throughput is not enough to represent engineering value.

The highest-value proof points are:

- low latency under stable traffic
- correct rejection of invalid signatures and replayed requests
- strict idempotency under repeated payment requests
- eventual consistency from payment event publication to purchase record persistence
- graceful degradation and recovery when Redis or Pods fail

This value test exists to turn the current architecture into measurable results that can be used in technical review, milestone reporting, and resume writing.

## 3. Problem Statement

The current project already contains the core technical hooks needed for value testing:

- payment nonce uniqueness and idempotency persistence
- RocketMQ topic `payment_events`
- verification-side `purchase_records`
- readiness and liveness probes in Kubernetes manifests
- Redis-based nonce and idempotency cache configuration

However, the project still needs a dedicated experiment definition so that backend value can be expressed as measurable outcomes rather than component names.

## 4. Product Goal

Build a backend value test framework and document set that proves StablePay can satisfy payment-system expectations in five dimensions:

1. Performance
2. Correctness
3. Consistency
4. Resilience
5. Recoverability

## 5. Non-Goals

- This phase does not aim to optimize for maximum benchmark score
- This phase does not require HPA production rollout if HPA manifests are not yet ready
- This phase does not redefine the business protocol of StablePay
- This phase does not replace existing functional test suites

## 6. Users

- Backend engineer preparing project delivery or interview material
- Reviewer evaluating whether the system is payment-grade rather than demo-grade
- Maintainer validating release safety before infrastructure rollout

## 7. Success Criteria

### 7.1 Core Success Criteria

- Repeated payment requests with the same idempotency key produce exactly one payment record
- Replayed requests with the same nonce are rejected with 100% accuracy
- Invalid signatures and expired signatures are rejected with 100% accuracy
- Payment events are eventually persisted to `purchase_records` with bounded latency
- Core services recover automatically after Pod restart or deletion

### 7.2 Quantitative Targets

| Layer | Metric | Target |
| --- | --- | --- |
| API Gateway `/healthz` | p95 latency | <= 30 ms |
| API Gateway `/api/v1/pay/require` | p95 latency | <= 80 ms |
| Query endpoint `/api/v1/revenue` or `/api/v1/transactions` | p95 latency | <= 120 ms |
| Gateway stable load | throughput | 300 to 500 RPS with error rate < 0.5% |
| DID signature verification | throughput | 200 to 400 req/s |
| DID signature verification | invalid signature rejection | 100% |
| Nonce replay protection | replay rejection | 100% |
| Idempotency | duplicate 100 requests | only 1 payment transaction |
| MQ eventual consistency | `payment_events -> purchase_records` p95 | <= 1 s |
| MQ eventual consistency | event loss | 0 |
| Redis failure | health endpoint availability | 100% available |
| Redis failure | correctness regression | no duplicate pay, no replay bypass |
| Kubernetes self-heal | single Pod recovery | <= 60 s |
| Rolling update | service rollout complete | <= 120 s |
| Rollback | undo complete | <= 90 s |

### 7.3 Resume-Oriented Success Criteria

At least three of the following should become provable result statements:

- duplicate payment replayed 100 times but only 1 payment record persisted
- invalid signature and expired nonce requests blocked at 100%
- payment event persisted to purchase record within p95 1 second
- gateway p95 held within sub-100 ms under 300+ RPS
- Pod self-heal completed within 60 seconds after failure injection

## 8. Experiment Scope

### 8.1 In Scope

- API Gateway latency and error rate baseline
- DID signature verification correctness
- payment idempotency and nonce anti-replay
- RocketMQ eventual consistency path
- Redis degradation test
- Kubernetes rollout, rollback, and self-heal test

### 8.2 Out of Scope

- multi-region disaster recovery
- cross-chain payment tests
- frontend UX testing
- production cost optimization

## 9. Experiment Matrix

| Experiment ID | Theme | Primary Value |
| --- | --- | --- |
| EXP-01 | API latency and throughput baseline | performance |
| EXP-02 | DID signature verification | correctness |
| EXP-03 | idempotency and anti-replay | correctness |
| EXP-04 | MQ eventual consistency | consistency |
| EXP-05 | Redis failure injection | resilience |
| EXP-06 | Kubernetes self-heal and release recovery | recoverability |

## 10. Acceptance Definition

The PRD is accepted when:

1. Each experiment has a fixed metric definition and target threshold
2. Each experiment has a clear pass or fail condition
3. Each result can be mapped to a backend engineering outcome
4. Each outcome can be converted into a STAR-format resume statement

## 11. Risks

### 11.1 Technical Risks

- Query-side data may still include placeholder or demo behavior
- Redis failure may expose hidden dependencies in nonce or limiter logic
- MQ consumer lag may fluctuate under local development environments
- Kubernetes cluster behavior may differ between local and ACK environments

### 11.2 Documentation Risks

- If only RPS is reported, the project will look generic
- If correctness metrics are missing, payment-system value is understated
- If recovery experiments are not measured, cloud-native claims remain weak

## 12. Output Deliverables

- this PRD
- a TRD with exact measurement and verification steps
- raw measurement logs and SQL evidence
- a resume-ready summary after experiments are executed

## 13. Resume Framing Guidance

The project should not be summarized as:

- used Redis and RocketMQ
- built microservices with Kubernetes

It should be summarized as:

- proved 100 repeated payment requests persisted only one final record
- proved invalid signature and replay traffic were blocked at 100%
- quantified event-to-record persistence latency and Pod recovery time

## 14. Suggested Before/After Narrative

### Before

- component-centric description
- vague statements like "implemented payment microservices"
- no quantified correctness or recovery proof

### After

- result-centric description
- quantified correctness, consistency, and resilience
- stronger interview signal for payment-grade backend engineering

## 15. Milestones

1. Define experiment data, endpoints, and SQL checkpoints
2. Run baseline load tests
3. Run correctness and consistency tests
4. Run resilience and recovery tests
5. Summarize measured values into STAR resume statements

