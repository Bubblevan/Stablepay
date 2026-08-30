# Task 01 Environment and Observability Baseline

## Goal

Establish whether the current local or target environment is actually ready for the remaining value-test tasks.

This task does not prove business performance. It proves testability.

## Date

- 2026-07-12

## Scope

- local tool availability
- local endpoint ownership
- Docker and Kubernetes accessibility
- route and auth inventory
- code-path constraints that affect later tasks

## Findings

### 0. WSL Path Is the Correct Execution Environment

Confirmed via WSL `Ubuntu-24.04`:

- `kubectl` has a valid active context
- `k6` is installed
- `wrk` is installed
- ACK cluster resources are reachable

Implication:

- all follow-up tasks should prefer WSL over the current Windows shell environment
- the Windows-side local shell should not be treated as the primary benchmark environment

### 1. Tool Availability

Detected in WSL:

- `kubectl`
- `k6`
- `wrk`

Detected in Windows shell:

- `kubectl.exe`
- `docker.exe`
- `mysql.exe`

Not detected in Windows shell:

- `k6`
- `wrk`

Implication:

- baseline pressure testing can proceed in WSL
- Windows shell should only be used for workspace edits, not as the primary execution environment

### 2. Docker Accessibility

Attempting `docker ps` did not succeed.

Observed result:

- Docker client could not connect to daemon
- local Docker config access also returned an access error

Implication:

- container-based service state cannot currently be verified through local Docker commands
- MQ and service-container validation may need either permission fixes or a Kubernetes-side path

### 3. Kubernetes Accessibility

Observed in Windows shell:

- `kubectl` exists
- `kubectl config current-context` returned no active context

Implication:

- ACK-side testing cannot begin until a valid context is configured

Observed in WSL:

- active context is present
- `kubectl get pods -n zheda-agent` succeeds
- `kubectl get ingress stablepay-ingress -n zheda-agent -o wide` succeeds

Confirmed namespace inventory includes:

- `stablepay-api-gateway`
- `stablepay-payment-service`
- `stablepay-verification-service`
- `stablepay-query-service`
- `stablepay-redis`
- `stablepay-rocketmq-broker`
- `stablepay-rocketmq-nameserver`

Ingress confirmed:

- host: `ai.wenfu.cn`
- address: `alb-3m55g2idsjhrdy3p6y.cn-hangzhou.alb.aliyuncsslb.com`

Implication:

- ACK-side testing is ready from WSL

### 4. Local Port 8080 Is Not Confirmed as StablePay Gateway

Observed result:

- `curl http://localhost:8080/healthz` returned HTTP `404`
- response server header was `Embedthis-http`
- `netstat` showed port `8080` is listening under PID `11744`

Implication:

- local `localhost:8080` currently appears to be occupied by another service
- no StablePay baseline test should use `localhost:8080` until endpoint ownership is confirmed

Follow-up confirmation from WSL ingress probing:

- `http://ai.wenfu.cn/*` returns `308 Permanent Redirect`
- the real benchmark entry should use `https://ai.wenfu.cn`

Observed HTTPS responses:

- `GET https://ai.wenfu.cn/healthz` -> `200 {"status":"ok"}`
- `GET https://ai.wenfu.cn/readyz` -> `200 {"status":"ready"}`
- `GET https://ai.wenfu.cn/api/v1/pay/require?skill_did=did:solana:testskill` -> `402` with structured JSON challenge
- `GET https://ai.wenfu.cn/verify?skill=did:solana:testskill&agent=did:solana:testagent` -> `200 {"purchased":false}`

Implication:

- Task 2 can benchmark real ACK ingress routes via `https://ai.wenfu.cn`
- `/api/v1/pay/require` and `/verify` are both confirmed alive and suitable as baseline candidates

### 5. Gateway Benchmark Candidates Confirmed in Config

Candidate routes from [config.yaml](D:\MyLab\StablePay\api-gateway\configs\config.yaml:1):

- `/healthz`
- `/api/v1/did/verify`
- `/api/v1/pay/require`
- `/api/v1/verify`
- `/api/v1/transactions`
- `/api/v1/revenue`
- `/verify`

Auth model confirmed:

- `did.verify` uses `did` auth
- `payment.require` uses `none`
- `verification.verify` uses `api_key`
- query routes use `did`

Implication:

- Task 2 should benchmark gateway interfaces by auth class and route type, not as payment-order throughput

### 6. Redis Fallback Confirmed on Gateway Side

Confirmed in [bootstrap.go](D:\MyLab\StablePay\api-gateway\internal\app\bootstrap.go:1):

- if Redis is enabled and healthy, gateway uses Redis-based limiter and nonce store
- if Redis is unavailable, gateway falls back to memory limiter and memory nonce store

Implication:

- gateway resilience claim is plausible
- payment correctness under Redis failure still requires separate verification

### 7. Payment Nonce Check Is Wired into Main Payment Path

Confirmed in [payment_service.go](D:\MyLab\StablePay\payment-service\internal\application\service\payment_service.go:1):

- nonce replay check runs before request validation and before payment execution

Implication:

- Task 4 and Task 5 are worth running because the check is actually in the payment path

### 8. MQ Producer State Is Mixed but Current Runtime Path Looks Real

Confirmed:

- legacy [handler.go](D:\MyLab\StablePay\payment-service\handler.go:1) still contains `noopEventPublisher`
- runtime [main.go](D:\MyLab\StablePay\payment-service\cmd\payment-service\main.go:1) creates a real RocketMQ producer through [producer.go](D:\MyLab\StablePay\payment-service\internal\adapter\mq\producer.go:1)
- verification side has a real consumer in [consumer.go](D:\MyLab\StablePay\verification-service\consumer.go:1)

Implication:

- MQ eventual-consistency testing is only valid if the deployed environment uses the runtime path from `cmd/payment-service/main.go`

## Task 01 Verdict

Task 01 is complete for environment discovery and can be used as the execution baseline for Task 2.

Completed:

- route inventory
- auth inventory
- code-path inventory
- local blocker discovery
- WSL execution path validation
- ACK pod and ingress validation
- real HTTPS ingress endpoint validation
- live route smoke checks for health, ready, 402 challenge, and short verify

Blocked:

- no usable local Docker daemon access
- local port `8080` not confirmed as StablePay Gateway

Non-blocking notes:

- Windows shell remains unsuitable for direct ACK execution
- local Docker checks are still unavailable from the current shell path
- benchmarking should target `https://ai.wenfu.cn`, not local port `8080`

## Required Next Actions

1. Run Task 2 from WSL against `https://ai.wenfu.cn`
2. Decide whether `/api/v1/did/verify` and query routes need test credentials or API keys for baseline pressure
3. Confirm whether Docker access is needed at all, or whether subsequent checks can remain fully WSL plus ACK based
