package editorpages

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestExposedSettingsJSONGHSync — WDV-F (owner capture 2026-10-03): the
// runtime githubSyncEnabled flip must emit VALID JSON. The earlier version
// appended a stray `"` after the boolean ("githubSyncEnabled":true"), which
// crashed every IDE/hub load with `SyntaxError: ... JSON at position 1413`
// (JSON.parse of the ol-ExposedSettings meta).
func TestExposedSettingsJSONGHSync(t *testing.T) {
	for _, want := range []bool{true, false} {
		s := ExposedSettingsJSON("http://x.test", true, false, false, false, false, want)
		var m map[string]any
		if err := json.Unmarshal([]byte(s), &m); err != nil {
			t.Fatalf("githubSync%v: invalid ol-ExposedSettings JSON: %v; tail=%q", want, err, tailN(s, 120))
		}
		if got := m["githubSyncEnabled"]; got != want {
			t.Fatalf("githubSyncEnabled = %v, want %v", got, want)
		}
		if strings.Contains(s, `true",`) || strings.Contains(s, `false",`) || strings.Contains(s, `true"}`) {
			t.Fatalf("stray quote after boolean in settings: %q", tailN(s, 120))
		}
	}
}

func tailN(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}
