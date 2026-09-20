package mysql

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stablepay/commerce-runtime/internal/evidence"
	"github.com/stablepay/commerce-runtime/internal/llm"
)

func TestMySQLS6EvidenceAndModelTracePersistence(t *testing.T) {
	dsn := os.Getenv("COMMERCE_RUNTIME_MYSQL_DSN")
	if dsn == "" {
		t.Skip("COMMERCE_RUNTIME_MYSQL_DSN is required for S6 MySQL persistence integration")
	}
	db, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	now := time.Now().UTC().Truncate(time.Microsecond)
	suffix := now.Format("20060102150405.000000")
	record := evidence.NewRecord("mysql-s6-"+suffix, string(evidence.SourceProtocolDoc), "protocol://s6", "v1", evidence.HashString("protocol-s6"), "text/plain", "x402 exact binding", evidence.TrustProtocolDoc, now)
	if err := store.SaveEvidenceRecord(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	read, err := store.GetEvidenceRecord(context.Background(), record.EvidenceRef)
	if err != nil || read.ChunkHash != record.ChunkHash {
		t.Fatalf("read=%#v err=%v", read, err)
	}
	traceValue := &llm.ModelDecisionTrace{TraceID: "mysql-s6-trace-" + suffix, EpisodeID: "mysql-s6-episode-" + suffix, Provider: "fake", ModelRef: "fake-model", ContextHash: evidence.HashString("context"), EvidenceRefs: []string{record.EvidenceRef}, RequestStartedAt: now, ResponseReceivedAt: now, Status: llm.TraceSuccess}
	if err := store.SaveModelDecisionTrace(context.Background(), traceValue); err != nil {
		t.Fatal(err)
	}
	traceRead, err := store.GetModelDecisionTrace(context.Background(), traceValue.TraceID)
	if err != nil || traceRead.ContextHash != traceValue.ContextHash {
		t.Fatalf("trace=%#v err=%v", traceRead, err)
	}
}
