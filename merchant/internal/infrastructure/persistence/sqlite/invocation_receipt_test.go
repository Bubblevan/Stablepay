package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stablepay/merchant-server/internal/domain/repository"
)

func TestInvocationReceiptRoundTripIsDurable(t *testing.T) {
	repo, err := NewProductRepo(":memory:", true)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	want := &repository.InvocationReceipt{IdempotencyKey: "op-1", AgentDID: "did:agent:1", SKUID: "sku-1", ResponseJSON: []byte(`{"purchased":true}`)}
	if err := repo.SaveInvocationReceipt(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetInvocationReceipt(context.Background(), want.IdempotencyKey, want.AgentDID, want.SKUID)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.ResponseJSON) != string(want.ResponseJSON) {
		t.Fatalf("receipt mismatch: got=%s want=%s", got.ResponseJSON, want.ResponseJSON)
	}
}

func TestDeliveryResultSameKeyConcurrentCallsAllocateOneGiftCode(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "merchant.db")
	first, err := NewProductRepo(dbPath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.SeedGiftCodes(ctx, []string{"gift-001", "gift-002"}); err != nil {
		t.Fatal(err)
	}
	second, err := NewProductRepo(dbPath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	start := make(chan struct{})
	results := make(chan string, 2)
	errorsOut := make(chan error, 2)
	var wait sync.WaitGroup
	for _, repo := range []*ProductRepoImpl{first, second} {
		wait.Add(1)
		go func(repo *ProductRepoImpl) {
			defer wait.Done()
			<-start
			receipt, err := repo.GetOrCreateDeliveryResult(ctx, "same-key", "did:agent:1", "sku-1", func(giftCode string) ([]byte, error) {
				return []byte(fmt.Sprintf(`{"gift_code":%q}`, giftCode)), nil
			})
			if err != nil {
				errorsOut <- err
				return
			}
			results <- string(receipt.ResponseJSON)
		}(repo)
	}
	close(start)
	wait.Wait()
	close(results)
	close(errorsOut)
	for err := range errorsOut {
		t.Fatal(err)
	}
	var responses []string
	for value := range results {
		responses = append(responses, value)
	}
	if len(responses) != 2 || responses[0] != responses[1] || responses[0] != `{"gift_code":"gift-001"}` {
		t.Fatalf("same-key results were not stable: %#v", responses)
	}
	var allocated int
	if err := first.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM gift_codes WHERE status = 'allocated'`).Scan(&allocated); err != nil {
		t.Fatal(err)
	}
	if allocated != 1 {
		t.Fatalf("same-key calls allocated %d gift codes", allocated)
	}
}

func TestDeliveryResultSurvivesRestartAndRejectsMetadataConflict(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "merchant.db")
	repo, err := NewProductRepo(dbPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SeedGiftCodes(ctx, []string{"gift-restart"}); err != nil {
		t.Fatal(err)
	}
	want, err := repo.GetOrCreateDeliveryResult(ctx, "restart-key", "did:agent:1", "sku-1", func(giftCode string) ([]byte, error) {
		return []byte(fmt.Sprintf(`{"gift_code":%q}`, giftCode)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewProductRepo(dbPath, true)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	got, err := restarted.GetOrCreateDeliveryResult(ctx, "restart-key", "did:agent:1", "sku-1", func(string) ([]byte, error) {
		return nil, errors.New("builder must not run for persisted replay")
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.ResponseJSON) != string(want.ResponseJSON) {
		t.Fatalf("restart replay changed result: got=%s want=%s", got.ResponseJSON, want.ResponseJSON)
	}
	if _, err := restarted.GetOrCreateDeliveryResult(ctx, "restart-key", "did:agent:2", "sku-1", func(string) ([]byte, error) { return []byte(`{}`), nil }); !errors.Is(err, repository.ErrInvocationReceiptConflict) {
		t.Fatalf("same key with different agent did not conflict: %v", err)
	}
	if _, err := restarted.GetOrCreateDeliveryResult(ctx, "restart-key", "did:agent:1", "sku-2", func(string) ([]byte, error) { return []byte(`{}`), nil }); !errors.Is(err, repository.ErrInvocationReceiptConflict) {
		t.Fatalf("same key with different sku did not conflict: %v", err)
	}
}

func TestDeliveryResultRollbackLeavesGiftReusable(t *testing.T) {
	ctx := context.Background()
	repo, err := NewProductRepo(filepath.Join(t.TempDir(), "merchant.db"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	if err := repo.SeedGiftCodes(ctx, []string{"gift-rollback"}); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected failure after allocation")
	if _, err := repo.GetOrCreateDeliveryResult(ctx, "rollback-key", "did:agent:1", "sku-1", func(giftCode string) ([]byte, error) {
		if giftCode != "gift-rollback" {
			t.Fatalf("unexpected allocated code: %s", giftCode)
		}
		return nil, injected
	}); !errors.Is(err, injected) {
		t.Fatalf("expected injected rollback error, got %v", err)
	}
	if _, err := repo.GetInvocationReceipt(ctx, "rollback-key", "did:agent:1", "sku-1"); !errors.Is(err, repository.ErrInvocationReceiptNotFound) {
		t.Fatalf("rollback left an invocation receipt: %v", err)
	}
	recovered, err := repo.GetOrCreateDeliveryResult(ctx, "rollback-key", "did:agent:1", "sku-1", func(giftCode string) ([]byte, error) {
		return []byte(fmt.Sprintf(`{"gift_code":%q}`, giftCode)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(recovered.ResponseJSON) != `{"gift_code":"gift-rollback"}` {
		t.Fatalf("rollback did not make code reusable: %s", recovered.ResponseJSON)
	}
}
