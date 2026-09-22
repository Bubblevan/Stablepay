# StablePay Engineering Stories

Each story is grounded in the frozen implementation and follows the same interview structure: Problem, Failure mode, Naive solution, Why naive fails, Final design, Invariant, Evidence, Trade-off.

## 1. Payment dual-write crash window

### Problem

A purchase needs a local economic record and an external payment call. The process can stop after one side has advanced and before the caller receives the result.

### Failure mode

The local Episode may be in `PAYING` or `PAYMENT_SUBMITTING` while the network outcome is unknown. Retrying blindly can create a second economic attempt or double settlement.

### Naive solution

Call the payment service first, then write a PaymentIntent; or create a fresh intent on every retry.

### Why naive fails

The two systems have no atomic transaction. A lost response is not proof that the external operation did not happen.

### Final design

`ReservePaymentIntent` first persists the budget reservation, ledger entry, Episode transition, and runtime-owned `PaymentIntent` through `CommitFinanceTransition`. `AuthorizeAndSubmitPayment` moves the intent through authorized/submitting states and records a transport trace. `ReconcilePayment` queries the persisted intent and external status before allowing another action.

### Invariant

The same idempotency key, economic identity, request fingerprint, and PaymentIntent identity are reused for a replay; terminal intent states are returned rather than resubmitted.

### Evidence

`commerce-runtime/internal/application/payment_runtime.go`; `internal/infrastructure/mysql/repository.go`; MySQL repository integration tests; live-local `duplicate_settlement_count=0` across evaluated trials.

### Trade-off

An unknown payment can remain pending until reconciliation. This is safer than guessing, but it makes caller-visible completion less immediate.

## 2. Cross-merchant recovery after sunk payment

### Problem

A Merchant can accept payment and then return invalid delivery. Recovery may need to retry or switch to an eligible Merchant without paying for the same economic attempt twice.

### Failure mode

If recovery chooses a target from free-form model text, it can select an unregistered Merchant, stale catalog version, wrong payee, or a previously attempted target.

### Naive solution

Ask the LLM for another Merchant and execute its string directly.

### Why naive fails

The model does not own the catalog snapshot, payment binding, attempt budget, or sunk-cost ledger. A plausible name is not evidence of eligibility.

### Final design

`BuildDecisionContext` supplies bounded observations and candidate facts. `ExecuteRecoveryDecision` obtains a proposal, and `RuntimeGuard.ValidateRecoveryProposalWithMemory` validates state, action, target, evidence, budget, and memory refs. `ResolveSelectedPayeeDID` reads the selected payee from the immutable CandidateSet. The application layer then commits a retry/switch using its own payment and recovery invariants.

### Invariant

Only an eligible candidate with matching catalog provenance and payment binding can become the next controlled action; a recovery proposal cannot mint a new authority.

### Evidence

`internal/application/s6_runtime.go`; `internal/decision/decision.go`; `internal/application/payment_runtime.go`; prior U2 script/report for the real-provider path; S6/S7 guard and recovery tests.

### Trade-off

Recovery is less flexible than a free-form planner and can stop or ask the parent when evidence or budget is insufficient.

## 3. LLM proposal versus deterministic authority

### Problem

LLMs are useful for choosing a bounded recovery action but are not reliable transaction coordinators.

### Failure mode

A malformed response, hallucinated target, stale evidence ref, or timeout could otherwise mutate payment or Merchant state.

### Naive solution

Let the provider return an action and call the corresponding application method directly.

### Why naive fails

It collapses decision and execution authority. The model can bypass current state, retry budgets, catalog provenance, and side-effect checks.

### Final design

`DecisionProvider.Propose` returns a `DecisionProposal`. `RuntimeGuard` evaluates the proposal, while `CommitProposal` and recovery handlers perform the deterministic commit. The system persists `ModelDecisionTrace` and `DecisionOutcomeTrace`, including guard verdict and side-effect snapshots.

### Invariant

`LLM != transaction authority`: no rejected proposal can commit a protected side effect.

### Evidence

`internal/decision/decision.go`; `internal/application/service.go`; `internal/application/s6_runtime.go`; `internal/llm/provider.go`; `internal/observability/grader.go`; live-local `guard-rejected` group with observed unsafe side effects `0`.

### Trade-off

The boundary adds trace and validation work and can reject a useful-but-under-specified proposal. That rejection is preferable to an ungrounded payment action.

## 4. Persistent memory provenance and contamination control

### Problem

Past Merchant outcomes can help recovery, but a memory item can be stale, scoped to another requester, or derived from a different catalog snapshot.

### Failure mode

Unscoped retrieval turns historical context into an implicit authorization or causes a proposal to cite a fact that does not belong to the current Episode.

### Naive solution

Store a text summary and inject the top result into every model prompt.

### Why naive fails

Text similarity is not transaction provenance. It cannot prove requester, parent session, Merchant, capability, catalog version, or evidence lineage.

### Final design

`Projector.ProjectEpisode` derives scoped memory records from events, payment intents, deliveries, and validation evidence. Retrieval filters by scope and provenance. `MemoryUseTrace` records retrieved refs, cited refs, context hash, proposed action, and guard acceptance. RuntimeGuard still owns authority.

### Invariant

Memory is advisory and provenance-scoped; it cannot authorize payment, change a quote, or bypass the current state machine.

### Evidence

`internal/memory/projector.go`; `internal/memory/use_trace.go`; `internal/repository/s7_memory_inmemory.go`; S7 boundary and MySQL tests; live-local memory-hit/memory-miss task groups.

### Trade-off

Strict scope reduces recall and sometimes produces a memory miss. The system prefers an explicit miss over cross-merchant or cross-session contamination.

## 5. Workflow artifact data plane

### Problem

A parent workflow needs to pass a child Episode’s result to the next step without copying untrusted bodies or losing provenance.

### Failure mode

Passing raw response bodies in workflow state can exceed limits, leak data, or allow a hash/type mismatch to become the next child’s input.

### Naive solution

Put the full artifact body in the workflow event and let the next step trust it.

### Why naive fails

Workflow state becomes an unbounded data plane and the parent cannot prove that the body came from the fulfilled child step.

### Final design

`WorkflowDefinition` validates a static sequential graph. `Manager.run` creates child Episodes. `completeStepFromArtifact` records a bounded artifact reference, content type, hash, and child provenance. `WorkflowArtifactResolver.Resolve` checks step fulfillment, stored artifact identity, expected hash/type, size, and provenance before returning bytes.

### Invariant

An artifact is consumable only when its reference, hash, content type, size, and child-step provenance all match the persisted workflow projection.

### Evidence

`internal/workflow/types.go`; `internal/workflowruntime/runner.go`; `internal/workflow/artifact/resolver.go`; workflow local, HTTP Merchant, restart, and artifact-boundary tests.

### Trade-off

Workflow v1 is intentionally sequential, single-sink, and static. It gives up planner flexibility and parallelism for a smaller, auditable data plane.

## 6. Why 85.71% was not a safe headline metric

### Problem

The first glance at the live-local output gives `48/56`, but the suite intentionally includes a `guard-rejected` safety-negative task group.

### Failure mode

Treating every row as a normal fulfillment task makes a correct rejection look like a failed business task, while still hiding the important side-effect assertion.

### Naive solution

Publish `task_success_rate=85.71%` without task taxonomy, mode, or numerator/denominator context.

### Why naive fails

It mixes functional fulfillment with adversarial safety behavior and encourages a reader to infer production reliability from deterministic local adapters.

### Final design

`GradeEpisode` preserves assertion-level results. `observability.Compute` records numerator/denominator, fault-trigger semantics, business versus operational recovery, and pass^k groupings. The resume evidence script reads the raw JSONL, classifies the explicit `guard-rejected` fixture group separately, and fails when evidence files are missing.

### Invariant

Every headline number names its trial set, mode, environment, numerator, denominator, and limitation.

### Evidence

`internal/observability/grader.go`; `internal/observability/integrity.go`; `internal/observability/report.go`; `scripts/summarize-resume-evidence.py`; `.local-run/s11-live-local/{episode_results,grade_results,metrics,report}`.

### Trade-off

The resume wording is less flashy than a single percentage, but it remains reproducible and interview-defensible.
