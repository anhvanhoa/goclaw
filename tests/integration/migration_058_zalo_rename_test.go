//go:build integration

package integration

import (
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestMigration58_RenameZaloChannelTypes_RoundTrip verifies the down/up
// behavior of migration 000058 on PG: legacy 'zalo_oa' → 'zalo_bot' and
// transient 'zalo_oauth' → 'zalo_oa'. Run on an isolated database so the
// shared test state isn't disturbed.
func TestMigration58_RenameZaloChannelTypes_RoundTrip(t *testing.T) {
	baseDSN := os.Getenv("TEST_DATABASE_URL")
	if baseDSN == "" {
		baseDSN = defaultTestDSN
	}

	admin, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Skipf("PG not available: %v", err)
	}
	defer admin.Close()
	if err := admin.Ping(); err != nil {
		t.Skipf("PG not reachable: %v", err)
	}

	dbName := "mig58_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	if _, err := admin.Exec("CREATE DATABASE " + dbName); err != nil {
		t.Fatalf("create db: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + dbName + " WITH (FORCE)")
	})

	parsed, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	parsed.Path = "/" + dbName
	isolatedDSN := parsed.String()

	m, err := migrate.New("file://../../migrations", isolatedDSN)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	t.Cleanup(func() { m.Close() })

	if err := m.Migrate(57); err != nil {
		t.Fatalf("migrate to 57: %v", err)
	}

	db, err := sql.Open("pgx", isolatedDSN)
	if err != nil {
		t.Fatalf("open isolated: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	tenantID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES ($1, 'mig-test', 'mt', 'active')`,
		tenantID,
	); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	agentID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES ($1, $2, $3, 'predefined', 'active', 'test', 'test-model', 'test-owner')`,
		agentID, tenantID, "a-"+agentID.String()[:8],
	); err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	legacyOA := uuid.New()
	transientOAuth := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO channel_instances (id, tenant_id, name, channel_type, agent_id) VALUES
		   ($1, $4, 'mig58-legacy-oa', 'zalo_oa', $3),
		   ($2, $4, 'mig58-transient-oauth', 'zalo_oauth', $3)`,
		legacyOA, transientOAuth, agentID, tenantID,
	); err != nil {
		t.Fatalf("seed channel_instances: %v", err)
	}

	if err := m.Migrate(58); err != nil {
		t.Fatalf("migrate up to 58: %v", err)
	}
	assertChannelType(t, db, legacyOA, "zalo_bot")
	assertChannelType(t, db, transientOAuth, "zalo_oa")

	if err := m.Migrate(57); err != nil {
		t.Fatalf("migrate down to 57: %v", err)
	}
	assertChannelType(t, db, legacyOA, "zalo_oa")
	assertChannelType(t, db, transientOAuth, "zalo_oauth")

	if err := m.Migrate(58); err != nil {
		t.Fatalf("migrate up to 58 again: %v", err)
	}
	assertChannelType(t, db, legacyOA, "zalo_bot")
	assertChannelType(t, db, transientOAuth, "zalo_oa")
}

func assertChannelType(t *testing.T, db *sql.DB, id uuid.UUID, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(
		`SELECT channel_type FROM channel_instances WHERE id = $1`, id,
	).Scan(&got); err != nil {
		t.Fatalf("query channel_type for %s: %v", id, err)
	}
	if got != want {
		t.Errorf("channel_type for %s = %q, want %q", id, got, want)
	}
}
