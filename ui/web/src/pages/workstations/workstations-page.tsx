import { Fragment, useMemo, useState } from "react";
import { MonitorCog, Plus, RefreshCw, Trash2, ChevronDown, ChevronRight, Link2, Unlink, Star, ShieldCheck } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { formatDate } from "@/lib/format";
import { useWorkstations, useWorkstationPermissions, type Workstation } from "./hooks/use-workstations";
import { WorkstationCreateDialog } from "./workstation-create-dialog";
import { WorkstationActivityTab } from "./workstation-activity-tab";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import type { AgentData } from "@/types/agent";

function formatOptionalDate(value: string): string {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return formatDate(date);
}

function agentLabel(agent: AgentData): string {
  return agent.display_name || agent.agent_key || agent.id;
}

function WorkstationOverviewTab({ workstation }: { workstation: Workstation }) {
  const { t } = useTranslation("workstations");
  const metadataEntries = Object.entries(workstation.metadataSummary ?? {});

  return (
    <div className="grid grid-cols-1 gap-3 p-3 text-sm sm:grid-cols-2 lg:grid-cols-3">
      <div>
        <p className="text-xs text-muted-foreground">{t("details.id")}</p>
        <p className="mt-1 break-all font-mono text-xs">{workstation.id}</p>
      </div>
      <div>
        <p className="text-xs text-muted-foreground">{t("columns.key")}</p>
        <p className="mt-1 font-mono text-xs">{workstation.workstationKey || "—"}</p>
      </div>
      <div>
        <p className="text-xs text-muted-foreground">{t("columns.backend")}</p>
        <p className="mt-1">{t(`backend.${workstation.backendType}`)}</p>
      </div>
      <div>
        <p className="text-xs text-muted-foreground">{t("details.defaultCwd")}</p>
        <p className="mt-1 font-mono text-xs">{workstation.defaultCwd || "—"}</p>
      </div>
      <div>
        <p className="text-xs text-muted-foreground">{t("columns.created")}</p>
        <p className="mt-1">{formatOptionalDate(workstation.createdAt)}</p>
      </div>
      <div>
        <p className="text-xs text-muted-foreground">{t("details.updated")}</p>
        <p className="mt-1">{formatOptionalDate(workstation.updatedAt)}</p>
      </div>
      {metadataEntries.map(([key, value]) => (
        <div key={key}>
          <p className="text-xs text-muted-foreground">{key}</p>
          <p className="mt-1 break-all font-mono text-xs">{String(value)}</p>
        </div>
      ))}
    </div>
  );
}

interface WorkstationAgentsTabProps {
  workstation: Workstation;
  agents: AgentData[];
  onLinkAgent: (workstationId: string, agentId: string, isDefault: boolean) => Promise<void>;
  onUnlinkAgent: (workstationId: string, agentId: string) => Promise<void>;
}

function WorkstationAgentsTab({ workstation, agents, onLinkAgent, onUnlinkAgent }: WorkstationAgentsTabProps) {
  const { t } = useTranslation("workstations");
  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [makeDefault, setMakeDefault] = useState(true);
  const [saving, setSaving] = useState(false);

  const linkedAgentIds = useMemo(
    () => new Set(workstation.agentLinks.map((link) => link.agentId)),
    [workstation.agentLinks],
  );
  const linkedAgents = agents.filter((agent) => linkedAgentIds.has(agent.id));
  const availableAgents = agents.filter((agent) => !linkedAgentIds.has(agent.id));

  async function handleLink() {
    if (!selectedAgentId) return;
    setSaving(true);
    try {
      await onLinkAgent(workstation.id, selectedAgentId, makeDefault);
      setSelectedAgentId("");
    } finally {
      setSaving(false);
    }
  }

  async function handleUnlink(agentId: string) {
    setSaving(true);
    try {
      await onUnlinkAgent(workstation.id, agentId);
    } finally {
      setSaving(false);
    }
  }

  async function handleSetDefault(agentId: string) {
    setSaving(true);
    try {
      await onLinkAgent(workstation.id, agentId, true);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-4 p-3">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-end">
        <div className="grid flex-1 gap-1.5">
          <p className="text-xs font-medium text-muted-foreground">{t("agents.selectLabel")}</p>
          <Select value={selectedAgentId} onValueChange={setSelectedAgentId} disabled={availableAgents.length === 0 || saving}>
            <SelectTrigger className="w-full text-base md:text-sm">
              <SelectValue placeholder={t("agents.selectPlaceholder")} />
            </SelectTrigger>
            <SelectContent>
              {availableAgents.map((agent) => (
                <SelectItem key={agent.id} value={agent.id}>
                  {agentLabel(agent)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <label className="flex min-h-9 items-center gap-2 text-sm">
          <Switch checked={makeDefault} onCheckedChange={setMakeDefault} disabled={saving} />
          {t("agents.makeDefault")}
        </label>
        <Button size="sm" className="gap-1" onClick={handleLink} disabled={!selectedAgentId || saving}>
          <Link2 className="h-3.5 w-3.5" />
          {t("agents.allow")}
        </Button>
      </div>

      {linkedAgents.length === 0 ? (
        <div className="py-8 text-center text-sm text-muted-foreground">
          {t("agents.empty")}
        </div>
      ) : (
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full min-w-[560px] text-sm">
            <thead className="border-b bg-muted/50">
              <tr>
                <th className="px-3 py-2 text-left font-medium">{t("agents.columns.agent")}</th>
                <th className="px-3 py-2 text-left font-medium">{t("agents.columns.status")}</th>
                <th className="px-3 py-2 text-right font-medium">{t("columns.actions")}</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {linkedAgents.map((agent) => {
                const link = workstation.agentLinks.find((item) => item.agentId === agent.id);
                return (
                  <tr key={agent.id}>
                    <td className="px-3 py-2">
                      <div className="font-medium">{agentLabel(agent)}</div>
                      <div className="font-mono text-xs text-muted-foreground">{agent.agent_key || agent.id}</div>
                    </td>
                    <td className="px-3 py-2">
                      {link?.isDefault ? (
                        <Badge variant="secondary" className="gap-1">
                          <Star className="h-3 w-3" />
                          {t("agents.default")}
                        </Badge>
                      ) : (
                        <Button variant="outline" size="sm" className="h-7 gap-1" onClick={() => handleSetDefault(agent.id)} disabled={saving}>
                          <Star className="h-3 w-3" />
                          {t("agents.setDefault")}
                        </Button>
                      )}
                    </td>
                    <td className="px-3 py-2 text-right">
                      <Button variant="ghost" size="sm" className="gap-1" onClick={() => handleUnlink(agent.id)} disabled={saving}>
                        <Unlink className="h-3.5 w-3.5" />
                        {t("agents.revoke")}
                      </Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

function WorkstationPermissionsTab({ workstationId }: { workstationId: string }) {
  const { t } = useTranslation("workstations");
  const { permissions, loading, addPermission, removePermission, togglePermission } =
    useWorkstationPermissions(workstationId);
  const [newPattern, setNewPattern] = useState("");
  const [saving, setSaving] = useState(false);

  async function handleAdd() {
    const pattern = newPattern.trim();
    if (!pattern) return;
    setSaving(true);
    try {
      await addPermission(pattern);
      setNewPattern("");
    } finally {
      setSaving(false);
    }
  }

  async function handleToggle(id: string, enabled: boolean) {
    setSaving(true);
    try {
      await togglePermission(id, enabled);
    } finally {
      setSaving(false);
    }
  }

  async function handleRemove(id: string) {
    setSaving(true);
    try {
      await removePermission(id);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="space-y-4 p-3">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center">
        <input
          type="text"
          className="flex-1 rounded-md border bg-background px-3 py-2 text-base text-sm md:text-sm focus:outline-none focus:ring-1 focus:ring-ring"
          placeholder={t("permissions.addPlaceholder")}
          value={newPattern}
          onChange={(e) => setNewPattern(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleAdd()}
          disabled={saving}
        />
        <Button size="sm" className="gap-1 shrink-0" onClick={handleAdd} disabled={!newPattern.trim() || saving}>
          <ShieldCheck className="h-3.5 w-3.5" />
          {t("permissions.add")}
        </Button>
      </div>

      <p className="text-xs text-muted-foreground">{t("permissions.hint")}</p>

      {loading ? null : permissions.length === 0 ? (
        <div className="py-8 text-center text-sm text-muted-foreground">{t("permissions.empty")}</div>
      ) : (
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full min-w-[500px] text-sm">
            <thead className="border-b bg-muted/50">
              <tr>
                <th className="px-3 py-2 text-left font-medium">{t("permissions.columns.pattern")}</th>
                <th className="px-3 py-2 text-left font-medium">{t("permissions.columns.enabled")}</th>
                <th className="px-3 py-2 text-left font-medium">{t("permissions.columns.createdBy")}</th>
                <th className="px-3 py-2 text-right font-medium">{t("columns.actions")}</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {permissions.map((perm) => (
                <tr key={perm.id}>
                  <td className="px-3 py-2 font-mono text-xs">{perm.pattern}</td>
                  <td className="px-3 py-2">
                    <Switch
                      checked={perm.enabled}
                      onCheckedChange={(v) => handleToggle(perm.id, v)}
                      disabled={saving}
                    />
                  </td>
                  <td className="px-3 py-2 text-muted-foreground">{perm.createdBy || "—"}</td>
                  <td className="px-3 py-2 text-right">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="gap-1 text-destructive hover:text-destructive"
                      onClick={() => handleRemove(perm.id)}
                      disabled={saving}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                      {t("permissions.remove")}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

export function WorkstationsPage() {
  const { t } = useTranslation("workstations");
  const { workstations, loading, refresh, createWorkstation, deleteWorkstation, linkAgent, unlinkAgent } = useWorkstations();
  const { agents } = useAgents();

  const spinning = useMinLoading(loading);
  const isEmpty = workstations.length === 0;
  const showSkeleton = useDeferredLoading(loading && isEmpty);

  const [createOpen, setCreateOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Workstation | null>(null);
  const [expandedId, setExpandedId] = useState<string | null>(null);

  function toggleExpand(id: string) {
    setExpandedId((prev) => (prev === id ? null : id));
  }

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex gap-2">
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} />
              {t("common:refresh", "Refresh")}
            </Button>
            <Button size="sm" onClick={() => setCreateOpen(true)} className="gap-1">
              <Plus className="h-3.5 w-3.5" />
              {t("addWorkstation")}
            </Button>
          </div>
        }
      />

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={4} />
        ) : isEmpty ? (
          <EmptyState
            icon={MonitorCog}
            title={t("emptyTitle")}
            description={t("emptyDescription")}
          />
        ) : (
          <div className="rounded-md border overflow-x-auto">
            <table className="w-full min-w-[600px] text-sm">
              <thead>
                <tr className="border-b bg-muted/50">
                  <th className="px-4 py-3 text-left font-medium w-8"></th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.name")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.key")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.backend")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.status")}</th>
                  <th className="px-4 py-3 text-left font-medium">{t("columns.created")}</th>
                  <th className="px-4 py-3 text-right font-medium">{t("columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {workstations.map((ws) => {
                  const isExpanded = expandedId === ws.id;
                  return (
                    <Fragment key={ws.id}>
                      <tr
                        className="border-b last:border-0 hover:bg-muted/30 cursor-pointer"
                        onClick={() => toggleExpand(ws.id)}
                      >
                        <td className="px-4 py-3 text-muted-foreground">
                          {isExpanded ? (
                            <ChevronDown className="h-4 w-4" />
                          ) : (
                            <ChevronRight className="h-4 w-4" />
                          )}
                        </td>
                        <td className="px-4 py-3 font-medium">{ws.name}</td>
                        <td className="px-4 py-3 font-mono text-xs text-muted-foreground">{ws.workstationKey || "—"}</td>
                        <td className="px-4 py-3">
                          <Badge variant="outline">{t(`backend.${ws.backendType}`)}</Badge>
                        </td>
                        <td className="px-4 py-3">
                          <Badge variant={ws.active ? "default" : "secondary"}>
                            {ws.active ? t("status.active") : t("status.inactive")}
                          </Badge>
                        </td>
                        <td className="px-4 py-3 text-muted-foreground">
                          {formatOptionalDate(ws.createdAt)}
                        </td>
                        <td className="px-4 py-3 text-right" onClick={(e) => e.stopPropagation()}>
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setDeleteTarget(ws)}
                            className="gap-1"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                            {t("actions.delete")}
                          </Button>
                        </td>
                      </tr>
                      {isExpanded && (
                        <tr key={`${ws.id}-detail`} className="bg-muted/10">
                          <td colSpan={7} className="px-4 py-4">
                            <Tabs defaultValue="overview">
                              <TabsList className="mb-3">
                                <TabsTrigger value="overview">{t("details.title")}</TabsTrigger>
                                <TabsTrigger value="agents">{t("agents.title")}</TabsTrigger>
                                <TabsTrigger value="permissions">{t("permissions.title")}</TabsTrigger>
                                <TabsTrigger value="activity">{t("activity.title")}</TabsTrigger>
                              </TabsList>
                              <TabsContent value="overview">
                                <WorkstationOverviewTab workstation={ws} />
                              </TabsContent>
                              <TabsContent value="agents">
                                <WorkstationAgentsTab
                                  workstation={ws}
                                  agents={agents}
                                  onLinkAgent={linkAgent}
                                  onUnlinkAgent={unlinkAgent}
                                />
                              </TabsContent>
                              <TabsContent value="permissions">
                                <WorkstationPermissionsTab workstationId={ws.id} />
                              </TabsContent>
                              <TabsContent value="activity">
                                <WorkstationActivityTab workstationId={ws.id} />
                              </TabsContent>
                            </Tabs>
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <WorkstationCreateDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreate={async (params) => {
          await createWorkstation(params);
        }}
      />

      {deleteTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setDeleteTarget(null)}
          title={t("deleteDialog.title")}
          description={t("deleteDialog.description", { name: deleteTarget.name })}
          confirmLabel={t("deleteDialog.confirmLabel")}
          variant="destructive"
          onConfirm={async () => {
            await deleteWorkstation(deleteTarget.id);
            setDeleteTarget(null);
          }}
        />
      )}
    </div>
  );
}
