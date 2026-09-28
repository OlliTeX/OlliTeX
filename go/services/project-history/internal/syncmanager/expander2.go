package syncmanager

import (
	"context"
	"fmt"
	"sort"
)

// expandUpdate — vendor (resyncProjectStructure / resyncDocContent / passthrough).
func (e *expander) expandUpdate(ctx context.Context, update map[string]any) error {
	psr, hasPSR := update["resyncProjectStructure"]
	if hasPSR && psr != nil {
		rsp, _ := update["resyncProjectStructure"].(map[string]any)
		if rsp == nil {
			rsp = map[string]any{}
		}
		expectedFiles, _ := rsp["files"].([]any)
		expectedFilesM := toMapList(expectedFiles)
		expectedDocs, _ := rsp["docs"].([]any)
		expectedDocsM := toMapList(expectedDocs)

		var persistedNonBinary []map[string]any
		var persistedBinary []map[string]any
		for path, file := range e.files {
			if e.isEditable(path, file, expectedFilesM) {
				persistedNonBinary = append(persistedNonBinary, map[string]any{"path": path})
			} else {
				pb := map[string]any{"path": path}
				if file.DataHash != "" {
					pb["data"] = map[string]any{"hash": file.DataHash}
				}
				persistedBinary = append(persistedBinary, pb)
			}
		}
		sort.Slice(persistedNonBinary, func(i, j int) bool {
			return fmt.Sprintf("%v", persistedNonBinary[i]["path"]) < fmt.Sprintf("%v", persistedNonBinary[j]["path"])
		})

		expectedNonBinary := make([]map[string]any, 0, len(expectedDocsM))
		for _, entity := range expectedDocsM {
			cp := map[string]any{}
			for k, v := range entity {
				cp[k] = v
			}
			cp["path"] = e.d.ConvertPathname(fmt.Sprintf("%v", entity["path"]))
			expectedNonBinary = append(expectedNonBinary, cp)
		}
		expectedBinary := make([]map[string]any, 0, len(expectedFilesM))
		for _, entity := range expectedFilesM {
			cp := map[string]any{}
			for k, v := range entity {
				cp[k] = v
			}
			cp["path"] = e.d.ConvertPathname(fmt.Sprintf("%v", entity["path"]))
			expectedBinary = append(expectedBinary, cp)
		}

		e.queueRemoveOpsForUnexpectedFiles(update, expectedBinary, persistedBinary)
		e.queueRemoveOpsForUnexpectedFiles(update, expectedNonBinary, persistedNonBinary)
		e.queueAddOpsForMissingFiles(update, expectedBinary, persistedBinary)
		e.queueAddOpsForMissingFiles(update, expectedNonBinary, persistedNonBinary)
		e.queueUpdateForOutOfSyncBinaryFiles(update, expectedBinary, persistedBinary)
		e.queueSetMetadataOpsForLinkedFiles(update)

		if truthy(update["resyncProjectStructureOnly"]) {
			docPaths := map[string]bool{}
			for _, entity := range expectedDocsM {
				docPaths[e.d.ConvertPathname(fmt.Sprintf("%v", entity["path"]))] = true
			}
			for _, op := range e.expandedUpdates {
				if pname, ok := op["pathname"].(string); ok && docPaths[pname] {
					// Clear the resync state and the queue; we must start over.
					e.expandedUpdates = nil
					if err := e.d.ClearResyncState(ctx, e.projectID); err != nil {
						return err
					}
					if d := e.d.DeleteAppliedDocUpdate; d != nil {
						if err := d(ctx, e.projectID, update); err != nil {
							return err
						}
					}
					return &NeedFullProjectStructureResyncError{Msg: "aborting partial resync: touched doc"}
				}
			}
		}
		return nil
	}
	if rdc, hasRDC := update["resyncDocContent"]; hasRDC && rdc != nil {
		return e.expandResyncDocContentUpdate(ctx, update)
	}
	e.expandedUpdates = append(e.expandedUpdates, update)
	return nil
}

func toMapList(v []any) []map[string]any {
	out := []map[string]any{}
	for _, x := range v {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// expandResyncDocContentUpdate — vendor (load, recover, diff, comments, tracking).
func (e *expander) expandResyncDocContentUpdate(ctx context.Context, update map[string]any) error {
	pathname := e.d.ConvertPathname(fmt.Sprintf("%v", update["path"]))
	snapshotFile := e.files[pathname]
	expected, _ := update["resyncDocContent"].(map[string]any)
	if expected == nil {
		expected = map[string]any{}
	}
	expectedContent, _ := expected["content"].(string)

	if snapshotFile == nil {
		return fmt.Errorf("unrecognised file: not in snapshot")
	}

	hashesMatch := false
	if snapshotFile.Hash != "" {
		expectedHash := e.d.GetBlobHashFromString(expectedContent)
		if snapshotFile.Hash == expectedHash {
			e.d.LogDebug(map[string]any{"projectId": e.projectID, "persistedHash": snapshotFile.Hash, "expectedHash": expectedHash}, "skipping diff because hashes match and persisted file has no ops")
			hashesMatch = true
		}
	} else {
		e.d.LogDebug(map[string]any{}, "cannot compare hashes, will retrieve content")
	}

	if id, err := e.d.GetHistoryID(ctx, e.projectID); err != nil {
		return err
	} else {
		_ = id
	}
	err := e.d.LoadFileContent(ctx, e.projectID, snapshotFile)
	persistedEmpty := snapshotFile.Content == ""
	if err != nil {
		if !e.recoverCorruptedFiles || !e.d.isDataCorruptionError(err) {
			e.d.LogErr(map[string]any{"name": "failed to load file from history during resync", "projectId": e.projectID, "pathname": pathname}, "load failed: "+err.Error())
			return err
		}
		if len(expectedContent) > maxStringLength {
			return &TooLongError{
				Msg:  fmt.Sprintf("Too long: %d > %d", len(expectedContent), maxStringLength),
				Info: map[string]any{"projectId": e.projectID, "pathname": pathname, "maxLength": maxStringLength},
			}
		}
		e.d.LogWarn(map[string]any{"projectId": e.projectID, "pathname": pathname}, "failed to load file from history during hard resync, removing and re-adding from docstore")
		e.d.Inc("project_history_resync_operation", 1, map[string]any{"status": "recover corrupted file"})
		e.expandedUpdates = append(e.expandedUpdates,
			map[string]any{"pathname": pathname, "new_pathname": "", "meta": metaResync(e, update)},
			map[string]any{"pathname": pathname, "doc": update["doc"], "docLines": expectedContent, "meta": metaResync(e, update)},
		)
		replacement := NewStringFile(pathname, expectedContent)
		e.files[pathname] = replacement
		snapshotFile = replacement
	} else if persistedEmpty {
		e.d.LogErr(map[string]any{"name": "failed to load file from history during resync", "projectId": e.projectID, "pathname": pathname}, "File was not properly loaded")
		return &FileContentEmptyError{Msg: "File was not properly loaded"}
	}

	persistedContent := snapshotFile.Content

	if !hashesMatch {
		expanded, err := e.queueUpdateForOutOfSyncContent(update, pathname, persistedContent, expectedContent)
		if err != nil {
			return err
		}
		if expanded != nil {
			ops, _ := expanded["op"].([]any)
			for _, opAny := range ops {
				op, _ := opAny.(map[string]any)
				if op == nil {
					continue
				}
				p, _ := op["p"].(int)
				if i, ok := op["i"]; ok && i != nil {
					s, _ := i.(string)
					snapshotFile.CommentsApplyInsert(p, len(s))
					snapshotFile.TrackedChangesApplyInsert(p, s)
				} else if dLen, ok := op["d"]; ok && dLen != nil {
					s, _ := dLen.(string)
					snapshotFile.CommentsApplyDelete(p, len(s))
					snapshotFile.TrackedChangesApplyDelete(p, len(s))
				}
			}
		}
	}

	if hotr, has := expected["historyOTRanges"]; has && hotr != nil {
		e.queueUpdatesForOutOfSyncCommentsHistoryOT(update, pathname, snapshotFile.CommentsToRaw())
	} else {
		e.queueUpdatesForOutOfSyncComments(update, pathname, snapshotFile.CommentsToRaw())
	}
	e.queueUpdatesForOutOfSyncTrackedChanges(update, pathname, snapshotFile.TrackedChangesAsSorted())
	return nil
}

// queueUpdateForOutOfSyncContent — vendor.
func (e *expander) queueUpdateForOutOfSyncContent(update map[string]any, pathname, persistedContent, expectedContent string) (map[string]any, error) {
	e.d.LogDebug(map[string]any{"projectId": e.projectID}, "diffing doc contents")
	op := e.d.DiffAsShareJsOps(persistedContent, expectedContent)
	if len(op) == 0 {
		return nil, nil
	}
	expanded := map[string]any{
		"doc": update["doc"],
		"op":  op,
		"meta": map[string]any{
			"resync":     true,
			"origin":     e.origin,
			"ts":         (func() any { m, _ := update["meta"].(map[string]any); return m["ts"] })(),
			"pathname":   pathname,
			"doc_length": len(persistedContent),
		},
	}
	e.d.LogDebug(map[string]any{"projectId": e.projectID, "diffCount": len(op)}, "doc contents differ")
	e.expandedUpdates = append(e.expandedUpdates, expanded)
	e.d.Inc("project_history_resync_operation", 1, map[string]any{"status": "update text file contents"})
	return expanded, nil
}
