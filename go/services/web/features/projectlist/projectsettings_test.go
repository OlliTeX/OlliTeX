package projectlist

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettingsRouteRegistered(t *testing.T) {
	f := Feature(nil)
	found := false
	for _, r := range f.Routes {
		if r.Method == "POST" && r.Pattern != nil && r.Pattern.MatchString("/project/6ab1/settings") && !strings.Contains(r.Pattern.String(), "settings/admin") {
			found = true
		}
	}
	if !found {
		t.Error("POST /project/:id/settings not registered")
	}
}

func TestSettingsBodyStrictness(t *testing.T) {
	// unknown keys must be rejected (Node z.strictObject)
	if _, ok := settingsFields["bogus"]; ok {
		t.Error("bogus key unexpectedly allowed")
	}
	// allowed keys
	for k := range map[string]bool{
		"compiler": true, "imageName": true, "png2pdf": true, "name": true,
		"spellCheckLanguage": true, "mainBibliographyDocId": true, "rootDocId": true,
		"referenceFormat": true, "grammarPicky": true,
	} {
		if _, ok := settingsFields[k]; !ok {
			t.Errorf("expected settings field %q", k)
		}
	}
	// wire shape of the 400 error matches the Node validation envelope
	va := settingsVa{"Unrecognized key: bogus", "body.bogus", 400}
	b := va.bytes()
	var env map[string]any
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatalf("error envelope not JSON: %v", err)
	}
	if env["statusCode"] != float64(400) {
		t.Errorf("statusCode = %v", env["statusCode"])
	}
	_ = httptest.NewRecorder
	_ = http.Header{}
	_ = io.Discard
}
