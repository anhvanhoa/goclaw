import { useEffect, useState } from "react";

const STORAGE_KEY = "goclaw.zalo.webhook_host";

function defaultHost(): string {
  if (typeof window === "undefined") return "";
  return window.location.origin;
}

/**
 * Persist a per-browser override for the gateway host that operators paste
 * into Zalo's dev console. Falls back to window.location.origin when no
 * override is stored. Stored in localStorage so it survives reloads.
 */
export function useWebhookHost(): [string, (next: string) => void] {
  const [host, setHost] = useState<string>(() => {
    if (typeof window === "undefined") return "";
    return window.localStorage.getItem(STORAGE_KEY) ?? defaultHost();
  });

  useEffect(() => {
    if (typeof window === "undefined") return;
    if (host && host !== defaultHost()) {
      window.localStorage.setItem(STORAGE_KEY, host);
    } else {
      window.localStorage.removeItem(STORAGE_KEY);
    }
  }, [host]);

  return [host, setHost];
}
