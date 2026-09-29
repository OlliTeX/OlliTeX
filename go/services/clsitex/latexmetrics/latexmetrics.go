// Package latexmetrics ports services/clsi/app/js/LatexMetrics.js (408L).
//
// Node parity notes:
//
//   - Node mutates a plain `stats` object. In Go `stats` and `timings` are
//     both `map[string]any` (consistent with the rest of the port, e.g.
//     outputcachemanager which writes `stats["pdf-size"] = ...`).
//   - `stats.latexmk` is, in Node, a NON-enumerable property holding the
//     detailed metrics; it is deliberately NOT serialized into the compile
//     response (so it never reaches web analytics). Go has no non-enumerable
//     maps, so it is stored as the plain key `stats["latexmk"]` (a
//     `map[string]any`) and OMITTED from the compile-response payload by the
//     (not-yet-ported) response builder via `ResponseStats`. See the doc on
//     `LatexMk`.
//   - Each metric parser replicates Node's `if (match)` truthiness: JS falsy
//     = null, 0, "", false, NaN. So e.g. `latexmk-clock-time` of 0 is NOT
//     stored, and an empty signature is NOT stored.
//
// Ported functions: EnableLatexMkMetrics, AddLatexMkMetrics, AddLatexFdbMetrics.
// Internal helpers (parseFdbContent / summarizeFileTypes / convertToArray /
// getFileTypeCategory / normalizeImageFilename) are mirrored faithfully.
package latexmetrics

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RuleTime mirrors each `latexmk-rule-times` entry {rule, time_ms}.
type RuleTime struct {
	Rule   string  `json:"rule"`
	TimeMS float64 `json:"time_ms"`
}

// ImgTime mirrors each `latexmk-img-times` entry {type, count, time_ms}.
type ImgTime struct {
	Type   string  `json:"type"`
	Count  int     `json:"count"`
	TimeMS float64 `json:"time_ms"`
}

// LatexMk returns the (detail) sub-map stored under stats["latexmk"], creating
// it if absent (Go standing-in for Node's non-enumerable `latexmk` property).
// See the package doc for the serialization caveat.
func LatexMk(stats map[string]any) map[string]any {
	if lm, ok := stats["latexmk"].(map[string]any); ok {
		return lm
	}
	m := map[string]any{}
	stats["latexmk"] = m
	return m
}

// LatexMkKeys returns the metric names present in the latexmk detail object.
func LatexMkKeys(stats map[string]any) []string {
	lm := LatexMk(stats)
	out := make([]string, 0, len(lm))
	for k := range lm {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ResponseStats mirrors the JS semantics that the `latexmk` property is
// non-enumerable: it returns a copy of `stats` WITHOUT the `latexmk` key, for
// use when building the compile response sent to web. (Node: JSON.stringify
// omits the non-enumerable prop.)
func ResponseStats(stats map[string]any) map[string]any {
	out := make(map[string]any, len(stats))
	for k, v := range stats {
		if k == "latexmk" {
			continue
		}
		out[k] = v
	}
	return out
}

// --- node-side regexes (JS flags m/g mapped to Go RE2 (?m) + FindAll over the
// --- whole string, which is equivalent to matchAll.

var (
	ruleTimesRe      = regexp.MustCompile(`(?m)^'([^' ]+).*': time = ([0-9]+\.[0-9]+)`)
	processingTimeRe = regexp.MustCompile(`(?m)^Processing time = ([0-9]+\.[0-9]+), of which invoked processes = ([0-9]+\.[0-9]+), other = ([0-9]+\.[0-9]+)`)
	accumTimeRe      = regexp.MustCompile(`(?m)^Accumulated processing time = ([0-9]+\.[0-9]+)`)
	clockTimeRe      = regexp.MustCompile(`(?m)^Elapsed clock time = ([0-9]+\.[0-9]+)`)
	rulesRunRe       = regexp.MustCompile(`(?m)^Number of rules run = ([0-9]+)`)
	pngCopyRe        = regexp.MustCompile(`(?m)^PNG copy: (.*)$`)
	pngCopySkippedRe = regexp.MustCompile(`(?m)^PNG copy skipped \((alpha|gamma|palette|interlaced|other)\): (.*)$`)
	imageWrittenRe   = regexp.MustCompile(`(?m)^Image written \((PNG|JPG|JBIG2|PDF), ([0-9]+) ms\): (.*)$`)
	fdbFileLineRe    = regexp.MustCompile(`(?m)^\s*"([^"]+)"\s+\d+(?:\.\d*)?\s+([0-9]+)`)
)

// NOTEWORTHY_DEPENDENCIES_RE mirrors /\/(beamer\.cls|tikz\.sty|microtype\.sty|minted\.sty)$/.
// (The leading `/` is a literal: group 1 is the filename after the last slash.)
var NOTEWORTHY_DEPENDENCIES_RE = regexp.MustCompile(`/(beamer\.cls|tikz\.sty|microtype\.sty|minted\.sty)$`)

var extRe = regexp.MustCompile(`\.([a-zA-Z]{1,4})$`)

// extname mirrors node:path extname (the last ".ext" suffix, lowercased is
// the caller's choice; here we return it as-is and lower in the caller).
func extname(p string) string {
	i := len(p) - 1
	if i < 0 || p == "" {
		return ""
	}
	lastSlash := strings.LastIndexByte(p, '/')
	start := lastSlash + 1
	for i >= start && i >= 0 {
		if p[i] == '.' {
			if i == start {
				// leading "." is not an extension
				return ""
			}
			return p[i:]
		}
		i--
	}
	return ""
}

func convertToMs(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return math.Floor(v * 1000)
}

func jsTruthyNumber(v float64) bool { return v != 0 && !math.IsNaN(v) }

func normalizeImageFilename(filename string) string {
	if strings.HasPrefix(filename, "/compile/") {
		filename = strings.TrimPrefix(filename, "/compile/")
	}
	if strings.HasPrefix(filename, "./") {
		filename = strings.TrimPrefix(filename, "./")
	}
	return filename
}

// parseRuleTimes extracts per-rule latexmk times (each line: 'cmd ...': time = N).
func parseRuleTimes(stdout string) []RuleTime {
	out := []RuleTime{}
	matches := ruleTimesRe.FindAllStringSubmatch(stdout, -1)
	for _, m := range matches {
		out = append(out, RuleTime{Rule: m[1], TimeMS: convertToMs(m[2])})
	}
	return out
}

// parseLatexMkTime extracts the total/invoked/other (or fallback total) times.
// Returns the value to store (a map, matching Node's object shape) and ok.
func parseLatexMkTime(stdout string) (v any, ok bool) {
	if m := processingTimeRe.FindStringSubmatch(stdout); m != nil {
		return map[string]any{
			"total":   convertToMs(m[1]),
			"invoked": convertToMs(m[2]),
			"other":   convertToMs(m[3]),
		}, true
	}
	if m := accumTimeRe.FindStringSubmatch(stdout); m != nil {
		return map[string]any{"total": convertToMs(m[1])}, true
	}
	return nil, false
}

// --- STDOUT metric pipeline (fixed order: rule-times, signature, time, clock,
// --- rules-run). Mirrors LATEX_MK_METRICS_STDOUT.
func runStdoutMetrics(stdout string, lm map[string]any) {
	// 1. latexmk-rule-times (Node stores even an empty array — [] is truthy).
	rt := parseRuleTimes(stdout)
	lm["latexmk-rule-times"] = rt
	// 2. latexmk-rule-signature (string; omitted when empty).
	if len(rt) > 0 {
		rules := make([]string, len(rt))
		for i, t := range rt {
			rules[i] = t.Rule
		}
		sig := strings.Join(rules, ",")
		if sig != "" {
			lm["latexmk-rule-signature"] = sig
		}
	}
	// 3. latexmk-time (object or null; omitted when no match).
	if v, ok := parseLatexMkTime(stdout); ok {
		lm["latexmk-time"] = v
	}
	// 4. latexmk-clock-time (number; omitted when 0/absent).
	if m := clockTimeRe.FindStringSubmatch(stdout); m != nil {
		ms := convertToMs(m[1])
		if jsTruthyNumber(ms) {
			lm["latexmk-clock-time"] = ms
		}
	}
	// 5. latexmk-rules-run (int; omitted when 0/absent).
	if m := rulesRunRe.FindStringSubmatch(stdout); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n != 0 {
			lm["latexmk-rules-run"] = n
		}
	} else if rt, has := lm["latexmk-rule-times"].([]RuleTime); has && len(rt) > 0 {
		lm["latexmk-rules-run"] = len(rt)
	}
}

func parsePNGSlow(stderr string) []string {
	files := map[string]bool{}
	order := []string{}
	for _, m := range pngCopySkippedRe.FindAllStringSubmatch(stderr, -1) {
		f := normalizeImageFilename(m[2])
		if !files[f] {
			files[f] = true
			order = append(order, f)
		}
	}
	return order
}

func parseImgTimes(stderr string) []ImgTime {
	// category per file (fast-copy default, overwritten by skip category)
	catByFile := map[string]string{}
	for _, m := range pngCopyRe.FindAllStringSubmatch(stderr, -1) {
		catByFile[m[1]] = "fast-copy"
	}
	for _, m := range pngCopySkippedRe.FindAllStringSubmatch(stderr, -1) {
		catByFile[m[2]] = m[1]
	}
	typeOrder := []string{}
	timeByType := map[string]float64{}
	countByType := map[string]int{}
	for _, m := range imageWrittenRe.FindAllStringSubmatch(stderr, -1) {
		t := m[1]
		ms, _ := strconv.Atoi(m[2])
		file := m[3]
		ext := strings.ToLower(extname(file))
		if t == "PDF" && ext == ".png" {
			t = "PNG-png2pdf"
		} else if t == "PNG" {
			if c, ok := catByFile[file]; ok {
				t = "PNG-" + c
			}
		}
		if _, exists := timeByType[t]; !exists {
			typeOrder = append(typeOrder, t)
		}
		timeByType[t] += float64(ms)
		countByType[t]++
	}
	out := []ImgTime{}
	for _, t := range typeOrder {
		out = append(out, ImgTime{Type: t, Count: countByType[t], TimeMS: timeByType[t]})
	}
	return out
}

// AddLatexMkMetrics mirrors addLatexMkMetrics(output, stats, timings). Node's
// `output` has {stdout, stderr}; we take both strings (nil-safe on the caller
// side). Mutates `stats` (top-level image counters) and `stats["latexmk"]`.
// `timings` is updated for the image-include timings when non-nil.
func AddLatexMkMetrics(stdout, stderr string, stats, timings map[string]any) {
	lm := LatexMk(stats)
	runStdoutMetrics(stdout, lm)
	// STDERR
	if files := parsePNGSlow(stderr); len(files) > 0 {
		lm["latexmk-png-slow"] = files
	}
	lm["latexmk-img-times"] = parseImgTimes(stderr)

	if img, ok := lm["latexmk-img-times"].([]ImgTime); ok {
		totalIncludeCount, totalIncludeTime := 0, 0.0
		optCount, optTime := 0, 0.0
		for _, it := range img {
			totalIncludeCount += int(it.Count)
			totalIncludeTime += it.TimeMS
			if it.Type == "PNG-png2pdf" {
				optCount += int(it.Count)
				optTime += it.TimeMS
			}
		}
		stats["include-image-all"] = totalIncludeCount
		stats["include-image-optimised"] = optCount
		if timings != nil {
			timings["include-image-optimised"] = optTime
			timings["include-image-all"] = totalIncludeTime
		}
	}
}

// EnableLatexMkMetrics mirrors Node's `enableLatexMkMetrics` (sets the
// non-enumerable `latexmk` prop). In Go: initialize the detail sub-map.
func EnableLatexMkMetrics(stats map[string]any) {
	_ = LatexMk(stats)
}

type fileTypeAgg struct {
	count int
	size  int
}

func getFileTypeCategory(ext string) string {
	switch ext {
	case "png", "jpg", "jpeg", "tif", "tiff", "bmp", "gif", "svg", "eps":
		return "image"
	case "tex", "sty", "cls", "bib":
		return "text"
	case "ttf", "otf", "pfb":
		return "font"
	default:
		return "other"
	}
}

type fdbResult struct {
	systemFileTypes map[string]fileTypeAgg
	userFileTypes   map[string]fileTypeAgg
	dependencies    []string
}

func parseFdbContent(fdbContent string) *fdbResult {
	systemFileTypes := map[string]fileTypeAgg{}
	userFileTypes := map[string]fileTypeAgg{}
	seen := map[string]bool{}
	depSet := map[string]bool{}
	dependencies := []string{}

	for _, m := range fdbFileLineRe.FindAllStringSubmatch(fdbContent, -1) {
		filePath := m[1]
		if strings.HasPrefix(filePath, "/compile/") {
			filePath = strings.TrimPrefix(filePath, "/compile/")
		}
		if seen[filePath] {
			continue
		}
		fileSize, _ := strconv.Atoi(m[2])
		isSystemFile := strings.HasPrefix(filePath, "/")
		ext := "other"
		if em := extRe.FindStringSubmatch(filePath); em != nil {
			ext = strings.ToLower(em[1])
		}
		fileTypes := userFileTypes
		if isSystemFile {
			fileTypes = systemFileTypes
		}
		if _, ok := fileTypes[ext]; !ok {
			fileTypes[ext] = fileTypeAgg{}
		}
		agg := fileTypes[ext]
		agg.count++
		agg.size += fileSize
		fileTypes[ext] = agg

		if dm := NOTEWORTHY_DEPENDENCIES_RE.FindStringSubmatch(filePath); dm != nil {
			depName := dm[1]
			if !depSet[depName] {
				depSet[depName] = true
				dependencies = append(dependencies, depName)
			}
		}

		seen[filePath] = true
	}

	return &fdbResult{systemFileTypes, userFileTypes, dependencies}
}

type catAgg struct {
	count int
	size  int
}

func summarizeFileTypes(fileTypes map[string]fileTypeAgg) map[string]catAgg {
	summary := map[string]catAgg{
		"image": {}, "text": {}, "font": {}, "other": {}, "total": {},
	}
	for ext, info := range fileTypes {
		category := getFileTypeCategory(ext)
		cs := summary[category]
		cs.count += info.count
		cs.size += info.size
		summary[category] = cs
		ts := summary["total"]
		ts.count += info.count
		ts.size += info.size
		summary["total"] = ts
	}
	return summary
}

type extSort struct {
	Ext   string `json:"ext"`
	Count int    `json:"count"`
	Size  int    `json:"size"`
}

func convertToArray(object map[string]fileTypeAgg) []extSort {
	out := make([]extSort, 0, len(object))
	for ext, v := range object {
		out = append(out, extSort{Ext: ext, Count: v.count, Size: v.size})
	}
	// sort by size descending (Node: b.size - a.size). For equal sizes, Node
	// leaves insertion order; Go sort.Slice is not stable, so to make the port
	// deterministic we tie-break by ascending ext (does not affect any
	// consumer that reads size/count only).
	sort.Slice(out, func(i, j int) bool {
		if out[i].Size != out[j].Size {
			return out[i].Size > out[j].Size
		}
		return out[i].Ext < out[j].Ext
	})
	return out
}

// AddLatexFdbMetrics mirrors addLatexFdbMetrics(fdbContent, stats). It is a
// no-op when fdbContent is empty/nil.
func AddLatexFdbMetrics(fdbContent string, stats map[string]any) {
	if fdbContent == "" {
		return
	}
	lm := LatexMk(stats)
	res := parseFdbContent(fdbContent)

	if len(res.systemFileTypes) > 0 || len(res.userFileTypes) > 0 {
		userSummary := summarizeFileTypes(res.userFileTypes)
		systemSummary := summarizeFileTypes(res.systemFileTypes)
		lm["fdb-file-types"] = map[string]any{
			"total": map[string]any{
				"systemFileCount": systemSummary["total"].count,
				"systemFileSize":  systemSummary["total"].size,
				"imageFileCount":  userSummary["image"].count,
				"imageFileSize":   userSummary["image"].size,
				"textFileCount":   userSummary["text"].count,
				"textFileSize":    userSummary["text"].size,
				"fontFileCount":   userSummary["font"].count,
				"fontFileSize":    userSummary["font"].size,
				"otherFileCount":  userSummary["other"].count,
				"otherFileSize":   userSummary["other"].size,
			},
			"system": convertToArray(res.systemFileTypes),
			"user":   convertToArray(res.userFileTypes),
		}
	}

	if len(res.dependencies) > 0 {
		lm["fdb-dependencies"] = res.dependencies
	}
}
