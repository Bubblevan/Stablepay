package auth

import "testing"

func TestParseTimestampFlexibleRFC3339(t *testing.T) {
	_, err := ParseTimestampFlexible("2026-03-11T00:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseTimestampFlexibleUnix(t *testing.T) {
	_, err := ParseTimestampFlexible("1767225600")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseTimestampFlexibleInvalid(t *testing.T) {
	_, err := ParseTimestampFlexible("abc")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}
