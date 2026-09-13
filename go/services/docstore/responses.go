package docstore

// responses.go — exact response writers, each verified against the running
// Node service:
//
//   - expressSendText: res.status(code).send(string) →
//     text/html; charset=utf-8 (live: 200 "docstore is alive", DELETE 500,
//     413 "document body too large", 500 "Oops, something went wrong").
//   - expressSendStatus: res.sendStatus(code) → Express status text,
//     text/plain; charset=utf-8 (live: 404 "Not Found"); 204 sends no body.
//   - expressFallback: router fallback page for unknown path/method → 404
//     HTML "Cannot <METHOD> <path>" (live: 176/146 byte bodies), with the
//     two security headers Express adds on these pages.

import (
	"io"
	"net/http"
	"strings"
)

func expressSendText(w http.ResponseWriter, code int, text string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, text)
}

func expressSendStatus(w http.ResponseWriter, code int) {
	if code == http.StatusNoContent || code == http.StatusNotModified {
		w.WriteHeader(code)
		return
	}
	text := http.StatusText(code)
	if text == "" {
		text = ""
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, text)
}

func expressFallback(w http.ResponseWriter, method, path string) {
	msg := "Cannot " + method + " " + path
	msg = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(msg)
	page := `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Error</title>
</head>
<body>
<h1>` + msg + `</h1>
</body>
</html>
`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, page)
}
