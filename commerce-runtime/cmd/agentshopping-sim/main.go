package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/stablepay/commerce-runtime/benchmarks/agentshopping"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "seed":
		err = seed(os.Args[2:])
	case "evaluate":
		err = evaluate(os.Args[2:])
	case "evaluate-intents":
		err = evaluateIntents(os.Args[2:])
	case "evaluate-challenge":
		err = evaluateChallenge(os.Args[2:])
	case "generate-challenge-draft":
		err = generateChallengeDraft(os.Args[2:])
	case "prepare-challenge-review":
		err = prepareChallengeReview(os.Args[2:])
	case "finalize-challenge-review":
		err = finalizeChallengeReview(os.Args[2:])
	case "serve":
		err = serve(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return
	default:
		usage()
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		// Do not dump environment, DSN, or provider credentials on failure.
		slog.Error("shopping simulation command failed", "error", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  agentshopping-sim.exe seed [--manifest path] [--seed 20261010] [--buyers 2000] [--tasks 500]
  agentshopping-sim.exe evaluate [--manifest path] [--seed 20261010] [--tasks 500] [--cutoff 20]
  agentshopping-sim.exe evaluate-intents [--manifest path] [--seed 20261010]
  agentshopping-sim.exe evaluate-challenge --tasks <explicit-suite.jsonl> [--manifest path] [--seed 20261010] [--cutoff 20] [--oracle-intents | --replay-parse-results <prior-run/parse-results.jsonl> | --require-human-review --review-bundle <bundle-dir> | --allow-unreviewed-diagnostic-model-run]
  agentshopping-sim.exe generate-challenge-draft [--manifest path] [--seed 20261010] [--task-seed 20261010] [--tasks 500] [--mixed-decisions] [--natural-language-v2] [--out-dir .local-run/agent-shopping/challenge-suites/draft-auto-v1]
  agentshopping-sim.exe prepare-challenge-review --prompts <master-prompts.jsonl> --manifest .local-run/simulated-merchants/manifest.json --out-dir <new-packet-dir>
  agentshopping-sim finalize-challenge-review --prompts <master-prompts.jsonl> --reviewer-a-prompts <reviewer-a-prompts.jsonl> --reviewer-b-prompts <reviewer-b-prompts.jsonl> --reviewer-catalog <reviewer-catalog.jsonl> --packet-manifest <packet-manifest.json> --reviewer-a <reviewer-a.jsonl> --reviewer-b <reviewer-b.jsonl> --adjudications <adjudications.jsonl> --out-dir <new-bundle-dir>
  agentshopping-sim.exe serve

Environment (loaded explicitly by the local runner; no StablePay .env is read):
  SHOP_SIM_DB_PASSWORD  Password for 127.0.0.1:13327/stablepay_shopping_sim only
  SHOP_SIM_API_KEY      Local simulation HTTP bearer secret (never written to evidence)
  SHOP_SIM_BUYER_APPROVAL_KEY Independent local credential used only by the human approval script
  SHOP_SIM_HTTP_ADDR    Optional loopback bind address; defaults to 127.0.0.1:18090
  SHOP_SIM_MERCHANT_MANIFEST Optional manifest; loads products from its loopback Merchant HTTP APIs
  SHOP_SIM_HERMES_BIN   Optional Hermes executable path; defaults to hermes`)
}

func openStore(ctx context.Context) (*agentshopping.MySQLStore, error) {
	store, err := agentshopping.OpenLocalMySQLStore(os.Getenv("SHOP_SIM_DB_PASSWORD"))
	if err != nil {
		return nil, err
	}
	if err := store.Ping(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("isolated simulation MySQL is not ready: %w", err)
	}
	return store, nil
}

func seed(args []string) error {
	flags := flag.NewFlagSet("seed", flag.ContinueOnError)
	manifest := flags.String("manifest", filepath.Join(".local-run", "simulated-merchants", "manifest.json"), "local synthetic Merchant manifest")
	seedValue := flags.Int64("seed", 20261010, "deterministic synthetic data seed")
	buyers := flags.Int("buyers", 2000, "number of synthetic buyers")
	tasks := flags.Int("tasks", 500, "locked shopping evaluation task count")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	manifestAbs, err := filepath.Abs(*manifest)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	store, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate isolated simulation schema: %w", err)
	}
	built, err := agentshopping.BuildSimulationSeed(ctx, manifestAbs, *seedValue, *buyers, *tasks)
	if err != nil {
		return err
	}
	manifestBytes, err := os.ReadFile(manifestAbs)
	if err != nil {
		return fmt.Errorf("read source manifest for evidence hash: %w", err)
	}
	manifestHash := sha256.Sum256(manifestBytes)
	existing, err := store.GetDatasetRun(ctx, built.DatasetID)
	if errors.Is(err, agentshopping.ErrNotFound) {
		if err := store.SeedDataset(ctx, built.Dataset); err != nil {
			return fmt.Errorf("seed isolated dataset: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("read dataset run: %w", err)
	} else if existing.Seed != *seedValue || existing.ProductCount != 24000 || existing.TaskCount != 500 {
		return errors.New("existing dataset metadata differs from the requested fixture; refusing to overwrite")
	}
	history, err := store.SeedHistoricalFlows(ctx, built)
	if err != nil {
		return fmt.Errorf("seed idempotent historical lifecycle examples: %w", err)
	}
	if err := store.UpdateDatasetRunSummary(ctx, built.DatasetID); err != nil {
		return fmt.Errorf("update dataset summary: %w", err)
	}
	counts, err := store.Counts(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"database": agentshopping.SimulationDatabaseName, "tenant_id": agentshopping.SimulationTenantID,
		"dataset_id": built.DatasetID, "seed": *seedValue, "manifest_sha256": hex.EncodeToString(manifestHash[:]),
		"merchants": len(built.Catalog.Merchants), "products": len(built.Catalog.Products),
		"inventory_rows": len(built.Catalog.Inventory), "buyers": len(built.Buyers), "tasks": len(built.Tasks),
		"history": history, "database_counts": counts, "data_origin": "SYNTHETIC_SIMULATION",
		"payment_provider": "benchmark-mock", "real_settlement": false,
	})
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	addr := strings.TrimSpace(os.Getenv("SHOP_SIM_HTTP_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:18090"
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("SHOP_SIM_HTTP_ADDR must bind to a loopback IP address")
	}
	apiKey := os.Getenv("SHOP_SIM_API_KEY")
	if len(apiKey) < 32 {
		return errors.New("SHOP_SIM_API_KEY must contain at least 32 characters")
	}
	buyerApprovalKey := os.Getenv("SHOP_SIM_BUYER_APPROVAL_KEY")
	if len(buyerApprovalKey) < 32 {
		return errors.New("SHOP_SIM_BUYER_APPROVAL_KEY must contain at least 32 characters")
	}
	if err := os.Unsetenv("SHOP_SIM_BUYER_APPROVAL_KEY"); err != nil {
		return errors.New("could not remove the buyer-approval key from the child-process environment")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate isolated simulation schema: %w", err)
	}
	hermesBinary := strings.TrimSpace(os.Getenv("SHOP_SIM_HERMES_BIN"))
	parser := agentshopping.NewHermesIntentParser(hermesBinary, "openai-codex", "gpt-6-luna")
	var api *agentshopping.HTTPAPI
	merchantManifest := strings.TrimSpace(os.Getenv("SHOP_SIM_MERCHANT_MANIFEST"))
	if merchantManifest != "" {
		catalogCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		catalog, loadErr := agentshopping.LoadMerchantCatalog(catalogCtx, merchantManifest, nil)
		if loadErr == nil {
			loadErr = store.ValidateMerchantCatalogSnapshot(catalogCtx, catalog)
		}
		cancel()
		if loadErr != nil {
			return fmt.Errorf("load or validate Merchant HTTP catalog snapshot: %w", loadErr)
		}
		api, err = agentshopping.NewHTTPAPIWithMerchantCatalog(store, parser, apiKey, catalog, buyerApprovalKey)
	} else {
		api, err = agentshopping.NewHTTPAPI(store, parser, apiKey, buyerApprovalKey)
	}
	if err != nil {
		return err
	}
	server := &http.Server{Addr: addr, Handler: api, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 3 * time.Minute, IdleTimeout: 60 * time.Second}
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- server.ListenAndServe() }()
	slog.Info("isolated shopping simulation ready", "addr", addr, "database", agentshopping.SimulationDatabaseName,
		"merchant_http_catalog", merchantManifest != "",
		"provider", parser.Provider(), "model", parser.Model(), "simulation_only", true)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errorsCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
