package main

import (
	"context"
	did_service "github.com/stablepay/did-service/kitex_gen/stablepay/did_service"
)

// DIDServiceImpl implements the last service interface defined in the IDL.
type DIDServiceImpl struct{}

// CreateDID implements the DIDServiceImpl interface.
func (s *DIDServiceImpl) CreateDID(ctx context.Context, req *did_service.CreateDIDRequest) (resp *did_service.CreateDIDResponse, err error) {
	// TODO: Your code here...
	return
}

// GetDID implements the DIDServiceImpl interface.
func (s *DIDServiceImpl) GetDID(ctx context.Context, req *did_service.GetDIDRequest) (resp *did_service.GetDIDResponse, err error) {
	// TODO: Your code here...
	return
}

// VerifySignature implements the DIDServiceImpl interface.
func (s *DIDServiceImpl) VerifySignature(ctx context.Context, req *did_service.VerifySignatureRequest) (resp *did_service.VerifySignatureResponse, err error) {
	// TODO: Your code here...
	return
}

// UpdateDIDConfig implements the DIDServiceImpl interface.
func (s *DIDServiceImpl) UpdateDIDConfig(ctx context.Context, req *did_service.UpdateDIDConfigRequest) (resp *did_service.UpdateDIDConfigResponse, err error) {
	// TODO: Your code here...
	return
}
