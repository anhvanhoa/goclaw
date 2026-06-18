package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrCustomToolNotFound      = errors.New("custom tool not found")
	ErrCustomToolDuplicateName = errors.New("custom tool name already exists")
)

// CustomToolDef represents a user-defined shell command exposed as an agent tool.
// The env field is never populated on reads — env vars are write-only (encrypted at rest).
type CustomToolDef struct {
	ID             string          `json:"id"`
	TenantID       uuid.UUID       `json:"tenantId"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	Parameters     json.RawMessage `json:"parameters"` // JSON Schema object
	Command        string          `json:"command"`
	WorkingDir     string          `json:"workingDir"`
	TimeoutSeconds int             `json:"timeoutSeconds"`
	AgentIDs       []string        `json:"agentIds"` // empty = global (available to all agents)
	Enabled        bool            `json:"enabled"`
	CreatedBy      string          `json:"createdBy"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

// CustomToolStore manages user-defined custom tool definitions.
// Env vars are accepted on write as map[string]string and encrypted before storage.
// They are never returned on read — consumers must use GetEnv explicitly if needed.
type CustomToolStore interface {
	// List returns all custom tools for the current tenant context.
	List(ctx context.Context) ([]CustomToolDef, error)
	// ListEnabled returns only enabled custom tools for the current tenant.
	ListEnabled(ctx context.Context) ([]CustomToolDef, error)
	// Get returns a single custom tool by UUID id.
	Get(ctx context.Context, id string) (*CustomToolDef, error)
	// Create inserts a new custom tool. envVars is encrypted before storage.
	// Returns the new tool's UUID.
	Create(ctx context.Context, def CustomToolDef, envVars map[string]string) (string, error)
	// Update applies allowed field updates. envVars replaces existing encrypted env; nil = no change.
	Update(ctx context.Context, id string, updates map[string]any, envVars map[string]string) error
	// Delete removes a custom tool by id.
	Delete(ctx context.Context, id string) error
	// GetEnv decrypts and returns env vars for a tool (used by the tool executor at startup).
	GetEnv(ctx context.Context, id string) (map[string]string, error)
	// ListEnvKeys returns the env var key names for a tool (never values — write-only secret model).
	ListEnvKeys(ctx context.Context, id string) ([]string, error)
}
