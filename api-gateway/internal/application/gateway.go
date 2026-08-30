package application

import (
	"context"
	"fmt"
)

type Service struct {
	did          DIDServiceClient
	payment      PaymentServiceClient
	verification VerificationServiceClient
	query        QueryServiceClient
}

func NewService(
	did DIDServiceClient,
	payment PaymentServiceClient,
	verification VerificationServiceClient,
	query QueryServiceClient,
) *Service {
	return &Service{
		did:          did,
		payment:      payment,
		verification: verification,
		query:        query,
	}
}

func (s *Service) Dispatch(ctx context.Context, routeName string, req map[string]interface{}) (map[string]interface{}, int, int, error) {
	switch routeName {
	case "did.create":
		return s.did.CreateDID(ctx, req)
	case "did.register":
		return s.did.RegisterDID(ctx, req)
	case "did.verify":
		return s.did.VerifyDID(ctx, req)
	case "did.get":
		did := getString(req, "did")
		return s.did.GetDID(ctx, did)
	case "payment.pay":
		if err := normalizeBusinessAmount(req); err != nil {
			return nil, 400, 10001, err
		}
		return s.payment.Pay(ctx, req)
	case "payment.require":
		return s.payment.GetPaymentRequirement(ctx, req)
	case "payment.get":
		return s.payment.GetPayment(ctx, getString(req, "tx_id"))
	case "payment.history":
		return s.payment.GetPaymentHistory(ctx, req)
	case "verification.verify":
		return s.verification.Verify(ctx, req)
	case "verification.batch":
		return s.verification.BatchVerify(ctx, req)
	case "verification.proof":
		return s.verification.GetProof(ctx, req)
	case "verification.verify_x":
		return s.verification.VerifyXTweet(ctx, req)
	case "verification.get_x_status":
		return s.verification.GetXVerificationStatus(ctx, req)
	case "query.balance":
		return s.query.GetBalance(ctx, req)
	case "query.transactions":
		return s.query.GetTransactions(ctx, req)
	case "query.revenue":
		return s.query.GetRevenue(ctx, req)
	case "query.sales":
		return s.query.GetSales(ctx, req)
	case "shortcut.pay":
		if err := normalizeBusinessAmount(req); err != nil {
			return nil, 400, 10001, err
		}
		return map[string]interface{}{
			"mode":               "guide",
			"payment_submit_api": "/api/v1/pay",
			"skill_did":          getString(req, "skill"),
			"price":              getString(req, "amount"),
			"amount_minor":       getString(req, "amount_minor"),
			"currency":           getString(req, "currency"),
		}, 200, 0, nil
	case "shortcut.verify":
		mapped := map[string]interface{}{
			"agent_did": getString(req, "agent"),
			"skill_did": getString(req, "skill"),
		}
		return s.verification.Verify(ctx, mapped)
	default:
		return nil, 404, 10002, fmt.Errorf("route not found")
	}
}

func getString(in map[string]interface{}, key string) string {
	value, ok := in[key]
	if !ok || value == nil {
		return ""
	}
	if str, ok := value.(string); ok {
		return str
	}
	return fmt.Sprintf("%v", value)
}
