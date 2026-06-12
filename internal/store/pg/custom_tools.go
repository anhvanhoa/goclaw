package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/crypto"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGCustomToolStore implements store.CustomToolStore backed by Postgres.
type PGCustomToolStore struct {
	db     *sql.DB
	encKey string
}

func NewPGCustomToolStore(db *sql.DB, encryptionKey string) *PGCustomToolStore {
	return &PGCustomToolStore{db: db, encKey: encryptionKey}
}

const customToolSelectCols = `id, tenant_id, name, description, parameters, command, working_dir,
 timeout_seconds, agent_id, enabled, created_by, created_at, updated_at`

func (s *PGCustomToolStore) List(ctx context.Context) ([]store.CustomToolDef, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+customToolSelectCols+` FROM custom_tools WHERE tenant_id = $1 ORDER BY name`, tenantID)
	if err != nil {
		return nil, err
	}
	return scanCustomTools(rows)
}

func (s *PGCustomToolStore) ListEnabled(ctx context.Context) ([]store.CustomToolDef, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+customToolSelectCols+` FROM custom_tools WHERE tenant_id = $1 AND enabled = true ORDER BY name`, tenantID)
	if err != nil {
		return nil, err
	}
	return scanCustomTools(rows)
}

func (s *PGCustomToolStore) Get(ctx context.Context, id string) (*store.CustomToolDef, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+customToolSelectCols+` FROM custom_tools WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	def, err := scanCustomTool(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrCustomToolNotFound
	}
	return def, err
}

func (s *PGCustomToolStore) Create(ctx context.Context, def store.CustomToolDef, envVars map[string]string) (string, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}

	id := store.GenNewID().String()
	now := time.Now()

	params := def.Parameters
	if params == nil {
		params = json.RawMessage("{}")
	}

	encEnv, err := encryptEnvVars(envVars, s.encKey)
	if err != nil {
		return "", fmt.Errorf("encrypt env: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO custom_tools (id, tenant_id, name, description, parameters, command, working_dir,
		 timeout_seconds, env, agent_id, enabled, created_by, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		id, tenantID, def.Name, def.Description, []byte(params), def.Command, def.WorkingDir,
		def.TimeoutSeconds, encEnv, def.AgentID, def.Enabled, def.CreatedBy, now, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return "", store.ErrCustomToolDuplicateName
		}
		return "", err
	}
	return id, nil
}

func (s *PGCustomToolStore) Update(ctx context.Context, id string, updates map[string]any, envVars map[string]string) error {
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
	allowed["updated_at"] = time.Now()

	// Encrypt env if caller provided new env vars
	if envVars != nil {
		encEnv, err := encryptEnvVars(envVars, s.encKey)
		if err != nil {
			return fmt.Errorf("encrypt env: %w", err)
		}
		allowed["env"] = encEnv
	}

	var setClauses []string
	var args []any
	i := 1
	for col, val := range allowed {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", col, i))
		args = append(args, val)
		i++
	}
	args = append(args, id, tenantID)
	q := fmt.Sprintf(
		"UPDATE custom_tools SET %s WHERE id = $%d AND tenant_id = $%d",
		strings.Join(setClauses, ", "), i, i+1,
	)
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		if isUniqueViolation(err) {
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

func (s *PGCustomToolStore) Delete(ctx context.Context, id string) error {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM custom_tools WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrCustomToolNotFound
	}
	return nil
}

func (s *PGCustomToolStore) GetEnv(ctx context.Context, id string) (map[string]string, error) {
	tenantID := store.TenantIDFromContext(ctx)
	if tenantID == uuid.Nil {
		tenantID = store.MasterTenantID
	}
	var encEnv []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT env FROM custom_tools WHERE id = $1 AND tenant_id = $2`, id, tenantID).Scan(&encEnv)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrCustomToolNotFound
	}
	if err != nil {
		return nil, err
	}
	return decryptEnvVars(encEnv, s.encKey)
}

func scanCustomTool(row *sql.Row) (*store.CustomToolDef, error) {
	var def store.CustomToolDef
	var params []byte
	var agentID *string
	err := row.Scan(
		&def.ID, &def.TenantID, &def.Name, &def.Description, &params, &def.Command, &def.WorkingDir,
		&def.TimeoutSeconds, &agentID, &def.Enabled, &def.CreatedBy, &def.CreatedAt, &def.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	def.AgentID = agentID
	if params != nil {
		def.Parameters = json.RawMessage(params)
	}
	return &def, nil
}

func scanCustomTools(rows *sql.Rows) ([]store.CustomToolDef, error) {
	defer rows.Close()
	var result []store.CustomToolDef
	for rows.Next() {
		var def store.CustomToolDef
		var params []byte
		var agentID *string
		if err := rows.Scan(
			&def.ID, &def.TenantID, &def.Name, &def.Description, &params, &def.Command, &def.WorkingDir,
			&def.TimeoutSeconds, &agentID, &def.Enabled, &def.CreatedBy, &def.CreatedAt, &def.UpdatedAt,
		); err != nil {
			continue
		}
		def.AgentID = agentID
		if params != nil {
			def.Parameters = json.RawMessage(params)
		}
		result = append(result, def)
	}
	return result, rows.Err()
}

// encryptEnvVars marshals envVars to JSON then encrypts with AES-256-GCM.
// Returns nil when envVars is nil or empty.
func encryptEnvVars(envVars map[string]string, encKey string) ([]byte, error) {
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

// decryptEnvVars decrypts encrypted env bytes and unmarshals to map[string]string.
func decryptEnvVars(encEnv []byte, encKey string) (map[string]string, error) {
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

func isUniqueViolation(err error) bool {
	var pgErr *pq.Error
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
