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
	"log/slog"
	"net/http"
	"path"

	"github.com/reearth/ygo/crdt"
	"github.com/reearth/ygo/persistence"
	ws "github.com/reearth/ygo/provider/websocket"
	"go.mongodb.org/mongo-driver/v2/bson"
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
	// VersionLog — optional d5dd23dd S1 actor/origin side log: when set,
	// the service wraps its store so every AppendUpdate mirrors its
	// (uid, origin) metadata into the log (fail-soft; see versionlog.go).
	VersionLog Log
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
	SeedFn func(ctx context.Context, room string) (string, error)
	// LegacyBackfill — D40 P4: materialize the legacy (OT-era) threads/
	// comments/tracked-changes into the room's review records at FIRST
	// seed (best-effort; legacy failures are logged by the implementer and
	// NEVER fail the room load — a seed that succeeded stands on its own).
	// Nil = no backfill (rooms get review records only through the D40
	// surface). Called once, after the seed is persisted as version 1.
	LegacyBackfill  func(ctx context.Context, room string, store persistence.VersionedPersistence) error
	AllowedOrigins  []string
	MaxConnections  int
	MaxPeersPerRoom int
	// Logger — ygo server logger (slog). Nil = ygo's slog.Default()
	// (Info). Operations may set a Debug-level logger to see dropped sync
	// frames / persistence decisions (D22: structured ops surface).
	Logger *slog.Logger
	// PresenceRegistry — AJ-3 "active projects": the in-process
	// (room, uid) presence plane (presence.go). Nil = disabled (the
	// admin surface reports an empty list).
	PresenceRegistry *presenceRegistry

	// Lifecycle observers (ops; nil = ignored): peer transitions and room
	// unload. D22: room occupancy is the first real-time signal we can
	// surface for the collab engine.
	OnFirstPeer      func(room string)
	OnLastPeer       func(room string)
	OnUnloadDocument func(room string)
	// WriteBack — 024 Option B: room→docstore write-through (the
	// persistence-of-record bridge; see writeback.go). Wrap with
	// WrapWriteBack to hook the store, and call FlushRoom from
	// OnUnloadDocument / Shutdown. Nil = disabled (pure CRDT plane).
	WriteBack *WriteBack
}

// Service — the collab service. ServeHTTP implements http.Handler for the
// room endpoint; the room name is the base path segment.
type Service struct {
	Server *ws.Server
	store  persistence.VersionedPersistence

	// presence — AJ-3: live (room, uid) registry behind Presences().
	presence *presenceRegistry
}

// Store — the versioned store backing this service (history endpoints use
// this surface directly).
func (s *Service) Store() persistence.VersionedPersistence { return s.store }

// Presences — the live collaboration pairs (projectId, user id); see presence.go.
func (s *Service) Presences() []PresenceEntry {
	if s.presence == nil {
		return []PresenceEntry{}
	}
	return ListProjects(s.presence)
}

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
	// d5dd23dd S1: actor/origin side log (no-op when opts.VersionLog is nil).
	store = Wrap(store, opts.VersionLog)
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
			if aerr != nil {
				return aerr
			}
			// D40 P4: legacy corpus → review records (after v1 exists; the
			// writers idempotently append as v2+; best-effort by contract).
			if opts.LegacyBackfill != nil {
				if berr := opts.LegacyBackfill(ctx, room, store); berr != nil {
					return berr // implementer policy (P4 wiring logs + continues)
				}
			}
			return nil
		}
	}
	srv.Authorize = func(r *http.Request) (ws.ConnectionConfig, bool) {
		uid, err := opts.Auth.Identity(r)
		if err != nil || uid == "" {
			return ws.ConnectionConfig{}, false
		}
		room := path.Base(r.URL.Path)
		// 024 Option B: the room may be a per-doc room ({pid}-{docID}); the
		// role always resolves against the room's PROJECT (roomkey.go).
		pid := RoomProject(room)
		if pid == "" {
			// Legacy tolerance (D19-era and test room names that are not the
			// 024 hex24/hex24-hex24 shapes): fall back to the D19 contract
			// (room name == project id). The SEED stays strict (roomkey.go)
			// — only the role check is tolerant.
			pid = room
		}
		role, err := opts.Auth.ProjectRole(uid, pid)
		if err != nil || role == Deny {
			return ws.ConnectionConfig{}, false
		}
		// AJ-3: the Authorize hook is the ONLY admission path, so the
		// presence registry sees every live connection exactly once.
		if opts.PresenceRegistry != nil && pid != "" {
			opts.PresenceRegistry.Add(pid, uid)
		}
		return ws.ConnectionConfig{ReadOnly: role == ReadOnly}, true
	}
	srv.AllowedOrigins = opts.AllowedOrigins
	srv.MaxConnections = opts.MaxConnections
	srv.MaxPeersPerRoom = opts.MaxPeersPerRoom
	if opts.Logger != nil {
		srv.Logger = opts.Logger
	}
	if opts.OnFirstPeer != nil {
		srv.OnFirstPeer = func(ctx context.Context, room string) { opts.OnFirstPeer(room) }
	}
	if opts.OnLastPeer != nil || opts.PresenceRegistry != nil {
		srv.OnLastPeer = func(ctx context.Context, room string) {
			pid := RoomProject(room)
			if pid == "" {
				pid = room
			}
			if opts.PresenceRegistry != nil {
				opts.PresenceRegistry.DropRoom(pid)
			}
			if opts.OnLastPeer != nil {
				opts.OnLastPeer(room)
			}
		}
	}
	if opts.OnUnloadDocument != nil {
		srv.OnUnloadDocument = func(ctx context.Context, room string) {
		// 024 Option B: the last peer left (or the room is being recycled) —
		// flush the write-through so the docstore carries the converged head
		// even when the session ends without more edits (fail-soft inside).
		if opts.WriteBack != nil {
			opts.WriteBack.FlushRoom(ctx, room)
		}
		if opts.OnUnloadDocument != nil {
			opts.OnUnloadDocument(room)
		}
	}
	}
	return &Service{Server: srv, store: store, presence: opts.PresenceRegistry}, nil
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
//   - cookie COOKIE_NAME (default "overleaf.sid"), value
//     percent-encode("s:" + sign(sid, SESSION_SECRET)) — decoded, stripped,
//     and un-signed exactly as the web does (see mongo.go sessionSid);
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
	// membership field drift (caught by Q two-cooperator e2e 2026-10-10):
	// the canonical schema field is `collaberator_refs` (see create.go /
	// colSetLevel); pre-migration docs may carry the legacy
	// `collab_refs` / `collaborator_refs` spellings. Accept all three —
	// the previous single-field check 401'd every real collaborator at the
	// WebSocket handshake (owner_ref was the only path that worked).
	for _, k := range []string{"collaberator_refs", "collab_refs", "collaborator_refs"} {
		if arr, ok := darr(p, k); ok && arrContains(arr, uid) {
			return ReadWrite, nil
		}
	}
	for _, k := range []string{"readOnly_refs", "readonly_refs"} {
		if arr, ok := darr(p, k); ok && arrContains(arr, uid) {
			return ReadOnly, nil
		}
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
			if o, ok := e.Value.(bson.ObjectID); ok {
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
		if o, ok := v.(bson.ObjectID); ok && o.Hex() == uid {
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
