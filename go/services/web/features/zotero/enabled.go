package zotero

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"ollitex/go/services/web/core"
)

// zoteroEnabled resolves the merged zotero "enabled" flag exactly like
// SiteSettingsManager.getSection('zotero'):
//
//	seed    = boolFromEnv(OVERLEAF_ZOTERO)
//	            ?? ENABLED_LINKED_FILE_TYPES.split(',') ⊇ 'zotero'    (false)
//	merged  = seed, with stored site_settings.global.zotero.enabled (bool)
//	            winning when present.
func zoteroEnabled(ctx context.Context, a *core.App) (bool, error) {
	enabled := false
	if raw := strings.TrimSpace(os.Getenv("OVERLEAF_ZOTERO")); raw != "" {
		if b, err := strconv.ParseBool(raw); err == nil {
			enabled = b
		}
	} else if lft := strings.TrimSpace(os.Getenv("ENABLED_LINKED_FILE_TYPES")); lft != "" {
		for _, part := range strings.Split(lft, ",") {
			if strings.TrimSpace(strings.ToLower(part)) == "zotero" {
				enabled = true
				break
			}
		}
	}
	if a.Mongo == nil {
		return enabled, nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return enabled, err
	}
	var doc struct {
		Zotero any `bson:"zotero"`
	}
	err = db.Collection("site_settings").FindOne(ctx,
		bson.D{{Key: "_id", Value: "global"}}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return enabled, nil
		}
		return enabled, err
	}
	if doc.Zotero != nil {
		// driver decodes a nested doc into primitive.D (a defined type —
		// asserting map[string]any would miss it). Handle D + M + map.
		var sec map[string]any
		switch v := doc.Zotero.(type) {
		case primitive.D:
			tmp := map[string]any{}
			for _, kv := range v {
				tmp[kv.Key] = kv.Value
			}
			sec = tmp
		case primitive.M:
			sec = v
		case map[string]any:
			sec = v
		}
		if sec != nil {
			if val, ok2 := sec["enabled"]; ok2 {
				if b, ok3 := val.(bool); ok3 {
					enabled = b
				}
			}
		}
	}
	return enabled, nil
}

// ensureEnabled mirrors ZoteroSection.ensureZoteroEnabled:
//
//	getSection lookup failure → ALLOW (Node: warn + next());
//	section.enabled === false → 403 text/html "Zotero is disabled on this site".
func ensureEnabled(a *core.App, cxt *core.Cxt, res *core.Res) bool {
	enabled, err := zoteroEnabled(cxt.Req.Context(), a)
	if err != nil {
		return true // allow (Node parity: lookup failure → next())
	}
	if enabled {
		return true
	}
	res.HTML(403, "Zotero is disabled on this site")
	return false
}
