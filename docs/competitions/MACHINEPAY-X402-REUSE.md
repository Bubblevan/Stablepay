# Machine Pay and x402 reuse boundary

This note describes a local implementation branch for PAYATHON 2026 Machine Pay. It records the protocol boundary and the missing end-to-end pieces before wiring the public Merchant API.

## Official protocol

The current [Machine Pay product guide](https://aipay.alipay.com/docs/ai-receive/MACHINE_PAY.html) describes a server-side precreate call to `alipay.aipay.agent.payment.precreate`, followed by HTTP 402 with `amtPaymentLink`; after payment, the caller retries with `Payment-Proof`, and the merchant verifies it through `alipay.aipay.agent.payment.verify`. Precreate requires a registered `service_id`, `out_trade_no`, `goods_name`, `amount`, and `pay_before`; currency is CNY and the per-call amount is capped at ¥50. The signed verification result must be bound to the merchant order, amount, resource, and one-time fulfillment state.

There is an older official A2M sample that uses `Payment-Needed`, a seller-signed bill, and `alipay.aipay.agent.fulfillment.confirm` ([Alipay sample](https://github.com/alipay/ai/tree/main/code_example/aipay-402-example)). That example is not silently mixed into the current product guide flow. Before enabling an external client, verify the exact header/receipt contract exposed for the registered sandbox service.

## Reuse from StablePay

| Existing component | Intended Machine Pay reuse |
| --- | --- |
| Merchant resource, price, and delivery logic | Keep server-side resource selection and fixed integer CNY price. Continue using HTTP 402 as the retry boundary. |
| `commerce-runtime/internal/x402` | Reuse the bounded amount/resource concepts and CNY minor-unit rules. Its x402 header parser does not parse Machine Pay's `amtPaymentLink` or `Payment-Proof`; those remain a separate protocol. |
| Hertz API Gateway | Public Merchant-to-payment control requests go through the existing Gateway. |
| Payment Service | Own idempotent order state, MySQL persistence, nonce/replay protection, reconciliation, and transactional outbox. |
| Alipay Adapter | Isolate Alipay RSA2 signing, precreate, and payment-proof verification behind Kitex RPC. |

Machine Pay does not need a DID or blockchain-adapter payment path. The public 402 resource request remains HTTP; internal service calls remain Kitex behind Hertz. Do not translate `amtPaymentLink` into an x402 accept or treat `Payment-Proof` as a chain signature.

## Work in this branch

`alipay-adapter` exposes the official Machine Pay precreate and proof-verification methods over Kitex. It validates signed Alipay responses, bounds request fields and CNY amounts, parses the bounded Base64URL proof, and accepts the raw Base64 key format shown by the AI Pay console or PEM key files. It does not store merchant orders.

Payment Service now owns Machine Pay order creation, idempotency, proof-to-order/resource/amount binding, unique trade-number replay prevention, MySQL settlement state, and a transactional outbox. Settled Machine Pay events publish to the separate `machinepay_events` topic; token-ledger consumers keep using `payment_events` and are not sent CNY Machine Pay records.

Hertz API Gateway can protect the configured `verification.batch` API route before its normal Verification Service Kitex call. With `STABLEPAY_MACHINE_PAY_ENABLED=true` (compose input `AIPAY_MACHINE_PAY_ENABLED=true`), the route requires `X-Idempotency-Key`, returns HTTP 402 with `amtPaymentLink`, and accepts the retried `Payment-Proof` header. Its sandbox example price is ¥0.01. Machine Pay uses the same 402/retry boundary as the existing x402 flow; it keeps Alipay's `amtPaymentLink` and `Payment-Proof` independent of x402's `PAYMENT-REQUIRED` and `PAYMENT-SIGNATURE`.

## Sandbox prerequisites

The official console flow still requires an approved Machine Pay service registration and its `service_id`. Local `.env` contains the classic sandbox buyer/seller accounts and app ID; it does not contain the AI Pay service ID or application key material. The one-stop guide's displayed test app/account set is different from those local values. A real AI Pay sandbox precreate/payment cannot be claimed until the chosen AI Pay app is registered, its matching key pair is configured, and the service ID is available. Keep private keys in a read-only mounted directory when possible; never commit them or print them to logs.

For the configured Gateway route, send the same idempotency key on the initial request and payment retry, while using a fresh Gateway nonce for each HTTP request. After a valid payment, the Gateway calls the normal Verification Service Kitex method and returns its result.
