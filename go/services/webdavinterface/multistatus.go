package webdavinterface

import (
	"bytes"
	"encoding/xml"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

// --- minimal DAV multistatus parse ------------------------------------------

type davMultistatus struct {
	XMLName   xml.Name      `xml:"multistatus"`
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href      string        `xml:"href"`
	PropStats []davPropstat `xml:"propstat"`
}

type davPropstat struct {
	XMLName xml.Name `xml:"propstat"`
	Prop    davProp  `xml:"prop"`
}

type davProp struct {
	Displayname      string      `xml:"displayname"`
	Getetag          string      `xml:"getetag"`
	Getlastmodified  string      `xml:"getlastmodified"`
	Getcontentlength string      `xml:"getcontentlength"`
	Resourcetype     *davResType `xml:"resourcetype"`
}

type davResType struct {
	Collection *struct{} `xml:"collection"`
}

func parseMultistatus(raw []byte) []WebDAVEntry {
	var ms davMultistatus
	if err := xmlUnmarshalFlexible(raw, &ms); err != nil {
		return nil
	}
	out := make([]WebDAVEntry, 0, len(ms.Responses))
	for _, r := range ms.Responses {
		e := WebDAVEntry{Href: baseName(r.Href)}
		for _, ps := range r.PropStats {
			p := ps.Prop
			if p.Displayname != "" {
				e.Href = p.Displayname
			}
			if p.Getetag != "" {
				e.Etag = p.Getetag
			}
			if p.Getlastmodified != "" {
				if t, terr := parseHTTPDate(p.Getlastmodified); terr == nil {
					e.ModifiedAt = t.UTC().Format(time.RFC3339)
				}
			}
			if p.Getcontentlength != "" {
				if n, serr := strconv.ParseInt(strings.TrimSpace(p.Getcontentlength), 10, 64); serr == nil {
					e.Size = n
				}
			}
			if p.Resourcetype != nil && p.Resourcetype.Collection != nil {
				e.IsDirectory = true
			}
		}
		out = append(out, e)
	}
	return out
}

func parseHTTPDate(s string) (time.Time, error) {
	if t, err := http.ParseTime(s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

func baseName(href string) string {
	p := uPath(href)
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return ""
	}
	return path.Base(p)
}

func xmlUnmarshalFlexible(b []byte, v interface{}) error {
	return xmlUnmarshal(b, v)
}

func xmlUnmarshal(b []byte, v interface{}) error {
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = false
	return dec.Decode(v)
}
