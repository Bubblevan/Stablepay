# SP-COMMERCE-001 interface contract (M0 contract, M1 implementation in progress)

- Status: M0 audit/contract recorded; buyer-confirmation, exact selection binding, live quote revalidation, and an opt-in Merchant catalog import are implemented. Full M1 acceptance is not complete (checkpoint 2026-10-11).
- Authority rule: shopping/discovery is an input to Commerce Runtime. Runtime remains the only owner of durable purchase/payment/recovery state.

## Existing interfaces observed

| Interface | Existing behavior | M0 conclusion |
|---|---|---|
| Runtime `GET /readyz` | Readiness check; can include production composition and MySQL/supervisor readiness | Use for process readiness, not as proof that Merchant, Payment Service, chain, or Verification are all transaction-ready |
| Runtime `POST /v1/episodes` | Accepts `AcquireCapabilityRequest`; `Idempotency-Key` must match `request_id`; creates/replays an episode and enqueues the Runner | Not safe as the shopping confirmation endpoint: it can begin autonomous processing immediately |
| Runtime `GET /v1/episodes/{episode_id}` and `/events` | Reads persisted episode/status and event history | Suitable for status and audit after a purchase is created |
| Runtime `POST /v1/episodes/{episode_id}/parent-decisions` | Records an existing Parent Approval decision and enqueues/resumes the episode | Additional policy gate only; not a substitute for buyer approval of the exact product/quote |
| Runtime `POST /v1/episodes/{episode_id}/buyer-confirmations` | Records a buyer signature bound to the current offer; accepted confirmation resumes the Runtime flow | Exact buyer authorization; distinct from Parent Approval |
| Runtime `POST /v1/episodes/{episode_id}/buyer-offers/refresh` | Re-invokes the selected Merchant adapter for a new HTTP 402 challenge while `AWAITING_BUYER_CONFIRMATION` | Refreshes the Runtime payment requirement; does not itself prove catalog stock, region eligibility, or service availability |
| AgentShopping `/v1/intents/parse`, `/v1/quotes`, `/confirm`, `/v1/orders`, `/fulfillment` | Benchmark API over isolated simulation store; checkout writes simulation-only settlement | Keep for local quality/fault evaluation; do not use as the production purchase API |
| Merchant `GET /api/v1/products`, `GET /api/v1/products/{sku}`, `GET /api/v1/products/{sku}/execute` | Lists/details products; execute returns x402 requirements or performs the purchase path when a valid payment signature is supplied | `MerchantCatalogReader` imports current listed price/status and operator-authored capability metadata at Runtime startup; current Merchant schema has no authoritative stock-region revision |

## Implemented M1 subset (not a full acceptance)

- `GET /v1/episodes/{episode_id}` exposes `buyer_offer` when the Runtime has a current persisted payment requirement. The offer includes `offer_ref` (the immutable Runtime payment-requirement ID), quote hash, product, amount/currency, payee/network/asset/resource, and expiry.
- `POST /v1/episodes/{episode_id}/buyer-offers/refresh` requires `Idempotency-Key`; it is accepted only while the episode is awaiting buyer confirmation. A new x402 challenge is persisted as a new payment-requirement fact and the episode remains paused for approval. A replay with the same key returns the persisted offer without re-invoking the Merchant.
- `POST /v1/episodes/{episode_id}/buyer-confirmations` now requires the exact `offer_ref` as well as quote hash, amount, currency, acceptance, and a requester signature. The signature message binds the immutable offer identity, so a later refresh needs a new signature even if the quote hash is unchanged.
- Runtime checks the current offer at confirmation time and again before payment reservation. A stale or expired offer must return to the buyer gate; no budget reservation or payment submission should occur for that stale offer.
- `AcquireCapabilityRequest.purchase_selection` now optionally pins a controlled shopping request to one exact `merchant_did` + `capability_id` + x402 `product_id` (stable SKU). It is part of the immutable request snapshot/idempotency hash, requires buyer confirmation, disallows cross-merchant fallback, filters Runtime discovery to the pinned capability, and rejects an x402 challenge whose `product_id` differs before a buyer offer or PaymentIntent is created. HTTP and MCP acquire share the same contract; the MCP schema advertises the field. This is exact identity binding only: quantity, region, catalog revision, and authoritative inventory revision are not yet part of this contract.
- Production composition can opt into `COMMERCE_RUNTIME_MERCHANT_CATALOG_MANIFEST`. The strict, bounded manifest declares the trusted Merchant DID, payee DID, product API URL, and product-to-capability metadata (task/content schemas, protocol, currency, pricing model, and metadata revision); the adapter reads Merchant list/detail endpoints and persists immutable Runtime catalog versions before starting the Runner. `updated_at` plus operator `metadata_version` form the catalog version. Missing declared SKUs fail closed unless `allow_missing_sku` is explicitly set for retirement. Removing a source/capability from a subsequent manifest sync retires its previously imported active capability; an empty capability list retires that source. Product tags/descriptions are never promoted to capability semantics. An example is in [SP-COMMERCE-001-merchant-catalog-manifest.example.json](SP-COMMERCE-001-merchant-catalog-manifest.example.json).
- This is a startup snapshot, not continuous catalog synchronization. Merchant `active` status currently means listed/purchasable and is mapped to Runtime availability; it is not a stock count, reservation, delivery-region check, or concurrency guarantee. The exact x402 quote is still reread before approval/payment. A Merchant price/status change is reflected in candidate discovery after a Runtime restart; periodic refresh and authoritative inventory/service availability remain open.
- Evidence so far: deterministic API tests cover changed terms, same-key/late refresh replay (including after completion), stale-after-approval, and reapproval of an identical quote hash with a new offer ref. A MySQL-backed API E2E completed the current Runtime path with deterministic mock Merchant, payment, and entitlement adapters. Episode `ce_115a0f277682ff112c635ed4dea0b009` ended `FULFILLED`; SQL showed two accepted confirmations on distinct offer refs, one confirmed PaymentIntent/economic identity, three ledger entries, one HTTP-200 delivery, one valid `NON_EMPTY` validation, and zero confirmations without a matching payment-requirement fact. Budget projection: limit 1000 minor units, consumed 25, available 975, sunk cost 25. Two earlier failing MySQL episodes were retained in `AWAITING_BUYER_CONFIRMATION` with zero confirmations, PaymentIntents, or ledger entries. Schema audit verified `offer_ref NOT NULL`, unique `(episode_id, offer_ref)`, non-unique quote lookup, removal of the old quote-only unique index, and `episode_events.actor VARCHAR(128)`. The read-only SQL is reproducible from [SP-COMMERCE-001-m1-audit.sql](SP-COMMERCE-001-m1-audit.sql). The database is isolated at `.local-run/commerce-m1-mysql-data`; its Docker container is stopped. This is local deterministic mock evidence, not real Merchant, Payment Service, Devnet, or process-restart/recovery evidence.

- Follow-up MySQL verification for exact product binding: Episode `ce_1e91123fc8c5d6a9cfa4a5bc0cc6d6cd` completed the isolated Runtime flow with `purchase_selection={merchant_did: did:merchant:test, capability_id: api-test, product_id: api-test}`. Read-only SQL returned `FULFILLED`; both accepted confirmations matched distinct persisted payment-requirement refs; there was 1 confirmed intent/economic key, 3 ledger entries, HTTP 200 delivery, valid `NON_EMPTY` evidence, and 0 confirmations without a requirement. Projection was limit/reserved/settled/consumed/available/sunk = `1000/0/25/25/975/25`. A separate negative test rejects a mismatched x402 ProductID with no PaymentIntent. The repeated MySQL run also uses a maximum-length buyer idempotency key in distinct Episodes; persisted event keys use a bounded digest scoped to the Episode, avoiding overflow and collision on the database's globally unique event-key index. Test adapters remain deterministic mocks.

Still missing for M1: quantity/region/inventory-revision binding and authoritative inventory semantics; natural-language Agent discovery through a unified HTTP/MCP/CLI entry point; catalog synchronization while Runtime remains up; audit of actual Payment Service and Verification-owned economic facts; concurrent/restart/response-loss and full fulfillment recovery acceptance; and resolution of Verification's repeated-purchase cardinality. The live HTTP x402 quote validator is wired in production composition, but it proves only that the current challenge matches approved terms; it does not assert stock/region semantics absent from the Merchant API. Do not describe this subset as the complete autonomous-shopping path.

## Proposed integrated operations

The following are logical operations. M1 must choose stable HTTP/MCP names and version the schema before implementation; the existing benchmark routes are not aliases for them.

### 1. Create or replay a purchase request

Input must include:

- stable `request_id` / idempotency key and requester/buyer identity;
- original buyer text or an immutable content reference plus SHA-256;
- normalized constraints: budget in integer minor units, currency, delivery region, preferences/required attributes, substitution permission, deadline, and whether buyer confirmation is required;
- contract version and trace/correlation ID.

The adapter validates and normalizes the request, then asks Runtime to create a durable episode (using the existing `AcquireCapabilityRequest` concept). The canonical request snapshot/hash is immutable: same key and same snapshot replays; same key with a changed snapshot conflicts. The facade must ensure the episode is paused in a non-economic discovery/selection phase; it must not call the current eager-enqueue path until that gate exists.

### 2. Discover candidates and issue a quote

Each candidate/quote response must identify:

- `episode_id`, `candidate_set_id`, merchant ID/DID, capability ID, product ID/SKU;
- catalog version/snapshot hash and data origin;
- live price in integer minor units and currency;
- inventory availability, quantity, delivery region, inventory revision (or an explicit `UNKNOWN` where no authoritative stock provider exists);
- quote ID/version/hash, issue/expiry timestamps, x402 resource/network/asset/payee identity when applicable;
- substitution decision and the reason/features used to rank it.

Catalog price hints are ranking inputs only. The purchase amount is the freshly validated merchant payment requirement that Runtime records as a payment-requirement fact. If price, stock revision, region eligibility, payee, or quote hash changes after presentation, invalidate the presented confirmation and return a reconfirmation-required result. Never silently substitute a product after approval.

### 3. Persist exact buyer confirmation

Confirmation is a Runtime-owned durable fact and is bound to:

`requester + episode + merchant + SKU + quantity + region + quote_id/version/hash + inventory_revision + amount_minor + currency + expires_at`.

The buyer must explicitly accept the displayed tuple. The server revalidates freshness and budget at confirmation time; client-supplied `accepted=true` or amount is not trusted without identity/authentication and equality checks. Repeating the same idempotency key and same payload returns the same fact; reusing it with a different tuple is a conflict. Rejection, expiry, stale stock, stale price, and insufficient budget must create no payment effect. A changed quote requires a new confirmation ID/key.

This exact-purchase confirmation is not the same record as `ParentApprovalRequest` / `ParentDecisionFact`. If the Runtime threshold policy also requires a parent decision, both gates must pass before payment authorization/submission.

### 4. Continue through the existing Runtime transaction path

Only after the exact buyer-confirmation fact is committed may Runtime:

1. persist `BUDGET_RESERVED`, the episode event/projection, and the Runtime `PaymentIntent` atomically;
2. run the existing payment guard and DID/credential authorization;
3. submit via the existing Runtime Payment Service adapter with the stable intent idempotency key;
4. reconcile ambiguous/pending outcomes without blind duplicate submission;
5. require Verification proof bound to the same `tx_id` before invoking delivery;
6. persist delivery and validation evidence and reach a terminal or explicitly recoverable state.

The shopping component must not write `payment_transactions`, `payment_idempotency_keys`, Runtime finance tables, or `purchase_records`; it must not report “paid” from a quote, order row, mock payment, or HTTP success alone.

## Identity and mapping rules

| Shopping concept | Runtime / service identity | Rule |
|---|---|---|
| Shopping task/request | `request_id` + `episode_id` | Task IDs may be recorded as provenance but must not replace Runtime identity |
| Merchant fixture ID | Merchant DID + Runtime capability ID | Do not infer payee wallet from display name; payee comes from trusted catalog/payment requirement |
| Product SKU | Merchant product ID and x402 `ProductID` | Keep SKU explicit through selection, quote, confirmation, and audit |
| Quote snapshot | Runtime payment-requirement fact + canonical quote hash | A benchmark quote ID is not a production quote unless the adapter can revalidate the same immutable tuple |
| Buyer confirmation | New Runtime-owned fact/event | Must be independently auditable and distinct from Parent Approval |
| Order / payment attempt | Runtime `episode_id` / `PaymentIntent.IntentID` / payment-service `tx_id` | Preserve all IDs; do not collapse or regenerate them on retry |
| Entitlement | Verification proof `tx_id` | Must exactly match the Runtime intent transaction before delivery |
| Delivery | Runtime delivery artifact + validation evidence | A simulation fulfillment reference is benchmark-only unless produced by the selected delivery adapter and persisted by Runtime |

## Verification cardinality blocker

The current Verification projection is keyed uniquely by `(agent_did, skill_did)`, and its proof endpoint reads by that pair. Runtime correctly rejects a proof whose `tx_id` is not the current payment intent. Consequently, replay of the same event is safe, but two distinct successful purchases by the same requester from the same merchant are not represented by the current one-row entitlement model: a later payment event may fail to persist, while the old proof cannot satisfy the new Runtime episode. M1 must choose and test one of these explicit semantics before repeated-purchase acceptance:

1. retain the legacy “one entitlement per buyer/merchant” contract and reject/short-circuit a second purchase before charging; or
2. add a backward-compatible transaction/purchase-scoped receipt query and persistence identity (including a reviewed migration and event-consumer idempotency rules), while leaving the existing entitlement query semantics intact.

The shopping adapter must never work around this by reading or writing Verification's database directly.

## Required invariants and errors

- Stable request ID with changed request content: conflict; no second episode.
- Duplicate same confirmation: replay; exactly one payment intent and one downstream economic action.
- Same confirmation/idempotency key with a different SKU, quote, amount, region, or buyer: conflict.
- Stale/expired quote or changed stock/price: no payment intent or settlement; require fresh quote and buyer confirmation.
- Budget exceeded or unauthorized buyer: fail closed before payment.
- Payment timeout/response loss: Runtime intent remains reconcilable; never infer failure and issue a new economic key blindly.
- Verification not yet projected: pending/retry; no delivery until exact `tx_id` proof is found.
- Local mock mode: unmistakable `data_origin=LOCAL_DETERMINISTIC_MOCK`, isolated DB/schema and credentials, no real payment adapter and no Devnet egress.
- Devnet mode: a separate explicit invocation with an allowlisted network, user-reviewed amount/count cap, and auditable transaction signature; it is not enabled by a local benchmark flag.

## M1 acceptance contract

Before the interface is considered implemented, tests must demonstrate the listed pre-payment gates, quote/stock invalidation, same-key replay and changed-payload conflict, concurrent duplicate requests, process restart recovery, exact verification binding, delivery gating, and SQL invariants across Runtime-owned tables. Test reports must distinguish deterministic mock evidence, independent discovery-quality evaluation, and any separately authorized Devnet evidence.
