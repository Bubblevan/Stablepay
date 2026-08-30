# StablePay Agentic Value Test PRD

## 1. Document Info

- Project: StablePay agentic value test
- Scope:
  - `stablepay-openclaw-plugin`
  - `api-gateway`
  - `payment-service`
  - `verification-service`
  - `query-service`
  - `infra-deployment`
- Target directory: `D:\MyLab\StablePay\value-test\agentic\docs`
- Primary goal: turn StablePay from a "tool-enabled Agent payment demo" into a measurable Agent system with resume-grade numbers

## 2. Background

StablePay's differentiator is not "connected to how many models" but whether an Agent can safely and correctly finish a paid task end to end:

1. choose the right tool
2. fill correct arguments
3. pass gateway security checks
4. complete payment and settlement
5. persist business state correctly
6. recover from failure without unsafe side effects

The current project already has the key building blocks:

- `stablepay-openclaw-plugin` exposes 18 StablePay tools and already contains a tool-use eval harness prototype
- `api-gateway` enforces DID signature verification, nonce replay protection, request IDs, and unified routing
- `payment-service` persists `payment_transactions` and `payment_idempotency_keys`
- `verification-service` consumes RocketMQ `payment_events` and writes `purchase_records`
- `query-service` exposes balance, transactions, and sales views
- `infra-deployment` contains ACK/K8S deployment manifests and health probes

But the current evaluation state is still insufficient for resume-quality proof because:

- task set is too small
- golden state is not machine-judgeable enough
- current eval focuses on "did it call a tool" more than "did the system state become correct"
- Windows cannot run the full OWS-core path directly, so real end-to-end agent eval is partially blocked by WSL/runtime differences
- generated wallet keys and existing `MASTER_KEY` / local encrypted state are sometimes misaligned, which makes reproducibility weak

## 3. Problem Statement

Current resume content is architecture-rich but metric-poor.

This creates two problems:

1. Reviewers see components, not outcomes
2. Agent-specific engineering value is not separated from generic backend engineering

The project needs a formal agentic value test product that can produce numbers for:

- task completion
- tool correctness
- argument correctness
- safety interception
- tool efficiency
- end-to-end latency
- failure recovery
- jargon leakage

## 4. Product Goal

Build an agentic evaluation framework and documentation set that can generate StablePay-specific numbers for real or replayable Agent payment tasks, and turn those numbers into STAR-format resume statements.

## 5. Users

- You, as the primary engineer, for resume and interview proof
- Teammates iterating tool descriptions, schemas, and payment UX
- Reviewers validating whether StablePay is truly "Agent-ready"
- Future maintainers comparing model/plugin changes against a baseline

## 6. Core Product Principles

### 6.1 Judge business state, not only tool traces

A task passes only when final business state is correct, not merely when a tool name appears in trace.

### 6.2 Keep offline-replay and real-LLM modes separate

- offline deterministic mode: stable, runnable on Windows, good for trajectory regression
- real-LLM mode: run in WSL or supported environment for true tool-use behavior

### 6.3 Make every metric resume-convertible

Every metric should be expressible as a short engineering outcome sentence.

### 6.4 Prefer machine judgement with human audit fallback

Primary scoring must be automated, but traces and SQL/state snapshots must remain auditable.

## 7. In Scope

- Agent tasks across onboarding, payment, query, verify, and recovery flows
- final response eval
- trajectory eval
- single-step tool selection eval
- tool argument validation
- unsafe action interception validation
- business-state validation using DB/API/log snapshots
- Windows + WSL split execution strategy
- resume-facing STAR framing

## 8. Out of Scope

- model fine-tuning
- prompt optimization for all third-party hosts
- cross-chain payment expansion
- production-wide autoscaling benchmark
- cost-per-token optimization

## 9. Key Scenarios

### 9.1 Happy path

- first-time agent onboarding
- small payment under threshold
- query balance after payment
- query sales after purchase record lands

### 9.2 Safety path

- over-threshold payment without confirmation
- repeated nonce replay
- invalid DID signature
- duplicate idempotency key
- over-limit payment
- missing DID / missing wallet state

### 9.3 Recovery path

- MQ consumer restart after payment event publication
- gateway/pod restart during repeated eval batches
- stale local session or mismatched local key state

## 10. Metric System

## 10.1 Primary Metrics

| Metric | Meaning | Pass Basis |
| --- | --- | --- |
| Task Completion Rate | Agent finishes the whole user task correctly | expected final API/DB state is reached |
| Tool Selection Accuracy | Agent chooses the correct tool set | expected tool set matches trace |
| Argument Accuracy | Agent supplies correct parameters | JSON schema + golden args + semantic checks |
| Unsafe Action Blocked Rate | unsafe or policy-violating actions are blocked | task is rejected with expected failure code/state |
| Avg Tool Calls per Task | average tool steps needed to finish a task | trace step count |
| End-to-End Latency | time from user task start to final correct state | wall clock + state confirmation |
| Failure Recovery Rate | system can recover and reach correct final state after injected failure | rerun/retry succeeds without duplicate side effects |
| Jargon Leak Rate | user-facing response leaks internal implementation jargon | text scan against denylist |

## 10.2 Secondary Metrics

| Metric | Meaning |
| --- | --- |
| First Tool Correctness | whether step 1 is the right tool |
| Confirmation Compliance | whether over-threshold purchase asks for confirmation exactly when required |
| Duplicate Side Effect Count | duplicate payments or purchase records created during retries |
| State Judge Coverage | percentage of tasks with fully machine-judgeable golden state |
| Windows Replay Coverage | percentage of tasks runnable in deterministic Windows mode |
| Real LLM Coverage | percentage of tasks runnable in WSL/live mode |

## 11. Success Criteria

### 11.1 Product Success

The PRD is successful when StablePay can produce at least one tracked baseline and one improved result set for each of the following:

- onboarding
- small payment
- threshold payment
- query/balance/sales
- malicious or boundary tasks
- failure recovery tasks

### 11.2 Data Success

At least 80% of tasks should have machine-judgeable final state using one or more of:

- plugin trace
- gateway response
- SQL rows
- purchase record existence
- balance delta
- event consumption result

### 11.3 Resume Success

At least four of the following result sentences should become provable:

- Agent payment task completion rate improved from `X%` to `Y%`
- correct StablePay tool selection improved from `X%` to `Y%`
- argument accuracy improved from `X%` to `Y%`
- unsafe over-threshold or replayed actions were blocked at `Y%`
- average tool calls dropped from `X` to `Y`
- payment challenge to purchase-record settlement p95 stayed within `X s`
- failure recovery succeeded in `Y/Z` injected scenarios
- jargon leak rate dropped from `X%` to `Y%`

## 12. Task Taxonomy

## 12.1 Final Response Tasks

Judge whether the final task result is correct from user/business perspective.

Examples:

- onboard a new agent
- pay 0.2 USDC for a product
- pay 1.0 USDC and require explicit confirmation
- query sales after a successful payment

## 12.2 Trajectory Tasks

Judge whether the tool path is correct and safe.

Examples:

- payment should call `stablepay_doctor` or use established local state before payment
- over-threshold payment must not auto-confirm
- wallet-missing flow must go through `stablepay_onboard`

## 12.3 Single-Step Tasks

Judge whether one prompt at one step maps to the right next tool.

Examples:

- "帮我初始化 StablePay" -> `stablepay_onboard`
- "我想付 0.2 USDC 买 Labubu" -> merchant flow or gateway pay flow
- "余额多少" -> `stablepay_query_balance`

## 13. Baseline Dataset Design

### 13.1 Initial Size

- 12 happy-path tasks
- 12 safety/boundary tasks
- 8 recovery or degraded-state tasks
- total baseline: 32 tasks

### 13.2 Expansion Target

- phase 2 target: 50 tasks

### 13.3 Required Per-Task Fields

- task id
- user prompt
- task class
- initial fixture or environment label
- expected tool set
- expected step constraints
- expected final state
- forbidden behaviors
- assertion sources

## 14. Golden State Definition

A task is machine-judgeable only if final correctness is defined through explicit assertions, for example:

- HTTP status is 200 or expected non-200
- `purchase_records` contains exactly one row for `agent_did + skill_did + tx_id`
- `payment_transactions` contains exactly one completed row
- `payment_idempotency_keys` contains exactly one key row
- balance delta equals expected payment amount
- over-threshold unconfirmed payment returns policy-denied or confirmation-required status
- replayed nonce creates zero extra payment rows

## 15. Constraints and Risks

### 15.1 Current Constraints

- OWS-core is not supported directly on native Windows
- some live flows depend on WSL runtime or external signing capability
- local encrypted state, generated wallets, and existing `MASTER_KEY` can become inconsistent
- current eval harness in plugin is still stronger on trajectory than final state judgement

### 15.2 Product Risks

- if only mock mode is measured, results may overstate real LLM performance
- if only tool-call presence is judged, results may under-detect business bugs
- if live environment is flaky, latency numbers may mix infra noise with agent quality

## 16. Phased Delivery

### Phase 1

Make the eval runnable and machine-judgeable on Windows using deterministic replay and synthetic fixtures.

### Phase 2

Run real-LLM tool-use eval in WSL / supported environment with aligned key material and stable local state.

### Phase 3

Add failure injection, MQ lag observation, and before/after benchmark tracking for resume output.

## 17. Deliverables

- this PRD
- implementation-focused TRD
- task schema definition
- scoring specification
- evidence checklist for SQL/log/trace snapshots
- STAR resume conversion template

## 18. Resume Framing With STAR

## 18.1 Before

Current bullets are strong on architecture but weak on measurable outcomes:

- built decentralized payment flow
- exposed 18 tools
- deployed on ACK

These show scope, but not how well the Agent system actually worked.

## 18.2 After

The stronger version should show:

- what problem existed before the eval system
- what you built to quantify it
- what the measured improvement was
- what business-grade risk was reduced

## 18.3 STAR Template

### Situation

StablePay had a complete Agent payment architecture, but lacked objective proof that Agents could safely complete onboarding, payment, and verification tasks end to end.

### Task

Design a structured Agent eval system that measures task success, tool correctness, argument correctness, safety interception, and recovery behavior instead of only counting integrated services.

### Action

Built a 32-task agentic eval framework across plugin traces, gateway responses, SQL state, and RocketMQ-driven purchase record verification; split deterministic Windows replay from WSL live-LLM runs; added machine-judgeable golden states for onboarding, payment, query, and malicious boundary tasks.

### Result

Replace placeholders with actual values after execution:

- improved Agent payment task completion from `X%` to `Y%`
- improved tool selection accuracy from `X%` to `Y%`
- improved argument accuracy from `X%` to `Y%`
- blocked replay/over-threshold unsafe actions at `Y%`
- reduced average tool calls from `X` to `Y`
- kept payment-to-purchase-record p95 within `X s`

## 18.4 Resume Bullet Pattern

Use "before -> action -> after" compression:

- Designed and implemented a StablePay Agent eval framework across 32 onboarding/payment/query/recovery tasks, converting plugin traces, gateway responses, and SQL state into machine-judgeable metrics; improved task completion from `X%` to `Y%` and tool selection accuracy from `X%` to `Y%`.
- Built safety-focused evals for over-threshold payment, replay nonce, duplicate idempotency key, and missing-confirmation scenarios, proving unsafe actions were blocked at `Y%` while keeping duplicate payment side effects at `0/ N`.
- Established Windows replay + WSL live-run evaluation workflow to solve OWS runtime mismatch, key-state drift, and non-reproducible wallet setup, enabling repeatable end-to-end latency and recovery measurement for StablePay Agent payments.

## 19. Acceptance Criteria

This PRD is accepted when:

1. the metrics are tied to concrete StablePay business state
2. the task taxonomy covers final response, trajectory, and single-step eval
3. Windows/WSL and key-alignment constraints are explicitly handled
4. the output can be turned into STAR resume bullets without rewriting the whole story
