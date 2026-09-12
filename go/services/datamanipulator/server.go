package datamanipulator

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	pbhttp "ollitex/go/pbhttp"
)

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
	auth := pbhttp.RequireServiceToken(cfg.ServiceToken, func() {
		fmt.Println("SHARED_SERVICE_TOKEN is unset; accepting unauthenticated requests (should be restricted to in-container callers)")
	})
	dispatch := auth(func(w http.ResponseWriter, r *http.Request) {
		handlers.route(w, r)
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		pbhttp.WriteJSON(w, 200, map[string]string{"status": "ok", "service": "datamanipulator"})
	})
	mux.HandleFunc("/tree", dispatch)
	mux.HandleFunc("/files", dispatch)
	mux.HandleFunc("/file", dispatch)
	mux.HandleFunc("/pull", dispatch)
	mux.HandleFunc("/push", dispatch)
	mux.HandleFunc("/compare", dispatch)
	mux.HandleFunc("/sync/full", dispatch)
	// 1:1 with Node express.json({ limit: '10mb' }).
	return pbhttp.LimitBody(mux, 10<<20)
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
		pbhttp.WriteJSON(w, 404, map[string]string{"error": "not found"})
	}
}

func (h *DMHandlers) projectIdOk(w http.ResponseWriter, q map[string]string) (string, bool) {
	id, _ := q["project_id"]
	if !dmIsValidProjectId(id) {
		pbhttp.WriteJSON(w, 400, map[string]string{"error": "Invalid project_id format"})
		return "", false
	}
	dir, err := h.projectDir(id)
	if err != nil {
		pbhttp.WriteJSON(w, 400, map[string]string{"error": "Invalid project_id"})
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
			pbhttp.WriteJSON(w, 404, map[string]string{"error": werr.Error()})
			return
		}
		if _, isDir := errAs[*DMDirectoryNotFoundError](werr); isDir {
			pbhttp.WriteJSON(w, 404, map[string]string{"error": werr.Error()})
			return
		}
		pbhttp.WriteJSON(w, 500, map[string]string{"error": "Internal server error"})
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
		pbhttp.WriteJSON(w, 200, map[string]interface{}{"path": relPath, "entries": filtered})
		return
	}
	pbhttp.WriteJSON(w, 200, map[string]interface{}{
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
		pbhttp.WriteJSON(w, 400, map[string]string{"error": "Missing path parameter"})
		return
	}
	data, err := dmReadFile(dir, relPath)
	if err != nil {
		if _, isFile := errAs[*DMFileNotFoundError](err); isFile {
			pbhttp.WriteJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		if _, isDir := errAs[*DMDirectoryNotFoundError](err); isDir {
			pbhttp.WriteJSON(w, 404, map[string]string{"error": err.Error()})
			return
		}
		pbhttp.WriteJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	if _, hasCB := data["content_base64"]; !hasCB {
		data["content_base64"] = ""
	}
	pbhttp.WriteJSON(w, 200, data)
}

func (h *DMHandlers) filePost(w http.ResponseWriter, q map[string]string, body map[string]interface{}) {
	dir, ok := h.projectIdOk(w, q)
	if !ok {
		return
	}
	relPath, _ := q["path"]
	if relPath == "" {
		pbhttp.WriteJSON(w, 400, map[string]string{"error": "Missing path parameter"})
		return
	}
	cb, _ := body["content_base64"].(string)
	if cb == "" {
		pbhttp.WriteJSON(w, 400, map[string]string{"error": "Missing content in request body"})
		return
	}
	content, derr := base64.StdEncoding.DecodeString(cb)
	if derr != nil {
		pbhttp.WriteJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	meta, werr := dmWriteFile(dir, relPath, content)
	if werr != nil {
		pbhttp.WriteJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	meta["content_base64"] = ""
	pbhttp.WriteJSON(w, 200, meta)
}

func (h *DMHandlers) fileDelete(w http.ResponseWriter, q map[string]string) {
	dir, ok := h.projectIdOk(w, q)
	if !ok {
		return
	}
	relPath, _ := q["path"]
	if relPath == "" {
		pbhttp.WriteJSON(w, 400, map[string]string{"error": "Missing path parameter"})
		return
	}
	if derr := dmDeletePath(dir, relPath); derr != nil {
		if _, isFile := errAs[*DMFileNotFoundError](derr); isFile {
			pbhttp.WriteJSON(w, 404, map[string]string{"error": derr.Error()})
			return
		}
		pbhttp.WriteJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	pbhttp.WriteJSON(w, 200, map[string]interface{}{"success": true})
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
		pbhttp.WriteJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	pbhttp.WriteJSON(w, 200, result)
}

func (h *DMHandlers) push(w http.ResponseWriter, q map[string]string, body map[string]interface{}) {
	dir, ok := h.projectIdOk(w, q)
	if !ok {
		return
	}
	tree, werr := dmWalkTree(dir, "")
	if werr != nil {
		pbhttp.WriteJSON(w, 500, map[string]string{"error": "Internal server error"})
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
	pbhttp.WriteJSON(w, 501, map[string]interface{}{
		"error":           "datamanipulator is a local-filesystem service and has no remote transport; push (upload) is not implemented",
		"to_upload_count": len(toUpload),
		"to_upload_paths": toUpload,
	})
}

func (h *DMHandlers) compare(w http.ResponseWriter, body map[string]interface{}) {
	left, lok := body["left_tree"].(map[string]interface{})
	right, rok := body["right_tree"].(map[string]interface{})
	if !lok || !rok {
		pbhttp.WriteJSON(w, 400, map[string]string{"error": "Missing left_tree or right_tree"})
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
	pbhttp.WriteJSON(w, 200, dmCompareTrees(toTree(left), toTree(right)))
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
		pbhttp.WriteJSON(w, 500, map[string]string{"error": "Internal server error"})
		return
	}
	pbhttp.WriteJSON(w, 200, result)
}
