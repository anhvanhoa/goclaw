package common

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/safego"
)

// Router dispatches webhook POSTs to registered Zalo channel instances.
// Channels register at Start() and unregister at Stop(); the process-global
// router (shared.go) is mounted once on the mux via MountRoute().
type Router struct {
	mu          sync.RWMutex
	instances   map[uuid.UUID]*registeredInstance
	dedup       *Dedup
	rateLimiter *channels.WebhookRateLimiter
	maxBodySize int64

	routeMu      sync.Mutex
	routeHandled bool
}

// MountRoute returns (WebhookPath, r) on the first call across the shared
// router and ("", nil) afterwards. Sticky across instance_loader.Reload
// because http.ServeMux would panic on re-mount.
func (r *Router) MountRoute() (string, http.Handler) {
	r.routeMu.Lock()
	defer r.routeMu.Unlock()
	if !r.routeHandled {
		r.routeHandled = true
		return WebhookPath, r
	}
	return "", nil
}

// emptyIDStreakWarnThreshold catches schema drift where the extractor
// silently disables dedup by always returning empty.
const emptyIDStreakWarnThreshold = 10

type registeredInstance struct {
	handler  WebhookHandler
	tenantID uuid.UUID

	ctx    context.Context
	cancel context.CancelFunc

	// emptyIDStreak counts consecutive empty extractor returns; resets on
	// any non-empty extraction.
	emptyIDStreak atomic.Int64
}

// WebhookHandler is the per-instance contract the router invokes after
// rate limit / signature / dedup checks pass.
type WebhookHandler interface {
	HandleWebhookEvent(ctx context.Context, raw json.RawMessage) error
	SignatureVerifier() SignatureVerifier
	MessageIDExtractor() MessageIDExtractor
}

// SignatureVerifier validates per-request authenticity.
type SignatureVerifier interface {
	Verify(headers http.Header, body []byte) error
}

// MessageIDExtractor pulls the dedup id; "" disables dedup for the event.
type MessageIDExtractor interface {
	ExtractMessageID(raw json.RawMessage) string
}

// ErrSignatureMismatch is the canonical signature-mismatch error; the
// router maps it to 401.
var ErrSignatureMismatch = errors.New("zalo_common: webhook signature mismatch")

const (
	defaultDedupTTL     = 5 * time.Minute
	defaultDedupMax     = 1000
	defaultMaxBodyBytes = 1 * 1024 * 1024
)

// NewRouter returns a router with default dedup and rate-limit params.
func NewRouter() *Router {
	return &Router{
		instances:   make(map[uuid.UUID]*registeredInstance),
		dedup:       NewDedup(defaultDedupTTL, defaultDedupMax),
		rateLimiter: channels.NewWebhookRateLimiter(),
		maxBodySize: defaultMaxBodyBytes,
	}
}

// RegisterInstance enrolls a channel for routing. The per-instance ctx
// is cancelled by UnregisterInstance so dispatch goroutines bail promptly.
func (r *Router) RegisterInstance(id uuid.UUID, h WebhookHandler, tenantID uuid.UUID) {
	ctx, cancel := context.WithCancel(context.Background())
	inst := &registeredInstance{
		handler:  h,
		tenantID: tenantID,
		ctx:      ctx,
		cancel:   cancel,
	}
	r.mu.Lock()
	r.instances[id] = inst
	r.mu.Unlock()
}

// UnregisterInstance removes the channel and cancels its dispatch ctx.
// Idempotent.
func (r *Router) UnregisterInstance(id uuid.UUID) {
	r.mu.Lock()
	inst, ok := r.instances[id]
	delete(r.instances, id)
	r.mu.Unlock()
	if ok && inst.cancel != nil {
		inst.cancel()
	}
}

func (r *Router) lookup(id uuid.UUID) (*registeredInstance, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	inst, ok := r.instances[id]
	return inst, ok
}

// ServeHTTP returns 200 once dispatch reaches the handler — Zalo retries
// hard on non-2xx, so handler errors are logged, not surfaced. Pre-dispatch
// failures (auth, rate limit, parse) return 4xx for operator visibility.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	instanceStr := req.URL.Query().Get("instance")
	instanceID, err := uuid.Parse(instanceStr)
	if err != nil {
		http.Error(w, "bad instance", http.StatusBadRequest)
		return
	}

	if !r.rateLimiter.Allow(instanceID.String()) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}

	inst, ok := r.lookup(instanceID)
	if !ok {
		http.Error(w, "unknown instance", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(io.LimitReader(req.Body, r.maxBodySize))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	if err := inst.handler.SignatureVerifier().Verify(req.Header, body); err != nil {
		slog.Warn("security.zalo_webhook_signature_mismatch",
			"instance_id", instanceID,
			"remote", req.RemoteAddr,
			"err", err)
		http.Error(w, "signature mismatch", http.StatusUnauthorized)
		return
	}

	mid := inst.handler.MessageIDExtractor().ExtractMessageID(body)
	if mid == "" {
		// Warn-and-reset at threshold so silent schema drift doesn't go
		// unnoticed; throttles to one warn per threshold-event window.
		n := inst.emptyIDStreak.Add(1)
		if n >= emptyIDStreakWarnThreshold {
			inst.emptyIDStreak.Store(0)
			slog.Warn("zalo_webhook.empty_message_id_streak",
				"count", n,
				"instance_id", instanceID,
				"hint", "extractor may need update for schema drift")
		}
	} else {
		inst.emptyIDStreak.Store(0)
		if r.dedup.SeenOrAdd(instanceID, mid) {
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	go r.dispatch(instanceID, inst, body)
	w.WriteHeader(http.StatusOK)
}

// dispatch runs the handler in a goroutine so the HTTP ack isn't blocked
// (Zalo expects ack within ~2s). Panics are recovered and logged.
func (r *Router) dispatch(instanceID uuid.UUID, inst *registeredInstance, body []byte) {
	defer safego.Recover(nil, "instance_id", instanceID, "tenant_id", inst.tenantID)
	if err := inst.handler.HandleWebhookEvent(inst.ctx, body); err != nil {
		slog.Error("zalo_webhook.handler_error",
			"instance_id", instanceID,
			"tenant_id", inst.tenantID,
			"err", err)
	}
}
