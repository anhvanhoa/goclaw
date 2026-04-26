package common

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeHandler struct {
	mu          sync.Mutex
	dispatched  atomic.Int32
	lastBody    json.RawMessage
	verifyErr   error
	extractedID string
	handlerErr  error
	panicMsg    string
	doneCh      chan struct{}
}

func newFakeHandler() *fakeHandler {
	return &fakeHandler{doneCh: make(chan struct{}, 16)}
}

func (f *fakeHandler) HandleWebhookEvent(_ context.Context, raw json.RawMessage) error {
	f.mu.Lock()
	f.lastBody = raw
	f.mu.Unlock()
	f.dispatched.Add(1)
	defer func() { f.doneCh <- struct{}{} }()
	if f.panicMsg != "" {
		panic(f.panicMsg)
	}
	return f.handlerErr
}

func (f *fakeHandler) SignatureVerifier() SignatureVerifier   { return staticVerifier{err: f.verifyErr} }
func (f *fakeHandler) MessageIDExtractor() MessageIDExtractor { return staticExtractor{id: f.extractedID} }

type staticVerifier struct{ err error }

func (v staticVerifier) Verify(_ http.Header, _ []byte) error { return v.err }

type staticExtractor struct{ id string }

func (e staticExtractor) ExtractMessageID(_ json.RawMessage) string { return e.id }

func waitForDispatch(t *testing.T, h *fakeHandler) {
	t.Helper()
	select {
	case <-h.doneCh:
	case <-time.After(time.Second):
		t.Fatalf("handler not dispatched")
	}
}

func newTestServer(t *testing.T) (*Router, uuid.UUID, *fakeHandler, *httptest.Server) {
	t.Helper()
	r := NewRouter()
	id := uuid.New()
	h := newFakeHandler()
	r.RegisterInstance(id, h, uuid.New())
	return r, id, h, httptest.NewServer(r)
}

func postBody(srv *httptest.Server, query, body string) *http.Response {
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"?"+query, strings.NewReader(body))
	resp, _ := srv.Client().Do(req)
	return resp
}

func TestRouter_RejectsNonPOST(t *testing.T) {
	_, _, _, srv := newTestServer(t)
	defer srv.Close()
	resp, _ := srv.Client().Get(srv.URL)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

func TestRouter_RejectsBadInstance(t *testing.T) {
	_, _, _, srv := newTestServer(t)
	defer srv.Close()
	resp := postBody(srv, "instance=not-a-uuid", "{}")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestRouter_404UnknownInstance(t *testing.T) {
	_, _, _, srv := newTestServer(t)
	defer srv.Close()
	resp := postBody(srv, "instance="+uuid.NewString(), "{}")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestRouter_401OnSignatureMismatch(t *testing.T) {
	_, id, h, srv := newTestServer(t)
	defer srv.Close()
	h.verifyErr = ErrSignatureMismatch
	resp := postBody(srv, "instance="+id.String(), "{}")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if h.dispatched.Load() != 0 {
		t.Error("handler invoked despite signature mismatch")
	}
}

func TestRouter_200OnValidEventDispatches(t *testing.T) {
	_, id, h, srv := newTestServer(t)
	defer srv.Close()
	resp := postBody(srv, "instance="+id.String(), `{"x":1}`)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	waitForDispatch(t, h)
	if h.dispatched.Load() != 1 {
		t.Errorf("dispatched = %d, want 1", h.dispatched.Load())
	}
}

func TestRouter_DedupShortCircuit(t *testing.T) {
	_, id, h, srv := newTestServer(t)
	defer srv.Close()
	h.extractedID = "evt-1"
	postBody(srv, "instance="+id.String(), `{}`)
	waitForDispatch(t, h)

	resp := postBody(srv, "instance="+id.String(), `{}`)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	// Give the goroutine a beat — it should NOT have been dispatched.
	time.Sleep(50 * time.Millisecond)
	if h.dispatched.Load() != 1 {
		t.Errorf("dispatched = %d, want 1 (deduped)", h.dispatched.Load())
	}
}

func TestRouter_PanicInHandlerRecovered(t *testing.T) {
	_, id, h, srv := newTestServer(t)
	defer srv.Close()
	h.panicMsg = "boom"
	resp := postBody(srv, "instance="+id.String(), `{}`)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	// We don't assert on doneCh here — panicMsg!="" panics before the
	// deferred channel send. Just verify the HTTP response did not crash
	// the server.
}

func TestRouter_RateLimitReturns429(t *testing.T) {
	r, id, _, srv := newTestServer(t)
	defer srv.Close()
	// Burn through the limit (30/window) — 31st request must be rejected.
	for i := 0; i < 30; i++ {
		_ = postBody(srv, "instance="+id.String(), `{}`)
	}
	resp := postBody(srv, "instance="+id.String(), `{}`)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", resp.StatusCode)
	}
	_ = r
}

func TestRouter_UnregisterRemovesInstance(t *testing.T) {
	r, id, _, srv := newTestServer(t)
	defer srv.Close()
	r.UnregisterInstance(id)
	resp := postBody(srv, "instance="+id.String(), `{}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 after unregister", resp.StatusCode)
	}
}

func TestRouter_NoSingletonPerTestIsolation(t *testing.T) {
	a := NewRouter()
	b := NewRouter()
	id := uuid.New()
	a.RegisterInstance(id, newFakeHandler(), uuid.New())
	if _, ok := b.lookup(id); ok {
		t.Error("router b should not see router a's registrations")
	}
}

// recordingHandler captures slog records emitted during a test so we can
// assert on warn-level events without depending on log output formatting.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *recordingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *recordingHandler) countWarn(msgPrefix string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, r := range h.records {
		if r.Level >= slog.LevelWarn && strings.HasPrefix(r.Message, msgPrefix) {
			n++
		}
	}
	return n
}

func swapDefaultLogger(t *testing.T) *recordingHandler {
	t.Helper()
	rec := &recordingHandler{}
	old := slog.Default()
	slog.SetDefault(slog.New(rec))
	t.Cleanup(func() { slog.SetDefault(old) })
	return rec
}

// R3-2: persistent empty ExtractMessageID emits exactly one warn at the
// streak threshold (N=10) and resets so the next 10 trigger another warn.
func TestRouter_EmptyIDStreak_WarnsAtThreshold(t *testing.T) {
	rec := swapDefaultLogger(t)
	_, id, h, srv := newTestServer(t)
	defer srv.Close()
	h.extractedID = "" // every event yields no message_id

	// Send 9 → no warn yet.
	for i := 0; i < 9; i++ {
		_ = postBody(srv, "instance="+id.String(), `{}`)
		waitForDispatch(t, h)
	}
	if got := rec.countWarn("zalo_webhook.empty_message_id_streak"); got != 0 {
		t.Fatalf("warn count after 9 = %d, want 0", got)
	}
	// 10th → exactly one warn.
	_ = postBody(srv, "instance="+id.String(), `{}`)
	waitForDispatch(t, h)
	if got := rec.countWarn("zalo_webhook.empty_message_id_streak"); got != 1 {
		t.Fatalf("warn count after 10 = %d, want 1", got)
	}
	// 11th → counter reset; no second warn yet.
	_ = postBody(srv, "instance="+id.String(), `{}`)
	waitForDispatch(t, h)
	if got := rec.countWarn("zalo_webhook.empty_message_id_streak"); got != 1 {
		t.Fatalf("warn count after 11 = %d, want 1 (counter reset)", got)
	}
}

// Non-empty ID resets the streak.
func TestRouter_EmptyIDStreak_ResetsOnNonEmpty(t *testing.T) {
	rec := swapDefaultLogger(t)
	r := NewRouter()
	id := uuid.New()
	h := newFakeHandler()
	r.RegisterInstance(id, h, uuid.New())
	srv := httptest.NewServer(r)
	defer srv.Close()

	h.extractedID = ""
	for i := 0; i < 5; i++ {
		_ = postBody(srv, "instance="+id.String(), `{}`)
		waitForDispatch(t, h)
	}
	// One non-empty event. Use unique ID per event so dedup short-circuits do not fire.
	h.extractedID = "non-empty-1"
	_ = postBody(srv, "instance="+id.String(), `{}`)
	waitForDispatch(t, h)

	// Then 9 more empty — total empty count is 5+9=14 across the test, but
	// the streak got reset after the non-empty, so we should NOT see a warn.
	h.extractedID = ""
	for i := 0; i < 9; i++ {
		_ = postBody(srv, "instance="+id.String(), `{}`)
		waitForDispatch(t, h)
	}
	if got := rec.countWarn("zalo_webhook.empty_message_id_streak"); got != 0 {
		t.Fatalf("warn count = %d, want 0 (streak should have been reset by non-empty event)", got)
	}
}

// R3-3: Unregister cancels the in-flight handler's ctx so it returns fast.
func TestRouter_UnregisterCancelsInFlightDispatch(t *testing.T) {
	r := NewRouter()
	id := uuid.New()
	started := make(chan struct{})
	finished := make(chan error, 1)
	blockingHandler := &ctxBlockingHandler{started: started, finished: finished}
	r.RegisterInstance(id, blockingHandler, uuid.New())
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp := postBody(srv, "instance="+id.String(), `{}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// Wait for handler to actually be running.
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}

	r.UnregisterInstance(id)

	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("handler returned err = %v, want context.Canceled", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("handler did not exit within 100ms after Unregister")
	}
}

type ctxBlockingHandler struct {
	started  chan struct{}
	finished chan error
}

func (b *ctxBlockingHandler) HandleWebhookEvent(ctx context.Context, _ json.RawMessage) error {
	close(b.started)
	<-ctx.Done()
	b.finished <- ctx.Err()
	return ctx.Err()
}

func (b *ctxBlockingHandler) SignatureVerifier() SignatureVerifier   { return staticVerifier{} }
func (b *ctxBlockingHandler) MessageIDExtractor() MessageIDExtractor { return staticExtractor{id: ""} }

// silence unused-import vigilance during incremental edits.
var _ = errors.New
