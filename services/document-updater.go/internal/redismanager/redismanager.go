// Package redismanager — 1:1 port of `app/js/RedisManager.js` (689 LOC),
// oracle-gated by `test/unit/js/RedisManager/RedisManagerTests.js` (8
// groups: getDoc, getPreviousDocOps, updateDocument, putDocInMemory,
// removeDocFromMemory, clearProjectState, renameDoc, getDocVersion).
//
// The RedisManager is the document-updater's Redis facade: doc snapshots
// (lines / version / hash / ranges / pathname / projectHistoryId /
// resolvedCommentIds), the per-doc op log (the DOC_OPS list with TTL +
// 100-ops tail), per-project keys (DocsIn, ProjectState, ProjectBlock),
// the HistoryRangesSupport + ResolvedCommentIds sets, the flushAndDelete
// zset queue, and the one-shot project-notification keys.
//
// Hermetic seam (mirrors the oracle's rclient/multi stubs): all redis
// traffic goes through the injected Client (direct commands) and Tx (a
// redis MULTI over hash-braced keys — commands queued in call order,
// replies surfaced by Exec). Tests provide a recording in-memory fake
// (package redismem); a real redis client can be wired behind the same
// seam later.
//
// Go divergences (documented; oracle byte pins still hold):
//   - vendor `JSON.stringify` is JSONString (a no-HTML-escape encoding/
//     json encoder — Node's stringify emits raw UTF-8, Go's default
//     encoder HTML-escapes; the oracle's doc lines
//     `["one","two","three","これは"]` and sha1
//     `8b69713c1e40897e9220cec1d5183ea1a7396672` are pinned
//     byte-for-byte);
//   - vendor null-byte sentinels run on the raw wire string: JSONString
//     itself escapes NUL, so the sentinels are exercised via the injected
//     DocStringify seam (the oracle stubs JSON.stringify directly);
//   - vendor `parseInt` of a missing value (JS NaN) is the Go zero value
//     0 — the "inconsistent version or lengths" NaN branch is
//     unreachable in Go and is not modelled;
//   - vendor `metrics.Timer` bail-outs (slow Redis while holding the 30s
//     lock) use the Now int64-millis seam (tests inject a slow wall);
//   - vendor metrics/logger side effects (the hash-mismatch error log,
//     the redis.docLines byte summaries, the pathname counters) are the
//     nil-tolerant LogHashRead / Summary / Inc seams;
//   - vendor `Settings.max_doc_length` defaults to 0 (no limit here);
//     the size check runs through vendored limits.DocIsTooLarge;
//   - vendor `Settings.smoothingOffset` jitter is the SmoothingOffset
//     int seam (0 here — deterministic);
//   - `GetDoc` / `GetDocVersion` dispatch to nil-defaulting seam fields
//     (the oracle stubs promises.getDoc / promises.getDocVersion);
//   - vendor OError.tag (the MULTI exec prior-error chain) is not
//     modelled (the fake driver never errors per-command);
//   - vendor `ONE_DAY_SECS` is exported as OneDaySecs (unused outside the
//     flush-queue window per vendor).
package redismanager

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"ollitex/go/libraries/oerror"

	"document-updater/internal/errorsx"
	"document-updater/internal/limits"
)

// Vendor tuning constants (module-level in the vendor file).
const (
	// DocOpsTTL mirrors DOC_OPS_TTL (60 * minutes).
	DocOpsTTL = 60 * 60
	// DocOpsMaxLength mirrors DOC_OPS_MAX_LENGTH.
	DocOpsMaxLength = 100
	// MaxRedisRequestLength mirrors MAX_REDIS_REQUEST_LENGTH (5s) — the
	// bail-out on a slow Redis while holding the 30s project lock.
	MaxRedisRequestLength int64 = 5000
	// projectBlockTTLSecs mirrors PROJECT_BLOCK_TTL_SECS.
	projectBlockTTLSecs = 30
	// MaxRangesSize mirrors MAX_RANGES_SIZE (3 MiB serialized).
	MaxRangesSize = 3 * 1024 * 1024
	// projectNotificationTTL mirrors the inline 1h one-shot marker TTL.
	projectNotificationTTL = 3600
	// OneDaySecs mirrors ONE_DAY_SECS (the flush-queue scan window bound).
	OneDaySecs = 24 * 60 * 60
)

// KeySchema mirrors Settings.redis.documentupdater.key_schema (the 17
// oracle-pinned factories + the 3 committed vendor templates).
type KeySchema struct {
	DocLines                     func(docID string) string
	DocOps                       func(docID string) string
	DocVersion                   func(docID string) string
	DocHash                      func(docID string) string
	ProjectKey                   func(docID string) string
	DocsInProject                func(projectID string) string
	Range                        func(docID string) string
	Pathname                     func(docID string) string
	ProjectHistoryID             func(docID string) string
	ProjectState                 func(projectID string) string
	ProjectBlock                 func(projectID string) string
	UnflushedTime                func(docID string) string
	LastUpdatedBy                func(docID string) string
	LastUpdatedAt                func(docID string) string
	HistoryRangesSupport         func() string
	ResolvedCommentIDs           func(docID string) string
	FlushAndDeleteQueue          func() string
	ProjectNotificationTimestamp func(projectID string) string
	ProjectNotificationEditor    func(projectID string) string
}

// DefaultKeySchema mirrors the oracle's 17-key fake + the committed
// vendor templates for the 3 post-history keys.
func DefaultKeySchema() KeySchema {
	return KeySchema{
		DocLines:             func(id string) string { return "doclines:" + id },
		DocOps:               func(id string) string { return "DocOps:" + id },
		DocVersion:           func(id string) string { return "DocVersion:" + id },
		DocHash:              func(id string) string { return "DocHash:" + id },
		ProjectKey:           func(id string) string { return "ProjectId:" + id },
		DocsInProject:        func(id string) string { return "DocsIn:" + id },
		Range:                func(id string) string { return "Ranges:" + id },
		Pathname:             func(id string) string { return "Pathname:" + id },
		ProjectHistoryID:     func(id string) string { return "ProjectHistoryId:" + id },
		ProjectState:         func(id string) string { return "ProjectState:" + id },
		ProjectBlock:         func(id string) string { return "ProjectBlock:" + id },
		UnflushedTime:        func(id string) string { return "UnflushedTime:" + id },
		LastUpdatedBy:        func(id string) string { return "lastUpdatedBy:" + id },
		LastUpdatedAt:        func(id string) string { return "lastUpdatedAt:" + id },
		HistoryRangesSupport: func() string { return "HistoryRangesSupport" },
		ResolvedCommentIDs:   func(id string) string { return "ResolvedCommentIds:" + id },
		FlushAndDeleteQueue:  func() string { return "FlushAndDeleteQueue" },
		ProjectNotificationTimestamp: func(id string) string {
			return "ProjectNotificationTimestamp:" + id
		},
		ProjectNotificationEditor: func(id string) string {
			return "ProjectNotificationEditor:" + id
		},
	}
}

// Ranges mirrors the doc ranges object (the ranges-manager wire).
type Ranges = map[string]any

// Doc mirrors the vendor getDoc multiResult shape (the 10 exported
// fields; absent redis values are nil-typed Go values).
type Doc struct {
	Lines                []string
	Version              int
	Ranges               Ranges
	Pathname             *string
	ProjectHistoryID     *string
	UnflushedTime        *string
	LastUpdatedAt        *string
	LastUpdatedBy        *string
	HistoryRangesSupport bool
	ResolvedCommentIDs   []string
}

// FlushNextProject mirrors the vendor getNextProjectToFlushAndDelete
// multiResult shape (projectId, flushTimestamp, queueLength).
type FlushNextProject struct {
	ProjectID      string
	FlushTimestamp string
	QueueLength    int
}

// Tx mirrors `rclient.multi()` — a redis MULTI: commands queue in call
// order, Exec surfaces their replies.
type Tx interface {
	Exists(key string) Tx
	SAdd(key string, members ...string) Tx
	MSet(values map[string]any) Tx
	Del(keys ...string) Tx
	Set(key string, value any, opts ...any) Tx
	RPush(key string, values ...string) Tx
	Expire(key string, seconds int) Tx
	LTrim(key string, start, stop int) Tx
	GetSet(key string, value string) Tx
	ZAdd(key string, score float64, member string) Tx
	ZRange(key string, start, stop int) Tx
	ZRemRangeByRank(key string, start, stop int) Tx
	ZCard(key string) Tx
	SRem(key string, members ...string) Tx
	StrLen(key string) Tx
	SetEx(key string, seconds int, value string) Tx
	SCard(key string) Tx
	Exec() []any
}

// Client mirrors the vendor rclient (the redis-wrapper client): the
// direct commands plus a MULTI factory.
type Client interface {
	MGet(keys ...string) ([]any, error)
	Get(key string) (string, error)
	Set(key string, value any, opts ...any) error
	Del(key string) (int, error)
	SAdd(key string, members ...string) error
	SRem(key string, members ...string) error
	SIsMember(key, member string) (int, error)
	SMembers(key string) ([]string, error)
	LLen(key string) (int, error)
	LRange(key string, start, stop int) ([]string, error)
	RPush(key string, values ...string) (int, error)
	ZAdd(key string, score float64, member string) error
	ZRangeByScore(key string, min, max any) ([]any, error)
	MultiFunc() Tx
}

// Manager mirrors the RedisManager module object (the vendor's
// RedisManager.promises) minus the redis-wrapper plumbing. All redis
// traffic goes through C; the stubbable seams mirror the oracle's
// promises.getDoc / promises.getDocVersion stubs and the vendored
// logger/metrics timers.
type Manager struct {
	Keys KeySchema
	C    Client
	// Now is the vendor metrics.Timer wall (ms since unix epoch).
	Now func() int64
	// Inc mirrors vendored metrics.inc.
	Inc func(name string, n float64, labels map[string]any)
	// Summary mirrors vendored metrics.summary (the redis.docLines byte
	// counters: set/update/del).
	Summary func(metric string, n int, labels map[string]any)
	// LogHashRead mirrors the vendor logger.error on hash mismatch.
	LogHashRead func(projectID, docID, docProjectID, computed, stored string)
	// DocStringify mirrors vendor raw JSON.stringify (the null-sentinel
	// seam); nil uses JSONString.
	DocStringify func(lines []string) string
	// GetDocSeam mirrors promises.getDoc (the oracle stubs it); nil uses
	// the real redis implementation.
	GetDocSeam func(projectID, docID string) (*Doc, error)
	// DocVersionSeam mirrors promises.getDocVersion (the oracle stubs
	// it); nil uses the real redis implementation.
	DocVersionSeam func(docID string) (int, error)
	// SerializeRanges mirrors promises._serializeRanges (the oracle
	// stubs it); nil uses the real implementation.
	SerializeRanges func(ranges Ranges) (any, error)
	// MaxDocLength mirrors Settings.max_doc_length (0 = no limit).
	MaxDocLength int
	// SmoothingOffset mirrors Settings.smoothingOffset (ms, 0 = none).
	SmoothingOffset int
}

// New wires the vendor production defaults (real getDoc / getDocVersion /
// serializeRanges, zero jitter, nil metric sinks).
func New(c Client) *Manager {
	return &Manager{
		Keys:    DefaultKeySchema(),
		C:       c,
		Now:     func() int64 { return time.Now().UnixMilli() },
		Inc:     func(name string, n float64, labels map[string]any) {},
		Summary: func(metric string, n int, labels map[string]any) {},
	}
}

// JSONString mirrors Node JSON.stringify (no-HTML-escape UTF-8 byte
// stream). The oracle's doc lines are
// `["one","two","three","これは"]` — Go's default encoder HTML-escapes
// none of these, so encoding/json reproduces the byte stream and, with
// it, the vendor sha1.
func JSONString(v any) string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		// The encoder errors only on non-JSON values (chan/func); the
		// vendor JSON.stringify would throw too — mirror as a panic.
		panic("redismanager: JSONString: " + err.Error())
	}
	// encoding/json Encode appends a trailing newline; the vendor
	// JSON.stringify does not.
	return strings.TrimSuffix(buf.String(), "\n")
}

// ComputeHash mirrors the static RedisManager._computeHash(docLines) —
// sha1 of the UTF-8 wire string.
func ComputeHash(docLines string) string {
	sum := sha1.Sum([]byte(docLines))
	return hex.EncodeToString(sum[:])
}

// SerializeRanges mirrors RedisManager._serializeRanges(ranges): JSON
// wire string, `'{}'` → nil (empty ranges don't fill redis), and the 3 MiB
// size guard.
func SerializeRanges(ranges Ranges) (any, error) {
	jsonRanges := JSONString(ranges)
	if len(jsonRanges) > MaxRangesSize {
		return nil, fmt.Errorf("ranges are too large")
	}
	if jsonRanges == "{}" {
		return nil, nil
	}
	return jsonRanges, nil
}

// DeserializeRanges mirrors RedisManager._deserializeRanges(ranges).
func DeserializeRanges(ranges string) (Ranges, error) {
	if ranges == "" {
		return Ranges{}, nil
	}
	var out Ranges
	if err := json.Unmarshal([]byte(ranges), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (m *Manager) nowMs() int64 {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now().UnixMilli()
}

func (m *Manager) docStringify(lines []string) string {
	if m.DocStringify != nil {
		return m.DocStringify(lines)
	}
	return JSONString(lines)
}

func (m *Manager) serializeRanges(ranges Ranges) (any, error) {
	if m.SerializeRanges != nil {
		return m.SerializeRanges(ranges)
	}
	return SerializeRanges(ranges)
}

// PutDocInMemory mirrors `putDocInMemory` (the doc-snapshot write):
// serialize lines (null-sentinel + size guard), hash, the project-block
// MULTI probe (block check + DocsIn sadd BEFORE the contents write), the
// HRS flag + pathname metric, then the contents MULTI (7-key MSET + the
// HRS del/sadd).
func (m *Manager) PutDocInMemory(
	projectID, docID string,
	docLines []string,
	version int,
	ranges Ranges,
	resolvedCommentIDs []string,
	pathname string,
	projectHistoryID string,
	historyRangesSupport bool,
) error {
	wire := m.docStringify(docLines)
	if strings.ContainsRune(wire, 0) {
		// the vendor's memory-corruption sentinel.
		return oerror.New("null bytes found in doc lines",
			map[string]any{"docId": docID}).WithName("OError")
	}
	// the size check (a no-op when MaxDocLength is 0 — the 0 sentinel
	// means "unset", not "reject"; the vendor production setting is a
	// positive value).
	if m.MaxDocLength > 0 && limits.DocIsTooLarge(len(wire), docLines, m.MaxDocLength) {
		return oerror.New("blocking doc insert into redis: doc is too large",
			map[string]any{"projectId": projectID, "docId": docID, "docSize": len(wire)}).
			WithName("OError")
	}
	wireHash := ComputeHash(wire)
	m.Summary("redis.docLines", len(wire), map[string]any{"status": "set"})
	rng, err := m.serializeRanges(ranges)
	if err != nil {
		return err
	}
	// the block probe MULTI (exec BEFORE the contents write).
	probe := m.C.MultiFunc()
	probe.Exists(m.Keys.ProjectBlock(projectID))
	probe.SAdd(m.Keys.DocsInProject(projectID), docID)
	if reply := probe.Exec(); replyLen(reply) > 0 {
		if toInt(reply[0]) == 1 {
			// no contents write — the orphaned DocsIn entry is
			// intentional (vendor comment: the spurious docId is left
			// behind; a missing docId would leak the redis doc).
			return oerror.New("Project blocked from loading docs",
				map[string]any{"projectId": projectID}).WithName("OError")
		}
	}
	// the HRS flag (a direct-client call, outside the MULTI).
	if err := m.SetHistoryRangesSupportFlag(docID, historyRangesSupport); err != nil {
		return err
	}
	if pathname == "" {
		m.Inc("pathname", 1, map[string]any{
			"path":   "RedisManager.setDoc",
			"status": "zero-length",
		})
	}
	// the contents MULTI (hash-braced doc keys).
	multi := m.C.MultiFunc()
	multi.MSet(map[string]any{
		m.Keys.DocLines(docID):         wire,
		m.Keys.ProjectKey(docID):       projectID,
		m.Keys.DocVersion(docID):       version,
		m.Keys.DocHash(docID):          wireHash,
		m.Keys.Range(docID):            rng,
		m.Keys.Pathname(docID):         pathname,
		m.Keys.ProjectHistoryID(docID): projectHistoryID,
	})
	if historyRangesSupport {
		multi.Del(m.Keys.ResolvedCommentIDs(docID))
		if len(resolvedCommentIDs) > 0 {
			multi.SAdd(m.Keys.ResolvedCommentIDs(docID), resolvedCommentIDs...)
		}
	}
	multi.Exec()
	return nil
}

// SetHistoryRangesSupportFlag mirrors the vendor same-named private (the
// HRS set flag: sadd / srem into HistoryRangesSupport).
func (m *Manager) SetHistoryRangesSupportFlag(docID string, historyRangesSupport bool) error {
	if historyRangesSupport {
		return m.C.SAdd(m.Keys.HistoryRangesSupport(), docID)
	}
	return m.C.SRem(m.Keys.HistoryRangesSupport(), docID)
}

// RemoveDocFromMemory mirrors `removeDocFromMemory` (the 11-key doc
// delete MULTI, the second project MULTI, + the HRS srem).
func (m *Manager) RemoveDocFromMemory(projectID, docID string) error {
	multi := m.C.MultiFunc()
	multi.StrLen(m.Keys.DocLines(docID))
	multi.Del(
		m.Keys.DocLines(docID),
		m.Keys.ProjectKey(docID),
		m.Keys.DocVersion(docID),
		m.Keys.DocHash(docID),
		m.Keys.Range(docID),
		m.Keys.Pathname(docID),
		m.Keys.ProjectHistoryID(docID),
		m.Keys.UnflushedTime(docID),
		m.Keys.LastUpdatedAt(docID),
		m.Keys.LastUpdatedBy(docID),
		m.Keys.ResolvedCommentIDs(docID),
	)
	if reply := multi.Exec(); replyLen(reply) > 0 {
		if l := toInt(reply[0]); l > 0 {
			m.Summary("redis.docLines", l, map[string]any{"status": "del"})
		}
	}
	multi = m.C.MultiFunc()
	multi.SRem(m.Keys.DocsInProject(projectID), docID)
	multi.Del(m.Keys.ProjectState(projectID))
	multi.Exec()
	return m.C.SRem(m.Keys.HistoryRangesSupport(), docID)
}

// CheckOrSetProjectState mirrors `checkOrSetProjectState` — returns true
// iff the state was NOT already newState (the getset-and-expire probe).
func (m *Manager) CheckOrSetProjectState(projectID, newState string) (bool, error) {
	multi := m.C.MultiFunc()
	multi.GetSet(m.Keys.ProjectState(projectID), newState)
	multi.Expire(m.Keys.ProjectState(projectID), 30*60)
	reply := multi.Exec()
	if replyLen(reply) == 0 {
		return true, nil
	}
	oldState := toString(reply[0])
	return oldState != newState, nil
}

// ClearProjectState mirrors `clearProjectState`.
func (m *Manager) ClearProjectState(projectID string) error {
	_, err := m.C.Del(m.Keys.ProjectState(projectID))
	return err
}

// GetDoc mirrors `getDoc` (the 10-key MGet snapshot + the HRS/RCID set
// lookups + the hash-mismatch log + the timeout bail-out + the NotFound
// project mismatch). Dispatches to GetDocSeam when set (the oracle
// stubs promises.getDoc).
func (m *Manager) GetDoc(projectID, docID string) (*Doc, error) {
	if m.GetDocSeam != nil {
		return m.GetDocSeam(projectID, docID)
	}
	start := m.nowMs()
	vals, err := m.C.MGet(
		m.Keys.DocLines(docID),
		m.Keys.DocVersion(docID),
		m.Keys.DocHash(docID),
		m.Keys.ProjectKey(docID),
		m.Keys.Range(docID),
		m.Keys.Pathname(docID),
		m.Keys.ProjectHistoryID(docID),
		m.Keys.UnflushedTime(docID),
		m.Keys.LastUpdatedAt(docID),
		m.Keys.LastUpdatedBy(docID),
	)
	if err != nil {
		return nil, err
	}
	hrs, err := m.C.SIsMember(m.Keys.HistoryRangesSupport(), docID)
	if err != nil {
		return nil, err
	}
	rcids, err := m.C.SMembers(m.Keys.ResolvedCommentIDs(docID))
	if err != nil {
		return nil, err
	}
	if m.nowMs()-start > MaxRedisRequestLength {
		return nil, oerror.New("redis getDoc exceeded timeout",
			map[string]any{"projectId": projectID, "docId": docID}).WithName("OError")
	}
	lineWire := toString(vals[0])
	storedHash := toString(vals[2])
	docProjectID := toString(vals[3])
	if len(lineWire) > 0 && len(storedHash) > 0 {
		computed := ComputeHash(lineWire)
		if computed != storedHash && m.LogHashRead != nil {
			m.LogHashRead(projectID, docID, docProjectID, computed, storedHash)
		}
	}
	var lines []string
	if len(lineWire) > 0 {
		if err := json.Unmarshal([]byte(lineWire), &lines); err != nil {
			return nil, err
		}
	}
	rng, err := DeserializeRanges(toString(vals[4]))
	if err != nil {
		return nil, err
	}
	version := toInt(vals[1])
	if len(docProjectID) > 0 && docProjectID != projectID {
		return nil, errorsx.NotFoundE("document not found", map[string]any{
			"projectId":    projectID,
			"docId":        docID,
			"docProjectId": docProjectID,
		})
	}
	pathname := toString(vals[5])
	if len(lines) > 0 && version > 0 && pathname == "" {
		m.Inc("pathname", 1, map[string]any{
			"path":   "RedisManager.getDoc",
			"status": "zero-length",
		})
	}
	return &Doc{
		Lines:                lines,
		Version:              version,
		Ranges:               rng,
		Pathname:             ptrIf(pathname),
		ProjectHistoryID:     ptrIf(toString(vals[6])),
		UnflushedTime:        ptrIf(toString(vals[7])),
		LastUpdatedAt:        ptrIf(toString(vals[8])),
		LastUpdatedBy:        ptrIf(toString(vals[9])),
		HistoryRangesSupport: hrs == 1,
		ResolvedCommentIDs:   rcids,
	}, nil
}

// GetDocRanges mirrors `getDocRanges`.
func (m *Manager) GetDocRanges(docID string) (Ranges, error) {
	jsonRange, err := m.C.Get(m.Keys.Range(docID))
	if err != nil {
		return nil, err
	}
	return DeserializeRanges(jsonRange)
}

// GetDocVersion mirrors `getDocVersion` (the oracle-stubbable seam; nil
// DocVersionSeam uses the real redis implementation).
func (m *Manager) GetDocVersion(docID string) (int, error) {
	if m.DocVersionSeam != nil {
		return m.DocVersionSeam(docID)
	}
	vals, err := m.C.MGet(m.Keys.DocVersion(docID))
	if err != nil {
		return 0, err
	}
	if len(vals) == 0 {
		return 0, nil
	}
	return toInt(vals[0]), nil
}

// GetDocLines mirrors `getDocLines`.
func (m *Manager) GetDocLines(docID string) (string, error) {
	return m.C.Get(m.Keys.DocLines(docID))
}

// GetPreviousDocOps mirrors `getPreviousDocOps` (the op-range bounds
// check, the 0-based offset shift, the lrange fetch + JS object parse,
// + the slow-timer bail-out).
func (m *Manager) GetPreviousDocOps(docID string, start, end int) ([]any, error) {
	timerStart := m.nowMs()
	length, err := m.C.LLen(m.Keys.DocOps(docID))
	if err != nil {
		return nil, err
	}
	versionStr, err := m.C.Get(m.Keys.DocVersion(docID))
	if err != nil {
		return nil, err
	}
	version := toInt(versionStr)
	firstVersionInRedis := version - length
	if start < firstVersionInRedis || end > version {
		return nil, errorsx.OpRangeNotAvailableE("doc ops range is not loaded in redis",
			map[string]any{
				"firstVersionInRedis": firstVersionInRedis,
				"version":             version,
				"ttlInS":              DocOpsTTL,
			})
	}
	start -= firstVersionInRedis
	if end > -1 {
		end -= firstVersionInRedis
	}
	jsonOps, err := m.C.LRange(m.Keys.DocOps(docID), start, end)
	if err != nil {
		return nil, err
	}
	ops := make([]any, 0, len(jsonOps))
	for _, jsonOp := range jsonOps {
		var op any
		if err := json.Unmarshal([]byte(jsonOp), &op); err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	if m.nowMs()-timerStart > MaxRedisRequestLength {
		return nil, fmt.Errorf("redis getPreviousDocOps exceeded timeout")
	}
	return ops, nil
}

// UpdateDocument mirrors `updateDocument` (the version-consistency
// check, the wire serialize + null-sentinels + size check, the 6-key
// MSET + LTrim + the ops RPush / TTL + the NX unflushedTime).
func (m *Manager) UpdateDocument(
	projectID, docID string,
	docLines []string,
	newVersion int,
	appliedOps []any,
	ranges Ranges,
	updateMeta map[string]any,
) error {
	if len(appliedOps) == 0 {
		appliedOps = []any{}
	}
	currentVersion, err := m.GetDocVersion(docID)
	if err != nil {
		return err
	}
	if currentVersion+len(appliedOps) != newVersion {
		return oerror.New("Version mismatch. doc is corrupted", map[string]any{
			"docId":          docID,
			"currentVersion": currentVersion,
			"newVersion":     newVersion,
			"opsLength":      len(appliedOps),
		}).WithName("OError")
	}
	jsonOps := make([]string, 0, len(appliedOps))
	for _, op := range appliedOps {
		jsonOp := JSONString(op)
		jsonOps = append(jsonOps, jsonOp)
		if strings.ContainsRune(jsonOp, 0) {
			return oerror.New("null bytes found in jsonOps", map[string]any{
				"docId":   docID,
				"jsonOps": jsonOps,
			}).WithName("OError")
		}
	}
	newDocLines := m.docStringify(docLines)
	if strings.ContainsRune(newDocLines, 0) {
		return oerror.New("null bytes found in doc lines", map[string]any{
			"docId":       docID,
			"newDocLines": newDocLines,
		}).WithName("OError")
	}
	if m.MaxDocLength > 0 && limits.DocIsTooLarge(len(newDocLines), docLines, m.MaxDocLength) {
		return oerror.New("blocking doc update: doc is too large", map[string]any{
			"projectId": projectID,
			"docId":     docID,
			"docSize":   len(newDocLines),
		}).WithName("OError")
	}
	newHash := ComputeHash(newDocLines)
	m.Summary("redis.docLines", len(newDocLines), map[string]any{"status": "update"})
	jsonRanges, err := m.serializeRanges(ranges)
	if err != nil {
		return err
	}
	if v := toString(jsonRanges); v != "" && strings.ContainsRune(v, 0) {
		return oerror.New("null bytes found in ranges",
			map[string]any{"docId": docID}).WithName("OError")
	}
	// hash-braced doc keys MULTI.
	multi := m.C.MultiFunc()
	multi.MSet(map[string]any{
		m.Keys.DocLines(docID):      newDocLines,
		m.Keys.DocVersion(docID):    newVersion,
		m.Keys.DocHash(docID):       newHash,
		m.Keys.Range(docID):         jsonRanges,
		m.Keys.LastUpdatedAt(docID): m.nowMs(),
		m.Keys.LastUpdatedBy(docID): metaUserID(updateMeta),
	})
	multi.LTrim(m.Keys.DocOps(docID), -DocOpsMaxLength, -1)
	if len(jsonOps) > 0 {
		multi.RPush(m.Keys.DocOps(docID), jsonOps...)
		multi.Expire(m.Keys.DocOps(docID), DocOpsTTL)
	}
	multi.Set(m.Keys.UnflushedTime(docID), m.nowMs(), "NX")
	multi.Exec()
	return nil
}

// RenameDoc mirrors `renameDoc` (the cached-pathname write).
func (m *Manager) RenameDoc(
	projectID, docID string,
	userID string,
	update map[string]any,
	projectHistoryID string,
) error {
	doc, err := m.GetDoc(projectID, docID)
	if err != nil {
		return err
	}
	if doc.Lines == nil || doc.Version == 0 {
		return nil
	}
	newPathname, _ := update["newPathname"].(string)
	if newPathname == "" {
		m.Inc("pathname", 1, map[string]any{
			"path":   "RedisManager.renameDoc",
			"status": "zero-length",
		})
	}
	err = m.C.Set(m.Keys.Pathname(docID), newPathname)
	return err
}

// ClearUnflushedTime mirrors `clearUnflushedTime`.
func (m *Manager) ClearUnflushedTime(docID string) error {
	_, err := m.C.Del(m.Keys.UnflushedTime(docID))
	return err
}

// UpdateCommentState mirrors `updateCommentState` (the ResolvedCommentIDs
// sadd/srem).
func (m *Manager) UpdateCommentState(docID, commentID string, resolved bool) error {
	if resolved {
		return m.C.SAdd(m.Keys.ResolvedCommentIDs(docID), commentID)
	}
	return m.C.SRem(m.Keys.ResolvedCommentIDs(docID), commentID)
}

// GetDocIDsInProject mirrors `getDocIdsInProject`.
func (m *Manager) GetDocIDsInProject(projectID string) ([]string, error) {
	return m.C.SMembers(m.Keys.DocsInProject(projectID))
}

// GetDocTimestamps mirrors `getDocTimestamps`.
func (m *Manager) GetDocTimestamps(docIDs []string) ([]string, error) {
	timestamps := make([]string, 0, len(docIDs))
	for _, docID := range docIDs {
		ts, err := m.C.Get(m.Keys.LastUpdatedAt(docID))
		if err != nil {
			return nil, err
		}
		timestamps = append(timestamps, ts)
	}
	return timestamps, nil
}

// QueueFlushAndDeleteProject mirrors `queueFlushAndDeleteProject` (the
// zset time-ordering + the random-offset jitter smoothing).
func (m *Manager) QueueFlushAndDeleteProject(projectID string) error {
	smoothing := 0
	if m.SmoothingOffset > 0 {
		smoothing = m.SmoothingOffset // Math.round(offset * Math.random())
	}
	return m.C.ZAdd(m.Keys.FlushAndDeleteQueue(),
		float64(int(m.nowMs())+smoothing), projectID)
}

// GetNextProjectToFlushAndDelete mirrors `getNextProjectToFlushAndDelete`
// (the oldest-ready pop: zrangebyscore probe + the zrange/zrem/zcard
// MULTI "poor man's ZPOPMIN").
func (m *Manager) GetNextProjectToFlushAndDelete(cutoffTime int64) (FlushNextProject, error) {
	projectsReady, err := m.C.ZRangeByScore(
		m.Keys.FlushAndDeleteQueue(),
		0, cutoffTime,
	)
	if err != nil {
		return FlushNextProject{}, err
	}
	if replyLen(projectsReady) == 0 {
		return FlushNextProject{}, nil
	}
	// pop the oldest entry (get + remove in a MULTI). The zrange is
	// WITHSCORES so the (key, score) pair is available for the
	// flushTimestamp field.
	multi := m.C.MultiFunc()
	multi.ZRange(m.Keys.FlushAndDeleteQueue(), 0, 0)
	multi.ZRemRangeByRank(m.Keys.FlushAndDeleteQueue(), 0, 0)
	multi.ZCard(m.Keys.FlushAndDeleteQueue())
	reply := multi.Exec()
	if replyLen(reply) == 0 {
		return FlushNextProject{}, nil
	}
	// reply: [[key, timestamp], nil, queueLength]
	var projectID, flushTS string
	if p, ok := reply[0].([]any); ok && len(p) >= 2 {
		projectID = toString(p[0])
		flushTS = toString(p[1])
	}
	return FlushNextProject{
		ProjectID:      projectID,
		FlushTimestamp: flushTS,
		QueueLength:    toInt(reply[2]),
	}, nil
}

// BlockProject mirrors `blockProject` (the projectBlock setex(30s,'1')
// probe MULTI + the orphaned-queue del on contention).
func (m *Manager) BlockProject(projectID string) (bool, error) {
	multi := m.C.MultiFunc()
	multi.SetEx(m.Keys.ProjectBlock(projectID), projectBlockTTLSecs, "1")
	multi.SCard(m.Keys.DocsInProject(projectID))
	reply := multi.Exec()
	if replyLen(reply) == 0 {
		return false, nil
	}
	if toInt(reply[1]) > 0 {
		// Too late to lock the project — release the block key.
		_, err := m.C.Del(m.Keys.ProjectBlock(projectID))
		return false, err
	}
	return true, nil
}

// UnblockProject mirrors `unblockProject`.
func (m *Manager) UnblockProject(projectID string) (bool, error) {
	n, err := m.C.Del(m.Keys.ProjectBlock(projectID))
	return n == 1, err
}

// RecordProjectNotificationTimestamp mirrors the vendor same-named
// one-shot batch marker (the timestamp NX + the editor-id NX, 1h TTL).
func (m *Manager) RecordProjectNotificationTimestamp(projectID string, timestamp, userID string) error {
	if err := m.C.Set(m.Keys.ProjectNotificationTimestamp(projectID), timestamp, "NX", "EX", projectNotificationTTL); err != nil {
		return err
	}
	if userID != "" {
		if err := m.C.Set(m.Keys.ProjectNotificationEditor(projectID), userID, "NX", "EX", projectNotificationTTL); err != nil {
			return err
		}
	}
	return nil
}

// --- wire coercions (the redis-wrapper Coercions the vendor relies on). ---

func toString(v any) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	case int:
		return strconv.Itoa(s)
	case int64:
		return strconv.FormatInt(s, 10)
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}

func ptrIf(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func metaUserID(meta map[string]any) any {
	if meta == nil {
		return nil
	}
	u, ok := meta["user_id"]
	if !ok {
		return nil
	}
	return u
}

func replyLen(reply []any) int { return len(reply) }
