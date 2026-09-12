package datamanipulator

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
)

// --- file operations (1:1 with fileOperations.mjs) ---------------------------

type DMTreeEntry = map[string]interface{}

type DMTreeResult struct {
	Entries    []DMTreeEntry
	TotalFiles int
	TotalSize  int64
}

// dmWalkTree mirrors fileOperations.walkTree.
func dmWalkTree(projectDir, basePath string) (*DMTreeResult, error) {
	root, _ := filepath.Abs(projectDir)
	if _, statErr := os.Stat(root); statErr != nil {
		return nil, &DMDirectoryNotFoundError{Path: root}
	}
	res := &DMTreeResult{}
	var walk func(currentPath, relativeBase string)
	walk = func(currentPath, relativeBase string) {
		dirEntries, err := os.ReadDir(currentPath)
		if err != nil {
			// Match Node: a non-listable directory during recursion is tolerated
			// (only a missing top-level raises DirectoryNotFoundError).
			return
		}
		for _, entry := range dirEntries {
			fullPath := filepath.Join(currentPath, entry.Name())
			relPath := entry.Name()
			if relativeBase != "" {
				relPath = relativeBase + "/" + entry.Name()
			}
			if dmSyncExcluded(relPath) {
				continue
			}
			if entry.IsDir() {
				res.Entries = append(res.Entries, map[string]interface{}{
					"relative_path": relPath,
					"name":          entry.Name(),
					"type":          "directory",
					"depth":         strings.Count(relPath, "/"),
				})
				if entry.Name() != "node_modules" {
					walk(fullPath, relPath)
				}
			} else if entry.Type().IsRegular() {
				buf, rerr := os.ReadFile(fullPath)
				if rerr != nil {
					continue
				}
				meta := dmFileMetadata(relPath, buf)
				res.Entries = append(res.Entries, meta)
				res.TotalFiles++
				res.TotalSize += int64(len(buf))
			}
		}
	}
	walk(root, basePath)
	return res, nil
}

// dmReadFile mirrors fileOperations.readFile.
func dmReadFile(projectDir, relativePath string) (map[string]interface{}, error) {
	fullPath, err := dmResolveProjectPath(projectDir, relativePath)
	if err != nil {
		return nil, err
	}
	buf, rerr := os.ReadFile(fullPath)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return nil, &DMFileNotFoundError{Path: relativePath}
		}
		return nil, rerr
	}
	st, serr := os.Stat(fullPath)
	if serr != nil {
		return nil, serr
	}
	meta := dmFileMetadata(relativePath, buf)
	meta["content_base64"] = base64.StdEncoding.EncodeToString(buf)
	meta["size"] = len(buf)
	meta["mtime"] = st.ModTime().UTC().Format(dmMute)
	return meta, nil
}

// dmWriteFile mirrors fileOperations.writeFile (creates parents).
func dmWriteFile(projectDir, relativePath string, content []byte) (map[string]interface{}, error) {
	fullPath, err := dmResolveProjectPath(projectDir, relativePath)
	if err != nil {
		return nil, err
	}
	if derr := os.MkdirAll(filepath.Dir(fullPath), 0o777); derr != nil {
		return nil, derr
	}
	if werr := os.WriteFile(fullPath, content, 0o666); werr != nil {
		return nil, werr
	}
	return dmFileMetadata(relativePath, content), nil
}

// dmDeletePath mirrors fileOperations.deletePath (file or recursive dir).
func dmDeletePath(projectDir, relativePath string) error {
	fullPath, err := dmResolveProjectPath(projectDir, relativePath)
	if err != nil {
		return err
	}
	st, serr := os.Stat(fullPath)
	if serr != nil {
		if os.IsNotExist(serr) {
			return &DMFileNotFoundError{Path: relativePath}
		}
		return serr
	}
	if st.IsDir() {
		return os.RemoveAll(fullPath)
	}
	if uerr := os.Remove(fullPath); uerr != nil {
		if os.IsNotExist(uerr) {
			return &DMFileNotFoundError{Path: relativePath}
		}
		return uerr
	}
	return nil
}
