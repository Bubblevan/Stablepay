package application

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/llm"
)

// TestS6RealProviderApplicationIntegration is opt-in because it makes a real
// provider request. The fixture persists all runtime facts through the same
// application repository boundary used by the deterministic S6 tests, then
// hands the real proposal to ExecuteRecoveryDecision and the RuntimeGuard.
func TestS6RealProviderApplicationIntegration(t *testing.T) {
	providerName := os.Getenv("LLM_PROVIDER")
	baseURL := os.Getenv("LLM_BASE_URL")
	apiKey := os.Getenv("LLM_API_KEY")
	model := os.Getenv("LLM_MODEL")
	if providerName == "" || baseURL == "" || model == "" {
		t.Skip("LLM_PROVIDER, LLM_BASE_URL, and LLM_MODEL are required for the real application integration")
	}
	client, err := llm.NewConfiguredClient(providerName, baseURL, apiKey, model, nil)
	if err != nil {
		t.Fatal(err)
	}
	fixture := makeS6SwitchFixture(t, switchBResponse)
	fixture.service.llmProvider = llm.NewLLMDecisionProvider(client, llm.WithProviderName(providerName), llm.WithModelRef(model), llm.WithProviderTTL(2*time.Minute), llm.WithProviderClock(func() time.Time { return fixture.now }))

	before, err := fixture.service.GetEpisode(context.Background(), fixture.current)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	commit, result, err := fixture.service.ExecuteRecoveryDecision(ctx, S6DecisionRequest{EpisodeID: fixture.current, Query: "merchant B constraints and the only unattempted recovery candidate"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Trace.Status != llm.TraceSuccess || result.Trace.ContextHash == "" || result.Proposal.ModelRef != model {
		t.Fatalf("real application proposal=%#v trace=%#v", result.Proposal, result.Trace)
	}
	contextHash, err := result.Context.Hash()
	if err != nil || contextHash != result.Trace.ContextHash {
		t.Fatalf("context hash=%q trace hash=%q err=%v", contextHash, result.Trace.ContextHash, err)
	}
	if !commit.Event.RuntimeVerdict.Allowed {
		t.Fatalf("runtime guard did not accept committed proposal: %#v", commit.Event.RuntimeVerdict)
	}
	if commit.Episode.Version <= before.Version || commit.Episode.State == before.State {
		t.Fatalf("real runtime transition did not occur: before=%#v after=%#v", before, commit.Episode)
	}
	persisted, err := fixture.store.GetModelDecisionTrace(context.Background(), result.Trace.TraceID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ContextHash != result.Trace.ContextHash || persisted.Status != llm.TraceSuccess || persisted.ModelRef == "" {
		t.Fatalf("persisted model trace=%#v", persisted)
	}
}
