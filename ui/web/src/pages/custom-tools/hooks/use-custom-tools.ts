import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import i18next from "i18next";
import { userFriendlyError } from "@/lib/error-utils";

export interface CustomToolData {
  id: string;
  name: string;
  description: string;
  parameters: Record<string, unknown>;
  command: string;
  workingDir: string;
  timeoutSeconds: number;
  agentId?: string | null;
  enabled: boolean;
  createdBy: string;
  createdAt: string;
  updatedAt: string;
}

export interface CustomToolFormData {
  name: string;
  description: string;
  command: string;
  parameters: Record<string, unknown>;
  workingDir: string;
  timeoutSeconds: number;
  agentId?: string;
  enabled: boolean;
  env?: Record<string, string>;
}

export function useCustomTools() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data: tools = [], isLoading: loading } = useQuery({
    queryKey: queryKeys.customTools.all,
    queryFn: async () => {
      const res = await http.get<{ tools: CustomToolData[] }>("/v1/tools/custom");
      return res.tools ?? [];
    },
    staleTime: 60_000,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.customTools.all }),
    [queryClient],
  );

  const createTool = useCallback(
    async (data: CustomToolFormData) => {
      try {
        await http.post("/v1/tools/custom", data);
        await invalidate();
        toast.success(
          i18next.t("tools:custom.toast.created"),
          i18next.t("tools:custom.toast.createdDesc", { name: data.name }),
        );
      } catch (err) {
        toast.error(i18next.t("tools:custom.toast.failedCreate"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  const updateTool = useCallback(
    async (id: string, data: Partial<CustomToolFormData>) => {
      try {
        await http.put(`/v1/tools/custom/${id}`, data);
        await invalidate();
        toast.success(i18next.t("tools:custom.toast.updated"));
      } catch (err) {
        toast.error(i18next.t("tools:custom.toast.failedUpdate"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  const deleteTool = useCallback(
    async (id: string) => {
      try {
        await http.delete(`/v1/tools/custom/${id}`);
        await invalidate();
        toast.success(i18next.t("tools:custom.toast.deleted"));
      } catch (err) {
        toast.error(i18next.t("tools:custom.toast.failedDelete"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  const toggleTool = useCallback(
    async (id: string, enabled: boolean) => {
      queryClient.setQueryData<CustomToolData[]>(queryKeys.customTools.all, (old) =>
        old?.map((t) => (t.id === id ? { ...t, enabled } : t)),
      );
      try {
        await http.put(`/v1/tools/custom/${id}`, { enabled });
        await invalidate();
      } catch (err) {
        await invalidate();
        throw err;
      }
    },
    [http, invalidate, queryClient],
  );

  return { tools, loading, createTool, updateTool, deleteTool, toggleTool };
}
