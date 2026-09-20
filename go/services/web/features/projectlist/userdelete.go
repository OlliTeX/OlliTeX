// userdelete.go — user-scoped project lifecycle bridges for the admin user
// mutation surface (P6.3b, adminusers package):
//
//   - DeleteOwnedProjects mirrors Node ProjectDeleter.deleteUsersProjects:
//     delete every project owned by the user (deleterData.deletedReason
//     "account-deletion", the full Node project-deletion chain), then remove
//     the user from every project they are a member of (Node
//     CollaboratorsHandler.removeUserFromAllProjects).
//   - RestoreOwnedDeletedProjects mirrors the project-restore step of Node
//     restoreDeletedUser: for each deletedProjects record with
//     project.owner_ref = user, undeleteProject(id, {suffix: ""}) —
//     best-effort (Node catches per-batch failures and keeps going).
package projectlist

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"ollitex/go/services/web/core"
)

// DeleteOwnedProjects deletes all projects owned by ownerHex (Node
// deleteUsersProjects), then pulls ownerHex from every membership reference
// array (Node removeUserFromAllProjects). actorHex/ip are the admin deleter
// identity recorded in the projects' deleterData (Node options
// {deleterUser, ipAddress}).
func DeleteOwnedProjects(a *core.App, cxt *core.Cxt, ownerHex, actorHex, ip string) {
	if a.Mongo == nil {
		return
	}
	owner, err := primitive.ObjectIDFromHex(ownerHex)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 30*time.Second)
	defer cancel()
	db, errb := a.Mongo.DB(ctx)
	if errb != nil {
		return
	}

	// 1) owned projects -> full deletion chain, reason account-deletion.
	cur, errf := db.Collection("projects").Find(ctx,
		bson.D{{Key: "owner_ref", Value: owner}})
	if errf != nil {
		return
	}
	var owned []primitive.D
	if err := cur.All(ctx, &owned); err != nil {
		return
	}
	for _, p := range owned {
		pid, _ := p[0].Value.(primitive.ObjectID)
		deleteProjectExec(a, cxt, pid, p, actorHex, ip, "account-deletion")
	}

	// 2) remove the user from every project they are a member of (Node
	//    dangerouslyGetAllProjectsUserIsMemberOf: readAndWrite/readOnly/
	//    tokenReadAndWrite/tokenReadOnly).
	refFields := []string{"collablator_refs", "readOnly_refs", "tokenAccessReadAndWrite_refs", "tokenAccessReadOnly_refs"}
	conds := bson.A{}
	for _, f := range refFields {
		conds = append(conds, bson.D{{Key: f, Value: owner}})
	}
	mcur, errm := db.Collection("projects").Find(ctx,
		bson.D{{Key: "$or", Value: conds}, {Key: "_id", Value: 1}})
	if errm != nil {
		return
	}
	var memberProjs []primitive.D
	if err := mcur.All(ctx, &memberProjs); err != nil {
		return
	}
	for _, mp := range memberProjs {
		pid, _ := mp[0].Value.(primitive.ObjectID)
		colPullUser(a, cxt, pid, ownerHex)
	}
}

// RestoreOwnedDeletedProjects mirrors restoreDeletedUser's project step:
// for each deletedProjects record whose project.owner_ref is userHex, run
// the pinned undelete chain with suffix "" (original name, kept owner).
// Best-effort: individual failures are skipped (Node logs and continues).
func RestoreOwnedDeletedProjects(a *core.App, cxt *core.Cxt, userHex string) {
	if a.Mongo == nil {
		return
	}
	owner, err := primitive.ObjectIDFromHex(userHex)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 30*time.Second)
	defer cancel()
	db, errb := a.Mongo.DB(ctx)
	if errb != nil {
		return
	}
	cur, errf := db.Collection("deletedProjects").Find(ctx,
		bson.D{{Key: "project.owner_ref", Value: owner}})
	if errf != nil {
		return
	}
	var recs []primitive.D
	if err := cur.All(ctx, &recs); err != nil {
		return
	}
	for _, dpDoc := range recs {
		pv, okP := dg(dpDoc, "project")
		if !okP || pv == nil {
			continue
		}
		saved := *dcast(pv)
		if saved[0].Key != "_id" {
			continue
		}
		pid, _ := saved[0].Value.(primitive.ObjectID)
		origOwnerHex := ""
		if dv, okv := dg(dpDoc, "deleterData"); okv && dv != nil {
			if dd := dcast(dv); dd != nil {
				if o, isO := dget(*dd, "deletedProjectOwnerId").(primitive.ObjectID); isO {
					origOwnerHex = o.Hex()
				}
			}
		}
		if origOwnerHex == "" {
			continue // Node: resolveProjectUserId OError -> caught by restore
		}
		savedName := ""
		if v, okv := dg(saved, "name"); okv {
			if s, isS := v.(string); isS {
				savedName = s
			}
		}
		// suffix "" -> name unchanged (Node: restored.name + "").
		nameSet := adNameSet(a, cxt, origOwnerHex)
		finalName, okN := adEnsureUnique(nameSet, savedName)
		if !okN || finalName == "" {
			continue
		}
		restored := bson.D{{Key: "_id", Value: saved[0].Value}}
		deletedDocs := []any{}
		for i := 1; i < len(saved); i++ {
			el := saved[i]
			switch el.Key {
			case "archived":
				continue
			case "name":
				restored = append(restored, bson.E{Key: "name", Value: finalName})
			case "deletedDocs":
				if arr, isA := el.Value.(primitive.A); isA {
					deletedDocs = arr
				}
				restored = append(restored, bson.E{Key: "deletedDocs", Value: primitive.A{}})
			default:
				restored = append(restored, el)
			}
		}
		if _, insErr := db.Collection("projects").InsertOne(ctx, restored); insErr != nil {
			continue
		}
		for _, ddv := range deletedDocs {
			if dd := dcast(ddv); dd != nil {
				did := dget2raw(*dd, "_id")
				dnm := dget2raw(*dd, "name")
				f := strings.TrimSuffix(crDocstoreBase(), "/")
				fireHTTP(cxt, "DELETE", f+"/project/"+pid.Hex()+"/doc/"+asStr(did)+"/name/"+asStr(dnm), nil)
			}
		}
		dpID, _ := dpDoc[0].Value.(primitive.ObjectID)
		_, _ = db.Collection("deletedProjects").DeleteOne(ctx, bson.D{{Key: "_id", Value: dpID}})
	}
}
