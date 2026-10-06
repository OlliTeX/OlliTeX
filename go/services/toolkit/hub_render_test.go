package toolkit

import (
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var hubNow = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func TestHub_TrashedFlagLiveShapes(t *testing.T) {
	// the LIVE legacy model (verified overleafmongo `ollitex` 2026-10-06):
	// trashed is an ARRAY — empty = not trashed, non-empty = trashed.
	if trashedFlag(bson.A{}) != false {
		t.Errorf("trashed: [] must be NOT trashed (the live default)")
	}
	if trashedFlag([]any{}) != false {
		t.Errorf("trashed: []any{} must be NOT trashed")
	}
	if trashedFlag(bson.A{"671b6c9ef2e168f259294e9a"}) != true {
		t.Errorf("trashed: [userid] must be trashed")
	}
	if trashedFlag(true) != true {
		t.Errorf("trashed: true must be trashed")
	}
	if trashedFlag("671b6c9ef2e168f259294e9a") != true {
		t.Errorf("trashed: a user-id string must be trashed")
	}
	if trashedFlag(nil) != false {
		t.Errorf("trashed: absent must be NOT trashed")
	}
	if trashedFlag("false") != false {
		t.Errorf("trashed: literal false-string must be NOT trashed")
	}
}

func TestHub_ProjectDocShapes(t *testing.T) {
	// name first (live), title second (newer)
	d := bson.M{"_id": bson.NewObjectID(), "name": "live-name", "title": "new-title"}
	r := projectDoc(d)
	if r.Name != "live-name" {
		t.Errorf("live projects use `name` — got %q", r.Name)
	}
	d2 := bson.M{"_id": bson.NewObjectID(), "title": "new-title"}
	if projectDoc(d2).Name != "new-title" {
		t.Errorf("newer projects use `title` — must be accepted")
	}
	// RFC3339 string dates (the live serialized shape)
	if asDocTime("2024-12-30T04:28:01.667Z") == 0 {
		t.Errorf("an RFC3339 string date must parse to ms")
	}
}

func TestHub_UsersDocFlags(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	yearAgo := now.AddDate(-1, 0, 0).UnixMilli()
	// admin (active)
	m := usersDoc(bson.M{"email": "admin@x.de", "isAdmin": true, "lastActive": now.UnixMilli(), "signUpDate": yearAgo - 1000}, now)
	if !m.IsAdmin || m.Inactive {
		t.Errorf("admin+active: got %+v", m)
	}
	// inactive: missing lastActive (Node: !user.lastActive)
	n := usersDoc(bson.M{"email": "never@x.de", "signUpDate": yearAgo - 1000}, now)
	if !n.Inactive {
		t.Errorf("missing lastActive must be inactive (Node parity)")
	}
	// inactive: stale lastActive
	o := usersDoc(bson.M{"email": "stale@x.de", "lastActive": yearAgo - 1}, now)
	if !o.Inactive {
		t.Errorf("lastActive just before the calendar-year boundary must be inactive")
	}
	// suspended: a date-string value is truthy (Node truthy())
	sp := usersDoc(bson.M{"email": "susp@x.de", "suspended": "2026-01-01", "lastActive": now.UnixMilli()}, now)
	if !sp.Suspended {
		t.Errorf("a date-string suspended value must be truthy (Node parity)")
	}
	// suspended: empty array = NOT suspended (live array model)
	ne := usersDoc(bson.M{"email": "ok@x.de", "suspended": bson.A{}, "lastActive": now.UnixMilli()}, now)
	if ne.Suspended {
		t.Errorf("an empty suspended array must be NOT suspended (the live default)")
	}
	// emails[] shape
	em := usersDoc(bson.M{"emails": []any{bson.M{"emailAddress": "e@a.de"}}, "lastActive": now.UnixMilli()}, now)
	if em.Email != "e@a.de" {
		t.Errorf("emails[] shape must resolve the address")
	}
	// owner id -> email resolution (the hub join)
	emap := map[string]string{"deadbeef0000000000000001": "resolved@x.de"}
	if got := ownerEmail("deadbeef0000000000000001", emap); got != "resolved@x.de" {
		t.Errorf("owner hex id must resolve through the users map, got %q", got)
	}
	if got := ownerEmail("already@x.de", emap); got != "already@x.de" {
		t.Errorf("a plain email owner must pass through, got %q", got)
	}
}

func TestHub_PanelRenders(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk)
	a.width, a.height = 118, 34

	// not loaded yet
	a.screen = "hub"
	v := a.View()
	if !strings.Contains(v, "HUB") || !strings.Contains(v, "not loaded") {
		t.Errorf("the hub panel must show its not-loaded state:\n%s", v)
	}

	// loaded
	a.hub = &HubStats{DB: "ollitex", Now: hubNow, MongoHost: "ollitex-mongo:27017"}
	a.hub.Projects.All = 280
	a.hub.Projects.Inactive = 2
	a.hub.Projects.Trashed = 1
	a.hub.Projects.Deleted = 3
	a.hub.Projects.Sample = []HubProjectRow{
		{Name: "frog", Owner: "admin@example.com", LastUpdated: hubNow.Add(-2 * time.Hour).UnixMilli()},
	}
	a.hub.Users.All = 144
	a.hub.Users.Admins = 2
	a.hub.Users.Suspended = 1
	a.hub.Users.Inactive = 0
	a.hub.Users.Deleted = 4
	a.hub.Users.Sample = []HubUserRow{
		{Email: "admin@example.com", IsAdmin: true, LastActive: hubNow.UnixMilli(), SignUp: hubNow.Add(-400 * 24 * time.Hour).UnixMilli()},
	}
	v = a.View()
	for _, want := range []string{"PROJECTS", "USERS", "280", "frog", "admin@example.com"} {
		if !strings.Contains(v, want) {
			t.Errorf("hub panel missing %q:\n%s", want, v)
		}
	}
	// the master list picks up the live subtitle
	found2 := false
	for _, it2 := range a.masterList() {
		if it2.id == "hub" {
			found2 = true
		}
	}
	if !found2 {
		t.Errorf("the master list must contain the Hub row")
	}
	found := false
	for _, it := range a.masterList() {
		if it.id == "hub" && strings.Contains(it.detail, "projects") {
			found = true
		}
	}
	if !found {
		t.Errorf("the Hub master row must carry the live counters as its subtitle")
	}
}

func TestAppBootScreen(t *testing.T) {
	tk, _ := offlineToolkit(t)
	a := newApp(tk, "hub")
	if a.screen != "hub" {
		t.Fatalf("ssh host hub must boot on the hub screen, got %q", a.screen)
	}
	if !validScreen(a, "hub") {
		t.Errorf("hub must be a valid boot screen")
	}
	if b := newApp(tk, "no-such-screen"); b.screen != "dashboard" {
		t.Fatalf("an unknown boot screen must fall back to dashboard, got %q", b.screen)
	}
	if c := newApp(tk); c.screen != "dashboard" {
		t.Fatalf("default boot stays dashboard")
	}
	// the hub boot must kick the load in Init()
	a2 := newApp(tk, "hub")
	if a2.hubSample == 0 {
		a2.hubSample = 10
	}
	cmd := a2.Init()
	if cmd == nil {
		t.Errorf("hub boot must return the auto-load command from Init")
	}
}
