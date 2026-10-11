# SP-RESUME-B1 — AgentPay evidence consolidation

Date: 2026-10-12 (Asia/Shanghai)
Repository baseline: `eca91280287980dcb1dcec5313d98990f48d910c` (`HEAD` before this working-tree iteration)

## Evidence modes — do not combine denominators

| Mode | Scope | Result | Evidence and limitation |
|---|---|---|---|
| Fixed 50-task Runtime matrix | Frozen fixture `commerce-runtime/testdata/m1-fixed-50.jsonl`, SHA-256 `5cdcfd267c12bd40dbf6904dbc3eb87ff82db5e53369b3800b8acd655deecf28`; authenticated Runtime path; three local Merchant catalog APIs; deterministic rule parser; Mock payment, entitlement, and delivery. | Correct expected outcomes `50/50`; approved purchases delivered `40/40`; quote-change approvals `10/10`; 4 budget refusals, 3 clarifications, 3 explicit rejections; duplicate PaymentIntent/settlement/tx IDs `0`; task P50/P95 `390.479/442.024 ms`; payment-confirmation-to-delivery P95 `126.561 ms`. MySQL audit: 43 terminal Episodes (`40 FULFILLED`, `3 ABORTED`), 40 confirmed intents matched to 40 settlement rows, no nonterminal/orphan rows in that matrix run. | Run: `.local-run/commerce-runtime/m1-fixed-50/m1-runtime-fixed-50-matrix-20261011T165230.796739600Z/`. These are local deterministic diagnostics—not model quality, Devnet, or production rates. |
| Single real Devnet purchase | One pinned synthetic product, `sim-m11-000995`, through local Merchant `sim-m11`; exact x402 quote; signed BuyerConfirmation; real Payment Service and Solana Devnet; verification and digital delivery. The harness called the Runtime `application.Service` in-process; it did not submit this task to the Runtime HTTP server. | `1/1` confirmed and delivered; `0.99 USDC` (`amount_minor=99`); Episode `ce_ea0623aa78e71e5851732ce29959cdd6` ended `FULFILLED`; PaymentIntent `pi_bbdf759822c626d0d0bd8f12d37d97fb` confirmed; one `PAYMENT_SETTLED`, one verification record, one Merchant HTTP 200 delivery; duplicate intent/settlement/tx rows `0`. | Report: `.local-run/unified-business-e2e-devnet-m11-20261012.json`. Devnet tx `5sJN6pWSJwUVJeSSnho3bF7kwpmPUG85HSt8AdVv9pZq6Hv5KnnrnK4CE45xFaWacBLcHpJ2DPfzMm2hviTTBQ3h`, slot `509976953`. This is one payment to a local synthetic Merchant, not a real Merchant or a statistical reliability claim. |

## Independent audit of the Devnet purchase

- The pre-payment HTTP 402 challenge named `solana:devnet`, the official Devnet USDC mint `4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU`, product `sim-m11-000995`, and seller `NBZtLpE3u95YfirCqMyjZg5p9LZ1f9rjtFnCeuibeCy` as both `payTo` and `skillDid`. Its raw amount was `990000` USDC atomic units (`0.99 USDC`). The signed buyer offer was checked against this exact seller, amount, product, network, and quote hash before settlement.
- Read-only MySQL audit found one confirmed Runtime PaymentIntent, one matching transaction ID/hash, one `PAYMENT_SETTLED` ledger row, one accepted buyer confirmation, one verification `purchase_records` row, and one delivery artifact with HTTP 200. The Runtime projection was budget `1000`, consumed/settled/sunk `99`, reserved `0`, available `901`. The Episode was `FULFILLED`; no duplicate payment or settlement rows were found.
- Independent Devnet `getTransaction` RPC returned no transaction error. Seller token balance changed `20,000,000 → 20,990,000` atomic units (`+0.99 USDC`); buyer changed `61,959,991 → 60,969,991` (`−0.99 USDC`). The first account/fee payer was the Hot Wallet `FMNs7xqezz4bYYioPyfqPzxLLmZyJhjSzbGApMdnrC2Z`, and the network fee was `10,000` lamports. The `gas_subsidies` table had no row for this transaction; fee-payer attribution comes from the chain transaction itself.
- The first pre-payment probe stopped at the seller-identity guard, before BuyerConfirmation and payment. Its Runtime Episode had zero PaymentIntents and zero settlement ledger rows; after its deadline it was closed via the formal `ExpireEpisode` transition as `EXPIRED` (not by direct SQL mutation and not by fabricating a buyer rejection).
- `STABLEPAY_API_KEY` was loaded only into the local process/environment and sent as an HTTP authentication header. A post-run exact-value scan of 37 current-run logs/report files found zero occurrences; no API key or private key is in the report or this document.

## Resume-safe BOSS bullet

> **AgentPay 全链路：** 通过认证 Runtime HTTP + 确定性 Mock 验证 Merchant SKU 发现、x402 报价复核、买家签名确认、PaymentIntent/Ledger、验单与交付；50 条冻结任务预期终态正确 **50/50**、可购任务有效交付 **40/40**、报价变更重确认 **10/10**、重复结算 **0**；另以 Runtime 应用层集成 harness 对 12 家本地模拟商户之一完成 **1 笔 0.99 USDC Devnet 实付**，MySQL 与链上金额/收款人核验一致并成功交付。

The bullet deliberately labels the 50-task Mock result and the single Devnet payment as separate modes. The Devnet Merchant and SKU data are synthetic/local; do not call them a real merchant integration. The 50-task parser is deterministic and did not call an LLM.

## Still unproven / not run

- The 500-task independently judged product-retrieval/plan-quality targets (`94.2%` target recall and `72.6% → 91.8%` valid-plan rate) remain unmeasured; this 50-task matrix is not a substitute.
- No 20–30 task live-model sample was run. DeepSeek was not contacted; the local E2E runner has no Codex GPT-6 Luna inference connector.
- The Devnet smoke used a pinned SKU/capability and the Runtime application service in-process, not the Runtime HTTP server or a natural-language Shopping selection; it is one transaction, not a repeated-purchase rate.
- The Runtime Episode used `stablepay_commerce_m1_test`; Payment and Verification wrote their one legitimate test transaction to the existing local dev schemas `stablepay_payment_db` and `stablepay_verification_db`. No production service/business logic was changed; working-tree changes for this iteration are harness/test/evidence changes.
