// Command minikube-seed registers a low-value Devnet Merchant capability in an
// isolated local Commerce Runtime database for the six-node acceptance run.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	kitexclient "github.com/cloudwego/kitex/client"
	"github.com/stablepay/commerce-runtime/internal/catalog"
	"github.com/stablepay/commerce-runtime/internal/evidence"
	"github.com/stablepay/commerce-runtime/internal/infrastructure/mysql"
	"github.com/stablepay/commerce-runtime/internal/repository"
	dcommon "github.com/stablepay/did-service/kitex_gen/stablepay/common"
	did "github.com/stablepay/did-service/kitex_gen/stablepay/did_service"
	didsvc "github.com/stablepay/did-service/kitex_gen/stablepay/did_service/didservice"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "minikube-seed:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dsn := strings.TrimSpace(os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"))
	if dsn == "" {
		return errors.New("COMMERCE_RUNTIME_MYSQL_DSN is required")
	}
	payee := strings.TrimSpace(os.Getenv("MINIKUBE_DEVNET_PAYEE_ADDRESS"))
	if payee == "" {
		payee = "2kZGwkLnVdSxjjNueeUQmqBf3tRKMn7y1bbktRZkJWdR"
	}
	agent := strings.TrimSpace(os.Getenv("MINIKUBE_DEVNET_AGENT_ADDRESS"))
	if agent == "" {
		agent = "CVgVYZGrKfmjFTMyBJFUtQxtSfEEYZ39q7HyJbNoDeh7"
	}
	didAddr := strings.TrimSpace(os.Getenv("MINIKUBE_DID_SERVICE_ADDR"))
	if didAddr == "" {
		didAddr = "127.0.0.1:18081"
	}
	didClient, err := didsvc.NewClient("did-service", kitexclient.WithHostPorts(didAddr))
	if err != nil {
		return fmt.Errorf("create isolated DID client: %w", err)
	}
	if err := registerDID(ctx, didClient, agent, did.UserType_AGENT, "minikube-devnet-agent"); err != nil {
		return fmt.Errorf("register Agent DID: %w", err)
	}
	if err := registerDID(ctx, didClient, payee, did.UserType_DEVELOPER, "minikube-devnet-merchant"); err != nil {
		return fmt.Errorf("register Merchant payee DID: %w", err)
	}
	db, err := mysql.Open(dsn)
	if err != nil {
		return fmt.Errorf("open isolated Runtime schema: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if err := mysql.AutoMigrate(ctx, db); err != nil {
		return fmt.Errorf("migrate isolated Runtime schema: %w", err)
	}
	store := mysql.NewStore(db)
	now := time.Now().UTC()
	fixtures := []struct {
		id, sourceType, sourceRef, content string
		trust                              evidence.TrustClass
	}{
		{"minikube-devnet-x402", string(evidence.SourceProtocolDoc), "fixture://minikube-six-node/x402", "Devnet exact payment binds network, USDC asset, amount, merchant payee, and timeout.", evidence.TrustProtocolDoc},
		{"minikube-devnet-merchant", string(evidence.SourceMerchantCapabilityDoc), "fixture://minikube-six-node/merchant", "Local StablePay Merchant delivers the purchased product after verified Devnet settlement.", evidence.TrustMerchantDoc},
	}
	for _, fixture := range fixtures {
		record := evidence.NewRecord(fixture.id, fixture.sourceType, fixture.sourceRef, "v1", evidence.HashString(fixture.content), "text/plain", fixture.content, fixture.trust, now)
		if err := store.SaveEvidenceRecord(ctx, record); err != nil {
			return fmt.Errorf("save evidence %s: %w", fixture.id, err)
		}
	}
	priceMinor := int64(1) // 0.01 USDC: minimum exact amount representable by the business ledger.
	capability := &catalog.MerchantCapability{
		MerchantDID: "did:merchant:minikube-devnet", CapabilityID: "minikube-devnet-e3",
		PayeeDID: "did:solana:" + payee, Name: "Minikube Devnet low-value E3 acceptance",
		Description: "Real local Merchant and Devnet payment; one cent per fulfilled episode.",
		TaskTypes:   []string{"local-eval"}, SemanticTags: []string{"minikube", "devnet", "e3"},
		InvokeEndpoint: catalog.EndpointRef{Endpoint: "http://stablepay-merchant-backend:8787/api/v1/products/ai-agent-job-2025/execute", Method: http.MethodGet},
		InputSchemaRef: "schema://minikube-devnet/input", OutputSchemaRef: "schema://merchant-delivery",
		InputContentTypes: []string{"text/plain"}, OutputContentTypes: []string{"application/json"},
		SupportedProtocolVersions: []string{"x402-v1", "x402-v2"}, SupportedCurrencies: []string{"USDC"},
		PricingModel: "fixed", PriceHintMinor: &priceMinor, PriceHintCurrency: "USDC",
		Status: catalog.StatusActive, Availability: catalog.AvailabilityAvailable, CatalogVersion: "v1-minikube-devnet",
		Source: "minikube-six-node-devnet", SourceRef: "merchant://stablepay-merchant-backend/ai-agent-job-2025",
		ValidFrom: now.Add(-time.Minute), ValidUntil: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt: now, UpdatedAt: now,
	}
	hash, err := capability.SnapshotHash()
	if err != nil {
		return fmt.Errorf("hash Devnet capability: %w", err)
	}
	capability.SourceHash = hash
	existing, lookupErr := store.GetCapabilityVersion(ctx, capability.MerchantDID, capability.CapabilityID, capability.CatalogVersion)
	if lookupErr == nil {
		if existing.PayeeDID != capability.PayeeDID || existing.PriceHintMinor == nil || *existing.PriceHintMinor != priceMinor || existing.PriceHintCurrency != "USDC" || existing.InvokeEndpoint.Endpoint != capability.InvokeEndpoint.Endpoint {
			return errors.New("existing Minikube capability differs from the expected Devnet fixture")
		}
	} else if errors.Is(lookupErr, repository.ErrNotFound) {
		if err := store.SaveCapabilityVersion(ctx, capability); err != nil {
			return fmt.Errorf("register Devnet capability: %w", err)
		}
	} else {
		return fmt.Errorf("load existing Devnet capability: %w", lookupErr)
	}
	fmt.Printf("registered capability=%s payee=did:solana:%s price_minor=%d currency=USDC network=devnet\n", capability.CapabilityID, payee, priceMinor)
	return nil
}

func registerDID(ctx context.Context, client didsvc.Client, publicKey string, userType did.UserType, name string) error {
	request := did.NewRegisterDIDRequest()
	request.Base = dcommon.NewBaseReq()
	request.UserType = userType
	request.PublicKey = publicKey
	request.WalletAddress = publicKey
	request.WalletId = "minikube-devnet-" + name
	request.WalletName = "StablePay Minikube Devnet " + name
	response, err := client.RegisterDID(ctx, request)
	if err != nil {
		return err
	}
	if response.GetBase() != nil && response.GetBase().GetCode() != 0 {
		return fmt.Errorf("DID service rejected registration: %s", response.GetBase().GetMessage())
	}
	if strings.TrimSpace(string(response.GetDid())) == "" {
		return errors.New("DID service returned an empty identity")
	}
	fmt.Printf("registered isolated DID identity=%s\n", response.GetDid())
	return nil
}
