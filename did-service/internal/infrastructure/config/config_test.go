package config

import "testing"

func TestResolvePathUsesConfigPathEnvironment(t *testing.T) {
	t.Setenv("CONFIG_PATH", "/etc/stablepay/did.yaml")
	if got := ResolvePath(); got != "/etc/stablepay/did.yaml" {
		t.Fatalf("ResolvePath()=%q, want configured path", got)
	}
}

func TestResolvePathDefaultsToDevelopmentConfig(t *testing.T) {
	t.Setenv("CONFIG_PATH", "")
	if got := ResolvePath(); got != "config/dev.yaml" {
		t.Fatalf("ResolvePath()=%q, want development config", got)
	}
}
