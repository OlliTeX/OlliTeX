package services

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// --- errors (1:1 with errors.mjs) -------------------------------------------

type DMFileNotFoundError struct{ Path string }

func (e *DMFileNotFoundError) Error() string { return "File not found: " + e.Path }

type DMDirectoryNotFoundError struct{ Path string }

func (e *DMDirectoryNotFoundError) Error() string { return "Directory not found: " + e.Path }

type DMPermissionError struct {
	Action string
	Path   string
}

func (e *DMPermissionError) Error() string { return "Permission denied: " + e.Action + " " + e.Path }

type DMConflictError struct {
	Path    string
	Message string
}

func (e *DMConflictError) Error() string {
	if e.Message == "" {
		e.Message = "File conflict detected"
	}
	return e.Message + ": " + e.Path
}

// File type constants (1:1 with fileUtils.FileTypes).
const (
	DMText   = "text"
	DMBinary = "binary"
)

// --- file utils (1:1 with fileUtils.mjs) ------------------------------------

var dmSyncTransientRe = regexp.MustCompile(`\.(aux|log|out|toc|fls|idx|vrb)$`)
var dmSyncTeXGZRe = regexp.MustCompile(`\.synctex\.gz$`)

// dmSyncExcluded mirrors isSyncExcluded (RF.5: hidden in ANY segment + LaTeX transients).
func dmSyncExcluded(name string) bool {
	if name == "" {
		return true
	}
	parts := []string{}
	for _, seg := range strings.Split(name, "/") {
		if seg != "" {
			parts = append(parts, seg)
		}
	}
	if len(parts) == 0 {
		return true
	}
	for _, part := range parts {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	base := parts[len(parts)-1]
	return dmSyncTransientRe.MatchString(base) || dmSyncTeXGZRe.MatchString(base)
}

var dmBinaryExtSet = map[string]bool{
	"pdf": true, "jpg": true, "jpeg": true, "png": true, "gif": true, "zip": true,
	"ttf": true, "woff": true, "woff2": true, "eot": true, "ico": true,
	"exe": true, "msi": true, "bin": true, "tar": true, "gz": true,
	"rar": true, "7z": true, "img": true, "iso": true, "dmg": true,
}

func dmExtOf(p string) string {
	idx := strings.LastIndex(p, ".")
	if idx < 0 {
		base := p
		if slash := strings.LastIndex(p, "/"); slash >= 0 {
			base = p[slash+1:]
		}
		return strings.ToLower(base)
	}
	return strings.ToLower(p[idx+1:])
}

// dmDetectFileType mirrors detectFileType (extension fast path + null-byte + UTF-8 check).
func dmDetectFileType(filePath string, buf []byte) (typ, encoding string) {
	if dmBinaryExtSet[dmExtOf(filePath)] {
		return DMBinary, ""
	}
	if len(buf) == 0 {
		return DMText, "utf8"
	}
	sample := buf
	if len(sample) > 8192 {
		sample = sample[:8192]
	}
	nullCount := bytes.Count(sample, []byte{0})
	if float64(nullCount) > float64(len(sample))*0.05 {
		return DMBinary, ""
	}
	if !utf8.Valid(sample) {
		if nullCount == 0 {
			return DMText, "latin1"
		}
		return DMBinary, ""
	}
	return DMText, "utf8"
}

// dmChecksum mirrors calculateChecksum (sha256:hex, empty-hash fallback).
func dmChecksum(buf []byte) string {
	if len(buf) == 0 {
		return "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	}
	h := sha256.Sum256(buf)
	return "sha256:" + hex.EncodeToString(h[:])
}

const dmMute = "2006-01-02T15:04:05.000Z"

// dmFileMetadata mirrors getFileMetadata.
func dmFileMetadata(filePath string, buf []byte) map[string]interface{} {
	typ, encoding := dmDetectFileType(filePath, buf)
	name := filePath
	if slash := strings.LastIndex(filePath, "/"); slash >= 0 {
		name = filePath[slash+1:]
	}
	if name == "" {
		name = filePath
	}
	m := map[string]interface{}{
		"relative_path": filePath,
		"name":          name,
		"type":          "file",
		"size":          len(buf),
		"binary":        typ == DMBinary,
		"checksum":      dmChecksum(buf),
		"mtime":         time.Now().UTC().Format(dmMute),
	}
	if encoding != "" {
		m["encoding"] = encoding
	}
	return m
}

// resolveProjectPath mirrors resolveProjectPath (traversal guard).
func dmResolveProjectPath(projectDir, relativePath string) (string, error) {
	root, _ := filepath.Abs(projectDir)
	resolved, _ := filepath.Abs(filepath.Join(root, relativePath))
	if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
		return "", errors.New("Path must stay within the project directory")
	}
	return resolved, nil
}

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

// --- HTTP server (1:1 routes + ARC-02 auth) ---------------------------------

var dmProjectIdRe = regexp.MustCompile(`^[0-9a-f]{12}$`)

func dmIsValidProjectId(id string) bool {
	return id != "" && dmProjectIdRe.MatchString(strings.ToLower(id))
}

type DMConfig struct {
	ProjectsRoot string
	ServiceToken string
}

func (c *DMConfig) withDefaults() {
	if c.ProjectsRoot == "" {
		c.ProjectsRoot = "/projects"
	}
}

type DMHandlers struct {
	Cfg DMConfig
}

func NewDMHandlers(cfg DMConfig) *DMHandlers {
	cfg.withDefaults()
	return &DMHandlers{Cfg: cfg}
}

func (h *DMHandlers) projectDir(projectId string) (string, error) {
	root, _ := filepath.Abs(h.Cfg.ProjectsRoot)
	resolved, _ := filepath.Abs(filepath.Join(root, projectId))
	if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
		return "", dmStatusErr(400, "Invalid project_id")
	}
	return resolved, nil
}

func dmStatusErr(code int, msg string) *dmErr {
	return &dmErr{code: code, msg: msg}
}

type dmErr struct {
	code int
	msg  string
}

func (e *dmErr) Error() string { return e.msg }

func dmErrCode(err error) int {
	var de *dmErr
	if errors.As(err, &de) {
		return de.code
	}
	return 0
}

func (h *DMHandlers) Mux() http.Handler {
	cfg := h.Cfg
	handlers := h
	auth := requireServiceToken(cfg.ServiceToken, func() {
		fmt.Println("SHARED_SERVICE_TOKEN is unset; accepting unauthenticated requests (should be restricted to in-container callers)")
	})
	dispatch := auth(func(w http.ResponseWriter, r *http.Request) {
		handlers.route(w, r)
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]string{"status": "ok", "service": "datamanipulator"})
	})
	mux.HandleFunc("/tree", dispatch)
	mux.HandleFunc("/files", dispatch)
	mux.HandleFunc("/file", dispatch)
	mux.HandleFunc("/pull", dispatch)
	mux.HandleFunc("/push", dispatch)
	mux.HandleFunc("/compare", dispatch)
	mux.HandleFunc("/sync/full", dispatch)
	return mux
}

func (h *DMHandlers) route(w http.ResponseWriter, r *http.Request) {
	uq := r.URL.Query()
	q := map[string]string{}
	for k, vs := range uq {
		if len(vs) > 0 {
			q[k] = vs[0]
		}
	}
	path := r.URL.Path
	body := map[string]interface{}{}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	switch {
	case path == "/tree" && r.Method == http.MethodGet:
		h.tree(w, q, false)
	case path == "/files" && r.Method == http.MethodGet:
		h.tree(w, q, true)
	case path == "/file" && r.Method == http.MethodGet:
		h.fileGet(w, q)
	case path == "/file" && r.Method == http.MethodPost:
		h.filePost(w, q, body)
	case path == "/file" && r.Method == http.MethodDelete:
		h.fileDelete(w, q)
	case path == "/pull" && r.Method == http.MethodPost:
		h.pull(w, q, body)
	case path == "/push" && r.Method == http.MethodPost:
		h.push(w, q, body)
	case path == "/compare" && r.Method == http.MethodPost:
		h.compare(w, body)
	case path == "/sync/full" && r.Method == http.MethodPost:
		h.syncFull(w, q, body)
	default:
		writeJSON(w, 404, map[string]string{"error": "not found"})
	}
}

func (h *DMHandlers) projectIdOk(w http.ResponseWriter, q map[string]string) (string, bool) {
	id, _ := q["project_id"]
	if !dmIsValidProjectId(id) {
		writeJSON(w, 400, map[string]string{"error": "Invalid project_id format"})
		return "", false
	}
	dir, err := h.projectDir(id)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": "Invalid project_id"})
		return "", false
	}
	return dir, true
}

func dmAsEntrySlice(v interface{}) []DMTreeEntry {
	if arr, ok := v.([]interface{}); ok {
		out := make([]DMTreeEntry, 0, len(arr))
		for _, it := range arr {
			if m, ok := it.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

func (h *DMHandlers) tree(w http.ResponseWriter, q map[string]string, filesMode bool) {
	dir, ok := h.projectIdOk(w, q)
	if !ok {
		return
	}
	relPath, _ := q["path"]
	tree, werr := dmWalkTree(dir, relPath)
	if werr != nil {
		if _, isFile := errAs[*DMFileNotFoundError](werr); isFile {
			writeJSON(w, 404, map[string]string{"error": werr.Error()})
			return
		}
		if _, isDir := errAs[*DMDirectoryNotFoundError](werr); isDir {
			writeJSON(w, 404, map[string]string{"error": werr.Error()})
			return
		}
		writeJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	if filesMode && relPath != "" {
		prefix := relPath + "/"
		filtered := make([]map[string]interface{}, 0)
		for _, e := range tree.Entries {
			p, _ := e["relative_path"].(string)
			if p == relPath || strings.HasPrefix(p, prefix) {
				cp := map[string]interface{}{}
				for k, v := range e {
					cp[k] = v
				}
				cp["relative_path"] = strings.TrimPrefix(p, prefix)
				filtered = append(filtered, cp)
			}
		}
		writeJSON(w, 200, map[string]interface{}{"path": relPath, "entries": filtered})
		return
	}
	writeJSON(w, 200, map[string]interface{}{
		"entries":    tree.Entries,
		"totalFiles": tree.TotalFiles,
		"totalSize":  tree.TotalSize,
	})
}

func errAs[T any](err error) (T, bool) {
	var t T
	if errors.As(err, &t) {
		return t, true
	}
	var zero T
	return zero, false
}

func (h *DMHandlers) fileGet(w http.ResponseWriter, q map[string]string) {
	dir, ok := h.projectIdOk(w, q)
	if !ok {
		return
	}
	relPath, _ := q["path"]
	if relPath == "" {
		writeJSON(w, 400, map[string]string{"error": "Missing path parameter"})
		return
	}
	data, err := dmReadFile(dir, relPath)
	if err != nil {
		if _, isFile := errAs[*DMFileNotFoundError](err); isFile {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		if _, isDir := errAs[*DMDirectoryNotFoundError](err); isDir {
			writeJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	if _, hasCB := data["content_base64"]; !hasCB {
		data["content_base64"] = ""
	}
	writeJSON(w, 200, data)
}

func (h *DMHandlers) filePost(w http.ResponseWriter, q map[string]string, body map[string]interface{}) {
	dir, ok := h.projectIdOk(w, q)
	if !ok {
		return
	}
	relPath, _ := q["path"]
	if relPath == "" {
		writeJSON(w, 400, map[string]string{"error": "Missing path parameter"})
		return
	}
	cb, _ := body["content_base64"].(string)
	if cb == "" {
		writeJSON(w, 400, map[string]string{"error": "Missing content in request body"})
		return
	}
	content, derr := base64.StdEncoding.DecodeString(cb)
	if derr != nil {
		writeJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	meta, werr := dmWriteFile(dir, relPath, content)
	if werr != nil {
		writeJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	meta["content_base64"] = ""
	writeJSON(w, 200, meta)
}

func (h *DMHandlers) fileDelete(w http.ResponseWriter, q map[string]string) {
	dir, ok := h.projectIdOk(w, q)
	if !ok {
		return
	}
	relPath, _ := q["path"]
	if relPath == "" {
		writeJSON(w, 400, map[string]string{"error": "Missing path parameter"})
		return
	}
	if derr := dmDeletePath(dir, relPath); derr != nil {
		if _, isFile := errAs[*DMFileNotFoundError](derr); isFile {
			writeJSON(w, 404, map[string]string{"error": derr.Error()})
			return
		}
		writeJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	writeJSON(w, 200, map[string]interface{}{"success": true})
}

func (h *DMHandlers) pull(w http.ResponseWriter, q map[string]string, body map[string]interface{}) {
	dir, ok := h.projectIdOk(w, q)
	if !ok {
		return
	}
	remoteRaw, _ := body["remote_files"].([]interface{})
	remoteFiles := []map[string]interface{}{}
	for _, it := range remoteRaw {
		if m, ok := it.(map[string]interface{}); ok {
			if p, _ := m["relative_path"].(string); !dmSyncExcluded(p) {
				remoteFiles = append(remoteFiles, m)
			}
		}
	}
	opts := DMPullOptions{
		ConfirmRemoteDeletions: body["confirm_remote_deletions"] == true,
		AllowEmptyRemote:       true,
	}
	if arr, ok := body["deleted_paths"].([]interface{}); ok {
		var filtered []string
		for _, it := range arr {
			if p, ok := it.(string); ok && !dmSyncExcluded(p) {
				filtered = append(filtered, p)
			}
		}
		opts.DeletedPaths = filtered
	}
	result, err := dmPullFiles(dir, remoteFiles, opts)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	writeJSON(w, 200, result)
}

func (h *DMHandlers) push(w http.ResponseWriter, q map[string]string, body map[string]interface{}) {
	dir, ok := h.projectIdOk(w, q)
	if !ok {
		return
	}
	tree, werr := dmWalkTree(dir, "")
	if werr != nil {
		writeJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	remoteRaw, _ := body["remote_files"].([]interface{})
	remoteSet := map[string]bool{}
	for _, it := range remoteRaw {
		if m, ok := it.(map[string]interface{}); ok {
			if p, ok := m["relative_path"].(string); ok {
				remoteSet[p] = true
			}
		}
	}
	var toUpload []string
	for _, e := range tree.Entries {
		p, _ := e["relative_path"].(string)
		if !remoteSet[p] {
			toUpload = append(toUpload, p)
		}
	}
	writeJSON(w, 501, map[string]interface{}{
		"error":           "datamanipulator is a local-filesystem service and has no remote transport; push (upload) is not implemented",
		"to_upload_count": len(toUpload),
		"to_upload_paths": toUpload,
	})
}

func (h *DMHandlers) compare(w http.ResponseWriter, body map[string]interface{}) {
	left, lok := body["left_tree"].(map[string]interface{})
	right, rok := body["right_tree"].(map[string]interface{})
	if !lok || !rok {
		writeJSON(w, 400, map[string]string{"error": "Missing left_tree or right_tree"})
		return
	}
	toTree := func(m map[string]interface{}) DMTreeResult {
		t := DMTreeResult{Entries: []DMTreeEntry{}}
		for _, e := range dmAsEntrySlice(m["entries"]) {
			t.Entries = append(t.Entries, e)
		}
		if tf, ok := m["totalFiles"].(float64); ok {
			t.TotalFiles = int(tf)
		}
		return t
	}
	writeJSON(w, 200, dmCompareTrees(toTree(left), toTree(right)))
}

func (h *DMHandlers) syncFull(w http.ResponseWriter, q map[string]string, body map[string]interface{}) {
	dir, ok := h.projectIdOk(w, q)
	if !ok {
		return
	}
	remoteRaw, _ := body["remote_files"].([]interface{})
	remoteFiles := []map[string]interface{}{}
	for _, it := range remoteRaw {
		if m, ok := it.(map[string]interface{}); ok {
			if p, _ := m["relative_path"].(string); !dmSyncExcluded(p) {
				remoteFiles = append(remoteFiles, m)
			}
		}
	}
	result, err := dmFullSync(dir, remoteFiles)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	writeJSON(w, 200, result)
}
