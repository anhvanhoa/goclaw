package oa

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
)

// oaInboundEvent maps a Zalo OA webhook event. Image/file/sticker
// variants are accepted but ignored (text-only).
type oaInboundEvent struct {
	EventName string `json:"event_name"`
	AppID     string `json:"app_id"`
	OAID      string `json:"oa_id"`
	Timestamp int64  `json:"timestamp"`
	Sender    struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name,omitempty"`
	} `json:"sender"`
	Recipient struct {
		ID string `json:"id"`
	} `json:"recipient"`
	Message struct {
		MessageID string `json:"message_id,omitempty"`
		MsgID     string `json:"msg_id,omitempty"` // alternate field in some OA payloads
		Text      string `json:"text,omitempty"`
	} `json:"message"`
}

func (e *oaInboundEvent) messageID() string {
	if e.Message.MessageID != "" {
		return e.Message.MessageID
	}
	return e.Message.MsgID
}

// HandleWebhookEvent routes a verified+deduped event onto the message bus.
// Drops self-echoes (Sender.ID == OAID) so we don't reply to our own sends.
func (c *Channel) HandleWebhookEvent(_ context.Context, raw json.RawMessage) error {
	var e oaInboundEvent
	if err := json.Unmarshal(raw, &e); err != nil {
		return fmt.Errorf("zalo_oa.webhook: decode event: %w", err)
	}
	if e.Sender.ID != "" && e.Sender.ID == c.creds.OAID {
		slog.Debug("zalo_oa.webhook.self_echo_filtered",
			"oa_id", c.creds.OAID, "message_id", e.messageID())
		return nil
	}

	switch e.EventName {
	case "user_send_text":
		c.dispatchWebhookText(&e)
		return nil
	case "user_send_image", "user_send_file", "user_send_sticker", "user_send_gif":
		slog.Info("zalo_oa.webhook.attachment_received_v1_text_only",
			"event", e.EventName, "message_id", e.messageID())
		return nil
	case "user_follow", "user_unfollow":
		slog.Info("zalo_oa.webhook.follow_event", "event", e.EventName, "user_id", e.Sender.ID)
		return nil
	default:
		slog.Debug("zalo_oa.webhook.unknown_event", "event", e.EventName)
		return nil
	}
}

// dispatchWebhookText forwards a text event via BaseChannel.HandleMessage
// (same downstream path as polling).
func (c *Channel) dispatchWebhookText(e *oaInboundEvent) {
	if e.Message.Text == "" || e.Sender.ID == "" {
		return
	}
	metadata := common.InboundMeta{
		MessageID:         e.messageID(),
		Platform:          common.PlatformZaloOA,
		SenderDisplayName: e.Sender.DisplayName,
	}.ToMap()
	c.BaseChannel.HandleMessage(e.Sender.ID, e.Sender.ID, e.Message.Text, nil, metadata, "direct")
}

// SignatureVerifier returns a verifier bound to this channel's webhook
// secret + signature mode.
func (c *Channel) SignatureVerifier() common.SignatureVerifier {
	return newOASignatureVerifier(
		c.creds.AppID,
		c.cfg.WebhookOASecretKey,
		c.cfg.WebhookSignatureMode,
		clampReplayWindowSeconds(c.cfg.WebhookReplayWindowSeconds),
	)
}

// MessageIDExtractor pulls the per-event id for the router's dedup.
// Empty id → router skips dedup; the streak counter watches for persistent
// emptiness as a schema-drift signal.
func (c *Channel) MessageIDExtractor() common.MessageIDExtractor {
	return oaMessageIDExtractor{}
}

type oaMessageIDExtractor struct{}

func (oaMessageIDExtractor) ExtractMessageID(raw json.RawMessage) string {
	var probe struct {
		Message struct {
			MessageID string `json:"message_id,omitempty"`
			MsgID     string `json:"msg_id,omitempty"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return ""
	}
	if probe.Message.MessageID != "" {
		return probe.Message.MessageID
	}
	return probe.Message.MsgID
}
