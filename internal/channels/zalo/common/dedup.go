package common

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Dedup is a bounded LRU+TTL cache of webhook message IDs scoped per
// channel-instance UUID. Used by the router to short-circuit retries
// Zalo sends after timeouts.
type Dedup struct {
	mu  sync.Mutex
	ttl time.Duration
	max int
	m   map[string]time.Time // key: instanceID|messageID
}

// NewDedup returns a Dedup with TTL and max-entries cap.
func NewDedup(ttl time.Duration, max int) *Dedup {
	return &Dedup{
		ttl: ttl,
		max: max,
		m:   make(map[string]time.Time),
	}
}

// SeenOrAdd records the (instanceID, messageID) pair and reports whether
// it was already seen within TTL. Empty messageID is not-seen and not recorded.
func (d *Dedup) SeenOrAdd(instanceID uuid.UUID, messageID string) bool {
	if messageID == "" {
		return false
	}
	key := instanceID.String() + "|" + messageID

	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	if t, ok := d.m[key]; ok && now.Sub(t) < d.ttl {
		return true
	}

	d.evictExpired(now)
	if len(d.m) >= d.max {
		d.evictOldest()
	}
	d.m[key] = now
	return false
}

// Len reports the current entry count (live + not-yet-pruned).
func (d *Dedup) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.m)
}

func (d *Dedup) evictExpired(now time.Time) {
	for k, t := range d.m {
		if now.Sub(t) >= d.ttl {
			delete(d.m, k)
		}
	}
}

func (d *Dedup) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, t := range d.m {
		if first || t.Before(oldestTime) {
			oldestKey = k
			oldestTime = t
			first = false
		}
	}
	if !first {
		delete(d.m, oldestKey)
	}
}
