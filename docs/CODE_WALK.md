# StablePay Source-Code Walk Map

This is the interview route from the production entry point to the authority boundaries. Paths are relative to the repository root and symbols are verified against the frozen tree.

## 1. Start at the production composition root

```text
commerce-runtime/cmd/server/main.go:main
  -> internal/composition.NewProduction
  -> internal/api.NewServerWithWorkflow
  -> http.Server
```

Read `internal/composition/composition.go` next. `NewProduction` wires MySQL, Catalog, Evidence, Memory, the OpenAI-compatible/DeepSeek configuration, Merchant adapter, DID policy, Kitex Payment adapter, Entitlement, Validator, `application.Service`, Episode Runner, and Workflow Manager. `Composition.Ready` is the readiness boundary; it checks composition, supervisors, and MySQL.

## 2. Follow an external request

```text
POST /v1/episodes
  -> internal/api.Server.ServeHTTP
  -> internal/api.Server.createEpisode
  -> application.Service.CreateEpisode
  -> runtime.Runner.Enqueue / RunEpisode
```

Then inspect:

- `internal/api.Server.authorize`: bearer/API-key boundary.
- `internal/api.Server.status`: public status plus parent approval/decision.
- `internal/api.Server.observability`: redacted Episode trace export.
- `internal/api.Server.parentDecision`: durable parent decision path.
- `cmd/stablepay-runtime`: CLI `acquire`, `status`, `approve`, and workflow commands.
- `cmd/external-agent-e2e`: external-client polling and artifact collection.

## 3. Follow MCP without losing the authority boundary

```text
POST /mcp
  -> internal/api.Server.serveMCP
  -> internal/api.Server.mcpCall
  -> HTTP/application path
```

`mcpTools` registers `stablepay.acquire`, `stablepay.status`, and `stablepay.approve`. `mcpCall` validates the request and delegates to the same application and Runner paths as HTTP. It does not call payment adapters or recovery providers directly. This is the code pointer for explaining “MCP is ingress, not a second Runtime.”

## 4. Episode Runner and resume path

```text
runtime.Runner.StartSupervisor
  -> ResumePersisted
  -> ResumeEpisode / RunEpisode
  -> runtime.Runner.run
  -> runtime.Runner.step
  -> persisted transition + event
```

Read `internal/runtime/runner.go` in this order:

1. `StartSupervisor`, `supervisorLoop`, and `ResumePersisted` for restart scanning.
2. `RunEpisode` and `ResumeEpisode`, which intentionally share `run`.
3. `beginExecution`, `recordExecutionError`, `finishExecution` for operational projection.
4. `step` for discovery, invocation, negotiation, payment, delivery, validation, and recovery dispatch.

Business authority remains the persisted `CommerceEpisode` and its events; `EpisodeExecutionStatus` is an operational guard against duplicate workers and an observability surface.

## 5. Recovery path

```text
VALIDATING_DELIVERY
  -> RECOVERING
  -> application.Service.BuildDecisionContext
  -> DecisionProvider.Propose
  -> RuntimeGuard.ValidateRecoveryProposalWithMemory
  -> application.Service.CommitProposal / recovery handler
  -> repository transition + trace
```

Exact sources:

- `internal/application/s6_runtime.go`: `BuildDecisionContext`, `ExecuteRecoveryDecision`, proposal commit and MemoryUseTrace recording.
- `internal/llm/context.go`: `DecisionContext.Validate`.
- `internal/llm/provider.go`: `DecisionProvider`, `ModelDecisionTrace`.
- `internal/decision/decision.go`: `RuntimeGuard.Evaluate`, `ValidateRecoveryProposal`, `ValidateRecoveryProposalWithMemory`.
- `internal/application/service.go`: `CommitProposal`, `CommitRuntimeAction`.
- `internal/application/recovery_runtime.go`: `RequestParentConfirmation`, `RecordParentDecision`.

## 6. Payment path

```text
negotiating
  -> ReservePaymentIntent
  -> AuthorizeAndSubmitPayment
  -> ReconcilePayment when outcome is pending/unknown
  -> entitlement / delivery validation
```

Read:

- `internal/application/payment_runtime.go`: `ReservePaymentIntent`, `AuthorizeAndSubmitPayment`, `ReconcilePayment`, `ResolveSelectedPayeeDID`.
- `internal/payment/intent.go`: `PaymentIntent` identity and status model.
- `internal/ledger/ledger.go`: budget projection and ledger invariants.
- `internal/reconciliation/reconciliation.go`: external status resolution.
- `internal/infrastructure/mysql/repository.go`: `CommitFinanceTransition`, payment-intent persistence, and CAS updates.

Then contrast the lower payment plane:

- `payment-service/internal/application/service/payment_service.go`: payment-service authority.
- `payment-service/internal/application/service/idempotency_test.go`: lower-plane idempotency tests.
- `blockchain-adapter/internal/application/service/submit_tx_service.go` and `tx_status_query_service.go`: chain submission/query boundary.
- `verification-service/internal/application/service.go`: entitlement/purchase verification boundary.

## 7. Memory path

```text
Episode events + payment/delivery/validation facts
  -> memory.Projector.ProjectEpisode
  -> scoped MemoryRecord / MemoryObservation
  -> RetrievalPolicy / DecisionContext
  -> MemoryUseTrace
```

Exact sources:

- `internal/memory/projector.go`: `NewProjector`, `ProjectEpisode`.
- `internal/memory/use_trace.go`: `MemoryUseTrace`, `Validate`, payload hash.
- `internal/repository/s7_memory_inmemory.go`: `Retrieve`, `SearchMemories`, use-trace storage.
- `internal/application/s6_runtime.go`: memory retrieval and use-trace persistence around decision execution.

The sentence to say in an interview is: “Memory can shape a proposal context; it cannot authorize a payment.”

## 8. Workflow and artifact path

```text
WorkflowDefinition
  -> workflowruntime.Manager.CreateRun
  -> Manager.RunWorkflow / ResumeWorkflow
  -> Manager.run
  -> child AcquireCapabilityRequest / child Episode
  -> completeStepFromArtifact
  -> workflow/artifact.Resolver.Resolve
  -> next child or fulfilled parent artifact
```

Read:

- `internal/workflow/types.go`: `WorkflowDefinition.Validate` and static step model.
- `internal/workflowruntime/runner.go`: `CreateRun`, `run`, `buildChildRequest`, `completeStepFromArtifact`, `GetFinalArtifact`, `ResumePersisted`.
- `internal/workflow/artifact/resolver.go`: `WorkflowArtifactResolver`, `Resolve`, `ResolveInput`.
- `internal/api/workflow.go`: public workflow ingress/status/artifact endpoints.

## 9. Eval and evidence path

```text
public HTTP/MCP/CLI client
  -> eval.Client
  -> public status/events/observability
  -> EpisodeResult JSONL
  -> GradeEpisode
  -> observability.Compute
  -> RenderMarkdown / metrics.json
```

Read:

- `internal/eval/client.go`: external submission, polling, and redacted result collection.
- `internal/eval/scenario.go`: task/fault metadata and expected outcomes.
- `internal/observability/collect.go`: trace assembly.
- `internal/observability/grader.go`: assertion-level grading.
- `internal/observability/integrity.go`: numerator/denominator, fault semantics, pass^k, and latency computation.
- `internal/observability/report.go`: Markdown export.
- `cmd/stablepay-agent-eval/main.go`: `run`, `report`, `export`, `check`, and `plans` commands.
- `cmd/s11-live-local-suite/main.go`: isolated public-client suite.

## 10. What not to claim from this walk

This walk proves source structure and points to tests. It does not by itself prove production scale, real DeepSeek usage, real Devnet usage, or zero failures. Those claims require the corresponding `REAL_LLM`, `REAL_DEVNET`, or recorded live artifact.
