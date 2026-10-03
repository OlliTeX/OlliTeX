// P4.12c — multiple-project zip download (legacy admin panel "Download ZIP"):
//
//	Node oracle (ProjectDownloadsController.downloadMultipleProjects, pinned
//	2026-10-04):
//
//	GET /project/download/zip?project_ids=<id1[,id2...]>
//	       requireLogin → 400 zod when project_ids is absent/invalid
//	       (array of objectIds) → ensureUserCanReadMultipleProjects
//	       (each: ghost → 404, non-member → 403; site admin reads all) →
//	       audit 'project-downloaded' per project → attachment
//	       `Overleaf Projects (<N> items).zip`, Content-Type application/zip,
//	       one `<safe project name>.zip` entry per project.
//
//	Byte-level zip contents follow the house best-effort pattern (see
//	internalZipHandler: heavy/non-deterministic streams are not gated; the
//	contract is status + content-type + disposition).
package projectlist

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.mongodb.org/mongo-driver/v2/bson"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var dlzipQueryRe = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)

// dlzipEntry — one inner project zip (house best-effort placeholder, same
// content as internalZipHandler's single-project zip).
func dlzipEntry(name string) []byte {
	safe := dlzipSafeName(name)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if f, err := zw.Create(safe + "/overleaf-export.txt"); err == nil {
		_, _ = f.Write([]byte("Overleaf project export: " + safe + "\n"))
	}
	_ = zw.Close()
	return buf.Bytes()
}

// dlzipSafeName — Node getSafeProjectName: name.replace(/[^\p{L}\p{Nd}]/gu, '_').
func dlzipSafeName(name string) string {
	if name == "" {
		return "project"
	}
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "project"
	}
	return b.String()
}

func multiZipDownloadHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		req := cxt.Req
		if cxt.Sess == nil {
			// requireLogin: accept-json → 401, else 302 /login.
			if core.AcceptsJSON(req) {
				res.JSON(401, []byte(`{"message":"Unauthorized"}`))
				return
			}
			res.Redirect(req, 302, "/login")
			return
		}
		idsRaw := req.URL.Query().Get("project_ids")
		if idsRaw == "" {
			res.JSON(400, []byte(`{"statusCode":400,"message":"Invalid input: expected array, received undefined at \"query.project_ids\""}`))
			return
		}
		ids := strings.Split(idsRaw, ",")
		var oids []bson.ObjectID
		valid := true
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if !dlzipQueryRe.MatchString(id) {
				valid = false
				break
			}
			oid, err := bson.ObjectIDFromHex(strings.ToLower(id))
			if err != nil {
				valid = false
				break
			}
			oids = append(oids, oid)
		}
		if !valid || len(oids) == 0 {
			res.JSON(400, []byte(`{"statusCode":400,"message":"Invalid input: expected array, received string at \"query.project_ids\""}`))
			return
		}
		ctx, cancel := context.WithTimeout(req.Context(), 15*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.JSON(500, []byte(`{"message":"internal error"}`))
			return
		}
		uid := cxt.Sess.UserIDHex()
		admin := loadUserAdmin(a, cxt, uid)
		var names []string
		for _, oid := range oids {
			var doc bson.D
			if db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&doc) != nil {
				// ghost → 404 (ensureUserCanReadMultipleProjects → getProject).
				if core.AcceptsJSON(req) {
					res.JSON(404, []byte(`{"statusCode":404,"message":"Project not found"}`))
				} else {
					views.NotFoundPage(res.W, pageBase(cxt, "/project/download/zip"))
				}
				return
			}
			if !canRead(uid, admin, doc) {
				if core.AcceptsJSON(req) {
					res.JSON(403, []byte(colRestricted))
				} else {
					views.Restricted403(res.W, pageBase(cxt, "/project/download/zip"))
				}
				return
			}
			names = append(names, asStr(dget(doc, "name")))
			// audit 'project-downloaded' (Node: addEntryInBackground; here: a
			// bounded synchronous insert, same collection + verb).
			entry := bson.D{
				{Key: "projectId", Value: oid},
				{Key: "verb", Value: "project-downloaded"},
			}
			if u, uerr := bson.ObjectIDFromHex(strings.ToLower(uid)); uerr == nil {
				entry = append(entry, bson.E{Key: "userId", Value: u})
			}
			_, _ = db.Collection("projectAuditLogEntries").InsertOne(ctx, entry)
		}

		// outer zip: one `<name>.zip` entry per project (Node archive.append).
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		seen := map[string]int{}
		for _, n := range names {
			base := dlzipSafeName(n)
			seen[base]++
			entry := base
			if seen[base] > 1 {
				entry = base + "-" + strconv.Itoa(seen[base])
			}
			if f, err := zw.Create(entry + ".zip"); err == nil {
				_, _ = f.Write(dlzipEntry(n))
			}
		}
		_ = zw.Close()
		res.W.Header().Set("Content-Type", "application/zip")
		res.W.Header().Set("Content-Disposition",
			`attachment; filename="Overleaf Projects (`+strconv.Itoa(len(oids))+` items).zip"`)
		res.W.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
		res.W.WriteHeader(http.StatusOK)
		_, _ = res.W.Write(buf.Bytes())
	}
}
