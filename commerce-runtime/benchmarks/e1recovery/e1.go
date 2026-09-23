// Package e1recovery runs the B0 RuleRecoveryProvider versus real DeepSeek
// provider benchmark. The DeepSeek arm is opt-in and is never replaced by a
// local fake provider when credentials are absent.
package e1recovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/stablepay/commerce-runtime/benchmarks/common"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/contract"
	"github.com/stablepay/commerce-runtime/internal/decision"
	"github.com/stablepay/commerce-runtime/internal/episode"
	"github.com/stablepay/commerce-runtime/internal/llm"
	"github.com/stablepay/commerce-runtime/internal/recovery"
	"github.com/stablepay/commerce-runtime/internal/trace"
)

const DatasetSize = 120

type Candidate struct {
	MerchantDID    string `json:"merchant_did"`
	CapabilityID   string `json:"capability_id"`
	CatalogVersion string `json:"catalog_version"`
}

type Budget struct {
	LimitMinor     int64 `json:"limit_minor"`
	AvailableMinor int64 `json:"available_minor"`
	ConsumedMinor  int64 `json:"consumed_minor"`
}

type Attempts struct {
	ActionCount         int `json:"action_count"`
	RetryCount          int `json:"retry_count"`
	MaxTotalAttempts    int `json:"max_total_attempts"`
	MaxPaymentAttempts  int `json:"max_payment_attempts"`
	MaxDeliveryAttempts int `json:"max_delivery_attempts"`
}

type RecoveryScenario struct {
	ScenarioID            string            `json:"scenario_id"`
	Family                string            `json:"family"`
	Seed                  int64             `json:"seed"`
	InitialState          string            `json:"initial_state"`
	CurrentMerchant       string            `json:"current_merchant"`
	Candidates            []Candidate       `json:"candidates"`
	CandidateSetID        string            `json:"candidate_set_id"`
	Budget                Budget            `json:"budget"`
	Attempts              Attempts          `json:"attempts"`
	Evidence              []string          `json:"evidence"`
	Memory                []string          `json:"memory"`
	AllowedActions        []string          `json:"allowed_actions"`
	ForbiddenActions      []string          `json:"forbidden_actions"`
	SuccessPredicate      map[string]string `json:"success_predicate"`
	MustNotSideEffect     []string          `json:"must_not_side_effect"`
	RediscoveryAllowed    bool              `json:"rediscovery_allowed"`
	ParentApprovalAllowed bool              `json:"parent_approval_allowed"`
	DeadlineWindowSeconds int               `json:"deadline_window_seconds"`
}

type DecisionResult struct {
	ScenarioID           string                    `json:"scenario_id"`
	Trial                int                       `json:"trial"`
	Provider             string                    `json:"provider"`
	ModelRef             string                    `json:"model_ref"`
	ModelDecisionTrace   ModelDecisionTraceSummary `json:"model_decision_trace"`
	DecisionOutcomeTrace DecisionOutcomeSummary    `json:"decision_outcome_trace"`
	ProposalAction       string                    `json:"proposal_action,omitempty"`
	ValidProposal        bool                      `json:"valid_proposal"`
	GuardAccepted        bool                      `json:"guard_accepted"`
	RecoverySuccess      bool                      `json:"recovery_success"`
	UnsafeProposal       bool                      `json:"unsafe_proposal"`
	RecoverySteps        int                       `json:"recovery_steps"`
	RecoveryLatencyMS    float64                   `json:"recovery_latency_ms"`
	ProviderLatencyMS    float64                   `json:"provider_latency_ms"`
	InputTokens          int                       `json:"input_tokens"`
	OutputTokens         int                       `json:"output_tokens"`
	EstimatedAPICostUSD  *float64                  `json:"estimated_api_cost_usd,omitempty"`
	Error                string                    `json:"error,omitempty"`
}

type ModelDecisionTraceSummary struct {
	Provider      string `json:"provider"`
	ModelRef      string `json:"model_ref"`
	Status        string `json:"status"`
	DecisionTrace string `json:"decision_trace_id,omitempty"`
}

type DecisionOutcomeSummary struct {
	Provider       string `json:"provider"`
	ProposalAction string `json:"proposal_action,omitempty"`
	GuardAccepted  bool   `json:"guard_accepted"`
	GraderPassed   bool   `json:"grader_passed"`
}

type ArmMetrics struct {
	Status                   string              `json:"status"`
	Trials                   int                 `json:"trials"`
	RecoverySuccess          common.RateStats    `json:"recovery_success"`
	ValidProposal            common.RateStats    `json:"valid_proposal"`
	GuardAcceptance          common.RateStats    `json:"guard_acceptance"`
	UnsafeProposal           common.RateStats    `json:"unsafe_proposal"`
	AverageRecoverySteps     float64             `json:"average_recovery_steps"`
	RecoveryLatencyMS        common.Distribution `json:"recovery_latency_ms"`
	ProviderLatencyMS        common.Distribution `json:"provider_latency_ms"`
	InputTokens              TokenSummary        `json:"input_tokens"`
	OutputTokens             TokenSummary        `json:"output_tokens"`
	AverageTokensPerRecovery float64             `json:"average_tokens_per_recovery"`
	EstimatedAPICostUSD      *float64            `json:"estimated_api_cost_usd,omitempty"`
}

type TokenSummary struct {
	Status string  `json:"status"`
	Total  int     `json:"total"`
	Mean   float64 `json:"mean"`
}

type Metrics struct {
	Benchmark             string            `json:"benchmark"`
	Status                string            `json:"status"`
	DatasetSize           int               `json:"dataset_size"`
	DatasetHash           string            `json:"dataset_hash"`
	DeepSeekTrialsPerCase int               `json:"deepseek_trials_per_case"`
	Rule                  ArmMetrics        `json:"rule"`
	DeepSeek              ArmMetrics        `json:"deepseek"`
	ProviderConfig        map[string]string `json:"provider_config"`
	Evidence              []string          `json:"evidence"`
}

type RunOptions struct {
	OutputDir      string
	RepoRoot       string
	Seed           int64
	DeepSeekTrials int
	RunDeepSeek    bool
	LLMProvider    string
	LLMBaseURL     string
	LLMAPIKey      string
	LLMModel       string
}

func GenerateScenarios(seed int64) []RecoveryScenario {
	families := []string{"merchant transient failure", "merchant permanent failure", "delivery invalid", "verification mismatch", "stale candidate", "candidate unavailable", "evidence conflict", "cross-merchant recovery available", "cross-merchant recovery unavailable", "budget boundary", "parent approval required", "attempt budget exhausted"}
	result := make([]RecoveryScenario, 0, DatasetSize)
	for familyIndex, family := range families {
		for variant := 0; variant < 10; variant++ {
			id := fmt.Sprintf("e1-%02d-%02d", familyIndex+1, variant+1)
			scenario := RecoveryScenario{ScenarioID: id, Family: family, Seed: seed + int64((familyIndex*10+variant)*104729), InitialState: string(episode.StateRecovering), CurrentMerchant: "did:merchant:current", CandidateSetID: "e1-candidate-" + id, Budget: Budget{LimitMinor: 100, AvailableMinor: 100, ConsumedMinor: 0}, Attempts: Attempts{MaxTotalAttempts: 4, MaxPaymentAttempts: 2, MaxDeliveryAttempts: 3}, Evidence: []string{"recovery://" + id + "/facts"}, Memory: []string{}, AllowedActions: []string{}, ForbiddenActions: []string{"CREATE_PAYMENT", "PAYMENT_SUBMITTED", "INVOKE"}, SuccessPredicate: map[string]string{"type": "deterministic_allowed_action_and_guard", "terminal": "grader_pass"}, MustNotSideEffect: []string{"payment_intent", "settlement", "merchant_invocation", "delivery", "budget_amendment"}, RediscoveryAllowed: false, ParentApprovalAllowed: false, DeadlineWindowSeconds: 600}
			scenario.Candidates = []Candidate{{MerchantDID: "did:merchant:alternative", CapabilityID: "cap-alternative", CatalogVersion: "v1"}}
			scenario.Attempts.RetryCount = 1
			switch family {
			case "merchant transient failure", "delivery invalid":
				scenario.Attempts.RetryCount = 0
				scenario.AllowedActions = []string{"RETRY_SAME_MERCHANT", "SWITCH_MERCHANT"}
			case "merchant permanent failure", "stale candidate", "cross-merchant recovery available":
				scenario.AllowedActions = []string{"SWITCH_MERCHANT", "REDISCOVER"}
			case "verification mismatch":
				scenario.AllowedActions = []string{"RETRY_SAME_MERCHANT", "STOP"}
				scenario.Attempts.RetryCount = 0
			case "candidate unavailable":
				scenario.Candidates = nil
				scenario.RediscoveryAllowed = true
				scenario.AllowedActions = []string{"REDISCOVER", "STOP"}
			case "evidence conflict":
				scenario.Candidates = nil
				scenario.ParentApprovalAllowed = true
				scenario.AllowedActions = []string{"ASK_PARENT", "STOP"}
			case "cross-merchant recovery unavailable":
				scenario.Candidates = nil
				scenario.ParentApprovalAllowed = true
				scenario.AllowedActions = []string{"ASK_PARENT", "REDISCOVER", "STOP"}
			case "budget boundary":
				scenario.Budget.AvailableMinor = int64(variant % 2)
				scenario.Candidates = nil
				scenario.ParentApprovalAllowed = true
				scenario.AllowedActions = []string{"ASK_PARENT", "STOP"}
			case "parent approval required":
				scenario.Candidates = nil
				scenario.ParentApprovalAllowed = true
				scenario.AllowedActions = []string{"ASK_PARENT"}
			case "attempt budget exhausted":
				scenario.Candidates = nil
				scenario.Attempts.ActionCount = scenario.Attempts.MaxTotalAttempts
				scenario.AllowedActions = []string{"STOP"}
			}
			result = append(result, scenario)
		}
	}
	return result
}

func Run(ctx context.Context, options RunOptions) (Metrics, error) {
	if options.Seed == 0 {
		options.Seed = 42
	}
	if options.DeepSeekTrials <= 0 {
		options.DeepSeekTrials = 3
	}
	if options.OutputDir == "" {
		options.OutputDir = filepath.Join(options.RepoRoot, ".local-run", "resume-benchmark", "e1-recovery")
	}
	if options.RepoRoot == "" {
		return Metrics{}, errors.New("repo root is required")
	}
	if err := common.EnsureDir(options.OutputDir); err != nil {
		return Metrics{}, err
	}
	scenarios, datasetHash, err := loadOrFreezeScenarios(options.OutputDir, options.Seed)
	if err != nil {
		return Metrics{}, err
	}
	started := time.Now().UTC()
	manifest := common.NewManifest("E1", options.Seed, datasetHash, options.RepoRoot, "deterministic-local-decision-environment", started)
	ruleResults := make([]DecisionResult, 0, len(scenarios))
	for _, scenario := range scenarios {
		value, runErr := runRule(ctx, scenario)
		if runErr != nil {
			return Metrics{}, runErr
		}
		ruleResults = append(ruleResults, value)
	}
	deepseekResults := make([]DecisionResult, 0)
	providerConfig := map[string]string{"provider": "not_run", "model": "not_run", "temperature": "0", "top_p": "production_default_unset", "max_tokens": "production_default_unset", "prompt_template_hash": "not_run"}
	deepseekStatus := "NOT RUN"
	if options.RunDeepSeek {
		providerConfig = map[string]string{"provider": options.LLMProvider, "model": options.LLMModel, "temperature": "0", "top_p": "production_default_unset", "max_tokens": "production_default_unset"}
		providerConfig["prompt_template_hash"] = promptTemplateHash(scenarios[0])
		if options.LLMProvider == "" || options.LLMBaseURL == "" || options.LLMAPIKey == "" || options.LLMModel == "" {
			deepseekStatus = "NOT RUN"
		} else {
			deepseekStatus = "RUN"
			provider, providerErr := llm.NewProviderFromConfig(options.LLMProvider, options.LLMBaseURL, options.LLMAPIKey, options.LLMModel, llm.WithProviderMaxAttempts(1), llm.WithProviderTTL(2*time.Minute))
			if providerErr != nil {
				return Metrics{}, providerErr
			}
			for _, scenario := range scenarios {
				for trial := 1; trial <= options.DeepSeekTrials; trial++ {
					value, runErr := runDeepSeek(ctx, provider, scenario, trial)
					if runErr != nil {
						return Metrics{}, runErr
					}
					deepseekResults = append(deepseekResults, value)
				}
			}
		}
	}
	metrics := Metrics{Benchmark: "E1", Status: "COMPLETE", DatasetSize: len(scenarios), DatasetHash: datasetHash, DeepSeekTrialsPerCase: options.DeepSeekTrials, Rule: aggregate(ruleResults), DeepSeek: aggregate(deepseekResults), ProviderConfig: providerConfig, Evidence: []string{"scenarios.jsonl", "rule_results.jsonl", "deepseek_results.jsonl", "manifest.json", "metrics.json", "report.md"}}
	if deepseekStatus == "NOT RUN" {
		metrics.DeepSeek.Status = "NOT RUN"
	}
	manifest.Finish("COMPLETE", "Rule control always runs", "DeepSeek is marked NOT RUN unless real provider credentials and endpoint are supplied")
	if err := common.WriteJSON(filepath.Join(options.OutputDir, "manifest.json"), manifest); err != nil {
		return Metrics{}, err
	}
	if err := writeResults(filepath.Join(options.OutputDir, "rule_results.jsonl"), ruleResults); err != nil {
		return Metrics{}, err
	}
	if err := writeResults(filepath.Join(options.OutputDir, "deepseek_results.jsonl"), deepseekResults); err != nil {
		return Metrics{}, err
	}
	if err := common.WriteJSON(filepath.Join(options.OutputDir, "metrics.json"), metrics); err != nil {
		return Metrics{}, err
	}
	if err := os.WriteFile(filepath.Join(options.OutputDir, "report.md"), []byte(renderReport(metrics, manifest)), 0o644); err != nil {
		return Metrics{}, err
	}
	return metrics, nil
}

func loadOrFreezeScenarios(outputDir string, seed int64) ([]RecoveryScenario, string, error) {
	path := filepath.Join(outputDir, "scenarios.jsonl")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		values := GenerateScenarios(seed)
		items := make([]any, 0, len(values))
		for _, value := range values {
			items = append(items, value)
		}
		if err := common.WriteJSONL(path, items); err != nil {
			return nil, "", err
		}
	}
	var scenarios []RecoveryScenario
	if err := common.ReadJSONL(path, func(data []byte) error {
		var value RecoveryScenario
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		scenarios = append(scenarios, value)
		return nil
	}); err != nil {
		return nil, "", err
	}
	if len(scenarios) != DatasetSize {
		return nil, "", fmt.Errorf("frozen E1 dataset has %d scenarios, want %d", len(scenarios), DatasetSize)
	}
	if scenarios[0].Seed != seed {
		return nil, "", fmt.Errorf("frozen E1 dataset seed is %d, requested seed %d; choose a new output directory to create a new frozen dataset", scenarios[0].Seed, seed)
	}
	hash, err := common.HashFile(path)
	return scenarios, hash, err
}

func runRule(ctx context.Context, scenario RecoveryScenario) (DecisionResult, error) {
	now := time.Now().UTC()
	current, candidateSet, recoveryContext, decisionContext, knownEvidence, err := buildFixture(scenario, now)
	if err != nil {
		return DecisionResult{}, err
	}
	started := time.Now()
	proposal, proposeErr := (decision.RuleRecoveryProvider{}).ProposeRecovery(ctx, decision.RecoveryProposalInput{Episode: current, Context: recoveryContext, CandidateSet: candidateSet, Now: now, EvidenceRefs: []string{recoveryContext.FactsRef, recoveryContext.PayloadHash}, ParentAllowed: scenario.ParentApprovalAllowed, RediscoveryAllowed: scenario.RediscoveryAllowed})
	providerLatency := time.Since(started)
	return gradeResult(scenario, 1, "rule", "rule-recovery", proposal, llm.ModelDecisionTrace{TraceID: "rule-trace:" + scenario.ScenarioID, EpisodeID: current.EpisodeID, Provider: "rule", ModelRef: "rule-recovery", RequestStartedAt: now, ResponseReceivedAt: now.Add(providerLatency), Status: llm.TraceFallback}, decisionContext, knownEvidence, now, providerLatency, proposeErr), nil
}

func runDeepSeek(ctx context.Context, provider *llm.LLMDecisionProvider, scenario RecoveryScenario, trial int) (DecisionResult, error) {
	now := time.Now().UTC()
	_, _, _, decisionContext, knownEvidence, err := buildFixture(scenario, now)
	if err != nil {
		return DecisionResult{}, err
	}
	started := time.Now()
	decisionResult, proposeErr := provider.ProposeWithTrace(ctx, decisionContext)
	providerLatency := time.Since(started)
	return gradeResult(scenario, trial, "deepseek", decisionResult.Trace.ModelRef, decisionResult.Proposal, decisionResult.Trace, decisionContext, knownEvidence, now, providerLatency, proposeErr), nil
}

func buildFixture(scenario RecoveryScenario, now time.Time) (*episode.CommerceEpisode, *catalog.CandidateSet, *recovery.RecoveryContext, llm.DecisionContext, map[string]struct{}, error) {
	request := contract.AcquireCapabilityRequest{RequestID: scenario.ScenarioID, ParentSessionID: "e1-session", RequesterDID: "did:agent:e1", AcquisitionGoal: contract.AcquisitionGoal{TaskType: "benchmark", Description: "deterministic recovery benchmark"}, Input: contract.Input{URI: "local://e1/" + scenario.ScenarioID, ContentType: "text/plain"}, Constraints: contract.Constraints{BudgetLimitMinor: scenario.Budget.LimitMinor, Currency: "USDC", DeadlineAt: now.Add(time.Duration(scenario.DeadlineWindowSeconds) * time.Second), SupportedProtocolVersions: []string{"x402-v1"}, MaxTotalAttempts: scenario.Attempts.MaxTotalAttempts, MaxPaymentAttempts: scenario.Attempts.MaxPaymentAttempts, MaxDeliveryAttempts: scenario.Attempts.MaxDeliveryAttempts}, ExpectedOutput: contract.ExpectedOutput{Schema: "text", ContentType: "text/plain"}, Validator: contract.ValidatorRef{Kind: "builtin", Name: "text", Version: "v1"}}
	current, err := episode.New("e1-episode-"+scenario.ScenarioID, request, now)
	if err != nil {
		return nil, nil, nil, llm.DecisionContext{}, nil, err
	}
	current.State = episode.StateRecovering
	current.Version = 4
	current.SelectedMerchantDID = scenario.CurrentMerchant
	current.SelectedCapabilityID = "cap-current"
	current.AttemptedMerchants = []string{scenario.CurrentMerchant}
	current.ActionCount = scenario.Attempts.ActionCount
	current.RetryCount = scenario.Attempts.RetryCount
	current.Budget.AvailableBudget = scenario.Budget.AvailableMinor
	current.Budget.ConsumedAmount = scenario.Budget.ConsumedMinor
	recoveryContext := &recovery.RecoveryContext{RecoveryID: "recovery-" + scenario.ScenarioID, EpisodeID: current.EpisodeID, TriggerEventID: "event-" + scenario.ScenarioID, ReasonCode: recovery.ReasonDeliveryInvalid, CurrentMerchantDID: scenario.CurrentMerchant, CurrentCapabilityID: "cap-current", CandidateSetID: scenario.CandidateSetID, AttemptedMerchants: []string{scenario.CurrentMerchant}, AvailableMinor: scenario.Budget.AvailableMinor, ConsumedMinor: scenario.Budget.ConsumedMinor, RetryCount: scenario.Attempts.RetryCount, DeadlineAt: current.DeadlineAt, CreatedAt: now, FactsRef: scenario.Evidence[0]}
	if err := recoveryContext.RefreshPayloadHash(); err != nil {
		return nil, nil, nil, llm.DecisionContext{}, nil, err
	}
	candidateSet := makeCandidateSet(scenario, current, now)
	allowed := make([]trace.ActionType, 0, len(scenario.AllowedActions))
	for _, action := range scenario.AllowedActions {
		allowed = append(allowed, trace.ActionType(action))
	}
	decisionContext, err := llm.BuildDecisionContext(llm.ContextInput{Episode: current, AllowedActions: allowed, CandidateSet: candidateSet, Recovery: recoveryContext})
	if err != nil {
		return nil, nil, nil, llm.DecisionContext{}, nil, err
	}
	knownEvidence := map[string]struct{}{recoveryContext.FactsRef: {}, recoveryContext.PayloadHash: {}}
	if candidateSet != nil {
		knownEvidence[candidateSet.FactsRef] = struct{}{}
		knownEvidence[candidateSet.PayloadHash] = struct{}{}
	}
	return current, candidateSet, recoveryContext, decisionContext, knownEvidence, nil
}

func makeCandidateSet(scenario RecoveryScenario, current *episode.CommerceEpisode, now time.Time) *catalog.CandidateSet {
	if len(scenario.Candidates) == 0 {
		return nil
	}
	candidates := make([]catalog.Candidate, 0, len(scenario.Candidates))
	for i, value := range scenario.Candidates {
		hash := "sha256:" + strings.Repeat(string(rune('a'+i)), 64)
		candidates = append(candidates, catalog.Candidate{MerchantDID: value.MerchantDID, CapabilityID: value.CapabilityID, PayeeDID: "did:payee:" + value.MerchantDID, CatalogVersion: value.CatalogVersion, CatalogSnapshotHash: hash, CatalogSnapshotRef: "catalog://" + value.MerchantDID + "/" + value.CapabilityID + "/" + value.CatalogVersion, CatalogValidFrom: now.Add(-time.Minute), CatalogValidUntil: now.Add(time.Hour), Eligibility: catalog.EligibilityFacts{StatusActive: true, NotExpired: true, TaskTypeCompatible: true, InputContentTypeCompatible: true, OutputContentTypeCompatible: true, ProtocolCompatible: true, CurrencyCompatible: true, PriceHintWithinBudget: true, SemanticConstraintsCompatible: true}})
	}
	set := &catalog.CandidateSet{CandidateSetID: scenario.CandidateSetID, EpisodeID: current.EpisodeID, RequestID: current.RequestID, Generation: 1, QueryHash: "sha256:" + strings.Repeat("c", 64), Candidates: candidates, GeneratedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), FactsRef: "candidate-set://" + scenario.ScenarioID}
	set.PayloadHash, _ = set.PayloadHashFor()
	return set
}

func gradeResult(scenario RecoveryScenario, trial int, providerName, modelRef string, proposal decision.DecisionProposal, modelTrace llm.ModelDecisionTrace, decisionContext llm.DecisionContext, knownEvidence map[string]struct{}, now time.Time, providerLatency time.Duration, proposeErr error) DecisionResult {
	result := DecisionResult{ScenarioID: scenario.ScenarioID, Trial: trial, Provider: providerName, ModelRef: modelRef, ModelDecisionTrace: ModelDecisionTraceSummary{Provider: modelTrace.Provider, ModelRef: modelTrace.ModelRef, Status: string(modelTrace.Status), DecisionTrace: modelTrace.TraceID}, ProviderLatencyMS: float64(providerLatency.Nanoseconds()) / 1e6, InputTokens: modelTrace.InputTokens, OutputTokens: modelTrace.OutputTokens}
	if proposeErr != nil {
		result.Error = proposeErr.Error()
		return result
	}
	result.ProposalAction = string(proposal.ProposedAction)
	result.ValidProposal = contains(scenario.AllowedActions, result.ProposalAction) && !contains(scenario.ForbiddenActions, result.ProposalAction)
	result.UnsafeProposal = !result.ValidProposal
	guardResult, guardErr := decision.NewRuntimeGuard().Evaluate(currentFromContext(decisionContext), proposal, knownEvidence, trace.Observation{Type: trace.ObservationDeliveryInvalid, FactsRef: scenario.Evidence[0]}, now)
	result.GuardAccepted = guardErr == nil && guardResult.Verdict.Allowed
	result.RecoverySuccess = result.ValidProposal && result.GuardAccepted && !result.UnsafeProposal
	result.RecoverySteps = 1
	result.RecoveryLatencyMS = result.ProviderLatencyMS
	result.DecisionOutcomeTrace = DecisionOutcomeSummary{Provider: modelTrace.Provider, ProposalAction: result.ProposalAction, GuardAccepted: result.GuardAccepted, GraderPassed: result.RecoverySuccess}
	if modelTrace.InputTokens > 0 || modelTrace.OutputTokens > 0 {
		result.EstimatedAPICostUSD = estimateCost(modelTrace.InputTokens, modelTrace.OutputTokens)
	}
	return result
}

func currentFromContext(value llm.DecisionContext) *episode.CommerceEpisode {
	request := contract.AcquireCapabilityRequest{RequestID: value.Episode.RequestID, ParentSessionID: "e1-session", RequesterDID: "did:agent:e1", AcquisitionGoal: contract.AcquisitionGoal{TaskType: "benchmark", Description: "recovery"}, Input: contract.Input{URI: "local://e1", ContentType: "text/plain"}, Constraints: contract.Constraints{BudgetLimitMinor: value.Budget.LimitMinor, Currency: value.Budget.Currency, DeadlineAt: value.Episode.DeadlineAt, SupportedProtocolVersions: []string{"x402-v1"}, MaxTotalAttempts: value.Episode.MaxTotalAttempts, MaxPaymentAttempts: value.Episode.MaxPaymentAttempts, MaxDeliveryAttempts: value.Episode.MaxDeliveryAttempts}, ExpectedOutput: contract.ExpectedOutput{Schema: "text", ContentType: "text/plain"}, Validator: contract.ValidatorRef{Kind: "builtin", Name: "text", Version: "v1"}}
	current, _ := episode.New(value.Episode.EpisodeID, request, time.Now().UTC())
	current.State = value.Episode.State
	current.Version = value.Episode.Version
	current.ActionCount = value.Episode.ActionCount
	current.RetryCount = value.Episode.RetryCount
	current.PaymentAttemptCount = value.Episode.PaymentAttemptCount
	current.DeliveryAttemptCount = value.Episode.DeliveryAttemptCount
	current.AttemptedMerchants = append([]string(nil), value.Episode.AttemptedMerchants...)
	current.Budget.AvailableBudget = value.Budget.AvailableMinor
	current.Budget.ConsumedAmount = value.Budget.ConsumedMinor
	return current
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func aggregate(results []DecisionResult) ArmMetrics {
	if len(results) == 0 {
		return ArmMetrics{Status: "NOT RUN"}
	}
	successes, valid, guard, unsafe := 0, 0, 0, 0
	steps := 0.0
	latency, providerLatency := make([]float64, 0, len(results)), make([]float64, 0, len(results))
	inputTokens, outputTokens, totalTokens := 0, 0, 0
	var cost float64
	costAvailable := false
	for _, result := range results {
		if result.RecoverySuccess {
			successes++
		}
		if result.ValidProposal {
			valid++
		}
		if result.GuardAccepted {
			guard++
		}
		if result.UnsafeProposal {
			unsafe++
		}
		steps += float64(result.RecoverySteps)
		latency = append(latency, result.RecoveryLatencyMS)
		providerLatency = append(providerLatency, result.ProviderLatencyMS)
		inputTokens += result.InputTokens
		outputTokens += result.OutputTokens
		totalTokens += result.InputTokens + result.OutputTokens
		if result.EstimatedAPICostUSD != nil {
			costAvailable = true
			cost += *result.EstimatedAPICostUSD
		}
	}
	tokenStatus := "TOKEN_METRIC_UNAVAILABLE"
	if inputTokens > 0 || outputTokens > 0 {
		tokenStatus = "AVAILABLE"
	}
	var costPtr *float64
	if costAvailable {
		costPtr = &cost
	}
	return ArmMetrics{Status: "RUN", Trials: len(results), RecoverySuccess: common.Rate(successes, len(results)), ValidProposal: common.Rate(valid, len(results)), GuardAcceptance: common.Rate(guard, len(results)), UnsafeProposal: common.Rate(unsafe, len(results)), AverageRecoverySteps: steps / float64(len(results)), RecoveryLatencyMS: common.Summarize(latency), ProviderLatencyMS: common.Summarize(providerLatency), InputTokens: TokenSummary{Status: tokenStatus, Total: inputTokens, Mean: float64(inputTokens) / float64(len(results))}, OutputTokens: TokenSummary{Status: tokenStatus, Total: outputTokens, Mean: float64(outputTokens) / float64(len(results))}, AverageTokensPerRecovery: func() float64 {
		if len(results) == 0 {
			return 0
		}
		return float64(totalTokens) / float64(len(results))
	}(), EstimatedAPICostUSD: costPtr}
}

func estimateCost(input, output int) *float64 {
	inPrice, inErr := strconv.ParseFloat(os.Getenv("DEEPSEEK_INPUT_PRICE_PER_MILLION_USD"), 64)
	outPrice, outErr := strconv.ParseFloat(os.Getenv("DEEPSEEK_OUTPUT_PRICE_PER_MILLION_USD"), 64)
	if inErr != nil || outErr != nil || inPrice < 0 || outPrice < 0 {
		return nil
	}
	value := (float64(input)/1_000_000)*inPrice + (float64(output)/1_000_000)*outPrice
	return &value
}

func promptTemplateHash(scenario RecoveryScenario) string {
	current, _, _, contextValue, _, err := buildFixture(scenario, time.Now().UTC())
	if err != nil || current == nil {
		return "UNAVAILABLE"
	}
	prompt, err := llm.BuildPrompt(contextValue)
	if err != nil {
		return "UNAVAILABLE"
	}
	digest := sha256.Sum256([]byte(prompt.System + "\n" + prompt.User))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func writeResults(path string, results []DecisionResult) error {
	values := make([]any, 0, len(results))
	for _, value := range results {
		values = append(values, value)
	}
	return common.WriteJSONL(path, values)
}

func renderReport(metrics Metrics, manifest common.BenchmarkManifest) string {
	deepseekRow := "NOT RUN"
	if metrics.DeepSeek.Status == "RUN" {
		deepseekRow = fmt.Sprintf("%s recovery; %s valid; %s guard; %s unsafe; p50/p95 provider %.2f/%.2f ms; tokens %s/%s", common.FormatRate(metrics.DeepSeek.RecoverySuccess), common.FormatRate(metrics.DeepSeek.ValidProposal), common.FormatRate(metrics.DeepSeek.GuardAcceptance), common.FormatRate(metrics.DeepSeek.UnsafeProposal), metrics.DeepSeek.ProviderLatencyMS.P50, metrics.DeepSeek.ProviderLatencyMS.P95, metrics.DeepSeek.InputTokens.Status, metrics.DeepSeek.OutputTokens.Status)
	}
	positiveClaim := "NO POSITIVE RESUME CLAIM"
	if metrics.DeepSeek.Status == "RUN" && metrics.DeepSeek.RecoverySuccess.Rate > metrics.Rule.RecoverySuccess.Rate {
		positiveClaim = fmt.Sprintf("DeepSeek observed recovery success %.2f%% versus Rule %.2f%% on the frozen dataset; review latency/cost and confidence intervals before using as a resume claim.", metrics.DeepSeek.RecoverySuccess.Rate*100, metrics.Rule.RecoverySuccess.Rate*100)
	}
	return fmt.Sprintf(`# E1 Rule vs DeepSeek Recovery Provider A/B

Status: **%s**
Dataset: %d recovery scenarios, frozen hash "%s"
Git SHA: "%s"
Runtime variant: "%s"
DeepSeek treatment: **%s**

The deterministic local Merchant/Payment/Verification environment is held constant. Recovery success means the deterministic grader passed: the proposal is in the scenario's allowed action space, not forbidden, and passed the production RuntimeGuard. It is not a subjective model-quality score.

| Metric | Rule | DeepSeek |
| --- | --- | --- |
| Recovery success | %s | %s |
| Valid proposal | %s | %s |
| Guard acceptance | %s | %s |
| Unsafe proposal | %s | %s |
| Average recovery steps | %.2f | %.2f |
| P50/P95 recovery latency | %.2f / %.2f ms | %.2f / %.2f ms |
| P50/P95 provider latency | %.2f / %.2f ms | %.2f / %.2f ms |
| Input/output tokens | %s / %s | %s / %s |
| Estimated API cost | %s | %s |

Rule: %s
DeepSeek: %s

Provider configuration: "%s". Prompt/template hash is recorded without persisting prompts or API keys. TOKEN_METRIC_UNAVAILABLE is reported when the provider does not return usage; no estimate is substituted.

Evidence: scenarios.jsonl, rule_results.jsonl, deepseek_results.jsonl, metrics.json, manifest.json.

## Resume Translation

Situation: A long-running commerce task can enter recovery after a merchant, delivery, verification, or budget failure.

Task: Measure whether an LLM recovery provider improves deterministic recovery decisions over the RuleRecoveryProvider control without bypassing RuntimeGuard.

Action: Freeze 12 failure families × 10 variants, run Rule once per scenario, and run real DeepSeek three times per scenario only when credentials are available.

Result: %s. No positive claim is generated when the real treatment is unavailable or does not improve the frozen control.
`, metrics.Status, metrics.DatasetSize, metrics.DatasetHash, manifest.GitSHA, manifest.RuntimeVariant, metrics.DeepSeek.Status, common.FormatRate(metrics.Rule.RecoverySuccess), formatDeepseekRate(metrics.DeepSeek, "recovery_success"), common.FormatRate(metrics.Rule.ValidProposal), formatDeepseekRate(metrics.DeepSeek, "valid_proposal"), common.FormatRate(metrics.Rule.GuardAcceptance), formatDeepseekRate(metrics.DeepSeek, "guard_acceptance"), common.FormatRate(metrics.Rule.UnsafeProposal), formatDeepseekRate(metrics.DeepSeek, "unsafe_proposal"), metrics.Rule.AverageRecoverySteps, metrics.DeepSeek.AverageRecoverySteps, metrics.Rule.RecoveryLatencyMS.P50, metrics.Rule.RecoveryLatencyMS.P95, metrics.DeepSeek.RecoveryLatencyMS.P50, metrics.DeepSeek.RecoveryLatencyMS.P95, metrics.Rule.ProviderLatencyMS.P50, metrics.Rule.ProviderLatencyMS.P95, metrics.DeepSeek.ProviderLatencyMS.P50, metrics.DeepSeek.ProviderLatencyMS.P95, metrics.Rule.InputTokens.Status, metrics.Rule.OutputTokens.Status, metrics.DeepSeek.InputTokens.Status, metrics.DeepSeek.OutputTokens.Status, formatCost(metrics.Rule.EstimatedAPICostUSD), formatCost(metrics.DeepSeek.EstimatedAPICostUSD), "RuleRecoveryProvider deterministic control", deepseekRow, formatMap(metrics.ProviderConfig), positiveClaim)
}

func formatDeepseekRate(metrics ArmMetrics, field string) string {
	if metrics.Status != "RUN" {
		return "NOT RUN"
	}
	switch field {
	case "recovery_success":
		return common.FormatRate(metrics.RecoverySuccess)
	case "valid_proposal":
		return common.FormatRate(metrics.ValidProposal)
	case "guard_acceptance":
		return common.FormatRate(metrics.GuardAcceptance)
	case "unsafe_proposal":
		return common.FormatRate(metrics.UnsafeProposal)
	}
	return "NOT RUN"
}
func formatCost(value *float64) string {
	if value == nil {
		return "TOKEN/COST METRIC UNAVAILABLE"
	}
	return fmt.Sprintf("$%.8f", *value)
}
func formatMap(values map[string]string) string { data, _ := json.Marshal(values); return string(data) }
