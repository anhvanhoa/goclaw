import { useEffect } from "react";
import { useForm, Controller, useFieldArray } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { customToolSchema, type CustomToolFormValues } from "@/schemas/custom-tool.schema";
import type { CustomToolData, CustomToolFormData } from "./hooks/use-custom-tools";

interface CustomToolDialogProps {
  open: boolean;
  tool?: CustomToolData | null;
  onOpenChange: (open: boolean) => void;
  onSave: (data: CustomToolFormData) => Promise<void>;
}

export function CustomToolDialog({ open, tool, onOpenChange, onSave }: CustomToolDialogProps) {
  const { t } = useTranslation("tools");
  const { agents } = useAgents();
  const isEdit = !!tool;

  const {
    register,
    control,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<CustomToolFormValues>({
    resolver: zodResolver(customToolSchema),
    defaultValues: {
      name: "",
      description: "",
      command: "",
      parametersStr: "{}",
      workingDir: "",
      timeoutSeconds: 60,
      agentId: "",
      enabled: true,
      envEntries: [],
    },
  });

  const { fields: envFields, append: appendEnv, remove: removeEnv } = useFieldArray({
    control,
    name: "envEntries",
  });

  useEffect(() => {
    if (open) {
      if (tool) {
        reset({
          name: tool.name,
          description: tool.description,
          command: tool.command,
          parametersStr: JSON.stringify(tool.parameters ?? {}, null, 2),
          workingDir: tool.workingDir ?? "",
          timeoutSeconds: tool.timeoutSeconds ?? 60,
          agentId: tool.agentId ?? "",
          enabled: tool.enabled,
          envEntries: [],
        });
      } else {
        reset({
          name: "",
          description: "",
          command: "",
          parametersStr: "{}",
          workingDir: "",
          timeoutSeconds: 60,
          agentId: "",
          enabled: true,
          envEntries: [],
        });
      }
    }
  }, [open, tool, reset]);

  const onFormSubmit = async (data: CustomToolFormValues) => {
    const env: Record<string, string> = {};
    for (const { key, value } of data.envEntries) {
      if (key.trim()) env[key.trim()] = value;
    }

    await onSave({
      name: data.name,
      description: data.description,
      command: data.command,
      parameters: JSON.parse(data.parametersStr || "{}"),
      workingDir: data.workingDir,
      timeoutSeconds: data.timeoutSeconds,
      agentId: data.agentId || undefined,
      enabled: data.enabled,
      env: Object.keys(env).length > 0 ? env : undefined,
    });
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {isEdit ? t("custom.form.editTitle") : t("custom.form.createTitle")}
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-4 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
          {/* Name */}
          <div className="space-y-2">
            <Label>{t("custom.form.name")}</Label>
            <Input
              {...register("name")}
              placeholder={t("custom.form.namePlaceholder")}
              disabled={isEdit}
            />
            {errors.name ? (
              <p className="text-xs text-destructive">{errors.name.message}</p>
            ) : (
              <p className="text-xs text-muted-foreground">{t("custom.form.nameHint")}</p>
            )}
          </div>

          {/* Description */}
          <div className="space-y-2">
            <Label>{t("custom.form.description")}</Label>
            <Input
              {...register("description")}
              placeholder={t("custom.form.descriptionPlaceholder")}
            />
          </div>

          {/* Command */}
          <div className="space-y-2">
            <Label>{t("custom.form.command")}</Label>
            <Textarea
              {...register("command")}
              rows={3}
              className="font-mono text-sm"
            />
            {errors.command ? (
              <p className="text-xs text-destructive">{errors.command.message}</p>
            ) : (
              <p className="text-xs text-muted-foreground">{t("custom.form.commandHint")}</p>
            )}
          </div>

          {/* Parameters JSON Schema */}
          <div className="space-y-2">
            <Label>{t("custom.form.parameters")}</Label>
            <Textarea
              {...register("parametersStr")}
              rows={5}
              className="font-mono text-xs"
            />
            {errors.parametersStr && (
              <p className="text-xs text-destructive">{errors.parametersStr.message}</p>
            )}
          </div>

          {/* Working Dir + Timeout */}
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-2">
              <Label>{t("custom.form.workingDir")}</Label>
              <Input
                {...register("workingDir")}
                placeholder={t("custom.form.workingDirPlaceholder")}
              />
            </div>
            <div className="space-y-2">
              <Label>{t("custom.form.timeout")}</Label>
              <Input
                type="number"
                min={1}
                max={3600}
                {...register("timeoutSeconds", { valueAsNumber: true })}
              />
              {errors.timeoutSeconds && (
                <p className="text-xs text-destructive">{errors.timeoutSeconds.message}</p>
              )}
            </div>
          </div>

          {/* Agent */}
          <div className="space-y-2">
            <Label>{t("custom.form.agentId")}</Label>
            <Controller
              control={control}
              name="agentId"
              render={({ field }) => (
                <Select
                  value={field.value || "__global__"}
                  onValueChange={(v) => field.onChange(v === "__global__" ? "" : v)}
                >
                  <SelectTrigger className="text-base md:text-sm">
                    <SelectValue placeholder={t("custom.form.agentIdPlaceholder")} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__global__">{t("custom.scope.global")}</SelectItem>
                    {agents.map((a) => (
                      <SelectItem key={a.id} value={a.id}>
                        {a.display_name || a.agent_key || a.id}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
          </div>

          {/* Env Vars */}
          <div className="space-y-2">
            <Label>Env Vars</Label>
            {envFields.map((field, i) => (
              <div key={field.id} className="flex gap-2 items-center">
                <Input
                  {...register(`envEntries.${i}.key`)}
                  placeholder="KEY"
                  className="flex-1 font-mono text-xs"
                />
                <Input
                  {...register(`envEntries.${i}.value`)}
                  placeholder="value"
                  type="password"
                  className="flex-1"
                />
                <button
                  type="button"
                  onClick={() => removeEnv(i)}
                  className="text-muted-foreground hover:text-destructive px-1 text-sm"
                >
                  ✕
                </button>
              </div>
            ))}
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => appendEnv({ key: "", value: "" })}
            >
              + Add
            </Button>
          </div>

          {/* Enabled */}
          <div className="flex items-center gap-3">
            <Controller
              control={control}
              name="enabled"
              render={({ field }) => (
                <Switch
                  checked={field.value}
                  onCheckedChange={field.onChange}
                />
              )}
            />
            <Label>{t("custom.form.enabled")}</Label>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={isSubmitting}>
            {t("custom.form.cancel")}
          </Button>
          <Button onClick={handleSubmit(onFormSubmit)} disabled={isSubmitting}>
            {isSubmitting
              ? t("custom.form.saving")
              : isEdit
                ? t("custom.form.update")
                : t("custom.form.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
