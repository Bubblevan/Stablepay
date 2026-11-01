# Alipay Adapter

`alipay-adapter` isolates the Alipay sandbox protocol and RSA2 key material. It exposes Kitex RPC on `:8086` only; it has no HTTP listener and stores no payment state.

Payment Service calls it to generate a cashier URL, query a trade, and verify provider notifications. Order creation, status, idempotency, and the transactional RocketMQ outbox remain owned by `payment-service` and MySQL. The Hertz Gateway receives `/api/payments/alipay/notify` and forwards the form to Payment Service over Kitex.

Configure `ALIPAY_APP_ID`, `ALIPAY_SELLER_ID`, `ALIPAY_PRIVATE_KEY_FILE`, `ALIPAY_PUBLIC_KEY_FILE`, `ALIPAY_NOTIFY_URL`, and `ALIPAY_RETURN_URL` through the deployment environment. Mount the key directory read-only. Never put key contents in config files or logs.

The current provider implementation deliberately uses the Alipay computer website sandbox API `alipay.trade.page.pay`; production credentials and mobile `alipay.trade.agent.pay` are not enabled by this adapter.
