# StablePay + Open Wallet Standard (OWS) Integration Plan

Updated: 2026-03-31

## 1. Goal

This document does not propose a new remote microservice.
It defines OWS as the local wallet and signing runtime that should live on the same machine as OpenClaw, the local HTTP client, or the StablePay plugin.

The target is to move wallet custody and signing back to the client side while keeping DID, Payment, Verification, and Query as backend services.

## 2. Current gap versus PRD and Tech

What already works today:

- The A2 backend chain is already runnable through `api-gateway -> payment-service -> RocketMQ -> verification-service -> query-service`.
- Gateway DID-auth signing is already connected.
- Verification and Query can already consume and display purchase results.

What still conflicts with the intended OWS model:

- `did-service` still creates wallets on the server side.
- `did-service` still stores encrypted private keys server side.
- `payment-service` still contains stubbed DID verification behavior in part of the flow.
- The current OpenClaw plugin is mostly a mock X verification helper, not a local wallet runtime.

This means the repository proves that the payment microservice chain works, but it does not yet satisfy these product requirements:

- private key stays local
- automatic purchase is controlled by a local signing runtime
- delegated agent signing is policy-gated before signing
- signing policy is enforced locally before backend execution

## 3. Design principles

### 3.1 OWS system role

OWS should be treated as a local runtime, not a remote service.

It should own:

- local wallet creation
- local encrypted key storage
- local policy creation
- local agent key creation
- local message or transaction signing

Backend services should still own:

- DID registration and signature verification
- replay protection
- amount and currency checks
- purchase record persistence
- query projection

### 3.2 What phase 1 should not do

To avoid breaking the working chain too early, phase 1 should not:

- rewrite the current 402 flow into x402
- implement real X verification
- implement real Solana execution
- move business validation out of backend services

### 3.3 What phase 1 must do

Phase 1 must:

- move wallet creation from the server to the OpenClaw side
- move signing from ad hoc local key handling to a dedicated local runtime abstraction
- encrypt local payment config and delegated key material
- keep backend DID, amount, nonce, timestamp, and balance validation intact

## 4. Phased implementation plan

### 4.1 Phase 1: OpenClaw local wallet runtime

Target:

- OpenClaw plugin can create and manage a local wallet runtime
- OpenClaw plugin stores local state in AES-256-GCM encrypted form
- payment still uses the existing `api-gateway -> payment-service` path

Plugin capabilities to add:

- `createLocalWalletByOWS(userId)`
- `loadLocalWalletState()`
- `saveLocalWalletStateEncrypted()`
- `buildStablePayPolicy(config)`
- `createAgentKey(walletId, policyId)`
- `signGatewayRequestWithOWS(...)`
- `signPaymentChallengeWithOWS(...)`

Local state to persist:

- `walletId`
- `did`
- `publicKey`
- `walletAddress`
- `signRuntime`
- `policyId`
- `apiKeyIdMasked`
- `ownerOrAgent`
- `singlePurchaseLimitMinor`
- `autoPurchaseThresholdMinor`
- `encryptedAgentToken`

Local encryption requirements:

- never store plaintext private keys
- never store plaintext agent tokens
- use AES-256-GCM for plugin state
- prefer environment-provided master key or passphrase

### 4.2 Phase 2: DID Service becomes local-wallet registration

Target:

- backend only stores DID, public key, and wallet address mapping
- backend no longer generates or owns user private keys

New API to add:

- `POST /api/v1/did/register`

Suggested request body:

```json
{
  "user_type": "agent",
  "public_key": "base58-pubkey",
  "wallet_address": "base58-address",
  "wallet_id": "stablepay-user123",
  "metadata": {
    "sign_runtime": "ows"
  }
}
```

Backend should store:

- `did`
- `public_key`
- `wallet_address`
- `status`
- `wallet_id`
- `metadata`

Backend should not store:

- user private key
- agent token
- owner passphrase

### 4.3 Phase 3: real backend verification and audit fields

Target:

- `payment-service` uses real DID verification instead of stub behavior
- `verification-service` and `query-service` expose OWS audit fields

Backend capabilities to add:

- `payment-service` verifies signatures through DID Service
- `payment-service` does not trust client-side policy verdicts
- `verification-service` stores signing audit fields
- `query-service` returns signing audit fields

## 5. OpenClaw plugin execution plan

Plugin path:

- `D:\MyLab\StablePay\stablepay-openclaw-plugin`

Current plugin role:

- mock wallet creation
- verify link generation
- mock tweet seeding
- mock verification request
- mock reward balance lookup

Missing today:

- local wallet runtime
- encrypted local key and config storage
- local OWS policy or agent key management
- local payment signing

Phase 1 plugin modules or responsibilities:

- secure local state store
- local crypto helpers
- OWS runtime adapter
- wallet runtime abstraction
- payment config management

Phase 1 plugin tools:

- `stablepay_runtime_status`
- `stablepay_create_local_wallet`
- `stablepay_register_local_did`
- `stablepay_configure_payment_limits`
- `stablepay_build_payment_policy`
- `stablepay_sign_message`

Runtime preference order:

1. `ows-sdk`
2. `ows-cli`
3. `local-dev fallback`

Notes:

- `ows-sdk` is the long-term target
- `ows-cli` is the easiest compatibility bridge when available
- `local-dev fallback` is only for local development when OWS is unavailable

## 6. ShowMeTheMoney demo path

Skill path:

- `D:\MyLab\StablePay\stablepay-openclaw-plugin\showmethemoney-skill`

This skill is a good first-phase demo target because it already has:

- paid skill semantics
- price metadata
- execute endpoint
- purchase verification semantics

Recommended demo chain:

### A1 downgraded

1. OpenClaw plugin creates local wallet
2. OpenClaw plugin registers DID
3. X verification remains skipped
4. real reward remains skipped
5. user configures local limits
6. plugin generates OWS-ready policy material

### A2 downgraded

1. user triggers `showmethemoney-skill`
2. skill backend returns 402
3. plugin reads local threshold config
4. local runtime applies policy or limit checks
5. local runtime signs the payment challenge
6. request goes through `api-gateway -> payment-service`
7. backend continues through `RocketMQ -> Verification -> Query`
8. purchase succeeds and the skill can be retried

## 7. Microservice change list

### API Gateway

- keep current authentication model
- optionally log `x-sign-mode`, `x-wallet-id`, `x-policy-id`
- do not treat these audit fields as authorization on their own

### DID Service

- add local-wallet registration API
- keep signature verification API
- stop server-side wallet generation as the product path

### Payment Service

- keep current 402 and payment flow shape
- verify DID signatures for real
- keep validating amount, currency, nonce, timestamp, skill target, and balance

### Blockchain Adapter

- keep minimal contract for now
- return `tx_id`, `tx_hash`, `status`, and `confirmed_at`

### Verification Service

- keep consuming payment success events
- extend purchase record with signing audit fields such as `sign_mode`, `wallet_id`, `policy_id`, and `api_key_id_masked`

### Query Service

- extend transactions and recent purchases with the same audit fields
- keep balance and purchase history as the final user-facing projection

## 8. Current recommendation

The best next step is not to wait for real X verification or real Solana.
The best next step is to move the OpenClaw side into the correct shape first:

- local encrypted wallet/runtime state
- local signing entrypoint
- DID registration for client-side wallets
- policy-ready payment preparation

That gives the project a clean path toward the PRD without discarding the backend chain that already works.
