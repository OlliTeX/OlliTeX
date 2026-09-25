// ---------------------------------------------------------------------------
// Seed source (S4 contract) — where a room's initial content COMES FROM.
//
// A room (projectId) adopts the project's CURRENT main-file content, read
// through the exact blob path the web's file proxy uses (live-pinned
// oracle, projectlist/fileproxy.go):
//
//	projects doc (Mongo "projects")
//	  → rootFolder tree walk (element fileRefs first, then nested folders —
//	    the fproxyFindFile order)
//	  → fileRef {name, hash}  +  project.overleaf.history.id (hid)
//	  → GET {WEB_V1_HISTORY_URL}/projects/{hid}/blobs/{hash}
//	     basic-auth V1_HISTORY_USER : V1_HISTORY_PASSWORD
//
// Main-file selection (deterministic, test-pinned):
//  1. the first candidate named "main.tex" (the template contract:
//     every project seeded by POST /project/new carries rootFolder
//     main.tex — see projectlist/create.go),
//  2. else the first candidate whose name ends in ".tex",
//  3. else "" (a project with no text file legitimately starts empty).
//
// Failure semantics (fail-closed where content is expected):
//   - no project doc           → error (the auth gate already proved the
//     caller is a member, so this is anomalous —
//     surfacing it is correct)
//   - no main file candidate   → "" (nil error): empty seed is a state, not
//     a failure
//   - blob 404                 → "" (nil error): the file exists in the tree
//     but its blob is absent — same observable as
//     an empty file for seeding purposes
//   - other HTTP / transport   → error (never seed a room from a failed read)
//
// The body is bounded (maxSeedBytes) — a safety valve; these are text files.
package collab

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// maxSeedBytes bounds the seeded text (safety valve; .tex sizes are small).
const maxSeedBytes = 512 << 20

// ErrSeedProject — the room's project doc cannot be resolved.
var ErrSeedProject = errors.New("collab: seed: project not found")

// SeedProjects — the ONE Mongo surface the seed source needs. The
// production implementation is mongoClient (ProjectByID); tests inject a
// fake. id is the 24-hex project id (= the room name).
type SeedProjects interface {
	ProjectByID(ctx context.Context, id string) (bson.D, error)
}

// SeedSource — the live SeedFn implementation (see the package comment).
// Constructor: NewSeedSource. All fields are injectable for hermetic tests.
type SeedSource struct {
	P    SeedProjects
	Base string // WEB_V1_HISTORY_URL (default "http://127.0.0.1:3100/api")
	User string // V1_HISTORY_USER (default "staging")
	Pass string // V1_HISTORY_PASSWORD (default "")
	HTTP *http.Client
}

var seedHex24 = regexp.MustCompile(`^[0-9a-f]{24}$`)

// NewSeedSource wires the production seed source over a projects reader.
func NewSeedSource(p SeedProjects, base, user, pass string, c *http.Client) *SeedSource {
	if base == "" {
		base = "http://127.0.0.1:3100/api"
	}
	if user == "" {
		user = "staging"
	}
	if c == nil {
		c = http.DefaultClient
	}
	return &SeedSource{P: p, Base: base, User: user, Pass: pass, HTTP: c}
}

// SeedText implements Options.SeedFn for the room named <projectId>: the
// project's main-file content ("" + nil error when there is none to seed).
func (s *SeedSource) SeedText(ctx context.Context, room string) (string, error) {
	if !seedHex24.MatchString(room) {
		return "", fmt.Errorf("%w: %q", ErrSeedProject, room)
	}
	doc, err := s.P.ProjectByID(ctx, room)
	if err != nil {
		return "", err
	}
	if doc == nil {
		return "", fmt.Errorf("%w: %s", ErrSeedProject, room)
	}
	_, hash := pickSeedFile(docGet(doc, "rootFolder"))
	if hash == "" {
		return "", nil
	}
	hid := dgetHistoryID(doc)
	if hid == "" {
		// No history id → no blob store for this project (degenerate); an
		// empty seed is the only honest content.
		return "", nil
	}
	body, status, err := fetchSeedBlob(ctx, s, hid, hash)
	if err != nil {
		return "", err
	}
	switch status {
	case http.StatusOK:
		return string(body), nil
	case http.StatusNotFound:
		return "", nil
	default:
		return "", fmt.Errorf("collab: seed: blob store returned %d", status)
	}
}

// fetchSeedBlob — GET {Base}/projects/{hid}/blobs/{hash} (basic auth),
// returning (body, status, transportError). The URL shape mirrors the
// web's file proxy exactly (no "/api" double-prefix — Base already ends
// in /api by default; TrimSuffix keeps a bare host working too).
func fetchSeedBlob(ctx context.Context, s *SeedSource, hid, hash string) ([]byte, int, error) {
	base := strings.TrimSuffix(s.Base, "/")
	up, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/projects/"+hid+"/blobs/"+hash, nil)
	if err != nil {
		return nil, 0, err
	}
	up.SetBasicAuth(s.User, s.Pass)
	resp, err := s.HTTP.Do(up)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSeedBytes+1))
	if err != nil {
		return nil, 0, err
	}
	if int64(len(body)) > maxSeedBytes {
		return nil, 0, errors.New("collab: seed: blob exceeds safety bound")
	}
	return body, resp.StatusCode, nil
}

// dgetHistoryID — project.overleaf.history.id as a string (ObjectID or
// stored string both occur in this lineage).
func dgetHistoryID(doc bson.D) string {
	ov, ok0 := asMap(docGet(doc, "overleaf"))
	if !ok0 {
		return ""
	}
	ovh, ok1 := asMap(ov["history"])
	if !ok1 {
		return ""
	}
	v, ok := ovh["id"]
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case primitive.ObjectID:
		return t.Hex()
	}
	return ""
}

// nestedMap — one level of {"key", {"k", v...}} descent (bson.D → map view).
func nestedMap(d bson.D, key string) map[string]any {
	for i := 0; i < len(d); i++ {
		if d[i].Key != key {
			continue
		}
		switch t := d[i].Value.(type) {
		case map[string]any:
			return t
		case bson.D:
			m := make(map[string]any, len(t))
			for _, e := range t {
				m[e.Key] = e.Value
			}
			return m
		}
		return nil
	}
	return nil
}

// docGet — first value for key in a top-level bson.D.
func docGet(d bson.D, key string) any {
	for i := 0; i < len(d); i++ {
		if d[i].Key == key {
			return d[i].Value
		}
	}
	return nil
}

// pickSeedFile — the fproxyFindFile walk order (each element's fileRefs
// BEFORE its nested folders), collecting {name, hash} candidates, then
// the selection rule (main.tex → first .tex → none).
func pickSeedFile(root any) (name, hash string) {
	type cand struct{ name, hash string }
	var cands []cand
	var walk func(v any) bool
	walk = func(v any) bool {
		m, ok := asMap(v)
		if !ok {
			return false
		}
		if fr := asArr(m["fileRefs"]); fr != nil {
			for _, fv := range fr {
				fm, ok := asMap(fv)
				if !ok {
					continue
				}
				nm := asString(fm["name"])
				ha := asString(fm["hash"])
				if nm != "" && ha != "" {
					cands = append(cands, cand{nm, ha})
				}
			}
		}
		if fl := asArr(m["folders"]); fl != nil {
			for _, sv := range fl {
				walk(sv)
			}
		}
		return true
	}
	if arr := asArr(root); arr != nil {
		for _, v := range arr {
			walk(v)
		}
	}
	for _, c := range cands {
		if c.name == "main.tex" {
			return c.name, c.hash
		}
	}
	for _, c := range cands {
		if strings.HasSuffix(c.name, ".tex") {
			return c.name, c.hash
		}
	}
	return "", ""
}

func asMap(v any) (map[string]any, bool) {
	switch t := v.(type) {
	case map[string]any:
		return t, true
	case bson.D:
		m := make(map[string]any, len(t))
		for _, e := range t {
			m[e.Key] = e.Value
		}
		return m, true
	}
	return nil, false
}

func asArr(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case primitive.A:
		return t
	}
	return nil
}

func asString(v any) string {
	if t, ok := v.(string); ok {
		return t
	}
	return ""
}
