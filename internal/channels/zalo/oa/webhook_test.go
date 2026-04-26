package oa

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	"github.com/nextlevelbuilder/goclaw/internal/config"
)

// newWebhookChannel builds an OA channel ready for webhook tests with a
// known app/secret/oa-id and the given sig mode + replay window.
func newWebhookChannel(t *testing.T, secret, mode string, replaySecs int) (*Channel, *bus.MessageBus) {
	t.Helper()
	creds := &ChannelCreds{
		AppID:     "app-1",
		SecretKey: "oauth-key", // distinct from webhook secret (S7)
		OAID:      "oa-1",
	}
	cfg := config.ZaloOAConfig{
		Transport:                  "webhook",
		WebhookOASecretKey:         secret,
		WebhookSignatureMode:       mode,
		WebhookReplayWindowSeconds: replaySecs,
	}
	mb := bus.New()
	c, err := New("webhook_test", cfg, creds, &fakeStore{}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	return c, mb
}

// signedPayload builds a body whose top-level timestamp + signature header
// are computed against (appID, body, ts, secret) per the OA scheme.
// Uses Header.Set so the canonical key matches verifier's Get lookup.
func signedPayload(t *testing.T, appID, secret string, ts int64, body string) (http.Header, []byte) {
	t.Helper()
	full := fmt.Sprintf(`{"timestamp":%d,%s}`, ts, body)
	tsStr := fmt.Sprintf("%d", ts)
	sig := computeOASignature(appID, full, tsStr, secret)
	h := http.Header{}
	h.Set(zaloOASignatureHeader, sig)
	return h, []byte(full)
}

// nowMs is the canonical millisecond timestamp used by Zalo OA payloads.
func nowMs() int64 { return time.Now().UnixMilli() }

// ---------- signature scheme + verifier ----------

func TestComputeOASignature_FixedFixture(t *testing.T) {
	t.Parallel()
	// Fixed input → known output. Verify with:
	//   echo -n 'XBODY1234567890Y' | shasum -a 256
	sig := computeOASignature("X", "BODY", "1234567890", "Y")
	const want = "2f1ef5aabe67e8396a459ca89562e108ad541f82ba5022c85f645bd6b7220cb9"
	if sig != want {
		t.Fatalf("sig = %q, want %q", sig, want)
	}
}

func TestNormalizeMode(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":         "strict",
		"strict":   "strict",
		"log_only": "log_only",
		"disabled": "disabled",
		"weird":    "strict",
	}
	for in, want := range cases {
		if got := normalizeMode(in); got != want {
			t.Errorf("normalizeMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClampReplayWindowSeconds(t *testing.T) {
	t.Parallel()
	cases := map[int]time.Duration{
		0:     5 * time.Minute,    // unset → default
		-5:    5 * time.Minute,    // negative → default
		30:    60 * time.Second,   // below floor
		120:   120 * time.Second,  // in range
		3600:  3600 * time.Second, // at ceiling
		10000: 3600 * time.Second, // above ceiling
	}
	for in, want := range cases {
		if got := clampReplayWindowSeconds(in); got != want {
			t.Errorf("clampReplayWindowSeconds(%d) = %v, want %v", in, got, want)
		}
	}
}

func TestVerifier_AcceptsValidSignature(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", time.Hour)
	hdr, body := signedPayload(t, "app-1", "secret", nowMs(), `"event_name":"x"`)
	if err := v.Verify(hdr, body); err != nil {
		t.Errorf("Verify: %v", err)
	}
}

func TestVerifier_RejectsMissingHeader(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", time.Hour)
	body := []byte(fmt.Sprintf(`{"timestamp":%d}`, nowMs()))
	if err := v.Verify(http.Header{}, body); err == nil || !strings.Contains(err.Error(), "missing X-ZEvent-Signature") {
		t.Errorf("Verify(no header) err = %v, want missing-header", err)
	}
}

func TestVerifier_RejectsLengthMismatch(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", time.Hour)
	body := []byte(fmt.Sprintf(`{"timestamp":%d}`, nowMs()))
	hdr := http.Header{}
	hdr.Set(zaloOASignatureHeader, "deadbeef") // shorter than 64-char hex
	err := v.Verify(hdr, body)
	if !errors.Is(err, common.ErrSignatureMismatch) {
		t.Errorf("Verify(short sig) err = %v, want ErrSignatureMismatch", err)
	}
}

func TestVerifier_RejectsWrongSignature(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", time.Hour)
	body := []byte(fmt.Sprintf(`{"timestamp":%d}`, nowMs()))
	wrong := strings.Repeat("a", 64) // valid hex length, wrong value
	hdr := http.Header{}
	hdr.Set(zaloOASignatureHeader, wrong)
	err := v.Verify(hdr, body)
	if !errors.Is(err, common.ErrSignatureMismatch) {
		t.Errorf("Verify(wrong sig) err = %v, want ErrSignatureMismatch", err)
	}
}

func TestVerifier_RejectsEmptySecretInStrict(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "", "strict", time.Hour)
	body := []byte(fmt.Sprintf(`{"timestamp":%d}`, nowMs()))
	if err := v.Verify(http.Header{}, body); err == nil || !strings.Contains(err.Error(), "secret unset") {
		t.Errorf("Verify err = %v, want secret-unset", err)
	}
}

// B5: log_only mode swallows mismatches but still accepts (return nil).
func TestVerifier_LogOnlyAcceptsMismatch(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "log_only", time.Hour)
	body := []byte(fmt.Sprintf(`{"timestamp":%d}`, nowMs()))
	hdr := http.Header{}
	hdr.Set(zaloOASignatureHeader, strings.Repeat("a", 64))
	if err := v.Verify(hdr, body); err != nil {
		t.Errorf("log_only Verify(wrong sig) err = %v, want nil", err)
	}
}

// B5/N6: disabled mode skips verification entirely (still warns once).
func TestVerifier_DisabledAcceptsAnything(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "", "disabled", time.Hour)
	if err := v.Verify(http.Header{}, []byte(`{"x":1}`)); err != nil {
		t.Errorf("disabled Verify err = %v, want nil", err)
	}
}

// B7: replay window in strict mode rejects out-of-window timestamps.
func TestVerifier_RejectsReplay(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", 5*time.Minute)
	old := nowMs() - int64((10 * time.Minute).Milliseconds())
	hdr, body := signedPayload(t, "app-1", "secret", old, `"event_name":"x"`)
	err := v.Verify(hdr, body)
	if err == nil || !strings.Contains(err.Error(), "replay window") {
		t.Errorf("Verify(replay) err = %v, want replay-window error", err)
	}
}

func TestVerifier_AcceptsWithinReplayWindow(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", 5*time.Minute)
	recent := nowMs() - int64((1 * time.Minute).Milliseconds())
	hdr, body := signedPayload(t, "app-1", "secret", recent, `"event_name":"x"`)
	if err := v.Verify(hdr, body); err != nil {
		t.Errorf("Verify(within window) err = %v, want nil", err)
	}
}

// S4: timestamp parsed via json.Number → strconv.FormatInt produces the
// canonical decimal Zalo signs against. The verifier hashes the
// canonical form, not the raw JSON bytes.
func TestVerifier_TimestampCanonicalizedViaInt64(t *testing.T) {
	t.Parallel()
	v := newOASignatureVerifier("app-1", "secret", "strict", time.Hour)
	tsInt := nowMs()
	body := []byte(fmt.Sprintf(`{"timestamp":%d,"event_name":"x"}`, tsInt))
	tsStr := fmt.Sprintf("%d", tsInt)
	sig := computeOASignature("app-1", string(body), tsStr, "secret")
	hdr := http.Header{}
	hdr.Set(zaloOASignatureHeader, sig)
	if err := v.Verify(hdr, body); err != nil {
		t.Errorf("Verify(canonical ts) err = %v", err)
	}

	// Also verify extractTimestamp handles json.Number happily (covers the
	// internal canonicalization path even if the body is well-formed int).
	got, err := extractTimestamp(body)
	if err != nil {
		t.Fatalf("extractTimestamp: %v", err)
	}
	if got != tsInt {
		t.Errorf("extractTimestamp = %d, want %d", got, tsInt)
	}
}

// ---------- HandleWebhookEvent dispatch ----------

func TestHandleWebhookEvent_DispatchesText(t *testing.T) {
	t.Parallel()
	ch, mb := newWebhookChannel(t, "secret", "strict", 0)
	payload := `{"event_name":"user_send_text","sender":{"id":"alice","display_name":"Alice"},"message":{"message_id":"m1","text":"hello"}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("no inbound published")
	}
	if got.Content != "hello" {
		t.Errorf("Content = %q", got.Content)
	}
	if got.SenderID != "alice" || got.ChatID != "alice" {
		t.Errorf("sender/chat = %q/%q, want alice/alice", got.SenderID, got.ChatID)
	}
	if got.Metadata["message_id"] != "m1" {
		t.Errorf("metadata.message_id = %q", got.Metadata["message_id"])
	}
}

// A8: sender == OAID is the bot's own outbound — must drop, not forward.
func TestHandleWebhookEvent_FiltersSelfEcho(t *testing.T) {
	t.Parallel()
	ch, mb := newWebhookChannel(t, "secret", "strict", 0)
	payload := `{"event_name":"user_send_text","sender":{"id":"oa-1"},"message":{"message_id":"m1","text":"loop"}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); ok {
		t.Error("self-echo should not have published")
	}
}

func TestHandleWebhookEvent_AttachmentSkippedV1(t *testing.T) {
	t.Parallel()
	ch, mb := newWebhookChannel(t, "secret", "strict", 0)
	payload := `{"event_name":"user_send_image","sender":{"id":"alice"},"message":{"message_id":"m9"}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); ok {
		t.Error("attachment should be log-and-skip in v1")
	}
}

func TestHandleWebhookEvent_UnknownEventNoError(t *testing.T) {
	t.Parallel()
	ch, _ := newWebhookChannel(t, "secret", "strict", 0)
	payload := `{"event_name":"some_future_thing","sender":{"id":"alice"}}`
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(payload)); err != nil {
		t.Errorf("unknown event should not error: %v", err)
	}
}

func TestHandleWebhookEvent_BadJSONReturnsError(t *testing.T) {
	t.Parallel()
	ch, _ := newWebhookChannel(t, "secret", "strict", 0)
	if err := ch.HandleWebhookEvent(context.Background(), json.RawMessage(`not-json`)); err == nil {
		t.Error("bad JSON must return error")
	}
}

func TestMessageIDExtractor(t *testing.T) {
	t.Parallel()
	e := oaMessageIDExtractor{}
	if got := e.ExtractMessageID(json.RawMessage(`{"message":{"message_id":"m1"}}`)); got != "m1" {
		t.Errorf("ExtractMessageID(message_id) = %q", got)
	}
	if got := e.ExtractMessageID(json.RawMessage(`{"message":{"msg_id":"m2"}}`)); got != "m2" {
		t.Errorf("ExtractMessageID(msg_id fallback) = %q", got)
	}
	if e.ExtractMessageID(json.RawMessage(`{}`)) != "" {
		t.Error("missing → empty")
	}
	if e.ExtractMessageID(json.RawMessage(`not-json`)) != "" {
		t.Error("invalid JSON → empty (no panic)")
	}
}

// Start with transport=webhook + missing secret → MarkFailed (not crash).
func TestStart_WebhookMissingSecretMarksFailed(t *testing.T) {
	t.Parallel()
	creds := &ChannelCreds{AppID: "app-1", SecretKey: "k", OAID: "oa-1"}
	cfg := config.ZaloOAConfig{
		Transport:            "webhook",
		WebhookSignatureMode: "strict",
		// no WebhookOASecretKey
	}
	mb := bus.New()
	c, err := New("start_test", cfg, creds, &fakeStore{}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	c.webhookRouter = common.NewRouter()

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	snap := c.HealthSnapshot()
	if !strings.Contains(strings.ToLower(string(snap.State)), "failed") {
		t.Errorf("State = %v, want failed", snap.State)
	}
	_ = c.Stop(context.Background())
}

// Start with transport=webhook + secret → registers with router; Stop unregisters.
func TestStart_WebhookRegistersAndStopUnregisters(t *testing.T) {
	t.Parallel()
	creds := &ChannelCreds{AppID: "app-1", SecretKey: "k", OAID: "oa-1"}
	cfg := config.ZaloOAConfig{
		Transport:          "webhook",
		WebhookOASecretKey: "secret",
	}
	mb := bus.New()
	c, err := New("start_test", cfg, creds, &fakeStore{}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	id := uuid.New()
	c.SetInstanceID(id)
	router := common.NewRouter()
	c.webhookRouter = router

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !c.IsRunning() {
		t.Error("channel not Running after Start")
	}
	// Confirm registered: dispatch a request through the router and assert
	// the channel's HandleWebhookEvent runs.
	srv := httptest.NewServer(router)
	defer srv.Close()
	hdr, body := signedPayload(t, "app-1", "secret", nowMs(),
		`"event_name":"user_send_text","sender":{"id":"alice"},"message":{"message_id":"m1","text":"hi"}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"?instance="+id.String(), bytes.NewReader(body))
	req.Header = hdr
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("router post: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); !ok {
		t.Fatal("router did not deliver event to channel handler")
	}

	// Stop unregisters → next request must 404.
	_ = c.Stop(context.Background())
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+"?instance="+id.String(), bytes.NewReader(body))
	req2.Header = hdr
	resp2, err := srv.Client().Do(req2)
	if err != nil {
		t.Fatalf("router post 2: %v", err)
	}
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("after Stop: status = %d, want 404", resp2.StatusCode)
	}
}

// Start polling (default) leaves the webhook router untouched.
func TestStart_PollingTransportIgnoresRouter(t *testing.T) {
	t.Parallel()
	creds := &ChannelCreds{AppID: "app-1", SecretKey: "k", OAID: "oa-1", AccessToken: "AT", RefreshToken: "RT", ExpiresAt: time.Now().Add(time.Hour)}
	cfg := config.ZaloOAConfig{} // Transport empty → defaults to polling
	mb := bus.New()
	c, err := New("start_test", cfg, creds, &fakeStore{}, mb, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.SetInstanceID(uuid.New())
	router := common.NewRouter()
	c.webhookRouter = router

	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Stop(context.Background())
	if !c.IsRunning() {
		t.Error("polling channel not Running")
	}
}

// S7: SignatureVerifier() must be wired to cfg.WebhookOASecretKey, NOT
// creds.SecretKey (the OAuth refresh credential). Verifying against the
// OAuth secret would silently reject every legit Zalo webhook delivery.
func TestSignatureVerifier_UsesWebhookSecretNotOAuthSecret(t *testing.T) {
	t.Parallel()
	ch, _ := newWebhookChannel(t, "WEBHOOK-SECRET", "strict", 0)
	ts := nowMs()
	hdr, body := signedPayload(t, "app-1", "WEBHOOK-SECRET", ts, `"event_name":"user_send_text"`)
	if err := ch.SignatureVerifier().Verify(hdr, body); err != nil {
		t.Errorf("verifier rejected webhook-secret payload: %v (S7: must wire WebhookOASecretKey, not creds.SecretKey)", err)
	}
	// Sanity: the OAuth secret should NOT verify.
	hdr2, body2 := signedPayload(t, "app-1", "oauth-key", ts, `"event_name":"user_send_text"`)
	if err := ch.SignatureVerifier().Verify(hdr2, body2); err == nil {
		t.Error("OAuth-secret-signed payload accepted — verifier wired to wrong field")
	}
}
