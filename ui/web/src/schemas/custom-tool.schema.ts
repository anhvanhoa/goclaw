import { z } from "zod";

const envEntrySchema = z.object({
  key: z.string(),
  value: z.string(),
});

export const customToolSchema = z.object({
  name: z
    .string()
    .min(1, "Required")
    .max(100, "Max 100 characters")
    .regex(/^[a-z0-9_]+$/, "Only lowercase letters, digits, and underscores"),
  description: z.string(),
  command: z.string().min(1, "Required"),
  parametersStr: z.string().refine((s) => {
    try { JSON.parse(s || "{}"); return true; } catch { return false; }
  }, "Must be valid JSON"),
  workingDir: z.string(),
  timeoutSeconds: z.number().min(1).max(3600),
  agentId: z.string(),
  enabled: z.boolean(),
  envEntries: z.array(envEntrySchema),
});

export type CustomToolFormValues = z.infer<typeof customToolSchema>;
