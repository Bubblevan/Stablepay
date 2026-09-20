package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrInvalidMemoryUseTrace = errors.New("invalid memory use trace")

type MemoryUseTrace struct {
	MemoryUseTraceID     string    `json:"memory_use_trace_id"`
	EpisodeID            string    `json:"episode_id"`
	ModelDecisionTraceID string    `json:"model_decision_trace_id"`
	ContextHash          string    `json:"context_hash"`
	RetrievedMemoryRefs  []string  `json:"retrieved_memory_refs,omitempty"`
	CitedMemoryRefs      []string  `json:"cited_memory_refs,omitempty"`
	ProposedAction       string    `json:"proposed_action"`
	GuardAccepted        bool      `json:"guard_accepted"`
	CreatedAt            time.Time `json:"created_at"`
	FactsRef             string    `json:"facts_ref"`
	PayloadHash          string    `json:"payload_hash"`
}

type MemoryUseTraceStore interface {
	SaveMemoryUseTrace(context.Context, *MemoryUseTrace) error
	GetMemoryUseTrace(context.Context, string) (*MemoryUseTrace, error)
	ListMemoryUseTraces(context.Context, string) ([]*MemoryUseTrace, error)
}

func (t MemoryUseTrace) CanonicalSnapshot() ([]byte, error) {
	copy := t
	copy.PayloadHash = ""
	copy.RetrievedMemoryRefs = sortedUnique(copy.RetrievedMemoryRefs)
	copy.CitedMemoryRefs = sortedUnique(copy.CitedMemoryRefs)
	return json.Marshal(copy)
}

func (t MemoryUseTrace) PayloadHashFor() (string, error) {
	snapshot, err := t.CanonicalSnapshot()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(snapshot)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (t *MemoryUseTrace) RefreshPayloadHash() error {
	if t == nil {
		return ErrInvalidMemoryUseTrace
	}
	hash, err := t.PayloadHashFor()
	if err != nil {
		return err
	}
	t.PayloadHash = hash
	return nil
}

func (t MemoryUseTrace) Validate() error {
	if strings.TrimSpace(t.MemoryUseTraceID) == "" || strings.TrimSpace(t.EpisodeID) == "" || strings.TrimSpace(t.ModelDecisionTraceID) == "" || strings.TrimSpace(t.ContextHash) == "" || strings.TrimSpace(t.ProposedAction) == "" || t.CreatedAt.IsZero() || t.FactsRef != "memory-use-trace://"+t.MemoryUseTraceID || strings.TrimSpace(t.PayloadHash) == "" {
		return ErrInvalidMemoryUseTrace
	}
	if expected, err := t.PayloadHashFor(); err != nil || expected != t.PayloadHash {
		return ErrInvalidMemoryUseTrace
	}
	return nil
}

func (t MemoryUseTrace) Clone() *MemoryUseTrace {
	t.RetrievedMemoryRefs = append([]string(nil), t.RetrievedMemoryRefs...)
	t.CitedMemoryRefs = append([]string(nil), t.CitedMemoryRefs...)
	return &t
}
