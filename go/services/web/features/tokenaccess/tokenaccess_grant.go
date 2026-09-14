package tokenaccess

import (
	"context"
	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func grant(a *core.App, rw bool) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		token := cxt.Params["1"]
		confirmed, _ := readGrantBody(cxt)
		cr := checkAndGet(a, cxt, token, rw)
		switch cr.action {
		case actAnonRwDenied:
			respondJSON(res, 200, `{"redirect":"/restricted","anonWriteAccessDenied":true}`)
			return
		case actAnonRoGrant:
			respondJSON(res, 200, `{"redirect":"/project/`+cr.project.ID.Hex()+`","grantAnonymousAccess":"readOnly"}`)
			return
		case actHigherAccess:
			respondJSON(res, 200, `{"redirect":"/project/`+cr.project.ID.Hex()+`","higherAccess":true}`)
			return
		}
		if cr.notFound {
			write404(res)
			return
		}
		p := cr.project
		uid := ""
		if cxt.Sess != nil {
			_, uid = core.PassportUser(cxt.Sess)
		}
		if !confirmed {
			respondJSON(res, 200, `{"requireAccept":{"projectName":`+jsonStr(p.Name)+`}}`)
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.W.WriteHeader(500)
			return
		}
		priv := "readOnly"
		refField := "tokenAccessReadOnly_refs"
		if rw {
			priv = "readAndWrite"
			refField = "tokenAccessReadAndWrite_refs"
		}
		member := p.inRefs(p.RWRefs, uid) || p.inRefs(p.RORefs, uid)
		if !member && uid != "" {
			_, _ = db.Collection("projectAuditLogEntries").InsertOne(ctx, bson.M{
				"projectId": p.ID,
				"operation": "join-via-token",
				"userId":    uid,
				"ip":        core.ClientIP(cxt.Req),
				"info":      bson.M{"privileges": priv},
				"createdAt": time.Now().UTC(),
				"updatedAt": time.Now().UTC(),
			})
		}
		_, _ = db.Collection("projects").UpdateOne(ctx,
			bson.M{"_id": p.ID},
			bson.M{"$addToSet": bson.M{refField: uid}})
		if rw {
			respondJSON(res, 200, `{"redirect":"/project/`+p.ID.Hex()+`"}`)
		} else {
			respondJSON(res, 200, `{"redirect":"/project/`+p.ID.Hex()+`","tokenAccessGranted":"readOnly"}`)
		}
	}
}

// ---------- consent (sharing-updates) ----------

// ---------- consent (sharing-updates) ----------

func consent(a *core.App, which string) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		pidHex := cxt.Params["1"]
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
		defer cancel()
		p, ok := findProject(a, ctx, pidHex)
		if !ok || p == nil {
			respondJSON(res, 404, `{"message":"Not found."}`)
			return
		}
		uid := ""
		if cxt.Sess != nil {
			_, uid = core.PassportUser(cxt.Sess)
		}
		// ensureUserCanReadProject: owner / named member (pinned: pure
		// token-ref members get 403 Restricted).
		if uid == "" || !(p.OwnerRef == uid || p.inRefs(p.CollabRefs, uid) || p.inRefs(p.ReadonlyNamedRefs, uid)) {
			// Node renders the Restricted view with a 403
			views.Restricted403(res.W, pageBase(cxt, nil))
			return
		}
		// ensureUserCanUseSharingUpdatesConsentPage
		if !p.inRefs(p.RWRefs, uid) || p.inRefs(p.CollabRefs, uid) {
			if core.AcceptsJSON(cxt.Req) {
				respondJSON(res, 200, `{"redir":"/project/`+pidHex+`"}`)
				return
			}
			res.Redirect(cxt.Req, 302, "/project/"+pidHex)
			return
		}
		if which == "page" {
			views.SharingUpdatesPage(res.W, pageBase(cxt, nil))
			return
		}
		// join / view moves
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.W.WriteHeader(500)
			return
		}
		oaid, oerr := primitive.ObjectIDFromHex(pidHex)
		if oerr != nil {
			res.W.WriteHeader(400)
			return
		}
		isInvited := p.OwnerRef == uid || p.inRefs(p.CollabRefs, uid) || p.inRefs(p.ReadonlyNamedRefs, uid)
		_, _ = db.Collection("projects").UpdateOne(ctx,
			bson.M{"_id": oaid},
			bson.M{"$pull": bson.M{"tokenAccessReadAndWrite_refs": uid}})
		if which == "join" {
			_, _ = db.Collection("projectAuditLogEntries").InsertOne(ctx, bson.M{
				"projectId": oaid,
				"operation": "accept-via-link-sharing",
				"userId":    uid,
				"ip":        core.ClientIP(cxt.Req),
				"info":      bson.M{"privileges": "readAndWrite", "tokenMember": true, "invitedMember": isInvited},
				"createdAt": time.Now().UTC(),
				"updatedAt": time.Now().UTC(),
			})
			_, _ = db.Collection("projects").UpdateOne(ctx, bson.M{"_id": oaid},
				bson.M{"$addToSet": bson.M{"collaberator_refs": uid}})
		} else {
			_, _ = db.Collection("projectAuditLogEntries").InsertOne(ctx, bson.M{
				"projectId": oaid,
				"operation": "readonly-via-sharing-updates",
				"userId":    uid,
				"ip":        core.ClientIP(cxt.Req),
				"createdAt": time.Now().UTC(),
				"updatedAt": time.Now().UTC(),
			})
			_, _ = db.Collection("projects").UpdateOne(ctx, bson.M{"_id": oaid},
				bson.M{"$addToSet": bson.M{"tokenAccessReadOnly_refs": uid}})
		}
		res.W.WriteHeader(204)
	}
}
