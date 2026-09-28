package syncmanager

import (
	"fmt"
	"time"
)

// Vendor constants (exact).
const (
	MaxResyncHistoryRecords = 100
	ExpireResyncHistoryMs   = int64(90 * 24 * 3600 * 1000)
	SyncStuckTimeoutMs      = int64(4 * 60 * 60 * 1000)
	MaxStuckClearAttempts   = 5
	SyncOngoingErrorMessage = "sync ongoing"
)

// isTextUpdate — vendor UpdateTranslator.isTextUpdate (doc/op non-null,
// meta.pathname non-null, meta.doc_length non-null).
func isTextUpdate(update map[string]any) bool {
	doc, docOK := update["doc"]
	op, opOK := update["op"]
	if !docOK || doc == nil || !opOK || op == nil {
		return false
	}
	meta, ok := update["meta"].(map[string]any)
	if !ok || meta["pathname"] == nil || meta["doc_length"] == nil {
		return false
	}
	return true
}

// isProjectStructureUpdate — vendor: version non-null.
func isProjectStructureUpdate(update map[string]any) bool {
	v, ok := update["version"]
	return ok && v != nil
}

// SyncError — vendor Errors.SyncError (an OError subclass; message + info).
type SyncError struct {
	Msg  string
	Info map[string]any
}

func (e *SyncError) Error() string { return e.Msg }

// InfoOf returns the info map (nil-safe).
func (e *SyncError) InfoOf() map[string]any {
	if e.Info == nil {
		return map[string]any{}
	}
	return e.Info
}

// SyncState — vendor SyncState class, 1:1 (constructor fields + methods).
// ResyncDocContents is the vendor Set (dedup, first-seen order).
type SyncState struct {
	ProjectID              string
	ResyncProjectStructure bool
	ResyncDocContents      []string
	Origin                 map[string]any
	ResyncCount            int
	ResyncPendingSince     *time.Time // nil = absent
	LastUpdated            any
	History                []map[string]any
	StuckClearCount        int
	LastStuckClearAt       any
	LastStuckDocPaths      []string
	HardResync             bool
	RecoverCorruptedFiles  bool
}

// intAny — tolerant int parse (numbers arrive as int/int64/float64/string).
func intAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		var out int
		if _, err := fmt.Sscanf(n, "%d", &out); err == nil {
			return out, true
		}
	}
	return 0, false
}

func strList(v any) []string {
	switch l := v.(type) {
	case []string:
		return l
	case []any:
		out := make([]string, 0, len(l))
		for _, e := range l {
			s, _ := e.(string)
			out = append(out, s)
		}
		return out
	}
	return nil
}

// FromRaw — vendor SyncState.fromRaw (incl. the resyncPendingSince back-fill).
func FromRaw(projectID string, raw any) *SyncState {
	var r map[string]any
	if raw != nil {
		m, _ := raw.(map[string]any)
		r = m
	}
	if r == nil {
		r = map[string]any{}
	}
	resyncProjectStructure, _ := r["resyncProjectStructure"].(bool)
	resyncDocContents := strList(r["resyncDocContents"])
	if resyncDocContents == nil {
		resyncDocContents = []string{}
	}
	origin, _ := r["origin"].(map[string]any)
	resyncCount, _ := intAny(r["resyncCount"])
	var resyncPendingSince *time.Time
	if ts, ok := r["resyncPendingSince"]; ok && ts != nil {
		if t, ok2 := ts.(time.Time); ok2 {
			resyncPendingSince = &t
		}
	}
	history, _ := r["history"].([]map[string]any)
	if h, ok := r["history"].([]any); ok {
		history = nil
		for _, e := range h {
			if m, ok2 := e.(map[string]any); ok2 {
				history = append(history, m)
			}
		}
	}
	stuckClearCount, _ := intAny(r["stuckClearCount"])
	if v, ok := r["stuckClearCount"]; ok && v == nil {
		stuckClearCount = 0
	}
	hardResync, _ := r["hardResync"].(bool)
	recoverCorruptedFiles, _ := r["recoverCorruptedFiles"].(bool)

	if (resyncProjectStructure || len(resyncDocContents) > 0) &&
		resyncPendingSince == nil && len(history) > 0 {
		// vendor back-fill: history is DESC; iterate in storage order reversed.
		for i := len(history) - 1; i >= 0; i-- {
			entry := history[i]
			ss, _ := entry["syncState"].(map[string]any)
			if ss == nil {
				ss = map[string]any{}
			}
			ongoing, _ := ss["resyncProjectStructure"].(bool)
			contents := strList(ss["resyncDocContents"])
			isOngoing := ongoing || len(contents) > 0
			if isOngoing {
				if resyncPendingSince == nil {
					if ts, ok := entry["timestamp"]; ok && ts != nil {
						if t, ok2 := ts.(time.Time); ok2 {
							resyncPendingSince = &t
						}
					}
				}
			} else {
				resyncPendingSince = nil
			}
		}
	}

	return &SyncState{
		ProjectID:              projectID,
		ResyncProjectStructure: resyncProjectStructure,
		ResyncDocContents:      resyncDocContents,
		Origin:                 origin,
		ResyncCount:            resyncCount,
		ResyncPendingSince:     resyncPendingSince,
		LastUpdated:            r["lastUpdated"],
		History:                history,
		StuckClearCount:        stuckClearCount,
		LastStuckClearAt:       r["lastStuckClearAt"],
		LastStuckDocPaths:      strList(r["lastStuckDocPaths"]),
		HardResync:             hardResync,
		RecoverCorruptedFiles:  recoverCorruptedFiles,
	}
}

// ToRaw — vendor: EXACTLY 5 keys.
func (s *SyncState) ToRaw() map[string]any {
	return map[string]any{
		"resyncProjectStructure": s.ResyncProjectStructure,
		"resyncDocContents":      strListCopy(s.ResyncDocContents),
		"origin":                 s.Origin,
		"hardResync":             s.HardResync,
		"recoverCorruptedFiles":  s.RecoverCorruptedFiles,
	}
}

func strListCopy(l []string) []any {
	out := make([]any, 0, len(l))
	for _, e := range l {
		out = append(out, e)
	}
	return out
}

// UpdateState — vendor (the three SyncError branches, verbatim conditions).
func (s *SyncState) UpdateState(update map[string]any) error {
	psr, hasPSR := update["resyncProjectStructure"]
	if hasPSR && psr != nil {
		if !s.IsProjectStructureSyncing() {
			return &SyncError{
				Msg: "unexpected resyncProjectStructure update",
				Info: map[string]any{
					"projectId":              s.ProjectID,
					"resyncProjectStructure": s.ResyncProjectStructure,
				},
			}
		}
		if s.IsAnyDocContentSyncing() {
			return &SyncError{
				Msg: "unexpected resyncDocContents update",
				Info: map[string]any{
					"projectId":         s.ProjectID,
					"resyncDocContents": strListCopy(s.ResyncDocContents),
				},
			}
		}
		if !truthy(update["resyncProjectStructureOnly"]) {
			ps, _ := update["resyncProjectStructure"].(map[string]any)
			if ps != nil {
				if docs, ok := ps["docs"].([]any); ok {
					for _, d := range docs {
						if dm, ok2 := d.(map[string]any); ok2 {
							if p, ok3 := dm["path"]; ok3 {
								s.StartDocContentSync(fmt.Sprintf("%v", p))
							}
						}
					}
				}
			}
		}
		s.StopProjectStructureSync()
		return nil
	}
	if rdc, hasRDC := update["resyncDocContent"]; hasRDC && rdc != nil {
		if s.IsProjectStructureSyncing() {
			return &SyncError{
				Msg: "unexpected resyncDocContent update",
				Info: map[string]any{
					"projectId":              s.ProjectID,
					"resyncProjectStructure": s.ResyncProjectStructure,
				},
			}
		}
		path := fmt.Sprintf("%v", update["path"])
		if !s.IsDocContentSyncing(path) {
			return &SyncError{
				Msg: "unexpected resyncDocContent update",
				Info: map[string]any{
					"projectId":         s.ProjectID,
					"resyncDocContents": strListCopy(s.ResyncDocContents),
					"path":              path,
				},
			}
		}
		s.StopDocContentSync(path)
		return nil
	}
	return nil
}

func truthy(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return true
}

func (s *SyncState) SetOrigin(origin map[string]any) { s.Origin = origin }

// ShouldSkipUpdate — vendor.
func (s *SyncState) ShouldSkipUpdate(update map[string]any) bool {
	if v, has := update["resyncProjectStructure"]; has && v != nil {
		return false
	}
	if v, has := update["resyncDocContent"]; has && v != nil {
		return false
	}
	if s.IsProjectStructureSyncing() {
		return true
	}
	if isTextUpdate(update) {
		meta, _ := update["meta"].(map[string]any)
		if meta != nil {
			if p, ok := meta["pathname"]; ok {
				if s.IsDocContentSyncing(fmt.Sprintf("%v", p)) {
					return true
				}
			}
		}
	}
	return false
}

func (s *SyncState) StartProjectStructureSync() {
	s.ResyncProjectStructure = true
	s.ResyncDocContents = []string{}
}

func (s *SyncState) StopProjectStructureSync() { s.ResyncProjectStructure = false }

func (s *SyncState) StopDocContentSync(path string) {
	out := []string{}
	for _, p := range s.ResyncDocContents {
		if p != path {
			out = append(out, p)
		}
	}
	s.ResyncDocContents = out
}

func (s *SyncState) StartDocContentSync(path string) {
	for _, p := range s.ResyncDocContents {
		if p == path {
			return
		}
	}
	s.ResyncDocContents = append(s.ResyncDocContents, path)
}

func (s *SyncState) IsProjectStructureSyncing() bool { return s.ResyncProjectStructure }

func (s *SyncState) IsDocContentSyncing(path string) bool {
	for _, p := range s.ResyncDocContents {
		if p == path {
			return true
		}
	}
	return false
}

func (s *SyncState) IsAnyDocContentSyncing() bool { return len(s.ResyncDocContents) > 0 }

func (s *SyncState) IsSyncOngoing() bool {
	return s.IsProjectStructureSyncing() || s.IsAnyDocContentSyncing()
}

// IsSyncStuck — vendor.
func (s *SyncState) IsSyncStuck(now time.Time) bool {
	if !s.IsSyncOngoing() {
		return false
	}
	if s.ResyncPendingSince == nil {
		return true
	}
	return now.Sub(*s.ResyncPendingSince).Milliseconds() > SyncStuckTimeoutMs
}

func (s *SyncState) IsSyncPermanentlyStuck() bool {
	return s.StuckClearCount >= MaxStuckClearAttempts
}
