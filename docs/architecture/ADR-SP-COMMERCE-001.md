# ADR: SP-COMMERCE-001 purchase orchestration boundary

- Status: M0 audit decision complete; M1 implementation in progress (checkpoint 2026-10-11).
- Audited baseline: `0a0144896e7a3c4cabbc5eaf4af8563c93aaab3a` (`main` = `origin/main` at audit time).
- Scope: connect buyer intent, discovery, live quote/stock validation, explicit approval, payment, verification, delivery, and recovery without creating a second production purchase state machine.

## Decision

`commerce-runtime` remains the sole durable authority for a purchase episode and all payment-related transitions. The existing AgentShopping benchmark package may supply discovery algorithms and benchmark fixtures, but its simulation task/order/payment tables are not the production transaction ledger and its `Checkout` method is not the production payment path.

The integration will be an adapter/facade over Runtime-owned operations and facts. It must not write Runtime, Payment Service, or Verification Service tables directly, call Payment Service around Runtime, or treat a benchmark `simulated_payments` row as a settled StablePay payment.

Before any payment intent is reserved, Runtime must persist a buyer authorization that identifies the exact selected merchant and SKU, the immutable quote identity/version/hash, amount, currency, quantity, inventory revision (when supplied by the merchant), buyer/requester, and expiry. A fresh server-side quote/availability check must still match that authorization. A mismatch expires the authorization and requires a new buyer confirmation. This buyer confirmation is separate from Runtime's existing threshold-based Parent Approval: the former approves the exact purchase; the latter is an additional risk/authority gate.

After that gate, continue through the existing Runtime-owned `PaymentIntent` + budget ledger + payment adapter + reconciliation + entitlement + delivery/validation flow. Do not add a competing order/payment saga. If an external order reference is required for merchant fulfillment, bind it to the Runtime episode and delivery facts rather than making it a second source of payment truth.

## Evidence from the audited implementation

1. `POST /v1/episodes` is authenticated and idempotent, but calls `CreateEpisode` and then immediately `Runner.Enqueue`. The Runner advances accepted episodes through discovery, merchant invocation/x402 parsing, payment-intent reservation, authorization/submission, reconciliation, entitlement verification, delivery, and validation. It selects the first Runtime candidate deterministically. The current endpoint therefore cannot safely accept an unconfirmed shopping result as though it were already a buyer-approved exact offer.
2. Runtime's `ReservePaymentIntent` validates the trusted quote against the selected candidate and persists the budget reservation, episode/event transition, ledger entry, and payment intent via one `CommitFinanceTransition`. The MySQL implementation uses a transaction, row lock, episode-version check, idempotency checks, and ledger sequence checks. This is the payment boundary to preserve.
3. Runtime's production composition uses a Merchant HTTP adapter, DID policy adapter, Payment Service Kitex adapter, and Verification entitlement adapter. Verification accepts proof only when the returned `tx_id` equals the Runtime intent's `tx_id`. Runtime has persisted episode events, candidate sets, payment requirement facts, payment intents, ledger entries, execution status, delivery artifacts, and validation evidence.
4. AgentShopping's persistent implementation is explicitly isolated to loopback MySQL `stablepay_shopping_sim` using a dedicated `stablepay_sim` user and fixed tenant `stablepay-shopping-sim`. Its `Checkout` transaction creates a simulated order/payment, adjusts/reserves inventory, and writes simulation audit rows in that isolated schema. It is useful for benchmark-only discovery/quote/confirmation/fault behavior, not evidence of a real payment-service transaction.
5. The production Merchant `products` schema currently contains catalog fields (SKU, title, description, price, currency, status, `skill_did`, etc.) but no stock quantity, reservation, shipping-region, or quote-revision contract. The benchmark simulation has those concepts in its own `sim_inventory` and `sim_quotes`; they are not automatically authoritative Merchant facts.
6. Payment Service owns `payment_transactions` and `payment_idempotency_keys`; Verification Service owns `purchase_records`, populated asynchronously from successful payment events. Those are separate service-owned records, not tables the shopping adapter may mutate.
7. Verification's current `purchase_records` schema has a unique key on `(agent_did, skill_did)`, and the proof lookup is also by that pair. The Runtime entitlement adapter additionally requires the returned proof `tx_id` to match the current Runtime intent. A later distinct transaction by the same buyer to the same merchant can therefore conflict with the existing row and remain unverifiable. The idempotent replay path handles duplicate event delivery, but does not make this schema multi-transaction-per-buyer/merchant.

## Source-of-truth inventory

| Fact | Current owner | Use in the integrated path | Boundary / gap |
|---|---|---|---|
| Merchant product identity, listed price, currency, status, payee identity | Merchant service (`products`) | Product identity and merchant-facing purchase resource | No live inventory/region/revision API in the current product schema |
| Synthetic stock, region, quote snapshots, buyer confirmations, simulated orders/payments, simulated fulfillment | `commerce-runtime/benchmarks/agentshopping` in isolated `stablepay_shopping_sim` | Local benchmark and deterministic fault tests only | `DataOrigin=SYNTHETIC_SIMULATION`; never production payment truth |
| Immutable purchase request, episode lifecycle, candidate selection, payment requirement, approval/recovery facts, budget projection, delivery/validation | Commerce Runtime MySQL store | Canonical end-to-end episode, restart/resume, audit, and payment authorization boundary | Shopping-specific exact-selection/confirmation API is not yet exposed |
| Payment transaction, payment idempotency, chain result | Payment Service | Canonical payment-side status and retry deduplication | Called only through Runtime's payment adapter in the unified flow |
| Purchase/entitlement proof | Verification Service `purchase_records` | Verify the exact payment transaction before delivery | Asynchronous projection; Runtime must tolerate not-yet-visible proof and reconcile/retry |

## Rejected alternatives

- **Use AgentShopping `Checkout` as the end-to-end purchase:** rejected because it creates a simulation-only payment synchronously inside its own MySQL transaction and never exercises Payment Service, chain settlement, Verification, or Runtime recovery.
- **Create a second production Shopping order/payment saga:** rejected because order/approval/payment/recovery facts would have competing authorities and would make crash recovery and audit ambiguous.
- **Call Payment Service directly from the shopping API or MCP bridge:** rejected because it bypasses Runtime's budget ledger, payment guard, persisted intent, and reconciliation.
- **Treat Parent Approval as buyer confirmation:** rejected because threshold approval is an additional risk policy gate, not proof that the buyer accepted this exact SKU/quote/amount.
- **Treat catalog price hints as a live quote or stock guarantee:** rejected; Runtime already distinguishes discovery facts from the x402 payment requirement, and stock freshness is currently absent from the production Merchant contract.

## M1 implementation gates

1. **Partially implemented:** Runtime exposes the buyer offer from the current persisted payment-requirement fact; `POST /v1/episodes/{id}/buyer-offers/refresh` obtains a new x402 challenge through the existing selected Merchant adapter and leaves the episode at the buyer-confirmation gate. The signed confirmation and PaymentIntent identity now include the exact `offer_ref` (payment-requirement ID), not only the quote hash. Runtime revalidates expiry/availability after signing and before finance reservation. A deterministic API test covers changed terms, idempotent refresh replay, stale-after-approval, and a fresh signature when the quote hash is unchanged but the offer identity advances. **Remaining:** no production catalog/stock/region revision adapter or production `BuyerQuoteValidator` wiring; this is not yet the full Shopping facade or full M1 acceptance.
2. **Not implemented:** a versioned Merchant inventory/quote contract that supplies authoritative product, region, stock revision, and availability. The current refresh consumes an x402 payment challenge only; it does not prove stock or region eligibility.
3. **Open blocker:** resolve Verification cardinality before claiming repeated production purchases: either make transaction receipts queryable by a stable transaction/purchase key while preserving the existing one-time entitlement API, or explicitly scope/deny repeat purchase of the same merchant entitlement. Do not remove the existing uniqueness constraint without a migration and compatibility review.
4. **Partially tested:** deterministic API tests exercise buyer approval, stale quote gating, changed terms, and stable refresh replay with in-memory Runtime storage and mocked service adapters. The MySQL 8.0 integration suite also ran against a fresh isolated database; the new offer-binding case persisted and reloaded two confirmations with one quote hash and distinct offer refs. SQL metadata confirmed `offer_ref NOT NULL`, unique `(episode_id, offer_ref)`, non-unique quote lookup, and removal of the old quote-only unique index; the test episode left zero confirmation rows after cleanup. **Still missing:** SQL audit of a complete payment/ledger/verification/delivery transaction, restart recovery at each failure point, response-loss recovery, full delivery recovery matrix, and repeated same-buyer/same-merchant purchase semantics.
5. **Partially exercised, not accepted:** the full Go suite ran with `COMMERCE_RUNTIME_MYSQL_DSN` pointed only at a dedicated disposable MySQL 8.0 test database; its data directory is under ignored `.local-run`, the test container is stopped, and existing Runtime/E4 databases were not used. The end-to-end buyer gate currently has deterministic API/mock evidence on in-memory storage, not a full Runtime/MySQL purchase acceptance. A real Devnet transaction remains a later, separately bounded step requiring explicit user confirmation of network, wallet, amount cap, and transaction count.

No Devnet payment, shared-database migration, dataset download, or Kubernetes mutation is part of this checkpoint.

## Rollout / safety

M0 makes no service-code, schema, database, cluster, or payment changes. For later M4 cluster work, first obtain an explicit maintenance window, confirm no in-flight payment/verification, save the exact manifests and rollback plan, run dry-run and resource checks, and preserve the six-node topology. Do not force load above a failing resource guard.
