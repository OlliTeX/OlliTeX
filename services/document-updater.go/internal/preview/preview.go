// Package preview — 1:1 port of `app/js/TrackedChangePreview.js`.
//
// Builds sparse previews of an accept/reject tracked-change batch. A batch
// can cover changes spread across the whole document, so the changes are
// first clustered by proximity: each cluster becomes one preview, with its
// own section path, start line and bounded slice of surrounding text. Web
// assembles the diff, before/after context and truncation from that.
//
// The clustering gap must stay in step with MERGE_MAX_GAP in
// services/web/modules/notifications/app/src/emails/changeBlock.mjs.
package preview

import (
	"math"
	"regexp"
	"sort"
)

// sectionHeaderPattern matches a LaTeX sectioning command. Captures:
//  1. command name (no backslash), 2. short title, 3. full title.
//
// Handles starred variants and one level of nested braces.
var sectionHeaderPattern = regexp.MustCompile(
	`^\s*\\(part|chapter|section|subsection|subsubsection|paragraph|subparagraph)\*?(?:\[([^\]]*)\])?\{((?:[^{}]|\{[^{}]*\})*)\}`)

// sectionLevels mirrors SECTION_LEVELS.
var sectionLevels = map[string]int{
	"part": 0, "chapter": 1, "section": 2, "subsection": 3,
	"subsubsection": 4, "paragraph": 5, "subparagraph": 6,
}

// contextPaddingChars — surrounding context per side (Node: 500).
const contextPaddingChars = 500

// mergeMaxGap — the maximum gap, in characters, between two changes that
// still read as one preview.
const mergeMaxGap = 50

// Op mirrors Node `Change` op: { i?, d?, p }.
type Op struct {
	I *string `json:"i,omitempty"`
	D *string `json:"d,omitempty"`
	P int     `json:"p"`
}

// Metadata mirrors Node change metadata: { user_id? }.
type Metadata struct {
	UserID string `json:"user_id,omitempty"`
}

// Change mirrors Node `Change`: { id?, op, metadata? }.
type Change struct {
	ID       *string   `json:"id,omitempty"`
	Op       Op        `json:"op"`
	Metadata *Metadata `json:"metadata,omitempty"`
}

// SparseChangePreview mirrors Node `SparseChangePreview`.
type SparseChangePreview struct {
	SectionPath []string `json:"sectionPath"`
	StartLine   int      `json:"startLine"`
	Changes     []Op     `json:"changes"`
	Slice       string   `json:"slice"`
	SliceStart  int      `json:"sliceStart"`
	UserIDs     []string `json:"userIds"`
}

// contentLengthOf is the length a change occupies in document-content
// coordinate space. Inserts are present in the document; deletes are
// zero-width (their text is no longer there, so it doesn't shift later
// offsets).
func contentLengthOf(op Op) int {
	if op.I != nil {
		return len(*op.I)
	}
	return 0
}

// buildLineStarts returns the character offset of the first character of
// each line, so position lookups don't re-walk the document.
func buildLineStarts(lines []string) []int {
	starts := make([]int, len(lines))
	offset := 0
	for i, line := range lines {
		starts[i] = offset
		offset += len(line) + 1 // +1 for the newline
	}
	return starts
}

// charPositionToLineIndex — binary search for the line a 0-indexed char
// position falls on. Mirrors Node's Math.ceil((low+high)/2) midpoint.
func charPositionToLineIndex(lineStarts []int, charPosition int) int {
	low := 0
	high := len(lineStarts) - 1
	for low < high {
		mid := (low + high + 1) / 2 // Math.ceil((low + high) / 2)
		if lineStarts[mid] > charPosition {
			high = mid - 1
		} else {
			low = mid
		}
	}
	return low
}

// collectSectionHeaders walks every line once, gathering every sectioning
// header (line index, level, title).
type sectionHeader struct {
	lineIndex int
	level     int
	title     string
}

func collectSectionHeaders(lines []string) []sectionHeader {
	headers := []sectionHeader{}
	for i, line := range lines {
		m := sectionHeaderPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		level, ok := sectionLevels[m[1]]
		if !ok {
			continue
		}
		title := m[2]
		if title == "" {
			title = m[3]
		}
		if title == "" {
			continue
		}
		headers = append(headers, sectionHeader{lineIndex: i, level: level, title: title})
	}
	return headers
}

// sectionPathAtLine walks the ancestors of the change line, returning the
// titles highest-level-first (e.g. ["Part", "Section", "Subsection"]).
//
// Mirrors Node: a header qualifies while `level < maxLevel` (starts at
// Infinity); the first (deepest) header that precedes the line starts the
// chain; iteration stops at a level-0 header.
func sectionPathAtLine(headers []sectionHeader, lineIndex int) []string {
	ancestors := []string{}
	maxLevel := math.Inf(1)
	for i := len(headers) - 1; i >= 0; i-- {
		h := headers[i]
		if h.lineIndex > lineIndex {
			continue
		}
		if float64(h.level) >= maxLevel {
			continue
		}
		ancestors = append(ancestors, h.title)
		maxLevel = float64(h.level)
		if h.level == 0 {
			break
		}
	}
	out := make([]string, 0, len(ancestors))
	for i := len(ancestors) - 1; i >= 0; i-- {
		out = append(out, ancestors[i])
	}
	return out
}

// clusterChanges groups changes that sit close enough together to read as
// one preview (position-ascending, gap <= mergeMaxGap).
func clusterChanges(changes []Change) [][]Change {
	sorted := make([]Change, len(changes))
	copy(sorted, changes)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Op.P < sorted[j].Op.P })
	clusters := [][]Change{{sorted[0]}}
	for i := 1; i < len(sorted); i++ {
		change := sorted[i]
		previous := clusters[len(clusters)-1][len(clusters[len(clusters)-1])-1]
		previousEnd := previous.Op.P + contentLengthOf(previous.Op)
		if change.Op.P-previousEnd <= mergeMaxGap {
			clusters[len(clusters)-1] = append(clusters[len(clusters)-1], change)
		} else {
			clusters = append(clusters, []Change{change})
		}
	}
	return clusters
}

// extractWindow returns `strings.Join(lines, "\n")[windowStart:windowEnd]`
// without materializing the full joined text.
func extractWindow(lines []string, lineStarts []int, windowStart, windowEnd int) string {
	var out string
	firstLine := charPositionToLineIndex(lineStarts, windowStart)
	for i := firstLine; i < len(lines); i++ {
		line := lines[i]
		lineStart := lineStarts[i]
		lineEnd := lineStart + len(line)
		if lineStart >= windowEnd {
			break
		}
		if lineEnd > windowStart {
			from := windowStart - lineStart
			if from < 0 {
				from = 0
			}
			to := windowEnd - lineStart
			if to > len(line) {
				to = len(line)
			}
			out += line[from:to]
		}
		if i < len(lines)-1 && lineEnd >= windowStart && lineEnd < windowEnd {
			out += "\n"
		}
	}
	return out
}

// BuildSparseChangePreviews builds the sparse change previews that web
// hydrates into stored previews, one per cluster of nearby changes.
//
//	Node: `buildSparseChangePreviews({ changes, lines })`.
func BuildSparseChangePreviews(changes []Change, lines []string) []SparseChangePreview {
	if len(changes) == 0 || len(lines) == 0 {
		return []SparseChangePreview{}
	}
	lineStarts := buildLineStarts(lines)
	headers := collectSectionHeaders(lines)

	previews := []SparseChangePreview{}
	for _, cluster := range clusterChanges(changes) {
		firstPos := cluster[0].Op.P
		lastChange := cluster[len(cluster)-1]
		lastEnd := lastChange.Op.P + contentLengthOf(lastChange.Op)
		lineIndex := charPositionToLineIndex(lineStarts, firstPos)

		windowStart := firstPos - contextPaddingChars
		if windowStart < 0 {
			windowStart = 0
		}
		windowEnd := lastEnd + contextPaddingChars

		seen := map[string]bool{}
		userIDs := []string{}
		for _, change := range cluster {
			if change.Metadata != nil && change.Metadata.UserID != "" {
				if !seen[change.Metadata.UserID] {
					seen[change.Metadata.UserID] = true
					userIDs = append(userIDs, change.Metadata.UserID)
				}
			}
		}

		projChanges := make([]Op, 0, len(cluster))
		for _, change := range cluster {
			projChanges = append(projChanges, Op{I: change.Op.I, D: change.Op.D, P: change.Op.P})
		}

		previews = append(previews, SparseChangePreview{
			SectionPath: sectionPathAtLine(headers, lineIndex),
			StartLine:   lineIndex + 1,
			Changes:     projChanges,
			Slice:       extractWindow(lines, lineStarts, windowStart, windowEnd),
			SliceStart:  windowStart,
			UserIDs:     userIDs,
		})
	}
	return previews
}
