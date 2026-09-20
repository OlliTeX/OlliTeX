package texfmt

import (
	"encoding/json"
	"os"
	"testing"
)

type corpusCase struct {
	Name  string
	Src   string
	Out   string
	Throw string
}

func loadCorpus(t *testing.T) []corpusCase {
	t.Helper()
	vars := []string{"GOTIDY_CORPUS", "../../../../../tests/e2e/fixtures/gotidy_corpus.json", "/tmp/gotidy_corpus.json"}
	p := ""
	for _, v := range vars {
		if v == "" {
			continue
		}
		if b, err := os.ReadFile(v); err == nil {
			p = v
			_ = string(b)
			data, _ := os.ReadFile(v)
			var cases []corpusCase
			if json.Unmarshal(data, &cases) == nil && len(cases) > 0 {
				return cases
			}
			_ = p
			break
		}
	}
	t.Skip("gotidy_corpus.json not found (set GOTIDY_CORPUS)")
	return nil
}

// TestFormatBib_Corpus pins byte-parity against the live Node bibtex-tidy
// corpus (bibtex-tidy@1.15.1, controller option set). Throw cases must
// error (Node controller -> 500 Formatting failed).
func TestFormatBib_Corpus(t *testing.T) {
	for _, c := range loadCorpus(t) {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			got, err := FormatBib(c.Src)
			if c.Throw != "" {
				if err == nil {
					t.Fatalf("expected throw, got output %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v\nwant: %q", err, c.Out)
			}
			if got != c.Out {
				t.Fatalf("output mismatch:\n got: %q\nwant: %q", got, c.Out)
			}
		})
	}
}

// TestFormatBib_Shapes covers the controller-level contract (non-corpus).
func TestFormatBib_Shapes(t *testing.T) {
	// empty -> "\n" (corpus also pins this; keep as fast unit guard)
	if got, err := FormatBib(""); err != nil || got != "\n" {
		t.Fatalf("empty input: got %q err=%v", got, err)
	}
	if got, err := FormatBib("   \n  \n\t\n"); err != nil || got != "\n" {
		t.Fatalf("whitespace input: got %q err=%v", got, err)
	}
}
