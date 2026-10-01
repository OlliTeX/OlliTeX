package sitesettings

import (
	"context"
	"os"
	"strings"

	"ollitex/go/services/web/core"
)

// GHSyncEnabled — the github-sync (Git Provider sync) module gate.
//
// Node: Settings.githubSync.enabled (the module's enabled flag in the live
// env / site settings). The env GITHUB_SYNC_ENABLED wins when conclusive;
// otherwise the site_settings `githubSync.enabled` section is used (D23
// config-DB contract); the DEFAULT is ENABLED (the module is installed in
// this fork; audit 006 requires the surface on).
func GHSyncEnabled(a *core.App, ctx context.Context) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GITHUB_SYNC_ENABLED"))) {
	case "1", "true", "yes", "on", "enabled":
		return true
	case "0", "false", "no", "off", "disabled":
		return false
	}
	if a == nil || a.Mongo == nil {
		return true
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return true
	}
	sections := loadAllSections(ctx, db)
	sec, ok := sections["githubSync"]
	if !ok {
		return true
	}
	v, jok := ObjGet(sec, "enabled")
	if !jok {
		return true
	}
	b, _ := v.(bool)
	return b
}
