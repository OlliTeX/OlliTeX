package ot

// Port of file_metadata.js.

// DOCUMENT_METADATA_KEYS are the metadata keys a doc can carry: `main` marks
// the project's root doc, `mainBibliography` the bibliography references are
// added to. Anything else a file's metadata holds records where it came from.
var DOCUMENT_METADATA_KEYS = []string{"main", "mainBibliography"}

// IsDocumentMetadata reports whether metadata holds nothing but the keys a
// doc can carry, which is what makes a file a doc rather than a file.
func IsDocumentMetadata(metadata map[string]any) bool {
	for key := range metadata {
		isDocKey := false
		for _, k := range DOCUMENT_METADATA_KEYS {
			if key == k {
				isDocKey = true
				break
			}
		}
		if !isDocKey {
			return false
		}
	}
	return true
}

// HasDocumentMetadataFlag reports whether metadata carries a document flag.
func HasDocumentMetadataFlag(metadata map[string]any, key string) bool {
	if metadata == nil {
		return false
	}
	// Boolean(metadata[key]): any present value (including numbers) is
	// truthy, mirroring JS truthiness.
	v, ok := metadata[key]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t != ""
	default:
		return true
	}
}

// WithDocumentMetadataFlag returns metadata with one document flag
// set or cleared, keeping the others. Metadata is replaced wholesale by
// SetFileMetadataOperation, so changing one flag means writing out the rest
// of them too. A cleared flag is left out rather than set to false, so a
// file carrying none has empty metadata.
func WithDocumentMetadataFlag(metadata map[string]any, key string, value bool) map[string]bool {
	next := map[string]bool{}
	for _, documentKey := range DOCUMENT_METADATA_KEYS {
		if documentKey == key {
			if value {
				next[documentKey] = true
			}
		} else if HasDocumentMetadataFlag(metadata, documentKey) {
			next[documentKey] = true
		}
	}
	return next
}
