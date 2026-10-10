package agentshopping

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var shoppingHTTPConcurrencyLevels = []int{1, 10, 25, 50, 100, 200}

const (
	shoppingHTTPConcurrentTaskCount = 500
	shoppingHTTPConcurrentWarmup    = shoppingHTTPConcurrentTaskCount
)

type shoppingHTTPConcurrentLoadTrial struct {
	Concurrency    int     `json:"concurrency"`
	TaskIndex      int     `json:"task_index"`
	TaskID         string  `json:"task_id"`
	Warmup         bool    `json:"warmup"`
	HTTPStatus     int     `json:"http_status"`
	LatencyMillis  float64 `json:"latency_ms"`
	TaskStatus     string  `json:"task_status,omitempty"`
	CandidateCount int     `json:"candidate_count"`
	Top1IsGold     bool    `json:"top1_is_gold"`
	Error          string  `json:"error,omitempty"`
}

type shoppingHTTPConcurrentTierMetrics struct {
	Status                     string           `json:"status"`
	Concurrency                int              `json:"concurrency"`
	WarmupRequests             int              `json:"warmup_requests"`
	WarmupErrors               int              `json:"warmup_errors"`
	WarmupDurationMillis       float64          `json:"warmup_duration_ms"`
	MeasurementRequests        int              `json:"measurement_requests"`
	CompletedSearches          int              `json:"completed_searches"`
	HTTPStatusCounts           map[string]int64 `json:"http_status_counts"`
	UnexpectedHTTPFailures     int              `json:"unexpected_http_failures"`
	BusinessErrors             int              `json:"business_errors"`
	Top1GoldHits               int              `json:"top1_gold_hits"`
	Top1GoldRate               float64          `json:"top1_gold_rate"`
	MeanCandidateCount         float64          `json:"mean_candidate_count"`
	MeasurementDurationMillis  float64          `json:"measurement_duration_ms"`
	CompletedSearchesPerSecond float64          `json:"completed_searches_per_second"`
	LatencyP50Millis           float64          `json:"latency_p50_ms"`
	LatencyP95Millis           float64          `json:"latency_p95_ms"`
	MySQLMaxOpenConnections    int              `json:"mysql_max_open_connections"`
	MySQLPoolWaitCount         int64            `json:"mysql_pool_wait_count"`
	MySQLPoolWaitMillis        float64          `json:"mysql_pool_wait_ms"`
}

type shoppingHTTPConcurrentLoadMetrics struct {
	SchemaVersion       string                              `json:"schema_version"`
	Status              string                              `json:"status"`
	RunID               string                              `json:"run_id"`
	TenantID            string                              `json:"tenant_id"`
	Database            string                              `json:"database"`
	Endpoint            string                              `json:"endpoint"`
	CatalogSource       string                              `json:"catalog_source"`
	CatalogMerchants    int                                 `json:"catalog_merchants"`
	CatalogProducts     int                                 `json:"catalog_products"`
	TaskCountPerTier    int                                 `json:"task_count_per_tier"`
	ConcurrencyLevels   []int                               `json:"concurrency_levels"`
	SourceIntentRunID   string                              `json:"source_intent_run_id"`
	Provider            string                              `json:"provider"`
	Model               string                              `json:"model"`
	GoVersion           string                              `json:"go_version"`
	HostOS              string                              `json:"host_os"`
	HostArchitecture    string                              `json:"host_architecture"`
	HostCPUCount        int                                 `json:"host_cpu_count"`
	GoMaxProcs          int                                 `json:"go_max_procs"`
	MySQLServerVersion  string                              `json:"mysql_server_version"`
	MySQLServerMaxConns int                                 `json:"mysql_server_max_connections"`
	MySQLPoolMaxOpen    int                                 `json:"mysql_pool_max_open_connections"`
	MySQLPoolMaxIdle    int                                 `json:"mysql_pool_max_idle_connections"`
	NewModelCalls       int                                 `json:"new_model_calls"`
	TokenUsage          string                              `json:"token_usage"`
	Hashes              EvaluationHashes                    `json:"hashes"`
	Audit               shoppingHTTPConcurrentLoadAudit     `json:"audit"`
	Tiers               []shoppingHTTPConcurrentTierMetrics `json:"tiers"`
}

type shoppingHTTPConcurrentLoadAudit struct {
	Passed              bool             `json:"passed"`
	ExpectedCounts      map[string]int64 `json:"expected_counts"`
	ObservedCounts      map[string]int64 `json:"observed_counts"`
	DiscoveredTaskCount int64            `json:"discovered_task_count"`
	InvariantViolations map[string]int64 `json:"invariant_violations"`
}

type shoppingHTTPConcurrentLoadJob struct {
	task       Task
	buyerID    string
	key        string
	index      int
	concurrent int
	warmup     bool
}

type shoppingHTTPConcurrentLoadResponse struct {
	TaskID                    string      `json:"task_id"`
	TaskStatus                string      `json:"task_status"`
	Intent                    Intent      `json:"intent"`
	Candidates                []Candidate `json:"candidates"`
	Provider                  string      `json:"provider"`
	Model                     string      `json:"model"`
	BuyerConfirmationRequired bool        `json:"buyer_confirmation_required"`
}

func TestMySQLHTTPConcurrentDiscoveryLoad(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_HTTP_CONCURRENT_LOAD") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_HTTP_CONCURRENT_LOAD=1 to run the recorded-intent concurrent HTTP load against isolated MySQL")
	}
	password := os.Getenv("SHOP_SIM_DB_PASSWORD")
	if strings.TrimSpace(password) == "" {
		t.Fatal("SHOP_SIM_DB_PASSWORD is required")
	}
	repoRoot, err := filepath.Abs(os.Getenv("SHOP_SIM_REPO_ROOT"))
	if err != nil || repoRoot == "." {
		t.Fatal("SHOP_SIM_REPO_ROOT must point to the StablePay repository")
	}
	suitePath, intentMetricsPath, sourceManifestPath := os.Getenv("SHOP_SIM_LOCKED_SUITE_PATH"),
		os.Getenv("SHOP_SIM_INTENT_METRICS_PATH"), os.Getenv("SHOP_SIM_SOURCE_MANIFEST_PATH")
	for label, path := range map[string]string{"locked suite": suitePath, "intent metrics": intentMetricsPath, "source manifest": sourceManifestPath} {
		if path == "" {
			t.Fatalf("missing %s path", label)
		}
	}
	tasks, err := readLifecycleSuite(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	var intentRun httpLifecycleIntentRun
	if err := readLifecycleJSON(intentMetricsPath, &intentRun); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != shoppingHTTPConcurrentTaskCount || intentRun.Provider != "openai-codex" ||
		intentRun.Model != "gpt-6-luna" || intentRun.Metrics.TaskCount != shoppingHTTPConcurrentTaskCount ||
		intentRun.Metrics.RawModelExactMatches != shoppingHTTPConcurrentTaskCount ||
		intentRun.Metrics.ExactIntentMatches != shoppingHTTPConcurrentTaskCount {
		t.Fatalf("concurrent load requires the locked 500/500 GPT-6 Luna intent run: run=%q tasks=%d provider=%q model=%q",
			intentRun.RunID, len(tasks), intentRun.Provider, intentRun.Model)
	}
	intentTrials, err := readLifecycleIntentTrials(filepath.Join(filepath.Dir(intentMetricsPath), "intent-trials.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	parser := replayLifecycleIntents(t, tasks, intentTrials, intentRun.PromptTemplateSHA256)
	manifestBytes, err := os.ReadFile(sourceManifestPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated concurrent-load MySQL: %v", err)
		}
	})
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	sourceTenant := store.tenant
	suffix := mysqlIntegrationSuffix(t)
	tenantID := "shopping-load-" + suffix
	catalog, buyers, err := cloneLifecycleTenant(ctx, store, sourceTenant, tenantID, 20261010)
	if err != nil {
		t.Fatal(err)
	}
	store.tenant = tenantID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cleanupCancel()
		if err := cleanupMySQLIntegrationTenant(cleanupCtx, store, tenantID); err != nil {
			t.Errorf("clean isolated concurrent-load tenant %q: %v", tenantID, err)
			return
		}
		counts, err := store.Counts(cleanupCtx)
		if err != nil {
			t.Errorf("verify concurrent-load tenant cleanup: %v", err)
			return
		}
		for table, count := range counts {
			if count != 0 {
				t.Errorf("concurrent-load tenant retained %d rows in %s", count, table)
			}
		}
	})
	poolLimit, err := shoppingHTTPMySQLPoolLimit()
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(poolLimit)
	database.SetMaxIdleConns(poolLimit)
	var mysqlVersion string
	if err := store.db.WithContext(ctx).Raw("SELECT VERSION()").Scan(&mysqlVersion).Error; err != nil {
		t.Fatalf("read isolated MySQL version: %v", err)
	}
	var mysqlServerMaxConns int
	if err := store.db.WithContext(ctx).Raw("SELECT @@GLOBAL.max_connections").Scan(&mysqlServerMaxConns).Error; err != nil {
		t.Fatalf("read isolated MySQL connection limit: %v", err)
	}
	if err := ValidateLockedSuite(catalog, tasks); err != nil {
		t.Fatalf("locked task suite validation: %v", err)
	}
	hashes, err := ComputeEvaluationHashes(catalog, tasks, manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	if hashes != intentRun.Hashes {
		t.Fatalf("recorded intent source no longer matches the locked catalog/suite: current=%+v source=%+v", hashes, intentRun.Hashes)
	}
	merchantCatalog, err := LoadMerchantCatalog(ctx, sourceManifestPath, nil)
	if err != nil {
		t.Fatalf("load live Merchant HTTP catalog: %v", err)
	}
	if err := store.ValidateMerchantCatalogSnapshot(ctx, merchantCatalog); err != nil {
		t.Fatalf("Merchant HTTP catalog differs from isolated MySQL snapshot: %v", err)
	}
	merchantHashes, err := ComputeEvaluationHashes(merchantCatalog, tasks, manifestBytes)
	if err != nil || merchantHashes != hashes {
		t.Fatalf("Merchant HTTP snapshot hash mismatch: live=%+v expected=%+v err=%v", merchantHashes, hashes, err)
	}

	api, err := NewHTTPAPIWithMerchantCatalog(store, parser, mysqlIntegrationAPIKey, merchantCatalog)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	readyResponse, err := server.Client().Get(server.URL + "/readyz")
	if err != nil {
		t.Fatalf("shopping API readiness request: %v", err)
	}
	readyBody, readErr := io.ReadAll(readyResponse.Body)
	_ = readyResponse.Body.Close()
	if readErr != nil || readyResponse.StatusCode != http.StatusOK || !strings.Contains(string(readyBody), `"status":"ready"`) {
		t.Fatalf("shopping API not ready: status=%d body=%s err=%v", readyResponse.StatusCode, readyBody, readErr)
	}

	outputDir, runID, err := lifecycleEvidenceDirectory(repoRoot, "http-concurrent-"+suffix)
	if err != nil {
		t.Fatal(err)
	}
	metrics := shoppingHTTPConcurrentLoadMetrics{SchemaVersion: "stablepay.agent-shopping-http-concurrent-load.v1",
		Status: "RUNNING", RunID: runID, TenantID: tenantID, Database: SimulationDatabaseName,
		Endpoint: "POST /v1/intents/parse", CatalogSource: "merchant_http_snapshot",
		CatalogMerchants: len(merchantCatalog.Merchants), CatalogProducts: len(merchantCatalog.Products),
		TaskCountPerTier: len(tasks), ConcurrencyLevels: append([]int(nil), shoppingHTTPConcurrencyLevels...),
		SourceIntentRunID: intentRun.RunID, Provider: parser.Provider(), Model: parser.Model(),
		GoVersion: runtime.Version(), HostOS: runtime.GOOS, HostArchitecture: runtime.GOARCH,
		HostCPUCount: runtime.NumCPU(), GoMaxProcs: runtime.GOMAXPROCS(0),
		MySQLServerVersion: mysqlVersion, MySQLServerMaxConns: mysqlServerMaxConns,
		MySQLPoolMaxOpen: poolLimit, MySQLPoolMaxIdle: poolLimit,
		NewModelCalls: 0, TokenUsage: "NOT_APPLICABLE_RECORDED_RESPONSE_REPLAY", Hashes: hashes}
	manifest := map[string]any{"schema_version": metrics.SchemaVersion, "status": "RUNNING", "run_id": runID,
		"tenant_id": tenantID, "database": SimulationDatabaseName, "endpoint": metrics.Endpoint,
		"load_model":        "closed-loop fixed worker pool; each tier runs the full 500-task set once as warmup, then 500 measured requests; not a steady-duration k6/VU test",
		"http_boundary":     "in-process httptest listener; HTTP requests execute concurrent task/trace writes and MySQL inventory reads",
		"catalog_source":    "12 local Merchant HTTP fixtures loaded as an immutable in-memory snapshot",
		"catalog_merchants": len(merchantCatalog.Merchants), "catalog_products": len(merchantCatalog.Products),
		"intent_source":        "locked recorded GPT-6 Luna intents; replay only, no model call",
		"source_intent_run_id": intentRun.RunID, "provider": parser.Provider(), "model": parser.Model(),
		"new_model_calls": 0, "token_usage": "NOT_APPLICABLE_RECORDED_RESPONSE_REPLAY",
		"task_count_per_tier": len(tasks), "warmup_requests_per_tier": shoppingHTTPConcurrentWarmup,
		"concurrency_levels": shoppingHTTPConcurrencyLevels, "mysql_pool_max_open_connections": poolLimit,
		"mysql_pool_max_idle_connections": poolLimit,
		"mysql_server_version":            mysqlVersion, "mysql_server_max_connections": mysqlServerMaxConns,
		"go_version": runtime.Version(), "host_os": runtime.GOOS, "host_architecture": runtime.GOARCH,
		"host_cpu_count": runtime.NumCPU(), "go_max_procs": runtime.GOMAXPROCS(0),
		"simulation_only": true, "real_payment": false, "api_key_recorded": false,
		"hashes": hashes, "created_at_utc": time.Now().UTC()}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}

	allTrials := make([]shoppingHTTPConcurrentLoadTrial, 0,
		len(shoppingHTTPConcurrencyLevels)*(shoppingHTTPConcurrentTaskCount+shoppingHTTPConcurrentWarmup))
	allTiersPassed := true
	for _, concurrency := range shoppingHTTPConcurrencyLevels {
		warmupJobs, err := makeShoppingHTTPConcurrentLoadJobs(tasks[:shoppingHTTPConcurrentWarmup], buyers,
			fmt.Sprintf("warmup-%d-%s", concurrency, suffix), concurrency, true)
		if err != nil {
			t.Fatal(err)
		}
		client := newShoppingHTTPConcurrentLoadClient(concurrency)
		warmupTrials, warmupDuration, err := runShoppingHTTPConcurrentLoadBatch(ctx, client, server.URL, warmupJobs)
		if err != nil {
			client.CloseIdleConnections()
			t.Fatal(err)
		}
		poolBefore := sqlStats(store)
		measureJobs, err := makeShoppingHTTPConcurrentLoadJobs(tasks, buyers,
			fmt.Sprintf("measure-%d-%s", concurrency, suffix), concurrency, false)
		if err != nil {
			client.CloseIdleConnections()
			t.Fatal(err)
		}
		measureTrials, duration, err := runShoppingHTTPConcurrentLoadBatch(ctx, client, server.URL, measureJobs)
		poolAfter := sqlStats(store)
		client.CloseIdleConnections()
		if err != nil {
			t.Fatal(err)
		}
		tier := summarizeShoppingHTTPConcurrentTier(concurrency, warmupTrials, measureTrials, duration, poolBefore, poolAfter)
		tier.WarmupDurationMillis = float64(warmupDuration.Microseconds()) / 1000
		metrics.Tiers = append(metrics.Tiers, tier)
		allTrials = append(allTrials, warmupTrials...)
		allTrials = append(allTrials, measureTrials...)
		if tier.Status != "PASS" {
			allTiersPassed = false
		}
		t.Logf("SHOPPING_HTTP_CONCURRENT_TIER status=%s concurrency=%d warmup_errors=%d measured=%d http_errors=%d business_errors=%d top1_gold=%d/%d rps=%.2f p50_ms=%.2f p95_ms=%.2f mysql_pool_waits=%d mysql_pool_wait_ms=%.2f",
			tier.Status, tier.Concurrency, tier.WarmupErrors, tier.MeasurementRequests,
			tier.UnexpectedHTTPFailures, tier.BusinessErrors, tier.Top1GoldHits, tier.MeasurementRequests,
			tier.CompletedSearchesPerSecond, tier.LatencyP50Millis, tier.LatencyP95Millis,
			tier.MySQLPoolWaitCount, tier.MySQLPoolWaitMillis)
	}

	metrics.Audit = auditShoppingHTTPConcurrentLoad(ctx, store, tenantID,
		int64(len(shoppingHTTPConcurrencyLevels)*(shoppingHTTPConcurrentTaskCount+shoppingHTTPConcurrentWarmup)))
	metrics.Status = "PASS"
	for _, tier := range metrics.Tiers {
		if tier.Status != "PASS" {
			metrics.Status = "FAIL"
		}
	}
	if !metrics.Audit.Passed || !allTiersPassed {
		metrics.Status = "FAIL"
	}
	manifest["status"] = metrics.Status
	manifest["audit_passed"] = metrics.Audit.Passed
	if err := writeLifecycleJSON(filepath.Join(outputDir, "metrics.json"), metrics); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "audit-summary.json"), metrics.Audit); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writeLifecycleLines(filepath.Join(outputDir, "trials.jsonl"), allTrials); err != nil {
		t.Fatal(err)
	}
	if metrics.Status != "PASS" {
		t.Errorf("concurrent shopping HTTP load gate failed; inspect %s", outputDir)
	}
}

func makeShoppingHTTPConcurrentLoadJobs(tasks []Task, buyers []BuyerRecord, prefix string,
	concurrency int, warmup bool) ([]shoppingHTTPConcurrentLoadJob, error) {
	byRegion := make(map[string][]BuyerRecord)
	for _, buyer := range buyers {
		byRegion[buyer.HomeRegion] = append(byRegion[buyer.HomeRegion], buyer)
	}
	used := make(map[string]bool, len(tasks))
	jobs := make([]shoppingHTTPConcurrentLoadJob, 0, len(tasks))
	for index, task := range tasks {
		buyer, ok := chooseLifecycleBuyer(byRegion[task.Intent.Region], task.Intent.MaxPriceMinor, used)
		if !ok {
			return nil, fmt.Errorf("no unique load buyer for task %s in region %s under budget %d",
				task.TaskID, task.Intent.Region, task.Intent.MaxPriceMinor)
		}
		jobs = append(jobs, shoppingHTTPConcurrentLoadJob{task: task, buyerID: buyer.BuyerID,
			key: "http-load-" + prefix + "-" + task.TaskID, index: index,
			concurrent: concurrency, warmup: warmup})
	}
	return jobs, nil
}

func shoppingHTTPMySQLPoolLimit() (int, error) {
	const defaultLimit = 24
	const maxAllowedLimit = 128
	raw := strings.TrimSpace(os.Getenv("SHOP_SIM_MYSQL_POOL_MAX_OPEN"))
	if raw == "" {
		return defaultLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxAllowedLimit {
		return 0, fmt.Errorf("SHOP_SIM_MYSQL_POOL_MAX_OPEN must be an integer from 1 to %d", maxAllowedLimit)
	}
	return limit, nil
}

func TestShoppingHTTPMySQLPoolLimit(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("SHOP_SIM_MYSQL_POOL_MAX_OPEN", "")
		limit, err := shoppingHTTPMySQLPoolLimit()
		if err != nil || limit != 24 {
			t.Fatalf("default pool limit=%d err=%v, want 24/nil", limit, err)
		}
	})
	t.Run("override", func(t *testing.T) {
		t.Setenv("SHOP_SIM_MYSQL_POOL_MAX_OPEN", "48")
		limit, err := shoppingHTTPMySQLPoolLimit()
		if err != nil || limit != 48 {
			t.Fatalf("override pool limit=%d err=%v, want 48/nil", limit, err)
		}
	})
	t.Run("rejects unsafe values", func(t *testing.T) {
		for _, value := range []string{"0", "129", "many"} {
			t.Setenv("SHOP_SIM_MYSQL_POOL_MAX_OPEN", value)
			if _, err := shoppingHTTPMySQLPoolLimit(); err == nil {
				t.Errorf("pool limit %q was accepted", value)
			}
		}
	})
}

func newShoppingHTTPConcurrentLoadClient(concurrency int) *http.Client {
	transport := &http.Transport{MaxIdleConns: concurrency, MaxIdleConnsPerHost: concurrency,
		MaxConnsPerHost: concurrency, IdleConnTimeout: 30 * time.Second}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}
}

func runShoppingHTTPConcurrentLoadBatch(ctx context.Context, client *http.Client, apiURL string,
	jobs []shoppingHTTPConcurrentLoadJob) ([]shoppingHTTPConcurrentLoadTrial, time.Duration, error) {
	if len(jobs) == 0 {
		return nil, 0, fmt.Errorf("concurrent shopping load batch is empty")
	}
	concurrency := jobs[0].concurrent
	if concurrency < 1 {
		return nil, 0, fmt.Errorf("invalid shopping load concurrency %d", concurrency)
	}
	workerCount := min(concurrency, len(jobs))
	jobChannel := make(chan shoppingHTTPConcurrentLoadJob)
	resultChannel := make(chan shoppingHTTPConcurrentLoadTrial, len(jobs))
	var workers sync.WaitGroup
	started := time.Now()
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobChannel {
				resultChannel <- executeShoppingHTTPConcurrentLoadJob(ctx, client, apiURL, job)
			}
		}()
	}
	for _, job := range jobs {
		jobChannel <- job
	}
	close(jobChannel)
	workers.Wait()
	duration := time.Since(started)
	close(resultChannel)
	results := make([]shoppingHTTPConcurrentLoadTrial, 0, len(jobs))
	for result := range resultChannel {
		results = append(results, result)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].TaskIndex < results[j].TaskIndex })
	return results, duration, nil
}

func executeShoppingHTTPConcurrentLoadJob(ctx context.Context, client *http.Client, apiURL string,
	job shoppingHTTPConcurrentLoadJob) shoppingHTTPConcurrentLoadTrial {
	trial := shoppingHTTPConcurrentLoadTrial{Concurrency: job.concurrent, TaskIndex: job.index,
		TaskID: job.task.TaskID, Warmup: job.warmup}
	body, err := json.Marshal(map[string]any{"buyer_id": job.buyerID, "prompt": job.task.Prompt,
		"idempotency_key": job.key})
	if err != nil {
		trial.Error = "encode request body: " + err.Error()
		return trial
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/v1/intents/parse", strings.NewReader(string(body)))
	if err != nil {
		trial.Error = "create request: " + err.Error()
		return trial
	}
	request.Header.Set("X-Simulation-API-Key", mysqlIntegrationAPIKey)
	request.Header.Set("Content-Type", "application/json")
	started := time.Now()
	response, err := client.Do(request)
	trial.LatencyMillis = float64(time.Since(started).Microseconds()) / 1000
	if err != nil {
		trial.Error = "HTTP request failed: " + err.Error()
		return trial
	}
	trial.HTTPStatus = response.StatusCode
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
	_ = response.Body.Close()
	if readErr != nil {
		trial.Error = "read response body: " + readErr.Error()
		return trial
	}
	if response.StatusCode != http.StatusOK {
		trial.Error = fmt.Sprintf("unexpected HTTP status %d", response.StatusCode)
		return trial
	}
	var decoded shoppingHTTPConcurrentLoadResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		trial.Error = "decode HTTP response: " + err.Error()
		return trial
	}
	trial.TaskStatus = decoded.TaskStatus
	trial.CandidateCount = len(decoded.Candidates)
	if decoded.TaskID == "" || decoded.TaskStatus != TaskStatusDiscovered || decoded.Intent != job.task.Intent ||
		decoded.Provider != "openai-codex" || decoded.Model != "gpt-6-luna" ||
		!decoded.BuyerConfirmationRequired || len(decoded.Candidates) == 0 || len(decoded.Candidates) > 20 {
		trial.Error = "shopping API response violated the locked discovery contract"
		return trial
	}
	for _, goldSKU := range job.task.GoldSKUs {
		if decoded.Candidates[0].Product.SKUID == goldSKU {
			trial.Top1IsGold = true
			break
		}
	}
	return trial
}

func summarizeShoppingHTTPConcurrentTier(concurrency int, warmup, measured []shoppingHTTPConcurrentLoadTrial,
	duration time.Duration, before, after sql.DBStats) shoppingHTTPConcurrentTierMetrics {
	metrics := shoppingHTTPConcurrentTierMetrics{Status: "PASS", Concurrency: concurrency,
		WarmupRequests: len(warmup), MeasurementRequests: len(measured), HTTPStatusCounts: make(map[string]int64),
		MeasurementDurationMillis: float64(duration.Microseconds()) / 1000,
		MySQLMaxOpenConnections:   after.MaxOpenConnections,
		MySQLPoolWaitCount:        after.WaitCount - before.WaitCount,
		MySQLPoolWaitMillis:       float64((after.WaitDuration - before.WaitDuration).Microseconds()) / 1000}
	var latencies []float64
	var totalCandidates int64
	for _, trial := range warmup {
		if trial.HTTPStatus != 0 {
			metrics.HTTPStatusCounts[fmt.Sprint(trial.HTTPStatus)]++
		}
		if trial.Error != "" || trial.HTTPStatus != http.StatusOK || !trial.Top1IsGold {
			metrics.WarmupErrors++
		}
	}
	for _, trial := range measured {
		if trial.HTTPStatus != 0 {
			metrics.HTTPStatusCounts[fmt.Sprint(trial.HTTPStatus)]++
		}
		latencies = append(latencies, trial.LatencyMillis)
		totalCandidates += int64(trial.CandidateCount)
		if trial.HTTPStatus != http.StatusOK {
			metrics.UnexpectedHTTPFailures++
		}
		if trial.Error != "" || !trial.Top1IsGold || trial.TaskStatus != TaskStatusDiscovered {
			metrics.BusinessErrors++
		}
		if trial.Top1IsGold {
			metrics.Top1GoldHits++
		}
		if trial.Error == "" && trial.HTTPStatus == http.StatusOK && trial.Top1IsGold && trial.TaskStatus == TaskStatusDiscovered {
			metrics.CompletedSearches++
		}
	}
	if len(measured) > 0 {
		metrics.Top1GoldRate = float64(metrics.Top1GoldHits) / float64(len(measured))
		metrics.MeanCandidateCount = float64(totalCandidates) / float64(len(measured))
	}
	metrics.LatencyP50Millis = lifecyclePercentile(latencies, 0.50)
	metrics.LatencyP95Millis = lifecyclePercentile(latencies, 0.95)
	if duration > 0 {
		metrics.CompletedSearchesPerSecond = float64(metrics.CompletedSearches) / duration.Seconds()
	}
	if metrics.WarmupErrors != 0 || len(measured) != shoppingHTTPConcurrentTaskCount ||
		metrics.UnexpectedHTTPFailures != 0 || metrics.BusinessErrors != 0 ||
		metrics.Top1GoldHits != shoppingHTTPConcurrentTaskCount || metrics.CompletedSearches != shoppingHTTPConcurrentTaskCount {
		metrics.Status = "FAIL"
	}
	return metrics
}

func auditShoppingHTTPConcurrentLoad(ctx context.Context, store *MySQLStore, tenant string,
	expectedTaskRows int64) shoppingHTTPConcurrentLoadAudit {
	audit := shoppingHTTPConcurrentLoadAudit{ExpectedCounts: map[string]int64{
		"merchants": 12, "products": 24000, "inventory": 96000, "buyers": 2000,
		"tasks": expectedTaskRows, "intent_parse_traces": expectedTaskRows,
		"quotes": 0, "confirmations": 0, "confirmation_attempts": 0, "orders": 0,
		"reservations": 0, "inventory_adjustments": 0, "simulated_payments": 0, "audit_events": 0,
	}, ObservedCounts: make(map[string]int64), InvariantViolations: make(map[string]int64)}
	counts, err := store.Counts(ctx)
	if err != nil {
		audit.InvariantViolations["counts_query_failed"] = 1
		audit.Passed = false
		return audit
	}
	for key, value := range counts {
		audit.ObservedCounts[key] = value
	}
	var discovered int64
	if err := store.db.WithContext(ctx).Model(&ShoppingTaskRecord{}).
		Where("tenant_id = ? AND status = ?", tenant, TaskStatusDiscovered).Count(&discovered).Error; err != nil {
		audit.InvariantViolations["discovered_tasks_query_failed"] = 1
	} else {
		audit.DiscoveredTaskCount = discovered
	}
	for name, expected := range audit.ExpectedCounts {
		if observed := audit.ObservedCounts[name]; observed != expected {
			audit.InvariantViolations[name] = observed
		}
	}
	if discovered != expectedTaskRows {
		audit.InvariantViolations["discovered_tasks"] = discovered
	}
	audit.Passed = len(audit.InvariantViolations) == 0
	return audit
}

func sqlStats(store *MySQLStore) sql.DBStats {
	database, err := store.db.DB()
	if err != nil {
		return sql.DBStats{}
	}
	return database.Stats()
}
