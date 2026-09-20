package commandrunner

import "testing"

type stub struct{}

func (stub) Run(string, []string, string, string, int64, map[string]string, string, string, func(error, *RunOutput)) string {
	return ""
}
func (stub) Kill(string, func(error))       {}
func (stub) CanRunSyncTeXInOutputDir() bool { return false }

func TestNewRejectsNonSandboxed(t *testing.T) {
	if _, err := New(false, stub{}, nil); err == nil {
		t.Fatal("expected error when dockerRunner flag is false")
	}
	if got, err := New(true, nil, nil); got != nil || err == nil {
		t.Fatalf("expected runner+err from New(true,nil); got %v %v", got, err)
	}
	r, err := New(true, stub{}, nil)
	if err != nil || r == nil {
		t.Fatalf("New(true, runner) got (%v,%v)", r, err)
	}
}

func TestNewLogsRefusalWhenNonSandboxed(t *testing.T) {
	var logged string
	loggerf := func(msg string, attrs map[string]any) { logged = msg }
	if _, err := New(false, stub{}, loggerf); err == nil {
		t.Fatal("expected error when dockerRunner flag is false")
	}
	if logged == "" {
		t.Fatalf("expected the refusal message to be logged, got empty string")
	}
	if !contains(logged, "refusing to start") {
		t.Errorf("refusal log %q missing the refusal message", logged)
	}
}

func TestNewLogsSelectionWhenSandboxed(t *testing.T) {
	var logged string
	loggerf := func(msg string, attrs map[string]any) { logged = msg }
	r, err := New(true, stub{}, loggerf)
	if err != nil || r == nil {
		t.Fatalf("New(true, runner) got (%v,%v)", r, err)
	}
	if !contains(logged, "commandrunner selected") {
		t.Errorf("selection log %q missing the selected message", logged)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
