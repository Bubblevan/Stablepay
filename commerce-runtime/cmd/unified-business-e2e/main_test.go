package main

import "testing"

func TestSameUnifiedSolanaPayee(t *testing.T) {
	const address = "NBZtLpE3u95YfirCqMyjZg5p9LZ1f9rjtFnCeuibeCy"
	tests := []struct {
		name     string
		actual   string
		expected string
		want     bool
	}{
		{name: "did equals raw address", actual: "did:solana:" + address, expected: address, want: true},
		{name: "raw address equals did", actual: address, expected: "did:solana:" + address, want: true},
		{name: "exact did equality", actual: "did:solana:" + address, expected: "did:solana:" + address, want: true},
		{name: "base58 case remains significant", actual: "did:solana:" + "nbztLpE3u95YfirCqMyjZg5p9LZ1f9rjtFnCeuibeCy", expected: "did:solana:" + address, want: false},
		{name: "different address rejected", actual: "did:solana:another-wallet", expected: "did:solana:" + address, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sameUnifiedSolanaPayee(test.actual, test.expected); got != test.want {
				t.Fatalf("sameUnifiedSolanaPayee(%q, %q) = %v, want %v", test.actual, test.expected, got, test.want)
			}
		})
	}
}
