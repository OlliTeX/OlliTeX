// routes.go ports the app.js route handlers: the (int, error) handlers
// (CompileController / ConversionController / OutputController / health)
// are wrapped with the express error-middleware contract, and the plain
// /status endpoint.
package apps

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	cc "ollitex/go/services/clsitex/compilecontroller"
	cv "ollitex/go/services/clsitex/conversioncontroller"
	fum "ollitex/go/services/clsitex/fileuploadmiddleware"
	otc "ollitex/go/services/clsitex/outputcontroller"
)

// --- expressify wrapper (the (int, error) handler contract) ----------------

// handle wraps a controller handler that returns (code, error) and
// applies the app-level error middleware (error.go) to the Node
// next(error) case (code == 0 + error). See the package doc for the full
// contract.
func (a *App) handle(w http.ResponseWriter, r *http.Request, f func(w http.ResponseWriter) (int, error)) {
	code, err := f(w)
	if code == 0 && err != nil {
		a.handleValidationError(w, 0, r.URL.String(), err)
		return
	}
	if err != nil {
		// The handler wrote a body and then failed (Node: next(error)
		// after a partial write — the middleware renders nothing).
		_ = code
	}
}

// --- compile routes (project-scoped + per-user) ------------------------------

// projectUser mirrors the express {project_id, user_id?} params pair.
func (a *App) projectUser(r *http.Request) cc.ProjectUser {
	p := cc.ProjectUser{ProjectID: r.PathValue("project_id")}
	if uid := r.PathValue("user_id"); uid != "" {
		p.UserID = uid
	}
	return p
}

func (a *App) compile(w http.ResponseWriter, r *http.Request) {
	body, ok := a.readJSON(w, r)
	if !ok {
		return
	}
	params := a.projectUser(r)
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CC.Compile(w, params, body)
	})
}

func (a *App) stopCompile(w http.ResponseWriter, r *http.Request) {
	params := a.projectUser(r)
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CC.StopCompile(w, params)
	})
}

func (a *App) clearCache(w http.ResponseWriter, r *http.Request) {
	params := a.projectUser(r)
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CC.ClearCache(w, params)
	})
}

func (a *App) syncCode(w http.ResponseWriter, r *http.Request) {
	params := a.projectUser(r)
	q := parseSyncQuery(r.URL.Query())
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CC.SyncFromCode(w, params, q)
	})
}

func (a *App) syncPdf(w http.ResponseWriter, r *http.Request) {
	params := a.projectUser(r)
	q := parseSyncQuery(r.URL.Query())
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CC.SyncFromPdf(w, params, q)
	})
}

func (a *App) wordcount(w http.ResponseWriter, r *http.Request) {
	params := a.projectUser(r)
	file, image := r.URL.Query().Get("file"), r.URL.Query().Get("image")
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CC.Wordcount(w, params, file, image)
	})
}

func (a *App) wordcountPost(w http.ResponseWriter, r *http.Request) {
	body, ok := a.readJSON(w, r)
	if !ok {
		return
	}
	params := a.projectUser(r)
	file, image := r.URL.Query().Get("file"), r.URL.Query().Get("image")
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CC.WordcountWithSync(w, params, file, image, body)
	})
}

// --- output.zip routes -----------------------------------------------------

func (a *App) outputZipNoUser(w http.ResponseWriter, r *http.Request) {
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		req := otc.Request{ProjectID: r.PathValue("project_id"),
			BuildID: r.PathValue("build_id")}
		return otc.CreateOutputZip(w, a.Config.Path.OutputDir, req)
	})
}

func (a *App) outputZipUser(w http.ResponseWriter, r *http.Request) {
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		req := otc.Request{ProjectID: r.PathValue("project_id"),
			UserID:  r.PathValue("user_id"),
			BuildID: r.PathValue("build_id")}
		return otc.CreateOutputZip(w, a.Config.Path.OutputDir, req)
	})
}

// --- convert routes (FUM multipart; Node FileUploadMiddleware) -------------

func (a *App) convertRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /convert/docx-to-latex", a.convertDocx)
	mux.HandleFunc("POST /convert/document-to-latex", a.convertDocument)
	mux.HandleFunc("POST /convert/pdf-to-jpeg", a.convertPDFToJPEG)
	mux.HandleFunc("POST /project/{project_id}/user/{user_id}/download/project-to-document",
		a.convertProject)
	// audit-013: prepared conversion artifact (Node CLSI getOutputFile) —
	// the web conversion-download route streams this back. Deliberately NOT
	// under /build/{id}/output/{file}: Go ServeMux gives the literal
	// /output/output.zip routes (server.go, the compile-flow zips) precedence
	// over a {filepath...} wildcard, which would shadow markdown/html exports.
	mux.HandleFunc("GET /project/{project_id}/user/{user_id}/download/build/{build_id}/output/{filepath...}",
		a.convertOutputFile)
	mux.HandleFunc("GET /project/{project_id}/download/build/{build_id}/output/{filepath...}",
		a.convertOutputFile)
}

func (a *App) convertOutputFile(w http.ResponseWriter, r *http.Request) {
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CV.GetOutputFile(w, r.PathValue("project_id"),
			r.PathValue("build_id"), r.PathValue("filepath"))
	})
}

func (a *App) convertDocx(w http.ResponseWriter, r *http.Request) {
	a.FUM.Handle(func(w http.ResponseWriter, r *http.Request, uf *fum.UploadedFile) {
		a.handle(w, r, func(w http.ResponseWriter) (int, error) {
			return a.CV.ConvertDocxToLaTeX(w, cvFile(uf))
		})
	}).ServeHTTP(w, r)
}

func (a *App) convertDocument(w http.ResponseWriter, r *http.Request) {
	q := cv.TypeQuery{Type: r.URL.Query().Get("type")}
	a.FUM.Handle(func(w http.ResponseWriter, r *http.Request, uf *fum.UploadedFile) {
		a.handle(w, r, func(w http.ResponseWriter) (int, error) {
			return a.CV.ConvertDocumentToLaTeX(w, cvFile(uf), q)
		})
	}).ServeHTTP(w, r)
}

func (a *App) convertPDFToJPEG(w http.ResponseWriter, r *http.Request) {
	q := cv.PDFQuery{Mode: r.URL.Query().Get("mode")}
	a.FUM.Handle(func(w http.ResponseWriter, r *http.Request, uf *fum.UploadedFile) {
		a.handle(w, r, func(w http.ResponseWriter) (int, error) {
			return a.CV.ConvertPDFToJPEG(w, cvFile(uf), q)
		})
	}).ServeHTTP(w, r)
}

// convertProject is the JSON (NO upload) route: parse the compile body then
// sync + convert + stream.
func (a *App) convertProject(w http.ResponseWriter, r *http.Request) {
	body, ok := a.readJSON(w, r)
	if !ok {
		return
	}
	cvParams := cv.ProjectUser{ProjectID: r.PathValue("project_id"),
		UserID: r.PathValue("user_id")}
	q := cv.ProjectToDocumentQuery{
		Type:           r.URL.Query().Get("type"),
		ResponseFormat: r.URL.Query().Get("responseFormat"),
	}
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CV.ConvertProjectToDocument(w, cvParams, q, body)
	})
}

// cvFile maps the fum upload onto the controller's req.file (Node multer
// single 'qqfile'; same field layout — the Go types mirror each other).
func cvFile(uf *fum.UploadedFile) *cv.UploadedFile {
	return &cv.UploadedFile{
		Path:        uf.Path,
		ContentType: uf.ContentType,
		Original:    uf.Original,
		Size:        uf.Size,
	}
}

// --- health + misc ------------------------------------------------------------

func (a *App) status(w http.ResponseWriter, r *http.Request) {
	a.handle(w, r, func(w http.ResponseWriter) (int, error) {
		return a.CC.Status(w)
	})
}

// statusPing is the bare GET /status (Node app.js: 'CLSI is alive\\n').
func (a *App) statusPing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, statusLine("CLSI is alive\\n"))
}

// healthCheck ports app.js:73-80 (Node /health_check, 500 JSON guards).
func (a *App) healthCheck(w http.ResponseWriter, r *http.Request) {
	if a.ProcessTooOld != nil && *a.ProcessTooOld {
		_ = a.writeJSON(w, 500, map[string]any{"processTooOld": true})
		return
	}
	if a.diskCritical() {
		_ = a.writeJSON(w, 500, map[string]any{"diskCritical": true})
		return
	}
	a.SmokeTest.SendLastResult(w)
}

func (a *App) smokeTestForce(w http.ResponseWriter, r *http.Request) {
	// Node: await smokeTest.sendNewResult(res).catch(next) — the method
	// itself renders 200 'OK' or the 500 error message; nothing to
	// forward on success.
	a.SmokeTest.SendNewResult(w)
}

// --- small wire helpers -------------------------------------------------------

func (a *App) writeJSON(w http.ResponseWriter, code int, body map[string]any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, err = w.Write(data)
	return err
}

// --- compile body (Node express.json({limit})) ---------------------------------

// readJSON bounds + parses the compile body. ok=false means the client
// has already received a response (413 over-limit / 400 read-or-parse
// error, rendered as Node's express.json ByteLimitError / SyntaxError
// statusCode through the same plain res.sendStatus chain).
//
// Node divergences:
//   - The body is drained with io.LimitReader (the Go server closes the
//     connection for oversized bodies, as Node's socket does).
//   - A missing / empty / non-JSON body forwards an empty map: the
//     controller's RequestParser port rejects it with the same forwarded
//     error Node emits for the absent 'compile' attribute.
func (a *App) readJSON(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	limit := compileSizeBytes(a.Config.CompileSizeLimit)
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		a.plainStatus(w, 400)
		return nil, false
	}
	if limit > 0 && int64(len(data)) > limit {
		// Node express.json 413: res.sendStatus(413).
		a.plainStatus(w, 413)
		return nil, false
	}
	body := map[string]any{}
	if len(data) > 0 {
		err := json.Unmarshal(data, &body)
		if err != nil {
			// Node express.json SyntaxError: res.sendStatus(400).
			a.plainStatus(w, 400)
			return nil, false
		}
	}
	return body, true
}

// compileSizeBytes ports bytes.parse for the settings.compileSizeLimit
// tokens (default '7mb'): b/kb/k/mb/m/gb/g.
func compileSizeBytes(s string) int64 {
	s = strings.TrimSpace(strings.ToLower(s))
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "kb"):
		s = strings.TrimSuffix(s, "kb")
		mult = 1000
	case strings.HasSuffix(s, "mb"):
		s = strings.TrimSuffix(s, "mb")
		mult = 1e6
	case strings.HasSuffix(s, "gb"):
		s = strings.TrimSuffix(s, "gb")
		mult = 1e9
	case strings.HasSuffix(s, "k"):
		s = strings.TrimSuffix(s, "k")
		mult = 1024
	case strings.HasSuffix(s, "m"):
		s = strings.TrimSuffix(s, "m")
		mult = 1e6
	case strings.HasSuffix(s, "g"):
		s = strings.TrimSuffix(s, "g")
		mult = 1e9
	case strings.HasSuffix(s, "b"):
		s = strings.TrimSuffix(s, "b")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n * mult
}
