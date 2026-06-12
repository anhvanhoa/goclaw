import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Wrench, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { SearchInput } from "@/components/shared/search-input";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";
import { useCustomTools, type CustomToolData, type CustomToolFormData } from "./hooks/use-custom-tools";
import { CustomToolDialog } from "./custom-tool-dialog";
import { CustomToolRow } from "./custom-tool-row";

export function CustomToolsPage() {
  const { t } = useTranslation("tools");
  const { tools, loading, createTool, updateTool, deleteTool, toggleTool, fetchToolEnv } = useCustomTools();
  const showSkeleton = useDeferredLoading(loading && tools.length === 0);

  const [search, setSearch] = useState("");
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingTool, setEditingTool] = useState<CustomToolData | null>(null);

  const filtered = tools.filter(
    (tool) =>
      tool.name.toLowerCase().includes(search.toLowerCase()) ||
      tool.description.toLowerCase().includes(search.toLowerCase()),
  );

  const handleOpenCreate = () => {
    setEditingTool(null);
    setDialogOpen(true);
  };

  const handleOpenEdit = (tool: CustomToolData) => {
    setEditingTool(tool);
    setDialogOpen(true);
  };

  const handleSave = async (data: CustomToolFormData) => {
    if (editingTool) {
      await updateTool(editingTool.id, data);
    } else {
      await createTool(data);
    }
  };

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("custom.title")}
        description={t("custom.description")}
        actions={
          <Button size="sm" onClick={handleOpenCreate} className="gap-1">
            <Plus className="h-3.5 w-3.5" />
            {t("custom.createTool")}
          </Button>
        }
      />

      <div className="mt-4">
        <SearchInput
          value={search}
          onChange={setSearch}
          placeholder={t("custom.searchPlaceholder")}
          className="max-w-sm"
        />
      </div>

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={6} />
        ) : filtered.length === 0 ? (
          <EmptyState
            icon={Wrench}
            title={search ? t("custom.noMatchTitle") : t("custom.emptyTitle")}
            description={search ? t("custom.noMatchDescription") : t("custom.emptyDescription")}
          />
        ) : (
          <div className="overflow-x-auto rounded-lg border">
            <table className="w-full min-w-150">
              <thead>
                <tr className="border-b bg-muted/50 text-xs font-medium text-muted-foreground uppercase tracking-wide">
                  <th className="py-2.5 px-4 text-left">{t("custom.columns.name")}</th>
                  <th className="py-2.5 px-4 text-left">{t("custom.columns.description")}</th>
                  <th className="py-2.5 px-4 text-left">{t("custom.columns.scope")}</th>
                  <th className="py-2.5 px-4 text-left">{t("custom.columns.enabled")}</th>
                  <th className="py-2.5 px-4 text-left">{t("custom.columns.timeout")}</th>
                  <th className="py-2.5 px-4 text-left">{t("custom.columns.actions")}</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((tool) => (
                  <CustomToolRow
                    key={tool.id}
                    tool={tool}
                    onEdit={handleOpenEdit}
                    onToggle={toggleTool}
                    onDelete={deleteTool}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <CustomToolDialog
        open={dialogOpen}
        tool={editingTool}
        onOpenChange={setDialogOpen}
        onSave={handleSave}
        fetchToolEnv={fetchToolEnv}
      />
    </div>
  );
}
