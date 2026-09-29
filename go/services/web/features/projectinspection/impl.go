package projectinspection

// default seam implementations (real endpoints), snapshot builder, and the
// small house helpers (bson walking follows projectlist/access.go).

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// ---- project + entity walking (projectlist/access.go shape) --------------

func dget(d bson.D, key string) any {
	for _, e := range d {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}

func dgetArr(d bson.D, key string) bson.A {
	a, _ := dget(d, key).(bson.A)
	return a
}

func oidHex(v any) string {
	switch t := v.(type) {
	case bson.ObjectID:
		return t.Hex()
	case string:
		return t
	}
	return ""
}

type ent struct {
	id    string
	path  string
	typ   string
	hash  string
}

// walkFolder mirrors _getAllFoldersFromProject + getAllEntitiesFromProject
// (docs + fileRefs), capturing ids + hashes where present.
func walkFolder(folder bson.D, base string, out *[]ent) {
	for _, doc := range dgetArr(folder, "docs") {
		dm, ok := doc.(bson.D)
		if !ok {
			continue
		}
		if nm, ok2 := dget(dm, "name").(string); ok2 {
			*out = append(*out, ent{
				id:   oidHex(dget(dm, "_id")),
				path: path.Join(strings.TrimSuffix(base, "/"), nm),
				typ:  "doc",
			})
		}
	}
	for _, fr := range dgetArr(folder, "fileRefs") {
		fm, ok := fr.(bson.D)
		if !ok {
			continue
		}
		if nm, ok2 := dget(fm, "name").(string); ok2 {
			*out = append(*out, ent{
				id:   oidHex(dget(fm, "_id")),
				path: path.Join(strings.TrimSuffix(base, "/"), nm),
				typ:  "file",
				hash: dget(fm, "hash").(string),
			})
		}
	}
	for _, c := range dgetArr(folder, "folders") {
		cm, ok := c.(bson.D)
		if !ok {
			continue
		}
		if nm, ok2 := dget(cm, "name").(string); ok2 {
			walkFolder(cm, path.Join(strings.TrimSuffix(base, "/"), nm), out)
		}
	}
}

// collectEntities returns the project's doc+file entities walking
// rootFolder[0] (paths relative, "/leading" stripped for the engine).
func collectEntities(doc *bson.D) []ent {
	var out []ent
	rf := dgetArr(*doc, "rootFolder")
	if len(rf) > 0 {
		if root, ok := rf[0].(bson.D); ok {
			walkFolder(root, ".", &out)
		}
	}
	return out
}

// historyIDOf — project.overleaf.history.id (v1-history blob API prefix).
func historyIDOf(proj *bson.D) string {
	ov, ok := dget(*proj, "overleaf").(bson.D)
	if !ok {
		return ""
	}
	h, ok := dget(ov, "history").(bson.D)
	if !ok {
		return ""
	}
	if id, ok := dget(h, "id").(bson.ObjectID); ok {
		return id.Hex()
	}
	if s, ok := dget(h, "id").(string); ok {
		return s
	}
	return ""
}

// ---- default seams ---------------------------------------------------------

func (s *svc) loadProjectDefault(ctx context.Context, pid string) (*bson.D, bool, error) {
	if s.a == nil || s.a.Mongo == nil {
		return nil, false, errors.New("projectinspection: app mongo not wired")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db, err := s.a.Mongo.DB(ctx)
	if err != nil || db == nil {
		return nil, false, fmt.Errorf("projectinspection: mongo db: %w", err)
	}
	oid, oerr := bson.ObjectIDFromHex(pid)
	if oerr != nil {
		return nil, false, oerr
	}
	var d bson.D
	if fErr := db.Collection("projects").FindOne(ctx,
		bson.D{{Key: "_id", Value: oid}}).Decode(&d); fErr != nil {
		if strings.Contains(fErr.Error(), "no documents") {
			return nil, false, nil
		}
		return nil, false, fErr
	}
	return &d, true, nil
}

// fetchDocDefault — docstore GET /project/{pid}/doc/{did} → lines joined.
func (s *svc) fetchDocDefault(ctx context.Context, pid, docID string) (string, error) {
	if s.cfg.DocstoreBase == "" {
		return "", errors.New("projectinspection: docstore base unset")
	}
	url := s.cfg.DocstoreBase + "/project/" + pid + "/doc/" + docID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	if s.cfg.V1HistoryUser != "" {
		req.SetBasicAuth(s.cfg.V1HistoryUser, s.cfg.V1HistoryPass)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, s.cfg.MaxBib+1))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return "", nil
		}
		return "", fmt.Errorf("projectinspection: docstore %d", resp.StatusCode)
	}
	var m struct {
		Lines []string `json:"lines"`
	}
	if json.Unmarshal(body, &m) != nil || m.Lines == nil {
		return "", nil
	}
	return strings.Join(m.Lines, "\n"), nil
}

var errMissingBlob = errors.New("projectinspection: blob missing upstream (404)")

// fetchBlobDefault — v1-history blob (P4.12a file-proxy upstream parity).
func (s *svc) fetchBlobDefault(ctx context.Context, historyID, hash string) ([]byte, error) {
	url := s.cfg.V1HistoryBase + "/api/projects/" + historyID + "/blobs/" + hash
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if s.cfg.V1HistoryUser != "" {
		req.SetBasicAuth(s.cfg.V1HistoryUser, s.cfg.V1HistoryPass)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bound := s.cfg.MaxBib + 1
	body, err := io.ReadAll(io.LimitReader(resp.Body, bound))
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
		if int64(len(body)) > s.cfg.MaxBib {
			return body, errBlobTooLarge
		}
		return body, nil
	case http.StatusNotFound:
		return nil, errMissingBlob
	default:
		return nil, fmt.Errorf("projectinspection: blob upstream %d", resp.StatusCode)
	}
}

var errBlobTooLarge = errors.New("projectinspection: blob exceeds the per-file bib bound")

// runWorkerDefault — one short-lived `node <worker>` per request.
func (s *svc) runWorkerDefault(ctx context.Context, snapshot []byte) ([]byte, error) {
	c := s.cfg
	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.NodeBin, c.Worker)
	cmd.Stdin = bytes.NewReader(snapshot)
	cmd.Stderr = io.Discard
	var out bytes.Buffer
	cmd.Stdout = &out
	runErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, errTimeout
	}
	if ctx.Err() != nil {
		return nil, errCancelled
	}
	if runErr != nil {
		return nil, errInternal("worker exited: " + runErr.Error())
	}
	if out.Len() > 128<<20 {
		return nil, errInternal("worker output exceeds the 128 MiB bound")
	}
	return out.Bytes(), nil
}

// ---- snapshot builder (reference reader parity) ---------------------------

func (s *svc) buildSnapshot(ctx context.Context, pid string, proj *bson.D, entryIDs []string) ([]byte, error) {
	ents := collectEntities(proj)

	docs := []map[string]any{}
	files := []map[string]any{}
	for _, e := range ents {
		if e.typ == "doc" {
			docs = append(docs, map[string]any{
				"id": e.id, "path": e.path,
			})
		} else {
			files = append(files, map[string]any{
				"id": e.id, "path": e.path, "hash": e.hash,
			})
		}
	}

	if int64(len(docs)+len(files)) > s.cfg.MaxEntities {
		return nil, errTooLarge(fmt.Sprintf(
			"Project contains more than %d entities", s.cfg.MaxEntities))
	}

	// entry points must be real documents with a compilable root extension
	byID := map[string]map[string]any{}
	for _, d := range docs {
		byID[d["id"].(string)] = d
	}
	for _, id := range entryIDs {
		d, ok := byID[id]
		if !ok {
			return nil, errInvalidEntry("One or more entry points are invalid")
		}
		if !validRootExt.MatchString(d["path"].(string)) {
			return nil, errInvalidEntry("Entry point must be a compilable document")
		}
	}

	// documents content (docstore → lines), total bound
	total := int64(0)
	for _, d := range docs {
		content, err := s.fetchDoc(ctx, pid, d["id"].(string))
		if err != nil {
			return nil, err
		}
		total += int64(len(content))
		if total > s.cfg.MaxSource {
			return nil, errTooLarge(fmt.Sprintf(
				"Project source exceeds the %d byte analysis limit", s.cfg.MaxSource))
		}
		d["content"] = content
		d["revision"] = 1
	}

	// binaryBibliographies (.bib files), per-file + total bounds
	bibs := []map[string]any{}
	totalBib := int64(0)
	hid := historyIDOf(proj)
	for _, f := range files {
		p, _ := f["path"].(string)
		if !bibExt.MatchString(p) {
			continue
		}
		entM := map[string]any{"id": f["id"], "path": p}
		hash, _ := f["hash"].(string)
		if hash == "" {
			entM["skippedReason"] = "missing-hash"
			bibs = append(bibs, entM)
			continue
		}
		body, err := s.fetchBlob(ctx, hid, hash)
		switch {
		case errors.Is(err, errMissingBlob):
			entM["skippedReason"] = "missing-hash"
			bibs = append(bibs, entM)
			continue
		case errors.Is(err, errBlobTooLarge):
			entM["skippedReason"] = "file-too-large"
			bibs = append(bibs, entM)
			continue
		case err != nil:
			return nil, err
		}
		totalBib += int64(len(body))
		if totalBib > s.cfg.MaxTotalBib {
			return nil, errTooLarge(fmt.Sprintf(
				"Bibliography content exceeds the %d byte analysis limit", s.cfg.MaxTotalBib))
		}
		str := string(body)
		entM["content"] = &str
		bibs = append(bibs, entM)
	}

	return json.Marshal(map[string]any{
		"projectId":            pid,
		"documents":            docs,
		"files":                files,
		"binaryBibliographies": bibs,
		"entryPointIds":        entryIDs,
	})
}

// ---- misc helpers ----------------------------------------------------------

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func readAllLimited(r io.Reader, n int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, n))
}

func qstr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func uuid4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString
	return h(b[:4]) + "-" + h(b[4:6]) + "-" + h(b[6:8]) + "-" + h(b[8:10]) + "-" + h(b[10:16])
}

func timeRFC3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func now() time.Time { return time.Now() }
