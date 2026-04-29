package methods

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ZaloWebhookMethods serves the WS RPC returning the webhook path fragment
// the operator pastes into the Zalo developer console (path-only; operator
// prepends their own externally-reachable host).
type ZaloWebhookMethods struct {
	store store.ChannelInstanceStore
}

func NewZaloWebhookMethods(s store.ChannelInstanceStore) *ZaloWebhookMethods {
	return &ZaloWebhookMethods{store: s}
}

func (m *ZaloWebhookMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodChannelInstancesZaloWebhookURL, m.handleWebhookURL)
}

// handleWebhookURL validates instance ownership + channel type and returns
// {path, instance_id, hint}. Cross-tenant lookups return ErrNotFound to
// avoid leaking instance existence across tenants.
func (m *ZaloWebhookMethods) handleWebhookURL(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
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
	if err != nil || inst.TenantID != client.TenantID() {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgInstanceNotFound)))
		return
	}
	if inst.ChannelType != channels.TypeZaloBot && inst.ChannelType != channels.TypeZaloOA {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgZaloWebhookWrongChannelType)))
		return
	}

	path := fmt.Sprintf("%s?instance=%s", common.WebhookPath, instID)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"path":        path,
		"instance_id": instID.String(),
		"hint":        i18n.T(locale, i18n.MsgZaloWebhookPathHint),
	}))
}
