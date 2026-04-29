package common

// WebhookPath is the single mount point for both Zalo channel flavors;
// per-instance dispatch uses the ?instance=<uuid> query param.
const WebhookPath = "/channels/zalo/webhook"

var sharedRouter = NewRouter()

// SharedRouter returns the process-global router.
func SharedRouter() *Router { return sharedRouter }
