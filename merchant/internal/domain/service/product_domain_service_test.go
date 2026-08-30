package domain_service

import (
	"testing"

	"github.com/stablepay/merchant-server/internal/domain/entity"
)

func TestBuildPaymentRequiredV2(t *testing.T) {
	product := entity.NewProductBuilder().
		WithSKUID("report-001").
		WithTitle("Report").
		WithDescription("Paid report").
		WithPrice("2.00", entity.CurrencyUSDC).
		WithStatus(entity.ProductStatusActive).
		WithTags([]string{"AI", "Agent"}).
		WithSellerAddress("seller111").
		MustBuild()

	svc := NewProductDomainService(nil)
	required, err := svc.BuildPaymentRequiredV2(BuildPaymentRequiredInput{
		Product:        product,
		ResourceURL:    "https://merchant.example.com/api/v1/products/report-001/execute",
		PayTo:          "seller111",
		Asset:          "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		Network:        "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
		FacilitatorURL: "https://ai.wenfu.cn",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if required.X402Version != X402VersionV2 {
		t.Fatalf("got version %d", required.X402Version)
	}
	if required.Accepts[0].Amount != "2000000" {
		t.Fatalf("got amount %s, want 2000000", required.Accepts[0].Amount)
	}
	if required.Accepts[0].Network != "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1" {
		t.Fatalf("unexpected network %s", required.Accepts[0].Network)
	}
}

func TestEncodePaymentRequiredHeader(t *testing.T) {
	header, err := EncodePaymentRequiredHeader(&X402PaymentRequired{
		X402Version: X402VersionV2,
		Resource:    X402ResourceInfo{URL: "https://merchant.example.com/resource"},
		Accepts: []X402PaymentRequirements{{
			Scheme:            X402SchemeExact,
			Network:           "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			Amount:            "1",
			Asset:             "USDC",
			PayTo:             "seller111",
			MaxTimeoutSeconds: 60,
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if header == "" {
		t.Fatal("expected non-empty header")
	}
}

func TestBuildSignedProof(t *testing.T) {
	svc := NewProductDomainService(nil)
	proof, err := svc.BuildSignedProof("did:solana:agent111", "report-001", "test-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proof.Signature == "" {
		t.Fatal("expected non-empty signature")
	}
	if proof.AgentDID != "did:solana:agent111" {
		t.Fatalf("got agent_did %s", proof.AgentDID)
	}
	if proof.ProductID != "report-001" {
		t.Fatalf("got product_id %s", proof.ProductID)
	}
	if proof.ProofVersion != "v1" {
		t.Fatalf("got version %s", proof.ProofVersion)
	}
}

func TestBuildSignedProofRejectsEmptyParams(t *testing.T) {
	svc := NewProductDomainService(nil)
	_, err := svc.BuildSignedProof("", "pid", "secret")
	if err == nil {
		t.Fatal("expected error for empty agent_did")
	}
	_, err = svc.BuildSignedProof("did:agent", "", "secret")
	if err == nil {
		t.Fatal("expected error for empty product_id")
	}
	_, err = svc.BuildSignedProof("did:agent", "pid", "")
	if err == nil {
		t.Fatal("expected error for empty secret")
	}
}
