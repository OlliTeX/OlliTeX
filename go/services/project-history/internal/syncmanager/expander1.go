package syncmanager

import (
	"fmt"
	"reflect"
)

// --- local error types (vendor overleaf-editor-core / Services Errors) -------

// UnprocessableError — vendor editor-core UnprocessableError (and subclasses
// ApplyError / InvalidInsertionError / TooLongError): data corruption.
type UnprocessableError struct{ Msg string }

func (e *UnprocessableError) Error() string { return e.Msg }

// FileContentEmptyError — vendor: blob loaded but null content.
type FileContentEmptyError struct{ Msg string }

func (e *FileContentEmptyError) Error() string { return e.Msg }

// TooLongError — vendor editor-core (content exceeds MAX_STRING_LENGTH).
type TooLongError struct {
	Msg  string
	Info map[string]any
}

func (e *TooLongError) Error() string { return e.Msg }

// NeedFullProjectStructureResyncError — vendor (partial resync aborted).
type NeedFullProjectStructureResyncError struct{ Msg string }

func (e *NeedFullProjectStructureResyncError) Error() string { return e.Msg }

// SyncOngoingError — vendor (sync in progress; message + 4 info keys).
type SyncOngoingError struct {
	Msg  string
	Info map[string]any
}

func (e *SyncOngoingError) Error() string { return e.Msg }

// --- SyncUpdateExpander (vendor class, 1:1) ----------------------------------

type expander struct {
	d                     *Deps
	projectID             string
	files                 map[string]*File
	expandedUpdates       []map[string]any
	origin                map[string]any
	hardResync            bool
	recoverCorruptedFiles bool
}

// isEditable — vendor (expected file with same path and mismatching _hash →
// non-editable); a matching expected file with NO _hash still blocks.
func (e *expander) isEditable(filePath string, file *File, expectedFiles []map[string]any) bool {
	if !file.Editable {
		return false
	}
	fileHash := file.DataHash
	for _, item := range expectedFiles {
		expectedFileHash, _ := item["_hash"].(string)
		if expectedFileHash != "" && fileHash != expectedFileHash {
			continue
		}
		if e.d.ConvertPathname(fmt.Sprintf("%v", item["path"])) == filePath {
			return false // matched an expected file → not editable
		}
	}
	return true
}

// differenceBy — vendor lodash differenceBy(a, b, 'path').
func differenceBy(a, b []map[string]any) []map[string]any {
	bPaths := map[string]bool{}
	for _, x := range b {
		bPaths[fmt.Sprintf("%v", x["path"])] = true
	}
	out := []map[string]any{}
	for _, x := range a {
		if !bPaths[fmt.Sprintf("%v", x["path"])] {
			out = append(out, x)
		}
	}
	return out
}

func metaResync(e *expander, update map[string]any) map[string]any {
	ts, _ := update["meta"].(map[string]any)
	return map[string]any{"resync": true, "origin": e.origin, "ts": ts["ts"]}
}

// queueRemoveOpsForUnexpectedFiles — vendor.
func (e *expander) queueRemoveOpsForUnexpectedFiles(update map[string]any, expectedFiles, persistedFiles []map[string]any) {
	for _, entity := range differenceBy(persistedFiles, expectedFiles) {
		e.expandedUpdates = append(e.expandedUpdates, map[string]any{
			"pathname":     entity["path"],
			"new_pathname": "",
			"meta":         metaResync(e, update),
		})
		e.d.Inc("project_history_resync_operation", 1, map[string]any{"status": "remove unexpected file"})
	}
}

// queueAddOpsForMissingFiles — vendor.
func (e *expander) queueAddOpsForMissingFiles(update map[string]any, expectedFiles, persistedFiles []map[string]any) {
	for _, entity := range differenceBy(expectedFiles, persistedFiles) {
		op := map[string]any{
			"pathname": entity["path"],
			"meta":     metaResync(e, update),
		}
		if doc, has := entity["doc"]; has && doc != nil {
			op["doc"] = doc
			op["docLines"] = ""
			// dummy entry for later diff computation (vendor File.fromString(''))
			e.files[fmt.Sprintf("%v", entity["path"])] = NewStringFile(fmt.Sprintf("%v", entity["path"]), "")
		} else {
			op["file"] = entity["file"]
			if v, has := entity["url"]; has && v != nil {
				op["url"] = v
			}
			if v, has := entity["_hash"]; has && v != nil {
				op["hash"] = v
			}
			if v, has := entity["createdBlob"]; has && v != nil {
				op["createdBlob"] = v
			}
			if v, has := entity["metadata"]; has && v != nil {
				op["metadata"] = v
			}
		}
		e.expandedUpdates = append(e.expandedUpdates, op)
		e.d.Inc("project_history_resync_operation", 1, map[string]any{"status": "add missing file"})
	}
}

// queueSetMetadataOpsForLinkedFiles — vendor.
func (e *expander) queueSetMetadataOpsForLinkedFiles(update map[string]any) {
	rsp, _ := update["resyncProjectStructure"].(map[string]any)
	if rsp == nil {
		return
	}
	var allEntities []map[string]any
	for _, key := range []string{"docs", "files"} {
		if l, ok := rsp[key].([]any); ok {
			for _, x := range l {
				if m, ok2 := x.(map[string]any); ok2 {
					allEntities = append(allEntities, m)
				}
			}
		}
	}
	for _, file := range allEntities {
		pathname := e.d.ConvertPathname(fmt.Sprintf("%v", file["path"]))
		fileMetadata, _ := file["metadata"].(map[string]any)
		// vendor reference-identity check: the queued add op metadata must be
		// the SAME object as file.metadata (Go: same map pointer).
		for _, u := range e.expandedUpdates {
			if u["pathname"] == pathname {
				uMeta, _ := u["metadata"].(map[string]any)
				if uMeta != nil && fileMetadata != nil &&
					reflect.ValueOf(uMeta).Pointer() == reflect.ValueOf(fileMetadata).Pointer() {
					continue
				}
			}
		}
		var metaData map[string]any
		if f, ok := e.files[pathname]; ok && f != nil {
			metaData = f.Metadata
		}
		shouldUpdate := false
		if fileMetadata != nil {
			for k, v := range fileMetadata {
				if metaData == nil || metaData[k] != v {
					shouldUpdate = true
					break
				}
			}
		} else if metaData != nil {
			if p, has := metaData["provider"]; has && p != nil {
				shouldUpdate = true
			}
		}
		if !shouldUpdate {
			continue
		}
		op := map[string]any{"pathname": pathname, "meta": metaResync(e, update)}
		if fileMetadata != nil {
			op["metadata"] = fileMetadata
		} else {
			op["metadata"] = map[string]any{}
		}
		e.expandedUpdates = append(e.expandedUpdates, op)
		e.d.Inc("project_history_resync_operation", 1, map[string]any{"status": "update metadata"})
	}
}

// queueUpdateForOutOfSyncBinaryFiles — vendor.
func (e *expander) queueUpdateForOutOfSyncBinaryFiles(update map[string]any, expectedFiles, persistedFiles []map[string]any) {
	persistedByPath := map[string]map[string]any{}
	for _, x := range persistedFiles {
		persistedByPath[fmt.Sprintf("%v", x["path"])] = x
	}
	for _, expected := range expectedFiles {
		path := fmt.Sprintf("%v", expected["path"])
		persisted, ok := persistedByPath[path]
		if !ok {
			continue
		}
		expectedHash, _ := expected["_hash"].(string)
		var persistedHash string
		if data, ok2 := persisted["data"].(map[string]any); ok2 {
			persistedHash, _ = data["hash"].(string)
		}
		if expectedHash == "" || persistedHash == "" || persistedHash == expectedHash {
			continue
		}
		// remove the outdated persisted file
		e.expandedUpdates = append(e.expandedUpdates, map[string]any{
			"pathname":     expected["path"],
			"new_pathname": "",
			"meta":         metaResync(e, update),
		})
		// add the new file content
		add := map[string]any{
			"pathname": expected["path"],
			"meta":     metaResync(e, update),
			"file":     expected["file"],
		}
		if v, has := expected["url"]; has && v != nil {
			add["url"] = v
		}
		if v, has := expected["_hash"]; has && v != nil {
			add["hash"] = v
		}
		if v, has := expected["createdBlob"]; has && v != nil {
			add["createdBlob"] = v
		}
		if v, has := expected["metadata"]; has && v != nil {
			add["metadata"] = v
		}
		e.expandedUpdates = append(e.expandedUpdates, add)
		e.d.Inc("project_history_resync_operation", 1, map[string]any{"status": "update binary file contents"})
	}
}
