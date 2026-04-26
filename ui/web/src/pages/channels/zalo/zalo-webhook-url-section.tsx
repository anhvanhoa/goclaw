import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Copy, Check } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useWsCall } from "@/hooks/use-ws-call";

interface WebhookURLResp {
  path: string;
  instance_id: string;
  hint: string;
}

interface ZaloWebhookURLSectionProps {
  instanceId: string;
  channelType: string; // "zalo_bot" | "zalo_oa"
}

/**
 * Renders the webhook path returned by `channels.instances.zalo.webhook_url`.
 * The RPC intentionally returns only the path — operator prepends their own
 * gateway host (B3: no fabricated gateway.PublicBaseURL config).
 */
export function ZaloWebhookURLSection({ instanceId, channelType }: ZaloWebhookURLSectionProps) {
  const { t } = useTranslation("channels");
  const { call, loading, error } = useWsCall<WebhookURLResp>("channels.instances.zalo.webhook_url");
  const [data, setData] = useState<WebhookURLResp | null>(null);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!instanceId) return;
    call({ instance_id: instanceId })
      .then(setData)
      .catch(() => {
        // error captured by hook
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [instanceId]);

  if (channelType !== "zalo_bot" && channelType !== "zalo_oa") {
    return null;
  }

  async function handleCopy() {
    if (!data?.path) return;
    try {
      await navigator.clipboard.writeText(data.path);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard unavailable — operator can copy manually
    }
  }

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <h3 className="text-sm font-medium">{t("detail.zaloWebhook.title", { defaultValue: "Webhook URL" })}</h3>
      <div className="grid gap-1.5">
        <Label>{t("detail.zaloWebhook.pathLabel", { defaultValue: "Path" })}</Label>
        <div className="flex gap-2">
          <Input
            value={loading ? "" : (data?.path ?? "")}
            placeholder={loading ? t("detail.zaloWebhook.loading", { defaultValue: "Loading..." }) : ""}
            readOnly
            className="text-base md:text-sm font-mono"
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={handleCopy}
            disabled={!data?.path}
            aria-label={t("detail.zaloWebhook.copy", { defaultValue: "Copy path" })}
          >
            {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
          </Button>
        </div>
        {data?.hint && (
          <p className="text-xs text-muted-foreground">{data.hint}</p>
        )}
        {error && (
          <p className="text-xs text-destructive">{error.message}</p>
        )}
      </div>
    </section>
  );
}
