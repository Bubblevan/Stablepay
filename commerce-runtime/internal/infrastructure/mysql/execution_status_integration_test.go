package mysql

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/repository"
)

func TestMySQLEpisodeExecutionStatusRoundTrip(t *testing.T) {
	dsn := os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COMMERCE_RUNTIME_MYSQL_DSN is not set")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx := context.Background()
	if err := AutoMigrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	episodeID := "execution-status-integration-" + time.Now().UTC().Format("20060102150405.000000000")
	// MySQL DATETIME(6) persists microseconds; canonicalize the fixture before
	// comparing the round-tripped value so driver precision does not leak into
	// the integration assertion.
	now := time.Now().UTC().Truncate(time.Microsecond)
	next := now.Add(time.Second)
	value := &repository.EpisodeExecutionStatus{EpisodeID: episodeID, Status: repository.ExecutionRetryWait, AttemptCount: 4, LastStartedAt: now, LastFinishedAt: &now, LastErrorCode: "DEPENDENCY_UNAVAILABLE", LastErrorMessage: "bounded transient error", LastErrorAt: &now, NextRetryAt: &next, UpdatedAt: now}
	if err := store.UpsertEpisodeExecutionStatus(ctx, value); err != nil {
		t.Fatal(err)
	}
	read, err := store.GetEpisodeExecutionStatus(ctx, episodeID)
	if err != nil {
		t.Fatal(err)
	}
	if read.Status != value.Status || read.AttemptCount != value.AttemptCount || read.NextRetryAt == nil || !read.NextRetryAt.Equal(next) {
		t.Fatalf("execution status round trip mismatch: got=%#v want=%#v", read, value)
	}
	if err := db.Exec("DELETE FROM episode_execution_status WHERE episode_id = ?", episodeID).Error; err != nil {
		t.Fatal(err)
	}
}
