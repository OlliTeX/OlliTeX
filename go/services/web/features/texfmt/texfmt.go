// Package texfmt is the Go parity implementation of the Node
// services/web/modules/tex-autoformatter module (WEB_GO_PLAN P6.17):
//
//	POST /api/format-tex  (requireLogin)
//	  body {content: string, filename?: string}
//	    content missing/non-string          -> 400 {"error":"content must be a string"}
//	    content length (UTF-16 units) > 5MB -> 400 {"error":"content too large"}
//	    .bib (last dot-ext, case-insens.)  -> bibtex normalizer (bibtex.go)
//	    anything else (incl. no filename)  -> tex-fmt --stdin (texfmt_exec.go)
//	    200 {"formatted": <string>}
//	    formatter throw / spawn fail       -> 500 {"error":"Formatting failed"}
//
// Invalid JSON body ({bad) -> 400 {} and no-csrf -> 403 come from the core
// full-parse + CSRF chain (P6.14), same as P6.15.
package texfmt

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"ollitex/go/services/web/core"
)

// MaxInputSize mirrors the Node constant 5 * 1024 * 1024 — a JS string
// length is UTF-16 code units (not runes, not bytes).
const MaxInputSize = 5 * 1024 * 1024

// texFmtTimeout mirrors the Node spawn option timeout: 10000.
const texFmtTimeout = 10 * time.Second

// Feature registers the P6.17 route.
func Feature(a *core.App) core.Feature {
	return core.Feature{
		Name: "texautoformatter",
		Routes: []core.Route{
			{Method: "POST", Path: "/api/format-tex", Handler: hFormatTex},
		},
	}
}

func hFormatTex(cxt *core.Cxt, res *core.Res) {
	raw := []byte(nil)
	if cxt.Req != nil && cxt.Req.Body != nil {
		raw, _ = io.ReadAll(io.LimitReader(cxt.Req.Body, 16<<20))
	}
	var body map[string]any
	if len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &body) // invalid JSON never reaches here (core full-parse 400s it)
	}
	if body == nil {
		body = map[string]any{}
	}
	content, ok := body["content"].(string)
	if !ok {
		res.JSON(400, []byte(`{"error":"content must be a string"}`))
		return
	}
	if utf16Len(content) > MaxInputSize {
		res.JSON(400, []byte(`{"error":"content too large"}`))
		return
	}
	ext := extension(body["filename"])
	var formatted string
	var err error
	if ext == ".bib" {
		formatted, err = FormatBib(content)
	} else {
		formatted, err = RunTexFmt(cxt.Req.Context(), content)
	}
	if err != nil {
		// Node: logger.error({err, filename}, 'formatting failed') then
		// res.status(500).json({error:"Formatting failed"}).
		res.JSON(500, []byte(`{"error":"Formatting failed"}`))
		return
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // Node JSON.stringify does not HTML-escape (& stays &)
	if err := enc.Encode(map[string]any{"formatted": formatted}); err != nil {
		res.JSON(500, []byte(`{"error":"Formatting failed"}`))
		return
	}
	b := bytes.TrimRight(buf.Bytes(), "\n\r") // Encoder appends a newline
	res.JSON(200, b)
}

// utf16Len is the JS String.length of s (UTF-16 code units).
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n++ // surrogate pair: two UTF-16 units
		}
		n++
	}
	return n
}

// extension mirrors Node getExtension: non-string -> "", last '.' + suffix
// lowercased, "" when no dot.
func extension(filename any) string {
	fs, ok := filename.(string)
	if !ok {
		return ""
	}
	dot := strings.LastIndex(fs, ".")
	if dot < 0 {
		return ""
	}
	return strings.ToLower(fs[dot:])
}

// RunTexFmt spawns `tex-fmt --stdin` with the content on stdin (Node:
// spawn('tex-fmt', ['--stdin'], stdio pipes, timeout 10000) and resolves
// Buffer.concat(stdout).toString('utf8'); any spawn error or non-zero close
// rejects -> the controller's 500).
func RunTexFmt(ctx context.Context, content string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, texFmtTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "tex-fmt", "--stdin")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = strings.NewReader(content)
	if runtime.GOOS != "windows" {
		// keep Node's env contract: PATH is inherited by the process.
	}
	if err := cmd.Run(); err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			return "", context.DeadlineExceeded // Node: timeout kill -> close != 0 -> reject
		}
		return "", err // Node: proc 'error' (ENOENT) or close code != 0 -> reject
	}
	_ = stderr
	return stdout.String(), nil
}
