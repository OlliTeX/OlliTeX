package core

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AdminPrivilegeAvailable mirrors settings.defaults.js:
//
//	adminPrivilegeAvailable: process.env.ADMIN_PRIVILEGE_AVAILABLE === 'true'
func AdminPrivilegeAvailable() bool { return os.Getenv("ADMIN_PRIVILEGE_AVAILABLE") == "true" }

// RequireSiteAdmin mirrors AuthorizationMiddleware.ensureUserIsSiteAdmin
// + AuthorizationManager.isUserSiteAdmin (CE; the SaaS admin-domain
// redirect is a no-op here):
//
//	allow ⇔ Settings.adminPrivilegeAvailable && user doc isAdmin:true
//	deny  → 302 /restricted?from=<encodeURIComponent(pathname)>   (pinned P3.1)
//
// Anonymous callers never reach this (the global login gate bounces first);
// the checks below keep the helper safe in isolation. res.Redirect supplies
// the Accept-negotiated body + Vary: Accept, exactly like express.
func (a *App) RequireSiteAdmin(cxt *Cxt, res *Res) bool {
	r := cxt.Req
	deny := func() bool {
		a.restrictedBounce(res, r)
		return false
	}
	if cxt.Sess == nil || !cxt.Sess.IsLoggedIn() {
		return deny()
	}
	if !AdminPrivilegeAvailable() {
		return deny()
	}
	uid := cxt.Sess.UserIDHex()
	if uid == "" || a.Mongo == nil {
		return deny()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return deny()
	}
	var doc struct {
		IsAdmin bool `bson:"isAdmin"`
	}
	oid, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		return deny()
	}
	err = db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}},
		options.FindOne().SetProjection(bson.D{{Key: "isAdmin", Value: 1}})).Decode(&doc)
	if err != nil || !doc.IsAdmin {
		return deny()
	}
	return true
}

// restrictedBounce implements _redirectToRestricted:
//
//	res.redirect('/restricted?from=' + encodeURIComponent(res.locals.currentUrl))
//
// with res.locals.currentUrl = originalUrl.pathname (PATH ONLY — pinned
// /restricted?from=%2Fadmin%2Feditor-state for /admin/editor-state).
func (a *App) restrictedBounce(res *Res, r *http.Request) {
	enc := strings.ReplaceAll(r.URL.Path, "/", "%2F")
	res.Redirect(r, 302, "/restricted?from="+enc)
}
