// Command e4-mysql-audit creates a uniquely named isolated E4 schema or runs
// read-only, per-episode SQL audits over benchmark-e4-chaos trials.jsonl.
package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

type trial struct {
	TrialID   string `json:"trial_id"`
	EpisodeID string `json:"episode_id"`
	Status    string `json:"status"`
	Window    string `json:"crash_window"`
	Persisted bool   `json:"persisted_target_observed"`
	Completed bool   `json:"task_completion_after_restart"`
}

type auditResult struct {
	TrialID                    string      `json:"trial_id"`
	EpisodeID                  string      `json:"episode_id"`
	CrashWindow                string      `json:"crash_window"`
	TrialStatus                string      `json:"trial_status"`
	DuplicatePaymentIntentRows []duplicate `json:"duplicate_payment_intent_rows"`
	PaymentIntentCount         int64       `json:"payment_intent_count"`
	DuplicateSettledRows       []duplicate `json:"duplicate_payment_settled_rows"`
	PaymentSettledCount        int64       `json:"payment_settled_count"`
	Budget                     budgetAudit `json:"budget"`
	State                      stateAudit  `json:"state"`
	Passed                     bool        `json:"passed"`
	Errors                     []string    `json:"errors,omitempty"`
}

type duplicate struct {
	Identity string `json:"identity"`
	Rows     int64  `json:"rows"`
}

type budgetAudit struct {
	ExpectedConsumed  int64 `json:"expected_consumed"`
	ActualConsumed    int64 `json:"actual_consumed"`
	ExpectedAvailable int64 `json:"expected_available"`
	ActualAvailable   int64 `json:"actual_available"`
	ExpectedSunkCost  int64 `json:"expected_sunk_cost"`
	ActualSunkCost    int64 `json:"actual_sunk_cost"`
	ExpectedReserved  int64 `json:"expected_reserved"`
	ActualReserved    int64 `json:"actual_reserved"`
	ExpectedSettled   int64 `json:"expected_settled"`
	ActualSettled     int64 `json:"actual_settled"`
	ExpectedRefunded  int64 `json:"expected_refunded"`
	ActualRefunded    int64 `json:"actual_refunded"`
	Consistent        bool  `json:"consistent"`
}

type stateAudit struct {
	Exists                bool   `json:"exists"`
	EpisodeState          string `json:"episode_state,omitempty"`
	ExecutionState        string `json:"execution_state,omitempty"`
	ExecutionErrorCode    string `json:"execution_error_code,omitempty"`
	ExecutionErrorMessage string `json:"execution_error_message,omitempty"`
	Terminal              bool   `json:"terminal"`
	Orphan                bool   `json:"orphan"`
	Stuck                 bool   `json:"stuck"`
}

type windowSummary struct {
	Trials              int `json:"trials"`
	Passed              int `json:"passed"`
	DuplicateIntentRows int `json:"duplicate_payment_intent_rows"`
	DuplicateSettleRows int `json:"duplicate_payment_settled_rows"`
	BudgetDrift         int `json:"budget_drift"`
	Orphaned            int `json:"orphaned"`
	Stuck               int `json:"stuck"`
}

type auditSummary struct {
	Status                      string                   `json:"status"`
	Trials                      int                      `json:"trials"`
	Passed                      int                      `json:"passed"`
	Failed                      int                      `json:"failed"`
	DuplicatePaymentIntentRows  int                      `json:"duplicate_payment_intent_rows"`
	DuplicateSettledRows        int                      `json:"duplicate_payment_settled_rows"`
	PaymentIntentCountMismatch  int                      `json:"payment_intent_count_mismatch"`
	PaymentSettledCountMismatch int                      `json:"payment_settled_count_mismatch"`
	BudgetDrift                 int                      `json:"budget_drift"`
	OrphanedEpisodes            int                      `json:"orphaned_episodes"`
	StuckEpisodes               int                      `json:"stuck_episodes"`
	Windows                     map[string]windowSummary `json:"windows"`
}

func main() {
	if len(os.Args) < 2 {
		fatal(errors.New("usage: e4-mysql-audit create-schema -name stablepay_e4_... | check-schema -name stablepay_e4_... | audit -trials trials.jsonl -out audit.jsonl"))
	}
	var err error
	switch os.Args[1] {
	case "create-schema":
		err = createSchema(os.Args[2:])
	case "check-schema":
		err = checkSchema(os.Args[2:])
	case "audit":
		err = auditTrials(os.Args[2:])
	default:
		err = fmt.Errorf("unknown subcommand %q", os.Args[1])
	}
	if err != nil {
		fatal(err)
	}
}

func checkSchema(args []string) error {
	flags := flag.NewFlagSet("check-schema", flag.ContinueOnError)
	name := flags.String("name", "", "expected isolated schema name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !regexp.MustCompile(`^stablepay_e4_[a-z0-9_]{1,48}$`).MatchString(*name) {
		return errors.New("schema name must match stablepay_e4_<lowercase letters, digits or underscores>")
	}
	dsn := strings.TrimSpace(os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"))
	if dsn == "" {
		return errors.New("COMMERCE_RUNTIME_MYSQL_DSN is required")
	}
	cfg, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("parse MySQL DSN: %w", err)
	}
	if cfg.DBName != *name {
		return fmt.Errorf("configured MySQL schema %q does not match expected isolated schema %q", cfg.DBName, *name)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to isolated MySQL schema %s: %w", *name, err)
	}
	var active string
	if err := db.QueryRowContext(ctx, `SELECT DATABASE()`).Scan(&active); err != nil {
		return err
	}
	if active != *name {
		return fmt.Errorf("connected database %q does not match expected isolated schema %q", active, *name)
	}
	fmt.Printf("isolated MySQL schema ready: %s\n", active)
	return nil
}

func createSchema(args []string) error {
	flags := flag.NewFlagSet("create-schema", flag.ContinueOnError)
	name := flags.String("name", "", "new isolated schema name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !regexp.MustCompile(`^stablepay_e4_[a-z0-9_]{1,48}$`).MatchString(*name) {
		return errors.New("schema name must match stablepay_e4_<lowercase letters, digits or underscores>")
	}
	dsn := strings.TrimSpace(os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"))
	if dsn == "" {
		return errors.New("COMMERCE_RUNTIME_MYSQL_DSN is required")
	}
	cfg, err := mysqlDriver.ParseDSN(dsn)
	if err != nil {
		return fmt.Errorf("parse MySQL DSN: %w", err)
	}
	cfg.DBName = ""
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to MySQL server: %w", err)
	}
	// The identifier is validated above and quoted; this never drops or alters
	// an existing database. Every invocation uses a fresh timestamped name.
	statement := "CREATE DATABASE `" + *name + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
	if _, err := db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("create isolated schema %s: %w", *name, err)
	}
	fmt.Println(*name)
	return nil
}

func auditTrials(args []string) error {
	flags := flag.NewFlagSet("audit", flag.ContinueOnError)
	trialsPath := flags.String("trials", "", "path to trials.jsonl")
	outPath := flags.String("out", "", "path to write one audit row per episode")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *trialsPath == "" || *outPath == "" {
		return errors.New("-trials and -out are required")
	}
	dsn := strings.TrimSpace(os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN"))
	if dsn == "" {
		return errors.New("COMMERCE_RUNTIME_MYSQL_DSN is required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to isolated E4 schema: %w", err)
	}

	input, err := os.Open(*trialsPath)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.Create(*outPath)
	if err != nil {
		return err
	}
	defer output.Close()
	writer := bufio.NewWriter(output)
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	seen := make(map[string]struct{})
	results := make([]auditResult, 0)
	lineNumber := 0
	failures := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimPrefix(scanner.Text(), "\ufeff")
		if strings.TrimSpace(line) == "" {
			continue
		}
		var t trial
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			return fmt.Errorf("parse trials.jsonl line %d: %w", lineNumber, err)
		}
		if strings.TrimSpace(t.EpisodeID) == "" {
			result := auditResult{TrialID: t.TrialID, CrashWindow: t.Window, TrialStatus: t.Status, Passed: false, State: stateAudit{Orphan: true, Stuck: true}, Errors: []string{"episode_id missing from trial; no SQL audit can be attached"}}
			if err := writeJSONLine(writer, result); err != nil {
				return err
			}
			results = append(results, result)
			failures++
			continue
		}
		if _, exists := seen[t.EpisodeID]; exists {
			return fmt.Errorf("duplicate episode_id in trials.jsonl: %s", t.EpisodeID)
		}
		seen[t.EpisodeID] = struct{}{}
		result, err := auditEpisode(ctx, db, t)
		if err != nil {
			return fmt.Errorf("audit episode %s: %w", t.EpisodeID, err)
		}
		if !result.Passed {
			failures++
		}
		if err := writeJSONLine(writer, result); err != nil {
			return err
		}
		results = append(results, result)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if err := writer.Flush(); err != nil {
		return err
	}
	summary := summarize(results)
	if err := writeSummaryFiles(filepath.Dir(*outPath), summary); err != nil {
		return err
	}
	fmt.Printf("audited=%d failed=%d output=%s\n", len(seen), failures, *outPath)
	if len(seen) == 0 {
		return errors.New("trials.jsonl contained no episode_id values")
	}
	if failures > 0 {
		return fmt.Errorf("MySQL audit failed for %d trial(s)", failures)
	}
	return nil
}

func summarize(results []auditResult) auditSummary {
	result := auditSummary{Status: "PASS", Trials: len(results), Windows: make(map[string]windowSummary)}
	for _, row := range results {
		window := result.Windows[row.CrashWindow]
		window.Trials++
		if row.Passed {
			result.Passed++
			window.Passed++
		} else {
			result.Failed++
			result.Status = "FAIL"
		}
		window.DuplicateIntentRows += len(row.DuplicatePaymentIntentRows)
		window.DuplicateSettleRows += len(row.DuplicateSettledRows)
		result.DuplicatePaymentIntentRows += len(row.DuplicatePaymentIntentRows)
		result.DuplicateSettledRows += len(row.DuplicateSettledRows)
		if row.PaymentIntentCount != 1 {
			result.PaymentIntentCountMismatch++
		}
		if row.PaymentSettledCount != 1 {
			result.PaymentSettledCountMismatch++
		}
		if !row.Budget.Consistent {
			result.BudgetDrift++
			window.BudgetDrift++
		}
		if row.State.Orphan {
			result.OrphanedEpisodes++
			window.Orphaned++
		}
		if row.State.Stuck {
			result.StuckEpisodes++
			window.Stuck++
		}
		result.Windows[row.CrashWindow] = window
	}
	return result
}

func writeSummaryFiles(dir string, summary auditSummary) error {
	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "audit-summary.json"), append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	windows := make([]string, 0, len(summary.Windows))
	for window := range summary.Windows {
		windows = append(windows, window)
	}
	sort.Strings(windows)
	var report strings.Builder
	fmt.Fprintf(&report, "# E4 MySQL Audit\n\nStatus: **%s**\n\nPer-episode results: `audit.jsonl` (%d trials)\n\n", summary.Status, summary.Trials)
	report.WriteString("| Crash window | Trials | Passed | Duplicate PaymentIntent rows | Duplicate PAYMENT_SETTLED rows | Budget drift | Orphaned | Stuck |\n|---|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, name := range windows {
		value := summary.Windows[name]
		fmt.Fprintf(&report, "| %s | %d | %d | %d | %d | %d | %d | %d |\n", name, value.Trials, value.Passed, value.DuplicateIntentRows, value.DuplicateSettleRows, value.BudgetDrift, value.Orphaned, value.Stuck)
	}
	fmt.Fprintf(&report, "\nTotals: %d/%d passed; duplicate PaymentIntent rows %d; duplicate `PAYMENT_SETTLED` rows %d; intent-count mismatches %d; settlement-count mismatches %d; budget drift %d; orphaned %d; stuck %d.\n\n", summary.Passed, summary.Trials, summary.DuplicatePaymentIntentRows, summary.DuplicateSettledRows, summary.PaymentIntentCountMismatch, summary.PaymentSettledCountMismatch, summary.BudgetDrift, summary.OrphanedEpisodes, summary.StuckEpisodes)
	report.WriteString("Each trial was read from `trials.jsonl` by its `episode_id`. The audit ran duplicate-intent and duplicate-settlement GROUP BY/HAVING queries per episode, recomputed reserved/settled/refunded ledger totals, derived `expected_consumed`, `expected_available`, and `expected_sunk_cost`, and compared them with `commerce_episodes`. It also joined `episode_execution_status` to confirm terminal completion.\n")
	return os.WriteFile(filepath.Join(dir, "audit-report.md"), []byte(report.String()), 0o600)
}

func auditEpisode(ctx context.Context, db *sql.DB, t trial) (auditResult, error) {
	r := auditResult{TrialID: t.TrialID, EpisodeID: t.EpisodeID, CrashWindow: t.Window, TrialStatus: t.Status}
	// A resumed E4 task is expected to create exactly one durable PaymentIntent.
	if err := countRows(ctx, db, `SELECT COUNT(*) FROM payment_intents WHERE episode_id = ?`, t.EpisodeID, &r.PaymentIntentCount); err != nil {
		return r, fmt.Errorf("count PaymentIntents: %w", err)
	}
	var err error
	r.DuplicatePaymentIntentRows, err = queryDuplicateRows(ctx, db,
		`SELECT episode_id, COUNT(*) AS row_count FROM payment_intents WHERE episode_id = ? GROUP BY episode_id HAVING COUNT(*) > 1`, t.EpisodeID)
	if err != nil {
		return r, fmt.Errorf("duplicate PaymentIntent query: %w", err)
	}
	r.DuplicateSettledRows, err = queryDuplicateRows(ctx, db,
		`SELECT payment_intent_id, COUNT(*) AS row_count FROM ledger_entries WHERE episode_id = ? AND type = 'PAYMENT_SETTLED' GROUP BY payment_intent_id HAVING COUNT(*) > 1`, t.EpisodeID)
	if err != nil {
		return r, fmt.Errorf("duplicate PAYMENT_SETTLED query: %w", err)
	}
	if err := countRows(ctx, db, `SELECT COUNT(*) FROM ledger_entries WHERE episode_id = ? AND type = 'PAYMENT_SETTLED'`, t.EpisodeID, &r.PaymentSettledCount); err != nil {
		return r, fmt.Errorf("count PAYMENT_SETTLED rows: %w", err)
	}
	if err := auditBudget(ctx, db, t.EpisodeID, &r.Budget); err != nil {
		return r, err
	}
	if err := auditState(ctx, db, t.EpisodeID, &r.State); err != nil {
		return r, err
	}
	if !t.Persisted {
		r.Errors = append(r.Errors, "persisted target state was not observed before kill")
	}
	if !t.Completed {
		r.Errors = append(r.Errors, "episode did not reach a terminal state after restart")
	}
	if r.PaymentIntentCount != 1 {
		r.Errors = append(r.Errors, fmt.Sprintf("expected exactly one PaymentIntent, got %d", r.PaymentIntentCount))
	}
	if len(r.DuplicatePaymentIntentRows) != 0 {
		r.Errors = append(r.Errors, "duplicate PaymentIntent query returned rows")
	}
	if r.PaymentSettledCount != 1 {
		r.Errors = append(r.Errors, fmt.Sprintf("expected exactly one PAYMENT_SETTLED row, got %d", r.PaymentSettledCount))
	}
	if len(r.DuplicateSettledRows) != 0 {
		r.Errors = append(r.Errors, "duplicate PAYMENT_SETTLED query returned rows")
	}
	if !r.Budget.Consistent {
		r.Errors = append(r.Errors, "ledger-derived consumed/available/sunk-cost projection does not match commerce_episodes")
	}
	if r.State.Orphan {
		r.Errors = append(r.Errors, "episode_id is orphaned: commerce_episodes row missing")
	}
	if r.State.Stuck {
		r.Errors = append(r.Errors, "episode is non-terminal or execution status is not COMPLETED")
	}
	if r.State.EpisodeState != "FULFILLED" {
		r.Errors = append(r.Errors, "episode did not finish FULFILLED")
	}
	r.Passed = len(r.Errors) == 0
	return r, nil
}

func queryDuplicateRows(ctx context.Context, db *sql.DB, query, episodeID string) ([]duplicate, error) {
	rows, err := db.QueryContext(ctx, query, episodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]duplicate, 0)
	for rows.Next() {
		var identity string
		var count int64
		if err := rows.Scan(&identity, &count); err != nil {
			return nil, err
		}
		result = append(result, duplicate{Identity: identity, Rows: count})
	}
	return result, rows.Err()
}

func countRows(ctx context.Context, db *sql.DB, query, episodeID string, value *int64) error {
	return db.QueryRowContext(ctx, query, episodeID).Scan(value)
}

func auditBudget(ctx context.Context, db *sql.DB, episodeID string, result *budgetAudit) error {
	var limit, actualReserved, actualSettled, actualRefunded int64
	var reusable bool
	var actualConsumed, actualAvailable, actualSunk int64
	err := db.QueryRowContext(ctx, `SELECT budget_limit_minor, refund_reusable, reserved_amount, settled_amount, refunded_amount, consumed_amount, available_budget, sunk_cost FROM commerce_episodes WHERE episode_id = ?`, episodeID).Scan(
		&limit, &reusable, &actualReserved, &actualSettled, &actualRefunded, &actualConsumed, &actualAvailable, &actualSunk)
	if errors.Is(err, sql.ErrNoRows) {
		result.Consistent = false
		return nil
	}
	if err != nil {
		return fmt.Errorf("read episode budget projection: %w", err)
	}
	var reservedEntries, releasedEntries, settledEntries, refundedEntries int64
	if err := db.QueryRowContext(ctx, `SELECT
		COALESCE(SUM(CASE WHEN type = 'BUDGET_RESERVED' THEN amount_minor ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN type = 'BUDGET_RELEASED' THEN amount_minor ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN type = 'PAYMENT_SETTLED' THEN amount_minor ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN type = 'REFUND_CONFIRMED' THEN amount_minor ELSE 0 END), 0)
		FROM ledger_entries WHERE episode_id = ?`, episodeID).Scan(&reservedEntries, &releasedEntries, &settledEntries, &refundedEntries); err != nil {
		return fmt.Errorf("sum ledger budget facts: %w", err)
	}
	result.ExpectedReserved = reservedEntries - releasedEntries
	result.ExpectedSettled = settledEntries
	result.ExpectedRefunded = refundedEntries
	result.ExpectedConsumed = max64(0, settledEntries-refundedEntries)
	if !reusable {
		result.ExpectedConsumed = settledEntries
	}
	result.ExpectedAvailable = limit - result.ExpectedConsumed - result.ExpectedReserved
	result.ExpectedSunkCost = max64(0, settledEntries-refundedEntries)
	result.ActualReserved = actualReserved
	result.ActualSettled = actualSettled
	result.ActualRefunded = actualRefunded
	result.ActualConsumed = actualConsumed
	result.ActualAvailable = actualAvailable
	result.ActualSunkCost = actualSunk
	result.Consistent = result.ExpectedReserved == actualReserved && result.ExpectedSettled == actualSettled && result.ExpectedRefunded == actualRefunded && result.ExpectedConsumed == actualConsumed && result.ExpectedAvailable == actualAvailable && result.ExpectedSunkCost == actualSunk
	return nil
}

func auditState(ctx context.Context, db *sql.DB, episodeID string, result *stateAudit) error {
	var execution, errorCode, errorMessage sql.NullString
	err := db.QueryRowContext(ctx, `SELECT e.state, x.status, x.last_error_code, x.last_error_message FROM commerce_episodes AS e LEFT JOIN episode_execution_status AS x ON x.episode_id = e.episode_id WHERE e.episode_id = ?`, episodeID).Scan(&result.EpisodeState, &execution, &errorCode, &errorMessage)
	if errors.Is(err, sql.ErrNoRows) {
		result.Orphan = true
		result.Stuck = true
		return nil
	}
	if err != nil {
		return fmt.Errorf("read episode and execution terminal state: %w", err)
	}
	result.Exists = true
	if execution.Valid {
		result.ExecutionState = execution.String
	}
	if errorCode.Valid {
		result.ExecutionErrorCode = errorCode.String
	}
	if errorMessage.Valid {
		result.ExecutionErrorMessage = errorMessage.String
	}
	switch result.EpisodeState {
	case "FULFILLED", "FAILED", "BLOCKED", "ABORTED", "EXPIRED", "DISPUTED":
		result.Terminal = true
	}
	result.Stuck = !result.Terminal || result.ExecutionState != "COMPLETED"
	return nil
}

func writeJSONLine(writer *bufio.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = writer.Write(append(encoded, '\n'))
	return err
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
