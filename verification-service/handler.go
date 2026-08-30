package main

import (
	"context"
	verification_service "github.com/stablepay/verification-service/kitex_gen/stablepay/verification_service"
)

// VerificationServiceImpl implements the last service interface defined in the IDL.
type VerificationServiceImpl struct{}

// VerifyPurchase implements the VerificationServiceImpl interface.
func (s *VerificationServiceImpl) VerifyPurchase(ctx context.Context, req *verification_service.VerifyPurchaseRequest) (resp *verification_service.VerifyPurchaseResponse, err error) {
	// TODO: Your code here...
	return
}

// BatchVerifyPurchase implements the VerificationServiceImpl interface.
func (s *VerificationServiceImpl) BatchVerifyPurchase(ctx context.Context, req *verification_service.BatchVerifyPurchaseRequest) (resp *verification_service.BatchVerifyPurchaseResponse, err error) {
	// TODO: Your code here...
	return
}

// GetPurchaseProof implements the VerificationServiceImpl interface.
func (s *VerificationServiceImpl) GetPurchaseProof(ctx context.Context, req *verification_service.GetPurchaseProofRequest) (resp *verification_service.GetPurchaseProofResponse, err error) {
	// TODO: Your code here...
	return
}
