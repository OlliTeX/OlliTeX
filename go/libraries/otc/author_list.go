package otc

import (
	"regexp"
)

// Mirrors libraries/overleaf-editor-core/lib/author_list.js.

// v2AuthorIDRx is the v2 author-id shape: a 24-hex-char id.
var v2AuthorIDRx = regexp.MustCompile(`^[0-9a-f]{24}$`)

// AssertAuthorsV1 mirrors assertV1: every non-null/undefined member is either a
// number (if the first non-null member is a number) or an Author value
// (otherwise). msg defaults to "bad authors".
func AssertAuthorsV1(authors []any, msg string) {
	if msg == "" {
		msg = "bad authors"
	}
	filtered := make([]any, 0, len(authors))
	for _, a := range authors {
		if a != nil {
			filtered = append(filtered, a)
		}
	}
	if len(filtered) == 0 {
		return
	}
	firstIsNumber := isIntLike(filtered[0])
	for _, author := range filtered {
		if firstIsNumber {
			if !isIntLike(author) {
				panic(typeErrorf("%s (expected a number)", msg))
			}
		} else {
			if _, ok := author.(*Author); !ok {
				panic(typeErrorf("%s (expected an Author)", msg))
			}
		}
	}
}

// AssertAuthorsV2 mirrors assertV2: every member (a maybe-regex, so null is
// allowed) must match the v2 author-id pattern when it is a string.
func AssertAuthorsV2(authors []any, msg string) {
	for _, author := range authors {
		if author == nil {
			continue
		}
		s, ok := author.(string)
		if !ok || !v2AuthorIDRx.MatchString(s) {
			panic(typeErrorf("%s (expected a v2 author id)", msg))
		}
	}
}

func isIntLike(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64, float64:
		return true
	default:
		return false
	}
}
