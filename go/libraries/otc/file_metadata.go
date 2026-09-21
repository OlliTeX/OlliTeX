package otc

// Mirrors libraries/overleaf-editor-core/lib/file_metadata.js.
//
// FileMetadata is the loose map a file carries: the doc flags (`main`,
// `mainBibliography`) plus optional origin records (e.g. importedAt, provider).

// DocumentMetadataKeys are the metadata keys a doc can carry: `main` marks the
// project's root doc, `mainBibliography` the bibliography references are added
// to.
var DocumentMetadataKeys = []string{"main", "mainBibliography"}

// IsDocumentMetadata mirors isDocumentMetadata: whether metadata holds nothing
// but the keys a doc can carry, which is what makes a file a doc rather than a
// file.
func IsDocumentMetadata(metadata map[string]any) bool {
	for key := range metadata {
		found := false
		for _, k := range DocumentMetadataKeys {
			if key == k {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// HasDocumentMetadataFlag mirrors hasDocumentMetadataFlag: whether metadata
// carries a document flag (truthy).
func HasDocumentMetadataFlag(metadata map[string]any, key string) bool {
	if metadata == nil {
		return false
	}
	return isTruthy(metadata[key])
}

// WithDocumentMetadataFlag mirrors withDocumentMetadataFlag: metadata with one
// document flag set or cleared, keeping the others. Metadata is replaced
// wholesale by SetFileMetadataOperation, so changing one flag means writing out
// the rest of them too. A cleared flag is left out rather than set to false, so
// a file carrying none has empty metadata. Non-flag keys are dropped.
func WithDocumentMetadataFlag(metadata map[string]any, key string, value bool) map[string]any {
	next := map[string]any{}
	for _, documentKey := range DocumentMetadataKeys {
		keep := HasDocumentMetadataFlag(metadata, documentKey)
		if documentKey == key {
			keep = value
		}
		if keep {
			next[documentKey] = true
		}
	}
	return next
}

func isTruthy(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	// A present (non-boolean) value counts as a set flag (JS truthy semantics).
	return true
}
