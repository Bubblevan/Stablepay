package agentshopping

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
)

type lifecycleSuiteTrial struct {
	TaskID               string  `json:"task_id"`
	Scenario             string  `json:"scenario"`
	Mutation             string  `json:"mutation"`
	Candidates           int     `json:"candidate_count"`
	SelectedSKU          string  `json:"selected_sku,omitempty"`
	SelectedIsGold       bool    `json:"selected_is_gold"`
	PlanValid            bool    `json:"plan_valid"`
	InitialPriceMinor    int64   `json:"initial_price_minor,omitempty"`
	FinalPriceMinor      int64   `json:"final_price_minor,omitempty"`
	StaleQuoteRejected   bool    `json:"stale_quote_rejected"`
	FreshConsentRecorded bool    `json:"fresh_consent_recorded"`
	OrderID              string  `json:"order_id,omitempty"`
	OrderStatus          string  `json:"order_status,omitempty"`
	PaymentStatus        string  `json:"mock_payment_status,omitempty"`
	TaskStatus           string  `json:"final_task_status,omitempty"`
	LatencyMillis        float64 `json:"end_to_end_latency_ms"`
	Failure              string  `json:"failure,omitempty"`
}

type lifecycleAuditSummary struct {
	Passed              bool             `json:"passed"`
	ExpectedCounts      map[string]int64 `json:"expected_counts"`
	ObservedCounts      map[string]int64 `json:"observed_counts"`
	InvariantViolations map[string]int64 `json:"invariant_violations"`
	TaskStatuses        map[string]int64 `json:"task_statuses"`
	AuditActions        map[string]int64 `json:"audit_actions"`
}

type lifecycleSuiteMetrics struct {
	Status                  string                `json:"status"`
	RunID                   string                `json:"run_id"`
	TenantID                string                `json:"tenant_id"`
	Database                string                `json:"database"`
	Seed                    int64                 `json:"seed"`
	Hashes                  EvaluationHashes      `json:"hashes"`
	TaskCount               int                   `json:"task_count"`
	CandidateCountTotal     int64                 `json:"candidate_count_total"`
	CandidateCountMean      float64               `json:"candidate_count_mean"`
	TopCandidateGoldHits    int                   `json:"top_candidate_gold_hits"`
	ValidPlans              int                   `json:"valid_plans"`
	ValidPlanRate           float64               `json:"valid_plan_rate"`
	CompletedFlows          int                   `json:"completed_flows"`
	CompletedFlowRate       float64               `json:"completed_flow_rate"`
	PaidOrders              int                   `json:"paid_orders"`
	DeclinedPayments        int                   `json:"declined_payments"`
	PriceChangeCases        int                   `json:"price_change_cases"`
	StockChangeCases        int                   `json:"stock_change_cases"`
	StaleQuoteConfirmations int                   `json:"stale_quote_confirmations"`
	EndToEndP50Millis       float64               `json:"end_to_end_p50_ms"`
	EndToEndP95Millis       float64               `json:"end_to_end_p95_ms"`
	ModelCalls              int                   `json:"model_calls"`
	TokenUsage              string                `json:"token_usage"`
	StructuredB0ValidPlans  int                   `json:"structured_b0_valid_plans"`
	StructuredB1ValidPlans  int                   `json:"structured_b1_valid_plans"`
	Audit                   lifecycleAuditSummary `json:"audit"`
}

func TestMySQLShoppingLocked500Lifecycle(t *testing.T) {
	if os.Getenv("SHOP_SIM_MYSQL_FULL_LIFECYCLE") != "1" {
		t.Skip("set SHOP_SIM_MYSQL_FULL_LIFECYCLE=1 to run the locked 500-case suite in a cloned isolated tenant")
	}
	password := os.Getenv("SHOP_SIM_DB_PASSWORD")
	if strings.TrimSpace(password) == "" {
		t.Fatal("SHOP_SIM_DB_PASSWORD is required")
	}
	repoRoot, err := filepath.Abs(os.Getenv("SHOP_SIM_REPO_ROOT"))
	if err != nil || repoRoot == "." {
		t.Fatal("SHOP_SIM_REPO_ROOT must point to the StablePay repository")
	}
	suitePath, metricsPath, manifestPath := os.Getenv("SHOP_SIM_LOCKED_SUITE_PATH"),
		os.Getenv("SHOP_SIM_LOCKED_METRICS_PATH"), os.Getenv("SHOP_SIM_SOURCE_MANIFEST_PATH")
	for label, path := range map[string]string{"suite": suitePath, "metrics": metricsPath, "source manifest": manifestPath} {
		if path == "" {
			t.Fatalf("missing %s path", label)
		}
	}
	tasks, err := readLifecycleSuite(suitePath)
	if err != nil {
		t.Fatal(err)
	}
	var previous EvaluationReport
	if err := readLifecycleJSON(metricsPath, &previous); err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
	defer cancel()
	store, err := OpenLocalMySQLStore(password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close isolated MySQL: %v", err)
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
	tenantID := "shopping-lifecycle-" + suffix
	catalog, buyers, err := cloneLifecycleTenant(ctx, store, sourceTenant, tenantID, previous.Seed)
	if err != nil {
		t.Fatal(err)
	}
	store.tenant = tenantID
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cleanupCancel()
		if err := cleanupMySQLIntegrationTenant(cleanupCtx, store, tenantID); err != nil {
			t.Errorf("clean lifecycle tenant: %v", err)
			return
		}
		counts, err := store.Counts(cleanupCtx)
		if err != nil {
			t.Errorf("verify lifecycle tenant cleanup: %v", err)
			return
		}
		for table, count := range counts {
			if count != 0 {
				t.Errorf("tenant has %d residual rows in %s", count, table)
			}
		}
	})
	if err := ValidateLockedSuite(catalog, tasks); err != nil {
		t.Fatalf("locked suite validation: %v", err)
	}
	hashes, err := ComputeEvaluationHashes(catalog, tasks, manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	if hashes != previous.Hashes || len(tasks) != 500 || previous.Treatment.ValidPlans != 500 {
		t.Fatalf("locked input mismatch: current=%+v previous=%+v", hashes, previous.Hashes)
	}
	if len(buyers) < 500 {
		t.Fatalf("only %d isolated buyers for 500 tasks", len(buyers))
	}
	outputDir, runID, err := lifecycleEvidenceDirectory(repoRoot, suffix)
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"schema_version": "stablepay.agent-shopping-lifecycle.v1", "run_id": runID,
		"status": "RUNNING", "database": SimulationDatabaseName, "tenant_id": tenantID,
		"source_tenant_id": sourceTenant, "seed": previous.Seed, "suite_file": filepath.Base(suitePath),
		"hashes": hashes, "intent_source": "locked gold intent; model not called", "model_calls": 0,
		"payment_provider": "simulation-only", "real_settlement": false, "created_at_utc": time.Now().UTC()}
	if err := writeLifecycleJSON(filepath.Join(outputDir, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}

	metrics := lifecycleSuiteMetrics{RunID: runID, TenantID: tenantID, Database: SimulationDatabaseName,
		Seed: previous.Seed, Hashes: hashes, TaskCount: len(tasks), ModelCalls: 0,
		TokenUsage: "NOT_APPLICABLE_NO_MODEL_CALLS", StructuredB0ValidPlans: previous.Baseline.ValidPlans,
		StructuredB1ValidPlans: previous.Treatment.ValidPlans}
	byRegion := make(map[string][]BuyerRecord)
	for _, buyer := range buyers {
		byRegion[buyer.HomeRegion] = append(byRegion[buyer.HomeRegion], buyer)
	}
	for region := range byRegion {
		sort.Slice(byRegion[region], func(i, j int) bool { return byRegion[region][i].BuyerID < byRegion[region][j].BuyerID })
	}
	usedBuyers := make(map[string]bool, len(tasks))
	trials := make([]lifecycleSuiteTrial, 0, len(tasks))
	latencies := make([]float64, 0, len(tasks))
	for index, task := range tasks {
		buyer, ok := chooseLifecycleBuyer(byRegion[task.Intent.Region], task.Intent.MaxPriceMinor, usedBuyers)
		if !ok {
			trials = append(trials, lifecycleSuiteTrial{TaskID: task.TaskID, Scenario: task.Scenario, Mutation: task.Mutation,
				Failure: "NO_UNIQUE_BUYER_WITH_REGION_AND_BUDGET"})
			continue
		}
		usedBuyers[buyer.BuyerID] = true
		trial := runPersistentLifecycleTask(ctx, store, tenantID, task, buyer)
		trials = append(trials, trial)
		metrics.CandidateCountTotal += int64(trial.Candidates)
		if trial.SelectedIsGold {
			metrics.TopCandidateGoldHits++
		}
		if trial.PlanValid {
			metrics.ValidPlans++
		}
		if trial.OrderID != "" && (trial.OrderStatus == TaskStatusPaid || trial.OrderStatus == TaskStatusPaymentFailed) {
			metrics.CompletedFlows++
		}
		if trial.OrderStatus == TaskStatusPaid && trial.PaymentStatus == PaymentSettled {
			metrics.PaidOrders++
		}
		if trial.OrderStatus == TaskStatusPaymentFailed && trial.PaymentStatus == PaymentDeclined {
			metrics.DeclinedPayments++
		}
		if trial.Mutation == "PRICE_INCREASE" {
			metrics.PriceChangeCases++
		}
		if trial.Mutation == "STOCK_LOSS" {
			metrics.StockChangeCases++
		}
		if trial.StaleQuoteRejected {
			metrics.StaleQuoteConfirmations++
		}
		if trial.LatencyMillis > 0 {
			latencies = append(latencies, trial.LatencyMillis)
		}
		if index == 0 || (index+1)%100 == 0 {
			t.Logf("lifecycle progress %d/%d valid=%d terminal=%d", index+1, len(tasks), metrics.ValidPlans, metrics.CompletedFlows)
		}
	}
	metrics.CandidateCountMean = float64(metrics.CandidateCountTotal) / float64(len(tasks))
	metrics.ValidPlanRate = float64(metrics.ValidPlans) / float64(len(tasks))
	metrics.CompletedFlowRate = float64(metrics.CompletedFlows) / float64(len(tasks))
	metrics.EndToEndP50Millis = lifecyclePercentile(latencies, 0.50)
	metrics.EndToEndP95Millis = lifecyclePercentile(latencies, 0.95)
	metrics.Audit = auditLockedLifecycle(ctx, store, tenantID, len(tasks), metrics.PriceChangeCases, metrics.StockChangeCases)
	metrics.Status = "PASS"
	if len(trials) != 500 || metrics.ValidPlans != 500 || metrics.CompletedFlows != 500 || !metrics.Audit.Passed {
		metrics.Status = "FAIL"
	}
	manifest["status"] = metrics.Status
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
	t.Logf("MYSQL_LIFECYCLE_500 status=%s tenant=%s plans=%d/%d completed=%d/%d paid=%d declined=%d price=%d stock=%d stale=%d audit=%v p95_ms=%.2f evidence=%s",
		metrics.Status, tenantID, metrics.ValidPlans, metrics.TaskCount, metrics.CompletedFlows, metrics.TaskCount,
		metrics.PaidOrders, metrics.DeclinedPayments, metrics.PriceChangeCases, metrics.StockChangeCases,
		metrics.StaleQuoteConfirmations, metrics.Audit.Passed, metrics.EndToEndP95Millis, outputDir)
	if metrics.Status != "PASS" {
		for _, trial := range trials {
			if trial.Failure != "" {
				t.Errorf("task %s: %s", trial.TaskID, trial.Failure)
			}
		}
		t.Errorf("500-case lifecycle/audit gate failed: %s", outputDir)
	}
}

func runPersistentLifecycleTask(ctx context.Context, store *MySQLStore, tenant string, task Task, buyer BuyerRecord) lifecycleSuiteTrial {
	started := time.Now()
	trial := lifecycleSuiteTrial{TaskID: task.TaskID, Scenario: task.Scenario, Mutation: task.Mutation}
	fail := func(err error) lifecycleSuiteTrial {
		trial.Failure = err.Error()
		trial.LatencyMillis = float64(time.Since(started).Microseconds()) / 1000
		return trial
	}
	key := "locked-" + task.TaskID
	row, err := store.CreateTask(ctx, buyer.BuyerID, task.Prompt, "task-"+key)
	if err != nil {
		return fail(fmt.Errorf("create task: %w", err))
	}
	if err := store.SaveParsedIntent(ctx, row.TaskID, "locked-gold-intent", "structured-fixture-v1", task.Intent,
		0, nil, ParseUsage{UsageStatus: "NOT_APPLICABLE_NO_MODEL_CALLS"}); err != nil {
		return fail(fmt.Errorf("persist locked intent: %w", err))
	}
	candidates, err := store.FindCandidates(ctx, task.Intent, 20)
	if err != nil {
		return fail(fmt.Errorf("search candidates: %w", err))
	}
	trial.Candidates = len(candidates)
	if len(candidates) == 0 {
		return fail(fmt.Errorf("no live candidates"))
	}
	selected := candidates[0]
	trial.SelectedSKU = selected.Product.SKUID
	trial.InitialPriceMinor, trial.FinalPriceMinor = selected.Inventory.PriceMinor, selected.Inventory.PriceMinor
	gold := make(map[string]bool, len(task.GoldSKUs))
	for _, sku := range task.GoldSKUs {
		gold[sku] = true
	}
	trial.SelectedIsGold = gold[selected.Product.SKUID]
	request := PersistentQuoteRequest{TaskID: row.TaskID, BuyerID: buyer.BuyerID,
		MerchantID: selected.Product.MerchantID, SKUID: selected.Product.SKUID,
		Region: task.Intent.Region, MaxMinor: task.Intent.MaxPriceMinor,
		Currency: "USDC", IdempotencyKey: "initial-quote-" + task.TaskID}
	quote, err := store.IssueQuote(ctx, request)
	if err != nil {
		return fail(fmt.Errorf("issue quote: %w", err))
	}
	if task.Mutation != "NONE" {
		reason, adjustKey := "", "mutation-"+task.TaskID
		var price *int64
		var stock *int
		switch task.Mutation {
		case "PRICE_INCREASE":
			newPrice := quote.AmountMinor + 1
			if newPrice > task.Intent.MaxPriceMinor {
				return fail(fmt.Errorf("selected quote has no headroom for price increase"))
			}
			price, reason = &newPrice, "LOCKED_PRICE_INCREASE"
			trial.FinalPriceMinor = newPrice
		case "STOCK_LOSS":
			zero := 0
			stock, reason = &zero, "LOCKED_STOCK_LOSS"
		default:
			return fail(fmt.Errorf("unknown mutation %q", task.Mutation))
		}
		if err := store.ChangeInventory(ctx, row.TaskID, selected.Product.MerchantID, selected.Product.SKUID,
			task.Intent.Region, reason, adjustKey, price, stock); err != nil {
			return fail(fmt.Errorf("mutate inventory: %w", err))
		}
		_, err := store.ConfirmBuyer(ctx, PersistentConfirmRequest{BuyerID: buyer.BuyerID, QuoteID: quote.ID,
			Accepted: true, AmountMinor: quote.AmountMinor, IdempotencyKey: "stale-confirm-" + task.TaskID})
		if !errors.Is(err, ErrStaleQuote) {
			return fail(fmt.Errorf("stale quote confirmation error=%v, want ErrStaleQuote", err))
		}
		trial.StaleQuoteRejected = true
		if task.Mutation == "STOCK_LOSS" {
			onHand := selected.Inventory.OnHand
			if err := store.ChangeInventory(ctx, row.TaskID, selected.Product.MerchantID, selected.Product.SKUID,
				task.Intent.Region, "LOCKED_STOCK_RESTORED_FOR_RECONFIRMATION", "restore-"+task.TaskID, nil, &onHand); err != nil {
				return fail(fmt.Errorf("restore inventory: %w", err))
			}
		}
		request.IdempotencyKey = "fresh-quote-" + task.TaskID
		quote, err = store.IssueQuote(ctx, request)
		if err != nil {
			return fail(fmt.Errorf("issue fresh quote: %w", err))
		}
	}
	confirmation, err := store.ConfirmBuyer(ctx, PersistentConfirmRequest{BuyerID: buyer.BuyerID, QuoteID: quote.ID,
		Accepted: true, AmountMinor: quote.AmountMinor, IdempotencyKey: "accepted-confirm-" + task.TaskID})
	if err != nil {
		return fail(fmt.Errorf("record buyer confirmation: %w", err))
	}
	trial.FreshConsentRecorded = confirmation.Accepted
	trial.PlanValid = trial.SelectedIsGold && quote.AmountMinor <= task.Intent.MaxPriceMinor && confirmation.Accepted
	checkout, err := store.Checkout(ctx, tenant, confirmation.ID, "locked-order-"+task.TaskID)
	if err != nil {
		return fail(fmt.Errorf("checkout: %w", err))
	}
	order, payment, err := store.GetOrder(ctx, checkout.Order.ID)
	if err != nil {
		return fail(fmt.Errorf("read terminal order: %w", err))
	}
	taskState, err := store.GetTask(ctx, row.TaskID)
	if err != nil {
		return fail(fmt.Errorf("read terminal task: %w", err))
	}
	if order.Status != paymentTerminalTaskStatus(payment.Status) || taskState.Status != order.Status {
		return fail(fmt.Errorf("terminal states mismatch: task=%s order=%s payment=%s", taskState.Status, order.Status, payment.Status))
	}
	trial.OrderID, trial.OrderStatus, trial.PaymentStatus, trial.TaskStatus = order.ID, order.Status, payment.Status, taskState.Status
	trial.LatencyMillis = float64(time.Since(started).Microseconds()) / 1000
	return trial
}

func paymentTerminalTaskStatus(paymentStatus string) string {
	switch paymentStatus {
	case PaymentSettled:
		return TaskStatusPaid
	case PaymentDeclined:
		return TaskStatusPaymentFailed
	default:
		return ""
	}
}

func cloneLifecycleTenant(ctx context.Context, store *MySQLStore, sourceTenant, targetTenant string, seed int64) (ImportedCatalog, []BuyerRecord, error) {
	var merchants []MerchantRecord
	if err := store.db.WithContext(ctx).Where("tenant_id = ?", sourceTenant).Order("merchant_id").Find(&merchants).Error; err != nil {
		return ImportedCatalog{}, nil, err
	}
	var productRows []ProductRecord
	if err := store.db.WithContext(ctx).Where("tenant_id = ?", sourceTenant).Find(&productRows).Error; err != nil {
		return ImportedCatalog{}, nil, err
	}
	var buyers []BuyerRecord
	if err := store.db.WithContext(ctx).Where("tenant_id = ?", sourceTenant).Order("buyer_id").Find(&buyers).Error; err != nil {
		return ImportedCatalog{}, nil, err
	}
	if len(merchants) != 12 || len(productRows) != 24000 || len(buyers) != 2000 {
		return ImportedCatalog{}, nil, fmt.Errorf("source dimensions merchants=%d products=%d buyers=%d, want 12/24000/2000", len(merchants), len(productRows), len(buyers))
	}
	catalog := ImportedCatalog{Seed: seed, Merchants: merchants, Products: make([]Product, 0, len(productRows)),
		Inventory: make([]Inventory, 0, len(productRows)*len(Regions))}
	for _, row := range productRows {
		var tags []string
		if err := json.Unmarshal(row.TagsJSON, &tags); err != nil {
			return ImportedCatalog{}, nil, fmt.Errorf("decode tags for %s: %w", row.SKUID, err)
		}
		catalog.Products = append(catalog.Products, Product{ID: StableID("product", row.MerchantID, row.SKUID), SKUID: row.SKUID,
			MerchantID: row.MerchantID, Category: row.Category, Title: row.Title, Description: row.Description,
			ImageURL: row.ImageURL, Price: FormatMinorUnits(row.BasePrice), PriceMinor: row.BasePrice,
			Currency: row.Currency, Tags: tags, Status: row.Status, Item: row.Item, Format: row.Format, Style: row.Style})
	}
	overlay := MakeOverlay(catalog.Products, seed)
	currency := make(map[string]string, len(catalog.Products))
	for _, product := range catalog.Products {
		currency[product.SKUID] = product.Currency
	}
	inventoryRows := make([]InventoryRecord, 0, len(overlay))
	now := time.Now().UTC()
	for _, row := range overlay {
		catalog.Inventory = append(catalog.Inventory, Inventory{TenantID: SimulationTenantID, MerchantID: row.MerchantID,
			SKUID: row.SKUID, Region: row.Region, OnHand: row.OnHand, Eligible: row.Eligible,
			PriceMinor: row.PriceMinor, Revision: row.Revision})
		inventoryRows = append(inventoryRows, InventoryRecord{TenantID: targetTenant, MerchantID: row.MerchantID,
			SKUID: row.SKUID, Region: row.Region, OnHand: row.OnHand, Reserved: 0, Eligible: row.Eligible,
			PriceMinor: row.PriceMinor, Currency: currency[row.SKUID], Revision: row.Revision,
			UpdatedAt: now, DataOrigin: syntheticOrigin})
	}
	for i := range merchants {
		merchants[i].TenantID = targetTenant
	}
	for i := range productRows {
		productRows[i].TenantID = targetTenant
	}
	for i := range buyers {
		buyers[i].TenantID = targetTenant
	}
	err := store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing int64
		if err := tx.Model(&MerchantRecord{}).Where("tenant_id = ?", targetTenant).Count(&existing).Error; err != nil {
			return err
		}
		if existing != 0 {
			return fmt.Errorf("lifecycle tenant already exists")
		}
		for _, batch := range []struct {
			rows any
			size int
		}{{merchants, 100}, {productRows, 500}, {inventoryRows, 1000}, {buyers, 500}} {
			if err := tx.CreateInBatches(batch.rows, batch.size).Error; err != nil {
				return err
			}
		}
		datasetID := StableID("lifecycle-dataset", targetTenant, fmt.Sprint(seed))
		return tx.Create(&DatasetRunRecord{DatasetID: datasetID, TenantID: targetTenant, Seed: seed,
			MerchantCount: len(merchants), ProductCount: len(productRows), InventoryCount: len(inventoryRows),
			BuyerCount: len(buyers), SourceManifest: "deterministic tenant clone from " + sourceTenant,
			SeededAt: now, DataOrigin: syntheticOrigin}).Error
	})
	if err != nil {
		return ImportedCatalog{}, nil, err
	}
	return catalog, buyers, nil
}

func seedLifecycleTenantFromMerchantSnapshot(ctx context.Context, store *MySQLStore, sourceTenant, targetTenant string,
	catalog ImportedCatalog, seed int64) (ImportedCatalog, []BuyerRecord, error) {
	if len(catalog.Merchants) != 12 || len(catalog.Products) != 24000 || len(catalog.ProductRows) != 24000 ||
		len(catalog.Inventory) != 96000 || len(catalog.InventoryRows) != 96000 {
		return ImportedCatalog{}, nil, fmt.Errorf("current Merchant snapshot dimensions are invalid: merchants=%d products=%d product_rows=%d inventory=%d inventory_rows=%d",
			len(catalog.Merchants), len(catalog.Products), len(catalog.ProductRows), len(catalog.Inventory), len(catalog.InventoryRows))
	}
	var buyers []BuyerRecord
	if err := store.db.WithContext(ctx).Where("tenant_id = ?", sourceTenant).Order("buyer_id").Find(&buyers).Error; err != nil {
		return ImportedCatalog{}, nil, fmt.Errorf("read isolated buyers for fresh lifecycle tenant: %w", err)
	}
	if len(buyers) != 2000 {
		return ImportedCatalog{}, nil, fmt.Errorf("isolated buyer source has %d rows, want 2000", len(buyers))
	}
	oldTenant := store.tenant
	store.tenant = targetTenant
	err := store.SeedDataset(ctx, SimulationSeed{
		DatasetID: StableID("lifecycle-dataset", targetTenant, fmt.Sprint(seed)),
		Seed:      seed,
		Manifest:  "fresh isolated tenant from current loopback Merchant HTTP snapshot: " + catalog.ManifestPath,
		Merchants: append([]MerchantRecord(nil), catalog.Merchants...),
		Products:  append([]ProductRecord(nil), catalog.ProductRows...),
		Inventory: append([]InventoryRecord(nil), catalog.InventoryRows...),
		Buyers:    append([]BuyerRecord(nil), buyers...),
	})
	if err != nil {
		store.tenant = oldTenant
		return ImportedCatalog{}, nil, fmt.Errorf("seed fresh isolated lifecycle tenant from Merchant HTTP snapshot: %w", err)
	}
	return catalog, buyers, nil
}

func readLifecycleSuite(path string) ([]Task, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var tasks []Task
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		var task Task
		if err := json.Unmarshal(scanner.Bytes(), &task); err != nil {
			return nil, fmt.Errorf("decode locked task %d: %w", len(tasks)+1, err)
		}
		tasks = append(tasks, task)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func readLifecycleJSON(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func chooseLifecycleBuyer(buyers []BuyerRecord, budget int64, used map[string]bool) (BuyerRecord, bool) {
	for _, buyer := range buyers {
		if !used[buyer.BuyerID] && buyer.BudgetMinor >= budget {
			return buyer, true
		}
	}
	return BuyerRecord{}, false
}

func lifecycleEvidenceDirectory(repoRoot, suffix string) (string, string, error) {
	root := filepath.Join(repoRoot, ".local-run", "agent-shopping", "lifecycle-evaluations")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", "", err
	}
	runID := "lifecycle-" + time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + suffix
	directory := filepath.Join(root, runID)
	if err := os.Mkdir(directory, 0o700); err != nil {
		return "", "", err
	}
	return directory, runID, nil
}

func writeLifecycleJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func writeLifecycleLines[T any](path string, values []T) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	for _, value := range values {
		if err := encoder.Encode(value); err != nil {
			_ = file.Close()
			return err
		}
	}
	return file.Close()
}

func lifecyclePercentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	return ordered[int(float64(len(ordered)-1)*p)]
}

func auditLockedLifecycle(ctx context.Context, store *MySQLStore, tenant string, tasks, priceChanges, stockChanges int) lifecycleAuditSummary {
	violations := make(map[string]int64)
	checks := map[string]string{
		"orphan_order_aggregates": `SELECT COUNT(*) FROM sim_orders o
			LEFT JOIN sim_quotes q ON q.tenant_id=o.tenant_id AND q.quote_id=o.quote_id
			LEFT JOIN sim_buyer_confirmations c ON c.tenant_id=o.tenant_id AND c.confirmation_id=o.confirmation_id
			LEFT JOIN simulated_payments p ON p.tenant_id=o.tenant_id AND p.order_id=o.order_id
			LEFT JOIN sim_inventory_reservations r ON r.tenant_id=o.tenant_id AND r.order_id=o.order_id
			WHERE o.tenant_id=? AND (q.quote_id IS NULL OR c.confirmation_id IS NULL OR p.payment_id IS NULL OR r.reservation_id IS NULL
			OR q.amount_minor<>o.amount_minor OR c.amount_minor<>o.amount_minor OR p.amount_minor<>o.amount_minor)`,
		"stale_quote_orders": `SELECT COUNT(*) FROM sim_buyer_confirmation_attempts a
			JOIN sim_orders o ON o.tenant_id=a.tenant_id AND o.quote_id=a.quote_id WHERE a.tenant_id=? AND a.status='STALE_QUOTE'`,
		"unfinished_tasks":  `SELECT COUNT(*) FROM sim_shopping_tasks WHERE tenant_id=? AND status NOT IN ('PAID','PAYMENT_FAILED','BUYER_REJECTED','NO_MATCHING_CANDIDATES','INTENT_PARSE_FAILED')`,
		"reservation_leaks": `SELECT COUNT(*) FROM sim_inventory WHERE tenant_id=? AND reserved<>0`,
		"non_mock_payments": `SELECT COUNT(*) FROM simulated_payments WHERE tenant_id=? AND (provider<>'simulation-only' OR data_origin<>'SYNTHETIC_SIMULATION')`,
		"payment_order_status_mismatches": `SELECT COUNT(*) FROM sim_orders o JOIN simulated_payments p ON p.tenant_id=o.tenant_id AND p.order_id=o.order_id
			WHERE o.tenant_id=? AND ((o.status='PAID' AND p.status<>'SETTLED') OR (o.status='PAYMENT_FAILED' AND p.status<>'DECLINED')
			OR o.status NOT IN ('PAID','PAYMENT_FAILED'))`,
		"invalid_reservation_terminal_state": `SELECT COUNT(*) FROM sim_orders o JOIN simulated_payments p ON p.tenant_id=o.tenant_id AND p.order_id=o.order_id
			JOIN sim_inventory_reservations r ON r.tenant_id=o.tenant_id AND r.order_id=o.order_id WHERE o.tenant_id=? AND
			((p.status='SETTLED' AND r.status<>'CONSUMED') OR (p.status='DECLINED' AND r.status<>'RELEASED'))`,
	}
	for name, query := range checks {
		var count int64
		if err := store.db.WithContext(ctx).Raw(query, tenant).Scan(&count).Error; err != nil {
			violations["query_error_"+name] = 1
		} else if count != 0 {
			violations[name] = count
		}
	}
	observed, err := store.Counts(ctx)
	if err != nil {
		observed = map[string]int64{}
		violations["counts_query_error"] = 1
	}
	expected := map[string]int64{
		"merchants": 12, "products": 24000, "inventory": 96000, "buyers": 2000,
		"tasks": int64(tasks), "quotes": int64(tasks + priceChanges + stockChanges),
		"confirmations": int64(tasks), "confirmation_attempts": int64(tasks + priceChanges + stockChanges),
		"orders": int64(tasks), "reservations": int64(tasks), "inventory_adjustments": int64(priceChanges + 2*stockChanges),
		"simulated_payments": int64(tasks), "intent_parse_traces": int64(tasks),
	}
	for name, want := range expected {
		if observed[name] != want {
			violations["count_mismatch_"+name] = observed[name] - want
		}
	}
	statuses := make(map[string]int64)
	var taskRows []struct {
		Status string
		Count  int64
	}
	if err := store.db.WithContext(ctx).Model(&ShoppingTaskRecord{}).Select("status, COUNT(*) AS count").
		Where("tenant_id = ?", tenant).Group("status").Scan(&taskRows).Error; err != nil {
		violations["task_status_query_error"] = 1
	} else {
		for _, row := range taskRows {
			statuses[row.Status] = row.Count
		}
	}
	actions := make(map[string]int64)
	var auditRows []struct {
		Action string
		Count  int64
	}
	if err := store.db.WithContext(ctx).Model(&SimulationAuditRecord{}).Select("action, COUNT(*) AS count").
		Where("tenant_id = ?", tenant).Group("action").Scan(&auditRows).Error; err != nil {
		violations["audit_action_query_error"] = 1
	} else {
		for _, row := range auditRows {
			actions[row.Action] = row.Count
		}
	}
	expectedActions := map[string]int64{
		"QUOTE_ISSUED": expected["quotes"], "INVENTORY_REVISED": expected["inventory_adjustments"],
		"BUYER_CONFIRMATION_BLOCKED_STALE_QUOTE": int64(priceChanges + stockChanges), "BUYER_CONFIRMED": int64(tasks),
		"ORDER_RESERVED": int64(tasks), "MOCK_PAYMENT_SETTLED": actions["MOCK_PAYMENT_SETTLED"],
		"MOCK_PAYMENT_DECLINED": actions["MOCK_PAYMENT_DECLINED"],
	}
	if actions["MOCK_PAYMENT_SETTLED"]+actions["MOCK_PAYMENT_DECLINED"] != int64(tasks) {
		violations["payment_audit_action_total"] = actions["MOCK_PAYMENT_SETTLED"] + actions["MOCK_PAYMENT_DECLINED"] - int64(tasks)
	}
	for name, want := range expectedActions {
		if actions[name] != want {
			violations["audit_action_count_mismatch_"+name] = actions[name] - want
		}
	}
	if statuses[TaskStatusPaid]+statuses[TaskStatusPaymentFailed] != int64(tasks) {
		violations["terminal_task_count_mismatch"] = statuses[TaskStatusPaid] + statuses[TaskStatusPaymentFailed] - int64(tasks)
	}
	return lifecycleAuditSummary{Passed: len(violations) == 0, ExpectedCounts: expected,
		ObservedCounts: observed, InvariantViolations: violations, TaskStatuses: statuses, AuditActions: actions}
}
