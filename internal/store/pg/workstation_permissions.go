package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGWorkstationPermissionStore implements store.WorkstationPermissionStore backed by PostgreSQL.
type PGWorkstationPermissionStore struct {
	db *sql.DB
}

// NewPGWorkstationPermissionStore creates a PGWorkstationPermissionStore.
func NewPGWorkstationPermissionStore(db *sql.DB) *PGWorkstationPermissionStore {
	return &PGWorkstationPermissionStore{db: db}
}

const wpSelectCols = `id, workstation_id, tenant_id, pattern, enabled, created_by, created_at`

func (s *PGWorkstationPermissionStore) ListForWorkstation(ctx context.Context, workstationID uuid.UUID) ([]store.WorkstationPermission, error) {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+wpSelectCols+` FROM workstation_permissions
		 WHERE workstation_id = $1 AND tenant_id = $2
		 ORDER BY created_at`,
		workstationID, tid)
	if err != nil {
		return nil, fmt.Errorf("workstation_permissions list: %w", err)
	}
	return scanPermRows(rows)
}

func (s *PGWorkstationPermissionStore) Add(ctx context.Context, perm *store.WorkstationPermission) error {
	if perm.ID == uuid.Nil {
		perm.ID = store.GenNewID()
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	perm.TenantID = tid
	if perm.CreatedAt.IsZero() {
		perm.CreatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO workstation_permissions
		 (id, workstation_id, tenant_id, pattern, enabled, created_by, created_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (workstation_id, pattern) DO NOTHING`,
		perm.ID, perm.WorkstationID, tid, perm.Pattern,
		perm.Enabled, perm.CreatedBy, perm.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("workstation_permissions add: %w", err)
	}
	return nil
}

func (s *PGWorkstationPermissionStore) Remove(ctx context.Context, id uuid.UUID) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM workstation_permissions WHERE id = $1 AND tenant_id = $2`, id, tid)
	if err != nil {
		return fmt.Errorf("workstation_permissions remove: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *PGWorkstationPermissionStore) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) error {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE workstation_permissions SET enabled = $1 WHERE id = $2 AND tenant_id = $3`,
		enabled, id, tid)
	return err
}

// SeedDefaults inserts default safe binary names for a new workstation.
// Must be called inside the same transaction as workstation creation (H5 fix).
// Uses ON CONFLICT DO NOTHING — safe to call multiple times.
func (s *PGWorkstationPermissionStore) SeedDefaults(ctx context.Context, workstationID, tenantID uuid.UUID) error {
	for _, pattern := range store.DefaultAllowedBinaries {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO workstation_permissions
			 (id, workstation_id, tenant_id, pattern, enabled, created_by, created_at)
			 VALUES ($1,$2,$3,$4,TRUE,'system',NOW())
			 ON CONFLICT (workstation_id, pattern) DO NOTHING`,
			store.GenNewID(), workstationID, tenantID, pattern,
		)
		if err != nil {
			return fmt.Errorf("seed default permission %q: %w", pattern, err)
		}
	}
	return nil
}

// BackfillDefaults seeds default binary allowlist entries for all workstations
// that currently have zero permission entries. Uses a single CTE+INSERT for efficiency.
// Returns the count of workstations that were backfilled. Safe to call at startup.
func (s *PGWorkstationPermissionStore) BackfillDefaults(ctx context.Context) (int, error) {
	// Build VALUES list for the default binaries.
	// Hardcoded to avoid dependency on store.DefaultAllowedBinaries inside SQL template.
	rows, err := s.db.QueryContext(ctx, `
		WITH needs_seed AS (
			SELECT w.id AS workstation_id, w.tenant_id
			FROM workstations w
			WHERE NOT EXISTS (
				SELECT 1 FROM workstation_permissions wp WHERE wp.workstation_id = w.id
			)
		)
		INSERT INTO workstation_permissions
			(id, workstation_id, tenant_id, pattern, enabled, created_by, created_at)
		SELECT gen_random_uuid(), ns.workstation_id, ns.tenant_id, p.pattern, TRUE, 'system', NOW()
		FROM needs_seed ns
		CROSS JOIN (VALUES
			('echo'),('pwd'),('ls'),('cat'),('git'),
			('whoami'),('hostname'),('date'),('uname'),('claude')
		) AS p(pattern)
		ON CONFLICT (workstation_id, pattern) DO NOTHING
		RETURNING workstation_id
	`)
	if err != nil {
		return 0, fmt.Errorf("workstation_permissions backfill: %w", err)
	}
	defer rows.Close()

	seen := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, fmt.Errorf("workstation_permissions backfill scan: %w", err)
		}
		seen[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("workstation_permissions backfill rows: %w", err)
	}
	return len(seen), nil
}

func scanPermRows(rows *sql.Rows) ([]store.WorkstationPermission, error) {
	defer rows.Close()
	var result []store.WorkstationPermission
	for rows.Next() {
		p, err := scanPermRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func scanPermRow(s interface {
	Scan(...any) error
}) (store.WorkstationPermission, error) {
	var p store.WorkstationPermission
	err := s.Scan(&p.ID, &p.WorkstationID, &p.TenantID, &p.Pattern, &p.Enabled, &p.CreatedBy, &p.CreatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("scan workstation_permission: %w", err)
	}
	return p, nil
}
