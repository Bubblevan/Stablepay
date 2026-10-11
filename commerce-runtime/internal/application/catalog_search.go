package application

import (
	"context"
	"errors"
	"time"

	"github.com/stablepay/commerce-runtime/internal/catalog"
)

const (
	defaultCatalogSearchLimit = 20
	maxCatalogSearchLimit     = 50
)

var ErrInvalidCatalogSearchLimit = errors.New("catalog search limit must be between 1 and 50")

// CatalogSearchResult is a read-only snapshot for agent browsing. It does not
// create an Episode, reserve inventory, or authorize a payment.
type CatalogSearchResult struct {
	QueryHash              string              `json:"query_hash"`
	SnapshotAt             time.Time           `json:"snapshot_at"`
	InventoryAuthoritative bool                `json:"inventory_authoritative"`
	RegionAuthoritative    bool                `json:"region_authoritative"`
	TotalCandidates        int                 `json:"total_candidates"`
	Candidates             []catalog.Candidate `json:"candidates"`
}

// SearchCapabilities reads the Runtime catalog and applies the same hard
// constraints/ranking rules as persisted Episode discovery. The eventual
// purchase always repeats discovery in its own Episode.
func (s *Service) SearchCapabilities(ctx context.Context, query catalog.DiscoveryQuery, limit int) (CatalogSearchResult, error) {
	store, err := s.discoveryStore()
	if err != nil {
		return CatalogSearchResult{}, err
	}
	if limit == 0 {
		limit = defaultCatalogSearchLimit
	}
	if limit < 1 || limit > maxCatalogSearchLimit {
		return CatalogSearchResult{}, ErrInvalidCatalogSearchLimit
	}
	query = query.Normalize()
	queryHash, err := query.SnapshotHash()
	if err != nil {
		return CatalogSearchResult{}, err
	}
	capabilities, err := store.ListActiveCapabilities(ctx)
	if err != nil {
		return CatalogSearchResult{}, err
	}
	now := time.Now().UTC()
	if s != nil && s.clock != nil {
		now = s.clock().UTC()
	}
	candidates, err := catalog.SearchCandidates(query, capabilities, now)
	if err != nil {
		return CatalogSearchResult{}, err
	}
	total := len(candidates)
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return CatalogSearchResult{QueryHash: queryHash, SnapshotAt: now, InventoryAuthoritative: false,
		RegionAuthoritative: false, TotalCandidates: total, Candidates: candidates}, nil
}
