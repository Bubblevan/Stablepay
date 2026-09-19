package x402

import (
	"encoding/base64"
	"errors"
	"testing"
)

func TestParseRequiredSupportsMerchantV2AndLegacyV1(t *testing.T) {
	v2 := []byte(`{"x402Version":2,"resource":{"url":"https://merchant.example/api/execute/"},"accepts":[{"scheme":"exact","network":"devnet","amount":"3000000","asset":"USDC","payTo":"payee-1","maxTimeoutSeconds":300,"extra":{"currency":"USDC","productId":"product-1"}}]}`)
	parsed, err := ParseRequired(map[string]string{"PAYMENT-REQUIRED": base64.StdEncoding.EncodeToString(v2)}, nil)
	if err != nil || parsed.ProtocolVersion != "x402-v2" || parsed.AtomicAmount != 3000000 || parsed.BusinessAmountMinor != 300 || parsed.ResourceURL != "https://merchant.example/api/execute" {
		t.Fatalf("unexpected v2 parse: %#v err=%v", parsed, err)
	}

	v1 := []byte(`{"x402Version":1,"accepts":[{"scheme":"exact","network":"devnet","maxAmountRequired":"1250000","asset":"USDC","payTo":"payee-1","resource":"https://merchant.example/api/execute","maxTimeoutSeconds":300,"extra":{"currency":"USDC"}}]}`)
	parsed, err = ParseRequired(map[string]string{"Payment-Required": string(v1)}, nil)
	if err != nil || parsed.ProtocolVersion != "x402-v1" || parsed.AtomicAmount != 1250000 || parsed.BusinessAmountMinor != 125 {
		t.Fatalf("unexpected v1 parse: %#v err=%v", parsed, err)
	}
}

func TestParseRequiredFailsClosedForUnsupportedOrUnsafeAmounts(t *testing.T) {
	unsupported := []byte(`{"x402Version":2,"resource":{"url":"https://merchant.example/api/execute"},"accepts":[{"scheme":"transfer","network":"devnet","amount":"3000000","asset":"USDC","payTo":"payee-1","maxTimeoutSeconds":300}]}`)
	if _, err := ParseRequired(nil, unsupported); !errors.Is(err, ErrUnsupportedScheme) {
		t.Fatalf("expected unsupported scheme rejection, got %v", err)
	}

	exponent := []byte(`{"x402Version":2,"resource":{"url":"https://merchant.example/api/execute"},"accepts":[{"scheme":"exact","network":"devnet","amount":"3e2","asset":"USDC","payTo":"payee-1","maxTimeoutSeconds":300}]}`)
	if _, err := ParseRequired(nil, exponent); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("expected exponent rejection, got %v", err)
	}

	unknownDecimal := []byte(`{"x402Version":2,"resource":{"url":"https://merchant.example/api/execute"},"accepts":[{"scheme":"exact","network":"devnet","amount":"1.2","asset":"UNKNOWN","payTo":"payee-1","maxTimeoutSeconds":300}]}`)
	if _, err := ParseRequired(nil, unknownDecimal); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("expected unknown-currency decimal rejection, got %v", err)
	}
}

func TestParseRequiredRejectsNonRepresentableAtomicAmount(t *testing.T) {
	body := []byte(`{"x402Version":2,"resource":{"url":"https://merchant.example/api/execute"},"accepts":[{"scheme":"exact","network":"devnet","amount":"1","asset":"USDC","payTo":"payee-1","maxTimeoutSeconds":300,"extra":{"currency":"USDC"}}]}`)
	if _, err := ParseRequired(nil, body); !errors.Is(err, ErrUnrepresentableAmount) {
		t.Fatalf("expected non-representable atomic amount rejection, got %v", err)
	}
	decimal := []byte(`{"x402Version":2,"resource":{"url":"https://merchant.example/api/execute"},"accepts":[{"scheme":"exact","network":"devnet","amount":"1.25","asset":"USDC","payTo":"payee-1","maxTimeoutSeconds":300,"extra":{"currency":"USDC"}}]}`)
	if _, err := ParseRequired(nil, decimal); !errors.Is(err, ErrInvalidChallenge) {
		t.Fatalf("expected decimal atomic amount rejection, got %v", err)
	}
}
