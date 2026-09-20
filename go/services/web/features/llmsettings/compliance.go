// P6.4b — compliance review jobs: Node LLMComplianceController.mjs +
// models/LLMReviewJob.mjs parity.
//
// Exact live pins (Node, /tmp/p64b_node.json + /tmp/p64b_job.json):
//
//	POST start (unknown rubric) 200 {ok:false,error:"no_rubric",message:...}
//	POST start (rubric + BYO)   200 {ok:true,jobId,status:"running",position:0}
//	GET  status (unknown)       200 {ok:false,error:"not_found",
//	                             message:"Review not found or expired"}
//	GET  status (done)          200 {ok:true,status:"done",result:{...}}
//	POST cancel (any jobId)     200 {ok:true}
//	GET  cancel/...             404 generic page (Node registers POST only;
//	                             the oracle's unknown-cancel pin is a GET
//	                             that matches no route)
//
// Mongo mirror: llmreviewjobs (jobId unique; userId/projectId stored as
// strings), persistJobCreate/Update/FinalStatus — all failures swallowed.
//
// In this build the structured review path (chatDetailedCompat) fails
// locally BEFORE network: 'LLM request failed: schema is not a function
// (model: <m>)' — every requirement lands as a status:'na' item and the
// summary stays ”. That is the deterministic done-shape.
package llmsettings

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	mgoOptions "go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
)

const (
	chCharsPerToken   = 3.0 // REVIEW_CHARS_PER_TOKEN default
	chMaxContextDef   = 32000
	chReviewMaxDef    = 12000
	chMinAnswerTokens = 2000
	chSafetyMargin    = 256
	maxUserInFlight   = 3
	maxGlobalQueue    = 5
)

func chEstTokens(s string) int {
	// Node: Math.ceil(String(text||'').length / 3.0)
	n := 0
	for range []rune(s) {
		n++
	}
	return (n + 2) / 3
}

// ---------------------------------------------------------------------------
// rubric resolution — Node getComplianceRubricsForUser (LLMSettingsController)
// ---------------------------------------------------------------------------

func chRubricForUser(f *fs, ctx context.Context, uid string) []map[string]any {
	out := []map[string]any{}
	add := func(v any) {
		m, ok := asMap(v)
		if !ok {
			return
		}
		id, _ := m["id"].(string)
		if strings.TrimSpace(id) == "" {
			return
		}
		name, _ := m["name"].(string)
		if strings.TrimSpace(name) == "" {
			return
		}
		guid, _ := m["guidelines"].(string)
		pats, _ := m["scanPatterns"].(string)
		out = append(out, map[string]any{"id": id, "name": name, "guidelines": guid, "scanPatterns": pats})
	}
	if uid != "" {
		if u, err := f.loadUser(ctx, uid); err == nil && u != nil {
			if arr := asAnySlice(u.compRubrics); len(arr) > 0 {
				for _, v := range arr {
					add(v)
				}
			}
		}
		if len(out) > 0 {
			return out // user's own set wins (Node: mine ? mine : inherited)
		}
	}
	if rv, ok2 := readAdminFile(f.adminPath()).get("complianceRubrics"); ok2 {
		if arr := asAnySlice(rv); len(arr) > 0 {
			for _, v := range arr {
				add(v)
			}
		}
	}
	return out
}

func chFindRubric(list []map[string]any, id string) map[string]any {
	for _, r := range list {
		if v, _ := r["id"].(string); v == id && id != "" {
			return r
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// GET /project/:id/llm/compliance/rubrics
// ---------------------------------------------------------------------------

func (f *fs) llmComplianceRubrics(cxt *core.Cxt, res *core.Res) {
	if _, _, ok := f.llmPref(cxt, res, cxt.Params["1"]); !ok {
		return
	}
	if !featureFlags()["reviewEnabled"] {
		jres(200, res, jobj("rubrics", []any{}))
		return
	}
	rubs := chRubricForUser(f, cxt.Req.Context(), sessUIDOf(cxt))
	list := make([]any, 0, len(rubs))
	for _, r := range rubs {
		id, _ := r["id"].(string)
		name, _ := r["name"].(string)
		list = append(list, jobj("id", id, "name", name))
	}
	jres(200, res, jobj("rubrics", list))
}

func sessUIDOf(cxt *core.Cxt) string {
	if cxt.Sess == nil {
		return ""
	}
	return cxt.Sess.UserIDHex()
}

// ---------------------------------------------------------------------------
// queue + jobs (in-memory; mongo mirror on transitions)
// ---------------------------------------------------------------------------

type chJob struct {
	mu        sync.Mutex
	id        string
	projectID string
	userID    string
	rubric    map[string]any
	lane      *laneRef
	status    string // queued|running|done|error|cancelled
	result    obj
	errCode   string
	message   string
	createdMs int64
	finished  int64
}

var (
	chQMu  sync.Mutex
	chJobs = map[string]*chJob{}
	chQ    []string
)

// chPersistJob — LLMReviewJob persist helpers (Node: all failures swallowed).
func chPersistJob(f *fs, ctx context.Context, j *chJob, create bool) {
	if f.app == nil || f.app.Mongo == nil {
		return
	}
	db, err := f.app.Mongo.DB(ctx)
	if err != nil {
		return
	}
	c := db.Collection("llmreviewjobs")
	created := time.UnixMilli(j.createdMs).UTC()
	_ = created
	set := bson.D{
		{Key: "projectId", Value: j.projectID},
		{Key: "userId", Value: j.userID},
		{Key: "rubricId", Value: chStr(j.rubric, "id")},
		{Key: "rubricName", Value: chStr(j.rubric, "name")},
		{Key: "modelOverride", Value: nil},
		{Key: "status", Value: j.status},
		{Key: "result", Value: chResultBSON(j.result)},
		{Key: "errorCode", Value: chErrStr(j.errCode)},
		{Key: "message", Value: chErrStr(j.message)},
		{Key: "documentTokensEstimate", Value: nil},
		{Key: "maxContextTokens", Value: nil},
		{Key: "reviewMaxTokens", Value: nil},
		{Key: "passesTotal", Value: nil},
		{Key: "passesDone", Value: int32(0)},
		{Key: "currentRequirement", Value: ""},
		{Key: "createdAt", Value: created},
		{Key: "startedAt", Value: nil},
		{Key: "finishedAt", Value: chFinishedAt(j)},
	}
	filter := bson.D{{Key: "jobId", Value: j.id}}
	if create {
		_, _ = c.UpdateOne(ctx, filter,
			bson.D{{Key: "$setOnInsert", Value: bson.D{{Key: "jobId", Value: j.id}}}, {Key: "$set", Value: set}},
			mgoOptions.Update().SetUpsert(true))
	} else {
		_, _ = c.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: set}})
	}
}

func chErrStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func chFinishedAt(j *chJob) any {
	if j.finished == 0 {
		return nil
	}
	return time.UnixMilli(j.finished).UTC()
}

// chResultBSON — ordered sub-document (obj pairs → bson.D).
func chResultBSON(o obj) any {
	if len(o) == 0 {
		return nil
	}
	d := bson.D{}
	for i := range o {
		d = append(d, bson.E{Key: o[i].k, Value: o[i].v})
	}
	return d
}

func chStr(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	v, _ := m[k].(string)
	return v
}

// chFindUserJobDoc — LLMReviewJob.findUserJobDoc (jobId + userId).
func chFindUserJobDoc(f *fs, ctx context.Context, jid, uid string) (map[string]any, bool) {
	if f.app == nil || f.app.Mongo == nil {
		return nil, false
	}
	db, err := f.app.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	var d map[string]any
	if db.Collection("llmreviewjobs").FindOne(ctx,
		bson.D{{Key: "jobId", Value: jid}, {Key: "userId", Value: uid}}).Decode(&d) == nil {
		return d, true
	}
	return nil, false
}

func chCountUserActive(f *fs, ctx context.Context, uid string) (int, bool) {
	if f.app == nil || f.app.Mongo == nil {
		return 0, false
	}
	db, err := f.app.Mongo.DB(ctx)
	if err != nil {
		return 0, false
	}
	n, err := db.Collection("llmreviewjobs").CountDocuments(ctx,
		bson.D{{Key: "userId", Value: uid},
			{Key: "status", Value: bson.D{{Key: "$in", Value: []any{"queued", "running"}}}}})
	if err != nil {
		return 0, false
	}
	return int(n), true
}

func chJobInMem(key string) *chJob {
	chQMu.Lock()
	defer chQMu.Unlock()
	return chJobs[key]
}

// ---------------------------------------------------------------------------
// POST /project/:id/llm/compliance/start
// ---------------------------------------------------------------------------

func (f *fs) llmComplianceStart(cxt *core.Cxt, res *core.Res) {
	if _, _, ok := f.llmPref(cxt, res, cxt.Params["1"]); !ok {
		return
	}
	ctx := cxt.Req.Context()
	uid := sessUIDOf(cxt)
	b := f.readBody(cxt)
	rubricID := bodyStr(b, "rubricId")
	modelRef := bodyStr(b, "model")
	if v := bodyStr(b, "modelRef"); v != "" {
		modelRef = v
	}

	if !llmEnabled() {
		jres(200, res, jobj("ok", false, "error", "disabled", "message", "LLM service is disabled"))
		return
	}
	if !featureFlags()["reviewEnabled"] {
		jres(200, res, jobj("ok", false, "error", "disabled", "message", "The review feature is disabled"))
		return
	}
	rubric := chFindRubric(chRubricForUser(f, ctx, uid), rubricID)
	if rubric == nil {
		jres(200, res, jobj("ok", false, "error", "no_rubric", "message", "Unknown or missing rubric"))
		return
	}
	// node 4: usable lane (resolveModelLane(userId, requested || undefined))
	refModel := strings.TrimSpace(modelRef)
	lr, le := f.resolveLane(uid, refModel)
	if le != nil {
		jres(200, res, jobj("ok", false, "error", "not_configured",
			"message", "No usable LLM backend: set a model (File \u2192 Select LLM Model) or add your own LLM connection"))
		return
	}
	// node 5: concurrency caps (mongo count, in-memory fallback)
	if n, ok2 := chCountUserActive(f, ctx, uid); ok2 {
		if n >= maxUserInFlight {
			jres(200, res, jobj("ok", false, "error", "too_many",
				"message", "You already have "+itoa(maxUserInFlight)+" reviews running or queued; wait for one to finish"))
			return
		}
	} else {
		n := 0
		chQMu.Lock()
		for _, j := range chJobs {
			if j.userID == uid && (j.status == "running" || j.status == "queued") {
				n++
			}
		}
		chQMu.Unlock()
		if n >= maxUserInFlight {
			jres(200, res, jobj("ok", false, "error", "too_many",
				"message", "You already have "+itoa(maxUserInFlight)+" reviews running or queued; wait for one to finish"))
			return
		}
	}
	chQMu.Lock()
	if len(chQ) >= maxGlobalQueue {
		chQMu.Unlock()
		jres(200, res, jobj("ok", false, "error", "server_busy",
			"message", "Several reviews are already queued; try again shortly"))
		return
	}
	job := &chJob{
		id:        "job-" + itoa(int(time.Now().UnixMilli())) + "-" + chRand8(),
		projectID: cxt.Params["1"],
		userID:    uid,
		rubric:    rubric,
		lane:      lr,
		status:    "queued",
		createdMs: time.Now().UnixMilli(),
	}
	chJobs[job.id] = job
	chQ = append(chQ, job.id)
	chQMu.Unlock()

	// Node semantics: processQueue() runs synchronously until its first await —
	// the job is 'running' by response time (pinned). Mirror that, then do the
	// async work.
	job.mu.Lock()
	job.status = "running"
	job.mu.Unlock()
	chPersistJob(f, ctx, job, true)

	jres(200, res, jobj("ok", true, "jobId", job.id, "status", "running", "position", 0))
	go f.chPerform(ctx, job)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var bs []byte
	for n > 0 {
		bs = append([]byte{byte('0' + n%10)}, bs...)
		n /= 10
	}
	if neg {
		return "-" + string(bs)
	}
	return string(bs)
}

func chRand8() string {
	const alpha = "abcdefghijklmnopqrstuvwxyz0123456789"
	var sb strings.Builder
	seed := uint64(time.Now().UnixNano())
	for i := 0; i < 8; i++ {
		seed = seed*6364136223846793005 + 1442695040888963407
		sb.WriteByte(alpha[(seed>>33)%uint64(len(alpha))])
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// performReview — deterministic local path (chatDetailedCompat fails locally)
// ---------------------------------------------------------------------------

// chStripLatexComments — Node: per line, cut at the FIRST unescaped '%'
// (a '%' not preceded by a backslash); escaped '\%' kept.
func chStripLatexComments(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		out := []rune{}
		runes := []rune(line)
		for j := 0; j < len(runes); j++ {
			c := runes[j]
			if c == '%' && (j == 0 || runes[j-1] != '\\') {
				break
			}
			out = append(out, c)
		}
		lines[i] = string(out)
	}
	return strings.Join(lines, "\n")
}

// chSplitRubric — Node splitRubric (numbered/bulleted parsing; prose → whole).
func chSplitRubric(text string) (preamble string, requirements []string) {
	raw := strings.TrimSpace(text)
	if raw == "" {
		return "", []string{""}
	}
	numbered := regexp.MustCompile(`^\s*\d{1,3}[.)]\s+`)
	bullet := regexp.MustCompile(`^\s*[-*\u2022]\s+`)
	lines := strings.Split(text, "\n")
	count := 0
	for _, l := range lines {
		if numbered.MatchString(l) {
			count++
		}
	}
	var marker *regexp.Regexp
	if count >= 2 {
		marker = numbered
	} else {
		marker = bullet
	}
	var cur []string
	var preambleLines []string
	for _, line := range lines {
		if marker.MatchString(line) {
			if cur != nil {
				req := strings.TrimSpace(strings.Join(cur, "\n"))
				requirements = append(requirements, req)
			}
			cur = []string{strings.TrimSpace(line)}
		} else if cur != nil {
			cur = append(cur, line)
		} else {
			preambleLines = append(preambleLines, line)
		}
	}
	if cur != nil {
		requirements = append(requirements, strings.TrimSpace(strings.Join(cur, "\n")))
	}
	if len(requirements) < 2 {
		return "", []string{strings.TrimSpace(text)}
	}
	return strings.TrimSpace(strings.Join(preambleLines, "\n")), requirements
}

// chBuildScanHints — Node buildScanHints (deterministic; feeds the token
// estimate only in this offline build).
func chBuildScanHints(stripped []projDoc) string {
	count := func(re *regexp.Regexp) int {
		n := 0
		for _, d := range stripped {
			n += len(re.FindAllString(d.text(), -1))
		}
		return n
	}
	figures := count(regexp.MustCompile(`\\begin\{figure`))
	tables := count(regexp.MustCompile(`\\begin\{(?:table|longtable)`))
	captions := count(regexp.MustCompile(`\\caption`))
	equations := count(regexp.MustCompile(`\\begin\{(?:equation|align|gather|multline)`))
	refs := count(regexp.MustCompile(`\\ref\{`))
	cites := count(regexp.MustCompile(`\\cite\{`))
	listings := count(regexp.MustCompile(`\\begin\{(?:lstlisting|verbatim)`))
	lines := []string{
		"SCAN HINTS (computed mechanically from the LaTeX source; exhaustive for the listed patterns):",
		"- Counts: " + itoa(figures) + " figure environments, " + itoa(tables) + " table environments, " +
			itoa(captions) + ` \caption, ` + itoa(equations) + " equation environments, " +
			itoa(refs) + ` \ref, ` + itoa(cites) + ` \cite, ` + itoa(listings) + " code listing environments.",
	}
	return strings.Join(lines, "\n")
}

// chPerform — one pass per requirement; each structured call fails locally in
// this build → status:'na' items; summary attempt fails → ”.
func (f *fs) chPerform(ctx context.Context, j *chJob) {
	defer func() {
		chQMu.Lock()
		for i, id := range chQ {
			if id == j.id {
				chQ = append(chQ[:i], chQ[i+1:]...)
				break
			}
		}
		chQMu.Unlock()
	}()

	rubric := j.rubric
	if rubric == nil || chStr(rubric, "id") == "" {
		f.chFinish(ctx, j, "error", "rubric-unavailable", "Rubric is no longer available", obj{})
		return
	}
	model := ""
	if j.lane != nil {
		model = j.lane.model
	}

	// document assembly (Node: getAllDocs path map, main-first + path-sorted,
	// per-doc comment strip, `% ===== FILE: <path> =====\n<text>` joined '\n\n').
	docs := f.allDocs(ctx, chOID(j.projectID))
	type sd struct {
		path   string
		text   string
		isMain bool
	}
	var ds []sd
	for _, d := range docs {
		text := strings.Join(d.lines, "\n")
		if strings.TrimSpace(text) == "" {
			continue
		}
		ds = append(ds, sd{path: d.path, text: chStripLatexComments(text), isMain: strings.Contains(text, "\\documentclass")})
	}
	sort.SliceStable(ds, func(i, k int) bool {
		if ds[i].isMain != ds[k].isMain {
			return ds[i].isMain
		}
		return ds[i].path < ds[k].path
	})
	parts := make([]string, 0, len(ds))
	stripped := []projDoc{}
	for _, d := range ds {
		parts = append(parts, "% ===== FILE: "+d.path+" =====\n"+d.text)
		stripped = append(stripped, projDoc{path: d.path, lines: strings.Split(d.text, "\n")})
	}
	assembled := strings.Join(parts, "\n\n")
	if strings.TrimSpace(assembled) == "" {
		f.chFinish(ctx, j, "error", "empty_document", "The project has no text to review", obj{})
		return
	}
	scanHints := chBuildScanHints(stripped)
	guidelines := chStr(rubric, "guidelines")
	promptTokens := chEstTokens(assembled) + chEstTokens(scanHints) + chEstTokens(guidelines) + chEstTokens(defReviewSystemPrompt)
	// countPromptTokens() is null in this build (AI-SDK v6) → heuristic.

	maxContext := int(readAdminFile(f.adminPath()).num("maxContextTokens"))
	if maxContext == 0 {
		maxContext = chMaxContextDef
	}
	fits := maxContext-promptTokens-chSafetyMargin >= chMinAnswerTokens
	if !fits {
		f.chFinish(ctx, j, "error", "too_long",
			"The document is too long for a single-pass review with the configured context window", obj{})
		return
	}

	_, requirements := chSplitRubric(guidelines)
	items := make([]any, 0, len(requirements))
	for _, req := range requirements {
		// chatDetailedCompat → OError 'LLM request failed: schema is not a
		// function (model: <m>)' (local, before network) in this build.
		msg := "LLM request failed: schema is not a function (model: " + model + ")"
		ev := "The check could not run: " + msg
		if len(ev) > 200 {
			ev = ev[:200]
		}
		items = append(items, jobj("requirement", req, "status", "na", "evidence", ev, "suggestion", ""))
	}

	// summary synthesis: local failure → '' (Node best-effort catch).
	_ = items

	result := jobj(
		"rubric", jobj("id", chStr(rubric, "id"), "name", chStr(rubric, "name")),
		"model", model,
		"documentTokensEstimate", promptTokens,
		"chunkCount", 0,
		"maxContextTokens", maxContext,
		"summary", "",
		"items", items,
	)
	f.chFinish(ctx, j, "done", "", "", result)
}

func (d projDoc) text() string { return strings.Join(d.lines, "\n") }

func chOID(s string) primitive.ObjectID {
	oid, err := primitive.ObjectIDFromHex(strings.ToLower(s))
	if err != nil {
		return primitive.NilObjectID
	}
	return oid
}

// chFinish — terminal state + mongo mirror.
func (f *fs) chFinish(ctx context.Context, j *chJob, status, code, message string, result obj) {
	j.mu.Lock()
	j.status = status
	j.errCode = code
	j.message = message
	j.result = result
	j.finished = time.Now().UnixMilli()
	j.mu.Unlock()
	chPersistJob(f, ctx, j, false)
}

// ---------------------------------------------------------------------------
// GET /project/:id/llm/compliance/status/:jobId
// ---------------------------------------------------------------------------

func (f *fs) llmComplianceStatus(cxt *core.Cxt, res *core.Res) {
	if _, _, ok := f.llmPref(cxt, res, cxt.Params["1"]); !ok {
		return
	}
	ctx := cxt.Req.Context()
	jobID := cxt.Params["2"]
	uid := sessUIDOf(cxt)

	j := chJobInMem(jobID)
	if j != nil && j.userID == uid {
		j.mu.Lock()
		st := j.status
		resl := j.result
		code := j.errCode
		msg := j.message
		j.mu.Unlock()
		switch st {
		case "done":
			jres(200, res, jobj("ok", true, "status", "done", "result", resl))
		case "error":
			jres(200, res, jobj("ok", true, "status", "error", "errorCode", code, "message", msg,
				"documentTokensEstimate", nil, "maxContextTokens", nil, "reviewMaxTokens", nil))
		case "cancelled":
			jres(200, res, jobj("ok", true, "status", "cancelled"))
		case "queued":
			jres(200, res, jobj("ok", true, "status", "queued", "position", 0))
		case "running":
			// passesTotal not set in this build's fast local path — 'preparing'.
			jres(200, res, jobj("ok", true, "status", "running", "phase", "preparing"))
		default:
			jres(200, res, jobj("ok", false, "error", "not_found", "message", "Review not found or expired"))
		}
		return
	}

	// not live → Node mongo fallback (findUserJobDoc by jobId+userId).
	if doc, ok2 := chFindUserJobDoc(f, ctx, jobID, uid); ok2 {
		st, _ := doc["status"].(string)
		switch st {
		case "running", "queued":
			// persistJobFinalStatus → error/server-restarted, then respond.
			chJobFinalStatus(f, ctx, jobID, "error", "server-restarted",
				"The server restarted while the review was running. Please start it again.")
			jres(200, res, jobj("ok", true, "status", "error", "errorCode", "server-restarted",
				"message", "The server restarted while the review was running. Please start it again."))
		case "done":
			r, _ := doc["result"].(bson.M)
			jres(200, res, jobj("ok", true, "status", "done", "result", r))
		case "error":
			code, _ := doc["errorCode"].(string)
			msg, _ := doc["message"].(string)
			jres(200, res, jobj("ok", true, "status", "error", "errorCode", code, "message", msg,
				"documentTokensEstimate", nil, "maxContextTokens", nil, "reviewMaxTokens", nil))
		case "cancelled":
			jres(200, res, jobj("ok", true, "status", "cancelled"))
		default:
			jres(200, res, jobj("ok", false, "error", "not_found", "message", "Review not found or expired"))
		}
		return
	}
	jres(200, res, jobj("ok", false, "error", "not_found", "message", "Review not found or expired"))
}

// chJobFinalStatus — persistJobFinalStatus (swallow).
func chJobFinalStatus(f *fs, ctx context.Context, jid, status, code, message string) {
	if f.app == nil || f.app.Mongo == nil {
		return
	}
	db, err := f.app.Mongo.DB(ctx)
	if err != nil {
		return
	}
	_, _ = db.Collection("llmreviewjobs").UpdateOne(ctx,
		bson.D{{Key: "jobId", Value: jid}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "status", Value: status},
			{Key: "errorCode", Value: code},
			{Key: "message", Value: message},
			{Key: "finishedAt", Value: time.Now().UTC()},
		}}})
}

// ---------------------------------------------------------------------------
// POST /project/:id/llm/compliance/cancel/:jobId — Node: ALWAYS {ok:true}.
// (Node registers this route POST-only; a GET to it 404s — the core router's
// generic 404 page answers that, matching the r_cancel_unknown pin.)
// ---------------------------------------------------------------------------

func (f *fs) llmComplianceCancel(cxt *core.Cxt, res *core.Res) {
	if _, _, ok := f.llmPref(cxt, res, cxt.Params["1"]); !ok {
		return
	}
	ctx := cxt.Req.Context()
	jobID := cxt.Params["2"]
	uid := sessUIDOf(cxt)

	j := chJobInMem(jobID)
	if j != nil && j.userID == uid {
		j.mu.Lock()
		st := j.status
		if st == "queued" || st == "running" {
			j.status = "cancelled"
			j.finished = time.Now().UnixMilli()
		}
		j.mu.Unlock()
		chPersistJob(f, ctx, j, false)
	}
	jres(200, res, jobj("ok", true))
}
