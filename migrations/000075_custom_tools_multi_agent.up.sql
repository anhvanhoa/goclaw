-- Migration 075: replace single agent_id with agent_ids JSONB array on custom_tools.
-- Allows a tool to be assigned to multiple agents (empty array = global).

ALTER TABLE custom_tools ADD COLUMN agent_ids JSONB NOT NULL DEFAULT '[]';

-- Migrate existing single-agent assignments into the new array column.
UPDATE custom_tools
SET agent_ids = jsonb_build_array(agent_id::text)
WHERE agent_id IS NOT NULL;

ALTER TABLE custom_tools DROP COLUMN agent_id;

-- Replace the two conditional unique indexes with one simple unique index.
DROP INDEX IF EXISTS idx_custom_tools_name_tenant_global;
DROP INDEX IF EXISTS idx_custom_tools_name_tenant_agent;
CREATE UNIQUE INDEX idx_custom_tools_name_tenant ON custom_tools(tenant_id, name);

-- GIN index for future membership queries (e.g., tools assigned to a given agent).
CREATE INDEX idx_custom_tools_agent_ids ON custom_tools USING gin(agent_ids)
  WHERE agent_ids != '[]'::jsonb;
