// Package notifprefs is the 1:1 Go port of `libraries/notification-preferences`
// (npm `@overleaf/notification-preferences` v0.1.0): the shared contract for
// Overleaf notification preferences (the 12-key per-project schema plus the
// global mute/delay shape) used by both the web `notifications` module and
// the chat service, so the schema + normalization live in one place.
//
// Storage (single collection `notificationsPreferences`):
//
//	per project: { user_id, project_id, <ProjectPreferences> }
//	global:       { user_id, project_id: null, MuteAllNotifications,
//	               NotificationDelayMinutes }
package notifprefs

import (
	"math"
	"strconv"
	"strings"
)

// ProjectPreferenceKeys is the per-project preference key list (Node
// `PROJECT_PREFERENCE_KEYS`). Order does not matter; membership does.
var ProjectPreferenceKeys = []string{
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

// DefaultProjectPreferences mirrors `defaultProjectPreferences()`: all 12
// keys set to true.
func DefaultProjectPreferences() map[string]bool {
	out := make(map[string]bool, len(ProjectPreferenceKeys))
	for _, k := range ProjectPreferenceKeys {
		out[k] = true
	}
	return out
}

// NormalizeProjectPreferences mirrors `normalizeProjectPreferences(prefs = {})`:
// for each of the 12 keys the output is `true` when the input key is ABSENT
// (Node `=== undefined`), else the JS-truthiness of its value — so an
// EXPLICIT null, 0, "" or false all normalize to false (a present-but-falsy
// value is a real "off", distinct from a missing key).
func NormalizeProjectPreferences(prefs map[string]any) map[string]bool {
	out := make(map[string]bool, len(ProjectPreferenceKeys))
	for _, k := range ProjectPreferenceKeys {
		v, present := prefs[k]
		if !present {
			out[k] = true
			continue
		}
		out[k] = jsTruthy(v)
	}
	return out
}

// GlobalPreferences mirrors the `normalizeGlobalPreferences()` result shape.
type GlobalPreferences struct {
	MuteAllNotifications bool
	// NotificationDelayMinutes: nil means NOT SET (Node null → the server
	// default PROJECT_CHANGE_NOTIFICATION_MIN_DELAY_MS applies); otherwise
	// the user's whole-minute grace delay clamped to 1..7 days.
	NotificationDelayMinutes *int64
}

// NormalizeGlobalPreferences mirrors `normalizeGlobalPreferences(prefs = {})`.
func NormalizeGlobalPreferences(prefs map[string]any) GlobalPreferences {
	var gp GlobalPreferences
	if prefs != nil {
		if v, present := prefs["muteAllNotifications"]; present {
			gp.MuteAllNotifications = jsTruthy(v)
		}
		gp.NotificationDelayMinutes = NormalizeGlobalDelayMinutes(prefs["notificationDelayMinutes"])
	}
	return gp
}

// GlobalDelayMinutesMin / GlobalDelayMinutesMax are the clamped user delay
// range (Node GLOBAL_DELAY_MINUTES_MIN/_MAX; 10080 = 7 days of minutes).
const (
	GlobalDelayMinutesMin int64 = 1
	GlobalDelayMinutesMax int64 = 10080 // 7 days
)

// NormalizeGlobalDelayMinutes mirrors `normalizeGlobalDelayMinutes(value)`:
// returns nil (Node null = "not set → server default") for missing, null,
// empty, non-integer or out-of-range values; otherwise the whole-minute
// value clamped to [GlobalDelayMinutesMin, GlobalDelayMinutesMax].
//
// Value conversion follows JS `Number(value)`: numbers pass through; strings
// parse like a JS number literal (decimal, leading/trailing whitespace, hex
// "0x…" — all oracle-verified); booleans convert (true→1, false→0); anything
// unparseable (Node NaN) is invalid.
func NormalizeGlobalDelayMinutes(value any) *int64 {
	if value == nil {
		return nil
	}
	n, ok := jsNumber(value)
	if !ok {
		return nil
	}
	if n != math.Trunc(n) || n < float64(GlobalDelayMinutesMin) || n > float64(GlobalDelayMinutesMax) {
		return nil
	}
	m := int64(n)
	return &m
}

// jsNumber mirrors JS `Number(value)` for the value kinds this contract sees
// (JSON/BSON scalars). ok=false means NaN (invalid number).
func jsNumber(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return 0, false
		}
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int8:
		return float64(t), true
	case int16:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint8:
		return float64(t), true
	case uint16:
		return float64(t), true
	case uint32:
		return float64(t), true
	case uint64:
		return float64(t), true
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	case string:
		return jsNumberString(t)
	}
	return 0, false
}

// jsNumberString mirrors JS `Number(string)` (trimmed; decimal and hex
// literals; NaN → ok=false). Whitespace strings → 0 in JS, kept faithful.
func jsNumberString(s string) (float64, bool) {
	t := strings.TrimSpace(s)
	switch strings.ToLower(t) {
	case "":
		return 0, true // JS: Number("") === 0
	case "inf", "+inf", "-inf", "infinity", "+infinity", "-infinity":
		return 0, false // ±Infinity is not an integer → invalid anyway
	}
	if hex, ok := strings.CutPrefix(strings.ToLower(strings.TrimLeft(t, "+-")), "0x"); ok {
		if hex == "" {
			return 0, false // JS: Number("0x") === NaN
		}
		u, err := strconv.ParseUint(hex, 16, 53)
		if err != nil {
			// Beyond 2^53 JS would produce a finite float; it is way past the
			// [1, 10080] clamp, so "invalid" is equivalent here.
			return 0, false
		}
		return float64(u), true
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, false // NaN
	}
	return f, true
}

// jsTruthy mirrors JS boolean coercion for the value kinds the contract sees
// (0, "", false, NaN, null are falsy; everything else truthy).
func jsTruthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0 && !math.IsNaN(t)
	case float32:
		return t != 0 && !math.IsNaN(float64(t))
	case int:
		return t != 0
	case int8:
		return t != 0
	case int16:
		return t != 0
	case int32:
		return t != 0
	case int64:
		return t != 0
	case uint:
		return t != 0
	case uint8:
		return t != 0
	case uint16:
		return t != 0
	case uint32:
		return t != 0
	case uint64:
		return t != 0
	}
	return v != nil
}
