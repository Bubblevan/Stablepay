package livelocal

import (
	"context"
	"testing"

	"github.com/stablepay/commerce-runtime/internal/adapters"
	"github.com/stablepay/commerce-runtime/internal/invocation"
)

func TestFaultControllerRateZeroDoesNotModifyMerchant(t *testing.T) {
	fault := &faultController{CaseID: "rate-zero", Seed: 41, Kind: "merchant_transient", RatePercent: 0, Repeat: 1, Configured: true}
	merchant := &localMerchant{calls: map[string]int{}, fault: fault}
	result, err := merchant.Invoke(context.Background(), adapters.MerchantInvokeRequest{EpisodeID: "episode-1", InputRef: "local://merchant-transient", Phase: invocation.PhaseInitial, Attempt: 1})
	if err != nil || result.HTTPStatus != 402 || fault.InjectionCount != 0 {
		t.Fatalf("rate=0 modified merchant: err=%v status=%d injections=%d", err, result.HTTPStatus, fault.InjectionCount)
	}
}

func TestFaultControllerRateHundredIsSourceOfTruth(t *testing.T) {
	fault := &faultController{CaseID: "rate-full", Seed: 41, Kind: "merchant_transient", RatePercent: 100, Repeat: 1, Configured: true}
	merchant := &localMerchant{calls: map[string]int{}, fault: fault}
	_, err := merchant.Invoke(context.Background(), adapters.MerchantInvokeRequest{EpisodeID: "episode-1", InputRef: "local://merchant-transient", Phase: invocation.PhaseInitial, Attempt: 1})
	if err == nil || fault.InjectionCount != 1 || !fault.status().Configured {
		t.Fatalf("rate=100 did not authorize actual merchant fault: err=%v status=%#v", err, fault.status())
	}
}
