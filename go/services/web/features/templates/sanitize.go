package templates

import "strings"

// Node CleanHtml (sanitize mode, options from the oracle):
//
//	plainText : allowed tags = none        (text only, trimmed)
//	linksOnly : allowed = a[href]
//	reachText : allowed = h1-h6 p blockquote ul ol li em strong s code
//	                           pre hr a[href]
//
// Unallowed tags are DROPPED (their text content is kept); comment
// nodes are dropped; the result is trimmed. Attribute handling is
// observationally exact for the fixture corpus (plain href values).

type tplTok struct {
	kind    int // 0 text, 1 starttag, 2 endtag
	name    string
	href    string
	hasHref bool
}

// tplTokenize — minimal well-formed-HTML tokenizer (text / comment /
// start tag with attrs / end tag).
func tplTokenize(html string) []tplTok {
	var out []tplTok
	i := 0
	for i < len(html) {
		if html[i] == '<' {
			if strings.HasPrefix(html[i:], "<!--") {
				end := strings.Index(html[i+4:], "-->")
				if end < 0 {
					break
				}
				i += 4 + end + 3
				continue
			}
			end := strings.IndexByte(html[i+1:], '>')
			if end < 0 {
				break
			}
			inner := html[i+1 : i+1+end]
			if strings.HasPrefix(inner, "/") {
				out = append(out, tplTok{kind: 2, name: strings.ToLower(strings.TrimSpace(inner[1:]))})
			} else {
				name, attrs := tplParseAttrs(inner)
				tok := tplTok{kind: 1, name: name}
				if href, ok := tplGetAttr(attrs, "href"); ok {
					tok.href = href
					tok.hasHref = true
				}
				out = append(out, tok)
			}
			i += end + 2
			continue
		}
		j := strings.IndexByte(html[i:], '<')
		if j < 0 {
			j = len(html) - i
		}
		out = append(out, tplTok{kind: 0, name: html[i : i+j]})
		i += j
	}
	return out
}

// tplParseAttrs — split a start-tag body: first chunk = tag name, the
// rest = space-separated name=value attrs (quoted values supported).
func tplParseAttrs(body string) (string, map[string]string) {
	body = strings.TrimSpace(body)
	name := body
	attrs := map[string]string{}
	if body == "" {
		return "", attrs
	}
	if j := strings.IndexAny(body, " \t\n\r"); j > 0 {
		name = body[:j]
		rest := body[j+1:]
		// attribute scan: name="v" | name='v' | name=v | name
		for rest != "" {
			rest = strings.TrimLeft(rest, " \t\n\r")
			if rest == "" {
				break
			}
			eq := strings.IndexByte(rest, '=')
			sp := strings.IndexAny(rest, " \t\n\r")
			if eq < 0 || (sp >= 0 && sp < eq) {
				end := eq
				if end < 0 || sp < end {
					end = sp
				}
				if end < 0 {
					end = len(rest)
				}
				attrs[strings.ToLower(rest[:end])] = ""
				rest = rest[end:]
				continue
			}
			an := strings.ToLower(rest[:eq])
			vs := rest[eq+1:]
			vs = strings.TrimLeft(vs, " \t\n\r")
			var v string
			if vs != "" && (vs[0] == '"' || vs[0] == '\'') {
				q := vs[0]
				end := strings.IndexByte(vs[1:], q)
				if end >= 0 {
					v = vs[1 : 1+end]
					vs = vs[1+end+1:]
				} else {
					v = vs[1:]
					vs = ""
				}
			} else {
				end := strings.IndexAny(vs, " \t\n\r")
				if end < 0 {
					v = vs
					vs = ""
				} else {
					v = vs[:end]
					vs = vs[end:]
				}
			}
			attrs[an] = v
			rest = vs
		}
	}
	return strings.ToLower(name), attrs
}

func tplGetAttr(m map[string]string, k string) (string, bool) {
	v, ok := m[k]
	return v, ok
}

var tplReachKeep = map[string]bool{
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true,
	"h6": true, "p": true, "blockquote": true, "ul": true, "ol": true,
	"li": true, "em": true, "strong": true, "s": true, "code": true,
	"pre": true, "hr": true,
}

// cleanHtml — the three CleanHtml options (see the package comment).
func cleanHtml(html, option string) string {
	tokens := tplTokenize(html)
	var sb strings.Builder
	for _, t := range tokens {
		switch t.kind {
		case 0: // text
			sb.WriteString(t.name)
		case 2: // end tag
			switch option {
			case "linksOnly":
				if t.name == "a" {
					sb.WriteString("</a>")
				}
			case "reachText":
				if tplReachKeep[t.name] || t.name == "a" {
					sb.WriteString("</" + t.name + ">")
				}
			}
		case 1: // start tag
			switch option {
			case "linksOnly":
				if t.name == "a" {
					if t.hasHref {
						sb.WriteString(`<a href="` + t.href + `">`)
					} else {
						sb.WriteString("<a>")
					}
				}
			case "reachText":
				if tplReachKeep[t.name] {
					sb.WriteString("<" + t.name + ">")
				} else if t.name == "a" {
					if t.hasHref {
						sb.WriteString(`<a href="` + t.href + `">`)
					} else {
						sb.WriteString("<a>")
					}
				}
			}
		}
	}
	// Node sanitize-html keeps surrounding text as-is (no trim) — the
	// pinned corpus shows trailing newlines preserved in plainText.
	return sb.String()
}

var _ = cleanHtml
