package procurementmvp

import (
	"errors"
	"sync"
	"time"
)

const maxSnapshotBytes = 8 << 20

var ErrCapacity = errors.New("演示服务容量已满，请稍后重试或联系维护者归档")

// Bound anonymous write traffic without Redis or another service. This is a
// per-process limit, not a distributed rate limiter. It also bounds the number
// of remembered principals; changing cookies cannot bypass the global limit.
type requestGate struct {
	mu     sync.Mutex
	minute int64
	total  int
	owners map[string]int
}

func (g *requestGate) allow(owner string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	minute := now.Unix() / 60
	if minute != g.minute {
		g.minute = minute
		g.total = 0
		g.owners = map[string]int{}
	}
	if g.total >= 120 || g.owners[owner] >= 30 || (len(g.owners) >= 1024 && g.owners[owner] == 0) {
		return false
	}
	g.total++
	g.owners[owner]++
	return true
}

// Remove only expired, unpurchased planning data. Never delete paid or pending
// purchases, approval evidence, tool attempts or reports to make room.
func cleanupPlanning(st *state, now time.Time) {
	usedQuotes, usedPlans := map[string]bool{}, map[string]bool{}
	for _, p := range st.Purchases {
		usedQuotes[p.Quote.ID] = true
		usedPlans[p.Quote.PlanID] = true
	}
	for key, q := range st.Quotes {
		if !usedQuotes[key] && now.After(q.ExpiresAt.Add(24*time.Hour)) {
			delete(st.Quotes, key)
		} else {
			usedPlans[q.PlanID] = true
		}
	}
	for key, p := range st.Plans {
		if !usedPlans[key] && now.After(p.CreatedAt.Add(24*time.Hour)) {
			delete(st.Plans, key)
		}
	}
}
