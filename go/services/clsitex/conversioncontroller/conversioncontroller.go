// Package conversioncontroller ports services/clsi/app/js/ConversionController.js
// (1:1 with the Node source; divergences documented per-handler and here).
//
// Handler contract (mirrors Node expressify): each handler writes the final
// response itself (JSON, plain status, or the streamed document) and returns
// (code, err); a non-nil err is Node's next(error) (the error middleware
// renders the final status), and code is informational on the handled paths
// (the response is already written).
//
// Handler-local error mapping (the three Node catch blocks):
//   - convertDocxToLaTeX / convertDocumentToLaTeX / convertProjectToDocument:
//     ConversionError -> 422 {error, exitCode} (user-facing) or 422 {}
//     (non-user-facing, logged); MissingUpdatesError (history sync) -> 409
//     {baseHistoryVersion}; any other error returned (-> 500).
//   - convertPDFToJPEG: NO ConversionError catch (that Node try has no catch)
//     — every error is returned (-> 500).
//
// Documented divergences:
//   - Node zod schemas (uploadedFileOnlySchema / convertDocumentToLaTeXSchema
//     / convertPDFToJPEGSchema / convertProjectToDocumentSchema + the
//     convertProjectToDocumentFallbackSchema) validate the query file in the
//     Node layer. The Go port validates the enum fields Node enforces and
//     mirrors the upload unlink on failure (parseUploadedFileReq), forwarded
//     as an error the same way. The request body reuses requestparser (the
//     same compileRequestBodySchema port).
//   - Metrics.inc('convert_project_to_document', ...) is a no-op seam
//     (MetricsInc); the server route owns exposure for now.
//   - Node `await fs.mkdir(projectCacheDir, {recursive:true})` always resolves
//     undefined (Node fs.mkdir), so `if (created)` is always falsy and
//     projectCacheDir is never pushed onto cleanupDirs; the Go port mirrors
//     that (the dir is left behind, as in Node).
//   - The buildId (json branch) is opaque; the Go default is a fresh UUID
//     (Node uses OutputCacheManager.generateBuildId, a uuidv4).
//   - Node parseUploadedFileReq wraps fs.unlink(req.file.path).catch() on
//     parse failure; the Go handler does the same (os.Remove best-effort)
//     before returning the error.
//   - The Go 404 "sendStatus" is rendered plainly (no JSON body), mirroring
//     express res.sendStatus for the feature-gate paths.
package conversioncontroller

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ollitex/go/services/clsitex/conversionoutputcleaner"
	histwriter "ollitex/go/services/clsitex/historyresourcewriter"
	"ollitex/go/services/clsitex/requestparser"
	"ollitex/go/services/clsitex/resourcewriter"
)

// CONVERSION_CONFIGS (Node module constant) — type -> file extension.
var conversionConfigs = map[string]string{
	"docx":     "docx",
	"markdown": "zip",
	"html":     "zip",
}

// ConversionManager is the seam over Node's ConversionManager.promises (the
// three WithLock methods the controller uses). The production
// *conversionmanager.Manager satisfies it directly; tests use a fake.
type ConversionManager interface {
	ConvertToLaTeXWithLock(conversionID, inputPath, conversionType string) (string, error)
	ConvertLaTeXToDocumentInDirWithLock(conversionID, compileDir, rootDocPath, docType string) (string, error)
	ConvertPDFToJPEGWithLock(conversionID, inputPath, mode string) (string, error)
}

// Settings is the Settings surface the handlers read (a minimal mirror of
// the Node Settings this controller touches).
type Settings struct {
	CompilesDir             string
	OutputDir               string
	ClsiCacheDir            string
	EnablePandocConversions bool
	EnablePdfConversions    bool
}

// Controller wires the four conversion route handlers (Node: expressify of
// the module's default export).
type Controller struct {
	// Manager is the ported ConversionManager (Node: ConversionManager).
	Manager ConversionManager

	// Config is the live settings snapshot (Node: Settings read at call time).
	Config Settings

	// ParserConfig feeds requestparser (the compileRequestBodySchema port).
	ParserConfig requestparser.Config

	// NewUUID mirrors crypto.randomUUID.
	NewUUID func() string

	// GenerateBuildId mirrors OutputCacheManager.generateBuildId (opaque
	// unique buildId string).
	GenerateBuildId func() string

	// MetricsInc mirrors Metrics.inc('convert_project_to_document', 1,
	// {compileFromHistory, method}).
	MetricsInc func(compileFromHistory bool, method string)

	// HistorySync mirrors HistoryResourceWriter.promises.syncResourcesToDisk.
	HistorySync func(ctx context.Context, projectID, userID string,
		req *histwriter.Request, compileDir string,
		timings, stats map[string]any) (*histwriter.Result, error)

	// ResourceSync mirrors ResourceWriter.promises.syncResourcesToDisk.
	ResourceSync func(req *resourcewriter.Request, basePath string) ([]resourcewriter.Resource, error)

	// ScheduleOutputCleanup mirrors ConversionOutputCleaner.scheduleCleanup.
	ScheduleOutputCleanup func(outputDir, conversionID string)
}

// New wires the controller (production). Tests construct the struct directly
// and set the seams they exercise. All nil seams get production defaults.
func New(manager ConversionManager, cfg Settings, parserConfig requestparser.Config) *Controller {
	c := &Controller{Manager: manager, Config: cfg, ParserConfig: parserConfig}
	if c.NewUUID == nil {
		c.NewUUID = uuid4
	}
	if c.GenerateBuildId == nil {
		c.GenerateBuildId = func() string { return uuid4() }
	}
	if c.MetricsInc == nil {
		c.MetricsInc = func(compileFromHistory bool, method string) {}
	}
	if c.HistorySync == nil {
		c.HistorySync = histwriter.SyncResourcesToDisk
	}
	if c.ResourceSync == nil {
		c.ResourceSync = resourcewriter.SyncResourcesToDisk
	}
	if c.ScheduleOutputCleanup == nil {
		c.ScheduleOutputCleanup = func(outputDir, conversionID string) {
			conversionoutputcleaner.ScheduleCleanup(outputDir, conversionID)
		}
	}
	return c
}

// uuid4 mirrors crypto.randomUUID (conversionId / default buildId).
func uuid4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// UploadedFile mirrors req.file (Node multer single 'qqfile').
type UploadedFile struct {
	Path        string
	ContentType string
	Original    string
	Size        int64
}

// --- narrows (requestparser decoded shape -> writer typed surface) ----------
// The Go conversion controller re-projects the parsed body onto the two
// writer request types (HRW for isCompileFromHistory, RW otherwise), exactly
// as Node's single `request` object drives both writers.

func snapshotOf(v interface{}) map[string]any {
	if v == nil {
		return nil
	}
	var m map[string]any
	if jsonRound(v, &m) != nil {
		return nil
	}
	return m
}

func globalBlobsOf(v interface{}) []string {
	if v == nil {
		return nil
	}
	var out []string
	if jsonRound(v, &out) != nil {
		return nil
	}
	return out
}

func rawChangeOpsOf(v interface{}) [][]map[string]any {
	if v == nil {
		return nil
	}
	var out [][]map[string]any
	if jsonRound(v, &out) != nil {
		return nil
	}
	return out
}

func jsonRound(v, out interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// buildHRWRequest ports the history-writer request (Node: the same parsed
// object; metricsOpts={} + png2pdf=false are the controller's in-place
// assignments — the Go defaults: MetricsPath "" and Png2pdf false).
func (c *Controller) buildHRWRequest(parsed requestparser.ParsedResponse,
	projectID, userID string) *histwriter.Request {
	cg := ""
	if parsed.CompileGroup != nil {
		cg = *parsed.CompileGroup
	}
	return &histwriter.Request{
		BaseHistoryVersion:  int(num64(parsed.BaseHistoryVersion)),
		RawSnapshot:         snapshotOf(parsed.RawSnapshot),
		GlobalBlobs:         globalBlobsOf(parsed.GlobalBlobs),
		RawChangeOperations: rawChangeOpsOf(parsed.RawChangeOperations),
		PopulateClsiCache:   parsed.PopulateClsiCache,
		Png2pdf:             false, // Node: request.png2pdf = false
		HistoryID:           strOf(parsed.HistoryID),
		FilestoreBlobPrefix: strOf(parsed.FilestoreBlobPrefix),
		ClSIPerfVariant:     strOf(parsed.ClSIPerfVariant),
		Draft:               parsed.Draft,
		RootResourcePath:    parsed.RootResourcePath,
		CompileGroup:        cg,
		// MetricsPath = "" mirrors request.metricsOpts = {} (Node sets it to
		// {}; the HRW reads metricsOpts.path undefined -> "").
	}
}

// buildRWRequest ports the resource-writer request (Node:
// ResourceWriter.syncResourcesToDisk(request, conversionDir); request is the
// parsed object with project_id/user_id assigned in place).
func (c *Controller) buildRWRequest(parsed requestparser.ParsedResponse,
	projectID, userID string) *resourcewriter.Request {
	resources := make([]resourcewriter.Resource, len(parsed.Resources))
	for i, r := range parsed.Resources {
		resources[i] = resourcewriter.Resource{
			Path:        r.Path,
			URL:         strOf(r.URL),
			FallbackURL: strOf(r.FallbackURL),
			Content:     []byte(strOf(r.Content)),
			Modified:    modifiedTime(r.Modified),
		}
	}
	return &resourcewriter.Request{
		ProjectID: projectID,
		UserID:    userID,
		SyncType:  strOf(parsed.SyncType),
		SyncState: strOf(parsed.SyncState),
		Resources: resources,
		// MetricsPath: "" mirrors metricsOpts = {} (Node request.metricsOpts
		// reset to an empty object by the controller).
	}
}

// --- cleanup (Node cleanupDirs) ------------------------------------------------

// cleanDirs mirrors the Node trailing cleanup loop:
//
//	for (const dir of cleanupDirs) {
//	  try { await fs.rm(dir, { recursive: true, force: true }) } catch ...
//	}
//
// (best-effort: each rm logs a warning and continues).
func cleanDirs(dirs []string) {
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			// Node: logger.warn({err, dir}, 'cleanup failed').
			_ = err
		}
	}
}

func strOf(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func num64(v interface{}) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	}
	return 0
}

// modifiedTime ports the modified->Date coercion (requestparser normalized to
// epoch ms float64).
func modifiedTime(v interface{}) *time.Time {
	if v == nil {
		return nil
	}
	if ms, ok := v.(float64); ok {
		t := time.UnixMilli(int64(ms)).UTC()
		return &t
	}
	return nil
}

// --- response helpers (Node: res.status/attachment/setHeader/pipeline) -------

// writeJSON mirrors res.status(code).json(v).
func writeJSON(res http.ResponseWriter, code int, v map[string]any) {
	res.Header().Set("Content-Type", "application/json; charset=utf-8")
	res.WriteHeader(code)
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	_, _ = res.Write(data)
}

// sendStatus mirrors express res.sendStatus(code) (plain text status body
// "404 Not Found\n" etc).
func sendStatus(res http.ResponseWriter, code int) {
	res.Header().Set("Content-Type", "text/html; charset=utf-8")
	res.WriteHeader(code)
	body := strconv.Itoa(code) + " " + http.StatusText(code) + "\n"
	_, _ = res.Write([]byte(body))
}

// GetOutputFile serves a staged conversion artifact from
// OutputDir/{project}/generated-files/{build}/{file} (Node CLSI getOutputFile)
// — the web plane streams it back as the attachment (output.docx / the
// markdown+html output.zip).
func (c *Controller) GetOutputFile(res http.ResponseWriter, projectID, buildID, file string) (int, error) {
	if projectID == "" || buildID == "" || file == "" ||
		strings.Contains(file, "..") || strings.HasPrefix(file, "/") {
		sendStatus(res, http.StatusNotFound)
		return http.StatusNotFound, nil
	}
	baseDir := filepath.Join(c.Config.OutputDir, projectID, "generated-files", buildID)
	fullPath := filepath.Join(baseDir, filepath.Clean("/"+file))
	// traversal guard: the joined path must stay under baseDir.
	if fullPath == baseDir || !strings.HasPrefix(fullPath, baseDir+string(filepath.Separator)) {
		sendStatus(res, http.StatusNotFound)
		return http.StatusNotFound, nil
	}
	info, err := os.Stat(fullPath)
	if err != nil || !info.Mode().IsRegular() {
		sendStatus(res, http.StatusNotFound)
		return http.StatusNotFound, nil
	}
	f, err := os.Open(fullPath)
	if err != nil {
		sendStatus(res, http.StatusNotFound)
		return http.StatusNotFound, nil
	}
	defer f.Close()
	// Node getOutputFile: res.attachment(filename) (basename) + the
	// Content-Type by extension + nosniff.
	switch strings.ToLower(filepath.Ext(fullPath)) {
	case ".docx":
		res.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	case ".zip":
		res.Header().Set("Content-Type", "application/zip")
	default:
		res.Header().Set("Content-Type", "application/octet-stream")
	}
	res.Header().Set("Content-Disposition", "attachment; filename="+info.Name())
	res.Header().Set("X-Content-Type-Options", "nosniff")
	res.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	io.Copy(res, f) // nolint:errcheck — the write loop ends the request
	return http.StatusOK, nil
}

// streamDownload mirrors the Node streaming block:
//
//	const x = await fs.stat(path)
//	res.setHeader('Content-Length', x.size)
//	res.attachment(name)
//	res.setHeader('X-Content-Type-Options', 'nosniff')
//	await pipeline(fsSync.createReadStream(path), res)
func streamDownload(res http.ResponseWriter, path, attachmentName string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	res.Header().Set("Content-Length", strconv.FormatInt(fi.Size(), 10))
	setAttachment(res, attachmentName)
	res.Header().Set("X-Content-Type-Options", "nosniff")
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	// Node: pipeline(readStream, res) — same bytes, Go plumbing.
	_, err = io.Copy(res, f)
	return err
}

// setAttachment mirrors express res.attachment(name): Content-Type by
// extension + "attachment; filename=name" disposition.
func setAttachment(res http.ResponseWriter, name string) {
	mime := "application/octet-stream"
	switch filepath.Ext(name) {
	case ".docx":
		mime = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".zip":
		mime = "application/zip"
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	}
	res.Header().Set("Content-Type", mime)
	res.Header().Set("Content-Disposition", "attachment; filename="+name)
}

// copyFile mirrors Node fs.copyFile (the json branch: copy a document into
// the staged build dir).
func copyFile(srcPath, dst string) error {
	data, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
