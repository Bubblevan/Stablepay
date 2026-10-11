package main

import "testing"

func TestSourceModifiedAtBuildPrefersRunnerSnapshot(t *testing.T) {
	for _, test := range []struct {
		value string
		want  string
	}{
		{value: "false", want: "false"},
		{value: " TRUE ", want: "true"},
	} {
		t.Setenv("STABLEPAY_SOURCE_MODIFIED", test.value)
		if got := sourceModifiedAtBuild(); got != test.want {
			t.Fatalf("sourceModifiedAtBuild() = %q, want %q", got, test.want)
		}
	}
}

func TestSourceRevisionPrefersRunnerSnapshot(t *testing.T) {
	t.Setenv("STABLEPAY_SOURCE_REVISION", "abc123")
	if got := sourceRevision(); got != "abc123" {
		t.Fatalf("sourceRevision() = %q, want %q", got, "abc123")
	}
}
