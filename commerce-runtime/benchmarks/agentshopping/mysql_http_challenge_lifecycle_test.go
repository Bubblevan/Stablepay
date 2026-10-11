package agentshopping

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type challengeHTTPReplayRunManifest struct {
	RunID       string                    `json:"run_id"`
	RunMode     string                    `json:"run_mode"`
	TaskSource  string                    `json:"task_source"`
	TaskCount   int                       `json:"task_count"`
	Provider    string                    `json:"provider"`
	Model       string                    `json:"model"`
	Hashes      ChallengeEvaluationHashes `json:"hashes"`
	UsageStatus string                    `json:"usage_status"`
}

type challengeHTTPSuiteManifest struct {
	Status    string                    `json:"status"`
	TaskCount int                       `json:"task_count"`
	Hashes    ChallengeEvaluationHashes `json:"hashes"`
}

type challengeHTTPParseRecord struct {
	TaskID string `json:"task_id"`
	Intent Intent `json:"intent"`
	Error  string `json:"error,omitempty"`
}

type challengeHTTPReplayParser struct {
	byPrompt map[string]challengeHTTPParseRecord
}

func (p challengeHTTPReplayParser) Provider() string { return "openai-codex" }
func (p challengeHTTPReplayParser) Model() string    { return "gpt-6-luna" }

func (p challengeHTTPReplayParser) Parse(_ context.Context, prompt, _ string, _ int64, _ Taxonomy) (Intent, error) {
	result, ok := p.byPrompt[prompt]
	if !ok {
		return Intent{}, errors.New("no hash-bound recorded challenge parse matches this prompt")
	}
	if result.Error != "" {
		return Intent{}, errors.New(result.Error)
	}
	return result.Intent, nil
}

func (p challengeHTTPReplayParser) ParseWithUsage(ctx context.Context, prompt, region string, budget int64,
	taxonomy Taxonomy) (Intent, ParseUsage, error) {
	intent, err := p.Parse(ctx, prompt, region, budget, taxonomy)
	usage := ParseUsage{PromptSHA256: hashValue(prompt), Reasoning: "none",
		UsageStatus: "NOT_APPLICABLE_RECORDED_RESPONSE_REPLAY", APICallsStatus: "NOT_APPLICABLE_REPLAY_NO_MODEL_CALL"}
	if err == nil {
		encoded, marshalErr := json.Marshal(intent)
		if marshalErr != nil {
			return Intent{}, usage, marshalErr
		}
		usage.ResponseSHA256 = hashValue(string(encoded))
	}
	return intent, usage, err
}

type challengeHTTPLifecycleTrial struct {
	TaskID                          string               `json:"task_id"`
	Scenario                        string               `json:"scenario"`
	ExpectedDisposition             ChallengeDisposition `json:"expected_disposition"`
	ActualDisposition               ChallengeDisposition `json:"actual_disposition"`
	HTTPStatus                      int                  `json:"http_status"`
	ExactIntentMatch                bool                 `json:"exact_intent_match"`
	ClarificationQuestion           bool                 `json:"clarification_question_provided"`
	ClarificationTargetsField       bool                 `json:"clarification_targets_missing_field"`
	ClarificationMissingFieldsEmpty bool                 `json:"clarification_missing_fields_empty"`
	CandidateCount                  int                  `json:"candidate_count"`
	TopSKU                          string               `json:"top_sku,omitempty"`
	TopCandidateValid               bool                 `json:"top_candidate_valid"`
	DecisionCorrect                 bool                 `json:"decision_correct"`
	ValidPlan                       bool                 `json:"valid_plan"`
	TransactionExecuted             bool                 `json:"transaction_executed"`
	Mutation                        string               `json:"mutation,omitempty"`
	StaleQuoteRejected              bool                 `json:"stale_quote_rejected"`
	OrderID                         string               `json:"order_id,omitempty"`
	OrderStatus                     string               `json:"order_status,omitempty"`
	PaymentStatus                   string               `json:"mock_payment_status,omitempty"`
	HTTPRequests                    int                  `json:"http_requests"`
	LatencyMillis                   float64              `json:"end_to_end_latency_ms"`
	Failure                         string               `json:"failure,omitempty"`
}

type challengeHTTPLifecycleMetrics struct {
	Status                  string                        `json:"status"`
	RunID                   string                        `json:"run_id"`
	TenantID                string                        `json:"tenant_id"`
	Database                string                        `json:"database"`
	ChallengeTaskCount      int                           `json:"challenge_task_count"`
	PlanTasks               int                           `json:"plan_tasks"`
	NoPlanTasks             int                           `json:"no_plan_tasks"`
	ClarificationTasks      int                           `json:"clarification_tasks"`
	CorrectDecisions        int                           `json:"correct_decisions"`
	CorrectDecisionRate     float64                       `json:"correct_decision_rate_all_tasks"`
	ValidPlans              int                           `json:"valid_plans"`
	ValidPlanRate           float64                       `json:"valid_plan_rate_on_expected_plan_tasks"`
	CompletedFlows          int                           `json:"completed_flows"`
	CompletedFlowRate       float64                       `json:"completed_flow_rate_on_expected_plan_tasks"`
	PaidOrders              int                           `json:"paid_orders"`
	DeclinedPayments        int                           `json:"declined_payments"`
	PriceChangeCases        int                           `json:"price_change_cases"`
	PriceChangeSkipped      int                           `json:"price_change_skipped_no_budget_headroom"`
	StockChangeCases        int                           `json:"stock_change_cases"`
	StaleQuoteConfirmations int                           `json:"stale_quote_confirmations"`
	UnexpectedHTTPFailures  int64                         `json:"unexpected_http_failures"`
	HTTPStatusCounts        map[string]int64              `json:"http_status_counts"`
	HTTPRequests            int64                         `json:"http_request_count"`
	TaskP50Millis           float64                       `json:"task_http_lifecycle_p50_ms"`
	TaskP95Millis           float64                       `json:"task_http_lifecycle_p95_ms"`
	TasksPerSecond          float64                       `json:"terminal_tasks_per_second"`
	OfflineReference        challengeHTTPReferenceSummary `json:"offline_reference_comparison"`
	Audit                   challengeHTTPLifecycleAudit   `json:"audit"`
	NewModelCalls           int                           `json:"new_model_calls"`
	TokenUsage              string                        `json:"token_usage"`
	TargetProductRecallAtK  string                        `json:"target_product_recall_at_k"`
	ResumeClaimEligibility  string                        `json:"resume_claim_eligibility"`
}

type challengeHTTPReferenceSummary struct {
	ModelRunID                     string  `json:"model_run_id"`
	Provider                       string  `json:"provider"`
	Model                          string  `json:"model"`
	ModelRunTaskHash               string  `json:"model_run_task_sha256"`
	ExecutedTaskHash               string  `json:"executed_task_sha256"`
	PromptRecordsExactMatchCount   int     `json:"exact_prompt_record_matches"`
	OfflineCorrectDecisionRate     float64 `json:"offline_correct_decision_rate"`
	OfflineBaselineValidPlanRate   float64 `json:"offline_baseline_valid_plan_rate"`
	OfflineTreatmentValidPlanRate  float64 `json:"offline_treatment_valid_plan_rate"`
	TargetProductRecallLabelStatus string  `json:"target_product_recall_label_status"`
	TaskProvenanceStatus           string  `json:"task_provenance_status"`
	Note                           string  `json:"note"`
}

type challengeHTTPLifecycleAudit struct {
	Passed              bool             `json:"passed"`
	ExpectedCounts      map[string]int64 `json:"expected_counts"`
	ObservedCounts      map[string]int64 `json:"observed_counts"`
	InvariantViolations map[string]int64 `json:"invariant_violations"`
	TaskStatuses        map[string]int64 `json:"task_statuses"`
	AuditActions        map[string]int64 `json:"audit_actions"`
}

// TestMySQLShoppingChallenge500HTTPLifecycle sends the frozen generated
// challenge prompts through the real benchmark HTTP API and isolated MySQL.
// Only a top candidate satisfying the task's predeclared feasible labels and
// live constraints is allowed to cross the simulated checkout boundary.
func TestMySQLShoppingChallenge500HTTPLifecycle(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_HTTP_CHALLENGE_LIFECYCLE") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_HTTP_CHALLENGE_LIFECYCLE=1 for the 500-task HTTP/MySQL challenge lifecycle")
	}
	password := strings.TrimSpace(os.Getenv("SHOP_SIM_DB_PASSWORD"))
	repoRoot := strings.TrimSpace(os.Getenv("SHOP_SIM_REPO_ROOT"))
	tasksPath := strings.TrimSpace(os.Getenv("SHOP_SIM_CHALLENGE_TASKS_PATH"))
	suiteManifestPath := strings.TrimSpace(os.Getenv("SHOP_SIM_CHALLENGE_SUITE_MANIFEST_PATH"))
	modelRunDir := strings.TrimSpace(os.Getenv("SHOP_SIM_CHALLENGE_MODEL_RUN_DIR"))
	merchantManifestPath := strings.TrimSpace(os.Getenv("SHOP_SIM_SOURCE_MANIFEST_PATH"))
	for name, value := range map[string]string{"SHOP_SIM_DB_PASSWORD": password, "SHOP_SIM_REPO_ROOT": repoRoot,
		"SHOP_SIM_CHALLENGE_TASKS_PATH": tasksPath, "SHOP_SIM_CHALLENGE_SUITE_MANIFEST_PATH": suiteManifestPath,
		"SHOP_SIM_CHALLENGE_MODEL_RUN_DIR": modelRunDir, "SHOP_SIM_SOURCE_MANIFEST_PATH": merchantManifestPath} {
		if value == "" {
			t.Fatalf("%s is required", name)
		}
	}

	tasks, err := readChallengeHTTPTasks(tasksPath)
	if err != nil {
		t.Fatal(err)
	}
	modelTasksPath := filepath.Join(modelRunDir, "suite.jsonl")
	modelTasks, err := readChallengeHTTPTasks(modelTasksPath)
	if err != nil {
		t.Fatalf("read model-run prompt packet: %v", err)
	}
	parseRows, err := readChallengeHTTPParseRows(filepath.Join(modelRunDir, "parse-results.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var modelRun challengeHTTPReplayRunManifest
	if err := readLifecycleJSON(filepath.Join(modelRunDir, "manifest.json"), &modelRun); err != nil {
		t.Fatal(err)
	}
	var suiteManifest challengeHTTPSuiteManifest
	if err := readLifecycleJSON(suiteManifestPath, &suiteManifest); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 500 || len(modelTasks) != 500 || len(parseRows) != 500 || modelRun.TaskCount != 500 || suiteManifest.TaskCount != 500 {
		t.Fatalf("challenge dimensions must be 500: tasks=%d model_tasks=%d parse_rows=%d model_manifest=%d suite_manifest=%d",
			len(tasks), len(modelTasks), len(parseRows), modelRun.TaskCount, suiteManifest.TaskCount)
	}
	if modelRun.Provider != "openai-codex" || modelRun.Model != "gpt-6-luna" ||
		modelRun.RunMode != "UNREVIEWED_DIAGNOSTIC_MODEL_PARSE_AND_EVALUATION" {
		t.Fatalf("model parse source is not the expected recorded GPT-6 Luna diagnostic run: run=%q provider=%q model=%q mode=%q",
			modelRun.RunID, modelRun.Provider, modelRun.Model, modelRun.RunMode)
	}
	if err := assertChallengeHTTPReplayBinding(tasks, modelTasks, parseRows); err != nil {
		t.Fatal(err)
	}
	parser, batchResults, err := newChallengeHTTPReplayParser(tasks, parseRows)
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(merchantManifestPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated challenge HTTP MySQL: %v", err)
		}
	})
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	merchantCatalog, err := LoadMerchantCatalog(ctx, merchantManifestPath, nil)
	if err != nil {
		t.Fatalf("load the live Merchant HTTP catalog snapshot: %v", err)
	}
	if err := store.ValidateMerchantCatalogSnapshot(ctx, merchantCatalog); err != nil {
		t.Fatalf("source MySQL seed differs from the current Merchant HTTP snapshot: %v", err)
	}
	computedHashes, err := ComputeChallengeHashes(merchantCatalog, tasks, manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	if computedHashes != suiteManifest.Hashes {
		t.Fatalf("executed challenge/catalog hashes differ from the frozen suite manifest: current=%+v frozen=%+v", computedHashes, suiteManifest.Hashes)
	}
	if err := ValidateChallengeTaskSet(merchantCatalog, tasks); err != nil {
		t.Fatalf("frozen challenge labels do not fit the current snapshot: %v", err)
	}
	offlineReport, _, err := EvaluateChallengeTasks(merchantCatalog, tasks, batchResults, DefaultRecallCutoff)
	if err != nil {
		t.Fatalf("compute the offline paired reference for this exact challenge snapshot: %v", err)
	}

	sourceTenant := store.tenant
	suffix := mysqlIntegrationSuffix(t)
	tenantID := "shopping-challenge-http-" + suffix
	catalog, buyers, err := seedLifecycleTenantFromMerchantSnapshot(ctx, store, sourceTenant, tenantID, merchantCatalog, 20261010)
	if err != nil {
		t.Fatal(err)
	}
	store.tenant = tenantID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cleanupCancel()
		if err := cleanupMySQLIntegrationTenant(cleanupCtx, store, tenantID); err != nil {
			t.Errorf("clean challenge HTTP tenant %q: %v", tenantID, err)
			return
		}
		counts, err := store.Counts(cleanupCtx)
		if err != nil {
			t.Errorf("verify challenge HTTP tenant cleanup: %v", err)
			return
		}
		for table, count := range counts {
			if count != 0 {
				t.Errorf("challenge HTTP tenant retained %d rows in %s", count, table)
			}
		}
	})
	api, err := NewHTTPAPIWithMerchantCatalog(store, parser, mysqlIntegrationAPIKey, catalog, mysqlIntegrationBuyerApprovalKey)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	client := &lifecycleHTTPClient{api: &mysqlIntegrationAPI{client: server.Client(), server: server, key: mysqlIntegrationAPIKey},
		statusCounts: make(map[int]int64)}
	ready := client.do(t, http.MethodGet, "/readyz", nil)
	if ready.status != http.StatusOK || !strings.Contains(string(ready.body), `"status":"ready"`) {
		t.Fatalf("challenge HTTP API is not ready: status=%d body=%s", ready.status, ready.body)
	}
	data := client.do(t, http.MethodGet, "/v1/dataset", nil)
	var dataInfo struct {
		TenantID string `json:"tenant_id"`
	}
	if data.status != http.StatusOK || json.Unmarshal(data.body, &dataInfo) != nil || dataInfo.TenantID != tenantID {
		t.Fatalf("challenge HTTP API escaped its isolated tenant: status=%d body=%s", data.status, data.body)
	}

	outputDir, runID, err := lifecycleEvidenceDirectory(repoRoot, "challenge-http-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"schema_version": "stablepay.agent-shopping-http-challenge-lifecycle.v1", "run_id": runID, "status": "RUNNING",
		"database": SimulationDatabaseName, "tenant_id": tenantID, "source_tenant_id": sourceTenant,
		"catalog_source": "12 loopback Merchant HTTP fixtures", "catalog_products": len(merchantCatalog.Products),
		"challenge_suite_status": suiteManifest.Status, "challenge_tasks_path": tasksPath,
		"challenge_tasks_sha256": computedHashes.TasksSHA256, "model_source_run_id": modelRun.RunID,
		"model_source_task_sha256": modelRun.Hashes.TasksSHA256, "model_provider": modelRun.Provider, "model": modelRun.Model,
		"model_prompt_packet_exact_matches": len(tasks), "model_run_mode": modelRun.RunMode,
		"new_model_calls": 0, "token_usage": "NOT_APPLICABLE_RECORDED_RESPONSE_REPLAY",
		"payment_provider": "simulation-only", "real_settlement": false,
		"http_boundary":       "net/http loopback httptest API plus persistent isolated MySQL",
		"task_label_boundary": "AUTO_GENERATED_UNREVIEWED_DRAFT; gold/feasibility labels derived from the same synthetic Merchant snapshot",
		"api_key_recorded":    false, "created_at_utc": time.Now().UTC(),
	}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}

	metrics := challengeHTTPLifecycleMetrics{RunID: runID, TenantID: tenantID, Database: SimulationDatabaseName,
		ChallengeTaskCount: len(tasks), NewModelCalls: 0, TokenUsage: "NOT_APPLICABLE_RECORDED_RESPONSE_REPLAY",
		TargetProductRecallAtK: "NOT_MEASURED_HTTP_FEASIBLE_CANDIDATE_RESPONSE_DOES_NOT_EXPOSE_PREFILTER_RETRIEVAL_WINDOW",
		ResumeClaimEligibility: "DIAGNOSTIC_ONLY_UNREVIEWED_SYNTHETIC_TASKS_AND_LABELS"}
	metrics.OfflineReference = challengeHTTPReferenceSummary{
		ModelRunID: modelRun.RunID, Provider: modelRun.Provider, Model: modelRun.Model,
		ModelRunTaskHash: modelRun.Hashes.TasksSHA256, ExecutedTaskHash: computedHashes.TasksSHA256,
		PromptRecordsExactMatchCount: len(tasks), OfflineCorrectDecisionRate: offlineReport.CorrectDecisionRate.Rate,
		OfflineBaselineValidPlanRate:   offlineReport.PlanComparison.BaselineValidPlanRate.Rate,
		OfflineTreatmentValidPlanRate:  offlineReport.ValidPlanRate.Rate,
		TargetProductRecallLabelStatus: offlineReport.TargetProductRecallAtK.Status,
		TaskProvenanceStatus:           "UNVERIFIED_INPUT_REQUIRES_INDEPENDENT_REVIEW",
		Note:                           "offline ranking comparison is a paired diagnostic on the same synthetic snapshot; its labels are not independent human relevance judgments",
	}
	byRegion := make(map[string][]BuyerRecord)
	for _, buyer := range buyers {
		byRegion[buyer.HomeRegion] = append(byRegion[buyer.HomeRegion], buyer)
	}
	usedBuyers := make(map[string]bool, len(tasks))
	trials := make([]challengeHTTPLifecycleTrial, 0, len(tasks))
	latencies := make([]float64, 0, len(tasks))
	executedOrderIDs := make(map[string]bool)
	startAll := time.Now()
	for index, task := range tasks {
		buyer, ok := chooseLifecycleBuyer(byRegion[task.Intent.Region], task.Intent.MaxPriceMinor, usedBuyers)
		if !ok {
			t.Fatalf("no unique synthetic buyer for task %s in region %s with budget %d", task.TaskID, task.Intent.Region, task.Intent.MaxPriceMinor)
		}
		usedBuyers[buyer.BuyerID] = true
		if task.ExpectedDisposition == ChallengePlan {
			metrics.PlanTasks++
		} else if task.ExpectedDisposition == ChallengeNoPlan {
			metrics.NoPlanTasks++
		} else if task.ExpectedDisposition == ChallengeClarify {
			metrics.ClarificationTasks++
		}
		trial := challengeHTTPLifecycleTrial{TaskID: task.TaskID, Scenario: task.Scenario,
			ExpectedDisposition: task.ExpectedDisposition}
		requestStart := client.requests
		started := time.Now()
		response := client.do(t, http.MethodPost, "/v1/intents/parse", map[string]any{
			"buyer_id": buyer.BuyerID, "prompt": task.Prompt, "idempotency_key": "http-lifecycle-parse-" + task.TaskID,
		})
		trial.HTTPStatus = response.status
		if response.status == http.StatusBadGateway && parser.byPrompt[task.Prompt].Error != "" {
			trial.ActualDisposition = ChallengeParseFail
			trial.Failure = "RECORDED_MODEL_PARSE_ERROR:" + parser.byPrompt[task.Prompt].Error
		} else if response.status != http.StatusOK {
			trial.ActualDisposition = ChallengeParseFail
			var envelope struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			_ = json.Unmarshal(response.body, &envelope)
			trial.Failure = fmt.Sprintf("UNEXPECTED_HTTP_STATUS_%d:%s:%s", response.status,
				envelope.Error.Code, strings.ReplaceAll(envelope.Error.Message, "\n", " "))
			metrics.UnexpectedHTTPFailures++
		} else {
			var parsed struct {
				TaskID                    string      `json:"task_id"`
				TaskStatus                string      `json:"task_status"`
				Intent                    Intent      `json:"intent"`
				Candidates                []Candidate `json:"candidates"`
				ClarificationQuestion     string      `json:"clarification_question"`
				BuyerConfirmationRequired bool        `json:"buyer_confirmation_required"`
				PaymentCreated            bool        `json:"payment_created"`
				RealSettlement            bool        `json:"real_settlement"`
			}
			if err := json.Unmarshal(response.body, &parsed); err != nil {
				trial.ActualDisposition = ChallengeParseFail
				trial.Failure = "INVALID_HTTP_RESPONSE_JSON"
				metrics.UnexpectedHTTPFailures++
			} else {
				trial.ExactIntentMatch = sameChallengeIntent(parsed.Intent, task.Intent)
				trial.CandidateCount = len(parsed.Candidates)
				switch parsed.TaskStatus {
				case TaskStatusClarificationRequired, TaskStatusClarificationResolved:
					trial.ActualDisposition = ChallengeClarify
					trial.ClarificationQuestion = strings.TrimSpace(parsed.ClarificationQuestion) != ""
					trial.ClarificationTargetsField = clarificationQuestionTargetsMissingFields(parsed.ClarificationQuestion, task.RequiredMissingFields)
					trial.ClarificationMissingFieldsEmpty = true
					for _, field := range task.RequiredMissingFields {
						if challengeIntentFieldFromTrial(parsed.Intent, field) != "" {
							trial.ClarificationMissingFieldsEmpty = false
						}
					}
					if len(parsed.Candidates) != 0 || parsed.BuyerConfirmationRequired || parsed.PaymentCreated || parsed.RealSettlement {
						trial.Failure = "CLARIFICATION_RETURNED_PURCHASE_SIDE_EFFECT_OR_CANDIDATE"
					}
				case TaskStatusNoMatchingCandidates:
					trial.ActualDisposition = ChallengeNoPlan
					if len(parsed.Candidates) != 0 || parsed.PaymentCreated || parsed.RealSettlement {
						trial.Failure = "NO_PLAN_RETURNED_PURCHASE_SIDE_EFFECT_OR_CANDIDATE"
					}
				case TaskStatusDiscovered:
					trial.ActualDisposition = ChallengePlan
					if len(parsed.Candidates) == 0 {
						trial.Failure = "DISCOVERED_WITHOUT_CANDIDATES"
					} else {
						candidate := parsed.Candidates[0]
						trial.TopSKU = candidate.Product.SKUID
						trial.TopCandidateValid = challengeHTTPTopCandidateValid(task, parsed.Intent, candidate)
						trial.ValidPlan = trial.ExactIntentMatch && trial.TopCandidateValid && parsed.BuyerConfirmationRequired
						if task.ExpectedDisposition == ChallengePlan && trial.ValidPlan {
							transactionTask := challengeHTTPTransactionTask(task, candidate)
							if strings.Contains(task.Scenario, "PRICE_CHANGE") && candidate.Inventory.PriceMinor+1 > task.Intent.MaxPriceMinor {
								metrics.PriceChangeSkipped++
							}
							flow := runHTTPShoppingLifecycleTask(t, client, transactionTask, buyer)
							trial.HTTPRequests = int(client.requests - requestStart)
							trial.Mutation = flow.Mutation
							trial.StaleQuoteRejected = flow.StaleQuoteRejected
							trial.OrderID, trial.OrderStatus, trial.PaymentStatus = flow.OrderID, flow.OrderStatus, flow.PaymentStatus
							trial.TransactionExecuted = flow.OrderID != "" && (flow.OrderStatus == TaskStatusPaid || flow.OrderStatus == TaskStatusPaymentFailed)
							if !trial.TransactionExecuted || !flow.PlanValid || !flow.SelectedIsGold || flow.SelectedSKU != candidate.Product.SKUID {
								trial.Failure = "HTTP_TRANSACTION_DID_NOT_PRESERVE_VALID_TOP_CANDIDATE"
							}
							if trial.TransactionExecuted {
								executedOrderIDs[flow.OrderID] = true
								metrics.CompletedFlows++
								metrics.ValidPlans++
								if trial.PaymentStatus == PaymentSettled {
									metrics.PaidOrders++
								} else if trial.PaymentStatus == PaymentDeclined {
									metrics.DeclinedPayments++
								}
								if trial.Mutation == "PRICE_INCREASE" {
									metrics.PriceChangeCases++
								} else if trial.Mutation == "STOCK_LOSS" {
									metrics.StockChangeCases++
								}
								if trial.StaleQuoteRejected {
									metrics.StaleQuoteConfirmations++
								}
							}
						}
					}
				}
			}
		}
		trial.DecisionCorrect = challengeHTTPLifecycleDecisionCorrect(task, trial)
		if trial.DecisionCorrect {
			metrics.CorrectDecisions++
		}
		if trial.HTTPRequests == 0 {
			trial.HTTPRequests = int(client.requests - requestStart)
		}
		trial.LatencyMillis = float64(time.Since(started).Microseconds()) / 1000
		latencies = append(latencies, trial.LatencyMillis)
		trials = append(trials, trial)
		if index == 0 || (index+1)%100 == 0 {
			t.Logf("challenge HTTP lifecycle progress %d/%d valid_plans=%d completed_flows=%d decisions=%d/%d",
				index+1, len(tasks), metrics.ValidPlans, metrics.CompletedFlows, metrics.CorrectDecisions, index+1)
		}
	}
	metrics.CorrectDecisionRate = float64(metrics.CorrectDecisions) / float64(len(tasks))
	if metrics.PlanTasks > 0 {
		metrics.ValidPlanRate = float64(metrics.ValidPlans) / float64(metrics.PlanTasks)
		metrics.CompletedFlowRate = float64(metrics.CompletedFlows) / float64(metrics.PlanTasks)
	}
	metrics.TaskP50Millis = lifecyclePercentile(latencies, 0.50)
	metrics.TaskP95Millis = lifecyclePercentile(latencies, 0.95)
	metrics.TasksPerSecond = float64(len(tasks)) / time.Since(startAll).Seconds()
	metrics.HTTPStatusCounts = make(map[string]int64, len(client.statusCounts))
	for status, count := range client.statusCounts {
		metrics.HTTPStatusCounts[fmt.Sprint(status)] = count
	}
	for _, count := range client.statusCounts {
		metrics.HTTPRequests += count
	}
	metrics.Audit = auditChallengeHTTPLifecycle(ctx, store, tenantID, len(tasks), metrics.CompletedFlows,
		metrics.PriceChangeCases, metrics.StockChangeCases, executedOrderIDs)
	metrics.Status = "PASS"
	if !metrics.Audit.Passed || metrics.UnexpectedHTTPFailures != 0 || len(trials) != 500 || metrics.CompletedFlows != metrics.ValidPlans {
		metrics.Status = "FAIL"
	}
	manifest["status"] = metrics.Status
	manifest["http_request_count"] = metrics.HTTPRequests
	manifest["http_status_counts"] = metrics.HTTPStatusCounts
	manifest["audit_passed"] = metrics.Audit.Passed
	manifest["tenant_cleanup"] = "registered_test_cleanup_verifies_zero_rows"
	for name, value := range map[string]any{"metrics.json": metrics, "audit-summary.json": metrics.Audit} {
		if err := writeLifecycleJSON(filepath.Join(outputDir, name), value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeLifecycleLines(filepath.Join(outputDir, "trials.jsonl"), trials); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	t.Logf("MYSQL_HTTP_CHALLENGE_LIFECYCLE status=%s tasks=%d plan=%d valid=%d flows=%d no_plan=%d clarify=%d decisions=%d/500 paid=%d declined=%d price=%d stock=%d stale=%d audit=%v http=%d p95_ms=%.2f tasks_per_sec=%.2f evidence=%s",
		metrics.Status, len(tasks), metrics.PlanTasks, metrics.ValidPlans, metrics.CompletedFlows, metrics.NoPlanTasks,
		metrics.ClarificationTasks, metrics.CorrectDecisions, metrics.PaidOrders, metrics.DeclinedPayments,
		metrics.PriceChangeCases, metrics.StockChangeCases, metrics.StaleQuoteConfirmations, metrics.Audit.Passed,
		metrics.HTTPRequests, metrics.TaskP95Millis, metrics.TasksPerSecond, outputDir)
	if metrics.Status != "PASS" {
		t.Errorf("500-task HTTP challenge lifecycle safety/audit gate failed: %s", outputDir)
	}
}

func readChallengeHTTPTasks(path string) ([]ChallengeTask, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var tasks []ChallengeTask
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var task ChallengeTask
		if err := json.Unmarshal(scanner.Bytes(), &task); err != nil {
			return nil, fmt.Errorf("decode challenge task %d: %w", len(tasks)+1, err)
		}
		tasks = append(tasks, task)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func readChallengeHTTPParseRows(path string) ([]challengeHTTPParseRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var rows []challengeHTTPParseRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var row challengeHTTPParseRecord
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return nil, fmt.Errorf("decode recorded challenge parse %d: %w", len(rows)+1, err)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

func assertChallengeHTTPReplayBinding(tasks, modelTasks []ChallengeTask, rows []challengeHTTPParseRecord) error {
	modelByID := make(map[string]ChallengeTask, len(modelTasks))
	for _, task := range modelTasks {
		if task.TaskID == "" || modelByID[task.TaskID].TaskID != "" {
			return fmt.Errorf("model-run prompt packet contains an empty or duplicate task id %q", task.TaskID)
		}
		modelByID[task.TaskID] = task
	}
	parseByID := make(map[string]challengeHTTPParseRecord, len(rows))
	for _, row := range rows {
		if row.TaskID == "" || parseByID[row.TaskID].TaskID != "" {
			return fmt.Errorf("recorded parse results contain an empty or duplicate task id %q", row.TaskID)
		}
		parseByID[row.TaskID] = row
	}
	seenPrompts := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		modelTask, ok := modelByID[task.TaskID]
		if !ok || modelTask.Prompt != task.Prompt || modelTask.ExpectedDisposition != task.ExpectedDisposition ||
			!sameChallengeIntent(modelTask.Intent, task.Intent) || !sameStringSet(modelTask.GoldSKUs, task.GoldSKUs) {
			return fmt.Errorf("challenge task %s differs from the prompt/gold context used by the recorded model run", task.TaskID)
		}
		if seenPrompts[task.Prompt] {
			return fmt.Errorf("challenge prompt is not unique at task %s", task.TaskID)
		}
		seenPrompts[task.Prompt] = true
		if _, ok := parseByID[task.TaskID]; !ok {
			return fmt.Errorf("recorded model run has no parse row for task %s", task.TaskID)
		}
		delete(modelByID, task.TaskID)
		delete(parseByID, task.TaskID)
	}
	if len(modelByID) != 0 || len(parseByID) != 0 {
		return fmt.Errorf("recorded model artifacts contain unexpected rows: prompts=%d parses=%d", len(modelByID), len(parseByID))
	}
	return nil
}

func newChallengeHTTPReplayParser(tasks []ChallengeTask, rows []challengeHTTPParseRecord) (challengeHTTPReplayParser, []BatchIntentResult, error) {
	rowByID := make(map[string]challengeHTTPParseRecord, len(rows))
	for _, row := range rows {
		if row.TaskID == "" || rowByID[row.TaskID].TaskID != "" {
			return challengeHTTPReplayParser{}, nil, fmt.Errorf("recorded parse rows contain duplicate task %q", row.TaskID)
		}
		rowByID[row.TaskID] = row
	}
	byPrompt := make(map[string]challengeHTTPParseRecord, len(tasks))
	results := make([]BatchIntentResult, 0, len(tasks))
	for _, task := range tasks {
		row, ok := rowByID[task.TaskID]
		if !ok {
			return challengeHTTPReplayParser{}, nil, fmt.Errorf("missing recorded parse result for %s", task.TaskID)
		}
		if _, exists := byPrompt[task.Prompt]; exists {
			return challengeHTTPReplayParser{}, nil, fmt.Errorf("duplicate challenge prompt for %s", task.TaskID)
		}
		byPrompt[task.Prompt] = row
		results = append(results, BatchIntentResult{TaskID: task.TaskID, Intent: row.Intent, Error: row.Error})
		delete(rowByID, task.TaskID)
	}
	if len(rowByID) != 0 {
		return challengeHTTPReplayParser{}, nil, fmt.Errorf("recorded parse artifact contains %d extra results", len(rowByID))
	}
	return challengeHTTPReplayParser{byPrompt: byPrompt}, results, nil
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	set := make(map[string]int, len(left))
	for _, value := range left {
		set[value]++
	}
	for _, value := range right {
		if set[value] == 0 {
			return false
		}
		set[value]--
	}
	return true
}

func challengeHTTPTopCandidateValid(task ChallengeTask, parsed Intent, candidate Candidate) bool {
	allowedSKU := false
	for _, sku := range task.GoldSKUs {
		if sku == candidate.Product.SKUID {
			allowedSKU = true
			break
		}
	}
	if !allowedSKU && task.Intent.AllowSubstitution && candidate.MatchType == MatchTypeSubstitute {
		for _, sku := range task.AcceptedSubstituteSKUs {
			if sku == candidate.Product.SKUID {
				allowedSKU = true
				break
			}
		}
	}
	return allowedSKU && sameChallengeIntent(parsed, task.Intent) && candidate.Product.Category == task.Intent.Category &&
		candidate.Inventory.Region == task.Intent.Region && candidate.Inventory.Eligible &&
		candidate.Inventory.OnHand > 0 && candidate.Inventory.PriceMinor <= task.Intent.MaxPriceMinor &&
		requiredAttributesMatch(candidate.Product.Attributes, task.Intent.RequiredAttributes)
}

func challengeHTTPTransactionTask(task ChallengeTask, candidate Candidate) Task {
	gold := append([]string(nil), task.GoldSKUs...)
	if task.Intent.AllowSubstitution {
		gold = append(gold, task.AcceptedSubstituteSKUs...)
	}
	mutation := "NONE"
	if strings.Contains(task.Scenario, "PRICE_CHANGE") && candidate.Inventory.PriceMinor+1 <= task.Intent.MaxPriceMinor {
		mutation = "PRICE_INCREASE"
	} else if strings.Contains(task.Scenario, "STOCK_CHANGE") {
		mutation = "STOCK_LOSS"
	}
	return Task{TaskID: task.TaskID, Scenario: task.Scenario, Prompt: task.Prompt, Intent: task.Intent,
		GoldSKUs: gold, Mutation: mutation}
}

func challengeHTTPLifecycleDecisionCorrect(task ChallengeTask, trial challengeHTTPLifecycleTrial) bool {
	switch task.ExpectedDisposition {
	case ChallengePlan:
		return trial.ActualDisposition == ChallengePlan && trial.ValidPlan && trial.TransactionExecuted
	case ChallengeNoPlan:
		return trial.ActualDisposition == ChallengeNoPlan && trial.ExactIntentMatch && trial.CandidateCount == 0 && trial.Failure == ""
	case ChallengeClarify:
		return trial.ActualDisposition == ChallengeClarify && trial.ExactIntentMatch && trial.ClarificationQuestion &&
			trial.ClarificationTargetsField && trial.ClarificationMissingFieldsEmpty && trial.CandidateCount == 0 && trial.Failure == ""
	default:
		return false
	}
}

func challengeIntentFieldFromTrial(intent Intent, field string) string {
	switch field {
	case "category":
		return intent.Category
	case "item":
		return intent.Item
	case "required_format":
		return intent.RequiredFormat
	case "preferred_style":
		return intent.PreferredStyle
	case "region":
		return intent.Region
	default:
		return "<unsupported>"
	}
}

func auditChallengeHTTPLifecycle(ctx context.Context, store *MySQLStore, tenant string, taskCount, flows, priceChanges, stockChanges int,
	executedOrderIDs map[string]bool) challengeHTTPLifecycleAudit {
	violations := make(map[string]int64)
	observed, err := store.Counts(ctx)
	if err != nil {
		observed = map[string]int64{}
		violations["counts_query_error"] = 1
	}
	expected := map[string]int64{
		"merchants": 12, "products": 24000, "inventory": 96000, "buyers": 2000,
		"tasks": int64(taskCount), "intent_parse_traces": int64(taskCount),
		"quotes": int64(flows + priceChanges + stockChanges), "confirmations": int64(flows),
		"confirmation_attempts": int64(flows + priceChanges + stockChanges), "orders": int64(flows),
		"reservations": int64(flows), "inventory_adjustments": int64(priceChanges + 2*stockChanges),
		"simulated_payments": int64(flows),
	}
	for name, count := range expected {
		if observed[name] != count {
			violations["count_mismatch_"+name] = observed[name] - count
		}
	}
	var taskRows []struct {
		Status string
		Count  int64
	}
	statuses := make(map[string]int64)
	if err := store.db.WithContext(ctx).Model(&ShoppingTaskRecord{}).Select("status, COUNT(*) AS count").
		Where("tenant_id = ?", tenant).Group("status").Scan(&taskRows).Error; err != nil {
		violations["task_status_query_error"] = 1
	} else {
		for _, row := range taskRows {
			statuses[row.Status] = row.Count
		}
	}
	var auditRows []struct {
		Action string
		Count  int64
	}
	actions := make(map[string]int64)
	if err := store.db.WithContext(ctx).Model(&SimulationAuditRecord{}).Select("action, COUNT(*) AS count").
		Where("tenant_id = ?", tenant).Group("action").Scan(&auditRows).Error; err != nil {
		violations["audit_action_query_error"] = 1
	} else {
		for _, row := range auditRows {
			actions[row.Action] = row.Count
		}
	}
	queries := map[string]string{
		"orphan_order_aggregates": `SELECT COUNT(*) FROM sim_orders o
			LEFT JOIN sim_quotes q ON q.tenant_id=o.tenant_id AND q.quote_id=o.quote_id
			LEFT JOIN sim_buyer_confirmations c ON c.tenant_id=o.tenant_id AND c.confirmation_id=o.confirmation_id
			LEFT JOIN simulated_payments p ON p.tenant_id=o.tenant_id AND p.order_id=o.order_id
			LEFT JOIN sim_inventory_reservations r ON r.tenant_id=o.tenant_id AND r.order_id=o.order_id
			WHERE o.tenant_id=? AND (q.quote_id IS NULL OR c.confirmation_id IS NULL OR p.payment_id IS NULL OR r.reservation_id IS NULL
			OR q.amount_minor<>o.amount_minor OR c.amount_minor<>o.amount_minor OR p.amount_minor<>o.amount_minor)`,
		"reservation_leaks": `SELECT COUNT(*) FROM sim_inventory WHERE tenant_id=? AND reserved<>0`,
		"non_mock_payments": `SELECT COUNT(*) FROM simulated_payments WHERE tenant_id=? AND (provider<>'simulation-only' OR data_origin<>'SYNTHETIC_SIMULATION')`,
		"payment_order_status_mismatches": `SELECT COUNT(*) FROM sim_orders o JOIN simulated_payments p ON p.tenant_id=o.tenant_id AND p.order_id=o.order_id
			WHERE o.tenant_id=? AND ((o.status='PAID' AND p.status<>'SETTLED') OR (o.status='PAYMENT_FAILED' AND p.status<>'DECLINED')
			OR o.status NOT IN ('PAID','PAYMENT_FAILED'))`,
		"unfinished_tasks": `SELECT COUNT(*) FROM sim_shopping_tasks WHERE tenant_id=?
			AND status NOT IN ('PAID','PAYMENT_FAILED','NO_MATCHING_CANDIDATES','INTENT_PARSE_FAILED','CLARIFICATION_REQUIRED')`,
	}
	for name, query := range queries {
		var count int64
		if err := store.db.WithContext(ctx).Raw(query, tenant).Scan(&count).Error; err != nil {
			violations["query_error_"+name] = 1
		} else if count != 0 {
			violations[name] = count
		}
	}
	var orders []OrderRecord
	if err := store.db.WithContext(ctx).Where("tenant_id = ?", tenant).Find(&orders).Error; err != nil {
		violations["order_task_query_error"] = 1
	} else {
		for _, order := range orders {
			if !executedOrderIDs[order.OrderID] {
				violations["order_for_unapproved_task"]++
			}
		}
	}
	var staleAttempts int64
	if err := store.db.WithContext(ctx).Model(&BuyerConfirmationAttemptRecord{}).
		Where("tenant_id = ? AND status = ?", tenant, "STALE_QUOTE").Count(&staleAttempts).Error; err != nil {
		violations["stale_quote_attempt_query_error"] = 1
	} else if staleAttempts != int64(priceChanges+stockChanges) {
		violations["stale_quote_attempt_count_mismatch"] = staleAttempts - int64(priceChanges+stockChanges)
	}
	if actions["MOCK_PAYMENT_SETTLED"]+actions["MOCK_PAYMENT_DECLINED"] != int64(flows) {
		violations["payment_audit_action_total"] = actions["MOCK_PAYMENT_SETTLED"] + actions["MOCK_PAYMENT_DECLINED"] - int64(flows)
	}
	if actions["BUYER_CONFIRMED"] != int64(flows) || actions["ORDER_RESERVED"] != int64(flows) ||
		actions["QUOTE_ISSUED"] != expected["quotes"] || actions["BUYER_CONFIRMATION_BLOCKED_STALE_QUOTE"] != int64(priceChanges+stockChanges) {
		violations["audit_action_count_mismatch"] = 1
	}
	return challengeHTTPLifecycleAudit{Passed: len(violations) == 0, ExpectedCounts: expected,
		ObservedCounts: observed, InvariantViolations: violations, TaskStatuses: statuses, AuditActions: actions}
}
