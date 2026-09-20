package llm

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

func TestRealLLMNetworkProvider(t *testing.T) {
	providerName := os.Getenv("LLM_PROVIDER")
	baseURL := os.Getenv("LLM_BASE_URL")
	apiKey := os.Getenv("LLM_API_KEY")
	model := os.Getenv("LLM_MODEL")
	if providerName == "" || baseURL == "" || model == "" {
		t.Skip("LLM_PROVIDER, LLM_BASE_URL, and LLM_MODEL are required for the real-provider integration test")
	}
	client, err := NewConfiguredClient(providerName, baseURL, apiKey, model, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	input, err := BuildDecisionContext(ContextInput{Episode: &episode.CommerceEpisode{EpisodeID: "real-llm-integration", RequestID: "real-llm-integration", State: episode.StateRecovering, DeadlineAt: now.Add(10 * time.Minute), Version: 2, Budget: episode.BudgetSnapshot{Currency: "USDC", BudgetLimitMinor: 1000, AvailableBudget: 1000, RefundReusable: true}}, AllowedActions: []trace.ActionType{trace.ActionStop, trace.ActionAskParent}, Recovery: &recovery.RecoveryContext{RecoveryID: "recovery:real-llm-integration", EpisodeID: "real-llm-integration", ReasonCode: recovery.ReasonBudgetInsufficient, FactsRef: "recovery://real-llm-integration", PayloadHash: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}})
	if err != nil {
		t.Fatal(err)
	}
	provider := NewLLMDecisionProvider(client, WithProviderName(providerName), WithModelRef(model), WithProviderTTL(2*time.Minute))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	result, err := provider.ProposeWithTrace(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Trace.Status != TraceSuccess || result.Proposal.EpisodeID != input.Episode.EpisodeID {
		t.Fatalf("trace=%#v proposal=%#v", result.Trace, result.Proposal)
	}
}
