package otc

// change_identity_test.go — 1:1 mirror of test/unit/change_identity.test.js
// (the Node oracle / acceptance spec).

import (
	"testing"
	"time"
)

const (
	ciEditorA = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
	ciEditorB = "9c858901-8a57-4791-81fe-4c455b099bc9"
	ciUserA   = "65b9d7fb2a1b2c3d4e5f6a7b"
	ciUserB   = "65b9d7fb2a1b2c3d4e5f6a7c"
)

var ciTimestamp = time.Date(2025, 1, 2, 3, 4, 5, 678000000, time.UTC)

func ciRaw(overrides map[string]any) map[string]any {
	raw := map[string]any{
		"operations": []any{},
		"timestamp":  "2025-01-02T03:04:05.678Z",
		"v2Authors":  []any{ciUserA},
		"origin":     map[string]any{"kind": EditorOriginKind, "historyClientId": ciEditorA},
	}
	for k, v := range overrides {
		raw[k] = v
	}
	return raw
}

func ciSame(a, b map[string]any) bool {
	return IsSameEditorChange(ChangeIdentityFromRaw(a), ChangeIdentityFromRaw(b))
}

func TestCISameChange(t *testing.T) {
	if !ciSame(ciRaw(nil), ciRaw(nil)) {
		t.Fatal("same raw change must match")
	}
}

func TestCIRefusesToMatch(t *testing.T) {
	cases := map[string]map[string]any{
		"different editor sharing author+ts":     {"origin": map[string]any{"kind": EditorOriginKind, "historyClientId": ciEditorB}},
		"different author sharing historyClient": {"v2Authors": []any{ciUserB}},
		"different timestamp":                    {"timestamp": "2025-01-02T03:04:05.679Z"},
		"another writer":                         {"origin": map[string]any{"kind": "dropbox"}},
		"no origin at all":                       {"origin": nil},
		"historyClientId stripped":               {"origin": map[string]any{"kind": EditorOriginKind}},
		"more than one author":                   {"v2Authors": []any{ciUserA, ciUserB}},
		"no authors":                             {"v2Authors": []any{}},
	}
	for name, ov := range cases {
		if ciSame(ciRaw(nil), ciRaw(ov)) {
			t.Errorf("must NOT match: %s", name)
		}
	}
}

func TestCIAnonymousAuthors(t *testing.T) {
	if !ciSame(ciRaw(map[string]any{"v2Authors": []any{nil}}), ciRaw(map[string]any{"v2Authors": []any{nil}})) {
		t.Error("two anonymous from same editor must match")
	}
	if ciSame(ciRaw(map[string]any{"v2Authors": []any{nil}}), ciRaw(map[string]any{"v2Authors": []any{nil}, "origin": map[string]any{"kind": EditorOriginKind, "historyClientId": ciEditorB}})) {
		t.Error("two anonymous editors must stay separate")
	}
	if ciSame(ciRaw(map[string]any{"v2Authors": []any{nil}}), ciRaw(nil)) {
		t.Error("anonymous must not match a signed-in change")
	}
}

func TestCITimestamps(t *testing.T) {
	if !ciSame(ciRaw(map[string]any{"timestamp": "2025-01-02T03:04:05.678Z"}),
		ciRaw(map[string]any{"timestamp": "2025-01-02T04:04:05.678+01:00"})) {
		t.Error("equal instants with different spellings must match")
	}
	if got := ChangeIdentityFromRaw(ciRaw(map[string]any{"timestamp": "not a date"})); got != nil {
		t.Errorf("unparseable timestamp must be nil, got %+v", got)
	}
}

func TestCIChangeIdentityOf(t *testing.T) {
	author := ciUserA
	mine := ChangeIdentityOf(ciEditorA, &author, ciTimestamp)
	if !IsSameEditorChange(mine, ChangeIdentityFromRaw(ciRaw(nil))) {
		t.Error("identityOf must match a raw change describing the same submission")
	}
	mineAnon := ChangeIdentityOf(ciEditorA, nil, ciTimestamp)
	if !IsSameEditorChange(mineAnon, ChangeIdentityFromRaw(ciRaw(map[string]any{"v2Authors": []any{nil}}))) {
		t.Error("null author must be treated as anonymous")
	}
}

func TestCINeverMatchesNull(t *testing.T) {
	if IsSameEditorChange(nil, nil) {
		t.Error("null must never match, including another null")
	}
	if IsSameEditorChange(ChangeIdentityFromRaw(ciRaw(nil)), nil) {
		t.Error("identity must never match null")
	}
}

func TestCIIsChangeFrom(t *testing.T) {
	const kind = "github"
	ciRawWriter := func(ov map[string]any) map[string]any {
		raw := map[string]any{
			"operations": []any{},
			"timestamp":  "2025-01-02T03:04:05.678Z",
			"v2Authors":  []any{ciUserA},
			"origin":     map[string]any{"kind": kind},
		}
		for k, v := range ov {
			raw[k] = v
		}
		return raw
	}
	if !IsChangeFrom(ciRawWriter(nil), kind, ciUserA, ciTimestamp) {
		t.Error("must recognise a change this writer placed")
	}
	if !IsChangeFrom(ciRawWriter(map[string]any{"v2Authors": []any{ciUserB, ciUserA}}), kind, ciUserA, ciTimestamp) {
		t.Error("must recognise it among several authors")
	}
	if !IsChangeFrom(ciRawWriter(map[string]any{"timestamp": "2025-01-02T04:04:05.678+01:00"}), kind, ciUserA, ciTimestamp) {
		t.Error("must compare the timestamp as a point in time")
	}

	refusal := []struct {
		name string
		raw  map[string]any
	}{
		{"another writer's kind", ciRawWriter(map[string]any{"origin": map[string]any{"kind": "dropbox"}})},
		{"no origin", ciRawWriter(map[string]any{"origin": nil})},
		{"another user's author", ciRawWriter(map[string]any{"v2Authors": []any{ciUserB}})},
		{"no authors", ciRawWriter(map[string]any{"v2Authors": nil})},
		{"empty authors", ciRawWriter(map[string]any{"v2Authors": []any{}})},
		{"another moment", ciRawWriter(map[string]any{"timestamp": "2025-01-02T03:04:05.679Z"})},
		{"unparseable timestamp", ciRawWriter(map[string]any{"timestamp": "not a date"})},
	}
	for _, tc := range refusal {
		if IsChangeFrom(tc.raw, kind, ciUserA, ciTimestamp) {
			t.Errorf("must NOT match: %s", tc.name)
		}
	}
	if IsChangeFrom(nil, kind, ciUserA, ciTimestamp) {
		t.Error("raw==nil must not match")
	}
}

func TestCIIdentityFields(t *testing.T) {
	id := ChangeIdentityFromRaw(ciRaw(nil))
	if id == nil {
		t.Fatal("expected an identity")
	}
	if id.HistoryClientId != ciEditorA {
		t.Errorf("historyClientId = %q", id.HistoryClientId)
	}
	if id.Author == nil || *id.Author != ciUserA {
		t.Errorf("author = %v", id.Author)
	}
	if id.Timestamp != ciTimestamp.UnixMilli() {
		t.Errorf("timestamp = %d, want %d", id.Timestamp, ciTimestamp.UnixMilli())
	}
}
