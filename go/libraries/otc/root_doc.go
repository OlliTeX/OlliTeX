package otc

import (
	"regexp"
	"sort"
	"strings"

	oerror "ollitex/go/libraries/oerror"
)

// root_doc.go — 1:1 port of `lib/root_doc.js`. Decides which doc a project opens
// on and writes (the `main: true` metadata flag) in the operations that record
// it. The choice rule and the operations that write it live here rather than in
// any one of the writers, so they all agree.

// maxRootDocContentToScan — how much of a doc is read looking for its document
// class (30 KiB; only the start is read, each line only from its beginning).
const maxRootDocContentToScan = 30 * 1000

var rootDocExtensionRx = regexp.MustCompile(`(?i)\.(` + strings.Join(DefaultRootDocExtensions, "|") + `)$`)
var rootDocClassRx = regexp.MustCompile(`^\s*\\documentclass`)

// IsRootDocCandidate — mirroring Node `isRootDocCandidate(pathname, content)`:
// whether this doc could be the one a project compiles from — carrying a root-doc
// extension and declaring a document class.
func IsRootDocCandidate(pathname, content string) bool {
	if !rootDocExtensionRx.MatchString(pathname) {
		return false
	}
	head := content
	if len(head) > maxRootDocContentToScan {
		head = head[:maxRootDocContentToScan]
	}
	for _, line := range strings.Split(head, "\n") {
		if rootDocClassRx.MatchString(line) {
			return true
		}
	}
	return false
}

// rdMetadataOf — mirroring Node `metadataOf(snapshot, pathname)`.
func rdMetadataOf(s *Snapshot, pathname string) map[string]any {
	if f := s.GetFile(pathname); f != nil {
		return f.GetMetadata()
	}
	return map[string]any{}
}

// SetMainPathnameOperations — mirroring Node `setMainPathnameOperations(snapshot,
// pathname)`: make the file at pathname the project's root doc, returning the
// operations that unset `main` everywhere and set it here (empty where history
// already records this file). Refuses a file whose metadata is not a doc (Node
// throws `only a doc can be the root doc`).
func SetMainPathnameOperations(s *Snapshot, pathname string) ([]Operation, error) {
	if !IsDocumentMetadata(rdMetadataOf(s, pathname)) {
		return nil, oerror.New("only a doc can be the root doc", map[string]any{"pathname": pathname})
	}
	operations := []Operation{}
	candidates := append([]string{}, s.GetFilePathnames()...)
	sort.Strings(candidates)
	for _, candidate := range candidates {
		metadata := rdMetadataOf(s, candidate)
		main := candidate == pathname
		if HasDocumentMetadataFlag(metadata, "main") == main {
			continue
		}
		operations = append(operations, OperationSetFileMetadata(candidate, WithDocumentMetadataFlag(metadata, "main", main)))
	}
	return operations, nil
}

// RootDocChoice is the result of ChooseRootDoc (Node `{pathname, operations}`).
type RootDocChoice struct {
	Pathname   string
	Operations []Operation
}

// byDepthThenName — mirroring Node `byDepthThenName`.
func byDepthThenName(a, b string) int {
	depth := countSlashes(a) - countSlashes(b)
	if depth != 0 {
		return depth
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func countSlashes(pathname string) int { return strings.Count(pathname, "/") }

// ChooseRootDoc — mirroring Node `chooseRootDoc(snapshot, candidates)`: the doc a
// project should open on + the operations that record it. Returns nil where
// there is no candidate or one is already recorded.
func ChooseRootDoc(s *Snapshot, candidates []string) (*RootDocChoice, error) {
	if s.GetPathnameWithDocFlag("main") != nil {
		// A project whose root doc history already records keeps it — a user's
		// choice, not something to re-derive.
		return nil, nil
	}
	// Only what the snapshot holds: a doc the caller could not write is not one
	// the project has.
	pool := []string{}
	for _, candidate := range candidates {
		if s.GetFile(candidate) != nil {
			pool = append(pool, candidate)
		}
	}
	sort.Slice(pool, func(i, j int) bool { return byDepthThenName(pool[i], pool[j]) < 0 })
	if len(pool) == 0 {
		return nil, nil
	}
	pathname := pool[0]
	operations, err := SetMainPathnameOperations(s, pathname)
	if err != nil {
		return nil, err
	}
	return &RootDocChoice{Pathname: pathname, Operations: operations}, nil
}
