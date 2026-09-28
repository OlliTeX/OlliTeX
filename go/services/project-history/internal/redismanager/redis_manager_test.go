package redismanager

import (
	"fmt"
	"strings"
	"testing"

	"ollitex/go/services/project-history/internal/redisx"
)

type testKeys struct{}

func (testKeys) ProjectHistoryOps(projectID string) string              { return "ops:" + projectID }
func (testKeys) ProjectHistoryFirstOpTimestamp(projectID string) string { return "ts:" + projectID }
func (testKeys) ProjectHistoryCachedHistoryID(projectID string) string  { return "c:" + projectID }
func (testKeys) ProjectHistoryLock(projectID string) string             { return "lock:" + projectID }

// fakeClient implements redisx.Client backed by in-memory maps/lists.
type fakeClient struct {
	str          map[string]string
	list         map[string][]string
	failLRangeAt int // -1: no error; else 0-based LRange call index that returns an error
	lrangeCalls  int
	failOps      map[string]error // op name → injected error for Get/Set/SetNX/SetNXWithTTL/Del/Exists/Expire/LRem/LLen/Scan/MGet/Ping
}

func newFake() *fakeClient {
	return &fakeClient{str: map[string]string{}, list: map[string][]string{}, failLRangeAt: -1}
}

func (f *fakeClient) seedList(k string, vals ...string) {
	f.list[k] = append(vals, f.list[k]...)
}
func (f *fakeClient) Get(k string) (string, bool, error) {
	if e := f.fail("Get"); e != nil {
		return "", false, e
	}
	v, ok := f.str[k]
	return v, ok, nil
}
func (f *fakeClient) Set(k, v string, ttl ...int) error {
	if e := f.fail("Set"); e != nil {
		return e
	}
	f.str[k] = v
	return nil
}
func (f *fakeClient) SetNX(k, v string) (bool, error) {
	if e := f.fail("SetNX"); e != nil {
		return false, e
	}
	if _, ok := f.str[k]; ok {
		return false, nil
	}
	f.str[k] = v
	return true, nil
}
func (f *fakeClient) SetNXWithTTL(k, v string, ttlSeconds int) (bool, error) {
	if e := f.fail("SetNXWithTTL"); e != nil {
		return false, e
	}
	if _, ok := f.str[k]; ok {
		return false, nil
	}
	f.str[k] = v
	return true, nil
}
func (f *fakeClient) Del(keys ...string) (int64, error) {
	if e := f.fail("Del"); e != nil {
		return 0, e
	}
	var n int64
	for _, k := range keys {
		if _, ok := f.str[k]; ok {
			delete(f.str, k)
			n++
		}
		if v, ok := f.list[k]; ok {
			// Redis DEL: deletes a non-empty list and returns 1 if the key
			// existed; a key that has been LREM'd to empty still counts.
			delete(f.list, k)
			n++
			if len(v) == 0 {
				n--
			}
		}
	}
	return n, nil
}
func (f *fakeClient) Exists(keys ...string) (int, error) {
	if e := f.fail("Exists"); e != nil {
		return 0, e
	}
	n := 0
	for _, k := range keys {
		if _, ok := f.str[k]; ok {
			n++
		}
		if len(f.list[k]) > 0 {
			n++
		}
	}
	return n, nil
}
func (f *fakeClient) Expire(k string, ttl int) error { return f.fail("Expire") }
func (f *fakeClient) LRange(k string, start, stop int) ([]string, error) {
	f.lrangeCalls += 1
	if f.lrangeCalls-1 == f.failLRangeAt {
		return nil, fmt.Errorf("LRANGE failed (injected at call %d)", f.lrangeCalls-1)
	}
	if e := f.fail("LRange"); e != nil {
		return nil, e
	}
	l := f.list[k]
	if len(l) == 0 {
		return []string{}, nil
	}
	// Redis index semantics: negative counts from the end.
	if start < 0 {
		start += len(l)
	}
	if stop < 0 {
		stop += len(l)
	}
	if start >= len(l) || start > stop {
		return []string{}, nil
	}
	if stop >= len(l) {
		stop = len(l) - 1
	}
	return l[start : stop+1], nil
}
func (f *fakeClient) LRem(k string, n int, val string) (int64, error) {
	if e := f.fail("LRem"); e != nil {
		return 0, e
	}
	l := f.list[k]
	out := []string{}
	nRemoved := 0
	for _, v := range l {
		if v == val && nRemoved < n {
			nRemoved++
			continue
		}
		out = append(out, v)
	}
	f.list[k] = out
	return int64(nRemoved), nil
}
func (f *fakeClient) LLen(k string) (int64, error) {
	if e := f.fail("LLen"); e != nil {
		return 0, e
	}
	return int64(len(f.list[k])), nil
}
func (f *fakeClient) Scan(pattern string, limit int) ([]string, error) {
	if e := f.fail("Scan"); e != nil {
		return nil, e
	}
	out := []string{}
	seen := map[string]bool{}
	add := func(k string) {
		if !seen[k] && scanMatch(pattern, k) {
			seen[k] = true
			out = append(out, k)
		}
	}
	for k := range f.str {
		add(k)
	}
	for k := range f.list {
		// Redis DEL on a list to empty still counts as a key only when len>0
		if len(f.list[k]) > 0 {
			add(k)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// scanMatch: redis SCAN MATCH semantics — '*' is a glob wildcard; we only
// support the "prefix*" and exact forms used by project-history keys.
func scanMatch(pattern, key string) bool {
	if pattern == key {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(key, strings.TrimSuffix(pattern, "*"))
	}
	return false
}
func (f *fakeClient) MGet(keys ...string) ([]string, error) {
	if e := f.fail("MGet"); e != nil {
		return nil, e
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, f.str[k])
	}
	return out, nil
}
func (f *fakeClient) Ping() error  { return f.fail("Ping") }
func (f *fakeClient) Close() error { return f.fail("Close") }

// fail returns the injected error for op, or nil.
func (f *fakeClient) fail(op string) error {
	if f.failOps == nil {
		return nil
	}
	return f.failOps[op]
}

var _ redisx.Client = (*fakeClient)(nil)

func TestBatchSizeBreak(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	// Seed 5 raw updates (LPush is reversed, so LRange reads front→back).
	for i := 5; i >= 1; i-- {
		f.seedList("ops:p1", "u"+string([]byte{byte(97 + i - 1)}))
	}
	batch, err := rm.GetRawUpdatesBatch("p1", 3)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(batch.RAWUpdates) != 3 || !batch.HasMore {
		t.Fatalf("want 3 raws hasMore=true, got %d hasMore=%v", len(batch.RAWUpdates), batch.HasMore)
	}
}

// TestDeleteAppliedDocUpdates: LREM removes only the applied raw (exact
// match); the rest of the queue survives; ts key is deleted.
func TestDeleteAppliedDocUpdates(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.seedList("ops:p1", "applied")
	f.str["ts:p1"] = "1"
	if err := rm.DeleteAppliedDocUpdates("p1", []string{"applied"}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if rest, _ := f.LRange("ops:p1", 0, -1); len(rest) != 0 {
		t.Fatalf("want empty queue, got %#v", rest)
	}
	if _, ok := f.str["ts:p1"]; ok {
		t.Fatalf("want ts deleted")
	}
}

// TestDeleteAppliedPartial: a second, later update stays in the queue.
func TestDeleteAppliedPartial(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.seedList("ops:p1", "later", "head")
	f.str["ts:p1"] = "1"
	if err := rm.DeleteAppliedDocUpdates("p1", []string{"head"}); err != nil {
		t.Fatalf("err: %v", err)
	}
	if rest, _ := f.LRange("ops:p1", 0, -1); len(rest) != 1 || rest[0] != "later" {
		t.Fatalf("want the later update, got %#v", rest)
	}
}

func TestDanglingFirstOpTimestamp(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	p := "p1"
	// neither present → no-op
	if n, _ := rm.ClearDanglingFirstOpTimestamp(p); n != 0 {
		t.Fatalf("neither: want 0 got %d", n)
	}
	// only ts → cleared
	f.str["ts:p1"] = "1"
	if n, _ := rm.ClearDanglingFirstOpTimestamp(p); n == 0 {
		t.Fatalf("only-ts: want 1 got %d", n)
	}
	if _, ok := f.str["ts:p1"]; ok {
		t.Fatalf("want ts cleared")
	}
	// both present → preserved
	f.str["ts:p1"] = "1"
	f.seedList("ops:p1", "x")
	if n, _ := rm.ClearDanglingFirstOpTimestamp(p); n != 0 {
		t.Fatalf("both: want 0 got %d", n)
	}
	if _, ok := f.str["ts:p1"]; !ok {
		t.Fatalf("want ts preserved")
	}
}

func TestCachedHistoryID(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	if err := rm.SetCachedHistoryID("p1", "hid"); err != nil {
		t.Fatalf("set: %v", err)
	}
	hid, ok, _ := rm.GetCachedHistoryID("p1")
	if !ok || hid != "hid" {
		t.Fatalf("want hid got ok=%v v=%q", ok, hid)
	}
	if err := rm.ClearCachedHistoryID("p1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok, _ := rm.GetCachedHistoryID("p1"); ok {
		t.Fatalf("want cleared")
	}
}

func TestFirstOpTimestampRoundtrip(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	if err := rm.SetFirstOpTimestamp("p1"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if ts, ok, _ := rm.GetFirstOpTimestamp("p1"); !ok || ts.IsZero() {
		t.Fatalf("want ts, ok=%v zero=%v", ok, ts.IsZero())
	}
	if _, ok, _ := rm.GetFirstOpTimestamp("missing"); ok {
		t.Fatalf("want missing ok=false")
	}
	if err := rm.ClearFirstOpTimestamp("p1"); err != nil {
		t.Fatalf("clear: %v", err)
	}
}

func TestDestroyDocUpdatesQueue(t *testing.T) {
	f := newFake()
	rm := New(f, testKeys{})
	f.seedList("ops:p1", "a", "b")
	f.str["ts:p1"] = "1"
	if err := rm.DestroyDocUpdatesQueue("p1"); err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(f.list["ops:p1"]) != 0 {
		t.Fatalf("want ops cleared")
	}
	if _, ok := f.str["ts:p1"]; ok {
		t.Fatalf("want ts cleared")
	}
}
