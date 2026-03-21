package auth

import "testing"

func TestBuildCanonicalV01(t *testing.T) {
	canonical := BuildCanonicalV01("POST", "/api/v1/pay", "skill=abc&price=1", BodySHA256([]byte(`{"a":1}`)), "2026-03-11T00:00:00Z", "n-1")
	expectedPrefix := "POST\n/api/v1/pay\nskill=abc&price=1\n"
	if canonical[:len(expectedPrefix)] != expectedPrefix {
		t.Fatalf("unexpected canonical prefix: %s", canonical)
	}
}

func TestBodySHA256EmptyBody(t *testing.T) {
	got := BodySHA256(nil)
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Fatalf("unexpected hash, got=%s want=%s", got, want)
	}
}
