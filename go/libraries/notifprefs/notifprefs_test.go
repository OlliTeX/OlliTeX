package notifprefs

// Notification preferences parity tests. Oracle:
//
//  1. libraries/notification-preferences/index.js (the contract itself).
//  2. services/web/modules/notifications/test/unit/src/
//     NotificationsPreferencesHandler.test.mjs (the Node test suite that
//     pins normalizeGlobalDelayMinutes / normalizeGlobalPreferences).
//  3. Live probes of JS Number() semantics on Node 22 (hex, bool,
//     whitespace, Infinity, "0x").

import (
	"math"
	"testing"
)

func TestProjectPreferenceKeys(t *testing.T) {
	want := []string{
		"commentOnOwnProject",
		"commentOnInvitedProject",
		"repliesOnAuthoredThread",
		"repliesOnParticipatingThread",
		"commentResolvedOnAuthoredThread",
		"commentResolvedOnParticipatingThread",
		"commentReopenedOnAuthoredThread",
		"commentReopenedOnParticipatingThread",
		"trackedChangesOnOwnProject",
		"trackedChangesOnInvitedProject",
		"trackChangesAcceptedOnAuthoredChange",
		"trackChangesRejectedOnAuthoredChange",
	}
	if len(ProjectPreferenceKeys) != len(want) {
		t.Fatalf("keys: got %d, want %d", len(ProjectPreferenceKeys), len(want))
	}
	for i := range want {
		if ProjectPreferenceKeys[i] != want[i] {
			t.Fatalf("key[%d] = %q, want %q", i, ProjectPreferenceKeys[i], want[i])
		}
	}
}

func TestDefaultProjectPreferences(t *testing.T) {
	d := DefaultProjectPreferences()
	if len(d) != 12 {
		t.Fatalf("defaults: %d keys, want 12", len(d))
	}
	for k, v := range d {
		if !v {
			t.Fatalf("default %q must be true", k)
		}
	}
}

func TestNormalizeProjectPreferences(t *testing.T) {
	// All default when nothing given.
	if got := NormalizeProjectPreferences(nil); len(got) != 12 {
		t.Fatalf("nil prefs: %v", got)
	}
	for k, v := range NormalizeProjectPreferences(map[string]any{}) {
		if !v {
			t.Fatalf("missing key %q must default to true (got %v)", k, v)
		}
	}
	// Present-but-falsy → false (a real OFF).
	falsy := NormalizeProjectPreferences(map[string]any{
		"commentOnOwnProject":          nil,
		"commentOnInvitedProject":      false,
		"repliesOnAuthoredThread":      0,
		"repliesOnParticipatingThread": "",
	})
	if falsy["commentOnOwnProject"] || falsy["commentOnInvitedProject"] ||
		falsy["repliesOnAuthoredThread"] || falsy["repliesOnParticipatingThread"] {
		t.Fatalf("falsy values must normalize false: %v", falsy)
	}
	if !falsy["trackedChangesOnOwnProject"] {
		t.Fatalf("unmentioned key must stay true: %v", falsy)
	}
	// Truthy non-bools stay true (JS Boolean(1) === true).
	if got := NormalizeProjectPreferences(map[string]any{"commentOnOwnProject": 1}); !got["commentOnOwnProject"] {
		t.Fatalf("Boolean(1) must be true: %v", got)
	}
}

func TestNormalizeGlobalPreferences(t *testing.T) {
	// Node test: valid '5' → 5; 10080 → 10080.
	gp := NormalizeGlobalPreferences(map[string]any{"notificationDelayMinutes": "5"})
	if gp.NotificationDelayMinutes == nil || *gp.NotificationDelayMinutes != 5 {
		t.Fatalf("delay '5' = %v, want 5", gp.NotificationDelayMinutes)
	}
	gp = NormalizeGlobalPreferences(map[string]any{"notificationDelayMinutes": 10080})
	if gp.NotificationDelayMinutes == nil || *gp.NotificationDelayMinutes != 10080 {
		t.Fatalf("delay 10080 = %v, want 10080", gp.NotificationDelayMinutes)
	}
	// Node test: missing / '' / 'abc' / -1 / 0.5 / 10081 → null.
	for _, in := range []any{nil, "", "abc", -1, 0.5, 10081} {
		if got := NormalizeGlobalPreferences(map[string]any{"notificationDelayMinutes": in}).NotificationDelayMinutes; got != nil {
			t.Fatalf("delay %v must be null, got %v", in, got)
		}
	}
	// muteAllNotifications: absent → false; false → false; 1 → true.
	if got := NormalizeGlobalPreferences(nil); got.MuteAllNotifications {
		t.Fatalf("mute absent must be false")
	}
	if got := NormalizeGlobalPreferences(map[string]any{"muteAllNotifications": false}); got.MuteAllNotifications {
		t.Fatalf("mute false must be false")
	}
	if got := NormalizeGlobalPreferences(map[string]any{"muteAllNotifications": 1}); !got.MuteAllNotifications {
		t.Fatalf("Boolean(1) must be true")
	}
}

func TestNormalizeGlobalDelayMinutes(t *testing.T) {
	// Node test pins (NotificationsPreferencesHandler.test.mjs).
	if got := NormalizeGlobalDelayMinutes(30); got == nil || *got != 30 {
		t.Fatalf("30 = %v, want 30", got)
	}
	if got := NormalizeGlobalDelayMinutes("30"); got == nil || *got != 30 {
		t.Fatalf("'30' = %v, want 30", got)
	}
	if got := NormalizeGlobalDelayMinutes(nil); got != nil {
		t.Fatalf("nil must be null")
	}
	if got := NormalizeGlobalDelayMinutes(100000); got != nil {
		t.Fatalf("100000 must be null")
	}
	// JS Number() semantics.
	if got := NormalizeGlobalDelayMinutes(true); got == nil || *got != 1 {
		t.Fatalf("true → Number(true)=1 → 1, got %v", got)
	}
	if got := NormalizeGlobalDelayMinutes(false); got != nil {
		t.Fatalf("false → Number(false)=0 → null, got %v", got)
	}
	if got := NormalizeGlobalDelayMinutes(" 42 "); got == nil || *got != 42 {
		t.Fatalf("' 42 ' → 42, got %v", got)
	}
	if got := NormalizeGlobalDelayMinutes("1e1"); got == nil || *got != 10 {
		t.Fatalf("'1e1' → 10, got %v", got)
	}
	if got := NormalizeGlobalDelayMinutes("0x10"); got == nil || *got != 16 {
		t.Fatalf("'0x10' → 16 (JS Number hex), got %v", got)
	}
	if got := NormalizeGlobalDelayMinutes("0x"); got != nil {
		t.Fatalf("'0x' → NaN → null, got %v", got)
	}
	if got := NormalizeGlobalDelayMinutes("12.5"); got != nil {
		t.Fatalf("'12.5' non-integer → null, got %v", got)
	}
	if got := NormalizeGlobalDelayMinutes(math.Inf(1)); got != nil {
		t.Fatalf("Infinity → null, got %v", got)
	}
	// Boundary values.
	if got := NormalizeGlobalDelayMinutes(1); got == nil || *got != 1 {
		t.Fatalf("1 = %v, want 1", got)
	}
	if got := NormalizeGlobalDelayMinutes(10080); got == nil || *got != 10080 {
		t.Fatalf("10080 = %v, want 10080", got)
	}
}
