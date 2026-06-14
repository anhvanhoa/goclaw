import { useState, useEffect, useCallback } from "react";
import { useWs } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";

export interface Workstation {
  id: string;
  workstationKey: string;
  tenantId: string;
  name: string;
  backendType: "ssh" | "docker";
  defaultCwd: string;
  active: boolean;
  createdAt: string;
  updatedAt: string;
  createdBy: string;
  metadataSummary?: Record<string, unknown>;
  agentLinks: AgentWorkstationLink[];
}

export interface AgentWorkstationLink {
  agentId: string;
  workstationId: string;
  tenantId: string;
  isDefault: boolean;
  createdAt: string;
}

export interface WorkstationPermission {
  id: string;
  workstationId: string;
  tenantId: string;
  pattern: string;
  enabled: boolean;
  createdBy: string;
  createdAt: string;
}

export interface CreateWorkstationParams {
  workstationKey: string;
  name: string;
  backendType: "ssh" | "docker";
  metadata?: Record<string, unknown>;
}

export interface UpdateWorkstationParams {
  name?: string;
  active?: boolean;
  metadata?: Record<string, unknown>;
}

type RawAgentWorkstationLink = Partial<AgentWorkstationLink> & {
  agent_id?: string;
  workstation_id?: string;
  tenant_id?: string;
  is_default?: boolean;
  created_at?: string;
};

type RawWorkstation = Partial<Workstation> & {
  workstation_key?: string;
  tenant_id?: string;
  backend_type?: "ssh" | "docker";
  default_cwd?: string;
  created_at?: string;
  updated_at?: string;
  created_by?: string;
  metadata_summary?: Record<string, unknown>;
  agent_links?: RawAgentWorkstationLink[];
};

function normalizeLink(raw: RawAgentWorkstationLink): AgentWorkstationLink {
  return {
    agentId: raw.agentId ?? raw.agent_id ?? "",
    workstationId: raw.workstationId ?? raw.workstation_id ?? "",
    tenantId: raw.tenantId ?? raw.tenant_id ?? "",
    isDefault: raw.isDefault ?? raw.is_default ?? false,
    createdAt: raw.createdAt ?? raw.created_at ?? "",
  };
}

function normalizeWorkstation(raw: RawWorkstation): Workstation {
  const agentLinks = raw.agentLinks ?? raw.agent_links ?? [];
  return {
    id: raw.id ?? "",
    workstationKey: raw.workstationKey ?? raw.workstation_key ?? "",
    tenantId: raw.tenantId ?? raw.tenant_id ?? "",
    name: raw.name ?? "",
    backendType: raw.backendType ?? raw.backend_type ?? "ssh",
    defaultCwd: raw.defaultCwd ?? raw.default_cwd ?? "",
    active: raw.active ?? false,
    createdAt: raw.createdAt ?? raw.created_at ?? "",
    updatedAt: raw.updatedAt ?? raw.updated_at ?? "",
    createdBy: raw.createdBy ?? raw.created_by ?? "",
    metadataSummary: raw.metadataSummary ?? raw.metadata_summary,
    agentLinks: agentLinks.map(normalizeLink),
  };
}

export function useWorkstations() {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const [workstations, setWorkstations] = useState<Workstation[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!connected) return;
    setLoading(true);
    setError(null);
    try {
      const res = await ws.call<{ workstations: RawWorkstation[] }>(Methods.WORKSTATIONS_LIST);
      setWorkstations((res.workstations ?? []).map(normalizeWorkstation));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load workstations");
    } finally {
      setLoading(false);
    }
  }, [ws, connected]);

  useEffect(() => {
    load();
  }, [load]);

  const createWorkstation = useCallback(
    async (params: CreateWorkstationParams): Promise<Workstation> => {
      const res = await ws.call<{ workstation: RawWorkstation }>(Methods.WORKSTATIONS_CREATE, params as unknown as Record<string, unknown>);
      await load();
      return normalizeWorkstation(res.workstation);
    },
    [ws, load],
  );

  const updateWorkstation = useCallback(
    async (id: string, params: UpdateWorkstationParams): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_UPDATE, { id, updates: params });
      await load();
    },
    [ws, load],
  );

  const linkAgent = useCallback(
    async (workstationId: string, agentId: string, isDefault: boolean): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_LINK_AGENT, { workstationId, agentId, isDefault });
      await load();
    },
    [ws, load],
  );

  const unlinkAgent = useCallback(
    async (workstationId: string, agentId: string): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_UNLINK_AGENT, { workstationId, agentId });
      await load();
    },
    [ws, load],
  );

  const deleteWorkstation = useCallback(
    async (id: string): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_DELETE, { id });
      await load();
    },
    [ws, load],
  );

  return {
    workstations,
    loading,
    error,
    refresh: load,
    createWorkstation,
    updateWorkstation,
    deleteWorkstation,
    linkAgent,
    unlinkAgent,
  };
}

type RawWorkstationPermission = Partial<WorkstationPermission> & {
  workstation_id?: string;
  tenant_id?: string;
  created_by?: string;
  created_at?: string;
};

function normalizePermission(raw: RawWorkstationPermission): WorkstationPermission {
  return {
    id: raw.id ?? "",
    workstationId: raw.workstationId ?? raw.workstation_id ?? "",
    tenantId: raw.tenantId ?? raw.tenant_id ?? "",
    pattern: raw.pattern ?? "",
    enabled: raw.enabled ?? false,
    createdBy: raw.createdBy ?? raw.created_by ?? "",
    createdAt: raw.createdAt ?? raw.created_at ?? "",
  };
}

export function useWorkstationPermissions(workstationId: string) {
  const ws = useWs();
  const connected = useAuthStore((s) => s.connected);
  const [permissions, setPermissions] = useState<WorkstationPermission[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async () => {
    if (!connected || !workstationId) return;
    setLoading(true);
    try {
      const res = await ws.call<{ permissions: RawWorkstationPermission[] }>(
        Methods.WORKSTATIONS_PERMS_LIST,
        { workstationId },
      );
      setPermissions((res.permissions ?? []).map(normalizePermission));
    } finally {
      setLoading(false);
    }
  }, [ws, connected, workstationId]);

  useEffect(() => {
    load();
  }, [load]);

  const addPermission = useCallback(
    async (pattern: string): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_PERMS_ADD, { workstationId, pattern });
      await load();
    },
    [ws, workstationId, load],
  );

  const removePermission = useCallback(
    async (id: string): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_PERMS_REMOVE, { id });
      await load();
    },
    [ws, load],
  );

  const togglePermission = useCallback(
    async (id: string, enabled: boolean): Promise<void> => {
      await ws.call(Methods.WORKSTATIONS_PERMS_TOGGLE, { id, enabled });
      await load();
    },
    [ws, load],
  );

  return { permissions, loading, refresh: load, addPermission, removePermission, togglePermission };
}
