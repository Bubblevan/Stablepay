package main

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/eval"
	"github.com/stablepay/commerce-runtime/internal/observability"
)

func TestReplayDatasetExportsFiniteMetrics(t *testing.T) {
	scenarios, err := readScenarios("../../testdata/s11/scenarios.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	results, err := (eval.Client{}).Run(context.Background(), scenarios)
	if err != nil {
		t.Fatal(err)
	}
	metrics := observability.Compute(results, time.Now().UTC())
	if path := firstNonFinite(reflect.ValueOf(metrics), "metrics"); path != "" {
		t.Fatalf("metrics contains non-finite value at %s", path)
	}
	if _, err := json.Marshal(metrics); err != nil {
		t.Fatal(err)
	}
}

func firstNonFinite(value reflect.Value, path string) string {
	if !value.IsValid() {
		return ""
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return ""
		}
		return firstNonFinite(value.Elem(), path)
	}
	switch value.Kind() {
	case reflect.Float32, reflect.Float64:
		if math.IsNaN(value.Float()) || math.IsInf(value.Float(), 0) {
			return path
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if found := firstNonFinite(value.Field(index), path+"."+value.Type().Field(index).Name); found != "" {
				return found
			}
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			if found := firstNonFinite(value.MapIndex(key), path+"["+key.String()+"]"); found != "" {
				return found
			}
		}
	case reflect.Slice, reflect.Array:
		for index := 0; index < value.Len(); index++ {
			if found := firstNonFinite(value.Index(index), path+"["+string(rune('0'+index))+"]"); found != "" {
				return found
			}
		}
	}
	return ""
}
