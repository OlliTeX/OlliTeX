// Package redismanager mirrors app/js/RedisManager.js.
package redismanager

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"ollitex/go/services/project-history/internal/config"
	"ollitex/go/services/project-history/internal/redisx"
)

// Constants from RedisManager.js (module-top exports).
const (
	RAWUpdateSizeThreshold = 4 * 1024 * 1024
	RawUpdatesBatchSize    = 50
	MaxUpdateOpLength      = 1024
	MaxNewDocContentCount  = 32
	warnRawUpdateSize      = 1024 * 1024
	_                      = warnRawUpdateSize
	CacheTTLSeconds        = 3600
)

// Keys abstracts the redis key schema so tests can substitute a fake.
type Keys interface {
	ProjectHistoryOps(projectID string) string
	ProjectHistoryFirstOpTimestamp(projectID string) string
	ProjectHistoryCachedHistoryID(projectID string) string
	ProjectHistoryLock(projectID string) string
}

// Compile-time check the key schema satisfies the interface.
var _ Keys = config.KeySchema{}

type RedisManager struct {
	Rd   redisx.Client
	Keys Keys
}

func New(rd redisx.Client, keys Keys) *RedisManager {
	return &RedisManager{Rd: rd, Keys: keys}
}

func (m *RedisManager) opsKey(projectID string) string { return m.Keys.ProjectHistoryOps(projectID) }
func (m *RedisManager) firstOpKey(projectID string) string {
	return m.Keys.ProjectHistoryFirstOpTimestamp(projectID)
}
func (m *RedisManager) cachedHistoryIDKey(projectID string) string {
	return m.Keys.ProjectHistoryCachedHistoryID(projectID)
}

func (m *RedisManager) CountUnprocessedUpdates(projectID string) (int, error) {
	n, err := m.Rd.LLen(m.opsKey(projectID))
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// RawUpdatesBatch mirrors getRawUpdatesBatch: { rawUpdates []string, hasMore bool }.
type RawUpdatesBatch struct {
	RAWUpdates []string
	HasMore    bool
}

// GetRawUpdatesBatch mirrors the JS getRawUpdatesBatch: page the ops list
// 50 at a time, accumulate size as we go, stop once batchSize is reached or
// the 4 MiB raw size threshold is crossed; hasMore=true on either stop.
func (m *RedisManager) GetRawUpdatesBatch(projectID string, batchSize int) (*RawUpdatesBatch, error) {
	out := &RawUpdatesBatch{RAWUpdates: []string{}}
	key := m.opsKey(projectID)
	totalSize := 0
	for i := 0; ; i += RawUpdatesBatchSize {
		page, err := m.Rd.LRange(key, i, i+RawUpdatesBatchSize-1)
		if err != nil {
			return nil, err
		}
		for _, u := range page {
			// JS: threshold is checked on the NEXT item after accumulating this
			// one; the first item always goes in even if it alone is huge.
			if len(out.RAWUpdates) > 0 && totalSize+len(u) > RAWUpdateSizeThreshold {
				out.HasMore = true
				return out, nil
			}
			totalSize += len(u)
			out.RAWUpdates = append(out.RAWUpdates, u)
			if len(out.RAWUpdates) >= batchSize {
				out.HasMore = true
				return out, nil
			}
		}
		if len(page) < RawUpdatesBatchSize {
			// stream exhausted
			return out, nil
		}
	}
}

// ParseDocUpdates = jsonUpdates.map(u => JSON.parse(u)).
func ParseDocUpdates(jsonUpdates []string) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(jsonUpdates))
	for _, u := range jsonUpdates {
		var m map[string]any
		if err := json.Unmarshal([]byte(u), &m); err != nil {
			return nil, fmt.Errorf("failed to parse update: %w", err)
		}
		out = append(out, m)
	}
	return out, nil
}

func opLength(update map[string]any) int {
	ops, _ := update["op"].([]any)
	if len(ops) == 0 {
		return 1 // op?.length || 1
	}
	return len(ops)
}

// jsTruthy mirrors the vendor's `if (update.resyncDocContent)` — plain JS
// truthiness (any non-null value is truthy; JSON-decoded numbers/strings
// included), NOT a Go bool assertion.
func jsTruthy(v any) bool { return v != nil }

// GetUpdatesInBatches mirrors getUpdatesInBatches: loop batches of batchSize,
// feed the runner parsed updates, LREM applied raws + DEL first-op-timestamp.
func (m *RedisManager) GetUpdatesInBatches(projectID string, batchSize int, runner func([]map[string]any) error) error {
	moreBatches := true
	for moreBatches {
		batch, err := m.GetRawUpdatesBatch(projectID, batchSize)
		if err != nil {
			return err
		}
		if len(batch.RAWUpdates) == 0 {
			break
		}
		moreBatches = batch.HasMore

		rawUpdates := []string{}
		var updates []map[string]any
		totalOpLength := 0
		totalDocContentCount := 0
		for _, raw := range batch.RAWUpdates {
			var upd map[string]any
			if err := json.Unmarshal([]byte(raw), &upd); err != nil {
				return fmt.Errorf("failed to parse update: %w", err)
			}
			totalOpLength += opLength(upd)
			if jsTruthy(upd["resyncDocContent"]) {
				totalDocContentCount += 1
			}
			if len(updates) > 0 && (totalOpLength > MaxUpdateOpLength || totalDocContentCount > MaxNewDocContentCount) {
				moreBatches = true
				break
			}
			if jsTruthy(upd["resyncProjectStructureOnly"]) {
				upd["_raw"] = raw
			}
			rawUpdates = append(rawUpdates, raw)
			updates = append(updates, upd)
		}
		if len(updates) > 0 && runner != nil {
			if err := runner(updates); err != nil {
				return err
			}
		}
		if err := m.DeleteAppliedDocUpdates(projectID, rawUpdates); err != nil {
			return err
		}
		if batchSize == 1 {
			break
		}
	}
	return nil
}

// DeleteAppliedDocUpdates: LREM each raw from the ops queue (first match) and
// DEL the first-op-timestamp key when any were applied.
func (m *RedisManager) DeleteAppliedDocUpdates(projectID string, rawUpdates []string) error {
	for _, raw := range rawUpdates {
		if _, err := m.Rd.LRem(m.opsKey(projectID), 1, raw); err != nil {
			return err
		}
	}
	if len(rawUpdates) > 0 {
		if _, err := m.Rd.Del(m.firstOpKey(projectID)); err != nil {
			return err
		}
	}
	return nil
}

// DestroyDocUpdatesQueue: DEL ops key + first-op-timestamp key.
func (m *RedisManager) DestroyDocUpdatesQueue(projectID string) error {
	_, err := m.Rd.Del(m.opsKey(projectID), m.firstOpKey(projectID))
	return err
}

var projIDRe = regexp.MustCompile(`:\{?([0-9a-f]{24})\}?`)

// GetProjectIDsWithHistoryOps: SCAN ProjectHistory:Ops:{*}; limit caps.
func (m *RedisManager) GetProjectIDsWithHistoryOps(limit ...int) ([]string, error) {
	l := 0
	if len(limit) > 0 {
		l = limit[0]
	}
	keys, err := m.Rd.Scan(m.Keys.ProjectHistoryOps("*"), l)
	if err != nil {
		return nil, err
	}
	return extractIDs(keys), nil
}

// GetProjectIDsWithFirstOpTimestamps mirrors getProjectIdsWithFirstOpTimestamps.
func (m *RedisManager) GetProjectIDsWithFirstOpTimestamps(limit ...int) ([]string, error) {
	l := 0
	if len(limit) > 0 {
		l = limit[0]
	}
	keys, err := m.Rd.Scan(m.Keys.ProjectHistoryFirstOpTimestamp("*"), l)
	if err != nil {
		return nil, err
	}
	return extractIDs(keys), nil
}

func extractIDs(keys []string) []string {
	ids := make([]string, 0, len(keys))
	for _, k := range keys {
		if m := projIDRe.FindStringSubmatch(k); len(m) >= 2 {
			ids = append(ids, m[1])
		}
	}
	return ids
}

// SetFirstOpTimestamp: SETNX ts key with current ms epoch.
func (m *RedisManager) SetFirstOpTimestamp(projectID string) error {
	if _, err := m.Rd.SetNX(m.firstOpKey(projectID), strconv.FormatInt(time.Now().UnixMilli(), 10)); err != nil {
		return err
	}
	return nil
}

func (m *RedisManager) GetFirstOpTimestamp(projectID string) (time.Time, bool, error) {
	v, ok, err := m.Rd.Get(m.firstOpKey(projectID))
	if err != nil {
		return time.Time{}, ok, err
	}
	if !ok || v == "" {
		return time.Time{}, false, nil
	}
	ts, err := strconv.ParseInt(v, 10, 64)
	if err != nil || ts == 0 {
		return time.Time{}, false, nil
	}
	return time.UnixMilli(ts), true, nil
}

func (m *RedisManager) GetFirstOpTimestamps(projectIDs []string) ([]time.Time, error) {
	keys := make([]string, len(projectIDs))
	for i, id := range projectIDs {
		keys[i] = m.firstOpKey(id)
	}
	vals, err := m.Rd.MGet(keys...)
	if err != nil {
		return nil, err
	}
	out := make([]time.Time, 0, len(projectIDs))
	for _, v := range vals {
		if v == "" {
			out = append(out, time.Time{})
			continue
		}
		ts, err := strconv.ParseInt(v, 10, 64)
		if err != nil || ts == 0 {
			out = append(out, time.Time{})
			continue
		}
		out = append(out, time.UnixMilli(ts))
	}
	return out, nil
}

func (m *RedisManager) ClearFirstOpTimestamp(projectID string) error {
	_, err := m.Rd.Del(m.firstOpKey(projectID))
	return err
}

// ClearDanglingFirstOpTimestamp: if ts+ops keys both exist or both absent,
// return 0; else delete the ts key and return the deleted count.
func (m *RedisManager) ClearDanglingFirstOpTimestamp(projectID string) (int, error) {
	n, err := m.Rd.Exists(m.firstOpKey(projectID), m.opsKey(projectID))
	if err != nil {
		return 0, err
	}
	if n == 2 || n == 0 {
		return 0, nil
	}
	deleted, err := m.Rd.Del(m.firstOpKey(projectID))
	if err != nil {
		return 0, err
	}
	return int(deleted), nil
}

func (m *RedisManager) GetCachedHistoryID(projectID string) (string, bool, error) {
	v, ok, err := m.Rd.Get(m.cachedHistoryIDKey(projectID))
	if err != nil {
		return "", ok, err
	}
	return v, ok, nil
}

func (m *RedisManager) SetCachedHistoryID(projectID, historyID string) error {
	return m.Rd.Set(m.cachedHistoryIDKey(projectID), historyID, CacheTTLSeconds)
}

func (m *RedisManager) ClearCachedHistoryID(projectID string) error {
	_, err := m.Rd.Del(m.cachedHistoryIDKey(projectID))
	return err
}

// GetProjectIDsWithHistoryOpsCount — vendor getProjectIdsWithHistoryOpsCount
// (HTTP status/queue path): count of projects with queued ops.
func (m *RedisManager) GetProjectIDsWithHistoryOpsCount() (int, error) {
	ids, err := m.GetProjectIDsWithHistoryOps()
	if err != nil {
		return 0, err
	}
	return len(ids), nil
}
