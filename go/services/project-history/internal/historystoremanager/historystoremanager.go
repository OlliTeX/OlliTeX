// Package historystoremanager is the 1:1 port of
// services/project-history/app/js/HistoryStoreManager.js (660 L) — the
// history-v1 service client (chunks, versions, blobs, project lifecycle).
//
// Seams (vendor module-level imports, mocked via esmock in the Node suite):
//
//	Client     — fetch-utils (fetchString/fetchStream/fetchNothing)
//	Settings   — overleaf.history (host/auth/timeout), apis.filestore,
//	             path.uploadFolder
//	Range      — HistoryBlobTranslator.createRangeBlobDataFromUpdate (C4)
//	FileWriter — LocalFileWriter.bufferOnDisk (C13)
//	Hasher     — HashManager._getBlobHash (B5)
//
// Faithful error envelopes:
//
//	E1 non-2xx → OError "history store a non-success status code: <status>",
//	   props {method, url, qs, statusCode, body} — body ONLY present when
//	   status ∈ [400, 409, 413, 422] (vendor _requestHistoryService).
//	E2 requestChunk invalid payload → OError('unexpected response', {path}).
//	E3 initializeProject missing id → OError(msg, code=<id>) (second arg is
//	   the OError CODE, not info).
//	E4 sendChanges failure → tag(err, 'failed to send changes to v1',
//	   {projectId, historyId, endVersion, errorCode, statusCode, errorBody}).
package historystoremanager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"time"

	"ollitex/go/services/project-history/internal/versions"
)

// Settings — vendor Settings.* surface used by this module.
type Settings struct {
	HistoryHost      string
	HistoryUser      string
	HistoryPass      string
	FilestoreURL     string // Settings.apis.filestore.url
	FilestoreEnabled bool   // Settings.apis.filestore.enabled
	RequestTimeout   time.Duration
	UploadFolder     string // Settings.path.uploadFolder
}

// Client is the fetch-utils seam.
//
// Do — fetchString/_requestHistoryService/PUT:
//   - transport failure → (nil, 0, err)
//   - any HTTP response → (body, status, nil); the port maps non-2xx to E1/E4.
//
// Head — fetchNothing(HEAD) for _checkBlobExists: (status, nil) for any
// response including 404; (0, err) on transport failure.
type Client interface {
	Do(ctx context.Context, method, url string, query map[string]string, header map[string]string, body []byte) ([]byte, int, error)
	Head(ctx context.Context, url string, header map[string]string) (int, error)
}

// Range — HistoryBlobTranslator seam (C4).
type Range interface {
	CreateRangeBlobDataFromUpdate(update map[string]any) (data map[string]any, found bool, err error)
}

// FileWriter — LocalFileWriter.bufferOnDisk seam (C13).
type FileWriter interface {
	WriteToDisk(ctx context.Context, data []byte, url, fileID string) (fsPath string, err error)
}

// Hasher — HashManager._getBlobHash.
type Hasher interface {
	GetBlobHash(path string) (hash string, byteLength int64, err error)
}

type defaultHasher struct{}

func (defaultHasher) GetBlobHash(path string) (string, int64, error) {
	h, l, e := hasherFunc(path)
	return h, l, e
}

// hasherFunc is the HashManager hook (wired to hashmanager at link time via
// SetHasher to avoid an import cycle at package init).
var hasherFunc = func(path string) (string, int64, error) {
	return "", 0, errNotWired
}

var errNotWired = oError{msg: "hash manager not wired"}

// SetHasher wires the HashManager implementation (default).
func SetHasher(fn func(path string) (string, int64, error)) { hasherFunc = fn }

// Deps — injectable module-level imports.
type Deps struct {
	Client     Client
	Settings   Settings
	Range      Range
	FileWriter FileWriter
	Hasher     Hasher
	// FileStore is the fetchStream(filestoreURL) seam (vendor branch 2).
	FileStore FileStoreReader
	// Mocks mirrors vendor `export const _mocks = {}`.
	Mocks Mocks
}

// FileStoreReader — the vendor fetchStream(filestoreURL) seam (branch 2
// reads the requested file over HTTP; 404 → *FileStoreStatus{404}).
type FileStoreReader interface {
	FileStoreRead(ctx context.Context, filestoreURL string) ([]byte, error)
}

// Mocks — vendor _mocks container (getMostRecentChunk substitution point).
type Mocks struct {
	GetMostRecentChunk func(projectID, historyID string) (map[string]any, error)
}

func (d *Deps) withDefaults() *Deps {
	if d == nil {
		d = &Deps{}
	}
	if d.Hasher == nil {
		d.Hasher = defaultHasher{}
	}
	return d
}

// oError — generic OError(message, code?, info?).
type oError struct {
	msg  string
	code string
	info map[string]any
}

func (e oError) Error() string        { return e.msg }
func (e oError) Code() string         { return e.code }
func (e oError) Info() map[string]any { return e.info }

func oerr(msg string) oError { return oError{msg: msg} }

func oerrInfo(msg string, info map[string]any) oError {
	return oError{msg: msg, info: info}
}

func oerrCode(msg, code string, info map[string]any) oError {
	return oError{msg: msg, code: code, info: info}
}

// oerrTag — vendor OError.tag(err, message, info) (message may be "").
func oerrTag(err error, message string, info map[string]any) error {
	if err == nil {
		return nil
	}
	oe, _ := asOError(err)
	if message != "" {
		oe.msg = message // vendor OError.tag: error.message = message
	}
	if message == "" && info == nil {
		return oe
	}
	merged := map[string]any{}
	for k, v := range oe.info {
		merged[k] = v
	}
	for k, v := range info {
		merged[k] = v
	}
	oe.info = merged
	return oe
}

// requestError — E1 envelope (status + body for E4 props).
type requestError struct {
	oError
	status int
	body   string
}

func (e requestError) StatusCode() int  { return e.status }
func (e requestError) PropBody() string { return e.body }

// RawVersion — vendor `getMostRecentVersionRaw` result.
type RawVersion struct {
	StartVersion int
	EndVersion   int
	EndTimestamp time.Time
}

// historyHeader — basic auth (vendor getHistoryFetchOptions).
func (d *Deps) historyHeader() map[string]string {
	h := map[string]string{}
	if d.Settings.HistoryUser != "" || d.Settings.HistoryPass != "" {
		h["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(d.Settings.HistoryUser+":"+d.Settings.HistoryPass))
	}
	return h
}

func (d *Deps) historyURL(path string) string {
	return d.Settings.HistoryHost + "/" + path
}

// requestHistoryService — vendor _requestHistoryService.
//
// Returns (parsed, raw, err): when jsonBody != nil (vendor `useJson =
// options.json != null`) a non-empty 2xx body is JSON-decoded into parsed
// (vendor `if (useJson && body) callback(null, JSON.parse(body))`).
func (d *Deps) requestHistoryService(ctx context.Context, method, path string, query map[string]string, jsonBody any) (any, []byte, error) {
	if method == "" {
		method = "GET"
	}
	header := d.historyHeader()
	var body []byte
	if jsonBody != nil {
		b, err := json.Marshal(jsonBody)
		if err != nil {
			return nil, nil, oerr("failed to encode request body")
		}
		body = b
		header["Content-Type"] = "application/json"
	}
	respBody, status, err := d.Client.Do(ctx, method, d.historyURL(path), query, header, body)
	if err != nil {
		return nil, nil, oerrTag(err, "", nil)
	}
	if status < 200 || status > 299 {
		e := requestError{
			oError: oerrInfo("history store a non-success status code: "+itoa(status), map[string]any{
				"method":     method,
				"url":        d.historyURL(path),
				"qs":         query,
				"statusCode": status,
			}),
			status: status,
			body:   string(respBody),
		}
		if status == 400 || status == 409 || status == 413 || status == 422 {
			e.info["body"] = string(respBody)
		} else {
			e.body = ""
			e.info["body"] = nil
		}
		return nil, nil, e
	}
	var parsed any
	if jsonBody != nil && len(respBody) > 0 {
		// vendor: `if (useJson && body) callback(null, JSON.parse(body))`
		var decoded any
		if e := json.Unmarshal(respBody, &decoded); e == nil {
			parsed = decoded
		}
	}
	return parsed, respBody, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// GetMostRecentChunk — vendor getMostRecentChunk (through _mocks).
func (d *Deps) GetMostRecentChunk(ctx context.Context, projectID, historyID string) (map[string]any, error) {
	d = d.withDefaults()
	if d.Mocks.GetMostRecentChunk != nil {
		return d.Mocks.GetMostRecentChunk(projectID, historyID)
	}
	return d.requestChunk(ctx, "projects/"+historyID+"/latest/history")
}

// GetChunkAtVersion — vendor getChunkAtVersion.
func (d *Deps) GetChunkAtVersion(ctx context.Context, projectID, historyID string, version int) (map[string]any, error) {
	d = d.withDefaults()
	return d.requestChunk(ctx, "projects/"+historyID+"/versions/"+itoa(version)+"/history")
}

// requestChunk — vendor _requestChunk (E2 validation).
func (d *Deps) requestChunk(ctx context.Context, path string) (map[string]any, error) {
	parsed, _, err := d.requestHistoryService(ctx, "GET", path, nil, true)
	if err != nil {
		return nil, err
	}
	chunk, _ := parsed.(map[string]any)
	if chunk == nil {
		return nil, oerrInfo("unexpected response", map[string]any{"path": path})
	}
	inner, _ := chunk["chunk"].(map[string]any)
	if inner == nil {
		return nil, oerrInfo("unexpected response", map[string]any{"path": path})
	}
	if _, has := inner["startVersion"]; !has {
		return nil, oerrInfo("unexpected response", map[string]any{"path": path})
	}
	return chunk, nil
}

// listChanges — `chunk.chunk.history.changes || []` as a map slice.
func listChanges(v any) []map[string]any {
	lst, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(lst))
	for _, e := range lst {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func tsMillis(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case string:
		var out int64
		dig := false
		for _, r := range n {
			if r >= '0' && r <= '9' {
				out = out*10 + int64(r-'0')
				dig = true
			} else {
				break
			}
		}
		if dig {
			return out
		}
	}
	return 0
}

// GetMostRecentVersion — vendor getMostRecentVersion
// (err1 || err2; both scans always run).
func (d *Deps) GetMostRecentVersion(ctx context.Context, projectID, historyID string) (
	version int,
	psdv map[string]any,
	lastChange map[string]any,
	chunk map[string]any,
	err error,
) {
	d = d.withDefaults()
	chunk, err = d.GetMostRecentChunk(ctx, projectID, historyID)
	if err != nil {
		return 0, nil, nil, nil, err
	}
	inner, _ := chunk["chunk"].(map[string]any)
	var changes []map[string]any
	var snapshot map[string]any
	if history, ok := inner["history"].(map[string]any); ok {
		changes = listChanges(history["changes"])
		snapshot, _ = history["snapshot"].(map[string]any)
	}
	startVersion := 0
	if v, ok := toIntAny(inner["startVersion"]); ok {
		startVersion = v
	}
	version = startVersion + len(changes)
	if len(changes) > 0 {
		best, bestTS := 0, tsMillis(changes[0]["timestamp"])
		for i := range changes {
			if ts := tsMillis(changes[i]["timestamp"]); ts >= bestTS {
				best, bestTS = i, ts
			}
		}
		lastChange = changes[best]
	}
	projectVersion, err1 := latestProjectVersion(projectID, inner, snapshot, changes)
	v2Docs, err2 := latestV2DocVersions(projectID, inner, snapshot, changes)
	psdv = map[string]any{
		"project": projectVersion,
		"docs":    v2Docs,
	}
	if err1 != nil {
		err = err1
	} else if err2 != nil {
		err = err2
	}
	return
}

// latestProjectVersion — vendor _getLatestProjectVersion (first-error kept).
func latestProjectVersion(projectID string, inner, snapshot map[string]any, changes []map[string]any) (any, error) {
	var projectVersion any
	if snapshot != nil {
		projectVersion = snapshot["projectVersion"]
	}
	chunkStartVersion := 0
	if v, ok := toIntAny(inner["startVersion"]); ok {
		chunkStartVersion = v
	}
	projectVersionInSnapshot := projectVersion
	var firstErr error
	for changeIdx, change := range changes {
		pvInChange, has := change["projectVersion"]
		if !has || pvInChange == nil {
			continue
		}
		if projectVersion != nil {
			if a, okA := pvInChange.(string); okA {
				if b, okB := projectVersion.(string); okB && versions.LT(a, b) {
					if firstErr == nil {
						firstErr = oerrInfo("project structure version out of order", map[string]any{
							"projectId":                projectID,
							"chunkStartVersion":        chunkStartVersion,
							"projectVersionInSnapshot": projectVersionInSnapshot,
							"changeIdx":                changeIdx,
							"projectVersion":           projectVersion,
							"projectVersionInChange":   pvInChange,
						})
					}
					continue
				}
			}
		}
		projectVersion = pvInChange
	}
	return projectVersion, firstErr
}

// latestV2DocVersions — vendor _getLatestV2DocVersions.
func latestV2DocVersions(projectID string, inner, snapshot map[string]any, changes []map[string]any) (map[string]any, error) {
	v2DocVersions := map[string]any{}
	if snapshot != nil {
		if v2, ok := snapshot["v2DocVersions"].(map[string]any); ok {
			for k, v := range v2 {
				v2DocVersions[k] = v
			}
		}
	}
	var firstErr error
	for _, change := range changes {
		v2cv, has := change["v2DocVersions"]
		if !has || v2cv == nil {
			continue
		}
		docVersions, ok := v2cv.(map[string]any)
		if !ok {
			continue
		}
		for docID, docInfoRaw := range docVersions {
			docInfo, ok := docInfoRaw.(map[string]any)
			if !ok {
				continue
			}
			v, hasV := docInfo["v"]
			if !hasV {
				continue
			}
			if prev, exists := v2DocVersions[docID]; exists {
				prevInfo, okI := prev.(map[string]any)
				prevV, hasPV := prevInfo["v"]
				if okI && hasPV {
					if a, okA := v.(string); okA {
						if b, okB := prevV.(string); okB && versions.LT(a, b) {
							if firstErr == nil {
								firstErr = oerrInfo("doc version out of order", map[string]any{})
							}
							continue
						}
					}
				}
			}
			v2DocVersions[docID] = docInfo
		}
	}
	return v2DocVersions, firstErr
}

// GetMostRecentVersionRaw — vendor getMostRecentVersionRaw
// (readOnly → qs {readOnly: true}; result {startVersion, endVersion,
// endTimestamp: Date}).
func (d *Deps) GetMostRecentVersionRaw(ctx context.Context, projectID, historyID string, readOnly bool) (RawVersion, error) {
	d = d.withDefaults()
	var query map[string]string
	if readOnly {
		query = map[string]string{"readOnly": "true"}
	}
	parsed, _, err := d.requestHistoryService(ctx, "GET", "projects/"+historyID+"/latest/history/raw", query, true)
	if err != nil {
		return RawVersion{}, err
	}
	payload, _ := parsed.(map[string]any)
	if payload == nil {
		return RawVersion{}, oerr("unexpected response")
	}
	sv := 0
	if v, ok := toIntAny(payload["startVersion"]); ok {
		sv = v
	}
	av := 0
	if v, ok := toIntAny(payload["endVersion"]); ok {
		av = v
	}
	var et time.Time
	if s, ok := payload["endTimestamp"].(string); ok {
		if t, e := time.Parse(time.RFC3339Nano, s); e == nil {
			et = t
		}
	}
	return RawVersion{StartVersion: sv, EndVersion: av, EndTimestamp: et}, nil
}

// GetProjectBlob — vendor getProjectBlob (non-JSON: raw body).
func (d *Deps) GetProjectBlob(ctx context.Context, historyID, blobHash string) (string, error) {
	d = d.withDefaults()
	_, body, err := d.requestHistoryService(ctx, "GET", "projects/"+historyID+"/blobs/"+blobHash, nil, nil)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// GetProjectBlobStream — vendor getProjectBlobStream (streaming seam: the
// port returns the full body — the services that consume it read to EOF).
func (d *Deps) GetProjectBlobStream(ctx context.Context, historyID, blobHash string) ([]byte, error) {
	d = d.withDefaults()
	header := d.historyHeader()
	respBody, status, err := d.Client.Do(ctx, "GET", d.historyURL("projects/"+historyID+"/blobs/"+blobHash), nil, header, nil)
	if err != nil {
		return nil, oerrTag(err, "", nil)
	}
	if status < 200 || status > 299 {
		return nil, requestError{
			oError: oerrInfo("history store a non-success status code: "+itoa(status), map[string]any{
				"method":     "GET",
				"url":        d.historyURL("projects/" + historyID + "/blobs/" + blobHash),
				"qs":         nil,
				"statusCode": status,
			}),
			status: status,
			body:   string(respBody),
		}
	}
	return respBody, nil
}

// SendChanges — vendor sendChanges (E4 error envelope; success →
// {resyncNeeded: response?.resyncNeeded ?? false}).
func (d *Deps) SendChanges(ctx context.Context, projectID, historyID string, changes []any, endVersion int) (bool, error) {
	d = d.withDefaults()
	query := map[string]string{"end_version": itoa(endVersion)}
	parsed, _, err := d.requestHistoryService(ctx, "POST", "projects/"+historyID+"/legacy_changes", query, changes)
	if err != nil {
		// E4: vendor reads error.code / error.statusCode / error.body.
		info := map[string]any{
			"projectId":  projectID,
			"historyId":  historyID,
			"endVersion": endVersion,
		}
		if oe, okE := asOError(err); okE {
			if c := oe.Code(); c != "" {
				info["errorCode"] = c
			}
		}
		if re, ok := err.(requestError); ok {
			info["statusCode"] = re.StatusCode()
			if re.PropBody() != "" {
				info["errorBody"] = re.PropBody()
			}
		}
		return false, oerrTag(err, "failed to send changes to v1", info)
	}
	resyncNeeded := false
	if m, ok := parsed.(map[string]any); ok {
		if v, has := m["resyncNeeded"]; has && v != nil {
			resyncNeeded, _ = v.(bool)
		}
	}
	return resyncNeeded, nil
}

// --- blob creation (vendor createBlob* block) ------------------------------

var filestorePathRE = regexp.MustCompile(`^/project/([0-9a-f]{24})/file/([0-9a-f]{24})$`)

// rewriteFilestoreUrl — vendor _rewriteFilestoreUrl.
//
// Faithful split: url empty → (nil, nil, nil) (the caller then raises
// 'no filestore URL provided and blob was not created' when the blob does
// not exist); bad pathname → 'invalid file for blob creation'; project
// mismatch → 'invalid project for blob creation'.
func (d *Deps) rewriteFilestoreUrl(updateURL, projectID string) (fileID *string, filestoreURL *string, err error) {
	if updateURL == "" {
		return nil, nil, nil
	}
	// vendor: const { pathname } = new URL(url) — extract the path portion
	// (scheme://host stripped, query/hash dropped) before running the
	// vendor regex.
	rest := updateURL
	if schemeIdx := strings.Index(rest, "://"); schemeIdx != -1 && isScheme(rest[:schemeIdx]) {
		rest = rest[schemeIdx+3:] // drop scheme://
		if slashIdx := strings.Index(rest, "/"); slashIdx >= 0 {
			rest = rest[slashIdx:] // drop host
		} else {
			rest = ""
		}
	} else if strings.Count(rest, "://") == 0 && strings.Contains(rest, "://") {
		rest = updateURL
	}
	pathname := rest
	if i := strings.IndexAny(pathname, "?#"); i >= 0 {
		pathname = pathname[:i]
	}
	if i := strings.Index(pathname, "#"); i >= 0 {
		pathname = pathname[:i]
	}
	m := filestorePathRE.FindStringSubmatch(pathname)
	if m == nil {
		return nil, nil, oerr("invalid file for blob creation")
	}
	if m[1] != projectID {
		return nil, nil, oerr("invalid project for blob creation")
	}
	fid := m[2]
	furl := d.Settings.FilestoreURL + "/project/" + projectID + "/file/" + m[2]
	return &fid, &furl, nil
}

// checkBlobExists — vendor _checkBlobExists (HEAD; 404 → false).
func (d *Deps) checkBlobExists(ctx context.Context, historyID, blobHash string) (bool, error) {
	if blobHash == "" {
		return false, nil
	}
	status, err := d.Client.Head(ctx, d.historyURL("projects/"+historyID+"/blobs/"+blobHash), d.historyHeader())
	if err != nil {
		return false, oerrTag(err, "", nil)
	}
	if status == 404 {
		return false, nil
	}
	return true, nil
}

// createBlob — vendor _createBlob: hash the temp file, PUT to the history
// service with Content-Length, return the hash.
func (d *Deps) createBlob(ctx context.Context, historyID, fsPath string) (string, error) {
	hash, byteLength, err := d.Hasher.GetBlobHash(fsPath)
	if err != nil {
		return "", oerrTag(err, "", nil)
	}
	var content []byte
	if b, e := readFileAll(fsPath); e == nil {
		content = b
	}
	header := d.historyHeader()
	header["Content-Length"] = itoa(int(byteLength))
	respBody, status, err := d.Client.Do(ctx, "PUT", d.historyURL("projects/"+historyID+"/blobs/"+hash), nil, header, content)
	if err != nil {
		return "", oerrTag(err, "", nil)
	}
	if status < 200 || status > 299 {
		return "", requestError{
			oError: oerrInfo("history store a non-success status code: "+itoa(status), map[string]any{
				"method":     "PUT",
				"url":        d.historyURL("projects/" + historyID + "/blobs/" + hash),
				"qs":         nil,
				"statusCode": status,
			}),
			status: status,
			body:   string(respBody),
		}
	}
	return hash, nil
}

// createBlobFromString — vendor createBlobFromString.
func (d *Deps) createBlobFromString(ctx context.Context, historyID, data, fileID string) (string, error) {
	fsPath, err := d.FileWriter.WriteToDisk(ctx, []byte(data), "", fileID)
	if err != nil {
		return "", oerrTag(err, "", nil)
	}
	return d.createBlob(ctx, historyID, fsPath)
}

// CreateBlobForUpdate — vendor createBlobForUpdate (branch structure kept
// verbatim, including 'invalid update for blob creation' fallthrough).
func (d *Deps) CreateBlobForUpdate(ctx context.Context, projectID, historyID string, update map[string]any) (map[string]any, error) {
	d = d.withDefaults()
	docOK := update["doc"] != nil && update["docLines"] != nil
	if docOK {
		docLinesStr, _ := update["docLines"].(string)
		var ranges map[string]any
		rangeErr := error(nil)
		if d.Range != nil {
			data, found, err := d.Range.CreateRangeBlobDataFromUpdate(update)
			if err != nil {
				return nil, err
			}
			if found {
				ranges = data
			}
		} else {
			rangeErr = oerr("range translator not wired")
		}
		if rangeErr != nil {
			return nil, oerrTag(rangeErr, "", nil)
		}
		fileHash, err := d.createBlobFromString(ctx, historyID, docLinesStr, "project-"+projectID+"-doc-"+strOf(update, "doc"))
		if err != nil {
			return nil, oerrTag(err, "", nil)
		}
		if ranges != nil {
			rangesJSON, e := json.Marshal(ranges)
			if e != nil {
				return nil, oerr("failed to encode ranges")
			}
			rangesHash, err := d.createBlobFromString(ctx, historyID, string(rangesJSON), "project-"+projectID+"-doc-"+strOf(update, "doc")+"-ranges")
			if err != nil {
				return nil, oerrTag(err, "", nil)
			}
			return map[string]any{"file": fileHash, "ranges": rangesHash}, nil
		}
		return map[string]any{"file": fileHash}, nil
	} else if update["file"] != nil && (update["url"] != nil || createdBlobAny(update)) {
		fileID, filestoreURL, err := d.rewriteFilestoreUrl(strOf(update, "url"), projectID)
		if err != nil {
			return nil, err
		}
		fileIDStr := ""
		if fileID != nil {
			fileIDStr = *fileID
		}
		blobExists, err := d.checkBlobExists(ctx, historyID, strOf(update, "hash"))
		if err != nil {
			return nil, oerrInfo("error checking whether blob exists", map[string]any{
				"projectId": projectID,
				"historyId": historyID,
				"update":    update,
			})
		}
		if blobExists {
			return map[string]any{"file": strOf(update, "hash")}, nil
		}
		if !createdBlobAny(update) {
			// vendor: warn branch (no-op semantically)
		}
		if filestoreURL == nil {
			return nil, oerr("no filestore URL provided and blob was not created")
		}
		if !d.Settings.FilestoreEnabled {
			return nil, oerrInfo("blocking filestore read", map[string]any{"update": update})
		}
		// vendor: fetchStream(filestoreURL) — the Go seam reads the file.
		if d.FileStore == nil {
			return nil, oerr("filestore read not wired")
		}
		content, ferr := d.FileStore.FileStoreRead(ctx, *filestoreURL)
		if ferr != nil {
			if sf, ok := ferr.(*FileStoreStatus); ok && sf.Status == 404 {
				// vendor: store as an empty file
				fsPath, werr := d.FileWriter.WriteToDisk(ctx, []byte{}, *filestoreURL, "project-"+projectID+"-file-"+fileIDStr)
				if werr != nil {
					return nil, oerrTag(werr, "", nil)
				}
				fileHash, cerr := d.createBlob(ctx, historyID, fsPath)
				if cerr != nil {
					return nil, oerrTag(cerr, "", nil)
				}
				return map[string]any{"file": fileHash}, nil
			}
			return nil, oerrTag(ferr, "error from filestore", map[string]any{"filestoreURL": *filestoreURL})
		}
		fsPath, werr := d.FileWriter.WriteToDisk(ctx, content, *filestoreURL, "project-"+projectID+"-file-"+fileIDStr)
		if werr != nil {
			return nil, oerrTag(werr, "", nil)
		}
		fileHash, cerr := d.createBlob(ctx, historyID, fsPath)
		if cerr != nil {
			return nil, oerrTag(cerr, "", nil)
		}
		if h := strOf(update, "hash"); h != "" && h != fileHash {
			// vendor: 'hash mismatch between web and project-history' (warn only)
		}
		return map[string]any{"file": fileHash}, nil
	}
	return nil, oerr("invalid update for blob creation")
}

// FileStoreStatus — vendor RequestFailedError.status for filestore reads.
type FileStoreStatus struct{ Status int }

func (f *FileStoreStatus) Error() string {
	return "filestore status " + itoa(f.Status)
}

// InitializeProject — vendor initializeProject (E3: `new OError(msg, id)` —
// the second argument is the OError CODE).
func (d *Deps) InitializeProject(ctx context.Context, historyID *string) (string, error) {
	d = d.withDefaults()
	var payload any
	if historyID == nil {
		payload = true
	} else {
		payload = map[string]any{"projectId": *historyID}
	}
	parsed, _, err := d.requestHistoryService(ctx, "POST", "projects", nil, payload)
	if err != nil {
		return "", oerrTag(err, "", nil)
	}
	project, _ := parsed.(map[string]any)
	id, has := project["projectId"]
	idStr, _ := id.(string)
	if !has || id == nil || idStr == "" {
		return "", oerrCode("history store did not return a project id", idStr, nil)
	}
	return idStr, nil
}

// DeleteProject — vendor deleteProject.
func (d *Deps) DeleteProject(ctx context.Context, projectID string) error {
	d = d.withDefaults()
	_, _, err := d.requestHistoryService(ctx, "DELETE", "projects/"+projectID, nil, nil)
	return err
}

// BlobStore — vendor BlobStore extends BlobStoreBase: fetchString.
type BlobStore struct {
	projectID string
	d         *Deps
}

// FetchString — vendor BlobStore.fetchString.
func (b *BlobStore) FetchString(ctx context.Context, hash string) (string, error) {
	return b.d.GetProjectBlob(ctx, b.projectID, hash)
}

// GetBlobStore — vendor getBlobStore.
func (d *Deps) GetBlobStore(projectID string) *BlobStore {
	d = d.withDefaults()
	return &BlobStore{projectID: projectID, d: d}
}

// CloneProject — vendor _cloneProject (POST clone with {targetProjectId},
// returns the streamed body).
func (d *Deps) CloneProject(ctx context.Context, sourceProjectID, targetProjectID string) ([]byte, error) {
	d = d.withDefaults()
	_, raw, err := d.requestHistoryService(ctx, "POST", "projects/"+sourceProjectID+"/clone", nil, map[string]any{"targetProjectId": targetProjectID})
	return raw, err
}

func strOf(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if key == "" {
		return ""
	}
	s2, _ := m[key].(string)
	return s2
}

func readFileAll(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func createdBlobAny(update map[string]any) bool {
	b, _ := update["createdBlob"].(bool)
	return b
}

func isScheme(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '+', r == '-', r == '.':
			if i == 0 && (r >= '0' && r <= '9') {
				return false
			}
			if i == 0 && (r == '+' || r == '-' || r == '.') {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// toIntAny — JSON numbers decode as float64; JS ints decode as int on the
// Go-side fakes. Accept both (vendor JS: both are "number").
func toIntAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n == float64(int64(n)) {
			return int(n), true
		}
		return 0, false
	}
	return 0, false
}

// asOError — the vendor OError "interface" on errors (requestError embeds it;
// plain oError values pass through).
func asOError(err error) (oError, bool) {
	switch e := err.(type) {
	case oError:
		return e, true
	case requestError:
		return e.oError, true
	case *requestError:
		return e.oError, true
	case *oError:
		return *e, true
	}
	return oError{}, false
}

// RequestFailedStatus — the vendor fetch-utils RequestFailedError accessor
// used by HttpController.getProjectBlob (`err instanceof RequestFailedError
// && err.response.status === 404`). Returns 0 when err is not one of ours.
func RequestFailedStatus(err error) int {
	var re requestError
	if errors.As(err, &re) {
		return re.status
	}
	var rp *requestError
	if errors.As(err, &rp) {
		return rp.status
	}
	return 0
}
