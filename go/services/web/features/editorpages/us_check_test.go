package editorpages

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestBuildUserSettingsP61Fixtures(t *testing.T) {
	user := map[string]any{
		"signUpDate": primitive.DateTime(1789391285099), // 2026-09-12 (>= cutoff)
		"ace": map[string]any{
			"zotero":   map[string]any{"enabled": true, "disablePersonalLibrary": false, "groups": []any{}},
			"mendeley": map[string]any{"enabled": true, "disablePersonalLibrary": false, "groups": []any{}},
			"papers":   map[string]any{"enabled": true, "disablePersonalLibrary": false, "groups": []any{}},
			"mode":     "none", "theme": "textmate", "lightTheme": "textmate", "darkTheme": "overleaf_dark",
			"fontSize": int32(12), "autoComplete": true, "autoPairDelimiters": true,
			"pdfViewer": "pdfjs", "previewTabs": false, "mathPreview": true, "breadcrumbs": false,
			"editorTabs": true, "nonBlinkingCursor": false, "referencesSearchMode": "advanced",
			"darkModePdf": false, "floatingMenu": true, "customKeybindings": map[string]any{},
		},
	}
	want := `{"mode":"none","editorTheme":"textmate","editorLightTheme":"textmate","editorDarkTheme":"overleaf_dark","fontSize":12,"autoComplete":true,"autoPairDelimiters":true,"pdfViewer":"pdfjs","previewTabs":false,"fontFamily":"lucida","lineHeight":"normal","overallTheme":"system","mathPreview":true,"breadcrumbs":false,"editorTabs":true,"nonBlinkingCursor":false,"referencesSearchMode":"advanced","darkModePdf":false,"floatingMenu":true,"customKeybindings":{},"zotero":{"enabled":true,"disablePersonalLibrary":false,"groups":[]},"mendeley":{"enabled":true,"disablePersonalLibrary":false,"groups":[]},"papers":{"enabled":true,"disablePersonalLibrary":false,"groups":[]}}`
	got := BuildUserSettings(user)
	if got != want {
		t.Fatalf("member mismatch:\ngot  %s\nwant %s", got, want)
	}
	admin := map[string]any{
		"ace": map[string]any{
			"zotero":   map[string]any{"enabled": true, "disablePersonalLibrary": false, "groups": []any{}},
			"mendeley": map[string]any{"enabled": true, "disablePersonalLibrary": false, "groups": []any{}},
			"papers":   map[string]any{"enabled": true, "disablePersonalLibrary": false, "groups": []any{}},
			"mode":     "none", "theme": "textmate", "lightTheme": "textmate", "darkTheme": "overleaf_dark",
			"fontSize": int32(12), "autoComplete": true, "autoPairDelimiters": true,
			"pdfViewer": "pdfjs", "syntaxValidation": true, "previewTabs": false, "mathPreview": true,
			"breadcrumbs": false, "editorTabs": true, "nonBlinkingCursor": false,
			"referencesSearchMode": "advanced", "darkModePdf": false, "floatingMenu": true,
			"customKeybindings": map[string]any{}, "overallTheme": "light-",
		},
	}
	wantA := `{"mode":"none","editorTheme":"textmate","editorLightTheme":"textmate","editorDarkTheme":"overleaf_dark","fontSize":12,"autoComplete":true,"autoPairDelimiters":true,"pdfViewer":"pdfjs","syntaxValidation":true,"previewTabs":false,"fontFamily":"lucida","lineHeight":"normal","overallTheme":"light-","mathPreview":true,"breadcrumbs":false,"editorTabs":true,"nonBlinkingCursor":false,"referencesSearchMode":"advanced","darkModePdf":false,"floatingMenu":true,"customKeybindings":{},"zotero":{"enabled":true,"disablePersonalLibrary":false,"groups":[]},"mendeley":{"enabled":true,"disablePersonalLibrary":false,"groups":[]},"papers":{"enabled":true,"disablePersonalLibrary":false,"groups":[]}}`
	gotA := BuildUserSettings(admin)
	if gotA != wantA {
		t.Fatalf("admin mismatch:\ngot  %s\nwant %s", gotA, wantA)
	}
}
