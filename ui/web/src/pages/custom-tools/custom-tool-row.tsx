import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Pencil, Trash2 } from "lucide-react";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import type { CustomToolData } from "./hooks/use-custom-tools";

interface CustomToolRowProps {
  tool: CustomToolData;
  onEdit: (tool: CustomToolData) => void;
  onToggle: (id: string, enabled: boolean) => Promise<void>;
  onDelete: (id: string) => Promise<void>;
}

export function CustomToolRow({ tool, onEdit, onToggle, onDelete }: CustomToolRowProps) {
  const { t } = useTranslation("tools");
  const [deleting, setDeleting] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  const handleDelete = async () => {
    setDeleting(true);
    try {
      await onDelete(tool.id);
    } finally {
      setDeleting(false);
      setConfirmOpen(false);
    }
  };

  return (
    <>
      <tr className="border-b last:border-0 hover:bg-muted/30 transition-colors">
        <td className="py-3 px-4">
          <span className="font-mono text-sm font-medium">{tool.name}</span>
        </td>
        <td className="py-3 px-4 text-sm text-muted-foreground max-w-xs truncate">
          {tool.description || (
            <span className="italic">{t("custom.noDescription")}</span>
          )}
        </td>
        <td className="py-3 px-4">
          <span className="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium bg-muted text-muted-foreground">
            {tool.agentId ? t("custom.scope.agent") : t("custom.scope.global")}
          </span>
        </td>
        <td className="py-3 px-4">
          <Switch
            checked={tool.enabled}
            onCheckedChange={(v) => onToggle(tool.id, v)}
            aria-label={t("custom.columns.enabled")}
          />
        </td>
        <td className="py-3 px-4 text-sm text-muted-foreground">
          {tool.timeoutSeconds}s
        </td>
        <td className="py-3 px-4">
          <div className="flex items-center gap-1.5">
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7"
              onClick={() => onEdit(tool)}
              title={t("custom.form.editTitle")}
            >
              <Pencil className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-destructive hover:text-destructive"
              onClick={() => setConfirmOpen(true)}
              title={t("custom.delete.title")}
            >
              <Trash2 className="h-3.5 w-3.5" />
            </Button>
          </div>
        </td>
      </tr>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t("custom.delete.title")}
        description={t("custom.delete.description", { name: tool.name })}
        confirmLabel={t("custom.delete.confirmLabel")}
        variant="destructive"
        onConfirm={handleDelete}
        loading={deleting}
      />
    </>
  );
}
