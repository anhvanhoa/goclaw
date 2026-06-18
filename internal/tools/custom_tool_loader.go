package tools

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// LoadCustomTools fetches all enabled custom tools for the current tenant context,
// decrypts their env vars, and registers each as a CustomTool in the registry.
// Called once at gateway startup after the tool registry is initialized.
func LoadCustomTools(ctx context.Context, s store.CustomToolStore, encKey string, reg *Registry) error {
	if s == nil {
		return nil
	}
	defs, err := s.ListEnabled(ctx)
	if err != nil {
		return err
	}

	registered := 0
	for _, def := range defs {
		envVars, err := s.GetEnv(ctx, def.ID)
		if err != nil {
			slog.Warn("custom_tools: failed to load env", "tool", def.Name, "id", def.ID, "error", err)
			envVars = nil
		}

		var params map[string]any
		if len(def.Parameters) > 0 {
			if err2 := json.Unmarshal(def.Parameters, &params); err2 != nil {
				slog.Warn("custom_tools: invalid parameters JSON", "tool", def.Name, "error", err2)
				params = map[string]any{"type": "object", "properties": map[string]any{}}
			}
		}

		ct := NewCustomTool(def.Name, def.Description, params, def.Command, def.WorkingDir, def.TimeoutSeconds, envVars)
		reg.RegisterWithMetadata(ct, ToolMetadata{
			Capabilities:    []ToolCapability{CapMutating},
			AllowedAgentIDs: def.AgentIDs,
		})
		registered++
		slog.Info("custom_tool registered", "name", def.Name, "id", def.ID)
	}
	if registered > 0 {
		slog.Info("custom_tools loaded", "count", registered)
	}
	return nil
}
