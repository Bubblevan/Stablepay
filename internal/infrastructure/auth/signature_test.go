package auth

import (
	"strings"
	"testing"
)

func TestBuildCanonicalV01(t *testing.T) {
	canonical := BuildCanonicalV01("POST", "/api/v1/pay", "skill=abc&price=1", BodySHA256([]byte(`{"a":1}`)))
	want := strings.Join([]string{
		"POST",
		"/api/v1/pay",
		"skill=abc&price=1",
		"015abd7f5cc57a2dd94b7590f04ad8084273905ee33ec5cebeae62276a97f862",
	}, "\n")
	if canonical != want {
		t.Fatalf("unexpected canonical, got=%q want=%q", canonical, want)
	}
}

func TestBodySHA256EmptyBody(t *testing.T) {
	got := BodySHA256(nil)
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got != want {
		t.Fatalf("unexpected hash, got=%s want=%s", got, want)
	}
}
