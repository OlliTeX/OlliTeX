package chatbridge

import (
	"testing"

	"ollitex/go/services/web/core"
)

func TestChatRoutes(t *testing.T) {
	f := Feature(&core.App{})
	if f.Name != "chatbridge" {
		t.Fatalf("feature name = %q", f.Name)
	}
	want := map[string]string{
		"GET /project/6ab1/messages":            "",
		"POST /project/6ab1/messages":           "",
		"DELETE /project/6ab1/messages/6ab2":    "",
		"POST /project/6ab1/messages/6ab2/edit": "",
	}
	seen := map[string]bool{}
	for _, r := range f.Routes {
		key := ""
		switch {
		case r.Method == "GET" && r.Pattern != nil && r.Pattern.MatchString("/project/6ab1/messages"):
			key = "GET /project/6ab1/messages"
		case r.Method == "POST" && r.Pattern != nil && r.Pattern.MatchString("/project/6ab1/messages") && !r.Pattern.MatchString("/project/6ab1/messages/6ab2/edit"):
			key = "POST /project/6ab1/messages"
		case r.Method == "DELETE" && r.Pattern != nil && r.Pattern.MatchString("/project/6ab1/messages/6ab2"):
			key = "DELETE /project/6ab1/messages/6ab2"
		case r.Method == "POST" && r.Pattern != nil && r.Pattern.MatchString("/project/6ab1/messages/6ab2/edit"):
			key = "POST /project/6ab1/messages/6ab2/edit"
		}
		if key != "" {
			seen[key] = true
		}
	}
	for k := range want {
		if !seen[k] {
			t.Errorf("chat route %q not registered (have %v)", k, seen)
		}
	}
}
