// Package evidence contains bounded, integrity-checked evidence facts used by
// the S6 prompt boundary. Evidence is data for explanation; it is never an
// authority for payment, budget, payee, or tool execution.
package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	MaxEvidenceContentBytes = 32 << 10
	MaxEvidenceRecords      = 64
)

type TrustClass string

const (
	TrustRuntimeFact   TrustClass = "RUNTIME_FACT"
	TrustCatalogFact   TrustClass = "CATALOG_FACT"
	TrustProtocolDoc   TrustClass = "PROTOCOL_DOC"
	TrustMerchantDoc   TrustClass = "MERCHANT_DOC"
	TrustRetrievedText TrustClass = "RETRIEVED_TEXT"
)

type SourceType string

const (
	SourceRuntimeFact           SourceType = "RUNTIME_FACT"
	SourceCatalogFact           SourceType = "CATALOG_FACT"
	SourceMerchantCapabilityDoc SourceType = "MERCHANT_CAPABILITY_DOCUMENT"
	SourceProtocolDoc           SourceType = "X402_PROTOCOL_DOCUMENT"
	SourceMerchantConstraint    SourceType = "MERCHANT_CONSTRAINT"
)

var (
	ErrInvalidRecord      = errors.New("invalid evidence record")
	ErrEvidenceConflict   = errors.New("evidence record conflicts with an existing identity")
	ErrEvidenceNotFound   = errors.New("evidence record not found")
	ErrInvalidEvidenceRef = errors.New("invalid evidence reference")
)

var evidenceRefPattern = regexp.MustCompile(`^evidence://[A-Za-z0-9][A-Za-z0-9._:/-]{0,255}$`)
var digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// EvidenceRecord is an immutable, bounded evidence fact. Content is retained
// only for the narrow S6 retrieval scopes and is never treated as trusted
// runtime state.
type EvidenceRecord struct {
	EvidenceID    string     `json:"evidence_id"`
	EvidenceRef   string     `json:"evidence_ref"`
	SourceType    SourceType `json:"source_type"`
	SourceRef     string     `json:"source_ref"`
	SourceVersion string     `json:"source_version"`
	SourceHash    string     `json:"source_hash"`
	ContentType   string     `json:"content_type"`
	PayloadHash   string     `json:"payload_hash"`
	ChunkHash     string     `json:"chunk_hash"`
	TrustClass    TrustClass `json:"trust_class"`
	Content       string     `json:"content,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	ValidUntil    *time.Time `json:"valid_until,omitempty"`
}

func HashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func HashString(value string) string { return HashBytes([]byte(value)) }

func NewRecord(evidenceID, sourceType, sourceRef, sourceVersion, sourceHash, contentType, content string, trust TrustClass, createdAt time.Time) *EvidenceRecord {
	record := &EvidenceRecord{
		EvidenceID: evidenceID, EvidenceRef: "evidence://" + strings.TrimSpace(evidenceID),
		SourceType: SourceType(sourceType), SourceRef: sourceRef, SourceVersion: sourceVersion,
		SourceHash: sourceHash, ContentType: contentType, TrustClass: trust, Content: content,
		CreatedAt: createdAt.UTC().Truncate(time.Nanosecond),
	}
	record.PayloadHash = HashString(content)
	record.ChunkHash = HashString(record.SourceHash + "\x00" + record.SourceVersion + "\x00" + record.PayloadHash)
	return record
}

func (r EvidenceRecord) Normalize() EvidenceRecord {
	r.EvidenceID = strings.TrimSpace(r.EvidenceID)
	r.EvidenceRef = strings.TrimSpace(r.EvidenceRef)
	r.SourceType = SourceType(strings.TrimSpace(string(r.SourceType)))
	r.SourceRef = strings.TrimSpace(r.SourceRef)
	r.SourceVersion = strings.TrimSpace(r.SourceVersion)
	r.SourceHash = strings.ToLower(strings.TrimSpace(r.SourceHash))
	r.ContentType = strings.ToLower(strings.TrimSpace(r.ContentType))
	r.PayloadHash = strings.ToLower(strings.TrimSpace(r.PayloadHash))
	r.ChunkHash = strings.ToLower(strings.TrimSpace(r.ChunkHash))
	r.TrustClass = TrustClass(strings.TrimSpace(string(r.TrustClass)))
	r.CreatedAt = r.CreatedAt.UTC().Truncate(time.Nanosecond)
	if r.ValidUntil != nil {
		value := r.ValidUntil.UTC().Truncate(time.Nanosecond)
		r.ValidUntil = &value
	}
	return r
}

func (r EvidenceRecord) Validate() error {
	r = r.Normalize()
	if r.EvidenceID == "" || !evidenceRefPattern.MatchString(r.EvidenceRef) || r.EvidenceRef != "evidence://"+r.EvidenceID || r.SourceType == "" || r.SourceRef == "" || r.SourceVersion == "" || r.ContentType == "" || r.SourceHash == "" || r.PayloadHash == "" || r.ChunkHash == "" || r.CreatedAt.IsZero() || len([]byte(r.Content)) > MaxEvidenceContentBytes {
		return ErrInvalidRecord
	}
	if !digestPattern.MatchString(r.SourceHash) || !digestPattern.MatchString(r.PayloadHash) || !digestPattern.MatchString(r.ChunkHash) {
		return ErrInvalidRecord
	}
	if !validSourceType(r.SourceType) || !validTrustClass(r.TrustClass) {
		return ErrInvalidRecord
	}
	if r.ValidUntil != nil && !r.ValidUntil.After(r.CreatedAt) {
		return ErrInvalidRecord
	}
	if expected := HashString(r.Content); expected != r.PayloadHash {
		return ErrInvalidRecord
	}
	if expected := HashString(r.SourceHash + "\x00" + r.SourceVersion + "\x00" + r.PayloadHash); expected != r.ChunkHash {
		return ErrInvalidRecord
	}
	return nil
}

func validSourceType(value SourceType) bool {
	switch value {
	case SourceRuntimeFact, SourceCatalogFact, SourceMerchantCapabilityDoc, SourceProtocolDoc, SourceMerchantConstraint:
		return true
	default:
		return false
	}
}

func validTrustClass(value TrustClass) bool {
	switch value {
	case TrustRuntimeFact, TrustCatalogFact, TrustProtocolDoc, TrustMerchantDoc, TrustRetrievedText:
		return true
	default:
		return false
	}
}

func (r EvidenceRecord) Clone() *EvidenceRecord {
	r = r.Normalize()
	return &r
}

func (r EvidenceRecord) CanonicalSnapshot() ([]byte, error) {
	r = r.Normalize()
	return json.Marshal(struct {
		EvidenceID    string     `json:"evidence_id"`
		EvidenceRef   string     `json:"evidence_ref"`
		SourceType    SourceType `json:"source_type"`
		SourceRef     string     `json:"source_ref"`
		SourceVersion string     `json:"source_version"`
		SourceHash    string     `json:"source_hash"`
		ContentType   string     `json:"content_type"`
		PayloadHash   string     `json:"payload_hash"`
		ChunkHash     string     `json:"chunk_hash"`
		TrustClass    TrustClass `json:"trust_class"`
		Content       string     `json:"content"`
		CreatedAt     time.Time  `json:"created_at"`
		ValidUntil    *time.Time `json:"valid_until,omitempty"`
	}{r.EvidenceID, r.EvidenceRef, r.SourceType, r.SourceRef, r.SourceVersion, r.SourceHash, r.ContentType, r.PayloadHash, r.ChunkHash, r.TrustClass, r.Content, r.CreatedAt, r.ValidUntil})
}

type Registry interface {
	Save(ctx context.Context, record *EvidenceRecord) error
	Get(ctx context.Context, evidenceRef string) (*EvidenceRecord, error)
	List(ctx context.Context) ([]*EvidenceRecord, error)
}

// PersistentStore is the small adapter seam implemented by the runtime
// repositories. Keeping this interface in evidence avoids coupling the
// retriever to MySQL or the application package.
type PersistentStore interface {
	SaveEvidenceRecord(context.Context, *EvidenceRecord) error
	GetEvidenceRecord(context.Context, string) (*EvidenceRecord, error)
	ListEvidenceRecords(context.Context) ([]*EvidenceRecord, error)
}

type PersistentRegistry struct{ Store PersistentStore }

func (r PersistentRegistry) Save(ctx context.Context, record *EvidenceRecord) error {
	if r.Store == nil {
		return ErrEvidenceNotFound
	}
	return r.Store.SaveEvidenceRecord(ctx, record)
}

func (r PersistentRegistry) Get(ctx context.Context, ref string) (*EvidenceRecord, error) {
	if r.Store == nil {
		return nil, ErrEvidenceNotFound
	}
	return r.Store.GetEvidenceRecord(ctx, ref)
}

func (r PersistentRegistry) List(ctx context.Context) ([]*EvidenceRecord, error) {
	if r.Store == nil {
		return nil, ErrEvidenceNotFound
	}
	return r.Store.ListEvidenceRecords(ctx)
}

// MemoryRegistry is deterministic and is used by unit tests and local
// fixtures. Production services can adapt the repository EvidenceRepository
// without changing the retriever or provider.
type MemoryRegistry struct {
	mu      sync.RWMutex
	records map[string]*EvidenceRecord
}

func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{records: make(map[string]*EvidenceRecord)}
}

func (r *MemoryRegistry) Save(ctx context.Context, record *EvidenceRecord) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	if record == nil {
		return ErrInvalidRecord
	}
	normalized := record.Normalize()
	if err := normalized.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.records[normalized.EvidenceRef]; ok {
		if existing.PayloadHash == normalized.PayloadHash && existing.ChunkHash == normalized.ChunkHash {
			return nil
		}
		return ErrEvidenceConflict
	}
	r.records[normalized.EvidenceRef] = normalized.Clone()
	return nil
}

func (r *MemoryRegistry) Get(ctx context.Context, evidenceRef string) (*EvidenceRecord, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	value, ok := r.records[strings.TrimSpace(evidenceRef)]
	if !ok {
		return nil, ErrEvidenceNotFound
	}
	return value.Clone(), nil
}

func (r *MemoryRegistry) List(ctx context.Context) ([]*EvidenceRecord, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*EvidenceRecord, 0, len(r.records))
	for _, value := range r.records {
		result = append(result, value.Clone())
	}
	sort.Slice(result, func(i, j int) bool { return result[i].EvidenceRef < result[j].EvidenceRef })
	return result, nil
}

type RetrievalQuery struct {
	Query              string
	AllowedSourceTypes []SourceType
	Limit              int
	Now                time.Time
}

type Retriever interface {
	Retrieve(ctx context.Context, query RetrievalQuery) ([]EvidenceRecord, error)
}

type LexicalRetriever struct{ Registry Registry }

func (r LexicalRetriever) Retrieve(ctx context.Context, query RetrievalQuery) ([]EvidenceRecord, error) {
	if r.Registry == nil {
		return nil, ErrEvidenceNotFound
	}
	query.Query = strings.TrimSpace(query.Query)
	if query.Query == "" || len(query.Query) > 2048 {
		return nil, ErrInvalidRecord
	}
	limit := query.Limit
	if limit <= 0 || limit > MaxEvidenceRecords {
		limit = 5
	}
	allowed := make(map[SourceType]struct{}, len(query.AllowedSourceTypes))
	for _, sourceType := range query.AllowedSourceTypes {
		allowed[sourceType] = struct{}{}
	}
	terms := lexicalTerms(query.Query)
	values, err := r.Registry.List(ctx)
	if err != nil {
		return nil, err
	}
	type scored struct {
		value *EvidenceRecord
		score int
	}
	items := make([]scored, 0, len(values))
	now := query.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	for _, value := range values {
		if len(allowed) > 0 {
			if _, ok := allowed[value.SourceType]; !ok {
				continue
			}
		}
		if value.ValidUntil != nil && !now.Before(*value.ValidUntil) {
			continue
		}
		text := strings.ToLower(value.Content + " " + string(value.SourceType) + " " + value.SourceRef)
		score := 0
		for _, term := range terms {
			if strings.Contains(text, term) {
				score++
			}
		}
		if score > 0 {
			items = append(items, scored{value: value, score: score})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		return items[i].value.EvidenceRef < items[j].value.EvidenceRef
	})
	result := make([]EvidenceRecord, 0, min(limit, len(items)))
	for _, item := range items[:min(limit, len(items))] {
		result = append(result, *item.value.Clone())
	}
	return result, nil
}

func lexicalTerms(value string) []string {
	parts := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	seen := make(map[string]struct{}, len(parts))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) < 2 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		result = append(result, part)
	}
	return result
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (r EvidenceRecord) String() string {
	return fmt.Sprintf("%s (%s)", r.EvidenceRef, r.SourceType)
}
