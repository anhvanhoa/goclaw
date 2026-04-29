package oa

import (
	"context"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// startWebhookTransport registers with the shared router and optionally
// fires the catch-up sweep. Returns nil on misconfig (channel is marked
// Failed) so instance_loader doesn't crash.
func (c *Channel) startWebhookTransport() error {
	mode := normalizeMode(c.cfg.WebhookSignatureMode)
	if c.cfg.WebhookOASecretKey == "" && mode != SignatureModeDisabled {
		c.MarkFailed("webhook secret missing",
			"transport=webhook with signature_mode=strict|log_only requires webhook_oa_secret_key",
			channels.ChannelFailureKindConfig, false)
		return nil
	}
	c.webhookRouter.RegisterInstance(c.instanceID, c, c.TenantID())
	slog.Info("zalo_oa.webhook.registered",
		"instance_id", c.instanceID, "oa_id", c.creds.OAID, "signature_mode", mode)

	if c.cfg.CatchUpOnRestart {
		// Goroutine + WaitGroup so Start returns immediately and Stop drains.
		c.catchUpWG.Add(1)
		go c.runCatchUpSweepGoroutine()
	}
	c.MarkHealthy("webhook")
	return nil
}

// runCatchUpSweepGoroutine runs runCatchUpSweep with stopCh-aware cancel.
func (c *Channel) runCatchUpSweepGoroutine() {
	defer c.catchUpWG.Done()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-c.stopCh:
			cancel()
		case <-done:
		}
	}()
	c.runCatchUpSweep(ctx)
}
