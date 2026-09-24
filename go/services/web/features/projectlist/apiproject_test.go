package projectlist

import (
	"testing"
	"time"
)

// ---------- apFormat (bucket order + levels + per-user flags) ----------

func TestAPFormatBucketOrderAndLevels(t *testing.T) {
	uid := "6aa4b8a873ef0e5094f4cba3"
	b := &buckets{
		owned:             []docRef{{id: "O1", name: "own"}},
		readWrite:         []docRef{{id: "RW1", name: "rw"}},
		review:            []docRef{{id: "RV1", name: "rv"}},
		readOnly:          []docRef{{id: "RO1", name: "ro"}},
		tokenReadAndWrite: []docRef{{id: "TW1", name: "tw"}, {id: "O1", name: "dup-owned"}},
		tokenReadOnly:     []docRef{{id: "TO1", name: "to"}, {id: "RW1", name: "dup-rw"}},
	}
	items := apFormat(b, uid)
	if len(items) != 6 {
		t.Fatalf("len = %d want 6 (token dedup)", len(items))
	}
	want := []struct {
		id, level, source string
	}{
		{"O1", "owner", "owner"},
		{"RW1", "readWrite", "invite"},
		{"RV1", "review", "invite"},
		{"RO1", "readOnly", "invite"},
		{"TW1", "readAndWrite", "token"},
		{"TO1", "readOnly", "token"},
	}
	for i, w := range want {
		if items[i].id != w.id || items[i].accessLevel != w.level || items[i].source != w.source {
			t.Fatalf("item %d = %+v want id=%s level=%s source=%s", i, items[i], w.id, w.level, w.source)
		}
	}
}

func TestAPFormatReadOnlyTokenNullsOwnerAndLUB(t *testing.T) {
	uid := "u"
	ownerHex := "6aa4b8a873ef0e5094f4cba3"
	lubHex := "6aa4b8a873ef0e5094f4cba4"
	b := &buckets{
		tokenReadOnly: []docRef{{
			id: "T1", name: "t",
			ownerRefHex: ownerHex, lastUpdatedByHex: lubHex,
			lastUpdated: time.Now(), hasLastUpdated: true,
		}},
	}
	items := apFormat(b, uid)
	if len(items) != 1 {
		t.Fatalf("len %d", len(items))
	}
	it := items[0]
	if it.ownerRef != "" || it.lastUpdatedBy != "" {
		t.Fatalf("readOnly-token must null owner+lastUpdatedBy: %+v", it)
	}
}

func TestAPFormatArchivedTrashedPerUser(t *testing.T) {
	uid := "me"
	other := "other"
	b := &buckets{
		owned: []docRef{
			{id: "A1", name: "a", archived: []string{uid}, trashed: []string{uid}},
			{id: "T1", name: "t", trashed: []string{uid}, archived: []string{other}},
			{id: "N1", name: "n"},
		},
	}
	items := apFormat(b, uid)
	if !items[0].archived || items[0].trashed {
		t.Fatalf("A1: archived && !trashed (both → archived wins), got %+v", items[0])
	}
	if items[1].archived || !items[1].trashed {
		t.Fatalf("T1: trashed only, got %+v", items[1])
	}
	if items[2].archived || items[2].trashed {
		t.Fatalf("N1: neither, got %+v", items[2])
	}
}

// ---------- apFilter ----------

func TestAPFilterNoActiveFilterPassesAll(t *testing.T) {
	items := []apItem{{id: "1", accessLevel: "owner"}, {id: "2", accessLevel: "readWrite"}}
	f := &apFilters{}
	if got := apFilter(items, nil, f); len(got) != 2 {
		t.Fatalf("len %d", len(got))
	}
	if got := apFilter(items, nil, nil); len(got) != 2 {
		t.Fatalf("nil filter len %d", len(got))
	}
}

func TestAPFilterOwnedSharedArchivedTrashed(t *testing.T) {
	items := []apItem{
		{id: "1", accessLevel: "owner", archived: false, trashed: false},
		{id: "2", accessLevel: "readWrite", archived: false, trashed: false},
		{id: "3", accessLevel: "owner", archived: true, trashed: false},
		{id: "4", accessLevel: "readWrite", archived: false, trashed: true},
	}
	if got := apFilter(items, nil, &apFilters{ownedByUser: true, anyFilter: true}); len(got) != 2 {
		t.Fatalf("owned: %+v", got)
	}
	if got := apFilter(items, nil, &apFilters{sharedWithUser: true, anyFilter: true}); len(got) != 2 {
		t.Fatalf("shared: %+v", got)
	}
	if got := apFilter(items, nil, &apFilters{archived: true, anyFilter: true}); len(got) != 1 || got[0].id != "3" {
		t.Fatalf("archived: %+v", got)
	}
	if got := apFilter(items, nil, &apFilters{trashed: true, anyFilter: true}); len(got) != 1 || got[0].id != "4" {
		t.Fatalf("trashed: %+v", got)
	}
}

func TestAPFilterTag(t *testing.T) {
	items := []apItem{{id: "p1", name: "a"}, {id: "p2", name: "b"}}
	tags := []apTagRow{{name: "work", projectIDs: []string{"p1"}}, {name: "other", projectIDs: []string{"p9"}}}
	if got := apFilter(items, tags, &apFilters{hasTag: true, tag: "work", anyFilter: true}); len(got) != 1 || got[0].id != "p1" {
		t.Fatalf("tag work: %+v", got)
	}
	if got := apFilter(items, tags, &apFilters{hasTag: true, tag: "nope", anyFilter: true}); len(got) != 0 {
		t.Fatalf("tag missing: %+v", got)
	}
}

func TestAPFilterSearchCaseInsensitive(t *testing.T) {
	items := []apItem{{id: "1", name: "My Project"}, {id: "2", name: "other"}}
	if got := apFilter(items, nil, &apFilters{hasSearch: true, search: "PROJ", anyFilter: true}); len(got) != 1 || got[0].id != "1" {
		t.Fatalf("search: %+v", got)
	}
}

func TestAPFilterTagNullIsActiveButMatchesAll(t *testing.T) {
	items := []apItem{{id: "1"}, {id: "2"}}
	f := &apFilters{tagIsNull: true, anyFilter: true}
	if got := apFilter(items, nil, f); len(got) != 2 {
		t.Fatalf("tag:null matches all, got %+v", got)
	}
}

// ---------- apSort (stable, no-op for title/owner) ----------

func TestAPSortLastUpdatedDesc(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []apItem{
		{id: "A", lastUpdated: t0.Add(1 * time.Second)},
		{id: "B", lastUpdated: t0},
		{id: "C", lastUpdated: t0.Add(2 * time.Second)},
	}
	got := apSort(items, nil) // default desc
	if got[0].id != "C" || got[1].id != "A" || got[2].id != "B" {
		t.Fatalf("desc: %+v", got)
	}
	got = apSort(items, &apSortReq{by: "lastUpdated", order: "asc"})
	if got[0].id != "B" || got[1].id != "A" || got[2].id != "C" {
		t.Fatalf("asc: %+v", got)
	}
}

func TestAPSortTitleOwnerAreStableNoOps(t *testing.T) {
	items := []apItem{{id: "X", name: "zzz"}, {id: "Y", name: "aaa"}, {id: "Z"}}
	for _, by := range []string{"title", "owner"} {
		got := apSort(items, &apSortReq{by: by, order: "desc"})
		if got[0].id != "X" || got[1].id != "Y" || got[2].id != "Z" {
			t.Fatalf("by=%s must be a stable no-op: %+v", by, got)
		}
		got = apSort(items, &apSortReq{by: by, order: "asc"})
		if got[0].id != "X" {
			t.Fatalf("by=%s asc must keep input order: %+v", by, got)
		}
	}
}

func TestAPSortStableEqualKeys(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []apItem{{id: "1", lastUpdated: t0}, {id: "2", lastUpdated: t0}, {id: "3", lastUpdated: t0}}
	got := apSort(items, &apSortReq{by: "lastUpdated", order: "desc"})
	if got[0].id != "1" || got[1].id != "2" || got[2].id != "3" {
		t.Fatalf("equal keys must keep relative order (stable): %+v", got)
	}
}

// ---------- apValidate matrix (pinned wire strings) ----------

func TestAPValidateClean(t *testing.T) {
	body := apOOBj{M: map[string]any{"filters": apOOBj{M: map[string]any{"ownedByUser": true, "tag": nil}}, "sort": apOOBj{M: map[string]any{"by": "title"}}, "page": apOOBj{M: map[string]any{"size": float64(3)}}}, Order: []string{"filters", "sort", "page"}}
	issues, p, ok := apValidate(body)
	if !ok {
		t.Fatalf("expected ok, issues=%q", issues)
	}
	if !p.hasFilters || !p.filters.ownedByUser || !p.filters.tagIsNull {
		t.Fatalf("filters: %+v", p.filters)
	}
	if !p.hasSort || p.sort.by != "title" {
		t.Fatalf("sort: %+v", p.sort)
	}
	if !p.hasPage || p.page.size != 3 {
		t.Fatalf("page: %+v", p.page)
	}
}

func TestAPValidateBodyUnknown(t *testing.T) {
	body := apOOBj{M: map[string]any{"zzz": 1}, Order: []string{"zzz"}}
	issues, _, ok := apValidate(body)
	if ok {
		t.Fatal("expected failure")
	}
	want := `Unrecognized key: \"zzz\" at \"body\"`
	if issues != want {
		t.Fatalf("got %q want %q", issues, want)
	}
}

func TestAPValidateFiltersUnknown(t *testing.T) {
	body := apOOBj{M: map[string]any{"filters": apOOBj{M: map[string]any{"zzz": 1}, Order: []string{"zzz"}}}, Order: []string{"filters"}}
	issues, _, ok := apValidate(body)
	if ok {
		t.Fatal("expected failure")
	}
	want := `Unrecognized key: \"zzz\" at \"body.filters\"`
	if issues != want {
		t.Fatalf("got %q want %q", issues, want)
	}
}

func TestAPValidateSortEnum(t *testing.T) {
	body := apOOBj{M: map[string]any{"sort": apOOBj{M: map[string]any{"by": "nope", "order": "updown"}, Order: []string{"by", "order"}}}, Order: []string{"sort"}}
	issues, _, ok := apValidate(body)
	if ok {
		t.Fatal("expected failure")
	}
	want := `Invalid option: expected one of \"lastUpdated\"|\"title\"|\"owner\" at \"body.sort.by\"; Invalid option: expected one of \"asc\"|\"desc\" at \"body.sort.order\"`
	if issues != want {
		t.Fatalf("got %q want %q", issues, want)
	}
}

func TestAPValidatePageSize(t *testing.T) {
	body := apOOBj{M: map[string]any{"page": apOOBj{M: map[string]any{"size": float64(-1)}, Order: []string{"size"}}}, Order: []string{"page"}}
	issues, _, ok := apValidate(body)
	if ok {
		t.Fatal("expected failure")
	}
	want := `Too small: expected number to be >0 at \"body.page.size\"`
	if issues != want {
		t.Fatalf("got %q want %q", issues, want)
	}
}

func TestAPValidatePageLastId(t *testing.T) {
	body := apOOBj{M: map[string]any{"page": apOOBj{M: map[string]any{"lastId": "zzz"}, Order: []string{"lastId"}}}, Order: []string{"page"}}
	issues, _, ok := apValidate(body)
	if ok {
		t.Fatal("expected failure")
	}
	want := `Invalid Mongo ObjectId at \"body.page.lastId\"`
	if issues != want {
		t.Fatalf("got %q want %q", issues, want)
	}
}

func TestAPValidateTypes(t *testing.T) {
	cases := []struct {
		m    map[string]any
		want string
	}{
		{map[string]any{"ownedByUser": "yes"}, `Invalid input: expected boolean, received string at \"body.filters.ownedByUser\"`},
		{map[string]any{"tag": true}, `Invalid input: expected string, received boolean at \"body.filters.tag\"`},
		{map[string]any{"search": float64(5)}, `Invalid input: expected string, received number at \"body.filters.search\"`},
	}
	for _, c := range cases {
		body := apOOBj{M: map[string]any{"filters": apOOBj{M: c.m}}, Order: []string{"filters"}}
		issues, _, ok := apValidate(body)
		if ok {
			t.Fatalf("%q: expected failure", c.want)
		}
		if issues != c.want {
			t.Fatalf("got %q want %q", issues, c.want)
		}
	}
}

func TestAPValidateNullSort(t *testing.T) {
	body := apOOBj{M: map[string]any{"sort": nil}, Order: []string{"sort"}}
	issues, _, ok := apValidate(body)
	if ok {
		t.Fatal("expected failure")
	}
	want := `Invalid input: expected object, received null at \"body.sort\"`
	if issues != want {
		t.Fatalf("got %q want %q", issues, want)
	}
}

func TestAPFillDefaults(t *testing.T) {
	now := time.Date(2026, 9, 22, 8, 50, 0, 0, time.UTC)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := apFillDefaults([]apItem{
		{id: "have", lastUpdated: t0, hasLastUpd: true},
		{id: "missing"},
	}, now)
	if items[1].lastUpdated != now || !items[1].hasLastUpd {
		t.Fatalf("missing lastUpdated must fill with now: %+v", items[1])
	}
	if items[0].lastUpdated != t0 {
		t.Fatalf("existing lastUpdated untouched: %+v", items[0])
	}
	// fill-then-sort (Node order): the filled value = now = newest → head of
	// lastUpdated-desc, tail of asc
	filled := apFillDefaults([]apItem{
		{id: "have", lastUpdated: t0, hasLastUpd: true},
		{id: "missing"},
	}, now)
	sorted := apSort(filled, &apSortReq{by: "lastUpdated", order: "desc"})
	if sorted[0].id != "missing" || sorted[1].id != "have" {
		t.Fatalf("desc: filled-now (newest) must be first: %s,%s", sorted[0].id, sorted[1].id)
	}
	filled = apFillDefaults([]apItem{
		{id: "have", lastUpdated: t0, hasLastUpd: true},
		{id: "missing"},
	}, now)
	sorted = apSort(filled, &apSortReq{by: "lastUpdated", order: "asc"})
	if sorted[0].id != "have" || sorted[1].id != "missing" {
		t.Fatalf("asc: filled-now (newest) must be last: %s,%s", sorted[0].id, sorted[1].id)
	}
}
