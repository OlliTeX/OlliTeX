package instancestats

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestComputeCutoff(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 30, 45, 0, time.UTC)
	expect := func(window string, retention int, want string) {
		t.Helper()
		got := computeCutoff(window, now, retention).UTC().Format(time.RFC3339)
		if got != want {
			t.Fatalf("cutoff(%s)=%s want %s", window, got, want)
		}
	}
	expect("day", 365, "2026-09-13T00:00:00Z")
	expect("week", 365, "2026-09-07T00:00:00Z")
	expect("month", 365, "2026-08-15T00:00:00Z")
	expect("6m", 365, "2026-03-18T00:00:00Z")
	expect("year", 365, "2025-09-14T00:00:00Z")
	expect("all", 365, "2025-09-14T00:00:00Z")
	expect("all", 30, "2026-08-15T00:00:00Z")

	// non-UTC now → the cutoff instant is now-1day in absolute time, then
	// floored to UTC midnight (Node: Date math + toUtcMidnight).
	nowLocal := time.Date(2026, 9, 14, 23, 0, 0, 0, time.FixedZone("UTC+9", 9*3600)) // = 14:00Z
	got := computeCutoff("day", nowLocal, 365).UTC().Format(time.RFC3339)
	want := "2026-09-13T00:00:00Z" // 2026-09-13T14:00Z floored
	if got != want {
		t.Fatalf("local now cutoff=%s want %s", got, want)
	}
}

func TestValidators(t *testing.T) {
	for _, k := range STAT_KEYS {
		if !isValidMetric(k) {
			t.Fatalf("metric %q should be valid", k)
		}
	}
	for _, bad := range []string{"", "bogus", "active_projects2"} {
		if isValidMetric(bad) {
			t.Fatalf("metric %q should be invalid", bad)
		}
	}
	for _, w := range []string{"day", "week", "month", "6m", "year", "all"} {
		if !validateWindow(w) {
			t.Fatalf("window %q should be valid", w)
		}
	}
	for _, w := range []string{"", "bogus", "10y"} {
		if validateWindow(w) {
			t.Fatalf("window %q should be invalid", w)
		}
	}
}

func TestValidEmail(t *testing.T) {
	for _, ok := range []string{
		"a@b.co",
		"first.last@sub.domain.io",
		"x@y.z",
		"weird+tag@exa_mple-dash.co",
	} {
		if !validEmail(ok) {
			t.Fatalf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{
		"",
		"no-at",
		"@b.co",
		"a@",
		"a@.co",
		"a@b.",
		"a b@c.co", // space (JS \s)
		"a@b .co",
		"\ta@b.co",
	} {
		if validEmail(bad) {
			t.Fatalf("%q should be invalid", bad)
		}
	}
}

func TestNormalizeEmails(t *testing.T) {
	emails, err := normalizeEmails(map[string]any{
		"alertEmails": []any{"a@x.co", "b@y.io", "a@x.co", "  "},
	})
	if err != "" {
		t.Fatalf("err: %s", err)
	}
	if len(emails) != 2 || emails[0] != "a@x.co" || emails[1] != "b@y.io" {
		t.Fatalf("got %v", emails)
	}

	emails, err = normalizeEmails(map[string]any{"alertEmails": "p@q.co r@s.io"})
	if err != "" || len(emails) != 2 || emails[0] != "p@q.co" || emails[1] != "r@s.io" {
		t.Fatalf("string split: %v %q", emails, err)
	}

	emails, err = normalizeEmails(map[string]any{"alertEmail": "legacy@z.tv"})
	if err != "" || len(emails) != 1 || emails[0] != "legacy@z.tv" {
		t.Fatalf("legacy: %v %q", emails, err)
	}

	_, err = normalizeEmails(map[string]any{"alertEmails": []any{"good@x.co", "bad"}})
	if err != "Invalid email address: bad" {
		t.Fatalf("expected bad-email err, got %q", err)
	}

	emails, err = normalizeEmails(map[string]any{"alertEmails": []any{}})
	if err != "" || len(emails) != 0 {
		t.Fatalf("empty: %v %q", emails, err)
	}
}

func TestParseAlertConfigBody(t *testing.T) {
	m, err := parseAlertConfigBody(map[string]any{
		"alertEmails":        []any{"a@x.co", "b@y.io"},
		"diskWarningPercent": float64(55),
		"ramWarningPercent":  float64(66),
	})
	if err != "" {
		t.Fatalf("err: %s", err)
	}
	if m["alertEmail"] != "a@x.co" || m["diskWarningPercent"] != float64(55) || m["ramWarningPercent"] != float64(66) {
		t.Fatalf("shape: %+v", m)
	}
	if _, ok := m["alertEmails"].([]string); !ok {
		t.Fatalf("alertEmails type: %+v", m["alertEmails"])
	}

	// error order: emails first, then disk, then ram
	if _, e := parseAlertConfigBody(map[string]any{"alertEmails": []any{"bad"}, "diskWarningPercent": float64(50), "ramWarningPercent": float64(50)}); e != "Invalid email address: bad" {
		t.Fatalf("email-first order: %q", e)
	}
	if _, e := parseAlertConfigBody(map[string]any{"diskWarningPercent": "50", "ramWarningPercent": float64(50)}); e != "diskWarningPercent must be a number between 1 and 100" {
		t.Fatalf("disk-err: %q", e)
	}
	if _, e := parseAlertConfigBody(map[string]any{"diskWarningPercent": float64(50), "ramWarningPercent": float64(0)}); e != "ramWarningPercent must be a number between 1 and 100" {
		t.Fatalf("ram-err: %q", e)
	}
	if _, e := parseAlertConfigBody(map[string]any{}); e != "diskWarningPercent must be a number between 1 and 100" {
		t.Fatalf("missing disk first: %q", e)
	}
	if _, e := parseAlertConfigBody(map[string]any{"diskWarningPercent": float64(101)}); e != "diskWarningPercent must be a number between 1 and 100" {
		t.Fatalf("disk range: %q", e)
	}
	// empty emails are legal (alerts disabled)
	if m, e := parseAlertConfigBody(map[string]any{"diskWarningPercent": float64(10), "ramWarningPercent": float64(20)}); e != "" || m["alertEmail"] != "" {
		t.Fatalf("empty emails: %v %q", m, e)
	}
}

func TestNormalizeValues(t *testing.T) {
	got := normalizeValues([]any{int64(1), float64(2), float64(2.5)})
	b, _ := json.Marshal(got)
	if string(b) != `[1,2,2.5]` {
		t.Fatalf("values: %s", b)
	}
	if b2, _ := json.Marshal(normalizeValues(nil)); string(b2) != `[]` {
		t.Fatalf("nil: %s", b2)
	}
}

func TestStatKeyOrderStable(t *testing.T) {
	// membership only, but keep the pinned set exact
	joined := strings.Join(STAT_KEYS, "|")
	want := "active_projects|active_users|new_users|shared_projects|user_count|project_count|file_count|mongodb_storage|overleaf_storage|redis_storage|disk_usage|cpu_load|ram_usage"
	if joined != want {
		t.Fatalf("STAT_KEYS drifted: %s", joined)
	}
}
