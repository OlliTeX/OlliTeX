// gsync merge-engine seam: apply remote git content to a project via the
// TPDS upsert / entity-delete primitives (Node UpdateMerger.mergeUpdate /
// deleteUpdate — the GitHub TPDS endpoint handlers' engines).
//
// The web profile does NOT mount the TPDS routes (APIOnly), so gsync calls
// these exported wrappers in-process instead of round-tripping over HTTP.
package projectlist

import (
	"context"
	"time"

	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// MergeUpsert — write `data` to fullPath (create or replace; doc/file
// classification inside). ok=false on engine failure. Node
// UpdateMerger.mergeUpdate(userId, projectId, path, data, origin=github).
// pd = the raw project document (LoadProjectDoc shape).
func MergeUpsert(a *core.App, uidHex string, pd *bson.D, pid bson.ObjectID, fullPath string, data []byte) bool {
	_, _, _, _, _, ok := syncUpsert(a, uidHex, pd, pid, fullPath, data)
	return ok
}

// LoadProjectDoc — the raw projects document (gsync: TPDS upsert engine +
// owner/email reads).
func LoadProjectDoc(a *core.App, cxt *core.Cxt, pid bson.ObjectID) (*bson.D, bool) {
	d, err := loadProjectFull(a, cxt, pid)
	if err != nil {
		return nil, false
	}
	return d, d != nil
}

// ProjectVersion — the project's current version field (the Go stack's
// version store; Node HistoryManager.latestVersion equivalent).
func ProjectVersion(a *core.App, cxt *core.Cxt, pid bson.ObjectID) (int64, bool) {
	d, okp := LoadProjectDoc(a, cxt, pid)
	if !okp || d == nil {
		return 0, false
	}
	for i := range *d {
		if (*d)[i].Key == "version" {
			switch v := (*d)[i].Value.(type) {
			case int32:
				return int64(v), true
			case int64:
				return v, true
			case int:
				return int64(v), true
			case float64:
				return int64(v), true
			}
			return 0, false
		}
	}
	return 0, true
}

// OwnerRef — the project's owner_ref hex (unlinkRepo owner check).
func OwnerRef(d *bson.D) string {
	if d == nil {
		return ""
	}
	for i := range *d {
		if (*d)[i].Key == "owner_ref" {
			if o, ok2 := (*d)[i].Value.(bson.ObjectID); ok2 {
				return o.Hex()
			}
			if s, ok2 := (*d)[i].Value.(string); ok2 {
				return s
			}
		}
	}
	return ""
}

// MergeDelete — delete the entity at fullPath. found=false if the project
// does not exist (Node: "project not found" warn) — gsync treats that as a
// failed apply.
func MergeDelete(a *core.App, uidHex string, pd *bson.D, pid bson.ObjectID, fullPath string) bool {
	_, _, found := syncDeleteEntity(a, uidHex, pd, pid, fullPath)
	return found
}

// UserEmail — users lookup by uid hex (owner email for 403/need-permission
// states).
func UserEmail(a *core.App, cxt *core.Cxt, uidHex string) string {
	if a == nil || a.Mongo == nil || uidHex == "" {
		return ""
	}
	uid, err := bson.ObjectIDFromHex(uidHex)
	if err != nil {
		return ""
	}
	ctx3, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx3)
	if err != nil {
		return ""
	}
	var u struct {
		Email string `bson:"email"`
	}
	if err := db.Collection("users").FindOne(ctx3, bson.D{{Key: "_id", Value: uid}}).Decode(&u); err != nil {
		return ""
	}
	return u.Email
}
