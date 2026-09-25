package emailtemplates

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"regexp"
	"time"

	"ollitex/go/services/web/core"
)

// routes — /hub admin API (remember.md item 3: "Manage the email template
// under /hub admin settings … Text areas with save and reset to default").
//
// Follows the /api/hub-theme + /api/hub/config core pattern: core mustCsrf
// (middleware) → a.RequireSiteAdmin → store op → JSON.
//
//	GET    /api/hub/email-templates
//	  → {version, templates:[{name,label,help,variables,
//	     default:{subject,text,html},current:{...},
//	     overridden:[...],updatedAt,updatedBy}]}
//	     (html = "" for the htmlFromText slots — no editable HTML part)
//
//	PUT    /api/hub/email-templates/<name>
//	  body: any subset of {"subject","text","html"};
//	    - a NON-EMPTY value → that field is saved (override),
//	    - an EMPTY value    → that field resets to its default,
//	    - an omitted key    → that field is unchanged.
//	  → 200 {ok:true, overridden:[fields]}
//	  → 400 {"error":"…"}   (unknown variable — "subject: unknown variables
//	                        x (allowed: app, link)"; html on a slot with no
//	                        editable HTML part; bad JSON → bare {})
//	  → 404 unknown slot
//
//	DELETE /api/hub/email-templates/<name>   (reset the whole slot)
//	  → 200 {ok:true, overridden:[]}
var putSlotRe = regexp.MustCompile(`^/api/hub/email-templates/([a-zA-Z0-9_-]+)$`)

// slotJSON — one entry of the GET list.
type slotJSON struct {
	Name       string   `json:"name"`
	Label      string   `json:"label"`
	Help       string   `json:"help"`
	Variables  []string `json:"variables"`
	Default    partJSON `json:"default"`
	Current    partJSON `json:"current"`
	Overridden []string `json:"overridden"`
	UpdatedAt  string   `json:"updatedAt"`
	UpdatedBy  string   `json:"updatedBy"`
}

type partJSON struct {
	Subject string `json:"subject"`
	Text    string `json:"text"`
	HTML    string `json:"html"`
}

// Feature — registers the API (logged-in + site-admin; CSRF on PUT/DELETE
// is applied by core like every other non-NoLogin mutation).
func Feature(a *core.App, store Store) core.Feature {
	if store == nil {
		store = StoreFor(a)
	}
	return core.Feature{
		Name: "emailtemplates",
		Routes: []core.Route{
			{Method: "GET", Path: "/api/hub/email-templates", Handler: hList(a, store)},
			{Method: "PUT", Pattern: putSlotRe, Handler: hPut(a, store)},
			{Method: "DELETE", Pattern: putSlotRe, Handler: hReset(a, store)},
		},
	}
}

// StoreFor — the production store over the app's Mongo (nil-safe: no
// Mongo → the in-memory store so non-DB runs stay functional).
func StoreFor(a *core.App) Store {
	if a != nil && a.Mongo != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if db, err := a.Mongo.DB(ctx); err == nil && db != nil {
			return NewMongoStore(db, Collection)
		}
	}
	return NewMapStore()
}

// ctx2 — bounded work context.
func ctx2(cxt *core.Cxt) (context.Context, context.CancelFunc) {
	return context.WithTimeout(cxt.Req.Context(), 5*time.Second)
}

// RenderFor — the call-site entry point: resolve the slot's current
// effective templates (app-store override-or-default) and interpolate.
// a may be nil (no app context → defaults only).
func RenderFor(a *core.App, slot string, vars map[string]string) (RenderResult, error) {
	store := StoreFor(a)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ov, err := store.GetOverride(ctx, slot)
	if err != nil {
		// a storage failure must not block the mail path: fall back to
		// defaults (availability-first, same policy as the consent
		// feature's local-write-first rule).
		ov = Override{}
	}
	return Render(Registry, map[string]Override{slot: ov}, slot, vars)
}

// hList — GET handler.
func hList(a *core.App, store Store) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		ctx, cancel := ctx2(cxt)
		defer cancel()
		overrides, err := store.LoadAll(ctx)
		if err != nil {
			res.JSON(500, []byte(`{"error":"template store unavailable"}`))
			return
		}
		type listResp struct {
			Version   int        `json:"version"`
			Templates []slotJSON `json:"templates"`
		}
		lr := listResp{Version: 1, Templates: make([]slotJSON, 0, len(Registry))}
		for _, t := range Registry {
			ov := overrides[t.Name]
			cur := partJSON{
				Subject: orDefault(ov.Subject, t.Subject),
				Text:    orDefault(ov.Text, t.Text),
				HTML:    orDefault(ov.HTML, t.HTML),
			}
			def := partJSON{Subject: t.Subject, Text: t.Text, HTML: t.HTML}
			f := overriddenList(ov)
			lr.Templates = append(lr.Templates, slotJSON{
				Name:       t.Name,
				Label:      t.Label,
				Help:       t.Help,
				Variables:  t.Vars,
				Default:    def,
				Current:    cur,
				Overridden: f,
				UpdatedAt:  ov.UpdatedAt,
				UpdatedBy:  ov.UpdatedBy,
			})
		}
		b, err := json.Marshal(lr)
		if err != nil {
			res.JSON(500, []byte(`{"error":"serialize failed"}`))
			return
		}
		res.JSON(200, b)
	}
}

// putBody — the PUT fields (any subset of subject/text/html).
type putBody struct {
	Subject *string `json:"subject"`
	Text    *string `json:"text"`
	HTML    *string `json:"html"`
}

// hPut — PUT <name> (save fields; empty value = reset that field).
func hPut(a *core.App, store Store) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		name := cxt.Params["1"]
		slot, okSlot := Lookup(name)
		if !okSlot {
			res.JSON(404, []byte(`{"error":"unknown email template `+jsEsc(name)+`"}`))
			return
		}
		raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		var body putBody
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				res.BareWrite(400, []byte(`{}`)) // express.json syntax (pinned P6.1)
				return
			}
		}
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		ctx, cancel := ctx2(cxt)
		defer cancel()

		// merge over the EXISTING override (omitted keys keep their value;
		// provided keys are replaced — "" = back to default).
		cur, _ := store.GetOverride(ctx, name)
		next := cur
		if body.Subject != nil {
			next.Subject = *body.Subject
		}
		if body.Text != nil {
			next.Text = *body.Text
		}
		if body.HTML != nil {
			next.HTML = *body.HTML
		}
		if body.HTML != nil && slot.HTML == "" {
			res.JSON(400, []byte(`{"error":"html: this template has no editable HTML part"}`))
			return
		}
		// (subject/text can never end up empty: a provided "" resets the
		// field to its non-empty default, so the effective mail always has
		// both part text and a subject.)
		// validate the EFFECTIVE templates (override-or-default) so a bad
		// save can never ship a broken mail.
		eff := map[string]Override{name: next}
		if _, err := Render(Registry, eff, name, map[string]string{}); err != nil {
			res.JSON(400, []byte(`{"error":"`+jsEsc(err.Error())+`"}`))
			return
		}
		// fully reset? (all override fields empty = clear the document)
		if next.Subject == "" && next.Text == "" && next.HTML == "" {
			if err := store.ClearOverride(ctx, name); err != nil {
				res.JSON(500, []byte(`{"error":"`+jsEsc(name)+`: save failed"}`))
				return
			}
			res.JSON(200, []byte(`{"ok":true,"overridden":[]}`))
			return
		}
		if err := store.SetOverride(ctx, name, next); err != nil {
			log.Printf("emailtemplates: save override %s: %v", name, err)
			res.JSON(500, []byte(`{"error":"`+jsEsc(name)+`: save failed"}`))
			return
		}
		f := overriddenList(next)
		b, _ := json.Marshal(map[string]any{"ok": true, "overridden": f})
		res.JSON(200, b)
	}
}

// hReset — DELETE <name> (reset the whole slot to defaults).
func hReset(a *core.App, store Store) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		name := cxt.Params["1"]
		if _, okSlot := Lookup(name); !okSlot {
			res.JSON(404, []byte(`{"error":"unknown email template `+jsEsc(name)+`"}`))
			return
		}
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		ctx, cancel := ctx2(cxt)
		defer cancel()
		if err := store.ClearOverride(ctx, name); err != nil {
			res.JSON(500, []byte(`{"error":"`+jsEsc(name)+`: reset failed"}`))
			return
		}
		res.JSON(200, []byte(`{"ok":true,"overridden":[]}`))
	}
}

// orDefault — v when set, else d.
func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// overriddenList — which fields are overridden (stable order).
func overriddenList(ov Override) []string {
	out := []string{}
	if ov.Subject != "" {
		out = append(out, "subject")
	}
	if ov.Text != "" {
		out = append(out, "text")
	}
	if ov.HTML != "" {
		out = append(out, "html")
	}
	return out
}

// jsEsc — JSON-escape for error strings (the hub pattern).
func jsEsc(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}
