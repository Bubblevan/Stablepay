# StablePay

StablePay is a Go microservice system for DID-based agent commerce and Solana stablecoin payments.

## Architecture at a glance

```text
HTTP :8080
    -> api-gateway
       -> Kitex: did-service :8081
       -> Kitex: payment-service :8888
       -> Kitex: query-service :8084
       -> Kitex: verification-service :8085

payment-service -> Kitex: blockchain-adapter :8083
payment-service -> RocketMQ: payment_events -> verification-service
blockchain-adapter -> Solana JSON-RPC
```

Only `api-gateway` exposes public HTTP. The other five services expose internal Kitex RPC, with verification-service also consuming payment events.

## Repository map

```text
stablepayai-idl/       # canonical Thrift, OpenAPI and event contracts
stablepay-common/      # non-communication shared foundations
api-gateway/           # Hertz HTTP gateway
did-service/           # DID authority and signature verification
payment-service/       # payment application and event producer
query-service/         # read-only query RPC
verification-service/  # purchase verification and proof
blockchain-adapter/    # Solana transaction boundary
```

Each service has its own technical README. The complete system note is [docs/SERVICE_TECHNICAL_MANUAL.md](docs/SERVICE_TECHNICAL_MANUAL.md).

## Contract and test gate

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\test-six-services.ps1
```

This runs every service's unit tests and verifies canonical IDL, generated Kitex methods and removed legacy contract copies. A real infrastructure E2E test still requires MySQL, Redis, RocketMQ and a Solana endpoint.
