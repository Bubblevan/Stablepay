# Alipay Adapter

`alipay-adapter` isolates the Alipay sandbox protocol and RSA2 key material. It exposes Kitex RPC on `:8086` only; it has no HTTP listener and stores no payment state.

Payment Service calls it to generate a cashier URL, query a trade, and verify provider notifications. Order creation, status, idempotency, and the transactional RocketMQ outbox remain owned by `payment-service` and MySQL. The Hertz Gateway receives `/api/payments/alipay/notify` and forwards the form to Payment Service over Kitex.

For website page-pay, configure `ALIPAY_APP_ID`, `ALIPAY_SELLER_ID`, `ALIPAY_PRIVATE_KEY_FILE`, `ALIPAY_PUBLIC_KEY_FILE`, `ALIPAY_NOTIFY_URL`, and `ALIPAY_RETURN_URL` through the deployment environment. This provider is optional when running Machine Pay only. Mount the key directory read-only. Never put key contents in config files or logs.

The website-payment path uses `alipay.trade.page.pay`. The optional AI Pay Machine Pay RPC uses `alipay.aipay.agent.payment.precreate` and `alipay.aipay.agent.payment.verify`, returning the opaque `amt_pre_pay_id` and validating the signed provider response for a `Payment-Proof`. It is a separate protocol from x402: the shared part is HTTP 402 and the resource/price boundary, while x402 `PAYMENT-REQUIRED` / `PAYMENT-SIGNATURE` are not translated into Alipay's `amtPaymentLink` / `Payment-Proof` fields.

Enable Machine Pay by setting `AIPAY_MACHINE_PAY_SERVICE_ID` after the service is registered and approved in the AI Pay console. `AIPAY_APP_ID`, `AIPAY_PRIVATE_KEY_FILE`, and `AIPAY_ALIPAY_PUBLIC_KEY_FILE` override the existing `ALIPAY_*` app/key settings when Machine Pay uses a different application; the console's raw Base64 values can also be supplied through `AIPAY_PRIVATE_KEY` and `AIPAY_ALIPAY_PUBLIC_KEY`. The Alipay Adapter process can run with these settings alone and does not use DID Service or Blockchain Adapter. Payment Service owns MySQL orders, idempotency, proof replay prevention, and the transactional RocketMQ outbox; Hertz API Gateway gates an explicitly configured API route and forwards it to the normal Kitex service after verified payment.

The sample Gateway route is `POST /api/v1/verify/batch` at CNY 0.01 per call. It is disabled by default; enable it with `AIPAY_MACHINE_PAY_ENABLED=true` in the local compose environment after the matching AI Pay app keys and service ID are configured. The sandbox app/account details displayed by AI Pay may differ from the classic Alipay sandbox values already in the project `.env`.
