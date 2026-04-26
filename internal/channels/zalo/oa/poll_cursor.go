package oa

import (
	"container/list"
	"encoding/json"
	"sort"
	"sync"
)

const (
	defaultCursorMaxEntries = 500
	configCursorKey         = "poll_cursor"
)

// pollCursor tracks the last-seen unix-ms timestamp per Zalo user_id so the
// polling loop doesn't re-emit messages on subsequent cycles. Bounded LRU
// (default 500 entries) prevents unbounded growth on high-traffic OAs;
// evicted entries lose history → that user may re-receive a single message
// the next time they message in (acceptable trade-off for v1).
type pollCursor struct {
	mu    sync.Mutex
	max   int
	data  map[string]*list.Element // user_id → element holding cursorEntry
	order *list.List               // front = most-recently-used
	dirty bool
}

type cursorEntry struct {
	userID string
	ts     int64
}

func newPollCursor(max int) *pollCursor {
	if max <= 0 {
		max = defaultCursorMaxEntries
	}
	return &pollCursor{
		max:   max,
		data:  make(map[string]*list.Element),
		order: list.New(),
	}
}

// Advance updates the cursor for userID if ts is strictly newer than the
// previous value. Returns true if the cursor moved (caller may use this
// to track work-done). Touching the entry promotes it to MRU regardless.
func (c *pollCursor) Advance(userID string, ts int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.data[userID]; ok {
		entry := elem.Value.(*cursorEntry)
		if ts <= entry.ts {
			c.order.MoveToFront(elem)
			return false
		}
		entry.ts = ts
		c.order.MoveToFront(elem)
		c.dirty = true
		return true
	}
	// New entry.
	entry := &cursorEntry{userID: userID, ts: ts}
	elem := c.order.PushFront(entry)
	c.data[userID] = elem
	c.dirty = true
	c.evictLocked()
	return true
}

// Get returns the cursor for userID; 0 if missing.
func (c *pollCursor) Get(userID string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.data[userID]; ok {
		return elem.Value.(*cursorEntry).ts
	}
	return 0
}

// Snapshot returns a copy of the cursor map. Safe to mutate; does not
// affect the cursor.
func (c *pollCursor) Snapshot() map[string]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int64, len(c.data))
	for k, elem := range c.data {
		out[k] = elem.Value.(*cursorEntry).ts
	}
	return out
}

func (c *pollCursor) IsDirty() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dirty
}

func (c *pollCursor) ClearDirty() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dirty = false
}

// evictLocked drops the LRU tail until size <= max. Caller MUST hold mu.
func (c *pollCursor) evictLocked() {
	for c.order.Len() > c.max {
		tail := c.order.Back()
		if tail == nil {
			return
		}
		entry := tail.Value.(*cursorEntry)
		delete(c.data, entry.userID)
		c.order.Remove(tail)
	}
}

// loadFromMap seeds the cursor from a previously-persisted map. When the
// persisted set is larger than max, eviction-on-load drops entries — keys
// are sorted ascending by timestamp first so the OLDEST cursors are the
// ones evicted, not random ones from Go map-iteration order. (Map order
// would mean a heavy OA loses different users on every restart.)
func (c *pollCursor) loadFromMap(m map[string]int64) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if m[keys[i]] != m[keys[j]] {
			return m[keys[i]] < m[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		c.Advance(k, m[k])
	}
	c.ClearDirty() // post-load is a clean state
}

// parseCursorFromConfig extracts the poll_cursor sub-object from the
// channel_instances.config blob. Tolerant of missing key + invalid JSON
// (returns empty map).
func parseCursorFromConfig(raw []byte) map[string]int64 {
	out := map[string]int64{}
	if len(raw) == 0 {
		return out
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return out
	}
	cursorRaw, ok := top[configCursorKey]
	if !ok {
		return out
	}
	_ = json.Unmarshal(cursorRaw, &out)
	return out
}

