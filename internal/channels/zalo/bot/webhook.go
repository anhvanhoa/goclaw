package bot

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
)

// HandleWebhookEvent unwraps the {ok, result} envelope per
// bot.zapps.me/docs/apis/webhook; polling already strips it inside
// callAPIWith.
func (c *Channel) HandleWebhookEvent(_ context.Context, raw json.RawMessage) error {
	if c.inBootstrap() {
		n := c.bootstrapDroppedCount.Add(1)
		// Cap warn-level at first hit so a guessed slug can't amplify logs.
		if n == 1 {
			slog.Warn("zalo_bot.webhook.bootstrap_drop",
				"instance_id", c.instanceID,
				"drop_count", n,
				"hint", "paste Webhook Secret in Credentials tab to enable processing")
		} else {
			slog.Debug("zalo_bot.webhook.bootstrap_drop",
				"instance_id", c.instanceID, "drop_count", n)
		}
		return nil
	}

	var wrap zaloAPIResponse
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return fmt.Errorf("zalo_bot.webhook: decode envelope: %w", err)
	}
	if !wrap.OK || len(wrap.Result) == 0 {
		return nil
	}
	var u zaloUpdate
	if err := json.Unmarshal(wrap.Result, &u); err != nil {
		return fmt.Errorf("zalo_bot.webhook: decode result: %w", err)
	}

	c.processUpdate(u)
	return nil
}

// SignatureVerifier returns a header-token verifier bound to the
// channel's webhook_secret. Bootstrap returns a no-op so the setWebhook
// URL-save ping gets 200; events are dropped in HandleWebhookEvent.
func (c *Channel) SignatureVerifier() common.SignatureVerifier {
	if c.inBootstrap() {
		return bootstrapVerifier{}
	}
	return botSignatureVerifier{secret: c.webhookSecret}
}

// MessageIDExtractor reads message_id for router dedup.
func (c *Channel) MessageIDExtractor() common.MessageIDExtractor {
	return botMessageIDExtractor{}
}

// botSignatureVerifier compares X-Bot-Api-Secret-Token in constant time.
// Empty secret is rejected up front — ConstantTimeCompare returns 1 when
// both inputs are empty.
type botSignatureVerifier struct {
	secret string
}

func (v botSignatureVerifier) Verify(h http.Header, _ []byte) error {
	if v.secret == "" {
		return errors.New("zalo_bot.webhook: secret unset")
	}
	got := h.Get("X-Bot-Api-Secret-Token")
	if got == "" {
		return errors.New("zalo_bot.webhook: missing X-Bot-Api-Secret-Token")
	}
	// Reject length mismatch up front; ConstantTimeCompare's len path
	// isn't documented as constant-time.
	if len(got) != len(v.secret) {
		return common.ErrSignatureMismatch
	}
	if subtle.ConstantTimeCompare([]byte(got), []byte(v.secret)) != 1 {
		return common.ErrSignatureMismatch
	}
	return nil
}

type bootstrapVerifier struct{}

func (bootstrapVerifier) Verify(http.Header, []byte) error { return nil }

type botMessageIDExtractor struct{}

func (botMessageIDExtractor) ExtractMessageID(raw json.RawMessage) string {
	var probe struct {
		Result struct {
			Message struct {
				MessageID string `json:"message_id"`
			} `json:"message"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	return probe.Result.Message.MessageID
}
