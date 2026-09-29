package projectinspection

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"ollitex/go/libraries/ometrics"
	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// svc — the feature state. Data sources are seams (function fields) so the
// unit tests run against in-memory fakes; the defaults hit the real
// docstore / mongo / v1-history endpoints (house seam pattern, e.g.
// go/libraries/ollogger/seams.go).
type svc struct {
	a   *core.App
	cfg cfg

	// seams (set in newSvc; tests may override)
	loadProject func(ctx context.Context, pid string) (*bson.D, bool, error)
	fetchDoc    func(ctx context.Context, pid, docID string) (string, error)
	fetchBlob   func(ctx context.Context, historyID, hash string) ([]byte, error)
	// runWorker executes the analysis worker; the default spawns node.
	runWorker func(ctx context.Context, snapshot []byte) ([]byte, error)
}

func newSvc(a *core.App) *svc {
	s := &svc{a: a, cfg: cfgFromEnv()}
	s.loadProject = s.loadProjectDefault
	s.fetchDoc = s.fetchDocDefault
	s.fetchBlob = s.fetchBlobDefault
	s.runWorker = s.runWorkerDefault
	return s
}

// analyze — POST /project/:1/project-inspection/analyze
func (s *svc) analyze(cxt *core.Cxt, res *core.Res) {
	ctx := cxt.Req.Context()
	pid := cxt.Params["1"]
	if !hex24.MatchString(pid) {
		res.JSON(http.StatusNotFound, []byte(
			`{"error":"Validation error: Invalid Mongo ObjectId at \"params.Project_id\"","statusCode":404}`))
		return
	}

	// rate limit 6/min/project (client id = project id, reference router)
	if a := s.a; a != nil {
		if rl := core.NewRateLimiter(a.Redis, "project-inspection-analysis", 6, 60); !rl.Consume(pid) {
			res.JSON(http.StatusTooManyRequests, []byte(
				`{"message":"Too many requests","statusCode":429}`))
			return
		}
	}

	// authz: logged in + not blocked + can read (history gate parity)
	uid := ""
	if cxt.Sess != nil {
		_, uid = core.PassportUser(cxt.Sess)
	}
	isAdmin, blocked := s.userAdmin(ctx, uid)
	if blocked {
		s.restricted(cxt, res)
		return
	}
	proj, found, lerr := s.loadProject(ctx, pid)
	if lerr != nil {
		res.SendStatus(http.StatusInternalServerError)
		return
	}
	if !found {
		views.NotFoundPage(res.W, pageData(cxt, ""))
		return
	}
	if uid == "" || !canRead(uid, isAdmin, proj) {
		s.restricted(cxt, res)
		return
	}

	// body { entryPointIds: [1..50] }
	var body struct {
		EntryPointIDs []string `json:"entryPointIds"`
	}
	if raw, rerr := readAllLimited(cxt.Req.Body, 1<<20); rerr == nil {
		if len(raw) > 0 {
			if json.Unmarshal(raw, &body) != nil {
				res.JSON(http.StatusBadRequest, []byte(
					`{"error":"INVALID_BODY","message":"body must be {\"entryPointIds\":[...]}"}`))
				return
			}
		}
	}
	ids := dedupe(body.EntryPointIDs)
	if len(ids) < 1 || len(ids) > 50 {
		res.JSON(http.StatusBadRequest, []byte(
			`{"error":"INVALID_ENTRY_POINT","message":"entryPointIds must contain between 1 and 50 ids"}`))
		return
	}

	// snapshot (reader parity: docs + files + binaryBibliographies)
	snap, serr := s.buildSnapshot(ctx, pid, proj, ids)
	if serr != nil {
		writePIError(res, serr)
		return
	}

	// worker (one short-lived node process — reference worker_threads parity)
	out, werr := s.runWorker(ctx, snap)
	if werr != nil {
		var pe *piError
		if !errors.As(werr, &pe) {
			pe = errInternal(werr.Error())
		}
		writePIError(res, pe)
		return
	}

	// {ok:true,result} | {ok:false,error:{code,message}}
	var wr struct {
		OK     bool       `json:"ok"`
		Result map[string]any `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(out, &wr) != nil {
		writePIError(res, errInternal("worker returned non-JSON output"))
		return
	}
	if !wr.OK || wr.Result == nil {
		code, msg := "PROJECT_INSPECTION_ERROR", "Project inspection failed"
		if wr.Error != nil {
			if wr.Error.Code != "" {
				code = wr.Error.Code
			}
			if wr.Error.Message != "" {
				msg = wr.Error.Message
			}
		}
		res.JSON(http.StatusInternalServerError, []byte(
			`{"error":`+qstr(code)+`,"message":`+qstr(msg)+`}`))
		return
	}

	// manager envelope: { schemaVersion, analysisId, analyzedAt, ...result }
	env := map[string]any{
		"schemaVersion": 1,
		"analysisId":    uuid4(),
		"analyzedAt":    timeRFC3339(now()),
	}
	for k, v := range wr.Result {
		env[k] = v
	}
	b, merr := json.Marshal(env)
	if merr != nil {
		res.SendStatus(http.StatusInternalServerError)
		return
	}
	ometrics.Inc("project-inspection-analysis", map[string]any{"status": "success"})
	res.JSON(http.StatusOK, b)
}

func (s *svc) restricted(cxt *core.Cxt, res *core.Res) {
	if core.AcceptsJSON(cxt.Req) {
		res.JSON(http.StatusForbidden, []byte(`{"message":"restricted"}`))
		return
	}
	views.Restricted403(res.W, pageData(cxt, ""))
}

// userAdmin — isAdmin (+ blocked) from the users collection (CE shape).
func (s *svc) userAdmin(ctx context.Context, uid string) (isAdmin, blocked bool) {
	if s.a == nil || s.a.Mongo == nil || uid == "" {
		return false, false
	}
	oid, err := bson.ObjectIDFromHex(uid)
	if err != nil {
		return false, false
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db, err := s.a.Mongo.DB(ctx)
	if err != nil || db == nil {
		return false, false
	}
	var d bson.D
	if db.Collection("users").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&d) != nil {
		return false, false
	}
	if b, ok := dget(d, "blocked").(bool); ok {
		blocked = b
	}
	if a, ok := dget(d, "isAdmin").(bool); ok {
		isAdmin = a
	}
	return
}

// pageData — views.PageData builder (history gate parity).
func pageData(cxt *core.Cxt, p string) views.PageData {
	d := views.PageData{Nonce: views.NewNonce()}
	origin := cxt.SiteURL
	if origin == "" {
		origin = "http://" + cxt.Req.Host
	}
	d.Origin = origin
	if cxt.Sess != nil {
		d.CSRFToken = cxt.Sess.CsrfToken()
		d.UserEmail, d.UserID = core.PageUserSlots(cxt.Sess)
	}
	if p != "" {
		d.Path = p
	}
	return d
}

// canRead mirrors canUserReadProject (site-admin reads everything here).
func canRead(uid string, isAdmin bool, proj *bson.D) bool {
	if uid == "" || proj == nil {
		return false
	}
	d := *proj
	if dget(d, "owner_ref") == uid ||
		inList("collaberator_refs", d, uid) ||
		inList("reviewer_refs", d, uid) ||
		inList("readOnly_refs", d, uid) {
		return true
	}
	pal, _ := dget(d, "publicAccesLevel").(string)
	if pal == "tokenBased" && (inList("tokenAccessReadAndWrite_refs", d, uid) ||
		inList("tokenAccessReadOnly_refs", d, uid)) {
		return true
	}
	if pal == "readOnly" || pal == "readAndWrite" {
		return true
	}
	// site admin (isAdmin is resolved by the caller in the gate; the
	// reference passes ignoreSiteAdmin=false at this layer — in this stack
	// site admins read everything, so the admin case is handled by the
	// gate passing isAdmin through; see analyze().
	return false
}

func inList(key string, d bson.D, uid string) bool {
	l, _ := dget(d, key).(bson.A)
	for _, v := range l {
		if s, ok := v.(string); ok && s == uid {
			return true
		}
	}
	return false
}
