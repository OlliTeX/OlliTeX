// legacybackfill.go — D40 P4: legacy (OT-era) comments/threads/tracked-changes
// backfill on FIRST room seed.
//
// The legacy world stores review data in TWO places (both read-side proven):
//
//  1. the docstore root document: `ranges.comments` (pointers
//     `{id, op:{t: threadId, p}, state?, metadata:{user_id, ts, name?}}`) and
//     `ranges.changes` (`{id, op:{i|d, p}, state?, metadata:{...}}`; op.i =
//     inserted text, op.d = deleted text — the d12 panel shape, pinned from
//     use-project-ranges.ts). The seed source already fetches this SAME
//     document (S4 contract) — P4 reuses the resolution + fetch and reads
//     the `ranges` field that seeding ignores.
//
//  2. the Node track-changes/chat service (WEB_CHAT_URL / CHAT_HOST:3010 —
//     the same bases the trackchanges DU-proxy feature calls):
//     GET /project/{pid}/threads → [{messages:[{id, content, timestamp,
//     user_id, edited_at?}], resolved?, resolved_at?}]
//     (shape pinned from features/trackchanges/handlers.go tcThread/tcMsg).
//
// P4 materializes both into the Y.Doc review records (the P1–P3 domain
// shapes) at first seed — the data-migration step that lets the OT engine
// retire once the last OT doc has a room (D41 note). Semantics:
//
//   - threads → Thread records (state opened|resolved, resolved-at passthrough).
//   - thread messages → Comment records (ThreadID set; text = message content).
//   - comment pointers (op.t) → the thread's message RANGES: a zero-length
//     range {start:p, end:p} — the legacy pointer carries exactly one
//     position (the d12 panel contract); no coordinate is invented.
//   - changes → TrackedChange records: insert = op.i at p (end = p+len),
//     delete = op.d at p; legacy state passthrough (pending|accepted|
//     rejected, default pending). Backfill NEVER mutates room content —
//     records carry content; the Y world renders it (d11b shape).
//
// Failure policy (best-effort, never breaks the seed): a docstore failure
// skips ranges (and pointer-derived ranges); a chat failure skips threads
// (pointers without a thread have no message content — skipping them rather
// than minting content-less comments is the honest call). What was skipped
// is logged by the caller (Backfill stats).
package collab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/reearth/ygo/persistence"

	"ollitex/go/libraries/configres"
)

// ---------------------------------------------------------------------------
// Legacy shapes (read-side only — pinned above; never write to legacy stores).

// LegacyMessage — Node TC chat message (tcMsg).
type LegacyMessage struct {
	ID        string
	Content   string
	Timestamp int64 // ms (TC timestamps are ms in the wire pinned above)
	UserID    string
	EditedAt  *int64
}

// LegacyThread — Node TC chat thread (tcThread, resolved* flattened).
type LegacyThread struct {
	ID         string
	Messages   []LegacyMessage
	Resolved   bool
	ResolvedAt int64  // ms; 0 = none
	ResolvedBy string // resolved_by_user_id ("" = unknown)
}

// LegacyPointer — a docstore `ranges.comments` entry (comment pointer).
type LegacyPointer struct {
	ID       string
	ThreadID string // op.t
	Start    int    // op.p
	State    string
	Metadata map[string]any
}

// LegacyChangeOp — a docstore `ranges.changes` entry (op.i XOR op.d).
type LegacyChangeOp struct {
	ID       string
	InsText  string // op.i ("" = not an insert)
	DelText  string // op.d ("" = not a delete)
	Start    int    // op.p
	State    string
	Metadata map[string]any
}

// LegacyData — the full legacy review corpus for one room (seeded root doc).
type LegacyData struct {
	Threads  []LegacyThread
	Pointers []LegacyPointer
	Changes  []LegacyChangeOp
	// Skipped — human-readable notes: "threads: <reason>", "ranges: <reason>"
	// (which legacy side did not deliver; empty = full corpus).
	Skipped []string
}

// ---------------------------------------------------------------------------
// Legacy source — legacy side of the seed (docstore + TC chat service).

// LegacySource — where the legacy review corpus comes from. Docs reuses the
// S4 seed's project resolution + docstore fetch (SeedSource); ChatBase is
// the Node track-changes/chat service (same env convention as the
// trackchanges feature: WEB_CHAT_URL, else CHAT_HOST:3010).
type LegacySource struct {
	Docs     *SeedSource
	ChatBase string
	HTTP     *http.Client
	// MaxBounds caps the legacy corpus (safety valve, mirrors maxSeedBytes
	// in spirit for JSON).
	MaxBytes int
}

// NewLegacySource wires the production legacy source (env defaults mirror
// features/trackchanges/downstream.go so both consumers agree).
func NewLegacySource(docs *SeedSource) *LegacySource {
	st := configres.Open()
	chat := configres.String(st, "WEB_CHAT_URL", "", "")
	if chat == "" {
		chat = os.Getenv("WEB_CHAT_URL")
	}
	if chat == "" {
		host := os.Getenv("CHAT_HOST")
		if host == "" {
			host = "127.0.0.1"
		}
		chat = "http://" + host + ":3010"
	}
	return &LegacySource{
		Docs:     docs,
		ChatBase: chat,
		HTTP:     &http.Client{Timeout: 5 * time.Second},
		MaxBytes: 8 << 20,
	}
}

func legacyLimit(n int) int64 {
	if n <= 0 {
		n = 8 << 20
	}
	return int64(n)
}

// Load — the legacy corpus for the room named <projectId>. Best-effort:
// each legacy side is independent; failures land in LegacyData.Skipped.
func (s *LegacySource) Load(ctx context.Context, room string) (LegacyData, error) {
	var out LegacyData
	if s.Docs == nil {
		out.Skipped = append(out.Skipped, "ranges: no docstore seed source configured")
		return out, nil
	}
	// 1) docstore ranges (SAME root doc as the seed text).
	p, perr := s.Docs.P.ProjectByID(ctx, room)
	if perr != nil {
		out.Skipped = append(out.Skipped, fmt.Sprintf("ranges: project not resolved: %v", perr))
		return out, nil
	}
	docID := rootDocID(p)
	if docID == "" {
		out.Skipped = append(out.Skipped, "ranges: project has no rootDoc (nothing to backfill)")
		return out, nil
	}
	body, status, ferr := fetchSeedDoc(ctx, s.Docs, room, docID)
	if ferr != nil {
		out.Skipped = append(out.Skipped, fmt.Sprintf("ranges: docstore fetch failed: %v", ferr))
		return out, nil
	}
	if status != http.StatusOK {
		out.Skipped = append(out.Skipped, fmt.Sprintf("ranges: docstore HTTP %d", status))
		return out, nil
	}
	var dd struct {
		Ranges *struct {
			Comments []map[string]any `json:"comments"`
			Changes  []map[string]any `json:"changes"`
		} `json:"ranges"`
	}
	if err := json.Unmarshal(body, &dd); err != nil {
		out.Skipped = append(out.Skipped, fmt.Sprintf("ranges: doc body not JSON: %v", err))
		return out, nil
	}
	if dd.Ranges != nil {
		for _, c := range dd.Ranges.Comments {
			out.Pointers = append(out.Pointers, legacyPointerFromMap(c))
		}
		for _, c := range dd.Ranges.Changes {
			out.Changes = append(out.Changes, legacyChangeFromMap(c))
		}
	}
	// 2) TC chat threads (best-effort — independent of the docstore side).
	if s.ChatBase == "" {
		out.Skipped = append(out.Skipped, "threads: no chat base configured")
		return out, nil
	}
	threads, terr := s.loadThreads(ctx, room)
	switch {
	case terr == nil:
		out.Threads = threads
	default:
		out.Skipped = append(out.Skipped, "threads: "+terr.Error())
	}
	return out, nil
}

func (s *LegacySource) loadThreads(ctx context.Context, room string) ([]LegacyThread, error) {
	url := strings.TrimSuffix(s.ChatBase, "/") + "/project/" + room + "/threads"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	limit := io.LimitReader(res.Body, legacyLimit(s.MaxBytes))
	body, err := io.ReadAll(limit)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chat HTTP %d", res.StatusCode)
	}
	var outer map[string]struct {
		Messages []struct {
			ID        string          `json:"id"`
			Content   json.RawMessage `json:"content"`
			Timestamp int64           `json:"timestamp"`
			UserID    string          `json:"user_id"`
			EditedAt  *int64          `json:"edited_at,omitempty"`
		} `json:"messages"`
		Resolved      *bool   `json:"resolved,omitempty"`
		ResolvedAt    *string `json:"resolved_at,omitempty"`
		ResolvedByHex *string `json:"resolved_by_user_id,omitempty"`
	}
	if err := json.Unmarshal(body, &outer); err != nil {
		return nil, fmt.Errorf("chat body not a threadId-keyed record: %v", err)
	}
	keys := make([]string, 0, len(outer))
	for k := range outer {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic corpus order (the record is unsorted)
	out := make([]LegacyThread, 0, len(outer))
	for _, k := range keys {
		t := outer[k]
		lt := LegacyThread{ID: k} // the record KEY is the thread id (op.t links here)
		if t.Resolved != nil && *t.Resolved {
			lt.Resolved = true
		}
		if t.ResolvedAt != nil {
			if ms, perr := parseTCMS(*t.ResolvedAt); perr == nil {
				lt.ResolvedAt = ms
			}
		}
		if t.ResolvedByHex != nil {
			lt.ResolvedBy = *t.ResolvedByHex
		}
		for _, m := range t.Messages {
			lt.Messages = append(lt.Messages, LegacyMessage{
				ID:        m.ID,
				Content:   legacyContentText(m.Content),
				Timestamp: m.Timestamp,
				UserID:    m.UserID,
				EditedAt:  m.EditedAt,
			})
		}
		out = append(out, lt)
	}
	return out, nil
}

// parseTCMS — Node resolved_at is an ISO string or epoch; accept both.
func parseTCMS(s string) (int64, error) {
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		return ms, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UnixMilli(), nil
	}
	return 0, fmt.Errorf("unparseable resolved_at %q", s)
}

// legacyContentText — TC content is a JSON raw (string or rich). Strings
// unwrap; anything else serializes (content preserved, not lost).
func legacyContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	b, _ := json.Marshal(raw)
	return string(b)
}

func legacyPointerFromMap(m map[string]any) LegacyPointer {
	lp := LegacyPointer{ID: anyString(m["id"])}
	if op, ok := m["op"].(map[string]any); ok {
		lp.ThreadID = anyString(op["t"])
		lp.Start = int(anyInt(op["p"]))
	}
	lp.State = anyString(m["state"])
	if md, ok := m["metadata"].(map[string]any); ok {
		lp.Metadata = md
	}
	return lp
}

func legacyChangeFromMap(m map[string]any) LegacyChangeOp {
	lc := LegacyChangeOp{ID: anyString(m["id"])}
	if op, ok := m["op"].(map[string]any); ok {
		lc.InsText = anyString(op["i"])
		lc.DelText = anyString(op["d"])
		lc.Start = int(anyInt(op["p"]))
	}
	lc.State = anyString(m["state"])
	if md, ok := m["metadata"].(map[string]any); ok {
		lc.Metadata = md
	}
	return lc
}

func anyString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func anyInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

// ---------------------------------------------------------------------------
// Materialization — legacy corpus → P1–P3 review records (pure mapping).

// MaterializeLegacy — maps the legacy corpus to the Y.Doc review domain
// (file = the P1 content doc the room seeded from). Documented semantics
// (package comment): point ranges for pointer comments, content-preserving
// insert/delete records, state passthrough with pending default.
func MaterializeLegacy(d LegacyData, file string) (threads []Thread, comments []Comment, changes []TrackedChange) {
	pointersByThread := map[string][]LegacyPointer{}
	for _, p := range d.Pointers {
		if p.ThreadID != "" {
			pointersByThread[p.ThreadID] = append(pointersByThread[p.ThreadID], p)
		}
	}
	for _, t := range d.Threads {
		th := Thread{
			ID:     t.ID,
			File:   file,
			State:  ThreadStateOpened,
			Author: legacyAuthor(firstMessageUser(t)),
		}
		if t.Resolved {
			th.State = ThreadStateResolved
			th.Resolved = t.ResolvedAt
			if t.ResolvedBy != "" {
				th.ResolvedBy = legacyAuthor(t.ResolvedBy)
			}
		}
		// Created = first message ts (authoritative legacy order).
		if len(t.Messages) > 0 {
			th.Created = t.Messages[0].Timestamp
		}
		threads = append(threads, th)
		for _, m := range t.Messages {
			cm := Comment{
				ID:       m.ID,
				ThreadID: t.ID,
				File:     file,
				Text:     m.Content,
				Author:   legacyAuthor(m.UserID),
				Created:  m.Timestamp,
			}
			if m.EditedAt != nil {
				cm.Edited = *m.EditedAt
			}
			// Ranges: the legacy POINTERS for this thread (op.t == t.ID).
			// Zero-length {start:p,end:p} — the legacy pointer shape carries
			// exactly one coordinate; do not invent an end.
			for _, p := range pointersByThread[t.ID] {
				cm.Ranges = append(cm.Ranges, map[string]any{"start": p.Start, "end": p.Start})
			}
			comments = append(comments, cm)
		}
	}
	for _, c := range d.Changes {
		ch := TrackedChange{
			ID:      c.ID,
			File:    file,
			Start:   c.Start,
			Author:  legacyMetaAuthor(c.Metadata),
			Created: legacyMetaTS(c.Metadata),
			State:   legacyChangeState(c.State),
		}
		switch {
		case c.InsText != "":
			ch.Kind = ChangeKindInsert
			ch.End = c.Start + len(c.InsText)
			ch.Content = c.InsText
		case c.DelText != "":
			ch.Kind = ChangeKindDelete
			ch.End = c.Start + len(c.DelText)
			ch.Content = c.DelText
		default:
			continue // no text — nothing faithful to materialize
		}
		changes = append(changes, ch)
	}
	return threads, comments, changes
}

func firstMessageUser(t LegacyThread) string {
	if len(t.Messages) > 0 {
		return t.Messages[0].UserID
	}
	return ""
}

func legacyAuthor(uid string) map[string]any {
	if uid == "" {
		return nil
	}
	return map[string]any{"user_id": uid}
}

func legacyMetaAuthor(md map[string]any) map[string]any {
	if md == nil {
		return nil
	}
	uid := anyString(md["user_id"])
	if uid == "" {
		return nil
	}
	out := map[string]any{"user_id": uid}
	if name := anyString(md["name"]); name != "" {
		out["name"] = name
	}
	if email := anyString(md["email"]); email != "" {
		out["email"] = email
	}
	return out
}

func legacyMetaTS(md map[string]any) int64 {
	if md == nil {
		return 0
	}
	for _, k := range []string{"ts", "timestamp", "created"} {
		if v, ok := md[k]; ok {
			switch n := v.(type) {
			case int64:
				return n
			case int:
				return int64(n)
			case float64:
				return int64(n)
			case string:
				if ms, err := strconv.ParseInt(n, 10, 64); err == nil {
					return ms
				}
			}
		}
	}
	return 0
}

func legacyChangeState(s string) string {
	switch s {
	case ChangeStatePending, ChangeStateAccepted, ChangeStateRejected:
		return s
	default:
		return ChangeStatePending // documented default (unknown legacy state)
	}
}

// ---------------------------------------------------------------------------
// Backfill — write the materialized records (best-effort, idempotent).

// BackfillStats — what the backfill did (logged by callers; idempotency is
// intrinsic — the Add* writers are per-id idempotent).
type BackfillStats struct {
	Threads  int
	Comments int
	Changes  int
	Errors   []string
}

func (s *BackfillStats) String() string {
	return fmt.Sprintf("backfill: %d threads, %d comments, %d changes, %d errors%s",
		s.Threads, s.Comments, s.Changes, len(s.Errors),
		joinErrs(s.Errors))
}

func joinErrs(errs []string) string {
	if len(errs) == 0 {
		return ""
	}
	if len(errs) > 4 {
		errs = errs[:4]
	}
	return " [" + strings.Join(errs, "; ") + "]"
}

// Backfill — writes the legacy corpus into the room's review records.
// Order: threads → their comments → changes (the P1–P3 writers; all
// idempotent per id, so a re-run is a no-op). Per-record failures are
// collected (best-effort) — one bad record never blocks the rest; a seed
// that succeeded is never un-seeded on account of legacy data.
func Backfill(ctx context.Context, store persistence.VersionedPersistence, room string, d LegacyData, file string) BackfillStats {
	var st BackfillStats
	threads, comments, changes := MaterializeLegacy(d, file)
	for _, t := range threads {
		if _, _, _, err := AddThread(ctx, store, room, t); err != nil {
			st.Errors = append(st.Errors, fmt.Sprintf("thread %s: %v", t.ID, err))
			continue
		}
		st.Threads++
	}
	for _, c := range comments {
		if _, _, _, err := AddComment(ctx, store, room, c); err != nil {
			st.Errors = append(st.Errors, fmt.Sprintf("comment %s (thread %s): %v", c.ID, c.ThreadID, err))
			continue
		}
		st.Comments++
	}
	for _, c := range changes {
		if _, _, _, err := AddChange(ctx, store, room, c); err != nil {
			st.Errors = append(st.Errors, fmt.Sprintf("change %s: %v", c.ID, err))
			continue
		}
		st.Changes++
	}
	return st
}
