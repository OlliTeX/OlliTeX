package wakatime

// Handlers (reference WakaTimeController.mjs parity, OlliTeX house shapes).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ollitex/go/libraries/ometrics"
	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

const bodyLimit = 1 << 20 // heartbeat bodies are tiny; cap anyway

// wakaLanguageForEntity — maps the active file (entity) to a WakaTime
// language name (audit 035: the upstream API requires `language`; the
// frontend only knows the file name). Unrecognized → "Other" (WakaTime's
// catch-all).
func wakaLanguageForEntity(entity string) string {
	ext := strings.ToLower(extname(entity))
	switch ext {
	case "tex", "sty":
		return "LaTeX"
	case "bib":
		return "BibTeX"
	case "typ":
		return "Typst"
	case "md", "markdown":
		return "Markdown"
	case "js", "mjs", "cjs":
		return "JavaScript"
	case "ts":
		return "TypeScript"
	case "py":
		return "Python"
	case "go":
		return "Go"
	case "sh":
		return "Shell"
	case "yaml", "yml":
		return "YAML"
	case "json":
		return "JSON"
	case "toml":
		return "TOML"
	case "html":
		return "HTML"
	case "css":
		return "CSS"
	default:
		return "Other"
	}
}

func extname(p string) string {
	i := strings.LastIndex(p, ".")
	if i < 0 || i == len(p)-1 {
		return ""
	}
	return p[i+1:]
}

// status — GET /user/wakatime/status (login).
func (s *svc) status(cxt *core.Cxt, res *core.Res) {
	ctx := cxt.Req.Context()
	if !s.enabled(ctx) {
		res.JSON(http.StatusNotFound, []byte(
			`{"message":"WakaTime integration is disabled on this instance"}`))
		return
	}
	uid := sessionUID(cxt)
	if uid == "" {
		res.JSON(http.StatusUnauthorized, []byte(`{"message":"Authentication required"}`))
		return
	}
	cr, ok, err := s.loadCreds(ctx, uid)
	if err != nil {
		res.SendStatus(http.StatusInternalServerError)
		return
	}
	if !ok {
		res.JSON(http.StatusOK, []byte(`{"connected":false}`))
		return
	}
	if verr := s.verify(ctx, cr); verr != nil {
		res.JSON(http.StatusOK, []byte(
			`{"connected":false,"apiUrl":`+qstr(cr.APIURL)+`,"error":true}`))
		return
	}
	// owner 2026-10-10 (Q2: "show it under /user-settings"): the status
	// endpoint doubles as the user's own account VIEW — where their data
	// lands + how to reach it (dashboard login + API key for local
	// WakaTime clients). All fields belong to the session user only.
	out := map[string]any{"connected": true, "apiUrl": cr.APIURL}
	if u, uerr := url.Parse(cr.APIURL); uerr == nil && u.Host != "" {
		dash := u.Scheme + "://" + u.Host
		if strings.HasPrefix(u.Path, "/wakapi") {
			dash += "/wakapi"
		}
		out["dashboardUrl"] = strings.TrimSuffix(dash, "/")
	}
	if cr.APIKey != "" {
		out["apiKey"] = cr.APIKey
	}
	// Instance-wakapi accounts (auto-provisioned from the user's email)
	// have a deterministic password the server re-derives — expose it so
	// the user can actually log into their dashboard. Security: this
	// leaks an instance-derived credential, so it is gated on the linked
	// URL BEING the instance server (exact base or its compat API prefix),
	// never on an arbitrary external URL that merely contains "/wakapi".
	if email, eerr := s.emailForUID(ctx, uid); eerr == nil && email != "" {
		base, berr := s.resolveServerBase(ctx, cxt, "")
		if berr == nil {
			nb := strings.TrimSuffix(base, "/")
			if cr.APIURL == nb || cr.APIURL == nb+"/api/compat/wakatime/v1" ||
				strings.HasPrefix(cr.APIURL, nb+"/") {
				out["username"] = strings.SplitN(email, "@", 2)[0]
				out["password"] = stableProvisionPassword(s.encryptorSecret(), email)
			}
		}
	}
	b, _ := json.Marshal(out)
	res.JSON(http.StatusOK, b)
}

// link — PUT /user/wakatime {apiUrl, apiKey} (login).
func (s *svc) link(cxt *core.Cxt, res *core.Res) {
	ctx := cxt.Req.Context()
	if !s.enabled(ctx) {
		res.JSON(http.StatusNotFound, []byte(
			`{"message":"WakaTime integration is disabled on this instance"}`))
		return
	}
	uid := sessionUID(cxt)
	if uid == "" {
		res.JSON(http.StatusUnauthorized, []byte(`{"message":"Authentication required"}`))
		return
	}
	var body struct {
		APIURL          string `json:"apiUrl"`
		APIKey          string `json:"apiKey"`
		AutoProvision   bool   `json:"auto"`
	}
	if raw, rerr := readBody(cxt.Req.Body); rerr == nil {
		_ = json.Unmarshal(raw, &body)
	}
	if body.APIKey == "" && body.AutoProvision {
		// G (owner 2026-10-09): the self-hosted wakapi flow — provision the
		// account from the session user's email (username = the email,
		// server-generated password) and store the fresh key encrypted.
		base, berr := s.resolveServerBase(ctx, cxt, "")
		if berr != nil {
			res.JSON(http.StatusOK, []byte(
				`{"ok":false,"message":"`+jsonq(berr.Error())+`"}`))
			return
		}
		email, eerr := s.emailForUID(ctx, uid)
		if eerr != nil || email == "" {
			res.JSON(http.StatusBadGateway, []byte(
				`{"ok":false,"message":"could not resolve your email for wakapi provisioning"}`))
			return
		}
		local := strings.SplitN(email, "@", 2)[0]
		pr, perr := provisionAccount(ctx, ProvisionRequest{
			ServerBase:     base,
			Username:       local,
			Email:          email,
			InstanceSecret: s.encryptorSecret(),
		})
		if perr != nil {
			res.JSON(http.StatusBadGateway, []byte(
				`{"ok":false,"message":"`+jsonq(perr.Error())+`"}`))
			return
		}
		if serr := s.storeCreds(ctx, uid, apiBaseFor(base), pr.Key); serr != nil {
			res.JSON(http.StatusInternalServerError, []byte(
				`{"ok":false,"message":"provisioned, but storing the key failed"}`))
			return
		}
		res.SendStatus(http.StatusOK)
		return
	}
	if body.APIKey == "" {
		res.JSON(http.StatusBadRequest, []byte(`{"message":"apiKey is required"}`))
		return
	}
	apiURL := strings.TrimSpace(body.APIURL)
	if apiURL == "" {
		apiURL = DefaultAPIURL
	}
	cr := wakaCreds{APIURL: apiURL, APIKey: body.APIKey}
	// audit 034-N1: reject a non-conformant endpoint NOW (400) instead of
	// dialling it and/or persisting it — the persisted URL would keep
	// receiving heartbeats for the lifetime of the account.
	if perr := checkCredsPolicy(ctx, cr); perr != nil {
		writeWakaError(res, perr)
		return
	}
	if verr := s.verify(ctx, cr); verr != nil {
		writeWakaError(res, verr)
		return
	}
	if serr := s.storeCreds(ctx, uid, apiURL, body.APIKey); serr != nil {
		res.JSON(http.StatusInternalServerError, []byte(
			`{"message":"failed to store WakaTime credentials"}`))
		return
	}
	res.SendStatus(http.StatusOK)
}

// unlink — DELETE /user/wakatime (login).
func (s *svc) unlink(cxt *core.Cxt, res *core.Res) {
	uid := sessionUID(cxt)
	if uid == "" {
		res.JSON(http.StatusUnauthorized, []byte(`{"message":"Authentication required"}`))
		return
	}
	if derr := s.delCreds(cxt.Req.Context(), uid); derr != nil {
		res.SendStatus(http.StatusInternalServerError)
		return
	}
	res.SendStatus(http.StatusOK)
}

// heartbeat — POST /project/:1/wakatime/heartbeat (login + read).
func (s *svc) heartbeat(cxt *core.Cxt, res *core.Res) {
	s.handleHeartbeats(cxt, res, false)
}

// heartbeatBulk — POST /project/:1/wakatime/heartbeats/bulk (login + read).
func (s *svc) heartbeatBulk(cxt *core.Cxt, res *core.Res) {
	s.handleHeartbeats(cxt, res, true)
}

func (s *svc) handleHeartbeats(cxt *core.Cxt, res *core.Res, bulk bool) {
	ctx := cxt.Req.Context()
	// Candidate F (owner report 2026-10-07): heartbeats are fire-and-forget.
	// When the instance feature is OFF or the user has not linked an account,
	// answer a quiet 204 No Content no-op instead of a 404/400 error — the
	// tracker already swallows failures, but a non-2xx response still surfaces
	// as console/network noise on every editor keystroke (the owner's "403/
	// 404" wakatime complaint). 204 = "received, nothing to record".
	if !s.enabled(ctx) {
		res.SendStatus(http.StatusNoContent)
		return
	}
	gate := s.gateProject(cxt)
	if gate.err != nil {
		res.SendStatus(http.StatusInternalServerError)
		return
	}
	if gate.blocked || gate.unauthorized {
		res.JSON(http.StatusForbidden, []byte(`{"message":"restricted"}`))
		return
	}
	if !gate.found {
		res.JSON(http.StatusNotFound, []byte(`{"message":"project not found"}`))
		return
	}
	raw, rerr := readBody(cxt.Req.Body)
	if rerr != nil {
		res.JSON(http.StatusBadRequest, []byte(`{"message":"bad request body"}`))
		return
	}
	var hbs []map[string]any
	if bulk {
		if jerr := json.Unmarshal(raw, &hbs); jerr != nil || len(hbs) == 0 {
			res.SendStatus(http.StatusNoContent)
			return
		}
		if len(hbs) > maxBulkHeartbeats {
			res.JSON(http.StatusBadRequest, []byte(`{"message":"too many heartbeats"}`))
			return
		}
	} else {
		var one map[string]any
		if jerr := json.Unmarshal(raw, &one); jerr != nil {
			res.JSON(http.StatusBadRequest, []byte(`{"message":"bad heartbeat payload"}`))
			return
		}
		hbs = []map[string]any{one}
	}
	cr, linked, lerr := s.loadCreds(ctx, gate.uid)
	if lerr != nil {
		res.SendStatus(http.StatusInternalServerError)
		return
	}
	// Candidate F: not linked = nothing to record → quiet 204 no-op (not a
	// 400 error the tracker would have to swallow as noise).
	if !linked {
		res.SendStatus(http.StatusNoContent)
		return
	}
	// server-side project fill (reference: the client never supplies
	// `project`; it is set from the project name).
	//
	// audit 035 (Wakapi v2.18 live contract, verified against a running
	// ghcr.io/muety/wakapi): the WakaTime v1 endpoint requires `language`
	// and `category` as STRINGS (arrays are rejected), and the frontend
	// only sends `{entity,time,isWrite}`. Build the canonical WakaTime
	// payload here — works against both wakatime.com and self-hosted
	// Wakapi — and drop fields the API does not accept (`isWrite`).
	canon := func(m map[string]any) map[string]any {
		out := map[string]any{"category": "Development"}
		if p, ok := m["project"].(string); ok && p != "" {
			out["project"] = p
		}
		if t, ok := m["time"].(float64); ok {
			out["time"] = int64(t)
		} else {
			out["time"] = time.Now().Unix()
		}
		entity, _ := m["entity"].(string)
		lang := wakaLanguageForEntity(entity)
		if l, ok := m["language"].(string); ok && l != "" {
			lang = l
		}
		out["language"] = lang
		return out
	}
	for i, hb := range hbs {
		hb = canon(hb)
		hb["project"] = gate.projectName
		hbs[i] = hb
	}
	var ferr error
	if bulk {
		ferr = s.wakaBulk(ctx, cr, hbs)
	} else {
		ferr = s.wakaOne(ctx, cr, hbs[0])
	}
	if ferr != nil {
		writeWakaError(res, ferr)
		return
	}
	ometrics.Inc("wakatime-heartbeat", map[string]any{"bulk": bulk})
	res.SendStatus(http.StatusAccepted)
}

// summary — GET /project/:1/wakatime/summary (login + read) →
// {connected, totalSeconds, rangeDays} | {connected:false} (reference widget
// contract; API errors never break the settings page).
func (s *svc) summary(cxt *core.Cxt, res *core.Res) {
	ctx := cxt.Req.Context()
	if !s.enabled(ctx) {
		res.JSON(http.StatusNotFound, []byte(
			`{"message":"WakaTime integration is disabled on this instance"}`))
		return
	}
	gate := s.gateProject(cxt)
	if gate.err != nil {
		res.SendStatus(http.StatusInternalServerError)
		return
	}
	if gate.blocked || gate.unauthorized {
		res.JSON(http.StatusForbidden, []byte(`{"message":"restricted"}`))
		return
	}
	if !gate.found {
		res.JSON(http.StatusNotFound, []byte(`{"message":"project not found"}`))
		return
	}
	cr, linked, lerr := s.loadCreds(ctx, gate.uid)
	if lerr != nil {
		res.SendStatus(http.StatusInternalServerError)
		return
	}
	if !linked {
		res.JSON(http.StatusOK, []byte(`{"connected":false}`))
		return
	}
	total, serra := s.wakaSummary(ctx, cr, gate.projectName, 7)
	if serra != nil {
		res.JSON(http.StatusOK, []byte(`{"connected":false}`))
		return
	}
	res.JSON(http.StatusOK, []byte(
		`{"connected":true,"totalSeconds":`+itoa(total)+`,"rangeDays":7}`))
}

// userSummaryHandler — GET /user/wakatime/summary (login) →
// {connected, totalSeconds, rangeDays} | {connected:false} — the user's own
// CROSS-PROJECT time (all projects, last 14 days). owner 2026-10-10 (Q2):
// the /user-settings WakaTime tab renders this (their dashboard at
// {dashboardUrl} remains the full per-project view). API errors never break
// the settings page (reference widget contract).
func (s *svc) userSummaryHandler(cxt *core.Cxt, res *core.Res) {
	ctx := cxt.Req.Context()
	if !s.enabled(ctx) {
		res.JSON(http.StatusNotFound, []byte(
			`{"message":"WakaTime integration is disabled on this instance"}`))
		return
	}
	uid := sessionUID(cxt)
	if uid == "" {
		res.JSON(http.StatusUnauthorized, []byte(`{"message":"Authentication required"}`))
		return
	}
	cr, ok, err := s.loadCreds(ctx, uid)
	if err != nil {
		res.SendStatus(http.StatusInternalServerError)
		return
	}
	if !ok {
		res.JSON(http.StatusOK, []byte(`{"connected":false}`))
		return
	}
	total, terr := s.wakaUserSummary(ctx, cr, 14)
	if terr != nil {
		res.JSON(http.StatusOK, []byte(`{"connected":false}`))
		return
	}
	res.JSON(http.StatusOK, []byte(
		`{"connected":true,"totalSeconds":`+itoa(total)+`,"rangeDays":14}`))
}

// ---- shared helpers --------------------------------------------------------

// gateResult — the outcome of the project authz gate.
type gateResult struct {
	uid          string
	projectName  string
	found        bool
	blocked      bool
	unauthorized bool
	isAdmin      bool
	err          error
}

// gateProject — login + project exists + requester can read (projectlist
// access parity: owner / collaborator / reviewer / readOnly / public /
// site-admin) + project name for the WakaTime "project" field.
func (s *svc) gateProject(cxt *core.Cxt) gateResult {
	if s.gateOverride != nil {
		return s.gateOverride(cxt)
	}
	ctx := cxt.Req.Context()
	pid := cxt.Params["1"]
	g := gateResult{}
	uid := sessionUID(cxt)
	g.uid = uid
	if uid == "" {
		g.unauthorized = true
		return g
	}
	if s.a == nil || s.a.Mongo == nil {
		g.err = errors.New("wakatime: app mongo not wired")
		return g
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db, dberr := s.a.Mongo.DB(ctx)
	if dberr != nil || db == nil {
		g.err = fmt.Errorf("wakatime: mongo db: %w", dberr)
		return g
	}
	oid, oerr := bson.ObjectIDFromHex(pid)
	if oerr != nil {
		g.err = oerr
		return g
	}
	// user doc: blocked + isAdmin (CE shape, projectlist parity)
	var u bson.D
	if uerr := db.Collection("users").FindOne(ctx,
		bson.D{{Key: "_id", Value: oid}}).Decode(&u); uerr == nil {
		if dgetBool(u, "blocked") {
			g.blocked = true
			return g
		}
		g.isAdmin = dgetBool(u, "isAdmin")
	}
	var p bson.D
	if perr := db.Collection("projects").
		FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&p); perr != nil {
		if errors.Is(perr, mongo.ErrNoDocuments) {
			g.found = false
			return g
		}
		g.err = perr
		return g
	}
	g.found = true
	if nm, ok := dgetString(p, "name"); ok {
		g.projectName = nm
	}
	if !canReadProject(uid, g.isAdmin, p) {
		g.unauthorized = true
	}
	return g
}

// canReadProject mirrors canUserReadProject (projectlist/access.go semantics).
func canReadProject(uid string, isAdmin bool, p bson.D) bool {
	if uid == "" {
		return false
	}
	if dgetStringVal(p, "owner_ref") == uid ||
		inListStr(p, "collab_refs", uid) || inListStr(p, "collaborator_refs", uid) || inListStr(p, "reviewer_refs", uid) ||
		inListStr(p, "readOnly_refs", uid) {
		return true
	}
	pal := dgetStringVal(p, "publicAccesLevel")
	if pal == "tokenBased" && (inListStr(p, "tokenAccessReadAndWrite_refs", uid) ||
		inListStr(p, "tokenAccessReadOnly_refs", uid)) {
		return true
	}
	if pal == "readOnly" || pal == "readAndWrite" {
		return true
	}
	return isAdmin
}

func dgetBool(d bson.D, key string) bool {
	for _, e := range d {
		if e.Key == key {
			if b, ok := e.Value.(bool); ok {
				return b
			}
		}
	}
	return false
}

func dgetString(d bson.D, key string) (string, bool) {
	for _, e := range d {
		if e.Key == key {
			if s, ok := e.Value.(string); ok {
				return s, true
			}
		}
	}
	return "", false
}

func strOrOidHex(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bson.ObjectID:
		return x.Hex()
	}
	return ""
}

func dgetStringVal(d bson.D, key string) string {
	for _, e := range d {
		if e.Key == key {
			return strOrOidHex(e.Value)
		}
	}
	return ""
}

func inListStr(p bson.D, key string, uid string) bool {
	for _, e := range p {
		if e.Key != key {
			continue
		}
		l, ok := e.Value.(bson.A)
		if !ok {
			continue
		}
		for _, v := range l {
			if strOrOidHex(v) == uid {
				return true
			}
		}
	}
	return false
}

func sessionUID(cxt *core.Cxt) string {
	if cxt.Sess == nil {
		return ""
	}
	_, uid := core.PassportUser(cxt.Sess)
	return uid
}

func readBody(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, bodyLimit))
}

func qstr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
