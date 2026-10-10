package main

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stablepay/commerce-runtime/benchmarks/agentshopping"
)

func TestTaxonomyFromCatalogMapsItemsToSortedCategories(t *testing.T) {
	catalog := agentshopping.ImportedCatalog{Products: []agentshopping.Product{
		{Category: "z-books", Item: "shared-item", Format: "ebook", Style: "minimal"},
		{Category: "a-books", Item: "shared-item", Format: "ebook", Style: "minimal"},
		{Category: "coffee", Item: "beans", Format: "bag", Style: "floral"},
	}}

	taxonomy := taxonomyFromCatalog(catalog)
	if got, want := taxonomy.ItemCategories["shared-item"], []string{"a-books", "z-books"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("item category mapping = %#v, want %#v", got, want)
	}
	if len(taxonomy.FormatAliases) == 0 || len(taxonomy.StyleAliases) == 0 {
		t.Fatal("taxonomy omitted explicit format/style alias hints")
	}
}

func TestSelectStratifiedDiagnosticTasksPreservesScenarioMix(t *testing.T) {
	scenarios := []string{"STANDARD", "FORMAT_ALIAS", "STYLE_ALIAS", "BUDGET_BOUNDARY", "SOFT_PREFERENCE", "STOCK_CHANGE", "PRICE_CHANGE"}
	tasks := make([]agentshopping.Task, 0, 500)
	for i := 0; i < 500; i++ {
		scenario := "STANDARD"
		switch (i + 1) % 10 {
		case 0:
			scenario = "PRICE_CHANGE"
		case 5:
			scenario = "STOCK_CHANGE"
		case 1:
			scenario = "FORMAT_ALIAS"
		case 2:
			scenario = "STYLE_ALIAS"
		case 3:
			scenario = "BUDGET_BOUNDARY"
		case 4:
			scenario = "SOFT_PREFERENCE"
		}
		tasks = append(tasks, agentshopping.Task{TaskID: fmt.Sprintf("shop-%04d", i+1), Scenario: scenario,
			SourceMerchantID: fmt.Sprintf("merchant-%02d", i/42+1)})
	}

	first, err := selectStratifiedDiagnosticTasks(tasks, 50)
	if err != nil {
		t.Fatal(err)
	}
	second, err := selectStratifiedDiagnosticTasks(tasks, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("stratified diagnostic sample is not deterministic")
	}
	if len(first) != 50 {
		t.Fatalf("sample has %d tasks, want 50", len(first))
	}
	gotCounts := make(map[string]int)
	for _, task := range first {
		gotCounts[task.Scenario]++
	}
	if got, want := gotCounts["STANDARD"], 20; got != want {
		t.Fatalf("STANDARD sample quota = %d, want %d", got, want)
	}
	for _, scenario := range scenarios[1:] {
		if got := gotCounts[scenario]; got != 5 {
			t.Errorf("%s sample quota = %d, want 5", scenario, got)
		}
	}
	merchantCoverage := make(map[string]bool)
	for _, task := range first {
		merchantCoverage[task.SourceMerchantID] = true
	}
	if len(merchantCoverage) < 10 {
		t.Fatalf("diagnostic sample covers only %d of 12 merchants", len(merchantCoverage))
	}
}
