package agentshopping

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"time"
)

type BuiltSimulationSeed struct {
	Dataset   SimulationSeed
	Catalog   ImportedCatalog
	Tasks     []Task
	Buyers    []BuyerRecord
	DatasetID string
}

type SeedHistoryStats struct {
	Tasks          int `json:"tasks"`
	Quotes         int `json:"quotes"`
	RepricedQuotes int `json:"repriced_quote_pairs"`
	BuyerAccepted  int `json:"buyer_confirmed"`
	BuyerRejected  int `json:"buyer_rejected"`
	StaleBlocked   int `json:"stale_quote_blocked"`
	Orders         int `json:"orders"`
	Payments       int `json:"simulated_payments"`
	Settled        int `json:"simulated_settled"`
	Declined       int `json:"simulated_declined"`
}

func BuildSimulationSeed(ctx context.Context, manifestPath string, seed int64, buyerCount, taskCount int) (BuiltSimulationSeed, error) {
	if buyerCount < 500 || buyerCount > 100000 || taskCount != 500 {
		return BuiltSimulationSeed{}, fmt.Errorf("seed dimensions must use 500 tasks and between 500 and 100,000 synthetic buyers")
	}
	catalog, err := LoadMerchantCatalog(ctx, manifestPath, nil)
	if err != nil {
		return BuiltSimulationSeed{}, err
	}
	if catalog.Seed != seed {
		return BuiltSimulationSeed{}, fmt.Errorf("source catalog seed %d does not match requested seed %d", catalog.Seed, seed)
	}
	overlay := MakeOverlay(catalog.Products, seed)
	tasks, err := GenerateTasks(catalog.Products, overlay, seed, taskCount)
	if err != nil {
		return BuiltSimulationSeed{}, fmt.Errorf("generate shopping tasks: %w", err)
	}
	buyers := BuildSyntheticBuyers(seed, buyerCount)
	taskRows := make([]ShoppingTaskRecord, 0, len(tasks))
	for i, task := range tasks {
		intentJSON, err := json.Marshal(task.Intent)
		if err != nil {
			return BuiltSimulationSeed{}, err
		}
		buyer := buyers[i%len(buyers)]
		taskRows = append(taskRows, ShoppingTaskRecord{TaskID: task.TaskID, TenantID: SimulationTenantID,
			BuyerID: buyer.BuyerID, IdempotencyKey: "seed-task-" + task.TaskID, Prompt: task.Prompt,
			IntentJSON: intentJSON, Provider: "deterministic-gold", Model: "synthetic-fixture-v1",
			ParseSource: "synthetic-gold", Status: "SEEDED", CreatedAt: deterministicTime(seed, "task", task.TaskID),
			DataOrigin: syntheticOrigin})
	}
	manifestAbs, err := filepath.Abs(manifestPath)
	if err != nil {
		return BuiltSimulationSeed{}, err
	}
	datasetID := StableID("shopping-simulation-dataset", fmt.Sprint(seed), manifestAbs)
	dataset := SimulationSeed{DatasetID: datasetID, Seed: seed, Manifest: manifestAbs,
		Merchants: catalog.Merchants, Products: catalog.ProductRows, Inventory: catalog.InventoryRows,
		Buyers: buyers, Tasks: taskRows}
	return BuiltSimulationSeed{Dataset: dataset, Catalog: catalog, Tasks: tasks, Buyers: buyers, DatasetID: datasetID}, nil
}

func BuildSyntheticBuyers(seed int64, count int) []BuyerRecord {
	regions := append([]string(nil), Regions...)
	sort.Strings(regions)
	segments := []string{"value", "standard", "premium", "business"}
	buyers := make([]BuyerRecord, 0, count)
	for i := 1; i <= count; i++ {
		id := fmt.Sprintf("sim-buyer-%06d", i)
		digest := sha256.Sum256([]byte(fmt.Sprintf("%d|%s", seed, id)))
		budget := int64(15000 + int(digest[0])*150) // $150–$532.50 USDC
		preferences, _ := json.Marshal(map[string]any{
			"preferred_categories":  []string{"coffee", "books", "education", "design-assets", "developer-tools"}[int(digest[1])%5],
			"delivery_region":       regions[int(digest[2])%len(regions)],
			"confirmation_required": true,
			"currency":              "USDC",
			"data_origin":           syntheticOrigin,
		})
		buyers = append(buyers, BuyerRecord{TenantID: SimulationTenantID, BuyerID: id,
			DisplayName: fmt.Sprintf("模拟买家 %06d", i), HomeRegion: regions[int(digest[3])%len(regions)],
			Segment: segments[int(digest[4])%len(segments)], BudgetMinor: budget,
			PreferencesJSON: preferences, DataOrigin: syntheticOrigin,
			CreatedAt: deterministicTime(seed, "buyer", id)})
	}
	return buyers
}

func (s *MySQLStore) SeedHistoricalFlows(ctx context.Context, built BuiltSimulationSeed) (SeedHistoryStats, error) {
	if len(built.Tasks) != 500 || len(built.Catalog.Products) != 24000 || len(built.Buyers) == 0 {
		return SeedHistoryStats{}, ErrInvalidSimulationData
	}
	productsBySKU := make(map[string]Product, len(built.Catalog.Products))
	inventory := make(map[string]Inventory, len(built.Catalog.Inventory))
	for _, product := range built.Catalog.Products {
		productsBySKU[product.SKUID] = product
	}
	for _, row := range built.Catalog.Inventory {
		inventory[inventoryKey(row.TenantID, row.MerchantID, row.SKUID, row.Region)] = row
	}
	stats := SeedHistoryStats{Tasks: len(built.Tasks)}
	for index, task := range built.Tasks {
		buyer := built.Buyers[index%len(built.Buyers)]
		best, ok := cheapestTaskProduct(task, productsBySKU, inventory)
		if !ok {
			return stats, fmt.Errorf("task %s has no eligible in-stock target", task.TaskID)
		}
		quote, err := s.IssueQuote(ctx, PersistentQuoteRequest{TaskID: task.TaskID, BuyerID: buyer.BuyerID,
			MerchantID: best.MerchantID, SKUID: best.SKUID, Region: task.Intent.Region,
			MaxMinor: task.Intent.MaxPriceMinor, Currency: "USDC", IdempotencyKey: "seed-quote-" + task.TaskID + "-v1"})
		if err != nil {
			return stats, fmt.Errorf("seed quote for %s: %w", task.TaskID, err)
		}
		stats.Quotes++
		activeQuote := quote
		if task.Mutation == "PRICE_INCREASE" {
			newPrice := best.PriceMinor + 100
			if newPrice > task.Intent.MaxPriceMinor {
				newPrice = best.PriceMinor - 1
			}
			if newPrice <= 0 || newPrice == best.PriceMinor {
				return stats, fmt.Errorf("task %s cannot produce a safe quote version mutation", task.TaskID)
			}
			if err := s.ChangeInventory(ctx, task.TaskID, best.MerchantID, best.SKUID, task.Intent.Region,
				"seed-price-revision", "seed-adjust-"+task.TaskID, &newPrice, nil); err != nil {
				return stats, err
			}
			activeQuote, err = s.IssueQuote(ctx, PersistentQuoteRequest{TaskID: task.TaskID, BuyerID: buyer.BuyerID,
				MerchantID: best.MerchantID, SKUID: best.SKUID, Region: task.Intent.Region,
				MaxMinor: task.Intent.MaxPriceMinor, Currency: "USDC", IdempotencyKey: "seed-quote-" + task.TaskID + "-v2"})
			if err != nil {
				return stats, fmt.Errorf("seed revised quote for %s: %w", task.TaskID, err)
			}
			stats.Quotes++
			stats.RepricedQuotes++
		}
		if task.Mutation == "STOCK_LOSS" {
			zero := 0
			if err := s.ChangeInventory(ctx, task.TaskID, best.MerchantID, best.SKUID, task.Intent.Region,
				"seed-stock-loss", "seed-adjust-"+task.TaskID, nil, &zero); err != nil {
				return stats, err
			}
			_, err := s.ConfirmBuyer(ctx, PersistentConfirmRequest{BuyerID: buyer.BuyerID, QuoteID: quote.ID,
				Accepted: true, AmountMinor: quote.AmountMinor, IdempotencyKey: "seed-confirm-" + task.TaskID})
			if !errors.Is(err, ErrStaleQuote) {
				return stats, fmt.Errorf("task %s should reject its stale inventory quote, got %v", task.TaskID, err)
			}
			stats.StaleBlocked++
			continue
		}
		accepted := deterministicPercent("buyer-decision", task.TaskID) < 86
		confirmation, err := s.ConfirmBuyer(ctx, PersistentConfirmRequest{BuyerID: buyer.BuyerID, QuoteID: activeQuote.ID,
			Accepted: accepted, AmountMinor: activeQuote.AmountMinor, IdempotencyKey: "seed-confirm-" + task.TaskID})
		if err != nil {
			return stats, fmt.Errorf("seed buyer confirmation for %s: %w", task.TaskID, err)
		}
		if !accepted {
			stats.BuyerRejected++
			continue
		}
		stats.BuyerAccepted++
		result, err := s.Checkout(ctx, s.tenant, confirmation.ID, "seed-order-"+task.TaskID)
		if err != nil {
			return stats, fmt.Errorf("seed checkout for %s: %w", task.TaskID, err)
		}
		stats.Orders++
		stats.Payments++
		if result.Payment.Status == PaymentSettled {
			stats.Settled++
		} else {
			stats.Declined++
		}
	}
	if err := s.UpdateDatasetRunSummary(ctx, built.DatasetID); err != nil {
		return stats, err
	}
	return stats, nil
}

func cheapestTaskProduct(task Task, products map[string]Product, inventory map[string]Inventory) (Product, bool) {
	var best Product
	bestPrice := int64(0)
	found := false
	for _, sku := range task.GoldSKUs {
		product, exists := products[sku]
		if !exists {
			continue
		}
		row, ok := inventory[inventoryKey(SimulationTenantID, product.MerchantID, product.SKUID, task.Intent.Region)]
		if !ok || !row.Eligible || row.OnHand-row.Reserved < 1 || row.PriceMinor > task.Intent.MaxPriceMinor {
			continue
		}
		if !found || row.PriceMinor < bestPrice || (row.PriceMinor == bestPrice && product.SKUID < best.SKUID) {
			best, bestPrice, found = product, row.PriceMinor, true
		}
	}
	if found {
		best.PriceMinor = bestPrice
	}
	return best, found
}

func deterministicPercent(namespace, key string) int {
	digest := sha256.Sum256([]byte(namespace + "|" + key))
	return int(digest[0]) % 100
}

func deterministicTime(seed int64, namespace, key string) time.Time {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d|%s|%s", seed, namespace, key)))
	base := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	return base.Add(time.Duration(int(digest[0])*24+int(digest[1])%24) * time.Hour)
}
