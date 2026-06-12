-- Custom Tools: user-defined shell commands exposed as agent tools.
-- Recreates the table dropped in migration 027 with tenant_id for multi-tenant support.

CREATE TABLE custom_tools (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE
                    DEFAULT '0193a5b0-7000-7000-8000-000000000001',
    name            VARCHAR(100) NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    parameters      JSONB NOT NULL DEFAULT '{}',
    command         TEXT NOT NULL,
    working_dir     TEXT NOT NULL DEFAULT '',
    timeout_seconds INT NOT NULL DEFAULT 60,
    env             BYTEA,        -- AES-256-GCM encrypted JSON {"KEY":"value"}
    agent_id        UUID REFERENCES agents(id) ON DELETE CASCADE,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    created_by      VARCHAR(255) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Global tools: unique per (tenant, name) when no agent scope
CREATE UNIQUE INDEX idx_custom_tools_name_tenant_global
  ON custom_tools(tenant_id, name) WHERE agent_id IS NULL;

-- Per-agent tools: unique per (tenant, name, agent_id)
CREATE UNIQUE INDEX idx_custom_tools_name_tenant_agent
  ON custom_tools(tenant_id, name, agent_id) WHERE agent_id IS NOT NULL;

CREATE INDEX idx_custom_tools_tenant ON custom_tools(tenant_id);
CREATE INDEX idx_custom_tools_agent ON custom_tools(agent_id) WHERE agent_id IS NOT NULL;
