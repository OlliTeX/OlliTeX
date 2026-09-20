package orcidpicker

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ---------- fetchWorks ----------

type orcidWork struct {
	title   string
	year    string
	typeS   string
	doi     string // "" means the JSON value is null
	hasDoi  bool
	putCode string // raw JSON
	hasPC   bool
}

const (
	errNullSummary = `Cannot read properties of null (reading 'title')`
	errUndSummary  = `Cannot read properties of undefined (reading 'title')`
)

func fetchWorks(ctx context.Context, orcid string) (string, error) {
	rawURL := orcidPubAPI + "/" + encU(strings.TrimSpace(orcid)) + "/works"
	top, err := fetchJson(ctx, rawURL)
	if err != nil {
		return "", err
	}

	works := []orcidWork{}
	raw, present, isNull := jsGet(top, "group")
	if present && !isNull && !isJSFalsyRaw(raw) {
		groups, ok := asArray(raw)
		if !ok {
			return "", orcidErr{msg: errGroupIt}
		}
		for _, g := range groups {
			if strings.TrimSpace(string(g)) == "null" {
				return "", orcidErr{msg: `Cannot read properties of null (reading 'work-summary')`}
			}
			gobj, ok := asObject(g)
			if !ok {
				continue // primitive group: work-summary undefined → length 0 → skip
			}
			raw, p, n := jsGet(gobj, "work-summary")
			if !p || n || isJSFalsyRaw(raw) {
				continue // (group['work-summary'] || []).length === 0
			}
			summaries, isArr := asArray(raw)
			if !isArr {
				// Node: summaries.length undefined → s = summaries[0] →
				// undefined → s.title throws.
				return "", orcidErr{msg: errUndSummary}
			}
			if len(summaries) == 0 {
				continue
			}
			sRaw := summaries[0]
			if strings.TrimSpace(string(sRaw)) == "null" {
				return "", orcidErr{msg: errNullSummary}
			}
			s, ok := asObject(sRaw)
			if !ok {
				// primitive summary: s.title → undefined → '' etc.; putCode
				// dropped from JSON.
				works = append(works, orcidWork{title: "", year: "", typeS: "", putCode: "", hasPC: false})
				continue
			}

			w := orcidWork{}
			if t, ok := jsChain(s, "title", "title", "value"); ok && t != "" {
				w.title = t
			}
			if y, ok := jsChain(s, "publication-date", "year", "value"); ok && y != "" {
				w.year = y
			}
			if ty, p2, n2 := jsGet(s, "type"); p2 && !n2 && !isJSFalsyRaw(json.RawMessage(ty)) {
				if ts, sok := jsStringOf(ty); sok {
					w.typeS = ts
				}
			}
			if pc, p3, n3 := jsGet(s, "put-code"); p3 && !n3 {
				w.putCode = string(pc)
				w.hasPC = !isJSFalsyRaw(pc)
			}
			// doi: external-ids.external-id[] first {type:'doi', value}
			doi, derr := workDoi(s)
			if derr != nil {
				return "", derr
			}
			w.doi = doi
			w.hasDoi = doi != ""
			works = append(works, w)
		}
	}

	// Node: works.sort((a, b) => (parseInt(b.year,10)||0) - (parseInt(a.year,10)||0))
	sort.SliceStable(works, func(i, j int) bool {
		ai, aj := jsIntPart(works[i].year), jsIntPart(works[j].year)
		return ai > aj // Node yb-ya: larger year sorts first (descending)
	})

	// Serialize: {"title":...,"year":...,"type":...,"doi":...,"putCode":...}
	parts := make([]string, len(works))
	for i, w := range works {
		var b strings.Builder
		b.WriteString(`{"title":"` + jsString(w.title) + `","year":"` + jsString(w.year) + `","type":"` + jsString(w.typeS) + `","doi":`)
		if w.hasDoi {
			b.WriteString(`"` + jsString(w.doi) + `"`)
		} else {
			b.WriteString("null")
		}
		if w.hasPC {
			b.WriteString(`,"putCode":` + w.putCode)
		}
		b.WriteString("}")
		parts[i] = b.String()
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}

// workDoi — (work['external-ids']?.['external-id'] || []).find(
//
//	eid => eid['external-id-type'] === 'doi' && eid['external-id-value'])
//
// → value string, or "" (null). Non-array external-id → Node for..of TypeError.
func workDoi(s map[string]json.RawMessage) (string, error) {
	ei, p, n := jsGet(s, "external-ids")
	if !p || n || isJSFalsyRaw(ei) {
		return "", nil
	}
	eio, ok := asObject(ei)
	if !ok {
		return "", nil // external-ids?.external-id → undefined → || [] → []
	}
	raw, p2, n2 := jsGet(eio, "external-id")
	if !p2 || n2 || isJSFalsyRaw(raw) {
		return "", nil
	}
	arr, ok := asArray(raw)
	if !ok {
		return "", orcidErr{msg: errExtIt}
	}
	for _, e := range arr {
		eo, ok := asObject(e)
		if !ok {
			continue // eid type read → undefined → !== 'doi'
		}
		typ, p3, n3 := jsGet(eo, "external-id-type")
		if !p3 || n3 || isJSFalsyRaw(typ) {
			continue
		}
		if ts, sok := jsStringOf(typ); sok && ts == "doi" {
			if v, p4, n4 := jsGet(eo, "external-id-value"); p4 && !n4 {
				if s4, sok := jsStringOf(v); sok && s4 != "" {
					return s4, nil
				}
			}
		}
	}
	return "", nil
}

// ---------- bibtex ----------

var typeMap = map[string]string{
	"journal-article":  "article",
	"conference-paper": "inproceedings",
	"book":             "book",
	"book-chapter":     "incollection",
	"dissertation":     "phdthesis",
	"report":           "techreport",
	"edited-book":      "book",
}

func fetchBibtexFromOrcid(ctx context.Context, orcid, putCode string) (string, error) {
	rawURL := orcidPubAPI + "/" + encU(strings.TrimSpace(orcid)) + "/work/" + encU(putCode)
	work, err := fetchJson(ctx, rawURL)
	if err != nil {
		return "", err
	}

	// 1) ORCID-provided BibTeX citation.
	if cRaw, p, n := jsGet(work, "citation"); p && !n && !isJSFalsyRaw(cRaw) {
		if c, ok := asObject(cRaw); ok {
			if t, p2, n2 := jsGet(c, "citation-type"); p2 && !n2 {
				if ts, sok := jsStringOf(t); sok && ts == "bibtex" {
					if v, p3, n3 := jsGet(c, "citation-value"); p3 && !n3 {
						if vs, sok := jsStringOf(v); sok {
							if strings.TrimSpace(vs) != "" && strings.HasPrefix(strings.TrimSpace(vs), "@") {
								return strings.TrimSpace(vs), nil
							}
						}
					}
				}
			}
		}
	}

	// 2) DOI-resolved BibTeX via doi.org.
	doi, derr := workDoi(work)
	if derr != nil {
		return "", derr
	}
	if doi != "" && doiRe.MatchString(doi) {
		if text, ferr := fetchBibtexFromDoiUrl(ctx, doi); ferr == nil {
			t := strings.TrimSpace(text)
			if strings.HasPrefix(t, "@") {
				return t, nil
			}
		}
		// Node: warn + fall through (never a failure).
	}

	// 3) Best-effort reconstruction from the ORCID work record.
	return buildBibtexFromOrcidWork(work, doi), nil
}

func fetchBibtexFromDoiUrl(ctx context.Context, doi string) (string, error) {
	// Node: `${DOI_BASE}/${encodeURI(doi)}` (encodeURI, not
	// encodeURIComponent — reserved chars stay raw).
	url := doiBase + "/" + encURI(doi)
	return safeFetch(ctx, url, "application/x-bibtex, text/x-bibtex, text/bibliography; style=bibtex")
}

// buildBibtexFromOrcidWork — Node template (whitespace-exact):
//
//	@<type>{<key>,
//	  [author = {<authors>},]
//	  title = {<title>},
//	  [year = {<year>},]
//	  [journal = {<journal>},]
//	  [doi = {<doi>},]
//	}
func buildBibtexFromOrcidWork(work map[string]json.RawMessage, doi string) string {
	title := "Untitled"
	if t, ok := jsChain(work, "title", "title", "value"); ok && t != "" {
		title = t
	}
	year := ""
	if y, ok := jsChain(work, "publication-date", "year", "value"); ok {
		year = y
	}
	journal := ""
	if j, ok := jsChain(work, "journal-title", "value"); ok {
		journal = j
	}
	wtype := "misc"
	if ty, p, n := jsGet(work, "type"); p && !n && !isJSFalsyRaw(ty) {
		if ts, sok := jsStringOf(ty); sok {
			wtype = ts
		}
	}
	bibType := typeMap[wtype]
	if bibType == "" {
		bibType = "misc"
	}

	cRaw, p, n := jsGet(work, "contributors")
	contribs := []json.RawMessage{}
	if p && !n && !isJSFalsyRaw(cRaw) {
		if co, ok := asObject(cRaw); ok {
			r, p2, n2 := jsGet(co, "contributor")
			if p2 && !n2 && !isJSFalsyRaw(r) {
				if arr, ok2 := asArray(r); ok2 {
					contribs = arr
				} else {
					// Node: contributors.map on a non-array → TypeError.
					panic("contributors not iterable") // unreachable on ORCID payloads
				}
			}
		}
	}

	authors := []string{}
	for _, c := range contribs {
		e, ok := asObject(c)
		if !ok {
			continue
		}
		if v, ok2 := jsChain(e, "credit-name", "value"); ok2 && v != "" {
			authors = append(authors, v)
		}
	}
	authorsStr := strings.Join(authors, " and ")

	firstAuthor := "unknown"
	if len(contribs) > 0 {
		if e, ok := asObject(contribs[0]); ok {
			if v, ok2 := jsChain(e, "credit-name", "value"); ok2 && v != "" {
				firstAuthor = v
			}
		}
	}
	fields := strings.Fields(firstAuthor)
	surname := ""
	if len(fields) > 0 {
		// Node: last whitespace-separated word, non-letter chars stripped.
		surname = strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				return r
			}
			return -1
		}, fields[len(fields)-1])
	}
	key := strings.ToLower(surname + yearDefault(year))

	var b strings.Builder
	b.WriteString(fmt.Sprintf("@%s{%s,\n", bibType, key))
	if authorsStr != "" {
		b.WriteString("  author = {" + authorsStr + "},\n")
	}
	b.WriteString("  title = {" + title + "},\n")
	if year != "" {
		b.WriteString("  year = {" + year + "},\n")
	}
	if journal != "" {
		b.WriteString("  journal = {" + journal + "},\n")
	}
	if doi != "" {
		b.WriteString("  doi = {" + doi + "},\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func yearDefault(y string) string {
	if y == "" {
		return "nd"
	}
	return y
}
