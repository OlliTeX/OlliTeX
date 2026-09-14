package projectlist

import (
	"context"
	"path"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
)

// P4.2 read-authorization + project read (the entities route). Pinned against
// the LIVE Node oracle (2026-09-14):
//
//	canUserReadProject(userId, projectId)  (AuthorizationManager.jss)
//	  = getPrivilegeLevelForProject(userId, projectId, {ignoreSiteAdmin:true})
//	      in [owner, readAndWrite, readOnly, review]
//	    || hasAdminProjectCapability(userId, "view-project-content")
//
//	privilege level (ProjectAccess.privilegeLevelForUser, CollaboratorsGetter.js)
//	  = the member-record level for userId from, in order:
//	      owner_ref                       -> owner
//	      collaberator_refs               -> readAndWrite   (INVITE)
//	      reviewer_refs                   -> review         (INVITE)
//	      readOnly_refs                   -> readOnly       (INVITE)
//	      (only if publicAccesLevel == "tokenBased"):
//	        tokenAccessReadAndWrite_refs  -> readAndWrite   (TOKEN)
//	        tokenAccessReadOnly_refs      -> readOnly       (TOKEN)
//	    else NONE; and, for a logged-in non-member:
//	      publicAccesLevel == "readOnly" / "readAndWrite" (LEGACY public) -> readable.
//
// Node's route:  GET /project/:Project_id/entities
//   requireLogin  ->  ensureUserCanReadProject  ->  projectEntitiesJson
//
//   _getProjectId runs parseReq(projectIdParamsSchema) -> zod objectId:
//     * invalid ObjectId -> 404 JSON  {"error":"Validation error: Invalid Mongo
//       ObjectId at \"params.Project_id\"","statusCode":404}  (NOT accept-dependent)
//   canUserReadProject:
//     * valid id, project absent  -> NotFoundError -> 404 HTML general/404
//       (NOT accept-dependent)
//     * present, !canRead         -> 403 (accept-dependent):
//                                       accept json -> {"message":"restricted"}
//                                       accept html -> restricted view
//     * present, canRead          -> 200 { project_id, entities:[{path,type}] }

// inOIDList reports whether uid (hex) appears in a refs array value.
func inOIDList(v any, uid string) bool {
	if uid == "" {
		return false
	}
	for _, s := range asOIDList(v) {
		if s == uid {
			return true
		}
	}
	return false
}

// canRead mirrors canUserReadProject for a logged-in user.
func canRead(uid string, isAdmin bool, d primitive.D) bool {
	if uid == "" {
		return false
	}
	if oidHex(dget(d, "owner_ref")) == uid {
		return true
	}
	if inOIDList(dget(d, "collaberator_refs"), uid) {
		return true
	}
	if inOIDList(dget(d, "reviewer_refs"), uid) {
		return true
	}
	if inOIDList(dget(d, "readOnly_refs"), uid) {
		return true
	}
	pal := asStr(dget(d, "publicAccesLevel"))
	if pal == "tokenBased" &&
		(inOIDList(dget(d, "tokenAccessReadAndWrite_refs"), uid) ||
			inOIDList(dget(d, "tokenAccessReadOnly_refs"), uid)) {
		return true
	}
	if pal == "readOnly" || pal == "readAndWrite" {
		return true
	}
	if isAdmin {
		return true
	}
	return false
}

// accessProj is the projection `getProjectAccess` + `projectEntitiesJson`
// together need (access fields + the tree). Node fetches the project with no
// projection for the entities; a single full read covers both.
var accessProj = bson.D{
	{Key: "owner_ref", Value: 1},
	{Key: "collaberator_refs", Value: 1},
	{Key: "reviewer_refs", Value: 1},
	{Key: "readOnly_refs", Value: 1},
	{Key: "tokenAccessReadAndWrite_refs", Value: 1},
	{Key: "tokenAccessReadOnly_refs", Value: 1},
	{Key: "publicAccesLevel", Value: 1},
	{Key: "rootFolder", Value: 1},
}

// loadProject returns the project doc (access fields + rootFolder), or
// (nil, nil) when it does not exist (Node: NotFoundError -> 404).
func loadProject(a *core.App, cxt *core.Cxt, oid primitive.ObjectID) (*primitive.D, error) {
	if a.Mongo == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, err
	}
	opts := options.FindOne().SetProjection(accessProj)
	var d primitive.D
	err = db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}, opts).Decode(&d)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// loadUserAdmin returns the user's isAdmin flag (drives the
// hasAdminProjectCapability branch of canUserReadProject; conservative).
func loadUserAdmin(a *core.App, cxt *core.Cxt, uid string) bool {
	if a.Mongo == nil || uid == "" {
		return false
	}
	oid, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	var d primitive.D
	opts := options.FindOne().SetProjection(bson.D{{Key: "isAdmin", Value: 1}})
	if db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}, opts).Decode(&d) != nil {
		return false
	}
	b, ok := dget(d, "isAdmin").(bool)
	return ok && b
}

// ---------- tree walk (ProjectEntityHandler.getAllEntitiesFromProject) ----------

type ent struct {
	path string
	typ  string // "doc" | "file"
}

func pjoin(base, name string) string { return path.Join(base, name) }

func dgetArr(d primitive.D, key string) primitive.A {
	a, _ := dget(d, key).(primitive.A)
	return a
}

// walkFolder mirrors _getAllFoldersFromProject (folders) +
// getAllEntitiesFromProject (docs + fileRefs), for one folder subtree.
func walkFolder(folder primitive.D, base string, out *[]ent) {
	for _, doc := range dgetArr(folder, "docs") {
		dm, ok := doc.(primitive.D)
		if !ok {
			continue
		}
		if nm, ok2 := dget(dm, "name").(string); ok2 {
			*out = append(*out, ent{path: pjoin(base, nm), typ: "doc"})
		}
	}
	for _, fr := range dgetArr(folder, "fileRefs") {
		fm, ok := fr.(primitive.D)
		if !ok {
			continue
		}
		if nm, ok2 := dget(fm, "name").(string); ok2 {
			*out = append(*out, ent{path: pjoin(base, nm), typ: "file"})
		}
	}
	for _, c := range dgetArr(folder, "folders") {
		cm, ok := c.(primitive.D)
		if !ok {
			continue
		}
		nm, ok2 := dget(cm, "name").(string)
		if ok2 { // Node: childFolder.name != null
			walkFolder(cm, pjoin(base, nm), out)
		}
	}
}

// collectEntities returns the project's doc+file entities (pre-sort), walking
// rootFolder[0] from "/".
func collectEntities(doc *primitive.D) []ent {
	var out []ent
	rf := dgetArr(*doc, "rootFolder")
	if len(rf) > 0 {
		if root, ok := rf[0].(primitive.D); ok {
			walkFolder(root, "/", &out)
		}
	}
	return out
}
