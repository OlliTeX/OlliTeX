// Package updatekeys — 1:1 port of `app/js/UpdateKeys.js`.
// ShareJS doc-key composition: '<projectId>:<docId>'. Project and doc ids
// never contain a colon, so the composition is unambiguous.
package updatekeys

import "strings"

// CombineProjectIdAndDocId builds the ShareJS `project_id:doc_id` key.
//
//	Node: `combineProjectIdAndDocId(projectId, docId)`.
func CombineProjectIdAndDocId(projectID, docID string) string {
	return projectID + ":" + docID
}

// SplitProjectIdAndDocId splits a `project_id:doc_id` key into its parts.
//
// Returns (projectID, docID, ok); ok=false when the key has no colon.
//
//	Node: `splitProjectIdAndDocId(projectAndDocId)`.
func SplitProjectIdAndDocId(projectAndDocID string) (projectID string, docID string, ok bool) {
	projectID, docID, found := strings.Cut(projectAndDocID, ":")
	if !found {
		return "", "", false
	}
	return projectID, docID, true
}
