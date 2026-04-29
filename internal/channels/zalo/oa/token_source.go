package oa

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// refreshMargin: refresh when the access token expires within this window.
const refreshMargin = 5 * time.Minute

// tokenSource lazily refreshes the access token. A single mutex guards
// both the cache and the HTTP refresh so only one refresh flies — Zalo
// refresh tokens are single-use and races would invalidate each other.
type tokenSource struct {
	client     *Client
	creds      *ChannelCreds
	store      store.ChannelInstanceStore
	instanceID uuid.UUID

	mu sync.Mutex // guards creds.{Access,Refresh}Token + ExpiresAt + serializes refresh
}

// ForceRefresh marks the cached token stale so the next Access() refreshes.
func (ts *tokenSource) ForceRefresh() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.creds.ExpiresAt = time.Time{}
	ts.creds.AccessToken = ""
}

// Access returns a valid access token, refreshing if within refreshMargin.
func (ts *tokenSource) Access(ctx context.Context) (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.creds.AccessToken != "" && time.Until(ts.creds.ExpiresAt) > refreshMargin {
		return ts.creds.AccessToken, nil
	}

	if err := ts.doRefresh(ctx); err != nil {
		return "", err
	}
	return ts.creds.AccessToken, nil
}

// doRefresh performs the HTTP refresh + persistence. Holds ts.mu.
// Persist-before-commit: if Persist fails after a successful refresh we
// keep the new tokens in memory (the old refresh token is already burned)
// but DB has stale tokens — next process restart will fail to invalid_grant
// and surface re-auth, which is the safe failure mode.
func (ts *tokenSource) doRefresh(ctx context.Context) error {
	if ts.creds.RefreshToken == "" {
		// Pre-authorization: distinct from a burned refresh token; do NOT
		// escalate to Failed.
		return ErrNotAuthorized
	}

	tok, rawErr := ts.client.RefreshToken(ctx, ts.creds.AppID, ts.creds.SecretKey, ts.creds.RefreshToken)
	if rawErr != nil {
		err := classifyRefreshError(rawErr)
		if errors.Is(err, ErrAuthExpired) {
			slog.Warn("zalo_oa.reauth_required", "instance_id", ts.instanceID, "oa_id", ts.creds.OAID)
			return err
		}
		slog.Warn("zalo_oa.refresh_failed", "instance_id", ts.instanceID, "oa_id", ts.creds.OAID, "error", err)
		return err
	}

	snapshot := *ts.creds
	snapshot.WithTokens(tok)
	if err := Persist(ctx, ts.store, ts.instanceID, &snapshot); err != nil {
		slog.Error("zalo_oa.persist_failed", "instance_id", ts.instanceID, "oa_id", ts.creds.OAID, "error", err)
		// Commit in memory: the new pair is the only valid one until restart.
		*ts.creds = snapshot
		return err
	}
	*ts.creds = snapshot
	slog.Info("zalo_oa.token_refreshed", "instance_id", ts.instanceID, "oa_id", ts.creds.OAID, "new_expires_at", ts.creds.ExpiresAt)
	return nil
}
