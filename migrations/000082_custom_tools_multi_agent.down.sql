-- Revert migration 075: restore agent_id column, drop agent_ids.

ALTER TABLE custom_tools ADD COLUMN agent_id UUID REFERENCES agents(id) ON DELETE CASCADE;

-- Restore single-agent assignment where the array had exactly one element.
UPDATE custom_tools
SET agent_id = (agent_ids ->> 0)::uuid
WHERE jsonb_array_length(agent_ids) = 1;

ALTER TABLE custom_tools DROP COLUMN agent_ids;

DROP INDEX IF EXISTS idx_custom_tools_name_tenant;
DROP INDEX IF EXISTS idx_custom_tools_agent_ids;

CREATE UNIQUE INDEX idx_custom_tools_name_tenant_global
  ON custom_tools(tenant_id, name) WHERE agent_id IS NULL;
CREATE UNIQUE INDEX idx_custom_tools_name_tenant_agent
  ON custom_tools(tenant_id, name, agent_id) WHERE agent_id IS NOT NULL;
CREATE INDEX idx_custom_tools_agent
  ON custom_tools(agent_id) WHERE agent_id IS NOT NULL;
