package messaging

import (
	"testing"
	"time"
)

func TestIsSuccessfulPaymentEvent(t *testing.T) {
	cases := []struct {
		name  string
		tag   string
		event PaymentEvent
		want  bool
	}{
		{name: "success tag", tag: "PAYMENT_SUCCEEDED", want: true},
		{name: "completed status", event: PaymentEvent{Status: "completed"}, want: true},
		{name: "failed status", event: PaymentEvent{Status: "failed"}, want: false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := isSuccessful(test.tag, test.event); got != test.want {
				t.Fatalf("isSuccessful()=%v, want %v", got, test.want)
			}
		})
	}
}

func TestPaymentEventNormalization(t *testing.T) {
	if got := currencyCode("USDT"); got != 2 {
		t.Fatalf("USDT currency code=%d, want 2", got)
	}
	if got := currencyCode("USDC"); got != 1 {
		t.Fatalf("USDC currency code=%d, want 1", got)
	}
	when := timeFromUnix(1)
	if !when.Equal(time.Unix(1, 0).UTC()) {
		t.Fatalf("timestamp=%v, want unix timestamp", when)
	}
	if len(resolveNameServers([]string{"namesrv-a", "namesrv-b:9876"})) != 2 {
		t.Fatalf("expected two resolved name servers")
	}
}
