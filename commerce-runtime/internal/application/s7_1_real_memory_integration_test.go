package application

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

type countingLLMClient struct {
	inner llm.LLMClient
	calls atomic.Int64
}

func (c *countingLLMClient) GenerateDecision(ctx context.Context, request llm.LLMDecisionRequest) (llm.LLMDecisionResponse, error) {
	c.calls.Add(1)
	return c.inner.GenerateDecision(ctx, request)
}

func (c *countingLLMClient) callCount() int {
	return int(c.calls.Load())
}

// This is deliberately opt-in: it sends one real neutral recovery prompt to
// the configured provider and records action/memory-ref/guard outcomes. It is
// an evaluation harness, not a learned policy or an S9 training loop.
func TestS71RealDeepSeekCandidateMemoryActionIntegration(t *testing.T) {
	providerName, baseURL, apiKey, model := os.Getenv("LLM_PROVIDER"), os.Getenv("LLM_BASE_URL"), os.Getenv("LLM_API_KEY"), os.Getenv("LLM_MODEL")
	if providerName == "" || baseURL == "" || model == "" {
		t.Skip("LLM_PROVIDER, LLM_BASE_URL, and LLM_MODEL are required for the real S7.1 integration")
	}
	client, err := llm.NewConfiguredClient(providerName, baseURL, apiKey, model, nil)
	if err != nil {
		t.Fatal(err)
	}
	countingClient := &countingLLMClient{inner: client}
	fixture := makeS6SwitchFixture(t, switchBResponse)
	seedCandidateOutcomeMemories(t, fixture)
	fixture.service.llmProvider = llm.NewLLMDecisionProvider(countingClient, llm.WithProviderName(providerName), llm.WithModelRef(model), llm.WithProviderTTL(2*time.Minute), llm.WithProviderClock(func() time.Time { return fixture.now }))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	commit, result, err := fixture.service.ExecuteRecoveryDecision(ctx, S6DecisionRequest{EpisodeID: fixture.current, Query: "which eligible recovery option has the strongest persisted outcome history?"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Trace.Status != llm.TraceSuccess || result.Proposal.ProposedAction != trace.ActionSwitchMerchant || result.Proposal.Target == nil || result.Proposal.Target.MerchantDID != "did:merchant:b" || len(result.Proposal.MemoryRefs) == 0 || !commit.Event.RuntimeVerdict.Allowed {
		t.Fatalf("real S7.1 result action=%s target=%#v memory_refs=%v guard=%v trace_status=%s", result.Proposal.ProposedAction, result.Proposal.Target, result.Proposal.MemoryRefs, commit.Event.RuntimeVerdict.Allowed, result.Trace.Status)
	}
	traces, err := fixture.store.ListMemoryUseTraces(context.Background(), fixture.current)
	if err != nil || len(traces) != 1 || !traces[0].GuardAccepted {
		t.Fatalf("memory use trace=%#v err=%v", traces, err)
	}
	if countingClient.callCount() < 1 {
		t.Fatal("real provider completed without a GenerateDecision call")
	}
	t.Logf("real S7.1 provider=%s model=%s calls=%d action=%s target=%s cited_memory_refs=%d guard_accepted=%t", providerName, model, countingClient.callCount(), result.Proposal.ProposedAction, result.Proposal.Target.MerchantDID, len(result.Proposal.MemoryRefs), traces[0].GuardAccepted)
}

func TestS71DeterministicMemoryCounterfactualRecordsActionAndGuard(t *testing.T) {
	without := makeS6SwitchFixture(t, switchBResponse)
	withoutCommit, withoutResult, err := without.service.ExecuteRecoveryDecision(context.Background(), S6DecisionRequest{EpisodeID: without.current, Query: "merchant constraints which eligible recovery option is suitable?"})
	if err != nil {
		t.Fatal(err)
	}
	with := makeS6SwitchFixture(t, switchBResponse)
	seedCandidateOutcomeMemories(t, with)
	with.client.response = switchBMemoryResponse(with.set, with.recovery, with.evidenceRef)
	withCommit, withResult, err := with.service.ExecuteRecoveryDecision(context.Background(), S6DecisionRequest{EpisodeID: with.current, Query: "merchant constraints which eligible recovery option is suitable?"})
	if err != nil {
		t.Fatal(err)
	}
	if !withoutCommit.Event.RuntimeVerdict.Allowed || !withCommit.Event.RuntimeVerdict.Allowed || withoutResult.Proposal.ProposedAction != withResult.Proposal.ProposedAction || len(withResult.Proposal.MemoryRefs) == 0 || len(withoutResult.Proposal.MemoryRefs) != 0 {
		t.Fatalf("counterfactual without=(action=%s refs=%v guard=%v) with=(action=%s refs=%v guard=%v)", withoutResult.Proposal.ProposedAction, withoutResult.Proposal.MemoryRefs, withoutCommit.Event.RuntimeVerdict.Allowed, withResult.Proposal.ProposedAction, withResult.Proposal.MemoryRefs, withCommit.Event.RuntimeVerdict.Allowed)
	}
	t.Logf("counterfactual without_memory action=%s cited_refs=0 guard_accepted=%t; with_memory action=%s cited_refs=%d guard_accepted=%t", withoutResult.Proposal.ProposedAction, withoutCommit.Event.RuntimeVerdict.Allowed, withResult.Proposal.ProposedAction, len(withResult.Proposal.MemoryRefs), withCommit.Event.RuntimeVerdict.Allowed)
}
