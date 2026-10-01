// Pandoc conversion (Word/Markdown/HTML export) gate — live-audit 013.
//
// The editor's "Export as …" items require BOTH the ol-ExposedSettings
// `enablePandocConversions` flag (this gate) and the export-docx /
// export-markdown / export-html split-test flags set to "enabled"
// (front-end isSplitTestEnabled). The gate reads the Node-legacy
// site_settings `pandoc` section ({enabled, image}) — the live deployment
// already carries pandoc.enabled from the Overleaf CE settings schema —
// with env ENABLE_PANDOC_CONVERSIONS=true|false winning when conclusive.
// Default OFF; the CLSI conversion endpoints are separately gated by the
// CLSI-side ENABLE_PANDOC_CONVERSIONS env (both sides must be on).
package sitesettings

import (
	"context"
	"os"

	"ollitex/go/services/web/core"
)

func PandocConversionsEnabled(a *core.App, ctx context.Context) bool {
	switch os.Getenv("ENABLE_PANDOC_CONVERSIONS") {
	case "true":
		return true
	case "false":
		return false
	}
	if a == nil || a.Mongo == nil {
		return false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	sections := loadAllSections(ctx, db)
	sec, ok := sections["pandoc"]
	if !ok {
		return false
	}
	v, jok := ObjGet(sec, "enabled")
	if !jok {
		return false
	}
	b, isB := v.(bool)
	return isB && b
}
