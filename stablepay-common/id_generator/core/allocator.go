package core

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"gorm.io/gorm"
)

// SegmentAllocator implements the Leaf-Segment algorithm with Dual Buffer.
type SegmentAllocator struct {
	db     *gorm.DB
	bizTag string

	mu      sync.Mutex
	current *Segment
	next    *Segment

	isLoadingNext bool
}

// NewSegmentAllocator creates a new allocator.
func NewSegmentAllocator(db *gorm.DB, bizTag string) *SegmentAllocator {
	return &SegmentAllocator{
		db:     db,
		bizTag: bizTag,
	}
}

// NextId returns the next globally unique ID.
func (a *SegmentAllocator) NextId(ctx context.Context) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.current == nil {
		seg, err := a.loadSegmentFromDB(ctx)
		if err != nil {
			return 0, err
		}
		a.current = seg
	}

	id, ok := a.current.Next()
	if ok {
		// Check if we need to load next segment asynchronously
		// Threshold: 50% consumed
		currentVal := a.current.current // unsafe access but ok for heuristic
		total := a.current.Max - a.current.Start
		consumed := currentVal - a.current.Start
		
		if consumed > total/2 && a.next == nil && !a.isLoadingNext {
			a.isLoadingNext = true
			go func() {
				defer func() {
					a.mu.Lock()
					a.isLoadingNext = false
					a.mu.Unlock()
				}()
				seg, err := a.loadSegmentFromDB(context.Background())
				if err != nil {
					log.Printf("Failed to load next segment for %s: %v", a.bizTag, err)
					return
				}
				a.mu.Lock()
				a.next = seg
				a.mu.Unlock()
			}()
		}
		return id, nil
	}

	// Current segment exhausted
	if a.next != nil {
		a.current = a.next
		a.next = nil
		id, ok = a.current.Next()
		if ok {
			return id, nil
		}
	}

	// If next is not ready, wait or load synchronously
	// For simplicity, load synchronously here if next is missing
	if a.isLoadingNext {
		// Wait for async load to finish? Or just force load.
		// Force load might race with async. 
		// Simpler: just wait a bit loop.
		for i := 0; i < 100; i++ {
			if a.next != nil {
				a.current = a.next
				a.next = nil
				id, ok = a.current.Next()
				if ok {
					return id, nil
				}
			}
			a.mu.Unlock()
			time.Sleep(10 * time.Millisecond)
			a.mu.Lock()
		}
	}
	
	// Fallback synchronous load
	seg, err := a.loadSegmentFromDB(ctx)
	if err != nil {
		return 0, err
	}
	a.current = seg
	id, ok = a.current.Next()
	if ok {
		return id, nil
	}

	return 0, errors.New("failed to get next ID")
}

func (a *SegmentAllocator) loadSegmentFromDB(ctx context.Context) (*Segment, error) {
	var alloc LeafAlloc
	var newMaxId int64
	var step int64

	err := a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// CRITICAL FIX: Use SELECT FOR UPDATE to lock the row BEFORE updating
		// This prevents race conditions where multiple instances read the same max_id
		if err := tx.Raw("SELECT max_id, step FROM leaf_alloc WHERE biz_tag = ? FOR UPDATE", a.bizTag).Scan(&alloc).Error; err != nil {
			return err
		}

		// Calculate new max_id in application (based on locked value)
		newMaxId = alloc.MaxId + alloc.Step
		step = alloc.Step

		// Update with explicit value (not max_id + step in SQL)
		// This ensures atomicity: the update is based on the locked value we just read
		if err := tx.Exec("UPDATE leaf_alloc SET max_id = ?, update_time = NOW() WHERE biz_tag = ?", newMaxId, a.bizTag).Error; err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	// The segment is [oldMaxId, newMaxId)
	// oldMaxId = newMaxId - step
	return NewSegment(newMaxId-step, step), nil
}
