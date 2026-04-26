// Package bot implements the Zalo Bot channel (static-token variant,
// distinct from the OAuth-backed Official Account in ../oa).
// Ported from OpenClaw TS extensions/zalo/.
//
// Zalo Bot API: https://bot-api.zaloplatforms.com
// DM only (no groups), text limit 2000 chars, polling + webhook modes.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	maxTextLength     = 2000
	defaultMediaMaxMB = 5
	pairingDebounce   = 60 * time.Second
)

// Channel connects to the Zalo OA Bot API.
type Channel struct {
	*channels.BaseChannel
	token      string
	dmPolicy   string
	mediaMaxMB int
	blockReply *bool
	stopCh     chan struct{}
	client     *http.Client
	pollClient *http.Client
	// pairingService, pairingDebounce are inherited from channels.BaseChannel.
}

// New creates a new Zalo channel.
func New(cfg config.ZaloConfig, msgBus *bus.MessageBus, pairingSvc store.PairingStore) (*Channel, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("zalo token is required")
	}

	base := channels.NewBaseChannel("zalo", msgBus, cfg.AllowFrom)
	base.ValidatePolicy(cfg.DMPolicy, "")

	dmPolicy := cfg.DMPolicy
	if dmPolicy == "" {
		dmPolicy = "pairing" // TS default
	}

	mediaMax := cfg.MediaMaxMB
	if mediaMax <= 0 {
		mediaMax = defaultMediaMaxMB
	}

	ch := &Channel{
		BaseChannel: base,
		token:       cfg.Token,
		dmPolicy:    dmPolicy,
		mediaMaxMB:  mediaMax,
		blockReply:  cfg.BlockReply,
		stopCh:      make(chan struct{}),
		client:      &http.Client{Timeout: 60 * time.Second},
		pollClient:  &http.Client{Timeout: 0},
	}
	ch.SetPairingService(pairingSvc)
	return ch, nil
}

// BlockReplyEnabled returns the per-channel block_reply override (nil = inherit gateway default).
func (c *Channel) BlockReplyEnabled() *bool { return c.blockReply }

// Start begins polling for Zalo updates.
func (c *Channel) Start(ctx context.Context) error {
	slog.Info("starting zalo bot (polling mode)")

	// Validate token
	info, err := c.getMe()
	if err != nil {
		return fmt.Errorf("zalo getMe failed: %w", err)
	}
	slog.Info("zalo bot connected", "bot_id", info.ID, "bot_name", info.Name)

	c.SetRunning(true)

	go c.pollLoop(ctx)

	return nil
}

// Stop shuts down the Zalo bot.
func (c *Channel) Stop(_ context.Context) error {
	slog.Info("stopping zalo bot")
	close(c.stopCh)
	c.SetRunning(false)
	return nil
}

// Send delivers an outbound message to a Zalo chat.
func (c *Channel) Send(_ context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("zalo bot not running")
	}

	// Strip markdown — Zalo does not support any markup rendering.
	msg.Content = StripMarkdown(msg.Content)

	// Check for media in content (URL-based photo sending)
	if strings.Contains(msg.Content, "[photo:") {
		// Extract photo URL from "[photo:URL]" pattern
		if start := strings.Index(msg.Content, "[photo:"); start >= 0 {
			end := strings.Index(msg.Content[start:], "]")
			if end > 0 {
				photoURL := msg.Content[start+7 : start+end]
				caption := strings.TrimSpace(msg.Content[:start] + msg.Content[start+end+1:])
				return c.sendPhoto(msg.ChatID, photoURL, caption)
			}
		}
	}

	// Send as text, chunking if over 2000 chars
	return c.sendChunkedText(msg.ChatID, msg.Content)
}

