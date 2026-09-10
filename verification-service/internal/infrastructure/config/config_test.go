package config

import "testing"

func TestConfigRequiresCanonicalPaymentTopic(t *testing.T) {
	cfg := Config{RPCAddress: ":8085", MySQLDSN: "dsn", RocketNameservers: []string{"127.0.0.1:9876"}, RocketTopic: "payment.success"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected event type to be rejected as physical topic")
	}
	cfg.RocketTopic = PaymentEventsTopic
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected canonical topic, got %v", err)
	}
}
