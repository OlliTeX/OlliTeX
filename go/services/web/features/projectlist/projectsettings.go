package projectlist

// P4.10c — project settings writes (Node ProjectController.updateProjectSettings,
// 2026-09-30 live audit item 027: "Error renaming project — Please try again
// in a moment").
//
// Node oracle (services/web/app/src/Features/Project/ProjectController.mjs
// strict schema + router.mjs):
//
//	POST /project/:Project_id/settings   (ensureUserCanWriteProjectSettings ->
//	                                         updateProjectSettings -> 204)
//
// Body (z.strictObject; ALL fields optional; unknown keys -> 400):
//
//	compiler                 string
//	imageName                string
//	png2pdf                  bool
//	mainBibliographyDocId    objectId
//	name                     string
//	rootDocId                objectId
//	spellCheckLanguage       string
//	referenceFormat          "bibtex" | "biblatex"
//	grammarPicky             bool
//
// Gate (canUserWriteProjectSettings): OWNER, READ_AND_WRITE collaborator, or
// site admin ('modify-project-setting' capability — CE default admin set).
// Public access levels are ignored. Each provided field $sets the same-named
// project doc field (the Node per-field setters all do exactly that).
// Zero provided fields -> no-op 204 (Node: no setter runs).

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var settingsPat = regexp.MustCompile(`^/[Pp]roject/([^/]+)/settings$`)

var settingsFields = map[string]string{
	"compiler":              "string",
	"imageName":             "string",
	"png2pdf":               "bool",
	"name":                  "string",
	"spellCheckLanguage":    "string",
	"mainBibliographyDocId": "oid",
	"rootDocId":             "oid",
	"referenceFormat":       "enum",
	"grammarPicky":          "bool",
}

// routesProjectsSettings registers the settings route (added to the
// projectlist Feature list).
func routesProjectsSettings(a *core.App) []core.Route {
	return []core.Route{
		{Method: "POST", Pattern: settingsPat, Handler: projectSettingsHandler(a)},
	}
}

type settingsVa struct {
	msg  string
	path string
	code int
}

func (v settingsVa) bytes() []byte {
	// Node envelope: {"error":"Validation error: <msg> at \"<path>\"","statusCode":N}
	// (inner quotes escaped; <msg> may itself carry escaped quotes).
	code := "400"
	if v.code == 404 {
		code = "404"
	}
	return []byte(`{"error":"Validation error: ` + v.msg + ` at \"` + v.path + `\"","statusCode":` + code + `}`)
}

func jsonKindName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		return "number"
	case []any:
		return "array"
	}
	return "object"
}

func projectSettingsHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		uid := gatedLogin(cxt, res)
		if uid == "" {
			return
		}
		oid, ok := paramProject(cxt.Params["1"], res)
		if !ok {
			return
		}
		doc, lerr := loadProjectFull(a, cxt, oid)
		if lerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		if doc == nil {
			views.NotFoundPage(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			return
		}
		if !entCanWrite(strings.ToLower(uid), *doc) && !loadUserAdmin(a, cxt, uid) {
			if core.AcceptsJSON(cxt.Req) {
				res.JSON(403, []byte(colRestricted))
			} else {
				views.Restricted403(res.W, pageBase(cxt, strings.TrimPrefix(cxt.Req.URL.Path, "/")))
			}
			return
		}

		raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		body := map[string]any{}
		if len(raw) > 0 {
			var parsed map[string]any
			if err := json.Unmarshal(raw, &parsed); err != nil {
				res.JSON(400, settingsVa{"Invalid JSON input", `body`, 400}.bytes())
				return
			}
			if parsed != nil {
				body = parsed
			}
		}

		update := bson.D{}
		for k, v := range body {
			kind, known := settingsFields[k]
			if !known {
				res.JSON(400, settingsVa{"Unrecognized key: " + q(k), `body`, 400}.bytes())
				return
			}
			pathBody := `body`
			switch kind {
			case "string":
				s, isS := v.(string)
				if !isS {
					res.JSON(400, settingsVa{"expected string, received " + jsonKindName(v), pathBody, 400}.bytes())
					return
				}
				update = append(update, bson.E{Key: k, Value: s})
			case "bool":
				b, isB := v.(bool)
				if !isB {
					res.JSON(400, settingsVa{"expected boolean, received " + jsonKindName(v), pathBody, 400}.bytes())
					return
				}
				update = append(update, bson.E{Key: k, Value: b})
			case "oid":
				hex, isS := v.(string)
				if !isS || !oid24.MatchString(hex) {
					res.JSON(400, settingsVa{"expected string, received " + jsonKindName(v), pathBody, 400}.bytes())
					return
				}
				poid, _ := bson.ObjectIDFromHex(hex)
				update = append(update, bson.E{Key: k, Value: poid})
			case "enum":
				s, isS := v.(string)
				if !isS || (s != "bibtex" && s != "biblatex") {
					res.JSON(400, settingsVa{"Invalid enum value: expected \"bibtex\"|\"biblatex\", received " + q(s), pathBody, 400}.bytes())
					return
				}
				update = append(update, bson.E{Key: k, Value: s})
			}
		}

		mdb, dberr := a.Mongo.DB(cxt.Req.Context())
		if dberr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		_, uerr := mdb.Collection("projects").
			UpdateByID(cxt.Req.Context(), oid, bson.D{{Key: "$set", Value: update}})
		if uerr != nil {
			res.JSON(500, []byte("internal error"))
			return
		}
		res.W.WriteHeader(204)
	}
}

func q(s string) string {
	return `"` + s + `"`
}

var oid24 = regexp.MustCompile(`^[0-9a-fA-F]{24}$`)
