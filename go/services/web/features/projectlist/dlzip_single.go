// P4.12d — user-facing single-project zip download:
//
//	GET /project/:Project_id/download/zip
//
// Owner scope (2026-10-13, board TODO-047dc1d9 — green-lit implementation):
// "Add import and export functionality for tracked changes and comments
// (stored in json files) ... via zip archive (except for templates)."
//
// CONTRACT (tests/e2e/specs/zip-tracked-comments.test.e2e.ts, self-activating
// on this route):
//   - 200 + Content-Type application/zip + attachment disposition.
//   - Zip entries: the project files (docs as source; file blobs via the
//     filestore reference) PLUS:
//       comments.json        {"comments":[{id,text,doc,ranges,state}]}
//       tracked-changes.json {"tracked":[{id,kind,type,doc,start,end,content,text,state}]}
//   - TEMPLATE EXCLUSION (owner scope): when a template row records this
//     project as its source (templates.source_project_id) the payload files
//     are OMITTED (templates carry no payloads). No such link rows exist in
//     this profile (project publish is the pinned 400 follow-up) → the check
//     is future-proof, not a no-op.
//   - SECURITY: session required (framework session router; anonymous bounces
//     to login); READ gate = the package gate (canRead + loadUserAdmin —
//     access.go) with the Node conventions: ghost → 404 page, non-member →
//     403 restricted (leak gate spec leg M); no cross-project bleed (each
//     project's own payloads only — leak-gated in spec).
//
// WORLD REFERENCES (this session's pins):
//   - rootFolder walk + doc lines + filestore blob URL = the compile
//     resource builder (features/compile/compile.go:811-945);
//   - collab review/tracked-change API = go/services/collab/review.go
//     (ListThreads/MessagesOfThread/ListChanges), store =
//     collab.NewMongoStore(ctx, db) (review.go h.run pattern); room = pid;
//   - project gate shapes = access.go canRead (owner/collab/review/readOnly
//     + publicAccesLevel + admin).
package projectlist

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"ollitex/go/services/collab"
	"ollitex/go/services/web/core"
	"ollitex/go/services/web/views"
)

var zipsinglePat = regexp.MustCompile(`^/project/([^/]+)/download/zip$`)

// zipSingleMaxZip — in-memory zip budget (bounded, best-effort house pattern).
const zipSingleMaxZip = 50 << 20 // 50 MiB

func zswDocstoreBase() string {
	host := os.Getenv("DOCSTORE_HOST")
	if host == "" {
		host = "127.0.0.1:3016"
	}
	base := crEnvOr("WEB_DOCSTORE_URL", "http://"+host+":3016")
	return strings.TrimSuffix(base, "/")
}
func zswFilestoreBase() string {
	host := os.Getenv("FILESTORE_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	base := crEnvOr("WEB_FILESTORE_URL", "http://"+host+":3009")
	return strings.TrimSuffix(base, "/")
}

// ---- rootFolder walk (compile.go walk parity: docs then fileRefs, depth-first)
type zswDoc struct{ ID, Path string }
type zswFile struct{ Path, Hash string }

func zswWalk(v any, base string) (docs []zswDoc, files []zswFile) {
	switch t := v.(type) {
	case bson.A:
		for _, el := range t {
			d, f := zswWalk(el, base)
			docs = append(docs, d...)
			files = append(files, f...)
		}
	case []any:
		for _, el := range t {
			d, f := zswWalk(el, base)
			docs = append(docs, d...)
			files = append(files, f...)
		}
	case bson.D:
		if ds, ok := dget(t, "docs").(bson.A); ok {
			for _, de := range ds {
				m, _ := de.(bson.D)
				name := asStr(dget(m, "name"))
				id := zswIDStr(dget(m, "_id"))
				if id == "" || name == "" {
					continue
				}
				docs = append(docs, zswDoc{ID: id, Path: zswJoin(base, name)})
			}
		}
		if frs, ok := dget(t, "fileRefs").(bson.A); ok {
			for _, fe := range frs {
				m, _ := fe.(bson.D)
				name := asStr(dget(m, "name"))
				if name == "" {
					continue
				}
				// fileRef shape: {name, _id, fileRef_id:{_id|hash...}} (legacy:
				// fileRef_id._id is the blob hash in the filestore)
				hash := ""
				if frv := dget(m, "fileRef_id"); frv != nil {
					switch frr := frv.(type) {
					case string:
						hash = frr
					case bson.D:
						hash = asStr(dget(frr, "_id"))
						if hash == "" {
							hash = asStr(dget(frr, "hash"))
						}
					}
				}
				files = append(files, zswFile{Path: zswJoin(base, name), Hash: hash})
			}
		}
		if fols, ok := dget(t, "folders").(bson.A); ok {
			for _, fo := range fols {
				m, _ := fo.(bson.D)
				name := asStr(dget(m, "name"))
				if name == "" {
					continue
				}
				d, f := zswWalk(dget(m, "rootFolder"), zswJoin(base, name))
				docs = append(docs, d...)
				files = append(files, f...)
			}
		}
	}
	return docs, files
}
// zswIDStr — a document/file id may be a hex string OR a bson.ObjectID
// (mongo round-trip) — normalize to hex.
func zswIDStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if o, ok := v.(bson.ObjectID); ok {
		return o.Hex()
	}
	return ""
}
func zswJoin(base, name string) string {
	if base == "" {
		return name
	}
	return base + "/" + name
}

// ---- doc lines (docstore reference, compile getDocLines parity) ----
func zswDocLines(ctx context.Context, pid string) (map[string][]string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(zswDocstoreBase() + "/project/" + pid + "/doc")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, &zswStatusErr{resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	parsed, err := zswParseDocStore(body)
	if err != nil {
		return nil, err
	}
	return parsed, nil
}
type zswStatusErr struct{ code int }
func (e *zswStatusErr) Error() string { return "docstore status " + strconv.Itoa(e.code) }

// zswParseDocStore — the docstore /project/:pid/doc body shape is a per-doc
// map of id → lines (or a wrapped {docs: {...}}); accept both.
func zswParseDocStore(body []byte) (map[string][]string, error) {
	out := map[string][]string{}
	var parsed map[string]json.RawMessage
	if json.Unmarshal(body, &parsed) == nil {
	} else {
		var wrapped struct {
			Docs map[string]json.RawMessage `json:"docs"`
		}
		if json.Unmarshal(body, &wrapped) == nil && wrapped.Docs != nil {
			parsed = wrapped.Docs
		} else {
			return nil, io.ErrUnexpectedEOF
		}
	}
	for id, raw := range parsed {
		var lines []string
		if json.Unmarshal(raw, &lines) == nil {
			out[id] = lines
			continue
		}
		var single string
		if json.Unmarshal(raw, &single) == nil {
			out[id] = []string{single}
		}
	}
	return out, nil
}

// ---- payloads (collab store: review threads/comments + tracked changes) ----
func zswPayloads(ctx context.Context, a *core.App, pid string) (commentsJSON, trackedJSON []byte) {
	commentsJSON = []byte(`{"comments":[]}`)
	trackedJSON = []byte(`{"tracked":[]}`)
	if a == nil || a.Mongo == nil {
		return
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return
	}
	st, err := collab.NewMongoStore(ctx, db)
	if err != nil {
		return
	}
	type flatComment struct {
		ID     string           `json:"id"`
		Text   string           `json:"text"`
		Doc    string           `json:"doc"`
		Ranges []map[string]any `json:"ranges"`
		State  string           `json:"state"`
	}
	type flatChange struct {
		ID      string `json:"id"`
		Kind    string `json:"kind"`
		Type    string `json:"type"`
		Doc     string `json:"doc"`
		Start   int    `json:"start"`
		End     int    `json:"end"`
		Content string `json:"content"`
		Text    string `json:"text"`
		State   string `json:"state"`
	}
	flats := []flatComment{}
	if ths, terr := collab.ListThreads(ctx, st, pid); terr == nil {
		for _, th := range ths {
			msgs, merr := collab.MessagesOfThread(ctx, st, pid, th.ID)
			if merr != nil || len(msgs) == 0 {
				continue
			}
			for _, cm := range msgs {
				doc := cm.File
				if doc == "" {
					doc = th.File
				}
				state := cm.State
				if state == "" {
					state = th.State
				}
				ranges := cm.Ranges
				if ranges == nil {
					ranges = []map[string]any{}
				}
				flats = append(flats, flatComment{ID: cm.ID, Text: cm.Text, Doc: doc, Ranges: ranges, State: state})
			}
		}
	}
	if b, jerr := json.Marshal(struct {
		Comments []flatComment `json:"comments"`
	}{flats}); jerr == nil {
		commentsJSON = b
	}
	if chs, cerr := collab.ListChanges(ctx, st, pid); cerr == nil {
		fch := []flatChange{}
		for _, ch := range chs {
			fch = append(fch, flatChange{
				ID: ch.ID, Kind: ch.Kind, Type: ch.Kind,
				Doc: ch.File, Start: ch.Start, End: ch.End,
				Content: ch.Content, Text: ch.Content, State: ch.State,
			})
		}
		if b, jerr := json.Marshal(struct {
			Tracked []flatChange `json:"tracked"`
		}{fch}); jerr == nil {
			trackedJSON = b
		}
	}
	return
}

// ---- file bytes (filestore reference URL, compile parity) ----
func zswFileBytes(ctx context.Context, historyID, hash string) []byte {
	if historyID == "" || hash == "" {
		return nil
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(zswFilestoreBase() + "/history/project/" + historyID + "/hash/" + hash)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil
	}
	return body
}

// ---- template exclusion link (owner scope: except for templates) ----
func zswIsTemplateSource(ctx context.Context, a *core.App, pidHex string) bool {
	if a == nil || a.Mongo == nil {
		return false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	oid, oerr := bson.ObjectIDFromHex(pidHex)
	if oerr != nil {
		return false
	}
	err = db.Collection("templates").FindOne(ctx,
		bson.D{{Key: "source_project_id", Value: oid}}).Err()
	return err == nil
}

// handler — GET /project/:pid/download/zip
func singleZipHandler(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		mm := zipsinglePat.FindStringSubmatch(cxt.Req.URL.Path)
		pidHex := strings.ToLower(mm[1])
		if !delHex24(pidHex) {
			res.SendStatus(http.StatusNotFound)
			return
		}
		uid := ""
		if cxt.Sess != nil {
			uid = cxt.Sess.UserIDHex()
		}
		if uid == "" {
			// framework session router normally bounces first; belt and braces:
			res.SendStatus(http.StatusFound)
			res.W.Header().Set("Location", "/login")
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 30*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.SendStatus(http.StatusInternalServerError)
			return
		}
		oid, _ := bson.ObjectIDFromHex(pidHex)
		var proj bson.D
		if db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&proj) != nil {
			views.NotFoundPage(res.W, pageBase(cxt, "project/"+pidHex))
			return
		}
		if !canRead(uid, loadUserAdmin(a, cxt, uid), proj) {
			views.Restricted403(res.W, pageBase(cxt, "project/"+pidHex))
			return
		}
		// the project zip (shared with the admin multi-zip inner entries)
		out := zswBuildZip(ctx, a, pidHex)
		name := ""
		if n, nerr := zswProjectName(ctx, a, pidHex); nerr == nil {
			name = n
		}
		filename := name + ".zip"
		if name == "" {
			filename = pidHex + ".zip"
		}
		res.W.Header().Set("Content-Type", "application/zip")
		res.W.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		res.W.Header().Set("Content-Length", strconv.Itoa(len(out)))
		res.W.WriteHeader(http.StatusOK)
		_, _ = res.W.Write(out)
	}
}

// zswProjectName — best-effort project name (attachment filename).
func zswProjectName(ctx context.Context, a *core.App, pidHex string) (string, error) {
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return "", err
	}
	oid, oerr := bson.ObjectIDFromHex(pidHex)
	if oerr != nil {
		return "", oerr
	}
	var p bson.D
	if db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&p) != nil {
		return "", io.ErrUnexpectedEOF
	}
	return asStr(dget(p, "name")), nil
}

// zswBuildZip — the project export (shared): all project files (docs as
// source; file blobs via the filestore reference) + the payload files
// (comments.json / tracked-changes.json) unless the project is a template
// source (owner scope: except for templates). Caller gates access.
func zswBuildZip(ctx context.Context, a *core.App, pidHex string) []byte {
	pidHex = strings.ToLower(pidHex)
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil
	}
	oid, oerr := bson.ObjectIDFromHex(pidHex)
	if oerr != nil {
		return nil
	}
	var proj bson.D
	if db.Collection("projects").FindOne(ctx, bson.D{{Key: "_id", Value: oid}}).Decode(&proj) != nil {
		return nil
	}
	// tree (compile walk parity)
	var docs []zswDoc
	var files []zswFile
	if rf := dget(proj, "rootFolder"); rf != nil {
		d, f := zswWalk(rf, "")
		docs, files = d, f
	}
	// doc contents (docstore) — best-effort; missing lines → skip
	lines := map[string][]string{}
	if lw, lerr := zswDocLines(ctx, pidHex); lerr == nil {
		lines = lw
	}
	// file blobs (filestore via the project history id)
	historyID := ""
	if ov, ok := dget(proj, "overleaf").(bson.D); ok {
		if h, ok2 := dget(ov, "history").(bson.D); ok2 {
			historyID = asStr(dget(h, "id"))
		}
	}
	// payloads (collab store) + template exclusion
	commentsJSON, trackedJSON := zswPayloads(ctx, a, pidHex)
	templateSource := zswIsTemplateSource(ctx, a, pidHex)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(path string, data []byte) {
		if path == "" || buf.Len() >= zipSingleMaxZip {
			return
		}
		f, err2 := zw.Create(path)
		if err2 != nil {
			return
		}
		_, _ = f.Write(data)
	}
	for _, d := range docs {
		content := ""
		if l, ok := lines[d.ID]; ok {
			content = strings.Join(l, "\n")
		}
		add(d.Path, []byte(content))
	}
	for _, f := range files {
		b := zswFileBytes(ctx, historyID, f.Hash)
		if b != nil {
			add(f.Path, b)
		}
	}
	if !templateSource {
		add("comments.json", commentsJSON)
		add("tracked-changes.json", trackedJSON)
	}
	_ = zw.Close()
	return buf.Bytes()
}
