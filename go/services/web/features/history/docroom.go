// docroom.go — 024 Option B (web history plane): the per-document room
// helpers for the version-history surface.
//
// Under Option B each project document has its own collab room
// (go/services/collab roomkey.go): the ROOT document keeps the D19 room
// "{projectID}"; every other document owns "{projectID}-{docID}". The
// project's VERSION TIMELINE is the union of all its rooms (the panel's
// version list must advance when sample.bib is edited, not only main.tex),
// and a /diff range (project version indices) maps onto a file's own room
// by timestamp (the latest file-room version at-or-before each timeline
// point). This file provides:
//
//	projectDocRooms   the project's room set (root + per-doc in the tree)
//	docIDForPathname  pathname → doc id (+ the project's root doc)
//	fileRoomTimeline  the union feed (root + per-doc versions + yops)
//
// All helpers follow the existing seams: MongoDocSeeker for the project
// doc, collab store for room versions, the vlog for row meta.

package history

import (
	"context"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"encoding/json"

	"ollitex/go/services/collab"

	"github.com/reearth/ygo/persistence"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// projectRoom — one of a project's collab rooms with its display file path.
type projectRoom struct {
	Room      string
	Pathname  string // "" for the root room (resolved at render time)
	IsRoot    bool
	DocID     string // "" for the root room
	Lversions []persistence.VersionMeta
	Vmetas    map[uint64]collab.VersionMeta
}

// projectDocRooms — enumerate the project's rooms: the root room (the D19
// room "{pid}") plus one per NON-root doc in the project tree
// ("{pid}-{docID}"). Each room carries its version list + vlog meta
// (empty when the room has no versions — the feed handles that).
//
// The tree walk mirrors rootDocPathname/rootFolderPaths (docs[] at any
// depth; the live lineage shape = array-of-folder wrappers).
func projectDocRooms(ctx context.Context, st persistence.VersionedPersistence, vlog collab.Log, findOne func(ctx context.Context, coll string, id bson.ObjectID) (bson.D, error), pid string) ([]projectRoom, error) {
	po, err := bson.ObjectIDFromHex(strings.ToLower(pid))
	if err != nil {
		return nil, nil
	}
	proj, err := findOne(ctx, "projects", po)
	if err != nil || len(proj) == 0 {
		return nil, err
	}
	var rootDoc string
	var rootFolder any
	for _, e := range proj {
		switch e.Key {
		case "rootDoc_id":
			switch v := e.Value.(type) {
			case bson.ObjectID:
				rootDoc = strings.ToLower(v.Hex())
			case string:
				rootDoc = strings.ToLower(v)
			}
		case "rootFolder":
			rootFolder = e.Value
		}
	}
	root, _ := loadRoomMeta(ctx, st, vlog, pid)
	rooms := []projectRoom{{Room: pid, IsRoot: true, Lversions: root.lvs, Vmetas: root.vm}}
	if rootFolder == nil {
		return rooms, nil
	}
	type entDoc struct{ idHex, pathname string }
	var docs []entDoc
	var walk func(prefix string, v any)
	walk = func(prefix string, v any) {
		ent, ok := v.(bson.D)
		if !ok {
			if dm, dok := v.(bson.M); dok {
				ent = dmToD(dm)
			} else {
				return
			}
		}
		if docsArr := entArrAny(ent, "docs"); docsArr != nil {
			for _, de := range docsArr {
				m, dok := de.(bson.D)
				if !dok {
					if dm, dok := de.(bson.M); dok {
						m = dmToD(dm)
					} else {
						continue
					}
				}
				name, ok := entField(m, "name")
				if !ok || name == "" {
					continue
				}
				idv := entIDHex(m)
				docs = append(docs, entDoc{idHex: idv, pathname: prefix + strings.TrimPrefix(name, "/")})
			}
		}
		if folders := entArrAny(ent, "folders"); folders != nil {
			fname := ""
			if nm, ok := entField(ent, "name"); ok {
				fname = nm + "/"
			}
			for _, f := range folders {
				walk(fname, f)
			}
		}
	}
	switch rf := rootFolder.(type) {
	case []any:
		for _, e := range rf {
			walk("", e)
		}
	case bson.A:
		for _, e := range rf {
			walk("", e)
		}
	case []bson.D:
		for _, e := range rf {
			walk("", bson.D(e))
		}
	case bson.D:
		walk("", rf)
	}
	for _, d := range docs {
		if d.idHex == "" {
			continue
		}
		if d.idHex == rootDoc {
			continue // the root doc IS the root room (contract)
		}
		r, _ := loadRoomMeta(ctx, st, vlog, pid+"-"+d.idHex)
		rooms = append(rooms, projectRoom{Room: pid + "-" + d.idHex, Pathname: d.pathname, IsRoot: false, DocID: d.idHex, Lversions: r.lvs, Vmetas: r.vm})
	}
	return rooms, nil
}

// loadRoomMeta — (versions, vlog-meta) for one room (nil-safe).
type roomMeta struct {
	lvs []persistence.VersionMeta
	vm  map[uint64]collab.VersionMeta
}

func loadRoomMeta(ctx context.Context, st persistence.VersionedPersistence, vlog collab.Log, room string) (roomMeta, error) {
	lvs, err := st.ListVersions(ctx, room)
	if err != nil {
		return roomMeta{}, err
	}
	var vm map[uint64]collab.VersionMeta
	if vlog != nil && len(lvs) > 0 {
		var maxV uint64
		for _, lv := range lvs {
			if uint64(lv.Version) > maxV {
				maxV = uint64(lv.Version)
			}
		}
		if metas, rerr := vlog.Range(ctx, room, 0, maxV); rerr == nil {
			vm = map[uint64]collab.VersionMeta{}
			for _, m := range metas {
				vm[m.V] = m
			}
		}
	}
	return roomMeta{lvs: lvs, vm: vm}, nil
}

// docIDForPathname — resolve a display pathname (e.g. "sample.bib" or
// "tex/sub/main.tex") to its doc id (+ the project's root doc id).
// ("","") when the pathname is not in the project tree.
func docIDForPathname(ctx context.Context, findOne func(ctx context.Context, coll string, id bson.ObjectID) (bson.D, error), pid, pathname string) (docID, rootDoc string) {
	po, err := bson.ObjectIDFromHex(strings.ToLower(pid))
	if err != nil {
		return "", ""
	}
	proj, err := findOne(ctx, "projects", po)
	if err != nil || len(proj) == 0 {
		return "", ""
	}
	var rootFolder any
	for _, e := range proj {
		switch e.Key {
		case "rootDoc_id":
			switch v := e.Value.(type) {
			case bson.ObjectID:
				rootDoc = strings.ToLower(v.Hex())
			case string:
				rootDoc = strings.ToLower(v)
			}
		case "rootFolder":
			rootFolder = e.Value
		}
	}
	if rootFolder == nil {
		return "", ""
	}
	parts := strings.Split(strings.Trim(pathname, "/"), "/")
	var found string
	var walk func(prefix []string, v any)
	walk = func(prefix []string, v any) {
		if found != "" {
			return
		}
		ent, ok := v.(bson.D)
		if !ok {
			if dm, dok := v.(bson.M); dok {
				ent = dmToD(dm)
			} else {
				return
			}
		}
		if docsArr := entArrAny(ent, "docs"); docsArr != nil {
			for _, de := range docsArr {
				m, dok := de.(bson.D)
				if !dok {
					if dm, dok := de.(bson.M); dok {
						m = dmToD(dm)
					} else {
						continue
					}
				}
				name, ok := entField(m, "name")
				if !ok || name == "" {
					continue
				}
				full := append(append([]string{}, prefix...), strings.TrimPrefix(name, "/"))
				if len(full) == len(parts) {
					match := true
					for i := range parts {
						if full[i] != parts[i] {
							match = false
							break
						}
					}
					if match {
						found = entIDHex(m)
						return
					}
				}
			}
		}
		if folders := entArrAny(ent, "folders"); folders != nil {
			fname := ""
			if nm, ok := entField(ent, "name"); ok {
				fname = nm
			}
			for _, f := range folders {
				walk(append(append([]string{}, prefix...), fname), f)
			}
		}
	}
	switch rf := rootFolder.(type) {
	case []any:
		for _, e := range rf {
			walk(nil, e)
		}
	case bson.A:
		for _, e := range rf {
			walk(nil, e)
		}
	case []bson.D:
		for _, e := range rf {
			walk(nil, bson.D(e))
		}
	case bson.D:
		walk(nil, rf)
	}
	if found == "" {
		return "", rootDoc
	}
	return found, rootDoc
}

// docstoreDocLines — the file's CURRENT content from the docstore (the
// store of record for text content in this stack), as a single joined
// string ("\n"-joined lines, no trailing newline). "" when the docstore is
// unreachable or the doc is unknown (the pane then shows an empty diff —
// an honest state).
func docstoreDocLines(ctx context.Context, pid, did string) string {
	base := os.Getenv("WEB_DOCSTORE_URL")
	if base == "" {
		base = "http://127.0.0.1:3016"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/project/"+pid+"/doc/"+did, nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	var doc struct {
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(body, &doc); err != nil || doc.Lines == nil {
		return ""
	}
	return strings.Join(doc.Lines, "\n")
}

// entIDHex — an entity'"'"'s _id as lowercase hex ("" when absent).
func entIDHex(m bson.D) string {
	switch v := entIDRaw(m).(type) {
	case bson.ObjectID:
		return strings.ToLower(v.Hex())
	case string:
		return strings.ToLower(v)
	}
	return ""
}

func entIDRaw(m bson.D) any {
	for i := 0; i < len(m); i++ {
		if m[i].Key == "_id" {
			return m[i].Value
		}
	}
	return nil
}

// fileTimelineMeta — the union timeline point for one file-room version
// (shared by /updates and the /diff range mapping).
type fileTimelinePoint struct {
	Ts       int64 // millisecond UTC
	Room     string
	RoomV    uint64
	Pathname string
	Meta     map[string]any
}

// buildFileTimeline — rooms → one sorted timeline (ts order, ties: root
// room first, then room name) with a unified index (1..N). This is the
// project's version index under Option B (the panel's /updates rows and
// the /diff from/to parameters both use it).
func buildFileTimeline(rooms []projectRoom, rootPath string) []fileTimelinePoint {
	var pts []fileTimelinePoint
	for _, r := range rooms {
		path := r.Pathname
		if r.IsRoot {
			path = rootPath
		}
		for _, lv := range r.Lversions {
			ts := lv.UpdatedAt.UTC().UnixMilli()
			users := []any{}
			metaInfo, has := r.Vmetas[uint64(lv.Version)]
			if has && metaInfo.UID != "" {
				users = []any{metaInfo.UID}
			}
			m := map[string]any{"users": users, "start_ts": ts, "end_ts": ts}
			if has {
				if o, hasO := originObject(metaInfo.Origin, path, ts); hasO {
					m["origin"] = o
				}
			}
			pts = append(pts, fileTimelinePoint{Ts: ts, Room: r.Room, RoomV: uint64(lv.Version), Pathname: path, Meta: m})
		}
	}
	sort.Slice(pts, func(i, j int) bool {
		if pts[i].Ts != pts[j].Ts {
			return pts[i].Ts < pts[j].Ts
		}
		if pts[i].Room != pts[j].Room {
			return pts[i].Room < pts[j].Room
		}
		return pts[i].RoomV < pts[j].RoomV
	})
	return pts
}

// fileAtTimeline — the room version in force for room at/before unified
// index idx (1-based; idx=0 → version 0 = empty), over the unified feed
// (mergedFeed; its V field carries the per-room version, Path the file).
func fileAtTimeline(feed mergedFeed, idx int, room string) (uint64, bool) {
	best := uint64(0)
	found := false
	for i := 0; i < len(feed) && i < idx; i++ {
		if feed[i].Source != 0 {
			continue
		}
		if len(feed[i].Path) == 0 {
			continue
		}
		if !found || uint64(feed[i].V) > best {
			// the feed is sorted by ts with the room-name tie-break, so the
			// LAST text point from this room before idx is its state
			if roomPoint(feed[i], room) {
				best = uint64(feed[i].V)
				found = true
			}
		}
	}
	return best, found
}

// roomPoint — a feed item's room identity (root room = "" path marker is
// the project's root file; per-doc rooms carry their file pathname). The
// identity is recovered via the item's room — encoded in the V source:
// items are produced room-by-room in buildUnifiedFeedMulti, so we track
// the room on the item itself (see the Room field added there).
func roomPoint(it mergedFeedItem, room string) bool {
	return it.Room == room
}
