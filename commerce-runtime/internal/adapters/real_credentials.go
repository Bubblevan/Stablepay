package adapters

// This file contains the local Devnet credential provider used by the unified
// business E2E. It is deliberately an adapter-side test harness: production
// runtime code receives only an opaque CredentialRef and the payment-service
// remains the cryptographic authority for the supplied signature.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	kitexclient "github.com/cloudwego/kitex/client"
	"github.com/gagliardetto/solana-go"
	"github.com/mr-tron/base58"

	bchain "github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/blockchain_adapter"
	bchainsvc "github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/blockchain_adapter/blockchainadapterservice"
	bcommon "github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/common"
	"github.com/stablepay/commerce-runtime/internal/payment"
	dcommon "github.com/stablepay/did-service/kitex_gen/stablepay/common"
	did "github.com/stablepay/did-service/kitex_gen/stablepay/did_service"
	didsvc "github.com/stablepay/did-service/kitex_gen/stablepay/did_service/didservice"
)

const defaultUnifiedAgentKeypair = ".local-run/secrets/e2e-agent.json"

type credentialKeypairFile struct {
	Address    string `json:"address,omitempty"`
	PublicKey  string `json:"public_key,omitempty"`
	PrivateKey string `json:"private_key"`
}

// RealCredentialProvider builds the exact partial transaction and payment
// signature consumed by payment-service. The in-memory cache is not a
// correctness boundary: payment-service owns durable idempotency. It exists
// so a retry of a persisted runtime CredentialRef sends the identical bytes.
type RealCredentialProvider struct {
	agentKey     solana.PrivateKey
	agentDID     string
	blockchain   bchainsvc.Client
	did          didsvc.Client
	mu           sync.Mutex
	credentials  map[string]PaymentCredentials
	fingerprints map[string]string
}

type RealCredentialProviderConfig struct {
	AgentKeypairPath string
	DIDServiceAddr   string
	BlockchainAddr   string
	RPCTimeout       time.Duration
}

func NewRealCredentialProvider(cfg RealCredentialProviderConfig) (*RealCredentialProvider, error) {
	path := strings.TrimSpace(cfg.AgentKeypairPath)
	if path == "" {
		path = os.Getenv("STABLEPAY_E2E_AGENT_KEYPAIR_PATH")
	}
	if path == "" {
		path = defaultUnifiedAgentKeypair
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve agent keypair path: %w", err)
	}
	key, err := loadCredentialKeypair(path)
	if err != nil {
		return nil, fmt.Errorf("load agent keypair: %w", err)
	}
	timeout := cfg.RPCTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	didAddr := firstNonEmpty(cfg.DIDServiceAddr, os.Getenv("STABLEPAY_DID_SERVICE_ADDR"), "127.0.0.1:8081")
	blockchainAddr := firstNonEmpty(cfg.BlockchainAddr, os.Getenv("STABLEPAY_BLOCKCHAIN_ADAPTER_ADDR"), "127.0.0.1:8083")
	didClient, err := didsvc.NewClient("did-service", kitexclient.WithHostPorts(didAddr), kitexclient.WithRPCTimeout(timeout))
	if err != nil {
		return nil, fmt.Errorf("create DID client: %w", err)
	}
	blockchainClient, err := bchainsvc.NewClient("blockchain-adapter", kitexclient.WithHostPorts(blockchainAddr), kitexclient.WithRPCTimeout(timeout))
	if err != nil {
		return nil, fmt.Errorf("create Blockchain Adapter client: %w", err)
	}
	return &RealCredentialProvider{
		agentKey: key, agentDID: didForCredentialKey(key), blockchain: blockchainClient, did: didClient,
		credentials: make(map[string]PaymentCredentials), fingerprints: make(map[string]string),
	}, nil
}

func (p *RealCredentialProvider) AgentDID() string { return p.agentDID }

// EnsureIdentities makes the real DID service the registry of both identities.
// The payment-service later verifies the actual payment signature through that
// same service; the runtime DID adapter below is only its policy layer.
func (p *RealCredentialProvider) EnsureIdentities(ctx context.Context, skillDID string) error {
	if p == nil || p.did == nil {
		return errors.New("real credential provider is not initialized")
	}
	if _, err := registerCredentialDID(ctx, p.did, p.agentKey.PublicKey().String(), did.UserType_AGENT, "unified-agent"); err != nil {
		return fmt.Errorf("register Agent DID: %w", err)
	}
	skillWallet, err := walletFromDID(skillDID)
	if err != nil {
		return fmt.Errorf("resolve Skill DID: %w", err)
	}
	if _, err := registerCredentialDID(ctx, p.did, skillWallet, did.UserType_DEVELOPER, "unified-skill"); err != nil {
		return fmt.Errorf("register Skill DID: %w", err)
	}
	return nil
}

func (p *RealCredentialProvider) Credentials(ctx context.Context, intent payment.PaymentIntent) (PaymentCredentials, error) {
	if err := intent.Validate(); err != nil {
		return PaymentCredentials{}, err
	}
	if p == nil || p.blockchain == nil {
		return PaymentCredentials{}, errors.New("real credential provider is not initialized")
	}
	if intent.RequesterDID != p.agentDID {
		return PaymentCredentials{}, fmt.Errorf("payment requester DID %q does not match the configured Agent DID", intent.RequesterDID)
	}
	ref := strings.TrimSpace(intent.CredentialRef)
	if ref == "" {
		return PaymentCredentials{}, errors.New("payment credential reference is required")
	}
	fingerprint := payment.RequestFingerprint(intent)
	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.credentials[ref]; ok {
		if p.fingerprints[ref] != fingerprint {
			return PaymentCredentials{}, errors.New("credential reference was reused for a different payment intent")
		}
		return existing, nil
	}

	toWallet, err := walletFromDID(intent.PayeeDID)
	if err != nil {
		return PaymentCredentials{}, err
	}
	currency, err := credentialCurrency(intent.Currency)
	if err != nil {
		return PaymentCredentials{}, err
	}
	txID := intent.IntentID
	buildReq := bchain.NewBuildUnsignedTransactionRequest()
	buildReq.Base = bcommon.NewBaseReq()
	buildReq.FromWalletAddress = p.agentKey.PublicKey().String()
	buildReq.ToWalletAddress = toWallet
	buildReq.AmountMinor = intent.AmountMinor
	buildReq.Currency = currency
	buildReq.TxId = &txID
	buildResp, err := p.blockchain.BuildUnsignedTransaction(ctx, buildReq)
	if err != nil {
		return PaymentCredentials{}, fmt.Errorf("build signed payment transaction: %w", err)
	}
	if err := checkCredentialBase(buildResp.GetBase()); err != nil {
		return PaymentCredentials{}, fmt.Errorf("build signed payment transaction rejected: %w", err)
	}
	if strings.TrimSpace(buildResp.GetUnsignedTxBase64()) == "" {
		return PaymentCredentials{}, errors.New("Blockchain Adapter returned an empty unsigned transaction")
	}
	tx := &solana.Transaction{}
	if err := tx.UnmarshalBase64(buildResp.GetUnsignedTxBase64()); err != nil {
		return PaymentCredentials{}, fmt.Errorf("decode unsigned payment transaction: %w", err)
	}
	if !containsCredentialSigner(tx.Message.Signers(), p.agentKey.PublicKey()) {
		return PaymentCredentials{}, fmt.Errorf("payment transaction does not require Agent signer %s", p.agentKey.PublicKey())
	}
	if _, err := tx.PartialSign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(p.agentKey.PublicKey()) {
			return &p.agentKey
		}
		return nil
	}); err != nil {
		return PaymentCredentials{}, fmt.Errorf("sign payment transaction: %w", err)
	}
	signedTx, err := tx.ToBase64()
	if err != nil {
		return PaymentCredentials{}, fmt.Errorf("encode signed payment transaction: %w", err)
	}
	timestamp := time.Now().Unix()
	nonce := "commerce-runtime-" + digestCredentialReference(ref)
	payload := paymentBusinessSignPayload(intent.RequesterDID, intent.PayeeDID, intent.AmountMinor, currencyForSignature(intent.Currency), signedTx, timestamp, nonce)
	signature, err := signCredentialMessage(p.agentKey, payload)
	if err != nil {
		return PaymentCredentials{}, fmt.Errorf("sign payment authorization: %w", err)
	}
	credentials := PaymentCredentials{Reference: ref, Signature: signature, Timestamp: fmt.Sprint(timestamp), Nonce: nonce, SignedTxBase64: signedTx}
	p.credentials[ref] = credentials
	p.fingerprints[ref] = fingerprint
	return credentials, nil
}

func (p *RealCredentialProvider) CredentialsFor(reference string) (PaymentCredentials, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	value, ok := p.credentials[strings.TrimSpace(reference)]
	return value, ok
}

func loadCredentialKeypair(path string) (solana.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var wrapper credentialKeypairFile
	if err := json.Unmarshal(data, &wrapper); err == nil && strings.TrimSpace(wrapper.PrivateKey) != "" {
		decoded, err := base58.Decode(strings.TrimSpace(wrapper.PrivateKey))
		if err != nil {
			return nil, fmt.Errorf("decode private_key: %w", err)
		}
		return validateCredentialKey(solana.PrivateKey(decoded), wrapper.PublicKey, wrapper.Address)
	}
	var numbers []byte
	if err := json.Unmarshal(data, &numbers); err != nil {
		return nil, errors.New("keypair must be a StablePay JSON wrapper or Solana CLI numeric array")
	}
	return validateCredentialKey(solana.PrivateKey(numbers), "", "")
}

func validateCredentialKey(key solana.PrivateKey, expectedPublic, expectedAddress string) (solana.PrivateKey, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	derived := key.PublicKey().String()
	if expectedPublic != "" && expectedPublic != derived {
		return nil, fmt.Errorf("public_key does not match private_key")
	}
	if expectedAddress != "" && expectedAddress != derived {
		return nil, fmt.Errorf("address does not match private_key")
	}
	return key, nil
}

func registerCredentialDID(ctx context.Context, client didsvc.Client, publicKey string, userType did.UserType, name string) (string, error) {
	req := did.NewRegisterDIDRequest()
	req.Base = dcommon.NewBaseReq()
	req.UserType = userType
	req.PublicKey = publicKey
	req.WalletAddress = publicKey
	req.WalletId = "stablepay-unified-" + name
	req.WalletName = "StablePay unified E2E " + name
	resp, err := client.RegisterDID(ctx, req)
	if err != nil {
		return "", err
	}
	if err := checkCredentialBase(resp.GetBase()); err != nil {
		return "", err
	}
	if strings.TrimSpace(string(resp.GetDid())) == "" {
		return "", errors.New("DID service returned an empty DID")
	}
	return string(resp.GetDid()), nil
}

func walletFromDID(value string) (string, error) {
	const prefix = "did:solana:"
	if !strings.HasPrefix(value, prefix) {
		return "", fmt.Errorf("payee DID %q is not a Solana DID", value)
	}
	wallet := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	if _, err := solana.PublicKeyFromBase58(wallet); err != nil {
		return "", fmt.Errorf("invalid Solana payee DID: %w", err)
	}
	return wallet, nil
}

func credentialCurrency(value string) (bcommon.Currency, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "USDC":
		return bcommon.Currency_USDC, nil
	case "USDT":
		return bcommon.Currency_USDT, nil
	default:
		return 0, fmt.Errorf("unsupported payment currency %q", value)
	}
}

func currencyForSignature(value string) int64 {
	if strings.EqualFold(strings.TrimSpace(value), "USDT") {
		return 2
	}
	return 1
}

func paymentBusinessSignPayload(agentDID, skillDID string, amountMinor, currency int64, signedTx string, timestamp int64, nonce string) string {
	sum := sha256.Sum256([]byte(signedTx))
	return fmt.Sprintf("%s|%s|%d|%d|%s%d%s", agentDID, skillDID, amountMinor, currency, hex.EncodeToString(sum[:]), timestamp, nonce)
}

func signCredentialMessage(key solana.PrivateKey, message string) (string, error) {
	signature, err := key.Sign([]byte(message))
	if err != nil {
		return "", err
	}
	return base58.Encode(signature[:]), nil
}

func digestCredentialReference(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:24]
}

func didForCredentialKey(key solana.PrivateKey) string {
	return "did:solana:" + key.PublicKey().String()
}

func containsCredentialSigner(signers solana.PublicKeySlice, expected solana.PublicKey) bool {
	for _, signer := range signers {
		if signer.Equals(expected) {
			return true
		}
	}
	return false
}

type credentialBaseResponse interface {
	GetCode() int32
	GetMessage() string
}

func checkCredentialBase(base credentialBaseResponse) error {
	if base == nil || base.GetCode() == 0 {
		return nil
	}
	return fmt.Errorf("code=%d message=%s", base.GetCode(), base.GetMessage())
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
