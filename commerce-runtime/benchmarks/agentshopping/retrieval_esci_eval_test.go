package agentshopping

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

type esciReferenceGroup struct {
	QueryID    string `json:"query_id"`
	Query      string `json:"query"`
	Candidates []struct {
		ProductID   string `json:"product_id"`
		Label       string `json:"label"`
		Title       string `json:"title"`
		Brand       string `json:"brand"`
		Color       string `json:"color"`
		BulletPoint string `json:"bullet_point"`
		Description string `json:"description"`
	} `json:"candidates"`
}

type esciRankingMetric struct {
	Ranker                  string  `json:"ranker"`
	Cutoff                  int     `json:"cutoff"`
	QueryCount              int     `json:"query_count"`
	CandidateCount          int     `json:"candidate_count"`
	ExactTargets            int     `json:"exact_targets"`
	ExactTargetsRetrieved   int     `json:"exact_targets_retrieved"`
	ExactRecallMacro        float64 `json:"exact_target_recall_macro"`
	ExactRecallMicro        float64 `json:"exact_target_recall_micro"`
	ExactRecallMacroCeiling float64 `json:"exact_target_recall_macro_ceiling"`
	ExactRecallMicroCeiling float64 `json:"exact_target_recall_micro_ceiling"`
	ExactTargetQueryHitRate float64 `json:"exact_target_query_hit_rate"`
	NDCGMacro               float64 `json:"esci_ndcg_macro"`
}

type esciRankingReport struct {
	SchemaVersion string              `json:"schema_version"`
	DatasetRole   string              `json:"dataset_role"`
	SamplePath    string              `json:"sample_path"`
	SampleSHA256  string              `json:"sample_sha256"`
	TaskCount     int                 `json:"task_count"`
	Boundary      string              `json:"boundary"`
	Metrics       []esciRankingMetric `json:"metrics"`
}

// TestRunESCIReferenceThroughRuntimeRanking is an opt-in local benchmark. It
// runs the real discovery ranker on the already-prepared 500-query ESCI sample
// and writes a bounded diagnostic artifact under the ignored .local-run tree.
func TestRunESCIReferenceThroughRuntimeRanking(t *testing.T) {
	if os.Getenv("STABLEPAY_RUN_ESCI_EVAL") != "1" {
		t.Skip("set STABLEPAY_RUN_ESCI_EVAL=1 to run the local ESCI reference benchmark")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	samplePath := strings.TrimSpace(os.Getenv("STABLEPAY_ESCI_SAMPLE_PATH"))
	if samplePath == "" {
		samplePath = filepath.Join(root, ".local-run", "agent-shopping", "reference-data", "esci-us-test-20261011",
			"sparse-exact-20261011", "esci-us-test-500-queries.jsonl")
	}
	samplePath, err = filepath.Abs(samplePath)
	if err != nil {
		t.Fatalf("resolve ESCI reference sample path: %v", err)
	}
	referenceRoot, err := filepath.Abs(filepath.Join(root, ".local-run", "agent-shopping", "reference-data"))
	if err != nil {
		t.Fatalf("resolve local reference-data root: %v", err)
	}
	relativeSample, err := filepath.Rel(referenceRoot, samplePath)
	if err != nil || filepath.IsAbs(relativeSample) || relativeSample == ".." || strings.HasPrefix(relativeSample, ".."+string(os.PathSeparator)) {
		t.Fatalf("ESCI sample must remain under the local reference-data directory: %s", samplePath)
	}
	info, err := os.Stat(samplePath)
	if err != nil {
		t.Fatalf("open pre-staged local reference sample: %v", err)
	}
	if info.Size() > 20*1024*1024 {
		t.Fatalf("refusing reference sample larger than 20 MiB: %d bytes", info.Size())
	}
	sampleBytes, err := os.ReadFile(samplePath)
	if err != nil {
		t.Fatalf("read local reference sample: %v", err)
	}
	sampleHash := sha256.Sum256(sampleBytes)

	groups, err := readESCIReferenceGroups(sampleBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 500 {
		t.Fatalf("expected exactly 500 sampled queries, got %d", len(groups))
	}
	cutoffs := []int{5, 10, 20}
	var titleCoverageWeights []float64
	if rawWeights := strings.TrimSpace(os.Getenv("STABLEPAY_ESCI_TITLE_COVERAGE_WEIGHTS")); rawWeights != "" {
		for _, rawWeight := range strings.Split(rawWeights, ",") {
			weight, parseErr := strconv.ParseFloat(strings.TrimSpace(rawWeight), 64)
			if parseErr != nil || weight <= 0 {
				t.Fatalf("invalid positive title-coverage weight %q", rawWeight)
			}
			titleCoverageWeights = append(titleCoverageWeights, weight)
		}
	}
	var titlePhraseWeights []float64
	if rawWeights := strings.TrimSpace(os.Getenv("STABLEPAY_ESCI_TITLE_PHRASE_WEIGHTS")); rawWeights != "" {
		for _, rawWeight := range strings.Split(rawWeights, ",") {
			weight, parseErr := strconv.ParseFloat(strings.TrimSpace(rawWeight), 64)
			if parseErr != nil || weight <= 0 {
				t.Fatalf("invalid positive title-phrase weight %q", rawWeight)
			}
			titlePhraseWeights = append(titlePhraseWeights, weight)
		}
	}
	report := esciRankingReport{SchemaVersion: "stablepay.esci-runtime-ranking.v2",
		DatasetRole:  "PUBLIC_REFERENCE_ONLY_NOT_STABLEPAY_PRODUCTION_RESULT",
		SamplePath:   filepath.ToSlash(filepath.Join(".local-run", "agent-shopping", "reference-data", relativeSample)),
		SampleSHA256: "sha256:" + hex.EncodeToString(sampleHash[:]), TaskCount: len(groups),
		Boundary: "Runtime BM25 with title-coverage and ordered-title-bigram reranking is evaluated only over each ESCI query's judged candidate subset. It excludes unjudged full-catalog negatives, Merchant category/format routing, availability, region, price, buyer constraints, offer generation, checkout, payment and fulfillment. It is not a StablePay production metric or resume claim.",
	}
	for _, cutoff := range cutoffs {
		productionCoverageRanker := "runtime_bm25_title_coverage_" + strconv.FormatFloat(defaultDiscoveryTitleCoverageWeight, 'f', -1, 64)
		for _, ranker := range []string{"runtime_bm25_baseline", productionCoverageRanker,
			"runtime_bm25_production", "title_overlap_reference"} {
			metric, err := scoreESCIReference(groups, ranker, cutoff)
			if err != nil {
				t.Fatal(err)
			}
			report.Metrics = append(report.Metrics, metric)
		}
		for _, weight := range titleCoverageWeights {
			ranker := "runtime_bm25_title_coverage_" + strconv.FormatFloat(weight, 'f', -1, 64)
			metric, err := scoreESCIReference(groups, ranker, cutoff)
			if err != nil {
				t.Fatal(err)
			}
			report.Metrics = append(report.Metrics, metric)
		}
		for _, weight := range titlePhraseWeights {
			if weight == defaultDiscoveryTitlePhraseWeight {
				continue
			}
			ranker := "runtime_bm25_title_phrase_" + strconv.FormatFloat(weight, 'f', -1, 64)
			metric, err := scoreESCIReference(groups, ranker, cutoff)
			if err != nil {
				t.Fatal(err)
			}
			report.Metrics = append(report.Metrics, metric)
		}
	}

	outputRoot := filepath.Join(root, ".local-run", "agent-shopping", "reference-evaluations")
	if err := os.MkdirAll(outputRoot, 0o700); err != nil {
		t.Fatalf("create local evaluation output directory: %v", err)
	}
	outputDir := filepath.Join(outputRoot, "esci-runtime-ranking-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.Mkdir(outputDir, 0o700); err != nil {
		t.Fatalf("create unique local evaluation run: %v", err)
	}
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "metrics.json"), append(encoded, '\n'), 0o600); err != nil {
		t.Fatalf("write local evaluation metrics: %v", err)
	}
	t.Logf("ESCI runtime-ranking diagnostic written: %s", outputDir)
	t.Logf("ESCI runtime-ranking metrics: %s", encoded)
}

func readESCIReferenceGroups(data []byte) ([]esciReferenceGroup, error) {
	groups := make([]esciReferenceGroup, 0, 500)
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for line := 1; scanner.Scan(); line++ {
		var group esciReferenceGroup
		if err := json.Unmarshal(scanner.Bytes(), &group); err != nil {
			return nil, fmt.Errorf("decode ESCI sample line %d: %w", line, err)
		}
		if group.QueryID == "" || strings.TrimSpace(group.Query) == "" || len(group.Candidates) == 0 {
			return nil, fmt.Errorf("ESCI sample line %d is missing query or candidates", line)
		}
		groups = append(groups, group)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return groups, nil
}

func scoreESCIReference(groups []esciReferenceGroup, ranker string, cutoff int) (esciRankingMetric, error) {
	metric := esciRankingMetric{Ranker: ranker, Cutoff: cutoff, QueryCount: len(groups)}
	macroRecall, macroRecallCeiling, queryHits, ndcgValues := 0.0, 0.0, 0, 0.0
	totalRetrievable := 0
	for _, group := range groups {
		products := make([]Product, len(group.Candidates))
		relevance := make(map[string]string, len(group.Candidates))
		for index, candidate := range group.Candidates {
			if candidate.ProductID == "" || (candidate.Label != "E" && candidate.Label != "S" && candidate.Label != "C" && candidate.Label != "I") {
				return esciRankingMetric{}, fmt.Errorf("invalid ESCI candidate in query %s", group.QueryID)
			}
			products[index] = Product{SKUID: candidate.ProductID, Category: "esci-reference", Item: group.QueryID,
				Format: "judged-candidate", Title: candidate.Title, Description: candidate.Description,
				Tags:       []string{candidate.BulletPoint},
				Attributes: map[string]any{"brand": candidate.Brand, "color": candidate.Color},
				Currency:   "USDC", Status: "active"}
			relevance[candidate.ProductID] = candidate.Label
		}
		metric.CandidateCount += len(products)
		var ranked []Product
		if strings.HasPrefix(ranker, "runtime_bm25_title_phrase_") {
			weightText := strings.TrimPrefix(ranker, "runtime_bm25_title_phrase_")
			weight, err := strconv.ParseFloat(weightText, 64)
			if err != nil || weight <= 0 {
				return esciRankingMetric{}, fmt.Errorf("invalid title-phrase weight in ranker %q", ranker)
			}
			intent := Intent{Category: "esci-reference", Item: group.QueryID, RequiredFormat: "judged-candidate"}.WithSearchText(group.Query)
			ranked = rankDiscoveryProductsWithWeights(products, intent, DefaultDiscoveryCandidateWindow,
				defaultDiscoveryTitleCoverageWeight, weight)
		} else if strings.HasPrefix(ranker, "runtime_bm25_title_coverage_") {
			weightText := strings.TrimPrefix(ranker, "runtime_bm25_title_coverage_")
			weight, err := strconv.ParseFloat(weightText, 64)
			if err != nil || weight <= 0 {
				return esciRankingMetric{}, fmt.Errorf("invalid title-coverage weight in ranker %q", ranker)
			}
			intent := Intent{Category: "esci-reference", Item: group.QueryID, RequiredFormat: "judged-candidate"}.WithSearchText(group.Query)
			ranked = rankDiscoveryProductsWithTitleCoverageWeight(products, intent, DefaultDiscoveryCandidateWindow, weight)
		} else if ranker == "runtime_bm25_baseline" {
			intent := Intent{Category: "esci-reference", Item: group.QueryID, RequiredFormat: "judged-candidate"}.WithSearchText(group.Query)
			ranked = rankDiscoveryProductsWithTitleCoverageWeight(products, intent, DefaultDiscoveryCandidateWindow, 0)
		} else if ranker == "runtime_bm25" || ranker == "runtime_bm25_production" {
			intent := Intent{Category: "esci-reference", Item: group.QueryID, RequiredFormat: "judged-candidate"}.WithSearchText(group.Query)
			ranked = rankDiscoveryProducts(products, intent, DefaultDiscoveryCandidateWindow)
		} else {
			ranked = rankESCIByTitleOverlap(products, group.Query)
		}
		exactCount, exactFound := 0, 0
		for _, candidate := range group.Candidates {
			if candidate.Label == "E" {
				exactCount++
			}
		}
		if exactCount == 0 {
			return esciRankingMetric{}, fmt.Errorf("query %s has no exact-target judgment", group.QueryID)
		}
		limit := cutoff
		if len(ranked) < limit {
			limit = len(ranked)
		}
		queryCeiling := exactCount
		if limit < queryCeiling {
			queryCeiling = limit
		}
		macroRecallCeiling += float64(queryCeiling) / float64(exactCount)
		totalRetrievable += queryCeiling
		dcg := 0.0
		for index, candidate := range ranked[:limit] {
			label := relevance[candidate.SKUID]
			grade := map[string]int{"E": 3, "S": 2, "C": 1, "I": 0}[label]
			gain := math.Pow(2, float64(grade)) - 1
			dcg += gain / math.Log2(float64(index+2))
			if label == "E" {
				exactFound++
			}
		}
		ideal := make([]float64, 0, len(group.Candidates))
		for _, label := range relevance {
			grade := map[string]int{"E": 3, "S": 2, "C": 1, "I": 0}[label]
			ideal = append(ideal, math.Pow(2, float64(grade))-1)
		}
		sort.Sort(sort.Reverse(sort.Float64Slice(ideal)))
		idcg := 0.0
		for index := 0; index < limit; index++ {
			idcg += ideal[index] / math.Log2(float64(index+2))
		}
		if idcg > 0 {
			ndcgValues += dcg / idcg
		}
		metric.ExactTargets += exactCount
		metric.ExactTargetsRetrieved += exactFound
		macroRecall += float64(exactFound) / float64(exactCount)
		if exactFound > 0 {
			queryHits++
		}
	}
	queryCount := float64(len(groups))
	metric.ExactRecallMacro = macroRecall / queryCount
	metric.ExactRecallMicro = float64(metric.ExactTargetsRetrieved) / float64(metric.ExactTargets)
	metric.ExactRecallMacroCeiling = macroRecallCeiling / queryCount
	metric.ExactRecallMicroCeiling = float64(totalRetrievable) / float64(metric.ExactTargets)
	metric.ExactTargetQueryHitRate = float64(queryHits) / queryCount
	metric.NDCGMacro = ndcgValues / queryCount
	return metric, nil
}

func TestESCIExactRecallCeilingAccountsForPerQueryTargetCount(t *testing.T) {
	newGroup := func(queryID string, exactCount, irrelevantCount int) esciReferenceGroup {
		group := esciReferenceGroup{QueryID: queryID, Query: "reference item"}
		for index := 0; index < exactCount+irrelevantCount; index++ {
			label := "I"
			if index < exactCount {
				label = "E"
			}
			group.Candidates = append(group.Candidates, struct {
				ProductID   string `json:"product_id"`
				Label       string `json:"label"`
				Title       string `json:"title"`
				Brand       string `json:"brand"`
				Color       string `json:"color"`
				BulletPoint string `json:"bullet_point"`
				Description string `json:"description"`
			}{ProductID: fmt.Sprintf("%s-%02d", queryID, index), Label: label, Title: "reference item"})
		}
		return group
	}
	metric, err := scoreESCIReference([]esciReferenceGroup{
		newGroup("one-target", 1, 1),
		newGroup("four-targets", 4, 1),
	}, "runtime_bm25_baseline", 1)
	if err != nil {
		t.Fatal(err)
	}
	if metric.ExactRecallMacroCeiling != 0.625 || metric.ExactRecallMicroCeiling != 0.4 {
		t.Fatalf("unexpected per-query Recall@1 ceilings: macro=%v micro=%v", metric.ExactRecallMacroCeiling, metric.ExactRecallMicroCeiling)
	}
}

func rankESCIByTitleOverlap(products []Product, query string) []Product {
	queryTerms := uniqueTerms(tokenizeDiscoveryText(query))
	scored := make([]struct {
		product Product
		score   float64
	}, 0, len(products))
	for _, product := range products {
		terms := uniqueTerms(tokenizeDiscoveryText(product.Title))
		present := make(map[string]bool, len(terms))
		for _, term := range terms {
			present[term] = true
		}
		score := 0.0
		for _, term := range queryTerms {
			if present[term] {
				score++
			}
		}
		if len(queryTerms) > 0 {
			score /= float64(len(queryTerms))
		}
		scored = append(scored, struct {
			product Product
			score   float64
		}{product: product, score: score})
	}
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].product.SKUID < scored[j].product.SKUID
	})
	ranked := make([]Product, len(scored))
	for index := range scored {
		ranked[index] = scored[index].product
	}
	return ranked
}
