# Task 02 API Gateway Baseline Pressure Test

## Goal

Measure ACK ingress API latency, throughput, and error rate for baseline gateway interfaces without overstating the result as payment-order throughput.

This task does not benchmark merchant-backend x402 negotiation behavior. The currently measured routes are API Gateway canonical or gateway-shortcut routes:

- `/api/v1/pay/require` -> `payment-service`
- `/verify` -> `verification-service`

Therefore, Task 02 is independent of whether merchant-backend has already adopted the official x402 npm package response shape.

## Candidate Routes

- `/healthz`
- `/readyz`
- `/api/v1/pay/require`
- `/verify`
- `/api/v1/did/verify` when valid DID-auth test data is ready
- `/api/v1/verify` when valid API key usage is confirmed
- `/api/v1/transactions` or `/api/v1/revenue` when valid DID-auth test data is ready

## Current Execution Scope

Immediately runnable without extra credentials:

- `/healthz`
- `/readyz`
- `/api/v1/pay/require`
- `/verify`

Blocked until credentials are confirmed:

- `/api/v1/did/verify`
- `/api/v1/verify`
- `/api/v1/transactions`
- `/api/v1/revenue`

## Measurement Standard

For each route, record:

- expected status code
- actual RPS
- p95 latency
- p99 latency
- error rate
- runtime environment

## Explicit Non-Claim

Do not summarize this task as:

- supports X orders per second
- supports X payments per second
- supports X completed settlements per second

Use:

- completed API Gateway baseline testing in ACK, with core interfaces reaching `X` RPS at p95 `Y ms` and error rate `Z%`

## Script Location

- [api_baseline.js](D:\MyLab\StablePay\value-test\backend\scripts\k6\api_baseline.js)
- [README.md](D:\MyLab\StablePay\value-test\backend\scripts\k6\README.md)

## Status

- Script implementation: complete
- ACK ingress smoke route validation: complete
- Lightweight baseline run: pending
- Auth-protected route baseline: pending credentials
