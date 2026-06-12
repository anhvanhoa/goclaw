package methods

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// CustomToolsMethods handles custom_tools.list/create/update/delete/toggle via WebSocket RPC.
type CustomToolsMethods struct {
	store    store.CustomToolStore
	encKey   string
	eventBus bus.EventPublisher
	cfg      *config.Config
}

func NewCustomToolsMethods(s store.CustomToolStore, encKey string, eventBus bus.EventPublisher, cfg *config.Config) *CustomToolsMethods {
	return &CustomToolsMethods{store: s, encKey: encKey, eventBus: eventBus, cfg: cfg}
}

func (m *CustomToolsMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodCustomToolsList, m.handleList)
	router.Register(protocol.MethodCustomToolsCreate, m.handleCreate)
	router.Register(protocol.MethodCustomToolsUpdate, m.handleUpdate)
	router.Register(protocol.MethodCustomToolsDelete, m.handleDelete)
	router.Register(protocol.MethodCustomToolsToggle, m.handleToggle)
}

func (m *CustomToolsMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	tools, err := m.store.List(ctx)
	if err != nil {
		slog.Error("custom_tools.list rpc", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}
	if tools == nil {
		tools = []store.CustomToolDef{}
	}
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"tools": tools}))
}

func (m *CustomToolsMethods) handleCreate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "admin role required")))
		return
	}

	var params struct {
		Name           string            `json:"name"`
		Description    string            `json:"description"`
		Parameters     json.RawMessage   `json:"parameters"`
		Command        string            `json:"command"`
		WorkingDir     string            `json:"workingDir"`
		TimeoutSeconds int               `json:"timeoutSeconds"`
		AgentID        *string           `json:"agentId,omitempty"`
		Enabled        *bool             `json:"enabled,omitempty"`
		Env            map[string]string `json:"env,omitempty"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}

	if params.Name == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "name")))
		return
	}
	if params.Command == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "command")))
		return
	}

	enabled := true
	if params.Enabled != nil {
		enabled = *params.Enabled
	}
	if params.TimeoutSeconds <= 0 {
		params.TimeoutSeconds = 60
	}

	def := store.CustomToolDef{
		Name:           params.Name,
		Description:    params.Description,
		Parameters:     params.Parameters,
		Command:        params.Command,
		WorkingDir:     params.WorkingDir,
		TimeoutSeconds: params.TimeoutSeconds,
		AgentID:        params.AgentID,
		Enabled:        enabled,
		CreatedBy:      client.UserID(),
	}

	id, err := m.store.Create(ctx, def, params.Env)
	if err != nil {
		if errors.Is(err, store.ErrCustomToolDuplicateName) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgAlreadyExists, "custom tool", params.Name)))
			return
		}
		slog.Error("custom_tools.create rpc", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	emitAudit(m.eventBus, client, "custom_tool.created", "custom_tool", params.Name)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"id": id, "status": "created"}))
}

func (m *CustomToolsMethods) handleUpdate(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "admin role required")))
		return
	}

	var params struct {
		ID             string            `json:"id"`
		Name           *string           `json:"name,omitempty"`
		Description    *string           `json:"description,omitempty"`
		Parameters     json.RawMessage   `json:"parameters,omitempty"`
		Command        *string           `json:"command,omitempty"`
		WorkingDir     *string           `json:"workingDir,omitempty"`
		TimeoutSeconds *int              `json:"timeoutSeconds,omitempty"`
		AgentID        *string           `json:"agentId,omitempty"`
		Enabled        *bool             `json:"enabled,omitempty"`
		Env            map[string]string `json:"env,omitempty"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.ID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "id")))
		return
	}

	updates := make(map[string]any)
	if params.Name != nil {
		updates["name"] = *params.Name
	}
	if params.Description != nil {
		updates["description"] = *params.Description
	}
	if params.Command != nil {
		updates["command"] = *params.Command
	}
	if params.WorkingDir != nil {
		updates["working_dir"] = *params.WorkingDir
	}
	if params.TimeoutSeconds != nil {
		updates["timeout_seconds"] = *params.TimeoutSeconds
	}
	if params.AgentID != nil {
		updates["agent_id"] = *params.AgentID
	}
	if params.Enabled != nil {
		updates["enabled"] = *params.Enabled
	}
	if params.Parameters != nil {
		updates["parameters"] = []byte(params.Parameters)
	}

	if err := m.store.Update(ctx, params.ID, updates, params.Env); err != nil {
		if errors.Is(err, store.ErrCustomToolNotFound) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "custom tool", params.ID)))
			return
		}
		if errors.Is(err, store.ErrCustomToolDuplicateName) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgAlreadyExists, "custom tool", "")))
			return
		}
		slog.Error("custom_tools.update rpc", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	emitAudit(m.eventBus, client, "custom_tool.updated", "custom_tool", params.ID)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"status": "updated"}))
}

func (m *CustomToolsMethods) handleDelete(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "admin role required")))
		return
	}

	var params struct {
		ID string `json:"id"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.ID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "id")))
		return
	}

	if err := m.store.Delete(ctx, params.ID); err != nil {
		if errors.Is(err, store.ErrCustomToolNotFound) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "custom tool", params.ID)))
			return
		}
		slog.Error("custom_tools.delete rpc", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	emitAudit(m.eventBus, client, "custom_tool.deleted", "custom_tool", params.ID)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"status": "deleted"}))
}

func (m *CustomToolsMethods) handleToggle(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	locale := store.LocaleFromContext(ctx)
	if !permissions.HasMinRole(client.Role(), permissions.RoleAdmin) {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgPermissionDenied, "admin role required")))
		return
	}

	var params struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if req.Params != nil {
		json.Unmarshal(req.Params, &params)
	}
	if params.ID == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgRequired, "id")))
		return
	}

	updates := map[string]any{"enabled": params.Enabled}
	if err := m.store.Update(ctx, params.ID, updates, nil); err != nil {
		if errors.Is(err, store.ErrCustomToolNotFound) {
			client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "custom tool", params.ID)))
			return
		}
		slog.Error("custom_tools.toggle rpc", "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	emitAudit(m.eventBus, client, "custom_tool.toggled", "custom_tool", params.ID)
	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{"status": "updated", "enabled": params.Enabled}))
}
