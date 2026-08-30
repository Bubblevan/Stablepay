---
name: showmethemoney-pro
description: paid stablepay skill for openclaw with strict backend purchase verification. use when the user wants to access a protected premium capability that should require stablepay payment and backend verification before execution.
---

# ShowMeTheMoney Pro

Provide a strong-protection paid skill that executes protected premium actions only after StablePay purchase verification succeeds.

## Merchant configuration

Use the following merchant placeholders for this skill:

- skill_name: `{{SKILL_NAME}}`
- skill_did: `{{SKILL_DID}}`
- default_price_usdc: `{{PRICE_USDC}}`
- currency: `{{CURRENCY}}`
- stablepay_gateway_base_url: `{{STABLEPAY_GATEWAY_BASE_URL}}`
- merchant_backend_base_url: `{{MERCHANT_BACKEND_BASE_URL}}`
- verify_endpoint: `{{VERIFY_ENDPOINT}}`
- premium_action_endpoint: `{{PREMIUM_ACTION_ENDPOINT}}`

Replace all placeholders before publishing a real version.

## Required runtime assumptions

Proceed only if all of the following are true:

- the StablePay plugin is installed and enabled
- a local wallet runtime is available
- the StablePay backend is reachable
- the merchant backend is reachable
- the merchant backend uses StablePay verification before serving the premium capability

If any dependency is missing, explain what is unavailable and stop.

## Preflight workflow

Before attempting a protected premium action:

1. Call `stablepay_runtime_status`
2. If no local wallet exists, call `stablepay_create_local_wallet`
3. If no backend DID is registered, call `stablepay_register_local_did`
4. If payment limits are missing, call `stablepay_configure_payment_limits`
5. If no payment policy exists, call `stablepay_build_payment_policy`

Do not continue to the premium action if wallet setup, DID registration, or payment policy setup is incomplete.

## Protected premium workflow

When the user requests the premium capability:

1. Call the merchant backend premium action endpoint.
2. Treat the merchant backend as the authority for whether the action is unlocked.
3. If the merchant backend succeeds, return the premium result.
4. If the merchant backend responds with HTTP `402 Payment Required`:
   - explain that payment is required for this premium capability
   - show the configured price and currency using:
     - price: `{{PRICE_USDC}}`
     - currency: `{{CURRENCY}}`
     - skill_did: `{{SKILL_DID}}`
   - use the StablePay plugin to complete payment
   - prefer `stablepay_pay_via_gateway`
5. Only continue after StablePay confirms successful payment.
6. Retry the merchant backend premium action once after successful payment.
7. If the retry still fails, or if verification still denies access, stop and explain the failure.

## Verification rules

Treat backend verification as the source of truth.

The expected backend behavior is:

1. receive the user's premium request
2. resolve the buyer identity
3. call StablePay verification using the configured `skill_did`
4. if not purchased, return HTTP `402 Payment Required`
5. if purchased, execute the premium action

Do not bypass merchant backend verification for protected actions.

## Purchase integrity rules

- Never treat local plugin state as proof of purchase.
- Never assume the user owns the premium action just because a wallet exists.
- Never continue a protected action after a failed verify or failed payment.
- Always rely on backend verification or confirmed StablePay purchase results.

## Intended protection level

This skill is a strong-protection paid skill.

That means:

- the skill is publicly installable
- the premium action is not publicly executable
- the true access boundary is the protected backend capability
- StablePay purchase and backend verification are both required for protected use

## Developer integration notes

Use this skill pattern when a developer wants to integrate StablePay into a real merchant flow.

The developer should:

- register a seller wallet and obtain a real `skill_did`
- configure the merchant backend with that `skill_did`
- implement a protected premium endpoint
- call StablePay verification before serving the premium result
- return HTTP `402 Payment Required` when the user has not purchased the capability

## Safety

- Never expose private keys, mnemonic material, decrypted local state, API keys, merchant secrets, or internal backend credentials.
- Never invent payment success.
- Never claim a premium action is unlocked unless the backend or StablePay verification confirms it.
- Never bypass the protected backend path for premium functionality.