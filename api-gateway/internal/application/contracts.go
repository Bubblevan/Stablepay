package application

import "context"

type DIDServiceClient interface {
	CreateDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	RegisterDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	VerifyDID(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	GetDID(ctx context.Context, did string) (map[string]interface{}, int, int, error)
	VerifySignature(ctx context.Context, req map[string]interface{}) (bool, error)
}

type PaymentServiceClient interface {
	Pay(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	GetPaymentRequirement(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	GetPayment(ctx context.Context, txID string) (map[string]interface{}, int, int, error)
	GetPaymentHistory(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
}

type VerificationServiceClient interface {
	Verify(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	BatchVerify(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	GetProof(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
}

type QueryServiceClient interface {
	GetBalance(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	GetTransactions(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	GetRevenue(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
	GetSales(ctx context.Context, req map[string]interface{}) (map[string]interface{}, int, int, error)
}
