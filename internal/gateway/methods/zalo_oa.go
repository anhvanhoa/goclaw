package methods

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	zalooa "github.com/nextlevelbuilder/goclaw/internal/channels/zalo/oa"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

const (
	zaloOAStateTTL          = 10 * time.Minute
	zaloOAMaxStatesPerInst  = 5 // most-recent-N consent attempts per instance
)

// ZaloOAMethods serves the WS handlers backing the paste-code consent flow.
type ZaloOAMethods struct {
	store  store.ChannelInstanceStore
	msgBus *bus.MessageBus

	stateMu sync.Mutex
	states  map[string]zaloOAStateEntry // key: instanceID|state
}

type zaloOAStateEntry struct {
	instID    uuid.UUID
	expiresAt time.Time
}

// NewZaloOAMethods constructs the handler. msgBus may be nil during tests.
func NewZaloOAMethods(s store.ChannelInstanceStore, msgBus *bus.MessageBus) *ZaloOAMethods {
	return &ZaloOAMethods{
		store:  s,
		msgBus: msgBus,
		states: make(map[string]zaloOAStateEntry),
	}
}

// Register wires the methods into the WS router.
func (m *ZaloOAMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodChannelInstancesZaloOAConsentURL, m.handleConsentURL)
	router.Register(protocol.MethodChannelInstancesZaloOAExchangeCode, m.handleExchangeCode)
}

// handleConsentURL builds the Zalo authorization URL server-side so the
// frontend never receives app_id (which is masked in maskInstance anyway).
func (m *ZaloOAMethods) handleConsentURL(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		InstanceID string `json:"instance_id"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	instID, err := uuid.Parse(params.InstanceID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "instance")))
		return
	}

	inst, err := m.store.Get(ctx, instID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgInstanceNotFound)))
		return
	}
	if inst.TenantID != client.TenantID() {
		// Defense-in-depth: store-layer Get already filters by tenant_id,
		// but a future refactor that loosens that check shouldn't allow
		// cross-tenant consent URL leakage.
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgInstanceNotFound)))
		return
	}
	if inst.ChannelType != channels.TypeZaloOA {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgZaloOAInvalidChannelType)))
		return
	}

	creds, err := zalooa.LoadCreds(inst.Credentials)
	if err != nil || creds.AppID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "zalo_oa: missing app_id in credentials"))
		return
	}
	if creds.RedirectURI == "" {
		// Zalo rejects mismatched redirect_uri with error_code=-14003 —
		// fail fast with an actionable error rather than letting the user
		// run the consent flow and hit an opaque Zalo error page.
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgZaloOARedirectURIRequired)))
		return
	}

	state, err := newStateToken()
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "zalo_oa: state token gen failed"))
		return
	}
	m.putState(instID, state)

	url := zalooa.ConsentURL(creds.AppID, creds.RedirectURI, state)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"url":   url,
		"state": state,
	}))
}

// handleExchangeCode swaps the pasted authorization code for tokens and
// persists them via the store-encrypted credentials blob.
func (m *ZaloOAMethods) handleExchangeCode(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	var params struct {
		InstanceID string `json:"instance_id"`
		Code       string `json:"code"`
		State      string `json:"state"`
		OAID       string `json:"oa_id"` // optional — from the callback URL query string
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}
	instID, err := uuid.Parse(params.InstanceID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "instance")))
		return
	}
	if params.Code == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "code")))
		return
	}
	if !m.consumeState(instID, params.State) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgZaloOAInvalidState)))
		return
	}

	inst, err := m.store.Get(ctx, instID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgInstanceNotFound)))
		return
	}
	if inst.TenantID != client.TenantID() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgInstanceNotFound)))
		return
	}
	if inst.ChannelType != channels.TypeZaloOA {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgZaloOAInvalidChannelType)))
		return
	}

	creds, err := zalooa.LoadCreds(inst.Credentials)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgZaloOACodeExchangeFailed, err.Error())))
		return
	}

	httpClient := zalooa.NewClient(15 * time.Second)
	tok, err := httpClient.ExchangeCode(ctx, creds.AppID, creds.SecretKey, params.Code)
	if err != nil {
		slog.Warn("zalo_oa.exchange_failed", "instance_id", instID, "oa_id", creds.OAID, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgZaloOACodeExchangeFailed, err.Error())))
		return
	}
	creds.WithTokens(tok)
	// Zalo's OAuth token endpoint does NOT return oa_id; it rides in the
	// callback URL query string alongside `code`. Persist it here so the
	// reloaded Channel's Start() sees a non-empty OAID and marks Healthy
	// (otherwise it stays Degraded "awaiting consent" forever).
	if params.OAID != "" {
		creds.OAID = params.OAID
	}
	credsBytes, err := creds.Marshal()
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgZaloOACodeExchangeFailed, err.Error())))
		return
	}
	if err := m.store.Update(ctx, instID, map[string]any{"credentials": credsBytes}); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, i18n.T(locale, i18n.MsgZaloOACodeExchangeFailed, err.Error())))
		return
	}
	m.emitCacheInvalidate()

	slog.Info("zalo_oa.connected", "instance_id", instID, "oa_id", creds.OAID, "expires_at", tok.ExpiresAt)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":         true,
		"oa_id":      creds.OAID,
		"expires_at": tok.ExpiresAt,
		"message":    i18n.T(locale, i18n.MsgZaloOAConnected, creds.OAID),
	}))
}

func (m *ZaloOAMethods) emitCacheInvalidate() {
	if m.msgBus == nil {
		return
	}
	m.msgBus.Broadcast(bus.Event{
		Name:    protocol.EventCacheInvalidate,
		Payload: bus.CacheInvalidatePayload{Kind: bus.CacheKindChannelInstances},
	})
}

// putState records a freshly minted state token with a 10min TTL. Caps
// pending entries per instance to bound memory abuse from an operator
// repeatedly clicking "Connect" without ever pasting the code.
func (m *ZaloOAMethods) putState(instID uuid.UUID, state string) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	m.gcStatesLocked()
	m.evictOldestForInstanceLocked(instID, zaloOAMaxStatesPerInst-1)
	m.states[stateKey(instID, state)] = zaloOAStateEntry{
		instID:    instID,
		expiresAt: time.Now().Add(zaloOAStateTTL),
	}
}

// evictOldestForInstanceLocked drops oldest-by-expiry entries for instID
// until at most `keep` remain. Caller MUST hold m.stateMu.
func (m *ZaloOAMethods) evictOldestForInstanceLocked(instID uuid.UUID, keep int) {
	type kv struct {
		key string
		exp time.Time
	}
	var entries []kv
	for k, v := range m.states {
		if v.instID == instID {
			entries = append(entries, kv{k, v.expiresAt})
		}
	}
	if len(entries) <= keep {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].exp.Before(entries[j].exp) })
	for i := 0; i < len(entries)-keep; i++ {
		delete(m.states, entries[i].key)
	}
}

// consumeState atomically validates+removes a state token. Returns false
// if missing or expired.
func (m *ZaloOAMethods) consumeState(instID uuid.UUID, state string) bool {
	if state == "" {
		return false
	}
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	key := stateKey(instID, state)
	entry, ok := m.states[key]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(m.states, key) // GC the expired entry too
		return false
	}
	delete(m.states, key)
	return true
}

func (m *ZaloOAMethods) gcStatesLocked() {
	now := time.Now()
	for k, v := range m.states {
		if now.After(v.expiresAt) {
			delete(m.states, k)
		}
	}
}

func stateKey(instID uuid.UUID, state string) string {
	return fmt.Sprintf("%s|%s", instID, state)
}

func newStateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
