package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/kitex/client"
	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/google/uuid"
	"github.com/mr-tron/base58"

	bchain "github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/blockchain_adapter"
	bchainsvc "github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/blockchain_adapter/blockchainadapterservice"
	bcommon "github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/common"
	dcommon "github.com/stablepay/did-service/kitex_gen/stablepay/common"
	did "github.com/stablepay/did-service/kitex_gen/stablepay/did_service"
	didsvc "github.com/stablepay/did-service/kitex_gen/stablepay/did_service/didservice"
	"github.com/stablepay/payment-service/internal/domain/vo"
	"github.com/stablepay/payment-service/pkg/constants"
	"github.com/stablepay/payment-service/pkg/utils"
)

const (
	defaultAgentKeypair = ".local-run/secrets/e2e-agent.json"
	defaultRPC          = "https://api.devnet.solana.com"
)

type keypairFile struct {
	Address     string `json:"address,omitempty"`
	DID         string `json:"did,omitempty"`
	PublicKey   string `json:"public_key,omitempty"`
	PrivateKey  string `json:"private_key"`
	Role        string `json:"role,omitempty"`
	Description string `json:"description,omitempty"`
}

type paymentBody struct {
	AgentDID       string `json:"agent_did"`
	SkillDID       string `json:"skill_did"`
	Amount         string `json:"amount"`
	Currency       string `json:"currency"`
	Signature      string `json:"signature"`
	Timestamp      int64  `json:"timestamp"`
	Nonce          string `json:"nonce"`
	SignedTxBase64 string `json:"signed_tx_base64"`
}

// Prepared is the non-secret handoff from the real-signing helper to the
// PowerShell E2E runner. The keypair itself is never included in this file.
type Prepared struct {
	AgentDID          string `json:"agent_did"`
	SkillDID          string `json:"skill_did"`
	AgentPublicKey    string `json:"agent_public_key"`
	SkillPublicKey    string `json:"skill_public_key"`
	Amount            string `json:"amount"`
	AmountMinor       int64  `json:"amount_minor"`
	Currency          string `json:"currency"`
	Timestamp         string `json:"timestamp"`
	Nonce             string `json:"nonce"`
	GatewayNonce      string `json:"gateway_nonce"`
	IdempotencyKey    string `json:"idempotency_key"`
	SignedTxBase64    string `json:"signed_tx_base64"`
	PaymentSignature  string `json:"payment_signature"`
	GatewaySignature  string `json:"gateway_signature"`
	BodyFile          string `json:"body_file"`
	RecentBlockhash   string `json:"recent_blockhash"`
	FeeEstimateMinor  int64  `json:"fee_estimate_minor"`
	AgentBalanceMinor int64  `json:"agent_balance_minor"`
	SkillBalanceMinor int64  `json:"skill_balance_minor"`
}

func main() {
	var (
		agentPath   string
		skillPath   string
		hotPath     string
		skillDIDArg string
		didAddr     string
		adapterAddr string
		outputPath  string
		bodyPath    string
		amount      string
		currency    string
		rpcURL      string
		timeout     time.Duration
	)
	flag.StringVar(&agentPath, "agent-keypair-path", os.Getenv("STABLEPAY_E2E_AGENT_KEYPAIR_PATH"), "gitignored E2E Agent keypair path")
	flag.StringVar(&skillPath, "skill-keypair-path", os.Getenv("STABLEPAY_E2E_SKILL_KEYPAIR_PATH"), "optional Skill keypair path; defaults to the hot wallet")
	flag.StringVar(&hotPath, "hot-wallet-path", os.Getenv("STABLEPAY_HOTWALLET_PATH"), "hot wallet path used as the default Skill/merchant identity")
	flag.StringVar(&skillDIDArg, "skill-did", os.Getenv("STABLEPAY_E2E_SKILL_DID"), "optional existing Skill DID")
	flag.StringVar(&didAddr, "did-address", envOr("STABLEPAY_DID_SERVICE_ADDR", "127.0.0.1:8081"), "did-service Kitex address")
	flag.StringVar(&adapterAddr, "adapter-address", envOr("STABLEPAY_BLOCKCHAIN_ADAPTER_ADDR", "127.0.0.1:8083"), "blockchain-adapter Kitex address")
	flag.StringVar(&outputPath, "output", ".local-run/e2e-prepared.json", "prepared input JSON output path")
	flag.StringVar(&bodyPath, "body-file", ".local-run/e2e-payment-body.json", "exact HTTP body output path")
	flag.StringVar(&amount, "amount", envOr("STABLEPAY_E2E_AMOUNT", "0.01"), "payment amount in major units")
	flag.StringVar(&currency, "currency", envOr("STABLEPAY_E2E_CURRENCY", "USDC"), "payment currency")
	flag.StringVar(&rpcURL, "rpc-url", envOr("STABLEPAY_SOLANA_RPC_URL", defaultRPC), "Solana RPC endpoint used for fee-payer prerequisite checks")
	flag.DurationVar(&timeout, "timeout", 20*time.Second, "per-RPC call timeout")
	flag.Parse()

	if err := run(agentPath, skillPath, hotPath, skillDIDArg, didAddr, adapterAddr, outputPath, bodyPath, amount, currency, rpcURL, timeout); err != nil {
		fmt.Fprintf(os.Stderr, "e2e-client: %v\n", err)
		os.Exit(1)
	}
}

func run(agentPath, skillPath, hotPath, skillDIDArg, didAddr, adapterAddr, outputPath, bodyPath, amount, currency, rpcURL string, timeout time.Duration) error {
	if strings.TrimSpace(agentPath) == "" {
		agentPath = defaultAgentKeypair
	}
	if strings.TrimSpace(hotPath) == "" {
		return errors.New("hot wallet path is required for the real Devnet fee payer")
	}
	agentPath, err := filepath.Abs(agentPath)
	if err != nil {
		return fmt.Errorf("resolve agent keypair path: %w", err)
	}
	hotPath, err = filepath.Abs(hotPath)
	if err != nil {
		return fmt.Errorf("resolve hot wallet path: %w", err)
	}
	if samePath(agentPath, hotPath) {
		return errors.New("refusing to use the hot wallet as the E2E Agent identity; provide a separate STABLEPAY_E2E_AGENT_KEYPAIR_PATH")
	}

	agentKey, agentCreated, err := loadOrCreateKeypair(agentPath, "e2e_agent")
	if err != nil {
		return fmt.Errorf("load E2E Agent keypair: %w", err)
	}
	hotKey, _, err := loadOrCreateKeypair(hotPath, "hot_wallet")
	if err != nil {
		return fmt.Errorf("load hot wallet keypair: %w", err)
	}

	amountMinor, err := parseAmountMinor(amount)
	if err != nil {
		return err
	}
	currency = strings.ToUpper(strings.TrimSpace(currency))
	bcurrency, pcurrency, err := parseCurrency(currency)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	didClient, err := didsvc.NewClient("did-service", client.WithHostPorts(didAddr), client.WithRPCTimeout(timeout))
	if err != nil {
		return fmt.Errorf("create DID client: %w", err)
	}
	adapterClient, err := bchainsvc.NewClient("blockchain-adapter", client.WithHostPorts(adapterAddr), client.WithRPCTimeout(timeout))
	if err != nil {
		return fmt.Errorf("create blockchain adapter client: %w", err)
	}

	agentDID := didForKey(agentKey)
	if _, err := registerDID(ctx, didClient, agentKey.PublicKey().String(), did.UserType_AGENT, "e2e-agent"); err != nil {
		return fmt.Errorf("real Agent DID registration/reuse failed: %w", err)
	}

	var skillKey solana.PrivateKey
	skillPublicKey := ""
	if strings.TrimSpace(skillDIDArg) != "" {
		skillPublicKey = publicKeyFromDID(skillDIDArg)
		if skillPublicKey == "" {
			return fmt.Errorf("invalid Skill DID %q", skillDIDArg)
		}
	} else {
		if strings.TrimSpace(skillPath) == "" {
			skillPath = hotPath
		}
		skillPath, err = filepath.Abs(skillPath)
		if err != nil {
			return fmt.Errorf("resolve skill keypair path: %w", err)
		}
		skillKey, _, err = loadOrCreateKeypair(skillPath, "e2e_skill")
		if err != nil {
			return fmt.Errorf("load Skill keypair: %w", err)
		}
		skillPublicKey = skillKey.PublicKey().String()
		skillDIDArg = didForKey(skillKey)
	}
	if _, err := registerDID(ctx, didClient, skillPublicKey, did.UserType_DEVELOPER, "e2e-skill"); err != nil {
		return fmt.Errorf("real Skill DID registration/reuse failed: %w", err)
	}

	// Agent must already own the canonical Devnet token. The adapter's
	// transaction builder intentionally does not create a source ATA.
	agentBalance, err := tokenBalance(ctx, adapterClient, agentKey.PublicKey().String(), bcurrency)
	if err != nil {
		return fmt.Errorf("Devnet funding prerequisite check for Agent token balance failed: %w", err)
	}
	requiredTokenRaw, err := utils.BusinessMinorToTokenRaw(amountMinor)
	if err != nil {
		return fmt.Errorf("convert E2E business amount to token raw units: %w", err)
	}
	if agentBalance < requiredTokenRaw {
		created := "reused"
		if agentCreated {
			created = "generated"
		}
		return fmt.Errorf("Devnet funding prerequisite not met: E2E Agent %s at %s has %d %s raw units, but %d are required for %d business minor units; fund this separate Agent wallet's %s ATA on Devnet before running the real E2E (the hot wallet only pays gas)", created, agentKey.PublicKey().String(), agentBalance, currency, requiredTokenRaw, amountMinor, currency)
	}

	// The default Skill is the hot wallet, so make the destination prerequisite
	// explicit as well. A custom Skill keypair needs its ATA funded/created by
	// the operator before this harness can submit a transfer to it.
	skillBalance, err := tokenBalance(ctx, adapterClient, skillPublicKey, bcurrency)
	if err != nil {
		return fmt.Errorf("Devnet Skill token account check failed: %w", err)
	}
	if err := checkTokenAccount(ctx, rpcURL, skillPublicKey, currency); err != nil {
		return err
	}
	if err := checkFeePayerSOL(ctx, rpcURL, hotKey.PublicKey()); err != nil {
		return err
	}

	buildReq := bchain.NewBuildUnsignedTransactionRequest()
	buildReq.Base = bcommon.NewBaseReq()
	buildReq.FromWalletAddress = agentKey.PublicKey().String()
	buildReq.ToWalletAddress = skillPublicKey
	buildReq.AmountMinor = amountMinor
	buildReq.Currency = bcurrency
	buildReq.TxId = stringPtr("e2e-" + uuid.NewString())
	buildResp, err := adapterClient.BuildUnsignedTransaction(ctx, buildReq)
	if err != nil {
		return fmt.Errorf("real Blockchain Adapter BuildUnsignedTransaction failed: %w", err)
	}
	if err := checkBase(buildResp.GetBase()); err != nil {
		return fmt.Errorf("real Blockchain Adapter BuildUnsignedTransaction rejected: %w", err)
	}
	if buildResp.UnsignedTxBase64 == "" {
		return errors.New("real Blockchain Adapter returned an empty unsigned transaction")
	}

	tx := &solana.Transaction{}
	if err := tx.UnmarshalBase64(buildResp.UnsignedTxBase64); err != nil {
		return fmt.Errorf("decode unsigned transaction from Adapter: %w", err)
	}
	if !containsSigner(tx.Message.Signers(), agentKey.PublicKey()) {
		return fmt.Errorf("Adapter transaction does not require the E2E Agent signer %s", agentKey.PublicKey())
	}
	if _, err := tx.PartialSign(func(key solana.PublicKey) *solana.PrivateKey {
		if key.Equals(agentKey.PublicKey()) {
			return &agentKey
		}
		return nil
	}); err != nil {
		return fmt.Errorf("client-side Agent transaction signing failed: %w", err)
	}
	signedTx, err := tx.ToBase64()
	if err != nil {
		return fmt.Errorf("encode client-signed transaction: %w", err)
	}

	timestamp := time.Now().Unix()
	nonce := "payment-" + uuid.NewString()
	gatewayNonce := "gateway-" + uuid.NewString()
	idempotencyKey := "e2e-" + uuid.NewString()
	paymentPayload := vo.PaymentBusinessSignPayload(agentDID, skillDIDArg, amountMinor, pcurrency, signedTx, timestamp, nonce)
	paymentSignature, err := signBase58(agentKey, paymentPayload)
	if err != nil {
		return fmt.Errorf("PaymentSignature generation failed: %w", err)
	}
	if err := verifyRemoteSignature(ctx, didClient, agentDID, paymentSignature, paymentPayload, strconv.FormatInt(timestamp, 10), "diagnostic-payment-"+uuid.NewString()); err != nil {
		return fmt.Errorf("real DID service rejected generated PaymentSignature: %w", err)
	}
	body := paymentBody{
		AgentDID:       agentDID,
		SkillDID:       skillDIDArg,
		Amount:         amount,
		Currency:       currency,
		Signature:      paymentSignature,
		Timestamp:      timestamp,
		Nonce:          nonce,
		SignedTxBase64: signedTx,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal exact payment body: %w", err)
	}
	gatewayCanonical := buildGatewayCanonical(bodyBytes)
	gatewaySignature, err := signBase58(agentKey, gatewayCanonical)
	if err != nil {
		return fmt.Errorf("GatewaySignature generation failed: %w", err)
	}
	// Verify the exact Gateway canonical payload through the real DID service
	// before handing the request to HTTP. Use a distinct diagnostic nonce so the
	// actual Gateway nonce remains one-shot for the end-to-end request.
	if err := verifyRemoteSignature(ctx, didClient, agentDID, gatewaySignature, gatewayCanonical, strconv.FormatInt(timestamp, 10), "diagnostic-"+uuid.NewString()); err != nil {
		return fmt.Errorf("real DID service rejected generated GatewaySignature: %w", err)
	}

	bodyPath, err = filepath.Abs(bodyPath)
	if err != nil {
		return fmt.Errorf("resolve body path: %w", err)
	}
	outputPath, err = filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	if err := writePrivateLocalFile(bodyPath, bodyBytes); err != nil {
		return fmt.Errorf("write exact payment body: %w", err)
	}
	prepared := Prepared{
		AgentDID:          agentDID,
		SkillDID:          skillDIDArg,
		AgentPublicKey:    agentKey.PublicKey().String(),
		SkillPublicKey:    skillPublicKey,
		Amount:            amount,
		AmountMinor:       amountMinor,
		Currency:          currency,
		Timestamp:         strconv.FormatInt(timestamp, 10),
		Nonce:             nonce,
		GatewayNonce:      gatewayNonce,
		IdempotencyKey:    idempotencyKey,
		SignedTxBase64:    signedTx,
		PaymentSignature:  paymentSignature,
		GatewaySignature:  gatewaySignature,
		BodyFile:          bodyPath,
		RecentBlockhash:   buildResp.RecentBlockhash,
		FeeEstimateMinor:  buildResp.FeeEstimateMinor,
		AgentBalanceMinor: agentBalance,
		SkillBalanceMinor: skillBalance,
	}
	preparedBytes, err := json.MarshalIndent(prepared, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal prepared inputs: %w", err)
	}
	if err := writePrivateLocalFile(outputPath, preparedBytes); err != nil {
		return fmt.Errorf("write prepared inputs: %w", err)
	}

	fmt.Printf("prepared real E2E identity agent=%s skill=%s amount_minor=%d required_token_raw=%d agent_balance_raw=%d body=%s\n", agentDID, skillDIDArg, amountMinor, requiredTokenRaw, agentBalance, bodyPath)
	return nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func loadOrCreateKeypair(path, role string) (solana.PrivateKey, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key, err := solana.NewRandomPrivateKey()
		if err != nil {
			return nil, false, err
		}
		wrapper := keypairFile{
			Address:     key.PublicKey().String(),
			DID:         didForKey(key),
			PublicKey:   key.PublicKey().String(),
			PrivateKey:  base58.Encode(key),
			Role:        role,
			Description: "local-only StablePay deterministic E2E identity; never use in production",
		}
		encoded, err := json.MarshalIndent(wrapper, "", "  ")
		if err != nil {
			return nil, false, err
		}
		if err := writePrivateLocalFile(path, encoded); err != nil {
			return nil, false, err
		}
		return key, true, nil
	}
	if err != nil {
		return nil, false, err
	}

	var wrapper keypairFile
	if err := json.Unmarshal(data, &wrapper); err == nil && strings.TrimSpace(wrapper.PrivateKey) != "" {
		keyBytes, err := base58.Decode(strings.TrimSpace(wrapper.PrivateKey))
		if err != nil {
			return nil, false, fmt.Errorf("decode base58 private_key: %w", err)
		}
		return validatePrivateKey(solana.PrivateKey(keyBytes), wrapper.PublicKey, wrapper.Address)
	}

	// Also accept the standard Solana CLI [u8,...] format for an explicitly
	// supplied test keypair, without ever writing that format by default.
	var numbers []byte
	if err := json.Unmarshal(data, &numbers); err != nil {
		return nil, false, errors.New("keypair must be a StablePay JSON wrapper or Solana CLI numeric array")
	}
	return validatePrivateKey(solana.PrivateKey(numbers), "", "")
}

func validatePrivateKey(key solana.PrivateKey, expectedPublic, expectedAddress string) (solana.PrivateKey, bool, error) {
	if err := key.Validate(); err != nil {
		return nil, false, err
	}
	derived := key.PublicKey().String()
	if expectedPublic != "" && expectedPublic != derived {
		return nil, false, fmt.Errorf("public_key does not match private_key; expected %s got %s", expectedPublic, derived)
	}
	if expectedAddress != "" && expectedAddress != derived {
		return nil, false, fmt.Errorf("address does not match private_key; expected %s got %s", expectedAddress, derived)
	}
	return key, false, nil
}

func registerDID(ctx context.Context, cli didsvc.Client, publicKey string, userType did.UserType, name string) (string, error) {
	req := did.NewRegisterDIDRequest()
	req.Base = dcommon.NewBaseReq()
	req.UserType = userType
	req.PublicKey = publicKey
	req.WalletAddress = publicKey
	req.WalletId = "stablepay-e2e-" + strings.ToLower(name)
	req.WalletName = "StablePay local E2E " + name
	resp, err := cli.RegisterDID(ctx, req)
	if err != nil {
		return "", err
	}
	if err := checkBase(resp.GetBase()); err != nil {
		return "", err
	}
	if resp.GetDid() == "" {
		return "", errors.New("DID service returned an empty DID")
	}
	return string(resp.GetDid()), nil
}

func verifyRemoteSignature(ctx context.Context, cli didsvc.Client, didString, signature, message, timestamp, nonce string) error {
	req := did.NewVerifySignatureRequest()
	req.Base = dcommon.NewBaseReq()
	req.Did = dcommon.DID(didString)
	req.Message = message
	req.Signature = signature
	req.Timestamp = timestamp
	req.Nonce = &nonce
	resp, err := cli.VerifySignature(ctx, req)
	if err != nil {
		return err
	}
	if err := checkBase(resp.GetBase()); err != nil {
		return err
	}
	if !resp.GetValid() {
		return errors.New("signature invalid")
	}
	return nil
}

func tokenBalance(ctx context.Context, cli bchainsvc.Client, address string, currency bcommon.Currency) (int64, error) {
	req := bchain.NewGetBalanceRequest()
	req.Base = bcommon.NewBaseReq()
	req.WalletAddress = address
	req.Currency = currency
	resp, err := cli.GetBalance(ctx, req)
	if err != nil {
		return 0, err
	}
	if err := checkBase(resp.GetBase()); err != nil {
		return 0, err
	}
	return resp.GetBalanceMinor(), nil
}

func checkFeePayerSOL(ctx context.Context, rpcURL string, publicKey solana.PublicKey) error {
	client := rpc.New(rpcURL)
	result, err := client.GetBalance(ctx, publicKey, rpc.CommitmentConfirmed)
	if err != nil {
		return fmt.Errorf("Devnet funding prerequisite check for hot-wallet SOL failed: %w", err)
	}
	if result == nil || result.Value < 5000 {
		value := uint64(0)
		if result != nil {
			value = result.Value
		}
		return fmt.Errorf("Devnet funding prerequisite not met: hot wallet %s has %d lamports; it must hold SOL for the adapter fee-payer subsidy", publicKey, value)
	}
	return nil
}

func checkTokenAccount(ctx context.Context, rpcURL, walletAddress, currency string) error {
	wallet, err := solana.PublicKeyFromBase58(walletAddress)
	if err != nil {
		return fmt.Errorf("invalid Skill wallet address: %w", err)
	}
	var mintAddress string
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "USDC":
		mintAddress = "4zMMC9srt5Ri5X14GAgXhaHii3GnPAEERYPJgZJDncDU"
	case "USDT":
		mintAddress = "BQcdHdAQW1hczDbBi9hiegXAR7A18QhzhCoXFBtBj9QA"
	default:
		return fmt.Errorf("unsupported Devnet token account currency: %s", currency)
	}
	mint, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return fmt.Errorf("invalid Devnet mint: %w", err)
	}
	ata, _, err := solana.FindAssociatedTokenAddress(wallet, mint)
	if err != nil {
		return fmt.Errorf("derive Skill associated token account: %w", err)
	}
	if _, err := rpc.New(rpcURL).GetAccountInfo(ctx, ata); err != nil {
		if errors.Is(err, rpc.ErrNotFound) {
			return fmt.Errorf("Devnet funding prerequisite not met: Skill wallet %s has no %s associated token account at %s; create/fund that ATA before running the real E2E", walletAddress, currency, ata)
		}
		return fmt.Errorf("Devnet Skill associated token account check failed: %w", err)
	}
	return nil
}

type baseResponse interface {
	GetCode() int32
	GetMessage() string
}

func checkBase(base baseResponse) error {
	if base == nil || base.GetCode() == 0 {
		return nil
	}
	return fmt.Errorf("code=%d message=%s", base.GetCode(), base.GetMessage())
}

func parseAmountMinor(value string) (int64, error) {
	// The public Gateway normalizes stablecoin business amounts to two decimal
	// places before forwarding amount_minor to payment-service. The harness must
	// sign that exact downstream value; Solana token decimals are handled by the
	// blockchain adapter boundary.
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("amount is required")
	}
	if strings.HasPrefix(value, "-") {
		return 0, fmt.Errorf("invalid amount %q", value)
	}
	var whole, fraction string
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("invalid amount %q", value)
	}
	whole = parts[0]
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 2 {
		return 0, fmt.Errorf("amount %q has more than 2 decimal places", value)
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	n, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid amount %q", value)
	}
	frac, err := strconv.ParseInt(fractionOrZero(fraction), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q", value)
	}
	if n > (int64(^uint64(0)>>1)-frac)/100 {
		return 0, fmt.Errorf("amount %q overflows minor units", value)
	}
	minor := n*100 + frac
	if minor <= 0 {
		return 0, errors.New("amount must be greater than zero")
	}
	return minor, nil
}

func fractionOrZero(value string) string {
	if value == "" {
		return "0"
	}
	return value
}

func parseCurrency(value string) (bcommon.Currency, constants.Currency, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "USDC":
		return bcommon.Currency_USDC, constants.CurrencyUSDC, nil
	case "USDT":
		return bcommon.Currency_USDT, constants.CurrencyUSDT, nil
	default:
		return 0, 0, fmt.Errorf("unsupported currency %q; use USDC or USDT", value)
	}
}

func signBase58(key solana.PrivateKey, message string) (string, error) {
	signature, err := key.Sign([]byte(message))
	if err != nil {
		return "", err
	}
	return base58.Encode(signature[:]), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func buildGatewayCanonical(body []byte) string {
	return fmt.Sprintf("POST\n/api/v1/pay\n\n%s", sha256Hex(body))
}

func didForKey(key solana.PrivateKey) string {
	return "did:solana:" + key.PublicKey().String()
}

func publicKeyFromDID(value string) string {
	const prefix = "did:solana:"
	if !strings.HasPrefix(value, prefix) {
		return ""
	}
	publicKey := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	if _, err := solana.PublicKeyFromBase58(publicKey); err != nil {
		return ""
	}
	return publicKey
}

func containsSigner(signers solana.PublicKeySlice, expected solana.PublicKey) bool {
	for _, signer := range signers {
		if signer.Equals(expected) {
			return true
		}
	}
	return false
}

func stringPtr(value string) *string { return &value }

func samePath(left, right string) bool {
	leftAbs, _ := filepath.Abs(left)
	rightAbs, _ := filepath.Abs(right)
	return strings.EqualFold(filepath.Clean(leftAbs), filepath.Clean(rightAbs))
}

func writePrivateLocalFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return nil
}
