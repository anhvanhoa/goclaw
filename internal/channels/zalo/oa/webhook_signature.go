package oa

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
)

// Webhook signature scheme for Zalo OA:
//
//	X-ZEvent-Signature: hex(SHA256(appID + rawBody + timestamp + secret))
//
// `timestamp` comes from the JSON body's top-level timestamp field
// (canonicalized via json.Number → strconv.FormatInt to avoid scientific
// notation drift between client and server signing inputs — S4).

const (
	zaloOASignatureHeader   = "X-ZEvent-Signature"
	defaultReplayWindow     = 5 * time.Minute
	tsMillisecondsThreshold = int64(1e12) // ~year 2001 in ms; below = seconds
)

// SignatureMode controls verifier behavior. Empty/unknown coerces to
// "strict" via normalizeMode so a misconfigured row never lands in
// disabled-by-default (N6/B5+).
type SignatureMode = string

const (
	SignatureModeStrict   SignatureMode = "strict"
	SignatureModeLogOnly  SignatureMode = "log_only"
	SignatureModeDisabled SignatureMode = "disabled"
)

// normalizeMode coerces empty / unknown values to "strict". Called at
// factory time to fail safe.
func normalizeMode(m string) string {
	switch m {
	case SignatureModeStrict, SignatureModeLogOnly, SignatureModeDisabled:
		return m
	default:
		return SignatureModeStrict
	}
}

// computeOASignature derives the expected X-ZEvent-Signature value.
func computeOASignature(appID, body, timestamp, secret string) string {
	h := sha256.New()
	h.Write([]byte(appID))
	h.Write([]byte(body))
	h.Write([]byte(timestamp))
	h.Write([]byte(secret))
	return hex.EncodeToString(h.Sum(nil))
}

// oaSignatureVerifier validates X-ZEvent-Signature with the configured
// app_id + secret. Modes per cfg.WebhookSignatureMode (strict/log_only/disabled).
type oaSignatureVerifier struct {
	appID        string
	secret       string
	mode         SignatureMode
	replayWindow time.Duration
}

func newOASignatureVerifier(appID, secret, mode string, replayWindow time.Duration) *oaSignatureVerifier {
	return &oaSignatureVerifier{
		appID:        appID,
		secret:       secret,
		mode:         normalizeMode(mode),
		replayWindow: replayWindow,
	}
}

func (v *oaSignatureVerifier) Verify(headers http.Header, body []byte) error {
	if v.mode == SignatureModeDisabled {
		slog.Warn("security.zalo_oa_webhook_unsigned_accept", "reason", "signature_mode=disabled")
		return nil
	}
	if v.secret == "" {
		return errors.New("zalo_oa.webhook: secret unset (open webhook is not allowed)")
	}

	tsInt, err := extractTimestamp(body)
	if err != nil {
		return err
	}
	tsStr := strconv.FormatInt(tsInt, 10) // canonical decimal — no scientific notation (S4)

	if rejErr := v.checkReplayWindow(tsInt); rejErr != nil {
		return rejErr
	}

	sig := headers.Get(zaloOASignatureHeader)
	if sig == "" {
		if v.mode == SignatureModeLogOnly {
			slog.Warn("security.zalo_oa_webhook_missing_sig_log_only")
			return nil
		}
		return fmt.Errorf("zalo_oa.webhook: missing %s", zaloOASignatureHeader)
	}
	expected := computeOASignature(v.appID, string(body), tsStr, v.secret)

	// Length precondition: ConstantTimeCompare's len-mismatch path is not
	// documented as constant-time. Reject up front.
	if len(sig) != len(expected) {
		if v.mode == SignatureModeLogOnly {
			slog.Warn("security.zalo_oa_webhook_sig_len_mismatch_log_only",
				"got_len", len(sig), "want_len", len(expected))
			return nil
		}
		return common.ErrSignatureMismatch
	}
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		if v.mode == SignatureModeLogOnly {
			slog.Warn("security.zalo_oa_webhook_sig_mismatch_log_only",
				"got", sig, "want_prefix", expected[:8]+"...")
			return nil
		}
		return common.ErrSignatureMismatch
	}
	return nil
}

// extractTimestamp pulls the top-level `timestamp` field via json.Number so
// scientific-notation values (e.g. 1.7e12 from a misbehaving client) round-
// trip to the same canonical decimal string Zalo signed against (S4).
func extractTimestamp(body []byte) (int64, error) {
	var env struct {
		Timestamp json.Number `json:"timestamp"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return 0, fmt.Errorf("zalo_oa.webhook: decode timestamp: %w", err)
	}
	tsInt, err := env.Timestamp.Int64()
	if err != nil {
		return 0, fmt.Errorf("zalo_oa.webhook: timestamp not integer: %w", err)
	}
	return tsInt, nil
}

// checkReplayWindow rejects events whose timestamp is too far from now.
// Determines unit (ms vs s) by magnitude — Zalo uses milliseconds in
// practice but the older API surface used seconds.
func (v *oaSignatureVerifier) checkReplayWindow(tsInt int64) error {
	if v.replayWindow <= 0 {
		return nil
	}
	var eventTime time.Time
	if tsInt < tsMillisecondsThreshold {
		eventTime = time.Unix(tsInt, 0)
	} else {
		eventTime = time.UnixMilli(tsInt)
	}
	skew := time.Since(eventTime)
	if skew > v.replayWindow || skew < -v.replayWindow {
		err := fmt.Errorf("event timestamp outside replay window: skew=%v, window=±%v", skew, v.replayWindow)
		if v.mode == SignatureModeLogOnly {
			slog.Warn("security.zalo_oa_webhook_replay_log_only", "err", err)
			return nil
		}
		return err
	}
	return nil
}

// clampReplayWindowSeconds clamps the configured window to [60, 3600] and
// substitutes the default (300s) when the value is unset (B7).
func clampReplayWindowSeconds(s int) time.Duration {
	switch {
	case s <= 0:
		return defaultReplayWindow
	case s < 60:
		return 60 * time.Second
	case s > 3600:
		return 3600 * time.Second
	default:
		return time.Duration(s) * time.Second
	}
}
