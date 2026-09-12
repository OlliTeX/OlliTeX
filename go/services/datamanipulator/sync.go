package datamanipulator

import (
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

// --- sync (1:1 with sync.mjs) ------------------------------------------------

type DMPullOptions struct {
	ConfirmRemoteDeletions bool
	DeletedPaths           []string
	AllowEmptyRemote       bool
}

func dmEtageFor(checksum, mtime string) string {
	parts := strings.SplitN(checksum, ":", 2)
	if len(parts) == 2 {
		return "sha256:" + parts[1] + "|" + mtime
	}
	return "sha256:" + checksum + "|" + mtime
}

// dmPullFiles mirrors sync.pullFiles.
func dmPullFiles(projectDir string, remoteFiles []map[string]interface{}, opts DMPullOptions) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"downloaded": 0,
		"skipped":    0,
		"deleted":    0,
		"conflicts":  []interface{}{},
	}
	if len(remoteFiles) == 0 && !opts.AllowEmptyRemote {
		return nil, errors.New("remote file listing is empty; refusing to derive deletions (possible incomplete listing)")
	}
	localTree, _ := dmWalkTree(projectDir, "")
	localMap := map[string]DMTreeEntry{}
	for _, e := range localTree.Entries {
		if p, _ := e["relative_path"].(string); p != "" {
			localMap[p] = e
		}
	}
	remoteMap := map[string]map[string]interface{}{}
	for _, f := range remoteFiles {
		if p, _ := f["relative_path"].(string); p != "" {
			remoteMap[p] = f
		}
	}

	downloaded, skipped, deleted := 0, 0, 0
	var conflicts []interface{}
	for path, remoteFile := range remoteMap {
		localFile, hasLoc := localMap[path]
		if !hasLoc {
			cb, _ := remoteFile["content_base64"].(string)
			content, derr := base64.StdEncoding.DecodeString(cb)
			if derr != nil {
				content = []byte{}
			}
			if _, werr := dmWriteFile(projectDir, path, content); werr == nil {
				downloaded++
			}
		} else {
			lc, _ := localFile["checksum"].(string)
			rc, _ := remoteFile["checksum"].(string)
			if lc == "" || rc == "" {
				skipped++
				continue
			}
			if lc == rc {
				skipped++
			} else {
				lm, _ := localFile["mtime"].(string)
				rm, _ := remoteFile["mtime"].(string)
				conflicts = append(conflicts, map[string]interface{}{"path": path, "local_etag": dmEtageFor(lc, lm), "remote_etag": dmEtageFor(rc, rm)})
			}
		}
	}

	// deletions
	var deletable []string
	for path := range localMap {
		if _, hasRemote := remoteMap[path]; !hasRemote {
			deletable = append(deletable, path)
		}
	}
	if len(deletable) > 0 {
		if opts.ConfirmRemoteDeletions {
			allowed := map[string]bool{}
			if len(opts.DeletedPaths) > 0 {
				for _, p := range opts.DeletedPaths {
					allowed[p] = true
				}
			} else {
				for _, p := range deletable {
					allowed[p] = true
				}
			}
			for _, path := range deletable {
				if !allowed[path] {
					continue
				}
				if dend := dmDeletePath(projectDir, path); dend == nil {
					deleted++
				}
			}
		} else {
			result["skipped_deletions"] = deletable
		}
	}

	result["downloaded"] = downloaded
	result["skipped"] = skipped
	result["deleted"] = deleted
	result["conflicts"] = conflicts
	return result, nil
}

// dmPushFiles mirrors sync.pushFiles (local-side counts; no transport).
func dmPushFiles(projectDir string, remoteFiles []map[string]interface{}) (map[string]interface{}, error) {
	result := map[string]interface{}{"uploaded": 0, "skipped": 0, "deleted_remote": 0}
	localTree, _ := dmWalkTree(projectDir, "")
	localMap := map[string]DMTreeEntry{}
	for _, e := range localTree.Entries {
		if p, _ := e["relative_path"].(string); p != "" {
			localMap[p] = e
		}
	}
	remoteMap := map[string]map[string]interface{}{}
	for _, f := range remoteFiles {
		if p, _ := f["relative_path"].(string); p != "" {
			remoteMap[p] = f
		}
	}
	uploaded, skipped := 0, 0
	for path, localFile := range localMap {
		remoteFile, hasRemote := remoteMap[path]
		if !hasRemote {
			if _, rerr := dmReadFile(projectDir, path); rerr == nil {
				uploaded++
			}
		} else {
			lc, _ := localFile["checksum"].(string)
			rc, _ := remoteFile["checksum"].(string)
			if lc == "" || rc == "" {
				skipped++
				continue
			}
			if lc == rc {
				skipped++
			} else {
				if _, rerr := dmReadFile(projectDir, path); rerr == nil {
					uploaded++
				}
			}
		}
	}
	deletedRemote := 0
	for path := range remoteMap {
		if _, hasLoc := localMap[path]; !hasLoc {
			deletedRemote++
		}
	}
	result["uploaded"] = uploaded
	result["skipped"] = skipped
	result["deleted_remote"] = deletedRemote
	return result, nil
}

// dmFullSync mirrors sync.fullSync.
func dmFullSync(projectDir string, remoteFiles []map[string]interface{}) (map[string]interface{}, error) {
	localTree, _ := dmWalkTree(projectDir, "")
	remote := DMTreeResult{Entries: []DMTreeEntry{}}
	for _, f := range remoteFiles {
		remote.Entries = append(remote.Entries, f)
	}
	comparison := dmCompareTrees(*localTree, remote)
	localMap := map[string]DMTreeEntry{}
	for _, e := range localTree.Entries {
		if p, _ := e["relative_path"].(string); p != "" {
			localMap[p] = e
		}
	}
	remoteMap := map[string]map[string]interface{}{}
	for _, f := range remoteFiles {
		if p, _ := f["relative_path"].(string); p != "" {
			remoteMap[p] = f
		}
	}
	conflicts := []interface{}{}
	if cs, ok := comparison["conflicts"].([]interface{}); ok {
		for _, c := range cs {
			cm, _ := c.(map[string]interface{})
			path, _ := cm["path"].(string)
			lc, _ := cm["leftChecksum"].(string)
			rc, _ := cm["rightChecksum"].(string)
			lm := ""
			if le, ok := localMap[path]; ok {
				lm, _ = le["mtime"].(string)
			}
			rm := ""
			if rf, ok := remoteMap[path]; ok {
				rm, _ = rf["mtime"].(string)
			}
			lcPart := strings.TrimPrefix(lc, "sha256:")
			rcPart := strings.TrimPrefix(rc, "sha256:")
			conflicts = append(conflicts, map[string]interface{}{
				"path":        path,
				"local_etag":  "sha256:" + lcPart + "|" + lm,
				"remote_etag": "sha256:" + rcPart + "|" + rm,
			})
		}
	}
	onlyLocal := comparison["onlyInLeft"].([]interface{})
	onlyRemote := comparison["onlyInRight"].([]interface{})
	identical := comparison["identical"].([]interface{})
	return map[string]interface{}{
		"summary": map[string]interface{}{
			"total_files":       localTree.TotalFiles,
			"conflicts_count":   len(conflicts),
			"only_local_count":  len(onlyLocal),
			"only_remote_count": len(onlyRemote),
			"identical_count":   len(identical),
		},
		"conflicts":   conflicts,
		"only_local":  onlyLocal,
		"only_remote": onlyRemote,
		"identical":   identical,
	}, nil
}

// dmResolveConflictByMtime mirrors sync.resolveConflictByMtime.
func dmResolveConflictByMtime(localFile, remoteFile map[string]interface{}) string {
	p := func(v interface{}) (int64, bool) {
		s, ok := v.(string)
		if !ok {
			return 0, false
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			if t2, e2 := time.Parse(dmMute, s); e2 == nil {
				return t2.UnixMilli(), true
			}
			return 0, false
		}
		return t.UnixMilli(), true
	}
	lt, lok := p(localFile["mtime"])
	rt, rok := p(remoteFile["mtime"])
	if !lok || !rok {
		return "needs_review"
	}
	if lt > rt {
		return "left"
	}
	if rt > lt {
		return "right"
	}
	return "needs_review"
}
