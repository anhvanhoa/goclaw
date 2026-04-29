package bot

import "github.com/nextlevelbuilder/goclaw/internal/channels/zalo/common"

// StripMarkdown re-exports common.StripMarkdown for external callers
// (zalo/personal).
func StripMarkdown(text string) string { return common.StripMarkdown(text) }
