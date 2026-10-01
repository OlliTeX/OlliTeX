package wakatime

import "testing"

// audit 035 — the entity→language mapping (Wakapi/WakaTime contract).
func TestWakaLanguageForEntity(t *testing.T) {
	cases := map[string]string{
		"main.tex":           "LaTeX",
		"refs.bib":           "BibTeX",
		"doc.typ":            "Typst",
		"README.md":          "Markdown",
		"app.js":             "JavaScript",
		"a.ts":               "TypeScript",
		"s.py":               "Python",
		"main.go":            "Go",
		"run.sh":             "Shell",
		"cfg.yaml":           "YAML",
		"x.json":             "JSON",
		"t.txt":              "Other",
		"noext":              "Other",
		"folder/sub/doc.tex": "LaTeX",
	}
	for in, want := range cases {
		if got := wakaLanguageForEntity(in); got != want {
			t.Errorf("wakaLanguageForEntity(%q) = %q, want %q", in, got, want)
		}
	}
}
