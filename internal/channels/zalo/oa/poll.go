package oa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
)

// message is a single entry in the /v2.0/oa/listrecentchat response. This
// endpoint returns the most-recent N messages across all users — each row
// IS a message, not a thread summary. The live response shape (verified
// against openapi.zalo.me via API explorer, 2026-04-20):
//
//	{"error":0,"message":"Success","data":[{
//	   "from_id":"...", "from_display_name":"...", "from_avatar":"...",
//	   "to_id":"...",   "to_display_name":"...",   "to_avatar":"...",
//	   "message_id":"...", "type":"text", "message":"...", "time":<unix-ms>
//	}]}
//
// Filter: from_id == creds.OAID means OA outbound echo — skip.
// The remaining fields are non-sensitive metadata we pass through as
// bus.InboundMessage.Metadata when useful.
type message struct {
	MessageID       string `json:"message_id"`
	FromID          string `json:"from_id"`
	FromDisplayName string `json:"from_display_name,omitempty"`
	ToID            string `json:"to_id,omitempty"`
	Time            int64  `json:"time,omitempty"`
	Text            string `json:"message,omitempty"` // Zalo's field is "message", not "text"
	Type            string `json:"type,omitempty"`    // text/image/file/sticker
}

// listRecentChat fetches the most-recent N messages across all users.
// Zalo v2.0 encodes GET params as a single JSON blob in the `data` query
// parameter (e.g. ?data={"offset":0,"count":10}).
func (c *Channel) listRecentChat(ctx context.Context, offset, count int) ([]message, error) {
	tok, err := c.tokens.Access(ctx)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(map[string]int{"offset": offset, "count": count})
	if err != nil {
		return nil, fmt.Errorf("zalo_oa: marshal listrecentchat params: %w", err)
	}
	q := url.Values{"data": {string(data)}}
	raw, err := c.client.apiGet(ctx, pathListRecentChat, q, tok)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Data []message `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("zalo_oa: decode listrecentchat: %w", err)
	}
	return wrap.Data, nil
}

// pollOnce runs one polling cycle. Returns ErrRateLimit if Zalo signals
// 429 (caller should back off); other errors are transient and the next
// cycle retries normally. Retry-once-on-auth mirrors Channel.post so a
// revoked token gets a chance to refresh before we give up.
//
// Design: listrecentchat returns the last N messages across all users
// (NOT a thread summary — each row is a message, verified via API
// explorer 2026-04-20). We iterate oldest-first, filter OA echoes
// (from_id == oa_id), dedup per-user by last-seen timestamp, and
// dispatch via BaseChannel.HandleMessage.
//
// Phase 06: burn-down loop pages through listrecentchat until a partial
// page (caught up) or maxPages cap (warn). Default 50 × 5 = 250 msg/cycle
// vs the prior hardcoded 10 — ~25× headroom for bursty OAs.
func (c *Channel) pollOnce(ctx context.Context) error {
	if c.skipPollIfAuthFailed() {
		return nil
	}

	pageSize := pollCountFromCfg(c.cfg.PollCount)
	maxPages := pollBurndownMaxPagesFromCfg(c.cfg.PollBurndownMaxPages)

	for page := 0; page < maxPages; page++ {
		offset := page * pageSize
		msgs, err := c.listRecentChatRetryAuth(ctx, offset, pageSize)
		if err != nil {
			return err
		}
		if len(msgs) == 0 {
			break
		}
		c.processMessages(msgs)
		if len(msgs) < pageSize {
			break // partial page — caught up
		}
		if page == maxPages-1 {
			slog.Warn("zalo_oa.poll.burndown_capped",
				"oa_id", c.creds.OAID,
				"max_pages", maxPages,
				"page_size", pageSize,
				"hint", "raise poll_count or shorten poll_interval_seconds if this is steady-state")
		}
	}
	return nil
}

// listRecentChatRetryAuth wraps listRecentChat with a single retry-on-auth-
// failure that forces a token refresh. Extracted from pollOnce so each
// burn-down page can retry independently.
func (c *Channel) listRecentChatRetryAuth(ctx context.Context, offset, count int) ([]message, error) {
	msgs, err := c.listRecentChat(ctx, offset, count)
	if err == nil {
		return msgs, nil
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.isAuth() {
		slog.Warn("zalo_oa.poll.token_rejected_forcing_refresh",
			"oa_id", c.creds.OAID, "zalo_code", apiErr.Code, "zalo_msg", apiErr.Message)
		c.tokens.ForceRefresh()
		return c.listRecentChat(ctx, offset, count)
	}
	return nil, err
}

// processMessages iterates a single page oldest-first, filters OA echoes
// + malformed rows, dedups via (cursor, seenIDs), and dispatches each
// surviving message through BaseChannel.HandleMessage.
func (c *Channel) processMessages(msgs []message) {
	// Process oldest-first so the cursor advances monotonically.
	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].Time < msgs[j].Time })

	for _, m := range msgs {
		if m.FromID == "" || m.FromID == c.creds.OAID {
			continue // drop malformed + OA echoes
		}
		if m.Time == 0 && m.MessageID == "" {
			// Without either signal there's no dedup hook — would re-dispatch
			// every poll for as long as the row stays in the listrecentchat
			// window. Drop rather than risk duplicate handler invocations.
			continue
		}
		// Dedup by the (from_id, time) cursor when Zalo provides `time`.
		// When time == 0 (field omitted), fall back to a bounded LRU of
		// message_ids — otherwise a missing-time row would re-dispatch
		// every poll tick for as long as it sits in listrecentchat's
		// window. Real-world incidence is near zero (Zalo always sets
		// time) but the safety net must hold.
		if m.Time != 0 {
			if m.Time <= c.cursor.Get(m.FromID) {
				continue
			}
		} else if m.MessageID != "" && c.seenIDs.SeenOrAdd(m.MessageID) {
			continue
		}
		c.dispatchInbound(m)
		if m.Time != 0 {
			c.cursor.Advance(m.FromID, m.Time)
		}
	}
}

// dispatchInbound maps a Zalo message into a BaseChannel.HandleMessage call.
// Zalo OA is DM-only, so chatID == senderID (the user's Zalo ID). Phase 04
// emits text only — non-text payloads are logged and skipped.
func (c *Channel) dispatchInbound(m message) {
	if m.Type != "" && m.Type != "text" {
		slog.Info("zalo_oa.poll.non_text_skipped",
			"oa_id", c.creds.OAID, "user_id", m.FromID, "message_id", m.MessageID, "type", m.Type)
		return
	}
	if m.Text == "" {
		return
	}
	metadata := common.InboundMeta{
		MessageID:         m.MessageID,
		Platform:          common.PlatformZaloOA,
		SenderDisplayName: m.FromDisplayName,
	}.ToMap()
	c.BaseChannel.HandleMessage(m.FromID, m.FromID, m.Text, nil, metadata, "direct")
}

// skipPollIfAuthFailed mirrors safety-ticker's skip behavior: once health
// is Failed/Auth, we stop calling the API until the operator re-auths.
func (c *Channel) skipPollIfAuthFailed() bool {
	snap := c.HealthSnapshot()
	return snap.State == channels.ChannelHealthStateFailed && snap.FailureKind == channels.ChannelFailureKindAuth
}

const (
	defaultPollInterval = 15 * time.Second
	rateLimitBackoff    = 30 * time.Second
	cursorFlushInterval = 60 * time.Second

	defaultPollCount            = 50
	pollCountFloor              = 10
	pollCountCeil               = 200
	defaultPollBurndownMaxPages = 5
	pollBurndownMaxPagesCeil    = 20
)

// pollIntervalFromCfg clamps cfg.PollIntervalSeconds to the safe range.
func pollIntervalFromCfg(s int) time.Duration {
	switch {
	case s < 5:
		return defaultPollInterval
	case s > 120:
		return 120 * time.Second
	default:
		return time.Duration(s) * time.Second
	}
}

// pollCountFromCfg clamps cfg.PollCount to [pollCountFloor, pollCountCeil].
// Zero/negative → defaultPollCount. Phase 06.
func pollCountFromCfg(n int) int {
	switch {
	case n <= 0:
		return defaultPollCount
	case n < pollCountFloor:
		return pollCountFloor
	case n > pollCountCeil:
		return pollCountCeil
	default:
		return n
	}
}

// pollBurndownMaxPagesFromCfg clamps cfg.PollBurndownMaxPages to [1, 20].
// Zero/negative → defaultPollBurndownMaxPages. 1 disables burn-down (single
// page per cycle, mirrors pre-phase-06 behavior). Phase 06.
func pollBurndownMaxPagesFromCfg(n int) int {
	switch {
	case n <= 0:
		return defaultPollBurndownMaxPages
	case n > pollBurndownMaxPagesCeil:
		return pollBurndownMaxPagesCeil
	default:
		return n
	}
}
