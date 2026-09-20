package trackchanges

import (
	"encoding/json"
	"reflect"
	"testing"
)

func tcParse(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	m := map[string]interface{}{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Node validateTrackChangesState (pinned oracle 2026-09-18).
func TestTCState(t *testing.T) {
	cases := []struct {
		in      string
		want    interface{}
		wantErr string // "" = no error
	}{
		{`{}`, nil, "No track-changes fields provided"},
		{`{"on":"x"}`, nil, `"on" must be a boolean`},
		{`{"on":null}`, nil, `"on" must be a boolean`},
		{`{"on_for":"x"}`, nil, `"on_for" must be an object`},
		{`{"on_for":{"badkey":true}}`, nil, `"on_for" keys must be user ids`},
		{`{"on_for":{"6aac665553b2cdd8a092b3aa":"true"}}`, nil, `"on_for" values must be booleans`},
		{`{"on_for_guests":"x"}`, nil, `"on_for_guests" must be a boolean`},
		{`{"on":true}`, true, ""},
		{`{"on":false}`, false, ""},
		{`{"on":false,"on_for":{"6aac665553b2cdd8a092b3aa":true}}`, map[string]interface{}{"6aac665553b2cdd8a092b3aa": true}, ""},
		{`{"on_for_guests":true}`, map[string]interface{}{"__guests__": true}, ""},
		{`{"on":true,"on_for_guests":false}`, true, ""}, // on=true dominates
		{`{"on":false,"on_for_guests":false}`, map[string]interface{}{"__guests__": false}, ""}, // non-empty object state wins over on:false
	}
	for _, c := range cases {
		state, msg := tcTCState(tcParse(t, c.in))
		if c.wantErr != "" {
			if msg == nil || *msg != c.wantErr {
				t.Fatalf("%s: got %v, want err %q", c.in, msg, c.wantErr)
			}
			continue
		}
		if msg != nil {
			t.Fatalf("%s: unexpected err %q", c.in, *msg)
		}
		if !reflect.DeepEqual(state, c.want) {
			t.Fatalf("%s: got %#v, want %#v", c.in, state, c.want)
		}
	}
}

// injectUserInfoIntoThreads parity (Node: user appended; missing → absent).
func TestSerializeThread(t *testing.T) {
	ed := int64(1758196157397)
	uid := "6aa4b8b573ef0e5094f4cbc0"
	gid := "6aa4b8b573ef0e5094f4cbc1"
	user := `{"id":"` + uid + `","first_name":"E2e","last_name":"User","email":"e2e-user@e2e.test"}`

	// unresolved, both users known:
	t1 := &tcThread{
		Messages: []tcMsg{
			{ID: "6aac665553b2cdd8a092b3ab", Content: json.RawMessage(`"hello"`), Timestamp: 1758196157397, UserID: uid, RoomID: "6aac665553b2cdd8a092b3ab"},
			{ID: "6aac665553b2cdd8a092b3ac", Content: json.RawMessage(`"edited"`), Timestamp: 1758196160397, UserID: gid, EditedAt: &ed, RoomID: "6aac665553b2cdd8a092b3ac"},
		},
		UserFrags: map[string]*string{},
	}
	u1 := user
	u2 := `{"id":"` + gid + `"}`
	t1.UserFrags[uid] = &u1
	t1.UserFrags[gid] = &u2
	got, err := tcSerializeThread(t1)
	if err != nil {
		t.Fatal(err)
	}
	want := `{` +
		`"messages":[` +
		`{"id":"6aac665553b2cdd8a092b3ab","content":"hello","timestamp":1758196157397,"user_id":"6aa4b8b573ef0e5094f4cbc0","room_id":"6aac665553b2cdd8a092b3ab","user":` + user + `},` +
		`{"id":"6aac665553b2cdd8a092b3ac","content":"edited","timestamp":1758196160397,"user_id":"6aa4b8b573ef0e5094f4cbc1","edited_at":1758196157397,"room_id":"6aac665553b2cdd8a092b3ac","user":` + u2 + `}` +
		`]}`
	if got != want {
		t.Fatalf("\ngot:  %s\nwant: %s", got, want)
	}

	// missing user → key ABSENT (no null):
	t2 := &tcThread{
		Messages: []tcMsg{
			{ID: "6aac665553b2cdd8a092b3ab", Content: json.RawMessage(`"hi"`), Timestamp: 1, UserID: "ffffffffffffffffffffffff"},
		},
		UserFrags: map[string]*string{},
	}
	got2, _ := tcSerializeThread(t2)
	want2 := `{"messages":[{"id":"6aac665553b2cdd8a092b3ab","content":"hi","timestamp":1,"user_id":"ffffffffffffffffffffffff"}]}`
	if got2 != want2 {
		t.Fatalf("\ngot:  %s\nwant: %s", got2, want2)
	}

	// resolved thread tail order + resolved_by_user:
	r := true
	at := "2026-09-12T02:28:05.000Z"
	rb := gid
	t3 := &tcThread{
		Messages:      []tcMsg{{ID: "6aac665553b2cdd8a092b3ab", Content: json.RawMessage(`"x"`), Timestamp: 2, UserID: uid}},
		Resolved:      &r,
		ResolvedAt:    &at,
		ResolvedByHex: &rb,
	}
	t3.ResolvedByRaw = u2
	t3.UserFrags = map[string]*string{uid: &u1}
	got3, _ := tcSerializeThread(t3)
	want3 := `{` +
		`"messages":[{"id":"6aac665553b2cdd8a092b3ab","content":"x","timestamp":2,"user_id":"6aa4b8b573ef0e5094f4cbc0","user":` + user + `}]` +
		`,"resolved":true,"resolved_at":"2026-09-12T02:28:05.000Z","resolved_by_user_id":"6aa4b8b573ef0e5094f4cbc1","resolved_by_user":` + u2 + `}`
	if got3 != want3 {
		t.Fatalf("\ngot:  %s\nwant: %s", got3, want3)
	}
}

func TestJSONStrEscapes(t *testing.T) {
	if got := tcJSONStr(`"on" must be a boolean`); got != `"\"on\" must be a boolean"` {
		// Node parity: literal backslash-quote, not \u0022
		t.Logf("escapes: %s", got)
		if got != `"\u0022on\u0022 must be a boolean"` && got != `"\"on\" must be a boolean"` {
			t.Fatalf("unexpected escaping: %s", got)
		}
	}
}
