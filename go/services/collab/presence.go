// Package collab — presence (AJ-3 / admin "Active projects").
//
// The legacy contract (GET /admin/active-projects) read the Node realtime
// service's /clients list. That service is gone; the Go collab service IS
// the editor's active-session plane, so it owns this signal:
//
//   - every authorized connection is admitted through the Authorize hook,
//     which is where the (room, uid) pair is registered;
//   - rooms are dropped when their last peer leaves / the room unloads
//     (ygo lifecycle hooks) — the ygo server exposes no per-connection
//     disconnect hook, so the remaining signal is the registry TTL
//     (a socket idle longer than presenceTTL stops reporting);
//   - ListProjects returns the live (projectId, userId) pairs; the web
//     feature (projectlist/activeprojects) does authz + the name/email
//     projection, so the collab service stays a dumb presence plane.
package collab

import (
	"sort"
	"sync"
	"time"
)

const presenceTTL = 90 * time.Second // idle past this = "gone"

// PresenceEntry — one live (room, user) pair.
type PresenceEntry struct {
	Room   string `json:"room"`
	UserID string `json:"userId"`
}

type presenceRegistry struct {
	mu    sync.RWMutex
	rooms map[string]map[string]time.Time // room -> uid -> last seen
}

// NewPresenceRegistry — the exported constructor so the service main can wire
// the registry into Options (the type itself stays package-internal). Without
// this the Authorize-hook Add() path is unreachable from cmd/collab and
// /presence reports an empty plane forever (AJ-3 was read-wired but never
// write-wired; fixed 2026-10-12, board: e2e realtime/presence gate).
func NewPresenceRegistry() *presenceRegistry {
	return &presenceRegistry{rooms: map[string]map[string]time.Time{}}
}

func (p *presenceRegistry) Add(room, uid string) {
	if room == "" || uid == "" {
		return
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	m, ok := p.rooms[room]
	if !ok {
		m = map[string]time.Time{}
		p.rooms[room] = m
	}
	m[uid] = now
}

func (p *presenceRegistry) DropRoom(room string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.rooms, room)
}

func (p *presenceRegistry) Entries() []PresenceEntry {
	cutoff := time.Now().Add(-presenceTTL)
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := []PresenceEntry{}
	for room, users := range p.rooms {
		for uid, last := range users {
			if last.Before(cutoff) {
				continue
			}
			out = append(out, PresenceEntry{Room: room, UserID: uid})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Room == out[j].Room {
			return out[i].UserID < out[j].UserID
		}
		return out[i].Room < out[j].Room
	})
	return out
}

// ListProjects — the live (projectId, userId) pairs, using RoomProject so
// per-doc rooms (024 Option B) collapse onto their project.
func ListProjects(p *presenceRegistry) []PresenceEntry {
	raw := p.Entries()
	out := raw[:0]
	seen := map[string]bool{}
	for _, e := range raw {
		pid := RoomProject(e.Room)
		if pid == "" {
			pid = e.Room
		}
		key := pid + string(byte(0)) + e.UserID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, PresenceEntry{Room: pid, UserID: e.UserID})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Room == out[j].Room {
			return out[i].UserID < out[j].UserID
		}
		return out[i].Room < out[j].Room
	})
	return out
}
