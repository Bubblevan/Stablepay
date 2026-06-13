package service

import (
	"context"
	"testing"

	appPort "github.com/stablepay/merchant-server/internal/application/port"
	"github.com/stablepay/merchant-server/internal/domain/entity"
	"github.com/stablepay/merchant-server/internal/domain/repository"
	domainSvc "github.com/stablepay/merchant-server/internal/domain/service"
)

type fakeProductRepo struct {
	products map[string]*entity.Product
}

var _ repository.ProductRepository = (*fakeProductRepo)(nil)

func (r *fakeProductRepo) FindAll(ctx context.Context, page, size int) ([]*entity.Product, int64, error) {
	items := make([]*entity.Product, 0, len(r.products))
	for _, p := range r.products {
		if p.Status == entity.ProductStatusActive {
			items = append(items, p)
		}
	}
	return items, int64(len(items)), nil
}

func (r *fakeProductRepo) FindBySKUID(ctx context.Context, skuID string) (*entity.Product, error) {
	p, ok := r.products[skuID]
	if !ok {
		return nil, entity.ErrProductNotFound
	}
	return p, nil
}

func (r *fakeProductRepo) FindByID(ctx context.Context, id int64) (*entity.Product, error) {
	for _, p := range r.products {
		if p.ID == id {
			return p, nil
		}
	}
	return nil, entity.ErrProductNotFound
}

func (r *fakeProductRepo) Save(ctx context.Context, product *entity.Product) error {
	if r.products == nil {
		r.products = map[string]*entity.Product{}
	}
	r.products[product.SKUID] = product
	return nil
}

func (r *fakeProductRepo) UpdateStatus(ctx context.Context, id int64, status entity.ProductStatus) error {
	p, err := r.FindByID(ctx, id)
	if err != nil {
		return err
	}
	p.Status = status
	return nil
}

type fakePaymentVerifier struct {
	result *appPort.VerifyPurchaseResult
	err    error
}

func (v fakePaymentVerifier) VerifyPurchase(ctx context.Context, req appPort.VerifyPurchaseRequest) (*appPort.VerifyPurchaseResult, error) {
	if v.err != nil {
		return nil, v.err
	}
	if v.result == nil {
		return &appPort.VerifyPurchaseResult{Purchased: false}, nil
	}
	return v.result, nil
}

func newTestProduct() *entity.Product {
	return entity.NewProductBuilder().
		WithSKUID("report-001").
		WithTitle("Report").
		WithDescription("Paid report").
		WithPrice("2.00", entity.CurrencyUSDC).
		WithStatus(entity.ProductStatusActive).
		WithSellerAddress("seller111").
		MustBuild()
}

func newTestApp(verifier appPort.PaymentVerifier) *ProductAppService {
	repo := &fakeProductRepo{products: map[string]*entity.Product{
		"report-001": newTestProduct(),
	}}
	return NewProductAppService(
		repo,
		domainSvc.NewProductDomainService(repo),
		verifier,
		"https://merchant.example.com",
		"seller111",
		"test-proof-secret",
		"https://ai.wenfu.cn",
		"EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v",
		"solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
	)
}

func TestExecutePurchaseWithoutPaymentReturnsX402V2Requirement(t *testing.T) {
	app := newTestApp(fakePaymentVerifier{})
	result, err := app.ExecutePurchase(context.Background(), ExecutePurchaseCommand{
		SKUID:    "report-001",
		AgentDID: "did:solana:agent111",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Purchased {
		t.Fatal("expected unpaid result")
	}
	if result.PaymentRequired == nil {
		t.Fatal("expected payment requirement")
	}
	if result.PaymentRequired.X402Version != domainSvc.X402VersionV2 {
		t.Fatalf("got version %d", result.PaymentRequired.X402Version)
	}
	if result.PaymentRequired.Accepts[0].Amount != "2000000" {
		t.Fatalf("got amount %s, want 2000000", result.PaymentRequired.Accepts[0].Amount)
	}
	if result.PaymentRequired.Resource.URL != "https://merchant.example.com/api/v1/products/report-001/execute" {
		t.Fatalf("unexpected resource url: %s", result.PaymentRequired.Resource.URL)
	}
}

func TestExecutePurchaseWhenPaidReturnsContentAndProof(t *testing.T) {
	app := newTestApp(fakePaymentVerifier{result: &appPort.VerifyPurchaseResult{
		Purchased: true,
		TxID:      "tx-001",
		TxHash:    "hash-001",
		Proof:     map[string]any{"purchased": true},
	}})
	result, err := app.ExecutePurchase(context.Background(), ExecutePurchaseCommand{
		SKUID:            "report-001",
		AgentDID:         "did:solana:agent111",
		PaymentSignature: "signed-payment-payload",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Purchased {
		t.Fatal("expected purchased result")
	}
	if result.PaymentRequired != nil {
		t.Fatal("did not expect payment requirement after purchase")
	}
	if result.MerchantProof == nil || result.MerchantProof.Signature == "" {
		t.Fatal("expected signed merchant proof")
	}
	if result.Content["tx_hash"] != "hash-001" {
		t.Fatalf("unexpected content tx_hash: %v", result.Content["tx_hash"])
	}
}

func TestListProductsNormalizesPagination(t *testing.T) {
	app := newTestApp(fakePaymentVerifier{})
	items, total, err := app.ListProducts(context.Background(), -1, 1000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("got total=%d len=%d", total, len(items))
	}
	if items[0].SKUID != "report-001" {
		t.Fatalf("unexpected sku %s", items[0].SKUID)
	}
}

func TestGetProductDetailReturnsProduct(t *testing.T) {
	app := newTestApp(fakePaymentVerifier{})
	item, err := app.GetProductDetail(context.Background(), "report-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item == nil || item.SKUID != "report-001" {
		t.Fatal("expected product")
	}
}

func TestGetProductDetailNotFound(t *testing.T) {
	app := newTestApp(fakePaymentVerifier{})
	_, err := app.GetProductDetail(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}
