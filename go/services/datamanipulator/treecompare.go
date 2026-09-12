package datamanipulator

// --- tree compare (1:1 with treeCompare.mjs) --------------------------------

func dmCompareTrees(leftTree, rightTree DMTreeResult) map[string]interface{} {
	result := map[string]interface{}{
		"conflicts":   []interface{}{},
		"onlyInLeft":  []interface{}{},
		"onlyInRight": []interface{}{},
		"identical":   []interface{}{},
		"unknown":     []interface{}{},
	}
	leftMap := map[string]DMTreeEntry{}
	for _, e := range leftTree.Entries {
		p, _ := e["relative_path"].(string)
		if dmSyncExcluded(p) {
			continue
		}
		leftMap[p] = e
	}
	rightMap := map[string]DMTreeEntry{}
	for _, e := range rightTree.Entries {
		p, _ := e["relative_path"].(string)
		if dmSyncExcluded(p) {
			continue
		}
		rightMap[p] = e
	}

	sz := func(e DMTreeEntry) int64 {
		switch v := e["size"].(type) {
		case float64:
			return int64(v)
		case int:
			return int64(v)
		case int64:
			return v
		}
		return 0
	}

	var conflicts, onlyInLeft, onlyInRight, identical, unknown []interface{}
	for path, leftEntry := range leftMap {
		rightEntry, hasRight := rightMap[path]
		if !hasRight {
			onlyInLeft = append(onlyInLeft, leftEntry)
			continue
		}
		lc, _ := leftEntry["checksum"].(string)
		rc, _ := rightEntry["checksum"].(string)
		if lc != "" && rc != "" {
			if lc == rc {
				identical = append(identical, map[string]interface{}{"path": path, "checksum": lc})
			} else {
				conflicts = append(conflicts, map[string]interface{}{"path": path, "leftChecksum": lc, "rightChecksum": rc})
			}
		} else {
			if sz(leftEntry) == sz(rightEntry) {
				unknown = append(unknown, map[string]interface{}{
					"path": path,
					"size": sz(leftEntry),
					"note": "checksums unavailable; equal size is not proof of identical content",
				})
			} else {
				conflicts = append(conflicts, map[string]interface{}{
					"path": path, "leftSize": sz(leftEntry), "rightSize": sz(rightEntry),
					"note": "size mismatch (checksums unavailable)",
				})
			}
		}
	}
	for path, rightEntry := range rightMap {
		if _, hasLeft := leftMap[path]; !hasLeft {
			onlyInRight = append(onlyInRight, rightEntry)
		}
	}
	result["conflicts"] = conflicts
	result["onlyInLeft"] = onlyInLeft
	result["onlyInRight"] = onlyInRight
	result["identical"] = identical
	result["unknown"] = unknown
	return result
}
