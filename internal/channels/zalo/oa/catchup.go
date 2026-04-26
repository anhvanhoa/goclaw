package oa

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	// catchUpStaleThreshold is how stale the cursor must be before the
	// catch-up sweep does a recovery list call. Picked to tolerate normal
	// gateway restarts without re-fetching every boot.
	catchUpStaleThreshold = 30 * time.Minute
	// catchUpPageSize is the bounded listrecentchat page size used by the
	// recovery sweep — single page only, no pagination.
	catchUpPageSize = 50
)

// runCatchUpSweep recovers messages potentially missed during gateway
// downtime. Single bounded listrecentchat page, error-tolerant. Gated on
// cursor staleness so a fresh restart in steady-state polling doesn't
// duplicate recent dispatches.
//
// The sweep funnels through the same dedup path as polling
// ((from_id, time) cursor + seen_ids LRU) so any overlap with messages
// already delivered via webhook is harmless.
func (c *Channel) runCatchUpSweep(parentCtx context.Context) {
	ctx := store.WithTenantID(parentCtx, c.TenantID())

	last := c.cursor.LastSeenTimestamp()
	if last != 0 && time.Since(time.UnixMilli(last)) < catchUpStaleThreshold {
		return
	}

	msgs, err := c.listRecentChat(ctx, 0, catchUpPageSize)
	if err != nil {
		slog.Warn("zalo_oa.webhook.catchup_failed", "err", err)
		return
	}
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].Time < msgs[j].Time })

	dispatched := 0
	for _, m := range msgs {
		if m.FromID == "" || m.FromID == c.creds.OAID {
			continue
		}
		if m.Time != 0 {
			if m.Time <= c.cursor.Get(m.FromID) {
				continue
			}
		} else if m.MessageID == "" || c.seenIDs.SeenOrAdd(m.MessageID) {
			continue
		}
		c.dispatchInbound(m)
		if m.Time != 0 {
			c.cursor.Advance(m.FromID, m.Time)
		}
		dispatched++
	}
	slog.Info("zalo_oa.webhook.catchup_done", "fetched", len(msgs), "dispatched", dispatched)
}
