// Mendeley integration gate (live-audit 010: hide the Mendeley tab from
// "Add files" when the admin settings are not configured).
package sitesettings

import (
	"context"
	"os"

	"ollitex/go/services/web/core"
)

// MendeleyEnabled — the merged mendeley "enabled" flag: env seed
// (MENDELEY_ENABLED true/false wins when conclusive) + site_settings
// `mendeley.enabled`. Absent section / no Mongo → false (Node loaded the
// mendeley module only when the service was configured; the owner has never
// configured Mendeley here, and the module API is not part of this stack,
// so the honest default is OFF).
func MendeleyEnabled(a *core.App, ctx context.Context) bool {
	switch os.Getenv("MENDELEY_ENABLED") {
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
	sec, ok := sections["mendeley"]
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
