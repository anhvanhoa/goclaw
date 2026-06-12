//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteCustomToolStore implements store.CustomToolStore backed by SQLite.
type SQLiteCustomToolStore struct {
	db     *sql.DB
	encKey string
}

func NewSQLiteCustomToolStore(db *sql.DB, encryptionKey string) *SQLiteCustomToolStore {
	return &SQLiteCustomToolStore{db: db, encKey: encryptionKey}
}

const customToolSelectCols = `id, tenant_id, name, description, parameters, command, working_dir,
 timeout_seconds, agent_id, enabled, created_by, created_at, updated_at`

func (s *SQLiteCustomToolStore) List(ctx context.Context) ([]store.CustomToolDef, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+customToolSelectCols+` FROM custom_tools WHERE tenant_id = ? ORDER BY name`, tenantID.String())
	if err != nil {
		return nil, err
	}
	return s.scanCustomTools(rows)
}

func (s *SQLiteCustomToolStore) ListEnabled(ctx context.Context) ([]store.CustomToolDef, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+customToolSelectCols+` FROM custom_tools WHERE tenant_id = ? AND enabled = 1 ORDER BY name`, tenantID.String())
	if err != nil {
		return nil, err
	}
	return s.scanCustomTools(rows)
}

func (s *SQLiteCustomToolStore) Get(ctx context.Context, id string) (*store.CustomToolDef, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+customToolSelectCols+` FROM custom_tools WHERE id = ? AND tenant_id = ?`, id, tenantID.String())
	def, err := s.scanCustomTool(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrCustomToolNotFound
	}
	return def, err
}

func (s *SQLiteCustomToolStore) Create(ctx context.Context, def store.CustomToolDef, envVars map[string]string) (string, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	id := store.GenNewID().String()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	params := def.Parameters
	if params == nil {
		params = json.RawMessage("{}")
	}

	encEnv, err := encryptEnvVarsSQLite(envVars, s.encKey)
	if err != nil {
		return "", fmt.Errorf("encrypt env: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO custom_tools (id, tenant_id, name, description, parameters, command, working_dir,
		 timeout_seconds, env, agent_id, enabled, created_by, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, tenantID.String(), def.Name, def.Description, string(params), def.Command, def.WorkingDir,
		def.TimeoutSeconds, encEnv, def.AgentID, boolToInt(def.Enabled), def.CreatedBy, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return "", store.ErrCustomToolDuplicateName
		}
		return "", err
	}
	return id, nil
}

func (s *SQLiteCustomToolStore) Update(ctx context.Context, id string, updates map[string]any, envVars map[string]string) error {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	allowed := make(map[string]any)
	for _, col := range []string{"name", "description", "parameters", "command", "working_dir", "timeout_seconds", "agent_id", "enabled"} {
		if v, ok := updates[col]; ok {
			allowed[col] = v
		}
	}
	if len(allowed) == 0 && envVars == nil {
		return nil
	}
	allowed["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)

	if envVars != nil {
		encEnv, err := encryptEnvVarsSQLite(envVars, s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt env: %w", err)
		}
		allowed["env"] = encEnv
	}

	var setClauses []string
	var args []any
	for col, val := range allowed {
		setClauses = append(setClauses, col+" = ?")
		args = append(args, val)
	}
	args = append(args, id, tenantID.String())
	q := fmt.Sprintf("UPDATE custom_tools SET %s WHERE id = ? AND tenant_id = ?", strings.Join(setClauses, ", "))
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return store.ErrCustomToolDuplicateName
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrCustomToolNotFound
	}
	return nil
}

func (s *SQLiteCustomToolStore) Delete(ctx context.Context, id string) error {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM custom_tools WHERE id = ? AND tenant_id = ?`, id, tenantID.String())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrCustomToolNotFound
	}
	return nil
}

func (s *SQLiteCustomToolStore) GetEnv(ctx context.Context, id string) (map[string]string, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	var encEnv []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT env FROM custom_tools WHERE id = ? AND tenant_id = ?`, id, tenantID.String()).Scan(&encEnv)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrCustomToolNotFound
	}
	if err != nil {
		return nil, err
	}
	return decryptEnvVarsSQLite(encEnv, s.encKey)
}

func (s *SQLiteCustomToolStore) scanCustomTool(row *sql.Row) (*store.CustomToolDef, error) {
	var def store.CustomToolDef
	var params string
	var agentID *string
	var tenantIDStr string
	var enabled int
	createdAt, updatedAt := scanTimePair()

	err := row.Scan(
		&def.ID, &tenantIDStr, &def.Name, &def.Description, &params, &def.Command, &def.WorkingDir,
		&def.TimeoutSeconds, &agentID, &enabled, &def.CreatedBy, createdAt, updatedAt,
	)
	if err != nil {
		return nil, err
	}
	def.CreatedAt = createdAt.Time
	def.UpdatedAt = updatedAt.Time
	def.Enabled = enabled != 0
	def.AgentID = agentID
	if tid, err2 := uuid.Parse(tenantIDStr); err2 == nil {
		def.TenantID = tid
	}
	if params != "" {
		def.Parameters = json.RawMessage(params)
	}
	return &def, nil
}

func (s *SQLiteCustomToolStore) scanCustomTools(rows *sql.Rows) ([]store.CustomToolDef, error) {
	defer rows.Close()
	var result []store.CustomToolDef
	for rows.Next() {
		var def store.CustomToolDef
		var params string
		var agentID *string
		var tenantIDStr string
		var enabled int
		createdAt, updatedAt := scanTimePair()

		if err := rows.Scan(
			&def.ID, &tenantIDStr, &def.Name, &def.Description, &params, &def.Command, &def.WorkingDir,
			&def.TimeoutSeconds, &agentID, &enabled, &def.CreatedBy, createdAt, updatedAt,
		); err != nil {
			continue
		}
		def.CreatedAt = createdAt.Time
		def.UpdatedAt = updatedAt.Time
		def.Enabled = enabled != 0
		def.AgentID = agentID
		if tid, err2 := uuid.Parse(tenantIDStr); err2 == nil {
			def.TenantID = tid
		}
		if params != "" {
			def.Parameters = json.RawMessage(params)
		}
		result = append(result, def)
	}
	return result, rows.Err()
}

func encryptEnvVarsSQLite(envVars map[string]string, encKey string) ([]byte, error) {
	if len(envVars) == 0 {
		return nil, nil
	}
	raw, err := json.Marshal(envVars)
	if err != nil {
		return nil, err
	}
	if encKey == "" {
		return raw, nil
	}
	encrypted, err := crypto.Encrypt(string(raw), encKey)
	if err != nil {
		return nil, err
	}
	return []byte(encrypted), nil
}

func decryptEnvVarsSQLite(encEnv []byte, encKey string) (map[string]string, error) {
	if len(encEnv) == 0 {
		return nil, nil
	}
	var raw string
	if encKey != "" && crypto.IsEncrypted(string(encEnv)) {
		var err error
		raw, err = crypto.Decrypt(string(encEnv), encKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt env: %w", err)
		}
	} else {
		raw = string(encEnv)
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("unmarshal env: %w", err)
	}
	return result, nil
}

