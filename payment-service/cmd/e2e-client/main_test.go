package main

import "testing"

func TestParseAmountMinor(t *testing.T) {
	tests := []struct {
		input string
		want  int64
		bad   bool
	}{
		{input: "0.01", want: 1},
		{input: "1", want: 100},
		{input: "1.00", want: 100},
		{input: "0", bad: true},
		{input: "-0.1", bad: true},
		{input: "1.001", bad: true},
	}
	for _, tt := range tests {
		got, err := parseAmountMinor(tt.input)
		if tt.bad {
			if err == nil {
				t.Fatalf("parseAmountMinor(%q) unexpectedly succeeded with %d", tt.input, got)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Fatalf("parseAmountMinor(%q) = %d, %v; want %d", tt.input, got, err, tt.want)
		}
	}
}

func TestBuildGatewayCanonical(t *testing.T) {
	got := buildGatewayCanonical([]byte(`{"a":1}`))
	want := "POST\n/api/v1/pay\n\n015abd7f5cc57a2dd94b7590f04ad8084273905ee33ec5cebeae62276a97f862"
	if got != want {
		t.Fatalf("unexpected gateway canonical: got %q want %q", got, want)
	}
}
