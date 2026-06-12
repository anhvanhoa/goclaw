package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/permissions"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

var customToolNameRe = regexp.MustCompile(`^[a-z0-9_]+$`)

// CustomToolsHandler handles CRUD for user-defined custom tool definitions.
type CustomToolsHandler struct {
	store  store.CustomToolStore
	encKey string
	msgBus *bus.MessageBus
	reg    *tools.Registry
}

func NewCustomToolsHandler(s store.CustomToolStore, encKey string, msgBus *bus.MessageBus, reg *tools.Registry) *CustomToolsHandler {
	return &CustomToolsHandler{store: s, encKey: encKey, msgBus: msgBus, reg: reg}
}

// reRegisterTool fetches decrypted env vars from the store and registers (or re-registers)
// the tool in the in-memory registry so running agents see the latest definition.
func (h *CustomToolsHandler) reRegisterTool(ctx context.Context, def *store.CustomToolDef) {
	if h.reg == nil {
		return
	}
	envVars, _ := h.store.GetEnv(ctx, def.ID)
	var params map[string]any
	if len(def.Parameters) > 0 {
		json.Unmarshal(def.Parameters, &params) //nolint:errcheck
	}
	ct := tools.NewCustomTool(def.Name, def.Description, params, def.Command, def.WorkingDir, def.TimeoutSeconds, envVars)
	h.reg.Register(ct)
}

func (h *CustomToolsHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/tools/custom", requireAuth("", h.handleList))
	mux.HandleFunc("POST /v1/tools/custom", requireAuth(permissions.RoleAdmin, h.handleCreate))
	mux.HandleFunc("GET /v1/tools/custom/{id}", requireAuth("", h.handleGet))
	mux.HandleFunc("GET /v1/tools/custom/{id}/env", requireAuth(permissions.RoleAdmin, h.handleGetEnv))
	mux.HandleFunc("PUT /v1/tools/custom/{id}", requireAuth(permissions.RoleAdmin, h.handleUpdate))
	mux.HandleFunc("DELETE /v1/tools/custom/{id}", requireAuth(permissions.RoleAdmin, h.handleDelete))
}

func (h *CustomToolsHandler) handleList(w http.ResponseWriter, r *http.Request) {
	tools, err := h.store.List(r.Context())
	if err != nil {
		slog.Error("custom_tools.list", "error", err)
		locale := extractLocale(r)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToList, "custom tools")})
		return
	}
	if tools == nil {
		tools = []store.CustomToolDef{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": tools})
}

func (h *CustomToolsHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "id")})
		return
	}
	def, err := h.store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrCustomToolNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "custom tool", id)})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, def)
}

func (h *CustomToolsHandler) handleGetEnv(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "id")})
		return
	}
	envVars, err := h.store.GetEnv(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrCustomToolNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "custom tool", id)})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if envVars == nil {
		envVars = map[string]string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"env": envVars})
}

// createCustomToolRequest is the request body for POST /v1/tools/custom.
type createCustomToolRequest struct {
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Parameters     json.RawMessage   `json:"parameters"`
	Command        string            `json:"command"`
	WorkingDir     string            `json:"workingDir"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	AgentIDs       []string          `json:"agentIds,omitempty"`
	Enabled        *bool             `json:"enabled,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
}

func (h *CustomToolsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	if !requireMasterScope(w, r) {
		return
	}
	locale := extractLocale(r)

	var req createCustomToolRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	if err := validateCustomToolRequest(locale, req.Name, req.Command, req.TimeoutSeconds, req.Parameters, req.Env); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 60
	}

	userID := extractUserID(r)
	def := store.CustomToolDef{
		Name:           req.Name,
		Description:    req.Description,
		Parameters:     req.Parameters,
		Command:        req.Command,
		WorkingDir:     req.WorkingDir,
		TimeoutSeconds: req.TimeoutSeconds,
		AgentIDs:       req.AgentIDs,
		Enabled:        enabled,
		CreatedBy:      userID,
	}

	id, err := h.store.Create(r.Context(), def, req.Env)
	if err != nil {
		if errors.Is(err, store.ErrCustomToolDuplicateName) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgAlreadyExists, "custom tool", req.Name)})
			return
		}
		slog.Error("custom_tools.create", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": i18n.T(locale, i18n.MsgFailedToCreate, "custom tool", err.Error())})
		return
	}

	// Sync in-memory tool registry so agents see the new tool immediately.
	if enabled && h.reg != nil {
		if created, err2 := h.store.Get(r.Context(), id); err2 == nil {
			h.reRegisterTool(r.Context(), created)
		}
	}

	emitAudit(h.msgBus, r, "custom_tool.created", "custom_tool", req.Name)
	writeJSON(w, http.StatusCreated, map[string]string{"id": id, "status": "created"})
}

// updateCustomToolRequest is the request body for PUT /v1/tools/custom/{id}.
type updateCustomToolRequest struct {
	Name           *string           `json:"name,omitempty"`
	Description    *string           `json:"description,omitempty"`
	Parameters     json.RawMessage   `json:"parameters,omitempty"`
	Command        *string           `json:"command,omitempty"`
	WorkingDir     *string           `json:"workingDir,omitempty"`
	TimeoutSeconds *int              `json:"timeoutSeconds,omitempty"`
	AgentIDs       []string          `json:"agentIds,omitempty"`
	Enabled        *bool             `json:"enabled,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
}

func (h *CustomToolsHandler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if !requireMasterScope(w, r) {
		return
	}
	locale := extractLocale(r)
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "id")})
		return
	}

	var req updateCustomToolRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}

	updates := make(map[string]any)
	if req.Name != nil {
		if err := validateCustomToolName(locale, *req.Name); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		updates["name"] = *req.Name
	}
	if req.Description != nil {
		updates["description"] = *req.Description
	}
	if req.Command != nil {
		if *req.Command == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "command")})
			return
		}
		updates["command"] = *req.Command
	}
	if req.WorkingDir != nil {
		updates["working_dir"] = *req.WorkingDir
	}
	if req.TimeoutSeconds != nil {
		if *req.TimeoutSeconds < 1 || *req.TimeoutSeconds > 3600 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "timeoutSeconds must be between 1 and 3600"})
			return
		}
		updates["timeout_seconds"] = *req.TimeoutSeconds
	}
	if req.AgentIDs != nil {
		agentIDsJSON, _ := json.Marshal(req.AgentIDs)
		updates["agent_ids"] = agentIDsJSON
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.Parameters != nil {
		if !isValidSettingsJSON(req.Parameters) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "parameters must be a JSON object"})
			return
		}
		updates["parameters"] = []byte(req.Parameters)
	}
	if len(req.Env) > 0 {
		rejectedKeys, valErr := crypto.ValidateGrantEnvVars(req.Env)
		if valErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": valErr.Error()})
			return
		}
		if len(rejectedKeys) > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "env contains denied keys: " + joinStrings(rejectedKeys)})
			return
		}
	}

	if len(updates) == 0 && req.Env == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidUpdates)})
		return
	}

	var envToStore map[string]string
	if req.Env != nil {
		envToStore = req.Env
	}

	if err := h.store.Update(r.Context(), id, updates, envToStore); err != nil {
		if errors.Is(err, store.ErrCustomToolNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "custom tool", id)})
			return
		}
		if errors.Is(err, store.ErrCustomToolDuplicateName) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": i18n.T(locale, i18n.MsgAlreadyExists, "custom tool", "")})
			return
		}
		slog.Error("custom_tools.update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Sync in-memory registry: re-register if enabled, unregister if disabled.
	if h.reg != nil {
		if updated, err2 := h.store.Get(r.Context(), id); err2 == nil {
			if updated.Enabled {
				h.reRegisterTool(r.Context(), updated)
			} else {
				h.reg.Unregister(updated.Name)
			}
		}
	}

	emitAudit(h.msgBus, r, "custom_tool.updated", "custom_tool", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (h *CustomToolsHandler) handleDelete(w http.ResponseWriter, r *http.Request) {
	if !requireMasterScope(w, r) {
		return
	}
	locale := extractLocale(r)
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgRequired, "id")})
		return
	}

	// Capture tool name before deletion so we can unregister from in-memory registry.
	var toolName string
	if h.reg != nil {
		if existing, err2 := h.store.Get(r.Context(), id); err2 == nil {
			toolName = existing.Name
		}
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrCustomToolNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "custom tool", id)})
			return
		}
		slog.Error("custom_tools.delete", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if toolName != "" {
		h.reg.Unregister(toolName)
	}

	emitAudit(h.msgBus, r, "custom_tool.deleted", "custom_tool", id)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func validateCustomToolRequest(locale, name, command string, timeoutSeconds int, parameters json.RawMessage, env map[string]string) error {
	if err := validateCustomToolName(locale, name); err != nil {
		return err
	}
	if command == "" {
		return errorf(i18n.T(locale, i18n.MsgRequired, "command"))
	}
	if timeoutSeconds != 0 && (timeoutSeconds < 1 || timeoutSeconds > 3600) {
		return errorf("timeoutSeconds must be between 1 and 3600")
	}
	if parameters != nil && !isValidSettingsJSON(parameters) {
		return errorf("parameters must be a JSON object")
	}
	if len(env) > 0 {
		rejectedKeys, valErr := crypto.ValidateGrantEnvVars(env)
		if valErr != nil {
			return valErr
		}
		if len(rejectedKeys) > 0 {
			return errorf("env contains denied keys: " + joinStrings(rejectedKeys))
		}
	}
	return nil
}

func validateCustomToolName(locale, name string) error {
	if name == "" {
		return errorf(i18n.T(locale, i18n.MsgRequired, "name"))
	}
	if len(name) > 100 {
		return errorf("name must be at most 100 characters")
	}
	if !customToolNameRe.MatchString(name) {
		return errorf("name must contain only lowercase letters, digits, and underscores")
	}
	return nil
}

func errorf(msg string) error {
	return errors.New(msg)
}

func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}
