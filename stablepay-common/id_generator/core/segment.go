package core

import (
	"sync/atomic"
	"time"
)

// Segment represents a range of IDs allocated from the database.
type Segment struct {
	Start int64
	Step  int64
	Max   int64
	
	current int64
}

// NewSegment creates a new segment.
func NewSegment(start, step int64) *Segment {
	return &Segment{
		Start:   start,
		Step:    step,
		Max:     start + step,
		current: start,
	}
}

// Next returns the next ID from the segment if available.
func (s *Segment) Next() (int64, bool) {
	val := atomic.AddInt64(&s.current, 1)
	if val > s.Max {
		return 0, false
	}
	return val, true
}

// Table Model
type LeafAlloc struct {
	BizTag      string    `gorm:"column:biz_tag;primaryKey"`
	MaxId       int64     `gorm:"column:max_id"`
	Step        int64     `gorm:"column:step"`
	Description string    `gorm:"column:description"`
	UpdateTime  time.Time `gorm:"column:update_time"`
}

func (LeafAlloc) TableName() string {
	return "leaf_alloc"
}
