---
name: showmethemoney-pro
description: execute the paid showmethemoney premium action through the merchant backend. use when the user wants to unlock or run the protected showmethemoney capability, and stablepay payment should be used before retrying the backend action.
---

# ShowMeTheMoney Pro

Execute the protected ShowMeTheMoney premium action only after merchant-backend verification and StablePay payment succeed.

## Configuration (Merchant Backend as Source of Truth)

**Important**: This skill does NOT define fixed prices or skill DIDs. All payment parameters are determined by the merchant backend.

- merchant_backend_base_url: `http://127.0.0.1:8787`
- premium_action_endpoint: `/execute`

**The merchant backend is the ONLY source of truth for**:
- `skill_did` - The skill identifier to pay for
- `price` - The amount to charge (in combination with `currency`)
- `currency` - The payment currency (USDC/USDT)
- `payment_endpoint` - Where to submit the payment

**Never use hardcoded defaults**. Always obtain these values from the backend's 402 response.

## Preconditions

Before using this skill:

1. Call `stablepay_runtime_status`.
2. If no local wallet exists, create or bind one.
3. If no backend DID is registered, call `stablepay_register_local_did`.
4. If payment limits are missing, call `stablepay_configure_payment_limits`.

Do not require `stablepay_build_payment_policy` unless another workflow explicitly depends on it.

## Main workflow

When the user asks to use the premium ShowMeTheMoney capability:

1. Resolve the current buyer DID from `stablepay_runtime_status`.
2. Call the merchant backend premium endpoint:
   - `GET http://127.0.0.1:8787/execute?agent_did=<buyer_did>`
3. Treat the merchant backend as the source of truth.
4. If the backend returns `200`, return the protected result.
5. If the backend returns `402 Payment Required`:
   - **MUST** read ALL payment parameters (`skill_did`, `price`, `currency`, `payment_endpoint`) from the backend response
   - **NEVER** use any hardcoded or fallback values
   - call `stablepay_pay_via_gateway` with the exact values from the backend
6. If payment succeeds, retry the same `/execute` request once.
7. If the retry still does not return `200`, explain that the premium action is still locked or verification failed.

**Important**: If the merchant backend is unreachable or does not return a valid 402 response, do NOT proceed with payment. Report the backend error to the user.

## Premium action contract

Use this request for the premium action:

- method: `GET`
- endpoint: `http://127.0.0.1:8787/execute`
- required query parameter: `agent_did`

Optional query parameters may be used when helpful:

- `q`
- `prompt`

These optional values are forwarded to the backend as request text for the premium action.

## Expected backend behavior

The backend should:

1. receive the premium request
2. verify purchase state via StablePay
3. return `402` when the user has not purchased the skill
4. return `200` only after verification succeeds
5. return a merchant-generated proof token in the premium result

Treat the backend response as the final authority.

## Success criteria

A successful premium response should include a backend-generated result such as:

- `protected_result.kind = merchant-generated-proof`
- `protected_result.proof.display_token`
- `access.verified_by_backend = true`

This proof token is the strongest evidence that the premium capability was unlocked through backend verification, not guessed or simulated locally.

## Payment rules

When payment is required:

1. Use `stablepay_pay_via_gateway`.
2. Use the requirement returned by the backend when present.
3. Respect local payment limits already configured in the StablePay plugin.
4. Never claim payment succeeded unless StablePay returns a successful result.
5. Retry the premium action only once after a successful payment.

## What not to use as proof of access

Never treat any of the following as proof that the premium action is unlocked:

- local plugin state
- wallet existence
- DID registration alone
- local payment policy alone
- raw Solana balance checks
- a previous purchase attempt without successful backend verification

Only the merchant backend may decide that access is unlocked.

## Scope limits

This skill is only for the protected premium action exposed by:

- `GET /execute?agent_did=<buyer_did>`

Do not treat these routes as the main premium workflow:

- `/developer/revenue`
- `/developer/sales`
- `/agent/balance`
- `/agent/transactions`

Those are debugging or operator-facing routes, not the premium capability itself.

## Safety

- Never expose private keys, mnemonic material, decrypted local state, API keys, or merchant secrets.
- Never bypass backend verification.
- Never invent payment success.
- Never claim premium access is unlocked unless the backend returns success.
- Never return fabricated proof tokens.
