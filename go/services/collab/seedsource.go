// ---------------------------------------------------------------------------
// Seed source (S4 contract) — where a room's initial content COMES FROM.
//
// A room (projectId) adopts the project's CURRENT main-file content. The
// oracle is the web's editor surface: the editor loads its initial doc
// through the docstore (projectlist/docapi.go → the Go docstore service),
// so the seed reads the SAME document the editor renders:
//
//	projects doc (Mongo "projects")
//	  → rootDoc_id  (ObjectID — the root document of the project tree)
//	  → GET {WEB_DOCSTORE_URL}/project/{pidHex}/doc/{rootDocID}
//	     → { _id, lines: [...], rev, version, ranges }
//	     → seed text = strings.Join(lines, "\n")
//
// Documented from the live stack (2026-09: ol-e2e lineage): a project
// created by POST /project/new carries rootDoc_id + a docstore revision-0
// document whose lines ARE the rendered template (mainbasic.tex with
// project name / author / date substituted). rootFolder is left empty in
// this model — the file tree lives inside the root document.
//
// Failure semantics (fail-closed where content is expected):
//   - no project doc              → error (the auth gate already proved the
//     caller is a member, so this is anomalous — surfacing it is correct)
//   - project has no rootDoc_id   → "" (nil error): nothing to seed is a
//     state, not a failure (the editor itself has no doc to render)
//   - docstore 404                → "" (nil error): doc absent (degenerate
//     project) — same observable as empty for seeding
//   - other HTTP / transport      → error (never open a room from a failed
//     content read — an empty room would silently diverge from reality)
//
// The body is bounded (maxSeedBytes) — a safety valve; these are text docs.
package collab

import (
	"context"
	"encoding/json"
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
	Base string // WEB_DOCSTORE_URL (default "http://127.0.0.1:3016")
	User string // basic-auth user (docstore parity; internal service, unused)
	Pass string // basic-auth pass (ditto)
	HTTP *http.Client
}

var seedHex24 = regexp.MustCompile(`^[0-9a-f]{24}$`)

// NewSeedSource wires the production seed source over a projects reader.
func NewSeedSource(p SeedProjects, base, user, pass string, c *http.Client) *SeedSource {
	if base == "" {
		base = "http://127.0.0.1:3016"
	}
	if c == nil {
		c = http.DefaultClient
	}
	return &SeedSource{P: p, Base: base, User: user, Pass: pass, HTTP: c}
}

// SeedText implements Options.SeedFn for the room named <projectId>: the
// project's root-document content ("" + nil error when there is none).
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
	did := rootDocID(doc)
	if did == "" {
		// No root document in this project model → nothing to seed.
		return "", nil
	}
	body, status, err := fetchSeedDoc(ctx, s, room, did)
	if err != nil {
		return "", err
	}
	switch status {
	case http.StatusOK:
		lines, ok := docLines(body)
		if !ok {
			return "", errors.New("collab: seed: docstore response has no lines")
		}
		return lines, nil
	case http.StatusNotFound:
		return "", nil
	default:
		return "", fmt.Errorf("collab: seed: docstore returned %d", status)
	}
}

// fetchSeedDoc — GET {Base}/project/{pid}/doc/{did}; returns (body, status,
// transportError). Basic auth is sent when credentials are configured
// (parity with the file-proxy style; the docstore is an internal service
// and does not enforce it today).
func fetchSeedDoc(ctx context.Context, s *SeedSource, pidHex, did string) ([]byte, int, error) {
	base := strings.TrimSuffix(s.Base, "/")
	up, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/project/"+pidHex+"/doc/"+did, nil)
	if err != nil {
		return nil, 0, err
	}
	if s.User != "" {
		up.SetBasicAuth(s.User, s.Pass)
	}
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
		return nil, 0, errors.New("collab: seed: doc exceeds safety bound")
	}
	return body, resp.StatusCode, nil
}

// rootDocID — project.rootDoc_id as a 24-hex string (ObjectID or stored
// string both occur in this lineage).
func rootDocID(doc bson.D) string {
	switch t := docGet(doc, "rootDoc_id").(type) {
	case string:
		return t
	case primitive.ObjectID:
		return t.Hex()
	}
	return ""
}

// docLines — docstore GET view → lines joined with "\n" (the editor's
// rendering: join(lines, newline)).
func docLines(body []byte) (string, bool) {
	var m struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return "", false
	}
	if m.Lines == nil {
		return "", false
	}
	return strings.Join(m.Lines, "\n"), true
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
