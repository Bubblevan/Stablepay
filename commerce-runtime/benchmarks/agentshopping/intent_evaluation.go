package agentshopping

import (
	"fmt"
	"reflect"
)

type IntentFieldMetric struct {
	Correct  int     `json:"correct"`
	Total    int     `json:"total"`
	Accuracy float64 `json:"accuracy"`
}

type IntentParseTrial struct {
	TaskID              string          `json:"task_id"`
	Scenario            string          `json:"scenario"`
	ParseValid          bool            `json:"parse_valid"`
	ParseError          string          `json:"parse_error,omitempty"`
	ExactIntentMatch    bool            `json:"exact_intent_match"`
	FieldMatches        map[string]bool `json:"field_matches"`
	RawIntent           *Intent         `json:"raw_model_intent,omitempty"`
	CanonicalizedFields []string        `json:"canonicalized_fields,omitempty"`
	GoldIntent          Intent          `json:"gold_intent"`
	ParsedIntent        Intent          `json:"parsed_intent"`
	Downstream          PolicyTrial     `json:"downstream_treatment"`
}

type IntentParseEvaluation struct {
	TaskCount            int               `json:"task_count"`
	ParseSuccessCount    int               `json:"parse_success_count"`
	ParseSuccessRate     float64           `json:"parse_success_rate"`
	ExactIntentMatches   int               `json:"exact_intent_matches"`
	ExactIntentMatchRate float64           `json:"exact_intent_match_rate"`
	RawModelExactMatches int               `json:"raw_model_exact_matches"`
	RawModelExactRate    float64           `json:"raw_model_exact_rate"`
	Canonicalizations    map[string]int    `json:"canonicalizations"`
	Category             IntentFieldMetric `json:"category"`
	Item                 IntentFieldMetric `json:"item"`
	RequiredFormat       IntentFieldMetric `json:"required_format"`
	PreferredStyle       IntentFieldMetric `json:"preferred_style"`
	Region               IntentFieldMetric `json:"region"`
	MaxPriceMinor        IntentFieldMetric `json:"max_price_minor"`
	AllowSubstitution    IntentFieldMetric `json:"allow_substitution"`
	DownstreamBaseline   EvaluationSummary `json:"downstream_baseline_using_parsed_intents"`
	DownstreamTreatment  EvaluationSummary `json:"downstream_treatment_using_parsed_intents"`
}

func EvaluateParsedIntentBatch(catalog ImportedCatalog, tasks []Task, results []BatchIntentResult, cutoff, bootstrapSamples int, seed int64) (IntentParseEvaluation, []IntentParseTrial, error) {
	if len(tasks) == 0 || len(results) != len(tasks) {
		return IntentParseEvaluation{}, nil, fmt.Errorf("task/result cardinality mismatch: %w", ErrInvalidEvaluationSuite)
	}
	resultByID := make(map[string]BatchIntentResult, len(results))
	for _, result := range results {
		if result.TaskID == "" || resultByID[result.TaskID].TaskID != "" {
			return IntentParseEvaluation{}, nil, fmt.Errorf("duplicate or empty parse result id: %w", ErrInvalidEvaluationSuite)
		}
		resultByID[result.TaskID] = result
	}
	fields := map[string]*IntentFieldMetric{
		"category": {}, "item": {}, "required_format": {}, "preferred_style": {},
		"region": {}, "max_price_minor": {}, "allow_substitution": {},
	}
	for _, metric := range fields {
		metric.Total = len(tasks)
	}
	evaluation := IntentParseEvaluation{TaskCount: len(tasks)}
	parsedTasks := append([]Task(nil), tasks...)
	trials := make([]IntentParseTrial, 0, len(tasks))
	for index, task := range tasks {
		result, ok := resultByID[task.TaskID]
		if !ok {
			return IntentParseEvaluation{}, nil, fmt.Errorf("missing parse result for %s: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
		valid := result.Error == "" && result.Intent.Category != ""
		parsed := result.Intent
		fieldMatches := map[string]bool{
			"category":           valid && parsed.Category == task.Intent.Category,
			"item":               valid && parsed.Item == task.Intent.Item,
			"required_format":    valid && parsed.RequiredFormat == task.Intent.RequiredFormat,
			"preferred_style":    valid && parsed.PreferredStyle == task.Intent.PreferredStyle,
			"region":             valid && parsed.Region == task.Intent.Region,
			"max_price_minor":    valid && parsed.MaxPriceMinor == task.Intent.MaxPriceMinor,
			"allow_substitution": valid && parsed.AllowSubstitution == task.Intent.AllowSubstitution,
		}
		exact := valid && reflect.DeepEqual(parsed, task.Intent)
		// Confirmation is a server invariant and is intentionally not an extracted field.
		if exact {
			evaluation.ExactIntentMatches++
		}
		if result.RawIntent != nil && reflect.DeepEqual(*result.RawIntent, task.Intent) {
			evaluation.RawModelExactMatches++
		}
		if evaluation.Canonicalizations == nil {
			evaluation.Canonicalizations = make(map[string]int)
		}
		for _, field := range result.CanonicalizedFields {
			evaluation.Canonicalizations[field]++
		}
		if valid {
			evaluation.ParseSuccessCount++
		}
		for name, matched := range fieldMatches {
			if matched {
				fields[name].Correct++
			}
		}
		parsedTasks[index].Intent = parsed
		if !valid {
			// A failed extraction stays in the denominator and must not silently
			// inherit the gold intent for downstream retrieval.
			parsedTasks[index].Intent = Intent{MaxPriceMinor: 1, ConfirmationNeeded: true}
		}
		trials = append(trials, IntentParseTrial{TaskID: task.TaskID, Scenario: task.Scenario,
			ParseValid: valid, ParseError: result.Error, ExactIntentMatch: exact, FieldMatches: fieldMatches,
			RawIntent: result.RawIntent, CanonicalizedFields: append([]string(nil), result.CanonicalizedFields...),
			GoldIntent: task.Intent, ParsedIntent: parsed, Downstream: PolicyTrial{}})
	}
	for _, metric := range fields {
		if metric.Total > 0 {
			metric.Accuracy = float64(metric.Correct) / float64(metric.Total)
		}
	}
	evaluation.Category = *fields["category"]
	evaluation.Item = *fields["item"]
	evaluation.RequiredFormat = *fields["required_format"]
	evaluation.PreferredStyle = *fields["preferred_style"]
	evaluation.Region = *fields["region"]
	evaluation.MaxPriceMinor = *fields["max_price_minor"]
	evaluation.AllowSubstitution = *fields["allow_substitution"]
	if evaluation.TaskCount > 0 {
		evaluation.ParseSuccessRate = float64(evaluation.ParseSuccessCount) / float64(evaluation.TaskCount)
		evaluation.ExactIntentMatchRate = float64(evaluation.ExactIntentMatches) / float64(evaluation.TaskCount)
		evaluation.RawModelExactRate = float64(evaluation.RawModelExactMatches) / float64(evaluation.TaskCount)
	}
	downstream, downstreamTrials, err := EvaluateStructuredTasks(catalog, parsedTasks, cutoff, bootstrapSamples, seed)
	if err != nil {
		return IntentParseEvaluation{}, nil, err
	}
	for i := range trials {
		trials[i].Downstream = downstreamTrials[i].Treatment
	}
	evaluation.DownstreamBaseline = downstream.Baseline
	evaluation.DownstreamTreatment = downstream.Treatment
	return evaluation, trials, nil
}
