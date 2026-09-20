// Small shared helpers for the webdav package (P6.9).

package webdav

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"
)

type jsonRaw = map[string]json.RawMessage

func jsonUnmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// wdCredStr — string field from a credentials JSON body (absent/"") → "".
func wdCredStr(m map[string]json.RawMessage, key string) (string, bool) {
	r, ok := m[key]
	if !ok {
		return "", false
	}
	var s *string
	if err := json.Unmarshal(r, &s); err != nil || s == nil {
		return "", false
	}
	return *s, true
}

func parseInt64(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// httpTimeISO — WebDAV getlastmodified (RFC 1123) → ISO (Node
// new Date(lastmod).toISOString()).
func httpTimeISO(s string) string {
	if t, err := time.Parse("Mon, 02 Jan 2006 15:04:05 GMT", s); err == nil {
		return t.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	if t, err := time.Parse(time.RFC1123Z, s); err == nil {
		return t.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	return s
}

func basename(p string) string {
	p = strings.TrimSuffix(p, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}

func joinOnce(parent, name string) string {
	p := strings.TrimSuffix(parent, "/")
	if p == "" {
		p = "/"
	}
	return p + "/" + name
}

func ioReadAll(r io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, limit))
}

var propfindDepth0 = []byte(`<?xml version="1.0" encoding="utf-8"?><d:prop xmlns:d="DAV:"><d:resourcetype/><d:displayname/><d:getcontentlength/><d:getlastmodified/><d:getetag/></d:prop>`)

var propfindDepth1 = []byte(`<?xml version="1.0" encoding="utf-8"?><d:prop xmlns:d="DAV:"><d:resourcetype/><d:displayname/><d:getcontentlength/><d:getlastmodified/><d:getetag/></d:prop>`)
