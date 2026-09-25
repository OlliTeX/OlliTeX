// Package collab is the Yjs/Ygo collaboration service (ARC-9, D19): the
// WebSocket relay + document persistence for the Yjs editor. It embeds the
// ygo (github.com/reearth/ygo) production WebSocket server — a
// Hocuspocus-compatible y-protocols relay with read-only peer support — and
// wires it to the OlliTeX authorization model:
//
//   - room name = project id (path base segment, e.g. /collab/<projectId>)
//   - auth      = shared session (COOKIE_NAME cookie -> Redis session store,
//     same store the Go web uses) + project role:
//     owner / collaborator  -> read-write peer
//     readOnly_refs         -> READ-ONLY peer (ygo ConnectionConfig.ReadOnly:
//     receives broadcasts, inbound writes dropped)
//     anything else         -> 401 at the WebSocket handshake
//   - storage   = ygo FilePersistence: a versioned binary update log +
//     snapshots per room (the new "history" model — D19 hard cut: the old OT
//     history is not read; content is seeded into Y.Text from file bytes)
//
// The service is deliberately standalone (own process, runit unit) so the Go
// web binary stays slim; it shares ONLY the session store + Mongo, the same
// two systems every other service uses.
package collab

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"

	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/persistence"
	ws "github.com/reearth/ygo/provider/websocket"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Role — the authorization outcome for (user, project).
type Role int

const (
	Deny Role = iota
	ReadOnly
	ReadWrite
)

// AuthSource — request-level authorization for the collab endpoint.
// Split into an interface so the service logic tests run without Mongo/Redis.
type AuthSource interface {
	// Identity — the authenticated user id for the request ("", nil when
	// anonymous or the session is missing/expired), or an error on store
	// failure (the service denies on error — fail closed).
	Identity(r *http.Request) (string, error)
	// ProjectRole — the role of the user on the room's project.
	ProjectRole(uid, projectID string) (Role, error)
}

// Options — service construction inputs.
type Options struct {
	Auth AuthSource
	// Store — the versioned persistence for room state. S2 production =
	// MongoStore (NewMongoStore); dev/test = nil, which defaults to ygo's
	// FilePersistence under DataDir.
	Store   persistence.VersionedPersistence
	DataDir string // used only when Store == nil (FilePersistence root)
	// KeepVersions — history-retention policy for the server's automatic
	// compaction (ygo CompactableAdapter): 0 = keep ALL history (default);
	// >0 = when the server calls Compact, fold the oldest updates and retain
	// the most recent N. The compaction mechanics are conformance tested
	// (CompactTrimsOldest); this knob is the operator-facing policy.
	KeepVersions int
	// CompactEvery — how often the ygo server triggers Compact per room:
	// 0 = on room unload only (ygo default); >0 = also after every N
	// persistence flushes. 0 keeps the current (safe) behavior.
	CompactEvery int
	// SeedFn — server-side initial-content seeding (S3). The server is the
	// single source of the first content: clients join EMPTY and receive the
	// seed through the initial sync. Called once per room, only for rooms with
	// NO stored versions, via the ygo OnLoadDocument lifecycle hook; the
	// returned text is inserted as the room's version 1. A returned "" seeds
	// nothing; an error fails the room load (fail-closed). Nil = no seeding
	// (rooms stay empty until a client writes).
	SeedFn          func(ctx context.Context, room string) (string, error)
	AllowedOrigins  []string
	MaxConnections  int
	MaxPeersPerRoom int
}

// Service — the collab service. ServeHTTP implements http.Handler for the
// room endpoint; the room name is the base path segment.
type Service struct {
	Server *ws.Server
	store  persistence.VersionedPersistence
}

// Store — the versioned store backing this service (history endpoints use
// this surface directly).
func (s *Service) Store() persistence.VersionedPersistence { return s.store }

// New builds the service. ygo server = relay + doc lifecycle + persistence;
// the Authorize hook is the only place OlliTeX policy enters.
func New(opts Options) (*Service, error) {
	if opts.Auth == nil {
		return nil, errors.New("collab: Auth source required (fail closed)")
	}
	var store persistence.VersionedPersistence
	if opts.Store != nil {
		store = opts.Store
	} else {
		var err error
		store, err = persistence.NewFilePersistence(opts.DataDir)
		if err != nil {
			return nil, err
		}
	}
	// The ygo WS server takes the two-method PersistenceAdapter; the versioned
	// store speaks the fuller interface, so bridge with LegacyAdapter. The
	// adapter already implements the server's optional CompactableAdapter and
	// forwards to store.Compact(ctx, room, KeepVersions). KeepVersions is the
	// retention policy (0 = keep-all, the safe default); CompactEvery sets how
	// often the server triggers compaction (0 = on room unload only).
	adapter := persistence.NewLegacyAdapter(store)
	adapter.KeepVersions = opts.KeepVersions
	srv := ws.NewServerWithPersistence(adapter)
	srv.CompactEvery = opts.CompactEvery
	// Room seeding (S3): ygo fires OnLoadDocument exactly once per room when
	// the doc first loads. We seed only truly-empty rooms (no stored versions)
	// and persist the seed as version 1, so every peer's initial sync carries
	// it. Non-empty rooms are untouched (no double seed, no overwrite of an
	// intentionally emptied doc).
	if opts.SeedFn != nil {
		srv.OnLoadDocument = func(ctx context.Context, room string, doc *crdt.Doc) error {
			lv, lerr := store.Load(ctx, room)
			if lerr != nil {
				return lerr
			}
			if lv.Version > 0 {
				return nil // room already has history
			}
			text, serr := opts.SeedFn(ctx, room)
			if serr != nil {
				return serr
			}
			if text == "" {
				return nil // no seed content — room stays empty
			}
			doc.Transact(func(txn *crdt.Transaction) {
				txn.GetText(TextType).Insert(txn, 0, text, nil)
			})
			_, aerr := store.AppendUpdate(ctx, room, doc.EncodeStateAsUpdate())
			return aerr
		}
	}
	srv.Authorize = func(r *http.Request) (ws.ConnectionConfig, bool) {
		uid, err := opts.Auth.Identity(r)
		if err != nil || uid == "" {
			return ws.ConnectionConfig{}, false
		}
		room := path.Base(r.URL.Path)
		role, err := opts.Auth.ProjectRole(uid, room)
		if err != nil || role == Deny {
			return ws.ConnectionConfig{}, false
		}
		return ws.ConnectionConfig{ReadOnly: role == ReadOnly}, true
	}
	srv.AllowedOrigins = opts.AllowedOrigins
	srv.MaxConnections = opts.MaxConnections
	srv.MaxPeersPerRoom = opts.MaxPeersPerRoom
	return &Service{Server: srv, store: store}, nil
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.Server.ServeHTTP(w, r)
}

// Shutdown closes peer connections then the store (mirror of ygo-server).
func (s *Service) Shutdown(ctx context.Context) error {
	err := s.Server.Shutdown(ctx)
	if c, ok := any(s.store).(interface{ Close() error }); ok {
		if cerr := c.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}
	return err
}

// ---------------------------------------------------------------------------
// SessionAuth — production AuthSource: shared session store + Mongo.
//
// Session shape (pinned from the Go web core/session.go contract):
//   - cookie COOKIE_NAME (default "overleaf.sid"), value "s:<sid>"
//   - redis key "sess:<sid>" -> JSON session doc
//   - logged-in sessions carry "passport": {"user": {...}}; the user doc
//     serializes its _id as a hex string (web-app serialization).
//
// User liveness mirror of the node real-time AuthorizationManager: the user
// must exist and not be frozen (activeStatus != "frozen" — the node
// AuthorizationManager checked exactly this for editor access).
// ---------------------------------------------------------------------------

// Mongo — the minimal Mongo surface SessionAuth needs (testable).
type Mongo interface {
	UserByID(ctx context.Context, id string) (bson.D, error)
	ProjectByID(ctx context.Context, id string) (bson.D, error)
}

type SessionAuth struct {
	SessionDoc func(ctx context.Context, r *http.Request) (map[string]any, error) // redis session lookup (injectable)
	M          Mongo
	CookieName string // COOKIE_NAME; default "overleaf.sid"
	Ctx        context.Context
}

const defaultCookieName = "overleaf.sid"

func (a *SessionAuth) Identity(r *http.Request) (string, error) {
	ctx := a.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if a.SessionDoc == nil {
		return "", nil
	}
	sidCookieName := a.CookieName
	if sidCookieName == "" {
		sidCookieName = defaultCookieName
	}
	sess, err := a.SessionDoc(ctx, r)
	if err != nil || sess == nil {
		return "", nil // anonymous (or store said "no such session")
	}
	if v, ok := sess["passport"].(map[string]any); ok {
		if u, ok := v["user"].(map[string]any); ok {
			if id, ok := u["_id"].(string); ok && id != "" {
				// Node real-time AuthorizationManager parity: the user must
				// exist and not be frozen. Fail closed on store errors (the
				// service denies on error).
				if a.M != nil {
					ud, err := a.M.UserByID(ctx, id)
					if err != nil {
						return "", err
					}
					if ud == nil {
						return "", nil
					}
					if s, ok := dstr(ud, "activeStatus"); ok && s == "frozen" {
						return "", nil
					}
				}
				return id, nil
			}
		}
	}
	return "", nil
}

func (a *SessionAuth) ProjectRole(uid, projectID string) (Role, error) {
	ctx := a.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if a.M == nil {
		return Deny, nil
	}
	p, err := a.M.ProjectByID(ctx, projectID)
	if err != nil {
		return Deny, nil // project missing -> deny
	}
	if str, ok := dstr(p, "owner_ref"); ok && str == uid {
		return ReadWrite, nil
	}
	if arr, ok := darr(p, "collaborator_refs"); ok && arrContains(arr, uid) {
		return ReadWrite, nil
	}
	if arr, ok := darr(p, "readOnly_refs"); ok && arrContains(arr, uid) {
		return ReadOnly, nil
	}
	return Deny, nil
}

// dstr — string field from a bson.D (owner_ref serializes as a hex string in
// the web-serialized shape; tolerate a raw ObjectID too).
func dstr(d bson.D, key string) (string, bool) {
	for _, e := range d {
		if e.Key == key {
			if s, ok := e.Value.(string); ok {
				return s, true
			}
			if o, ok := e.Value.(primitive.ObjectID); ok {
				return o.Hex(), true
			}
		}
	}
	return "", false
}

func darr(d bson.D, key string) ([]any, bool) {
	for _, e := range d {
		if e.Key == key {
			if a, ok := e.Value.([]any); ok {
				return a, true
			}
			if a, ok := e.Value.(bson.A); ok {
				v := make([]any, len(a))
				for i, x := range a {
					v[i] = x
				}
				return v, true
			}
		}
	}
	return nil, false
}

func arrContains(arr []any, uid string) bool {
	for _, v := range arr {
		if s, ok := v.(string); ok && s == uid {
			return true
		}
		if o, ok := v.(primitive.ObjectID); ok && o.Hex() == uid {
			return true
		}
	}
	return false
}

// jsonRoundtrip — helper for tests/fakes marshaling session docs.
func jsonRoundtrip(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
