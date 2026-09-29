package conversioncontroller

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	clserrors "ollitex/go/services/clsitex/errors"
	"ollitex/go/services/clsitex/logger"
	"ollitex/go/services/clsitex/requestparser"
)

// --- query/param structs (Node zod query strictObjects; the routing-only
// --- compileBackendClass / compileGroup fields are omitted, as Node does not
// --- read them again after the strictObject allows them) ------------------------

// TypeQuery mirrors the convertDocumentToLaTeXSchema query {type}.
type TypeQuery struct {
	Type string // "docx" | "markdown"
}

// PDFQuery mirrors the convertPDFToJPEGSchema query {mode}.
type PDFQuery struct {
	Mode string // "preview" | "thumbnail"
}

// ProjectToDocumentQuery mirrors the convertProjectToDocumentSchema query.
type ProjectToDocumentQuery struct {
	Type           string // "docx" | "markdown" | "html"
	ResponseFormat string // "json" | "stream"; "" => "stream"
}

// ProjectUser is the {project_id, user_id} params pair.
type ProjectUser struct {
	ProjectID string
	UserID    string
}

// --- convertDocxToLaTeX (Node legacy alias: type fixed to 'docx') -------------

// ConvertDocxToLaTeX ports convertDocxToLaTeX (Node legacy alias of
// convertDocumentToLaTeX: conversionType fixed to 'docx' rather than read
// from the query string).
func (c *Controller) ConvertDocxToLaTeX(res http.ResponseWriter, file *UploadedFile) (int, error) {
	if !c.Config.EnablePandocConversions {
		os.Remove(file.Path) // Node: fs.unlink(...).catch
		sendStatus(res, http.StatusNotFound)
		return http.StatusNotFound, nil
	}
	// Node: uploadedFileOnlySchema (file only; no query fields read).
	return c.runDocumentToLaTeXConversion(res, file.Path, "docx")
}

// --- convertDocumentToLaTeX ------------------------------------------------------

// ConvertDocumentToLaTeX ports convertDocumentToLaTeX: feature-gate,
// type enum, then the shared runDocumentToLaTeXConversion.
func (c *Controller) ConvertDocumentToLaTeX(res http.ResponseWriter, file *UploadedFile, q TypeQuery) (int, error) {
	if !c.Config.EnablePandocConversions {
		os.Remove(file.Path)
		sendStatus(res, http.StatusNotFound)
		return http.StatusNotFound, nil
	}
	// strictObject type enum (docx|markdown); compileBackendClass/compileGroup
	// are allowed by the schema (routing-only) and not read further.
	if q.Type != "docx" && q.Type != "markdown" {
		os.Remove(file.Path) // Node: parseUploadedFileReq unlink-on-fail
		// Node: zod error forwarded -> 500 (the error middleware shape).
		return 0, clserrors.NewOError("unsupported conversion type",
			map[string]any{"type": q.Type})
	}
	return c.runDocumentToLaTeXConversion(res, file.Path, q.Type)
}

// runDocumentToLaTeXConversion ports the shared runDocumentToLaTeXConversion:
// convertToLaTeXWithLock -> 422 error envelope | stream conversion.zip.
func (c *Controller) runDocumentToLaTeXConversion(res http.ResponseWriter, path, conversionType string) (int, error) {
	logger.Debug(map[string]any{"path": path, "conversionType": conversionType},
		"received file for conversion")
	conversionID := c.NewUUID()
	defer os.Remove(path) // Node finally: fs.unlink(path).catch()

	zipPath, cerr := c.Manager.ConvertToLaTeXWithLock(conversionID, path, conversionType)
	if cerr != nil {
		var ce *clserrors.ConversionError
		if errors.As(cerr, &ce) {
			if ce.UserFacing {
				writeJSON(res, http.StatusUnprocessableEntity, map[string]any{
					"error":    ce.Stderr,
					"exitCode": ce.ExitCode,
				})
			} else {
				logger.Warn(map[string]any{
					"err":            cerr,
					"conversionType": conversionType,
					"stderr":         ce.Stderr,
				}, "Conversion failed with non-user-facing error")
				writeJSON(res, http.StatusUnprocessableEntity, map[string]any{})
			}
			return http.StatusUnprocessableEntity, nil
		}
		return 0, cerr
	}
	defer os.RemoveAll(filepath.Dir(zipPath)) // Node finally: fs.rm(dirname(zipPath))
	if serr := streamDownload(res, zipPath, "conversion.zip"); serr != nil {
		return 0, serr
	}
	return 200, nil
}

// --- convertPDFToJPEG ----------------------------------------------------------------

// ConvertPDFToJPEG ports convertPDFToJPEG: feature-gate, mode enum, then
// convertPDFToJPEGWithLock -> stream output.jpg.
//
// Note: Node's try block has NO catch — every error (incl. ConversionError)
// is forwarded (next(err) -> 500), unlike the LaTeX conversions which map
// ConversionError -> 422.
func (c *Controller) ConvertPDFToJPEG(res http.ResponseWriter, file *UploadedFile, q PDFQuery) (int, error) {
	if !c.Config.EnablePdfConversions {
		os.Remove(file.Path)
		sendStatus(res, http.StatusNotFound)
		return http.StatusNotFound, nil
	}
	if q.Mode != "preview" && q.Mode != "thumbnail" {
		os.Remove(file.Path) // Node: schema error -> forwarded (500)
		return 0, clserrors.NewOError("unsupported conversion mode",
			map[string]any{"mode": q.Mode})
	}
	logger.Debug(map[string]any{"path": file.Path, "mode": q.Mode},
		"received pdf for conversion to jpeg")
	conversionID := c.NewUUID()
	defer os.Remove(file.Path) // Node finally: fs.unlink(path).catch()

	jpegPath, cerr := c.Manager.ConvertPDFToJPEGWithLock(conversionID, file.Path, q.Mode)
	if cerr != nil {
		// Node: no ConversionError catch here -> forwarded (500).
		return 0, cerr
	}
	defer os.RemoveAll(filepath.Dir(jpegPath)) // Node finally: fs.rm(dirname(jpegPath))
	if serr := streamDownload(res, jpegPath, "output.jpg"); serr != nil {
		return 0, serr
	}
	return 200, nil
}

// --- convertProjectToDocument ------------------------------------------------------------

// ConvertProjectToDocument ports convertProjectToDocument (the JSON route —
// NO upload): feature-gate, schema, parse the compile body, sync resources
// (HRW or RW branch), convert, then stream (stream mode) or stage+copy (json
// mode), with the trailing cleanupDirs loop (finally).
func (c *Controller) ConvertProjectToDocument(res http.ResponseWriter, params ProjectUser,
	q ProjectToDocumentQuery, body map[string]any) (int, error) {
	if !c.Config.EnablePandocConversions {
		sendStatus(res, http.StatusNotFound)
		return http.StatusNotFound, nil
	}

	// strictObject: type enum (docx|markdown|html) + responseFormat enum
	// (optional, default 'stream').
	responseFormat := q.ResponseFormat
	if responseFormat == "" {
		responseFormat = "stream"
	}
	_, isKnownType := conversionConfigs[q.Type]
	isKnownRf := responseFormat == "json" || responseFormat == "stream"
	if !isKnownType || !isKnownRf {
		// Node: zod error forwarded (500); no upload here to unlink.
		return 0, clserrors.NewOError("unsupported type/responseFormat",
			map[string]any{"type": q.Type, "responseFormat": responseFormat})
	}

	parsed, perr := requestparser.Parse(body, c.ParserConfig)
	if perr != nil {
		return 0, perr
	}
	// Node: request.project_id = projectId (and .user_id when present);
	// request.metricsOpts = {}; request.png2pdf = false.
	projectID, userID := params.ProjectID, params.UserID
	typeKey := q.Type

	conversionID := c.NewUUID()
	conversionDir := filepath.Join(c.Config.CompilesDir, conversionID)
	conversionCacheDir := filepath.Join(c.Config.ClsiCacheDir, conversionID)
	projectCacheDir := filepath.Join(c.Config.ClsiCacheDir, projectID)
	cleanupDirs := []string{conversionCacheDir, conversionDir}
	defer cleanDirs(cleanupDirs)

	logger.Debug(map[string]any{
		"projectId":        projectID,
		"userId":           userID,
		"rootResourcePath": parsed.RootResourcePath,
		"type":             typeKey,
	}, "syncing resources for project-to-document conversion")
	if c.MetricsInc != nil {
		c.MetricsInc(parsed.IsCompileFromHistory, typeKey)
	}

	// Node: if (await fs.mkdir(projectCacheDir, {recursive: true})) { ... }
	// is always falsy (Node fs.mkdir resolves undefined) so projectCacheDir
	// is NEVER pushed onto cleanupDirs; mirror that (best-effort mkdir only).
	os.MkdirAll(projectCacheDir, 0o755)

	if parsed.IsCompileFromHistory {
		os.MkdirAll(conversionDir, 0o755) // Node: fs.mkdir(conversionDir)
		hrw := c.buildHRWRequest(parsed, projectID, userID)
		_, syncErr := c.HistorySync(context.Background(), projectID, userID,
			hrw, conversionDir, map[string]any{}, map[string]any{})
		if syncErr != nil {
			var mue *clserrors.MissingUpdatesError
			if errors.As(syncErr, &mue) {
				// Node: return res.status(409).json({baseHistoryVersion}).
				writeJSON(res, http.StatusConflict, map[string]any{
					"baseHistoryVersion": mue.Info["baseHistoryVersion"],
				})
				return http.StatusConflict, nil
			}
			return 0, syncErr
		}
	} else {
		rw := c.buildRWRequest(parsed, projectID, userID)
		if _, syncErr := c.ResourceSync(rw, conversionDir); syncErr != nil {
			return 0, syncErr
		}
	}

	documentPath, cerr := c.Manager.ConvertLaTeXToDocumentInDirWithLock(
		conversionID, conversionDir, parsed.RootResourcePath, typeKey)
	if cerr != nil {
		var ce *clserrors.ConversionError
		if errors.As(cerr, &ce) {
			if ce.UserFacing {
				writeJSON(res, http.StatusUnprocessableEntity, map[string]any{
					"error":    ce.Stderr,
					"exitCode": ce.ExitCode,
				})
			} else {
				logger.Warn(map[string]any{
					"err":    cerr,
					"type":   typeKey,
					"stderr": ce.Stderr,
				}, "Conversion failed with non-user-facing error")
				writeJSON(res, http.StatusUnprocessableEntity, map[string]any{})
			}
			return http.StatusUnprocessableEntity, nil
		}
		return 0, cerr
	}

	outputName := "output." + conversionConfigs[typeKey]

	if responseFormat == "json" {
		buildID := c.GenerateBuildId()
		buildDir := filepath.Join(
			c.Config.OutputDir, conversionID, "generated-files", buildID)
		os.MkdirAll(buildDir, 0o755) // Node: fs.mkdir(buildDir, {recursive: true})
		cerr = copyFile(documentPath, filepath.Join(buildDir, outputName))
		c.ScheduleOutputCleanup(c.Config.OutputDir, conversionID) // Node finally
		if cerr != nil {
			// Node: error propagates (500) after the finally scheduleCleanup.
			return 0, cerr
		}
		writeJSON(res, http.StatusOK, map[string]any{
			"conversionId": conversionID,
			"buildId":      buildID,
			"file":         outputName,
		})
		return http.StatusOK, nil
	}

	// stream mode: Content-Length + attachment(outputName) + nosniff.
	if serr := streamDownload(res, documentPath, outputName); serr != nil {
		return 0, serr
	}
	return 200, nil
}
