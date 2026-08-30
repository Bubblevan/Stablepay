// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	appPort "github.com/stablepay/merchant-server/internal/application/port"
	domainSvc "github.com/stablepay/merchant-server/internal/domain/service"
)

func TestVerifyPurchaseCallsGatewayAndMapsProof(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/verify/proof" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("agent_did"); got != "did:solana:agent111" {
			t.Fatalf("agent_did = %q", got)
		}
		if got := r.URL.Query().Get("skill_did"); got != "did:solana:seller111" {
			t.Fatalf("skill_did = %q", got)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-key" {
			t.Fatalf("api key = %q", got)
		}
		if got := r.Header.Get(domainSvc.X402PaymentSignatureHeader); got != "signed-payload" {
			t.Fatalf("payment signature = %q", got)
		}
		_, _ = w.Write([]byte(`{
			"code": 0,
			"message": "success",
			"data": {
				"purchased": true,
				"tx_id": "tx-001",
				"tx_hash": "hash-001",
				"proof_version": "v1"
			}
		}`))
	}))
	defer server.Close()

	client := NewStablePayClient(server.URL, "test-key")
	result, err := client.VerifyPurchase(context.Background(), appPort.VerifyPurchaseRequest{
		AgentDID:         "did:solana:agent111",
		SkillDID:         "did:solana:seller111",
		PaymentSignature: "signed-payload",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Purchased || result.TxID != "tx-001" || result.TxHash != "hash-001" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.Proof["proof_version"] != "v1" {
		t.Fatalf("proof not mapped: %+v", result.Proof)
	}
}

func TestVerifyPurchase404MeansNotPurchased(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	client := NewStablePayClient(server.URL, "")
	result, err := client.VerifyPurchase(context.Background(), appPort.VerifyPurchaseRequest{
		AgentDID: "did:solana:agent111",
		SkillDID: "did:solana:seller111",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Purchased {
		t.Fatal("expected not purchased")
	}
}
