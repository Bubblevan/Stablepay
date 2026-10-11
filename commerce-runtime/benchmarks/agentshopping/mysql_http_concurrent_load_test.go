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
	"reflect"
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
	shoppingHTTPConcurrentTaskCount    = 500
	shoppingHTTPConcurrentWarmup       = shoppingHTTPConcurrentTaskCount
	shoppingHTTPMySQLConnectionReserve = 8
)

type shoppingHTTPConcurrentLoadTrial struct {
	Concurrency         int     `json:"concurrency"`
	TaskIndex           int     `json:"task_index"`
	TaskID              string  `json:"task_id"`
	Warmup              bool    `json:"warmup"`
	HTTPStatus          int     `json:"http_status"`
	LatencyMillis       float64 `json:"latency_ms"`
	TaskSetupMillis     float64 `json:"task_setup_ms"`
	TaskSetupTimed      bool    `json:"task_setup_timed"`
	IntentParseMillis   float64 `json:"intent_parse_ms"`
	IntentParseTimed    bool    `json:"intent_parse_timed"`
	IntentPersistMillis float64 `json:"intent_persist_ms"`
	IntentPersistTimed  bool    `json:"intent_persist_timed"`
	CatalogSearchMillis float64 `json:"catalog_search_ms"`
	CatalogSearchTimed  bool    `json:"catalog_search_timed"`
	TaskStatus          string  `json:"task_status,omitempty"`
	CandidateCount      int     `json:"candidate_count"`
	Top1IsGold          bool    `json:"top1_is_gold"`
	Error               string  `json:"error,omitempty"`
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
	TaskSetupTimedRequests     int              `json:"task_setup_timed_requests"`
	TaskSetupP50Millis         float64          `json:"task_setup_p50_ms"`
	TaskSetupP95Millis         float64          `json:"task_setup_p95_ms"`
	IntentParseTimedRequests   int              `json:"intent_parse_timed_requests"`
	IntentParseP50Millis       float64          `json:"intent_parse_p50_ms"`
	IntentParseP95Millis       float64          `json:"intent_parse_p95_ms"`
	IntentPersistTimedRequests int              `json:"intent_persist_timed_requests"`
	IntentPersistP50Millis     float64          `json:"intent_persist_p50_ms"`
	IntentPersistP95Millis     float64          `json:"intent_persist_p95_ms"`
	CatalogSearchTimedRequests int              `json:"catalog_search_timed_requests"`
	CatalogSearchP50Millis     float64          `json:"catalog_search_p50_ms"`
	CatalogSearchP95Millis     float64          `json:"catalog_search_p95_ms"`
	MySQLMaxOpenConnections    int              `json:"mysql_max_open_connections"`
	MySQLPoolWaitCount         int64            `json:"mysql_pool_wait_count"`
	MySQLPoolWaitMillis        float64          `json:"mysql_pool_wait_ms"`
}

type shoppingHTTPConcurrentLoadMetrics struct {
	SchemaVersion                    string                                 `json:"schema_version"`
	Status                           string                                 `json:"status"`
	RunID                            string                                 `json:"run_id"`
	TenantID                         string                                 `json:"tenant_id"`
	Database                         string                                 `json:"database"`
	Endpoint                         string                                 `json:"endpoint"`
	CatalogSource                    string                                 `json:"catalog_source"`
	CatalogMerchants                 int                                    `json:"catalog_merchants"`
	CatalogProducts                  int                                    `json:"catalog_products"`
	TaskCountPerTier                 int                                    `json:"task_count_per_tier"`
	ConcurrencyLevels                []int                                  `json:"concurrency_levels"`
	SourceIntentRunID                string                                 `json:"source_intent_run_id"`
	Provider                         string                                 `json:"provider"`
	Model                            string                                 `json:"model"`
	GoVersion                        string                                 `json:"go_version"`
	HostOS                           string                                 `json:"host_os"`
	HostArchitecture                 string                                 `json:"host_architecture"`
	HostCPUCount                     int                                    `json:"host_cpu_count"`
	GoMaxProcs                       int                                    `json:"go_max_procs"`
	HostAvailableRAMGB               float64                                `json:"host_available_ram_gb,omitempty"`
	HostSystemDiskFreeGB             float64                                `json:"host_system_disk_free_gb,omitempty"`
	HostRepositoryDiskFreeGB         float64                                `json:"host_repository_disk_free_gb,omitempty"`
	MySQLServerVersion               string                                 `json:"mysql_server_version"`
	MySQLServerMaxConns              int                                    `json:"mysql_server_max_connections"`
	MySQLThreadsConnectedAtPreflight int                                    `json:"mysql_threads_connected_at_preflight"`
	MySQLPoolOpenAtPreflight         int                                    `json:"mysql_pool_open_at_preflight"`
	MySQLOtherConnectionsAtPreflight int                                    `json:"mysql_other_connections_at_preflight"`
	MySQLConnectionReserve           int                                    `json:"mysql_connection_reserve"`
	MySQLPoolMaxOpen                 int                                    `json:"mysql_pool_max_open_connections"`
	MySQLPoolMaxIdle                 int                                    `json:"mysql_pool_max_idle_connections"`
	NewModelCalls                    int                                    `json:"new_model_calls"`
	TokenUsage                       string                                 `json:"token_usage"`
	Hashes                           EvaluationHashes                       `json:"hashes"`
	SourceIntentHashes               EvaluationHashes                       `json:"source_intent_hashes"`
	CatalogCompatibility             shoppingHTTPIntentCatalogCompatibility `json:"catalog_compatibility"`
	Audit                            shoppingHTTPConcurrentLoadAudit        `json:"audit"`
	Tiers                            []shoppingHTTPConcurrentTierMetrics    `json:"tiers"`
}

type shoppingHTTPIntentCatalogCompatibility struct {
	Status                       string `json:"status"`
	RecordedProductSchema        string `json:"recorded_product_schema"`
	ExecutionProductSchema       string `json:"execution_product_schema"`
	RecordedCatalogSHA256        string `json:"recorded_catalog_sha256"`
	ExecutionCatalogSHA256       string `json:"execution_catalog_sha256"`
	VerifiedLegacyProjectionHash string `json:"verified_legacy_projection_sha256"`
}

type shoppingHTTPConcurrentLoadAudit struct {
	Passed              bool             `json:"passed"`
	ExpectedCounts      map[string]int64 `json:"expected_counts"`
	ObservedCounts      map[string]int64 `json:"observed_counts"`
	DiscoveredTaskCount int64            `json:"discovered_task_count"`
	InvariantViolations map[string]int64 `json:"invariant_violations"`
}

type shoppingMySQLConnectionSnapshot struct {
	ServerVersion    string
	ServerMax        int
	ThreadsConnected int
	PoolOpen         int
	OtherConnections int
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

func TestMySQLHTTPConcurrentLoadConnectionPreflight(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_HTTP_CONCURRENT_PREFLIGHT") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_HTTP_CONCURRENT_PREFLIGHT=1 to check isolated MySQL connection headroom before starting Merchant fixtures")
	}
	password := os.Getenv("SHOP_SIM_DB_PASSWORD")
	if strings.TrimSpace(password) == "" {
		t.Fatal("SHOP_SIM_DB_PASSWORD is required for the local MySQL connection preflight")
	}
	poolLimit, err := shoppingHTTPMySQLPoolLimit()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated MySQL preflight connection: %v", err)
		}
	})
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot, err := inspectShoppingMySQLConnectionBudget(ctx, store, poolLimit)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("MYSQL_CONNECTION_PREFLIGHT pool_limit=%d max_connections=%d threads_connected=%d benchmark_pool_open=%d other_connections=%d reserved=%d",
		poolLimit, snapshot.ServerMax, snapshot.ThreadsConnected, snapshot.PoolOpen,
		snapshot.OtherConnections, shoppingHTTPMySQLConnectionReserve)
}

func inspectShoppingMySQLConnectionBudget(ctx context.Context, store *MySQLStore, poolLimit int) (shoppingMySQLConnectionSnapshot, error) {
	database, err := store.db.DB()
	if err != nil {
		return shoppingMySQLConnectionSnapshot{}, err
	}
	database.SetMaxOpenConns(poolLimit)
	database.SetMaxIdleConns(poolLimit)
	snapshot := shoppingMySQLConnectionSnapshot{}
	if err := store.db.WithContext(ctx).Raw("SELECT VERSION()").Scan(&snapshot.ServerVersion).Error; err != nil {
		return snapshot, fmt.Errorf("read isolated MySQL version: %w", err)
	}
	if err := store.db.WithContext(ctx).Raw("SELECT @@GLOBAL.max_connections").Scan(&snapshot.ServerMax).Error; err != nil {
		return snapshot, fmt.Errorf("read isolated MySQL connection limit: %w", err)
	}
	var status struct {
		VariableName string `gorm:"column:Variable_name"`
		Value        string `gorm:"column:Value"`
	}
	if err := store.db.WithContext(ctx).Raw("SHOW GLOBAL STATUS LIKE 'Threads_connected'").Scan(&status).Error; err != nil {
		return snapshot, fmt.Errorf("read isolated MySQL connected-thread count: %w", err)
	}
	if status.VariableName != "Threads_connected" {
		return snapshot, fmt.Errorf("MySQL did not return Threads_connected status (returned %q)", status.VariableName)
	}
	snapshot.ThreadsConnected, err = strconv.Atoi(status.Value)
	if err != nil {
		return snapshot, fmt.Errorf("invalid isolated MySQL Threads_connected value %q: %w", status.Value, err)
	}
	if snapshot.ThreadsConnected < 0 {
		return snapshot, fmt.Errorf("invalid negative MySQL Threads_connected value %d", snapshot.ThreadsConnected)
	}
	snapshot.PoolOpen = database.Stats().OpenConnections
	snapshot.OtherConnections, err = shoppingMySQLConcurrentLoadOtherConnections(poolLimit, snapshot.ServerMax,
		snapshot.ThreadsConnected, snapshot.PoolOpen, shoppingHTTPMySQLConnectionReserve)
	if err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func TestMySQLHTTPConcurrentDiscoveryLoad(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_HTTP_CONCURRENT_LOAD") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_HTTP_CONCURRENT_LOAD=1 to run the recorded-intent concurrent HTTP load against isolated MySQL")
	}
	concurrencyLevels, err := shoppingHTTPConcurrencyLevelsFromEnv(os.Getenv("SHOP_SIM_HTTP_CONCURRENCY_LEVELS"))
	if err != nil {
		t.Fatal(err)
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
	hostAvailableRAMGB := optionalResourceGBFromEnv(t, "SHOP_SIM_HOST_AVAILABLE_RAM_GB")
	hostSystemDiskFreeGB := optionalResourceGBFromEnv(t, "SHOP_SIM_HOST_SYSTEM_DISK_FREE_GB")
	hostRepositoryDiskFreeGB := optionalResourceGBFromEnv(t, "SHOP_SIM_HOST_REPOSITORY_DISK_FREE_GB")
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
	poolLimit, err := shoppingHTTPMySQLPoolLimit()
	if err != nil {
		t.Fatal(err)
	}
	mysqlPreflight, err := inspectShoppingMySQLConnectionBudget(ctx, store, poolLimit)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("MYSQL_CONNECTION_PREFLIGHT pool_limit=%d max_connections=%d threads_connected=%d benchmark_pool_open=%d other_connections=%d reserved=%d",
		poolLimit, mysqlPreflight.ServerMax, mysqlPreflight.ThreadsConnected, mysqlPreflight.PoolOpen,
		mysqlPreflight.OtherConnections, shoppingHTTPMySQLConnectionReserve)
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
	if err := ValidateLockedSuite(catalog, tasks); err != nil {
		t.Fatalf("locked task suite validation: %v", err)
	}
	merchantCatalog, err := LoadMerchantCatalog(ctx, sourceManifestPath, nil)
	if err != nil {
		t.Fatalf("load live Merchant HTTP catalog: %v", err)
	}
	if err := store.ValidateMerchantCatalogSnapshot(ctx, merchantCatalog); err != nil {
		t.Fatalf("Merchant HTTP catalog differs from isolated MySQL snapshot: %v", err)
	}
	hashes, legacyCatalogHash, err := validateRecordedIntentHashesV1(merchantCatalog, tasks, manifestBytes, intentRun.Hashes)
	if err != nil {
		t.Fatalf("recorded intent source is not compatible with the live Merchant execution catalog: %v", err)
	}
	catalogCompatibility := shoppingHTTPIntentCatalogCompatibility{Status: "PASS_V1_PRODUCT_PROJECTION",
		RecordedProductSchema: "stablepay.agent-shopping-product.v1", ExecutionProductSchema: "stablepay.agent-shopping-product.v2",
		RecordedCatalogSHA256: intentRun.Hashes.CatalogSHA256, ExecutionCatalogSHA256: hashes.CatalogSHA256,
		VerifiedLegacyProjectionHash: legacyCatalogHash}

	api, err := NewHTTPAPIWithMerchantCatalog(store, parser, mysqlIntegrationAPIKey, merchantCatalog, mysqlIntegrationBuyerApprovalKey)
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
	metrics := shoppingHTTPConcurrentLoadMetrics{SchemaVersion: "stablepay.agent-shopping-http-concurrent-load.v2",
		Status: "RUNNING", RunID: runID, TenantID: tenantID, Database: SimulationDatabaseName,
		Endpoint: "POST /v1/intents/parse", CatalogSource: "merchant_http_snapshot",
		CatalogMerchants: len(merchantCatalog.Merchants), CatalogProducts: len(merchantCatalog.Products),
		TaskCountPerTier: len(tasks), ConcurrencyLevels: append([]int(nil), concurrencyLevels...),
		SourceIntentRunID: intentRun.RunID, Provider: parser.Provider(), Model: parser.Model(),
		GoVersion: runtime.Version(), HostOS: runtime.GOOS, HostArchitecture: runtime.GOARCH,
		HostCPUCount: runtime.NumCPU(), GoMaxProcs: runtime.GOMAXPROCS(0),
		HostAvailableRAMGB: hostAvailableRAMGB, HostSystemDiskFreeGB: hostSystemDiskFreeGB,
		HostRepositoryDiskFreeGB: hostRepositoryDiskFreeGB,
		MySQLServerVersion:       mysqlPreflight.ServerVersion, MySQLServerMaxConns: mysqlPreflight.ServerMax,
		MySQLThreadsConnectedAtPreflight: mysqlPreflight.ThreadsConnected, MySQLPoolOpenAtPreflight: mysqlPreflight.PoolOpen,
		MySQLOtherConnectionsAtPreflight: mysqlPreflight.OtherConnections, MySQLConnectionReserve: shoppingHTTPMySQLConnectionReserve,
		MySQLPoolMaxOpen: poolLimit, MySQLPoolMaxIdle: poolLimit,
		NewModelCalls: 0, TokenUsage: "NOT_APPLICABLE_RECORDED_RESPONSE_REPLAY", Hashes: hashes,
		SourceIntentHashes: intentRun.Hashes, CatalogCompatibility: catalogCompatibility}
	manifest := map[string]any{"schema_version": metrics.SchemaVersion, "status": "RUNNING", "run_id": runID,
		"tenant_id": tenantID, "database": SimulationDatabaseName, "endpoint": metrics.Endpoint,
		"load_model":        "closed-loop fixed worker pool; each tier runs the full 500-task set once as warmup, then 500 measured requests; not a steady-duration k6/VU test",
		"http_boundary":     "in-process httptest listener; HTTP requests execute concurrent task/trace writes and MySQL inventory reads",
		"catalog_source":    "12 local Merchant HTTP fixtures loaded as an immutable in-memory snapshot",
		"catalog_merchants": len(merchantCatalog.Merchants), "catalog_products": len(merchantCatalog.Products),
		"intent_source":        "recorded GPT-6 Luna intent outputs replayed as fixed inputs; original intent hash binds the v1 Product projection, and exact v1 compatibility is revalidated for this execution; no current-parser quality claim",
		"source_intent_run_id": intentRun.RunID, "provider": parser.Provider(), "model": parser.Model(),
		"new_model_calls": 0, "token_usage": "NOT_APPLICABLE_RECORDED_RESPONSE_REPLAY",
		"task_count_per_tier": len(tasks), "warmup_requests_per_tier": shoppingHTTPConcurrentWarmup,
		"concurrency_levels": concurrencyLevels, "mysql_pool_max_open_connections": poolLimit,
		"mysql_pool_max_idle_connections": poolLimit,
		"mysql_server_version":            mysqlPreflight.ServerVersion, "mysql_server_max_connections": mysqlPreflight.ServerMax,
		"mysql_threads_connected_at_preflight": mysqlPreflight.ThreadsConnected, "mysql_pool_open_at_preflight": mysqlPreflight.PoolOpen,
		"mysql_other_connections_at_preflight": mysqlPreflight.OtherConnections, "mysql_connection_reserve": shoppingHTTPMySQLConnectionReserve,
		"go_version": runtime.Version(), "host_os": runtime.GOOS, "host_architecture": runtime.GOARCH,
		"host_cpu_count": runtime.NumCPU(), "go_max_procs": runtime.GOMAXPROCS(0),
		"host_available_ram_gb": hostAvailableRAMGB, "host_system_disk_free_gb": hostSystemDiskFreeGB,
		"host_repository_disk_free_gb": hostRepositoryDiskFreeGB,
		"simulation_only":              true, "real_payment": false, "api_key_recorded": false,
		"hashes": hashes, "source_intent_hashes": intentRun.Hashes, "catalog_compatibility": catalogCompatibility,
		"created_at_utc": time.Now().UTC()}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}

	allTrials := make([]shoppingHTTPConcurrentLoadTrial, 0,
		len(concurrencyLevels)*(shoppingHTTPConcurrentTaskCount+shoppingHTTPConcurrentWarmup))
	allTiersPassed := true
	for _, concurrency := range concurrencyLevels {
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
		t.Logf("SHOPPING_HTTP_CONCURRENT_TIER status=%s concurrency=%d warmup_errors=%d measured=%d http_errors=%d business_errors=%d top1_gold=%d/%d rps=%.2f p50_ms=%.2f p95_ms=%.2f setup_p50_ms=%.3f setup_p95_ms=%.3f parse_p50_ms=%.3f parse_p95_ms=%.3f persist_p50_ms=%.3f persist_p95_ms=%.3f catalog_p50_ms=%.3f catalog_p95_ms=%.3f mysql_pool_waits=%d mysql_pool_wait_ms=%.2f",
			tier.Status, tier.Concurrency, tier.WarmupErrors, tier.MeasurementRequests,
			tier.UnexpectedHTTPFailures, tier.BusinessErrors, tier.Top1GoldHits, tier.MeasurementRequests,
			tier.CompletedSearchesPerSecond, tier.LatencyP50Millis, tier.LatencyP95Millis,
			tier.TaskSetupP50Millis, tier.TaskSetupP95Millis, tier.IntentParseP50Millis, tier.IntentParseP95Millis,
			tier.IntentPersistP50Millis, tier.IntentPersistP95Millis, tier.CatalogSearchP50Millis, tier.CatalogSearchP95Millis,
			tier.MySQLPoolWaitCount, tier.MySQLPoolWaitMillis)
	}

	metrics.Audit = auditShoppingHTTPConcurrentLoad(ctx, store, tenantID,
		int64(len(concurrencyLevels)*(shoppingHTTPConcurrentTaskCount+shoppingHTTPConcurrentWarmup)))
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
	// MySQL 8.0.46 in the isolated harness allows 151 connections; the
	// connection preflight separately reserves eight and accounts for other
	// active clients before permitting a pool above the historical 128 cap.
	const maxAllowedLimit = 143
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

func shoppingHTTPConcurrencyLevelsFromEnv(raw string) ([]int, error) {
	if strings.TrimSpace(raw) == "" {
		return append([]int(nil), shoppingHTTPConcurrencyLevels...), nil
	}
	allowed := make(map[int]bool, len(shoppingHTTPConcurrencyLevels))
	for _, level := range shoppingHTTPConcurrencyLevels {
		allowed[level] = true
	}
	seen := make(map[int]bool)
	levels := make([]int, 0)
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		level, err := strconv.Atoi(value)
		if err != nil || !allowed[level] {
			return nil, fmt.Errorf("SHOP_SIM_HTTP_CONCURRENCY_LEVELS entries must be selected from %v", shoppingHTTPConcurrencyLevels)
		}
		if seen[level] {
			return nil, fmt.Errorf("SHOP_SIM_HTTP_CONCURRENCY_LEVELS contains duplicate tier %d", level)
		}
		seen[level] = true
		levels = append(levels, level)
	}
	if len(levels) == 0 {
		return nil, fmt.Errorf("SHOP_SIM_HTTP_CONCURRENCY_LEVELS must select at least one tier")
	}
	return levels, nil
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
	t.Run("upper limit preserves the MySQL connection reserve", func(t *testing.T) {
		t.Setenv("SHOP_SIM_MYSQL_POOL_MAX_OPEN", "143")
		limit, err := shoppingHTTPMySQLPoolLimit()
		if err != nil || limit != 143 {
			t.Fatalf("upper pool limit=%d err=%v, want 143/nil", limit, err)
		}
	})
	t.Run("rejects unsafe values", func(t *testing.T) {
		for _, value := range []string{"0", "144", "many"} {
			t.Setenv("SHOP_SIM_MYSQL_POOL_MAX_OPEN", value)
			if _, err := shoppingHTTPMySQLPoolLimit(); err == nil {
				t.Errorf("pool limit %q was accepted", value)
			}
		}
	})
}

func TestShoppingHTTPConcurrencyLevelsSelection(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []int
		wantErr bool
	}{
		{name: "default full matrix", want: []int{1, 10, 25, 50, 100, 200}},
		{name: "single high tier", input: "200", want: []int{200}},
		{name: "controlled pair", input: "100, 200", want: []int{100, 200}},
		{name: "unsupported tier", input: "128", wantErr: true},
		{name: "duplicate tier", input: "200,200", wantErr: true},
		{name: "empty element", input: "200,", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := shoppingHTTPConcurrencyLevelsFromEnv(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v, wantErr=%t", err, test.wantErr)
			}
			if err == nil && !reflect.DeepEqual(got, test.want) {
				t.Fatalf("levels=%v, want %v", got, test.want)
			}
		})
	}
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
	if err != nil {
		trial.LatencyMillis = float64(time.Since(started).Microseconds()) / 1000
		trial.Error = "HTTP request failed: " + err.Error()
		return trial
	}
	trial.HTTPStatus = response.StatusCode
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024))
	_ = response.Body.Close()
	trial.LatencyMillis = float64(time.Since(started).Microseconds()) / 1000
	if readErr != nil {
		trial.Error = "read response body: " + readErr.Error()
		return trial
	}
	if response.StatusCode != http.StatusOK {
		trial.Error = fmt.Sprintf("unexpected HTTP status %d", response.StatusCode)
		return trial
	}
	trial.TaskSetupMillis, err = parseSimulationStageMillis(response.Header, simulationTaskSetupMicrosHeader)
	if err != nil {
		trial.Error = err.Error()
		return trial
	}
	trial.TaskSetupTimed = true
	trial.IntentParseMillis, err = parseSimulationStageMillis(response.Header, simulationIntentParseMicrosHeader)
	if err != nil {
		trial.Error = err.Error()
		return trial
	}
	trial.IntentParseTimed = true
	trial.IntentPersistMillis, err = parseSimulationStageMillis(response.Header, simulationIntentPersistenceMicrosHeader)
	if err != nil {
		trial.Error = err.Error()
		return trial
	}
	trial.IntentPersistTimed = true
	trial.CatalogSearchMillis, err = parseSimulationCatalogSearchMillis(response.Header)
	if err != nil {
		trial.Error = err.Error()
		return trial
	}
	trial.CatalogSearchTimed = true
	var decoded shoppingHTTPConcurrentLoadResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		trial.Error = "decode HTTP response: " + err.Error()
		return trial
	}
	trial.TaskStatus = decoded.TaskStatus
	trial.CandidateCount = len(decoded.Candidates)
	if decoded.TaskID == "" || decoded.TaskStatus != TaskStatusDiscovered || !sameChallengeIntent(decoded.Intent, job.task.Intent) ||
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
	var taskSetupLatencies, intentParseLatencies, intentPersistLatencies, catalogSearchLatencies []float64
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
		if trial.TaskSetupTimed {
			metrics.TaskSetupTimedRequests++
			taskSetupLatencies = append(taskSetupLatencies, trial.TaskSetupMillis)
		}
		if trial.IntentParseTimed {
			metrics.IntentParseTimedRequests++
			intentParseLatencies = append(intentParseLatencies, trial.IntentParseMillis)
		}
		if trial.IntentPersistTimed {
			metrics.IntentPersistTimedRequests++
			intentPersistLatencies = append(intentPersistLatencies, trial.IntentPersistMillis)
		}
		if trial.CatalogSearchTimed {
			metrics.CatalogSearchTimedRequests++
			catalogSearchLatencies = append(catalogSearchLatencies, trial.CatalogSearchMillis)
		}
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
	metrics.TaskSetupP50Millis = lifecyclePercentile(taskSetupLatencies, 0.50)
	metrics.TaskSetupP95Millis = lifecyclePercentile(taskSetupLatencies, 0.95)
	metrics.IntentParseP50Millis = lifecyclePercentile(intentParseLatencies, 0.50)
	metrics.IntentParseP95Millis = lifecyclePercentile(intentParseLatencies, 0.95)
	metrics.IntentPersistP50Millis = lifecyclePercentile(intentPersistLatencies, 0.50)
	metrics.IntentPersistP95Millis = lifecyclePercentile(intentPersistLatencies, 0.95)
	metrics.CatalogSearchP50Millis = lifecyclePercentile(catalogSearchLatencies, 0.50)
	metrics.CatalogSearchP95Millis = lifecyclePercentile(catalogSearchLatencies, 0.95)
	if duration > 0 {
		metrics.CompletedSearchesPerSecond = float64(metrics.CompletedSearches) / duration.Seconds()
	}
	if metrics.WarmupErrors != 0 || len(measured) != shoppingHTTPConcurrentTaskCount ||
		metrics.UnexpectedHTTPFailures != 0 || metrics.BusinessErrors != 0 ||
		metrics.TaskSetupTimedRequests != shoppingHTTPConcurrentTaskCount ||
		metrics.IntentParseTimedRequests != shoppingHTTPConcurrentTaskCount ||
		metrics.IntentPersistTimedRequests != shoppingHTTPConcurrentTaskCount ||
		metrics.CatalogSearchTimedRequests != shoppingHTTPConcurrentTaskCount ||
		metrics.Top1GoldHits != shoppingHTTPConcurrentTaskCount || metrics.CompletedSearches != shoppingHTTPConcurrentTaskCount {
		metrics.Status = "FAIL"
	}
	return metrics
}

func TestParseSimulationCatalogSearchMillis(t *testing.T) {
	tests := []struct {
		name      string
		header    string
		want      float64
		wantError bool
	}{
		{name: "microseconds convert to milliseconds", header: "1250", want: 1.25},
		{name: "zero is valid", header: "0", want: 0},
		{name: "missing", wantError: true},
		{name: "malformed", header: "fast", wantError: true},
		{name: "negative", header: "-1", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			header := make(http.Header)
			if test.header != "" {
				header.Set(simulationCatalogSearchMicrosHeader, test.header)
			}
			got, err := parseSimulationCatalogSearchMillis(header)
			if (err != nil) != test.wantError {
				t.Fatalf("parseSimulationCatalogSearchMillis() error = %v, wantError %v", err, test.wantError)
			}
			if err == nil && got != test.want {
				t.Fatalf("parseSimulationCatalogSearchMillis() = %v, want %v", got, test.want)
			}
		})
	}
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

func shoppingMySQLConcurrentLoadOtherConnections(poolLimit, serverMax, threadsConnected, poolOpen, reserve int) (int, error) {
	if poolLimit <= 0 || serverMax <= 0 || threadsConnected < 0 || poolOpen < 0 || reserve < 0 {
		return 0, fmt.Errorf("invalid MySQL connection budget inputs: pool_limit=%d server_max=%d threads_connected=%d pool_open=%d reserve=%d",
			poolLimit, serverMax, threadsConnected, poolOpen, reserve)
	}
	otherConnections := threadsConnected - poolOpen
	if otherConnections < 0 {
		otherConnections = 0
	}
	if poolLimit+otherConnections+reserve > serverMax {
		return otherConnections, fmt.Errorf("refusing concurrent load: MySQL connection headroom is insufficient (pool_limit=%d other_connections=%d reserve=%d max_connections=%d)",
			poolLimit, otherConnections, reserve, serverMax)
	}
	return otherConnections, nil
}

func optionalResourceGBFromEnv(t *testing.T, name string) float64 {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 {
		t.Fatalf("invalid non-negative GB value in %s: %q", name, value)
	}
	return parsed
}

func TestShoppingMySQLConcurrentLoadConnectionBudget(t *testing.T) {
	tests := []struct {
		name             string
		poolLimit        int
		serverMax        int
		threadsConnected int
		poolOpen         int
		reserve          int
		wantOther        int
		wantError        bool
	}{
		{name: "fits with exact reserve", poolLimit: 128, serverMax: 151, threadsConnected: 16, poolOpen: 1, reserve: 8, wantOther: 15},
		{name: "rejects when reserve would be consumed", poolLimit: 128, serverMax: 151, threadsConnected: 17, poolOpen: 1, reserve: 8, wantOther: 16, wantError: true},
		{name: "rejects saturated smaller pool", poolLimit: 96, serverMax: 151, threadsConnected: 50, poolOpen: 1, reserve: 8, wantOther: 49, wantError: true},
		{name: "does not count benchmark connections twice", poolLimit: 24, serverMax: 151, threadsConnected: 5, poolOpen: 5, reserve: 8, wantOther: 0},
		{name: "rejects invalid pool limit", poolLimit: 0, serverMax: 151, threadsConnected: 1, poolOpen: 1, reserve: 8, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotOther, err := shoppingMySQLConcurrentLoadOtherConnections(test.poolLimit, test.serverMax,
				test.threadsConnected, test.poolOpen, test.reserve)
			if gotOther != test.wantOther || (err != nil) != test.wantError {
				t.Fatalf("connection budget = (other=%d, err=%v), want (other=%d, error=%t)",
					gotOther, err, test.wantOther, test.wantError)
			}
		})
	}
}

func sqlStats(store *MySQLStore) sql.DBStats {
	database, err := store.db.DB()
	if err != nil {
		return sql.DBStats{}
	}
	return database.Stats()
}
